package fixtures

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"calendar/server/internal/api"
	"calendar/server/internal/domain"
	"calendar/server/internal/events"
	"calendar/server/internal/provider"
	"calendar/server/internal/recur"
	"calendar/server/internal/tz"
)

// Write outcomes (--fixtures-write, or the X-Fixture-Outcome request header).
const (
	OutcomeOK          = "ok"
	OutcomeFail        = "fail"        // 502 provider_error with a plain message
	OutcomeConflict    = "conflict"    // 409 conflict with `current` (the event was "edited elsewhere")
	OutcomeSlow        = "slow"        // succeeds after 5 s
	OutcomeRateLimited = "ratelimited" // 429 rate_limited, Retry-After 30
)

var Outcomes = []string{OutcomeOK, OutcomeFail, OutcomeConflict, OutcomeSlow, OutcomeRateLimited}

func ValidOutcome(s string) bool {
	for _, o := range Outcomes {
		if o == s {
			return true
		}
	}
	return s == ""
}

// shared is the mutable part of the fixture world; copies of Data share it.
type shared struct {
	mu  sync.RWMutex
	rev int64
	seq int
}

type idemEntry struct {
	hash   [32]byte
	status int
	body   []byte
}

// writeState is the per-server bookkeeping of the write endpoints.
type writeState struct {
	mu   sync.Mutex
	idem map[string]idemEntry
	subs map[chan int64]struct{}
	subM sync.Mutex
}

func (w *writeState) subscribe() (chan int64, func()) {
	ch := make(chan int64, 8)
	w.subM.Lock()
	if w.subs == nil {
		w.subs = map[chan int64]struct{}{}
	}
	w.subs[ch] = struct{}{}
	w.subM.Unlock()
	return ch, func() { w.subM.Lock(); delete(w.subs, ch); w.subM.Unlock() }
}

func (w *writeState) publish(rev int64) {
	w.subM.Lock()
	defer w.subM.Unlock()
	for ch := range w.subs {
		select {
		case ch <- rev:
		default:
		}
	}
}

// Rev is the current revision (starts at 1, +1 per accepted write).
func (d *Data) Rev() int64 {
	d.st.mu.RLock()
	defer d.st.mu.RUnlock()
	return d.st.rev
}

type werr struct {
	status  int
	code    string
	msg     string
	field   string
	current *api.Event
	retry   int
}

func (e *werr) write(w http.ResponseWriter) {
	if e.retry > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(e.retry))
	}
	var b struct {
		Error struct {
			Code    string     `json:"code"`
			Message string     `json:"message"`
			Field   string     `json:"field,omitempty"`
			Current *api.Event `json:"current,omitempty"`
		} `json:"error"`
	}
	b.Error.Code, b.Error.Message, b.Error.Field, b.Error.Current = e.code, e.msg, e.field, e.current
	api.WriteJSON(w, e.status, b)
}

func verr(field, msg string) *werr {
	return &werr{status: 400, code: api.CodeValidation, msg: msg, field: field}
}

func (s *Server) writeRoutes(mux *http.ServeMux, auth func(http.HandlerFunc) http.Handler) {
	w := func(h func(r *http.Request, body []byte) (int, any, *werr)) http.Handler {
		return auth(func(rw http.ResponseWriter, r *http.Request) { s.doWrite(rw, r, h) })
	}
	mux.Handle("POST /v1/events", w(s.wCreate))
	mux.Handle("PATCH /v1/events/{id}", w(s.wPatch))
	mux.Handle("DELETE /v1/events/{id}", w(s.wDelete))
	mux.Handle("POST /v1/events/{id}/rsvp", w(s.wRSVP))
	mux.Handle("POST /v1/events/{id}/meet", w(s.wMeet))
	mux.Handle("POST /v1/events/{id}/move", w(s.wMove))
	mux.Handle("PUT /v1/events/{id}/reminder", w(s.wReminder))
}

func (s *Server) latency() time.Duration {
	if s.WriteLatency > 0 {
		return s.WriteLatency
	}
	if s.WriteLatency < 0 {
		return 0
	}
	return 600 * time.Millisecond
}

func (s *Server) slow() time.Duration {
	if s.SlowDelay > 0 {
		return s.SlowDelay
	}
	return 5 * time.Second
}

func sleepCtx(r *http.Request, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	select {
	case <-time.After(d):
		return true
	case <-r.Context().Done():
		return false
	}
}

// doWrite wraps a mutation with latency, forced outcomes and Idempotency-Key handling.
func (s *Server) doWrite(rw http.ResponseWriter, r *http.Request, h func(r *http.Request, body []byte) (int, any, *werr)) {
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	ws := s.ws
	ws.mu.Lock()
	defer ws.mu.Unlock() // one write at a time, like the per-account lock
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	h256 := sha256.Sum256(append([]byte(r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("If-Match")+"\n"), b...))
	if key != "" {
		if e, ok := ws.idem[key]; ok {
			if e.hash != h256 {
				(&werr{status: 422, code: api.CodeValidation, msg: "this Idempotency-Key was already used with a different request", field: "Idempotency-Key"}).write(rw)
				return
			}
			rw.Header().Set("Idempotent-Replayed", "true")
			if e.status == http.StatusNoContent {
				rw.WriteHeader(e.status)
				return
			}
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(e.status)
			rw.Write(e.body)
			return
		}
	}
	if !sleepCtx(r, s.latency()) {
		return
	}
	outcome := r.Header.Get("X-Fixture-Outcome")
	if outcome == "" {
		outcome = s.WriteOutcome
	}
	if outcome == OutcomeSlow {
		if !sleepCtx(r, s.slow()) {
			return
		}
	}
	reminder := strings.HasSuffix(r.URL.Path, "/reminder")
	if !reminder {
		switch outcome {
		case OutcomeFail:
			(&werr{status: 502, code: "provider_error", msg: "Google didn't accept the change. Nothing was saved."}).write(rw)
			return
		case OutcomeRateLimited:
			(&werr{status: 429, code: "rate_limited", msg: "Google is limiting requests right now. Try again in a moment.", retry: 30}).write(rw)
			return
		case OutcomeConflict:
			if r.Method != http.MethodPost || r.URL.Path != "/v1/events" {
				if e := s.editedElsewhere(r.PathValue("id")); e != nil {
					e.write(rw)
					return
				}
			}
		}
	}
	status, body, e := h(r, b)
	if e != nil {
		e.write(rw)
		return
	}
	var out []byte
	if body != nil {
		out, _ = json.Marshal(body)
		out = append(out, '\n')
	}
	if key != "" {
		if ws.idem == nil {
			ws.idem = map[string]idemEntry{}
		}
		ws.idem[key] = idemEntry{h256, status, out}
	}
	if status == http.StatusCreated {
		if ev, ok := body.(api.Event); ok {
			rw.Header().Set("Location", "/v1/events/"+ev.EventID)
		}
	}
	if status == http.StatusNoContent {
		rw.WriteHeader(status)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	rw.Write(out)
}

// editedElsewhere simulates someone changing the event in Google: the title changes and the
// version moves on, and the 409 carries that latest state.
func (s *Server) editedElsewhere(id string) *werr {
	d := s.Data
	d.st.mu.Lock()
	i, ok := d.find(id)
	if !ok {
		d.st.mu.Unlock()
		return &werr{status: 404, code: api.CodeNotFound, msg: "no such event"}
	}
	for _, j := range d.series(d.items[i].EventID) {
		it := d.items[j]
		if !strings.HasSuffix(it.Title, " (edited elsewhere)") {
			it.Title += " (edited elsewhere)"
		}
		d.st.rev++
		it.Version = 1000 + d.st.rev
		d.put(j, it)
	}
	cur := d.details[d.items[i].ID]
	rev := d.st.rev
	d.st.mu.Unlock()
	s.ws.publish(rev)
	return &werr{status: 409, code: api.CodeConflict, msg: "This event changed elsewhere.", current: &cur}
}

// ---- store helpers (callers hold d.st.mu) ----

func (d *Data) find(id string) (int, bool) {
	for i, it := range d.items {
		if it.ID == id {
			return i, true
		}
	}
	for i, it := range d.items {
		if it.EventID == id {
			return i, true
		}
	}
	return 0, false
}

func (d *Data) series(eventID string) []int {
	var out []int
	for i, it := range d.items {
		if it.EventID == eventID {
			out = append(out, i)
		}
	}
	return out
}

// put stores an item and mirrors it into its detail record.
func (d *Data) put(i int, it api.AgendaItem) {
	old := d.items[i]
	ev := d.details[old.ID]
	delete(d.details, old.ID)
	ev.AgendaItem = it
	d.items[i] = it
	d.details[it.ID] = ev
}

func (d *Data) resort() {
	sort.SliceStable(d.items, func(i, j int) bool { return sortKey(d.items[i]).Before(sortKey(d.items[j])) })
}

func (d *Data) newEventID() string {
	d.st.seq++
	return fmt.Sprintf("01FIXNEW%018d", d.st.seq)
}

func occID(eid string, key int64) string { return fmt.Sprintf("%s:%d", eid, key) }

func (d *Data) calendar(id string) (api.Calendar, bool) {
	for _, c := range d.Calendars {
		if c.ID == id {
			return c, true
		}
	}
	return api.Calendar{}, false
}

func writableCal(c api.Calendar) bool { return c.Access == "owner" || c.Access == "writer" }

func parseIfMatch(r *http.Request) (int64, bool, *werr) {
	h := strings.TrimSpace(r.Header.Get("If-Match"))
	if h == "" || h == "*" {
		return 0, false, nil
	}
	n, err := strconv.ParseInt(strings.Trim(strings.TrimPrefix(h, "W/"), `"`), 10, 64)
	if err != nil {
		return 0, false, verr("If-Match", `If-Match must be the event version, e.g. "4812"`)
	}
	return n, true, nil
}

func checkScope(r *http.Request, def string) (provider.Scope, *werr) {
	switch sc := provider.Scope(r.URL.Query().Get("scope")); sc {
	case "":
		return provider.Scope(def), nil
	case provider.ScopeThis, provider.ScopeFollowing, provider.ScopeAll:
		return sc, nil
	}
	return "", verr("scope", "scope must be this, following or all")
}

// ---- time ----

func (d *Data) loc() *time.Location { l, _ := tz.Load(*d.Settings.UserTZID); return l }

// whenFor resolves a wire When against the item being edited (defaults for tz and duration).
func (d *Data) whenFor(w events.When, base *api.AgendaItem, cal api.Calendar) (allDay bool, start, end time.Time, tzid string, date, endDate string, e *werr) {
	if w.AllDay {
		sd, err := time.Parse(time.DateOnly, w.Date)
		if err != nil {
			return false, start, end, "", "", "", verr("when.date", "all-day events need when.date as YYYY-MM-DD")
		}
		ed := sd.AddDate(0, 0, 1)
		if w.EndDate != "" {
			if ed, err = time.Parse(time.DateOnly, w.EndDate); err != nil || !ed.After(sd) {
				return false, start, end, "", "", "", verr("when.endDate", "endDate is exclusive and must be after date")
			}
		} else if base != nil && base.AllDay {
			a, _ := time.Parse(time.DateOnly, base.Date)
			b, _ := time.Parse(time.DateOnly, base.EndDate)
			ed = sd.Add(b.Sub(a))
		}
		return true, start, end, "", sd.Format(time.DateOnly), ed.Format(time.DateOnly), nil
	}
	tzid = w.TZID
	if tzid == "" && base != nil && !base.AllDay {
		tzid = base.TZID
	}
	if tzid == "" {
		tzid = cal.TZID
	}
	if tzid == "" {
		tzid = *d.Settings.UserTZID
	}
	loc, warn := tz.Load(tzid)
	if warn != "" {
		return false, start, end, "", "", "", verr("when.tzid", "unknown time zone "+tzid)
	}
	var st time.Time
	var err error
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if st, err = time.ParseInLocation(layout, w.Start, loc); err == nil {
			break
		}
	}
	if err != nil {
		return false, start, end, "", "", "", verr("when.start", "must be a local time like 2026-10-16T13:00:00 (no zone suffix)")
	}
	dur := time.Hour
	switch {
	case w.DurationMin > 0:
		dur = time.Duration(w.DurationMin) * time.Minute
	case base != nil && !base.AllDay && base.Start != nil:
		dur = base.End.Sub(*base.Start)
	}
	return false, st.UTC(), st.UTC().Add(dur), tzid, "", "", nil
}

func applyWhen(it *api.AgendaItem, allDay bool, start, end time.Time, tzid, date, endDate string) {
	it.AllDay = allDay
	if allDay {
		it.Start, it.End, it.TZID = nil, nil, ""
		it.Date, it.EndDate = date, endDate
		return
	}
	s, e := start, end
	it.Start, it.End, it.TZID, it.Date, it.EndDate = &s, &e, tzid, "", ""
}

func rkeyOf(it api.AgendaItem) int64 {
	if it.AllDay {
		t, _ := time.Parse(time.DateOnly, it.Date)
		return t.Unix()
	}
	return it.Start.Unix()
}

// ---- handlers ----

func (s *Server) wCreate(r *http.Request, b []byte) (int, any, *werr) {
	var in events.CreateIn
	if len(bytes.TrimSpace(b)) == 0 {
		return 0, nil, verr("", "a JSON body is required")
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return 0, nil, verr("", "invalid JSON: "+err.Error())
	}
	d := s.Data
	d.st.mu.Lock()
	defer d.st.mu.Unlock()
	cal, ok := d.calendar(in.CalendarID)
	switch {
	case in.CalendarID == "" || !ok:
		return 0, nil, verr("calendarId", "no such calendar")
	case !writableCal(cal):
		return 0, nil, &werr{status: 403, code: api.CodeForbidden, msg: "this calendar is read-only", field: "calendarId"}
	case len(in.Title) > 1024:
		return 0, nil, verr("title", "too long")
	}
	allDay, st, en, tzid, date, endDate, e := d.whenFor(in.When, nil, cal)
	if e != nil {
		return 0, nil, e
	}
	var attendees []string
	for _, a := range in.Attendees {
		if !strings.Contains(a, "@") {
			return 0, nil, verr("attendees", "not an email address: "+a)
		}
		attendees = append(attendees, a)
	}
	var rule string
	if in.Recurrence != nil {
		rule = strings.TrimPrefix(strings.TrimSpace(in.Recurrence.RRule), "RRULE:")
		if rule == "" {
			return 0, nil, verr("recurrence.rrule", "rrule is empty")
		}
	}
	eid := d.newEventID()
	d.st.rev++
	proto := api.AgendaItem{EventID: eid, Version: 1000 + d.st.rev, CalendarID: cal.ID, AccountID: cal.AccountID, Color: cal.Color, Title: in.Title,
		Organizer: &api.Person{Email: d.Accounts[0].Email, Name: "Alex", IsSelf: true}, Location: in.Location, Status: "confirmed", Busy: true, Recurring: rule != "", CanEdit: true, CanAddConference: in.Conference != "meet" && !allDay}
	if in.Conference == "meet" {
		proto.Join = &api.Join{URL: "https://meet.google.com/new-" + strings.ToLower(eid[len(eid)-6:]) + "-xyz", Kind: "meet"}
	}
	applyWhen(&proto, allDay, st, en, tzid, date, endDate)
	mkDetail := func(it api.AgendaItem) api.Event {
		ev := api.Event{AgendaItem: it, Description: in.Description, UpdatedAt: ptr(d.Now), CalendarName: cal.Name, ReminderMin: in.ReminderMin}
		if len(attendees) > 0 {
			ev.Attendees = append([]api.Attendee{{Email: d.Accounts[0].Email, Name: "Alex", Response: "accepted", IsSelf: true, IsOrganizer: true}}, nil...)
			for _, a := range attendees {
				ev.Attendees = append(ev.Attendees, api.Attendee{Email: a, Response: "needsAction"})
			}
		}
		if rule != "" {
			ev.Recurrence = &api.Recurrence{RRule: rule, Summary: "Repeats"}
		}
		return ev
	}
	first := ""
	add := func(it api.AgendaItem) {
		it.ID = occID(eid, rkeyOf(it))
		d.items = append(d.items, it)
		d.details[it.ID] = mkDetail(it)
		if first == "" || strings.Compare(it.ID, first) < 0 && false {
			first = it.ID
		}
	}
	if rule == "" {
		add(proto)
	} else {
		loc := d.loc()
		ser := recur.Series{AllDay: allDay, TZID: tzid, Start: st, End: en, StartDate: date, EndDate: endDate, RRule: rule}
		lo := d.Now.In(loc)
		lo = time.Date(lo.Year(), lo.Month(), lo.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -2)
		occs, _ := recur.Expand(ser, recur.Window{From: lo.UTC(), To: lo.AddDate(0, 0, 16).UTC()}, 60)
		if len(occs) == 0 { // outside the fixture window: keep the first occurrence so the event exists
			add(proto)
		}
		for _, o := range occs {
			it := proto
			if o.AllDay {
				applyWhen(&it, true, time.Time{}, time.Time{}, "", o.StartDate, o.EndDate)
			} else {
				applyWhen(&it, false, o.Start, o.End, tzid, "", "")
			}
			add(it)
		}
	}
	d.resort()
	rev := d.st.rev
	i, _ := d.find(eid)
	ev := d.details[d.items[i].ID]
	go s.ws.publish(rev)
	return http.StatusCreated, ev, nil
}

// target resolves {id} and the version check shared by the edit endpoints.
func (s *Server) target(r *http.Request) (int, *werr) {
	d := s.Data
	i, ok := d.find(r.PathValue("id"))
	if !ok {
		return 0, &werr{status: 404, code: api.CodeNotFound, msg: "no such event"}
	}
	n, has, e := parseIfMatch(r)
	if e != nil {
		return 0, e
	}
	if has && n != d.items[i].Version {
		cur := d.details[d.items[i].ID]
		return 0, &werr{status: 412, code: "precondition_failed", msg: "This event changed since you last loaded it.", current: &cur}
	}
	return i, nil
}

func (s *Server) wPatch(r *http.Request, b []byte) (int, any, *werr) {
	var in events.PatchIn
	if len(bytes.TrimSpace(b)) == 0 {
		return 0, nil, verr("", "a JSON body is required")
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return 0, nil, verr("", "invalid JSON: "+err.Error())
	}
	if in.Empty() {
		return 0, nil, verr("", "nothing to change")
	}
	scope, e := checkScope(r, "this")
	if e != nil {
		return 0, nil, e
	}
	d := s.Data
	d.st.mu.Lock()
	defer d.st.mu.Unlock()
	i, e := s.target(r)
	if e != nil {
		return 0, nil, e
	}
	base := d.items[i]
	if !base.CanEdit {
		return 0, nil, &werr{status: 403, code: api.CodeForbidden, msg: "you cannot edit this event"}
	}
	if in.Title != nil && len(*in.Title) > 1024 {
		return 0, nil, verr("title", "too long")
	}
	cal, _ := d.calendar(base.CalendarID)
	var (
		hasWhen             bool
		allDay              bool
		st, en              time.Time
		tzid, date, endDate string
	)
	if in.When != nil {
		hasWhen = true
		if allDay, st, en, tzid, date, endDate, e = d.whenFor(*in.When, &base, cal); e != nil {
			return 0, nil, e
		}
	}
	if in.Recurrence.Set && scope == provider.ScopeThis && base.Recurring {
		return 0, nil, verr("recurrence", "the repeat rule cannot change for a single occurrence; use scope following or all")
	}
	// which occurrences does the edit touch, and under which event id do they end up?
	idx := []int{i}
	newEID := base.EventID
	if base.Recurring {
		all := d.series(base.EventID)
		switch scope {
		case provider.ScopeAll:
			idx = all
		case provider.ScopeFollowing:
			idx = nil
			for _, j := range all {
				if !sortKey(d.items[j]).Before(sortKey(base)) {
					idx = append(idx, j)
				}
			}
			newEID = d.newEventID()
		case provider.ScopeThis:
			newEID = d.newEventID()
		}
	}
	d.st.rev++
	version := 1000 + d.st.rev
	var delta time.Duration // wall-clock shift for scope all/following
	if hasWhen && !allDay && base.Start != nil && len(idx) > 1 {
		loc, _ := tz.Load(base.TZID)
		oldW := wall(*base.Start, loc)
		nl, _ := tz.Load(tzid)
		delta = wall(st, nl).Sub(oldW)
	}
	var returned string
	for _, j := range idx {
		it := d.items[j]
		old := it
		det := d.details[it.ID]
		if in.Title != nil {
			it.Title = *in.Title
		}
		if in.Location.Set {
			it.Location = ""
			if in.Location.Val != nil {
				it.Location = *in.Location.Val
			}
		}
		if in.Description.Set {
			det.Description = ""
			if in.Description.Val != nil {
				det.Description = *in.Description.Val
			}
		}
		if in.Attendees.Set {
			det.Attendees = nil
			if in.Attendees.Val != nil {
				det.Attendees = append(det.Attendees, api.Attendee{Email: d.Accounts[0].Email, Name: "Alex", Response: "accepted", IsSelf: true, IsOrganizer: true})
				for _, a := range *in.Attendees.Val {
					det.Attendees = append(det.Attendees, api.Attendee{Email: a, Response: "needsAction"})
				}
			}
		}
		if in.Conference.Set {
			if in.Conference.Val != nil && *in.Conference.Val == "meet" {
				it.Join, it.CanAddConference = &api.Join{URL: "https://meet.google.com/upd-" + strings.ToLower(base.EventID[len(base.EventID)-5:]) + "-xyz", Kind: "meet"}, false
			} else {
				it.Join, it.CanAddConference = nil, !it.AllDay
			}
		}
		if hasWhen {
			switch {
			case allDay:
				applyWhen(&it, true, time.Time{}, time.Time{}, "", date, endDate)
			case len(idx) == 1 || j == i:
				applyWhen(&it, false, st, en, tzid, "", "")
			case old.Start != nil:
				loc, _ := tz.Load(tzid)
				w := wall(*old.Start, loc).Add(delta)
				ns := time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, loc).UTC()
				applyWhen(&it, false, ns, ns.Add(en.Sub(st)), tzid, "", "")
			}
		}
		it.Version = version
		it.EventID = newEID
		it.ID = occID(newEID, rkeyOf(it))
		if in.Recurrence.Set && in.Recurrence.Val != nil {
			det.Recurrence = &api.Recurrence{RRule: strings.TrimPrefix(in.Recurrence.Val.RRule, "RRULE:"), Summary: "Repeats"}
		}
		det.UpdatedAt = ptr(d.Now)
		delete(d.details, old.ID)
		det.AgendaItem = it
		d.items[j] = it
		d.details[it.ID] = det
		if j == i {
			returned = it.ID
		}
	}
	if in.ReminderMin.Set {
		if ev, ok := d.details[returned]; ok {
			ev.ReminderMin = in.ReminderMin.Val
			d.details[returned] = ev
		}
	}
	d.resort()
	rev := d.st.rev
	go s.ws.publish(rev)
	return http.StatusOK, d.details[returned], nil
}

func wall(t time.Time, loc *time.Location) time.Time {
	x := t.In(loc)
	return time.Date(x.Year(), x.Month(), x.Day(), x.Hour(), x.Minute(), x.Second(), 0, time.UTC)
}

func (s *Server) wDelete(r *http.Request, _ []byte) (int, any, *werr) {
	scope, e := checkScope(r, "this")
	if e != nil {
		return 0, nil, e
	}
	d := s.Data
	d.st.mu.Lock()
	defer d.st.mu.Unlock()
	i, e := s.target(r)
	if e != nil {
		return 0, nil, e
	}
	base := d.items[i]
	if !base.CanEdit {
		return 0, nil, &werr{status: 403, code: api.CodeForbidden, msg: "you cannot delete this event"}
	}
	drop := map[int]bool{i: true}
	if base.Recurring {
		for _, j := range d.series(base.EventID) {
			switch scope {
			case provider.ScopeAll:
				drop[j] = true
			case provider.ScopeFollowing:
				if !sortKey(d.items[j]).Before(sortKey(base)) {
					drop[j] = true
				}
			}
		}
	}
	var keep []api.AgendaItem
	for j, it := range d.items {
		if drop[j] {
			delete(d.details, it.ID)
			continue
		}
		keep = append(keep, it)
	}
	d.items = keep
	d.st.rev++
	go s.ws.publish(d.st.rev)
	return http.StatusNoContent, nil, nil
}

func (s *Server) wRSVP(r *http.Request, b []byte) (int, any, *werr) {
	var in struct{ Response, Note, Scope string }
	if err := json.Unmarshal(b, &in); err != nil {
		return 0, nil, verr("", "invalid JSON: "+err.Error())
	}
	if in.Response != "accepted" && in.Response != "declined" && in.Response != "tentative" {
		return 0, nil, verr("response", "response must be accepted, declined or tentative")
	}
	scope := provider.Scope(in.Scope)
	if scope == "" {
		var e *werr
		if scope, e = checkScope(r, "this"); e != nil {
			return 0, nil, e
		}
	}
	if scope == provider.ScopeFollowing {
		return 0, nil, verr("scope", "a reply can apply to this occurrence or the whole series")
	}
	d := s.Data
	d.st.mu.Lock()
	defer d.st.mu.Unlock()
	i, e := s.target(r)
	if e != nil {
		return 0, nil, e
	}
	base := d.items[i]
	if !base.CanRSVP {
		return 0, nil, &werr{status: 403, code: api.CodeForbidden, msg: "you cannot reply to this event"}
	}
	d.st.rev++
	idx := []int{i}
	if scope == provider.ScopeAll {
		idx = d.series(base.EventID)
	}
	for _, j := range idx {
		it := d.items[j]
		it.MyResponse, it.Version = ptr(in.Response), 1000+d.st.rev
		d.put(j, it)
		ev := d.details[it.ID]
		for k := range ev.Attendees {
			if ev.Attendees[k].IsSelf {
				ev.Attendees[k].Response = in.Response
			}
		}
		d.details[it.ID] = ev
	}
	go s.ws.publish(d.st.rev)
	return http.StatusOK, d.details[d.items[i].ID], nil
}

func (s *Server) wMeet(r *http.Request, _ []byte) (int, any, *werr) {
	d := s.Data
	d.st.mu.Lock()
	defer d.st.mu.Unlock()
	i, e := s.target(r)
	if e != nil {
		return 0, nil, e
	}
	base := d.items[i]
	if !base.CanAddConference || base.Join != nil {
		return 0, nil, &werr{status: 403, code: api.CodeForbidden, msg: "a video call cannot be added to this event"}
	}
	d.st.rev++
	for _, j := range d.series(base.EventID) {
		it := d.items[j]
		it.Join, it.CanAddConference, it.Version = &api.Join{URL: "https://meet.google.com/fix-" + strings.ToLower(base.EventID[len(base.EventID)-5:]) + "-new", Kind: "meet"}, false, 1000+d.st.rev
		d.put(j, it)
	}
	go s.ws.publish(d.st.rev)
	return http.StatusOK, d.details[d.items[i].ID], nil
}

func (s *Server) wMove(r *http.Request, b []byte) (int, any, *werr) {
	var in struct {
		CalendarID string `json:"calendarId"`
	}
	if err := json.Unmarshal(b, &in); err != nil || in.CalendarID == "" {
		return 0, nil, verr("calendarId", "calendarId is required")
	}
	d := s.Data
	d.st.mu.Lock()
	defer d.st.mu.Unlock()
	cal, ok := d.calendar(in.CalendarID)
	if !ok {
		return 0, nil, verr("calendarId", "no such calendar")
	}
	if !writableCal(cal) {
		return 0, nil, &werr{status: 403, code: api.CodeForbidden, msg: "this calendar is read-only", field: "calendarId"}
	}
	i, e := s.target(r)
	if e != nil {
		return 0, nil, e
	}
	base := d.items[i]
	if !base.CanEdit {
		return 0, nil, &werr{status: 403, code: api.CodeForbidden, msg: "you cannot move this event"}
	}
	crossAccount := cal.AccountID != base.AccountID
	if crossAccount && base.Recurring {
		return 0, nil, verr("calendarId", "a repeating event cannot be moved to another account")
	}
	d.st.rev++
	newEID := base.EventID
	if crossAccount {
		newEID = d.newEventID()
	}
	for _, j := range d.series(base.EventID) {
		it := d.items[j]
		old := it.ID
		it.CalendarID, it.AccountID, it.Color, it.Version, it.EventID = cal.ID, cal.AccountID, cal.Color, 1000+d.st.rev, newEID
		it.ID = occID(newEID, rkeyOf(it))
		ev := d.details[old]
		delete(d.details, old)
		ev.AgendaItem, ev.CalendarName = it, cal.Name
		d.items[j] = it
		d.details[it.ID] = ev
	}
	d.resort()
	go s.ws.publish(d.st.rev)
	k, _ := d.find(newEID)
	return http.StatusOK, d.details[d.items[k].ID], nil
}

func (s *Server) wReminder(r *http.Request, b []byte) (int, any, *werr) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return 0, nil, verr("", "invalid JSON: "+err.Error())
	}
	v, present := raw["minutesBefore"]
	if !present {
		return 0, nil, verr("minutesBefore", "minutesBefore is required (null clears the override)")
	}
	var minutes *int
	if string(v) != "null" {
		var n int
		if err := json.Unmarshal(v, &n); err != nil || n < 0 || n > 40320 {
			return 0, nil, verr("minutesBefore", "must be 0..40320 or null")
		}
		minutes = &n
	}
	d := s.Data
	d.st.mu.Lock()
	defer d.st.mu.Unlock()
	i, ok := d.find(r.PathValue("id"))
	if !ok {
		return 0, nil, &werr{status: 404, code: api.CodeNotFound, msg: "no such event"}
	}
	d.st.rev++
	it := d.items[i]
	it.Version = 1000 + d.st.rev
	d.put(i, it)
	ev := d.details[it.ID]
	ev.ReminderMin = minutes
	d.details[it.ID] = ev
	go s.ws.publish(d.st.rev)
	return http.StatusOK, ev, nil
}

var _ = domain.Accepted
