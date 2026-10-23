// Package gtest is an in-memory Google Calendar v3 speaking the REST subset the provider uses,
// including writes: insert, get, patch, delete, move, instances, recurring exceptions, etag
// preconditions and Meet creation. It exists for tests of the provider, the write service and
// the HTTP API; it is not used by the hub binary.
package gtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teambition/rrule-go"
)

type Event struct {
	Raw     map[string]any
	Seq     int
	Deleted bool
}

type Cal struct {
	ID, Summary, Role string
	Color, TZ         string // calendarList backgroundColor and timeZone ("" = "#112233" / Europe/London)
	Hidden            bool   // calendarList hidden
	Unselected        bool   // calendarList selected:false
	Events            map[string]*Event
}

// Req is one request the fake received (writes only are interesting to tests).
type Req struct {
	Method, Path string
	Query        map[string][]string
	Body         map[string]any
	IfMatch      string
}

type Fake struct {
	*httptest.Server
	Self string // the signed-in user's email

	mu       sync.Mutex
	seq      int
	cals     []*Cal
	revoked  map[string]bool
	Reqs     []Req
	PageSize int

	// MeetDelay: number of GETs of an event after creation that still show no link.
	MeetDelay int
	meetGets  map[string]int
	meetN     int

	fail []failRule

	tokenFloor int // list calls with a syncToken older than this answer 410
	mux        *http.ServeMux
}

type failRule struct {
	method, sub string
	code        int
	n           int
	retryAfter  string
}

// NewStandalone builds a fake with no listener; serve Handler() yourself (cmd/fakegoogle).
func NewStandalone(self string) *Fake {
	f := &Fake{Self: self, revoked: map[string]bool{}, PageSize: 100, meetGets: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", f.token)
	mux.HandleFunc("/users/me/calendarList", f.calendarList)
	mux.HandleFunc("/calendars/", f.route)
	f.mux = mux
	return f
}

func New(t testing.TB) *Fake {
	f := NewStandalone("me@x.test")
	f.Server = httptest.NewServer(f.mux)
	t.Cleanup(f.Close)
	return f
}

// Handler is the Google Calendar v3 API (plus /token) of this one account.
func (f *Fake) Handler() http.Handler { return f.mux }

// StartSeqAt sets the change counter that sync tokens ("st-N") are made from. The rich profile
// starts it from the clock so that tokens of an earlier run or reset are always older.
func (f *Fake) StartSeqAt(n int) { f.mu.Lock(); f.seq = n; f.mu.Unlock() }

// InvalidateSyncTokens makes every syncToken issued so far answer 410 (a full re-list is needed).
func (f *Fake) InvalidateSyncTokens() {
	f.mu.Lock()
	f.seq++
	f.tokenFloor = f.seq
	f.mu.Unlock()
}

// Dump returns copies of the live events of a calendar (nil if the calendar is unknown).
func (f *Fake) Dump(cal string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var c *Cal
	for _, x := range f.cals {
		if x.ID == cal {
			c = x
		}
	}
	if c == nil {
		return nil
	}
	out := []map[string]any{}
	for _, e := range c.Events {
		if !e.Deleted {
			out = append(out, clone(e.Raw))
		}
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["id"]) < fmt.Sprint(out[j]["id"]) })
	return out
}

// CalIDs lists the calendar ids in creation order.
func (f *Fake) CalIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.cals {
		out = append(out, c.ID)
	}
	return out
}

// Delete removes an event as an outside change.
func (f *Fake) Delete(cal, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.cal(cal).Events[id]; e != nil {
		f.seq++
		e.Deleted, e.Seq = true, f.seq
		e.Raw["etag"] = f.etag()
	}
}

// ClearFailures drops pending failure rules.
func (f *Fake) ClearFailures() { f.mu.Lock(); f.fail = nil; f.mu.Unlock() }

func (f *Fake) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	rt := r.Form.Get("refresh_token")
	if f.revoked[rt] || rt == "" {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-" + rt, "token_type": "Bearer", "expires_in": 3600})
}

func (f *Fake) authorised(w http.ResponseWriter, r *http.Request) bool {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer at-")
	if tok == r.Header.Get("Authorization") || f.revoked[tok] {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"code":401}}`))
		return false
	}
	return true
}

func (f *Fake) calendarList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.authorised(w, r) {
		return
	}
	items := []any{}
	for i, c := range f.cals {
		color, tz := c.Color, c.TZ
		if color == "" {
			color = "#112233"
		}
		if tz == "" {
			tz = "Europe/London"
		}
		it := map[string]any{"id": c.ID, "summary": c.Summary, "accessRole": c.Role, "primary": i == 0,
			"backgroundColor": color, "timeZone": tz}
		if c.Hidden {
			it["hidden"] = true
		}
		if c.Unselected {
			it["selected"] = false
		}
		items = append(items, it)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}

// ---- test controls ----

func (f *Fake) AddCal(id, summary, role string) *Cal {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := &Cal{ID: id, Summary: summary, Role: role, Events: map[string]*Event{}}
	f.cals = append(f.cals, c)
	return c
}

// AddCalWith adds a calendar with list metadata (colour, time zone, hidden, selected:false).
func (f *Fake) AddCalWith(id, summary, role, color, tz string, hidden, unselected bool) *Cal {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := &Cal{ID: id, Summary: summary, Role: role, Color: color, TZ: tz, Hidden: hidden, Unselected: unselected, Events: map[string]*Event{}}
	f.cals = append(f.cals, c)
	return c
}

// RemoveCal drops a calendar from the list (as when the user unsubscribes in Google). Reports whether it existed.
func (f *Fake) RemoveCal(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.cals {
		if c.ID == id {
			f.cals = append(f.cals[:i:i], f.cals[i+1:]...)
			return true
		}
	}
	return false
}

// CalInfo describes a calendar for the admin summary.
type CalInfo struct {
	ID, Summary, Role string
	Hidden            bool
	Events            int // live events stored (series count once)
}

// Cals lists the calendars in creation order.
func (f *Fake) Cals() []CalInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []CalInfo
	for _, c := range f.cals {
		n := 0
		for _, e := range c.Events {
			if !e.Deleted {
				n++
			}
		}
		out = append(out, CalInfo{c.ID, c.Summary, c.Role, c.Hidden, n})
	}
	return out
}

// HasCal reports whether the calendar exists.
func (f *Fake) HasCal(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cals {
		if c.ID == id {
			return true
		}
	}
	return false
}

// Revoked reports whether the refresh token is revoked.
func (f *Fake) Revoked(rt string) bool { f.mu.Lock(); defer f.mu.Unlock(); return f.revoked[rt] }

// PendingFailures counts the failure rules that still have requests left.
func (f *Fake) PendingFailures() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.fail {
		if r.n > 0 {
			n++
		}
	}
	return n
}

func (f *Fake) Revoke(rt string) { f.mu.Lock(); f.revoked[rt] = true; f.mu.Unlock() }

// FailNext makes the next n requests whose method matches (""=any) and path contains sub answer code.
func (f *Fake) FailNext(method, sub string, code, n int, retryAfter string) {
	f.mu.Lock()
	f.fail = append(f.fail, failRule{method, sub, code, n, retryAfter})
	f.mu.Unlock()
}

// Put stores or replaces an event as an outside change (new etag).
func (f *Fake) Put(cal, id string, raw map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	raw["id"] = id
	raw["etag"] = f.etag()
	if _, ok := raw["status"]; !ok {
		raw["status"] = "confirmed"
	}
	raw["iCalUID"] = id + "@google.com"
	raw["updated"] = time.Now().UTC().Format(time.RFC3339)
	f.cal(cal).Events[id] = &Event{Raw: raw, Seq: f.seq}
}

// Edit applies fn to an event as an outside change (new etag).
func (f *Fake) Edit(cal, id string, fn func(raw map[string]any)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.cal(cal).Events[id]
	fn(e.Raw)
	f.seq++
	e.Seq = f.seq
	e.Raw["etag"] = f.etag()
}

// Get returns a copy of the stored raw event (nil if absent or deleted).
func (f *Fake) Get(cal, id string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.cal(cal).Events[id]
	if e == nil || e.Deleted {
		return nil
	}
	return clone(e.Raw)
}

// IDs lists live (non-deleted) event ids of a calendar, sorted.
func (f *Fake) IDs(cal string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for id, e := range f.cal(cal).Events {
		if !e.Deleted {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func (f *Fake) ClearReqs() { f.mu.Lock(); f.Reqs = nil; f.mu.Unlock() }

// Writes returns the recorded non-GET requests.
func (f *Fake) Writes() []Req {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Req
	for _, r := range f.Reqs {
		if r.Method != "GET" {
			out = append(out, r)
		}
	}
	return out
}

func (f *Fake) etag() string { return fmt.Sprintf(`"e%d"`, f.seq) }

func (f *Fake) cal(id string) *Cal {
	for _, c := range f.cals {
		if c.ID == id {
			return c
		}
	}
	panic("gtest: no calendar " + id)
}

func clone(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// Timed / AllDay build event bodies.
func Timed(summary, start, end string) map[string]any {
	return map[string]any{"summary": summary,
		"start": map[string]any{"dateTime": start, "timeZone": "Europe/London"},
		"end":   map[string]any{"dateTime": end, "timeZone": "Europe/London"}}
}

func AllDay(summary, d1, d2 string) map[string]any {
	return map[string]any{"summary": summary, "start": map[string]any{"date": d1}, "end": map[string]any{"date": d2}}
}

// ---- HTTP ----

func (f *Fake) route(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.authorised(w, r) {
		return
	}
	var body map[string]any
	if r.Body != nil {
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &body)
		}
	}
	f.Reqs = append(f.Reqs, Req{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Body: body, IfMatch: r.Header.Get("If-Match")})
	for i := range f.fail {
		fr := &f.fail[i]
		if fr.n > 0 && (fr.method == "" || fr.method == r.Method) && strings.Contains(r.URL.Path, fr.sub) {
			fr.n--
			if fr.retryAfter != "" {
				w.Header().Set("Retry-After", fr.retryAfter)
			}
			w.WriteHeader(fr.code)
			_, _ = w.Write([]byte(`{"error":{"code":` + strconv.Itoa(fr.code) + `}}`))
			return
		}
	}
	rest := strings.TrimPrefix(r.URL.EscapedPath(), "/calendars/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[1] != "events" {
		w.WriteHeader(404)
		return
	}
	calID := unesc(parts[0])
	var cal *Cal
	for _, c := range f.cals {
		if c.ID == calID {
			cal = c
		}
	}
	if cal == nil {
		w.WriteHeader(404)
		return
	}
	switch {
	case len(parts) == 2 && r.Method == "GET":
		f.list(w, r, cal)
	case len(parts) == 2 && r.Method == "POST":
		f.insert(w, r, cal, body)
	case len(parts) == 3:
		id := unesc(parts[2])
		switch r.Method {
		case "GET":
			f.get(w, cal, id)
		case "PATCH":
			f.patch(w, r, cal, id, body)
		case "DELETE":
			f.del(w, r, cal, id)
		default:
			w.WriteHeader(405)
		}
	case len(parts) == 4 && parts[3] == "instances" && r.Method == "GET":
		f.instances(w, r, cal, unesc(parts[2]))
	case len(parts) == 4 && parts[3] == "move" && r.Method == "POST":
		f.move(w, r, cal, unesc(parts[2]))
	default:
		w.WriteHeader(404)
	}
}

func unesc(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *Fake) list(w http.ResponseWriter, r *http.Request, cal *Cal) {
	q := r.URL.Query()
	if q.Get("singleEvents") != "false" {
		w.WriteHeader(400)
		return
	}
	st := q.Get("syncToken")
	since := -1
	if st != "" {
		if !strings.HasPrefix(st, "st-") {
			w.WriteHeader(410)
			return
		}
		since, _ = strconv.Atoi(strings.TrimPrefix(st, "st-"))
		if since < f.tokenFloor {
			w.WriteHeader(410)
			return
		}
	}
	var ids []string
	for id, e := range cal.Events {
		if st == "" && (!e.Deleted || e.Raw["recurringEventId"] != nil) || st != "" && e.Seq > since {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	off, _ := strconv.Atoi(q.Get("pageToken"))
	end := off + f.PageSize
	resp := map[string]any{"timeZone": "Europe/London"}
	if end < len(ids) {
		resp["nextPageToken"] = strconv.Itoa(end)
	} else {
		end = len(ids)
		resp["nextSyncToken"] = fmt.Sprintf("st-%d", f.seq)
	}
	items := []any{}
	for _, id := range ids[off:end] {
		items = append(items, f.view(cal.Events[id]))
	}
	resp["items"] = items
	writeJSON(w, 200, resp)
}

// view renders an event as the API returns it (deleted = cancelled stub).
func (f *Fake) view(e *Event) map[string]any {
	out := clone(e.Raw)
	if e.Deleted {
		out["status"] = "cancelled"
	}
	return out
}

func (f *Fake) get(w http.ResponseWriter, cal *Cal, id string) {
	e := cal.Events[id]
	if e == nil {
		e = f.virtualInstance(cal, id)
	}
	if e == nil {
		w.WriteHeader(404)
		return
	}
	v := f.view(e)
	if f.MeetDelay > 0 {
		if _, ok := v["conferenceData"]; ok {
			k := cal.ID + "/" + id
			f.meetGets[k]++
			if f.meetGets[k] <= f.MeetDelay {
				delete(v, "hangoutLink")
				v["conferenceData"] = map[string]any{"createRequest": map[string]any{"status": map[string]any{"statusCode": "pending"}}}
			}
		}
	}
	writeJSON(w, 200, v)
}

func (f *Fake) insert(w http.ResponseWriter, r *http.Request, cal *Cal, body map[string]any) {
	id, _ := body["id"].(string)
	if id == "" {
		f.seq++
		id = fmt.Sprintf("gen%d", f.seq)
	}
	if _, ok := cal.Events[id]; ok {
		writeJSON(w, 409, map[string]any{"error": map[string]any{"code": 409, "message": "The requested identifier already exists."}})
		return
	}
	raw := clone(body)
	raw["id"] = id
	raw["status"] = "confirmed"
	raw["iCalUID"] = id + "@google.com"
	raw["organizer"] = map[string]any{"email": f.Self, "self": true}
	raw["updated"] = time.Now().UTC().Format(time.RFC3339)
	raw["htmlLink"] = "https://calendar.google.com/event?eid=" + id
	if at, ok := raw["attendees"].([]any); ok && len(at) > 0 {
		for _, a := range at {
			m := a.(map[string]any)
			if _, ok := m["responseStatus"]; !ok {
				m["responseStatus"] = "needsAction"
			}
		}
		raw["attendees"] = append(at, map[string]any{"email": f.Self, "self": true, "organizer": true, "responseStatus": "accepted"})
	}
	f.applyConference(r, raw)
	f.seq++
	raw["etag"] = f.etag()
	cal.Events[id] = &Event{Raw: raw, Seq: f.seq}
	writeJSON(w, 200, f.view(cal.Events[id]))
}

func (f *Fake) applyConference(r *http.Request, raw map[string]any) {
	cd, ok := raw["conferenceData"].(map[string]any)
	if !ok {
		return
	}
	if _, ok := cd["createRequest"]; !ok {
		return
	}
	if r.URL.Query().Get("conferenceDataVersion") != "1" {
		delete(raw, "conferenceData")
		return
	}
	f.meetN++
	link := fmt.Sprintf("https://meet.google.com/abc-def%d-ghi", f.meetN)
	raw["hangoutLink"] = link
	raw["conferenceData"] = map[string]any{
		"entryPoints":   []any{map[string]any{"entryPointType": "video", "uri": link}},
		"createRequest": map[string]any{"status": map[string]any{"statusCode": "success"}},
	}
}

func (f *Fake) checkMatch(w http.ResponseWriter, r *http.Request, e *Event) bool {
	if im := r.Header.Get("If-Match"); im != "" && im != e.Raw["etag"] {
		writeJSON(w, 412, map[string]any{"error": map[string]any{"code": 412, "message": "Precondition Failed"}})
		return false
	}
	return true
}

func (f *Fake) patch(w http.ResponseWriter, r *http.Request, cal *Cal, id string, body map[string]any) {
	e := cal.Events[id]
	if e == nil {
		e = f.materialiseInstance(cal, id)
	}
	if e == nil || e.Deleted {
		w.WriteHeader(404)
		return
	}
	if !f.checkMatch(w, r, e) {
		return
	}
	for k, v := range body {
		if v == nil {
			delete(e.Raw, k)
		} else {
			e.Raw[k] = v
		}
	}
	f.applyConference(r, e.Raw)
	f.seq++
	e.Seq = f.seq
	e.Raw["etag"] = f.etag()
	e.Raw["updated"] = time.Now().UTC().Format(time.RFC3339)
	writeJSON(w, 200, f.view(e))
}

func (f *Fake) del(w http.ResponseWriter, r *http.Request, cal *Cal, id string) {
	e := cal.Events[id]
	if e == nil {
		e = f.materialiseInstance(cal, id)
	}
	if e == nil || e.Deleted {
		w.WriteHeader(410)
		return
	}
	if !f.checkMatch(w, r, e) {
		return
	}
	f.seq++
	e.Deleted, e.Seq = true, f.seq
	e.Raw["etag"] = f.etag()
	// a deleted series takes its exceptions with it
	if _, ok := e.Raw["recurrence"]; ok {
		for _, x := range cal.Events {
			if x.Raw["recurringEventId"] == id {
				x.Deleted, x.Seq = true, f.seq
			}
		}
	}
	w.WriteHeader(204)
}

func (f *Fake) move(w http.ResponseWriter, r *http.Request, cal *Cal, id string) {
	e := cal.Events[id]
	if e == nil || e.Deleted {
		w.WriteHeader(404)
		return
	}
	if !f.checkMatch(w, r, e) {
		return
	}
	var dst *Cal
	for _, c := range f.cals {
		if c.ID == r.URL.Query().Get("destination") {
			dst = c
		}
	}
	if dst == nil {
		w.WriteHeader(404)
		return
	}
	f.seq++
	move := func(x *Event, xid string) {
		dst.Events[xid] = &Event{Raw: x.Raw, Seq: f.seq}
		x.Raw["etag"] = f.etag()
		x.Deleted, x.Seq = true, f.seq
		x.Raw = clone(x.Raw) // the source keeps a cancelled stub
	}
	for xid, x := range cal.Events {
		if x.Raw["recurringEventId"] == id && !x.Deleted {
			move(x, xid)
		}
	}
	move(e, id)
	writeJSON(w, 200, f.view(dst.Events[id]))
}

// ---- recurrence ----

func basic(t time.Time, allDay bool) string {
	if allDay {
		return t.Format("20060102")
	}
	return t.UTC().Format("20060102T150405Z")
}

type inst struct {
	id       string
	start    time.Time // original start
	raw      map[string]any
	allDay   bool
	original map[string]any
}

// expand returns the generated instances of a master (before exceptions are applied).
func expand(master map[string]any, from, to time.Time) []inst {
	st := master["start"].(map[string]any)
	en := master["end"].(map[string]any)
	allDay := st["date"] != nil
	var loc *time.Location
	var dtstart, dtend time.Time
	if allDay {
		loc = time.UTC
		dtstart, _ = time.ParseInLocation("2006-01-02", st["date"].(string), time.UTC)
		dtend, _ = time.ParseInLocation("2006-01-02", en["date"].(string), time.UTC)
	} else {
		tzid, _ := st["timeZone"].(string)
		loc, _ = time.LoadLocation(tzid)
		if loc == nil {
			loc = time.UTC
		}
		dtstart, _ = time.Parse(time.RFC3339, st["dateTime"].(string))
		dtend, _ = time.Parse(time.RFC3339, en["dateTime"].(string))
		dtstart = dtstart.In(loc)
	}
	dur := dtend.Sub(dtstart)
	set := rrule.Set{}
	set.DTStart(dtstart)
	var rdates []time.Time
	for _, l := range toStrings(master["recurrence"]) {
		switch {
		case strings.HasPrefix(l, "RRULE:"):
			opt, err := rrule.StrToROptionInLocation(strings.TrimPrefix(l, "RRULE:"), loc)
			if err != nil {
				continue
			}
			opt.Dtstart = dtstart
			rr, err := rrule.NewRRule(*opt)
			if err != nil {
				continue
			}
			set.RRule(rr)
		case strings.HasPrefix(l, "EXDATE"), strings.HasPrefix(l, "RDATE"):
			head, vals, _ := strings.Cut(l, ":")
			l2 := loc
			if _, tz, ok := strings.Cut(head, "TZID="); ok {
				l2, _ = time.LoadLocation(strings.Split(tz, ";")[0])
			}
			for _, v := range strings.Split(vals, ",") {
				var t time.Time
				var err error
				switch {
				case len(v) == 8:
					t, err = time.ParseInLocation("20060102", v, time.UTC)
				case strings.HasSuffix(v, "Z"):
					t, err = time.Parse("20060102T150405Z", v)
				default:
					t, err = time.ParseInLocation("20060102T150405", v, l2)
				}
				if err != nil {
					continue
				}
				if strings.HasPrefix(l, "EXDATE") {
					set.ExDate(t)
				} else {
					rdates = append(rdates, t)
				}
			}
		}
	}
	for _, t := range rdates {
		set.RDate(t)
	}
	var out []inst
	for _, t := range set.Between(from.Add(-dur-time.Second), to.Add(time.Second), true) {
		if !t.Before(to) || !t.Add(dur).After(from) {
			continue
		}
		raw := clone(master)
		delete(raw, "recurrence")
		id := fmt.Sprintf("%v_%s", master["id"], basic(t, allDay))
		raw["id"] = id
		raw["recurringEventId"] = master["id"]
		var orig, ns, ne map[string]any
		if allDay {
			orig = map[string]any{"date": t.Format(time.DateOnly)}
			ns = map[string]any{"date": t.Format(time.DateOnly)}
			ne = map[string]any{"date": t.Add(dur).Format(time.DateOnly)}
		} else {
			tzid := st["timeZone"]
			orig = map[string]any{"dateTime": t.In(loc).Format(time.RFC3339), "timeZone": tzid}
			ns = map[string]any{"dateTime": t.In(loc).Format(time.RFC3339), "timeZone": tzid}
			ne = map[string]any{"dateTime": t.Add(dur).In(loc).Format(time.RFC3339), "timeZone": tzid}
		}
		raw["originalStartTime"], raw["start"], raw["end"] = orig, ns, ne
		out = append(out, inst{id: id, start: t, raw: raw, allDay: allDay, original: orig})
	}
	return out
}

func toStrings(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, s := range x {
			if str, ok := s.(string); ok {
				out = append(out, str)
			}
		}
	case []string:
		out = x
	}
	return out
}

func split(id string) (master string, ok bool) {
	i := strings.LastIndex(id, "_")
	if i <= 0 {
		return "", false
	}
	return id[:i], true
}

// virtualInstance returns an unsaved view of a generated instance.
func (f *Fake) virtualInstance(cal *Cal, id string) *Event {
	mid, ok := split(id)
	if !ok {
		return nil
	}
	m := cal.Events[mid]
	if m == nil || m.Deleted || m.Raw["recurrence"] == nil {
		return nil
	}
	for _, in := range expand(m.Raw, time.Unix(0, 0), time.Now().AddDate(10, 0, 0)) {
		if in.id == id {
			in.raw["etag"] = m.Raw["etag"]
			return &Event{Raw: in.raw, Seq: m.Seq}
		}
	}
	return nil
}

// materialiseInstance turns a generated instance into a stored exception (Google does this on first edit).
func (f *Fake) materialiseInstance(cal *Cal, id string) *Event {
	v := f.virtualInstance(cal, id)
	if v == nil {
		return nil
	}
	f.seq++
	v.Seq = f.seq // etag stays the master's, so the caller's If-Match still matches
	cal.Events[id] = v
	return v
}

func (f *Fake) instances(w http.ResponseWriter, r *http.Request, cal *Cal, mid string) {
	m := cal.Events[mid]
	if m == nil || m.Deleted || m.Raw["recurrence"] == nil {
		w.WriteHeader(404)
		return
	}
	q := r.URL.Query()
	from, to := time.Unix(0, 0), time.Now().AddDate(5, 0, 0)
	if v := q.Get("timeMin"); v != "" {
		from, _ = time.Parse(time.RFC3339, v)
	}
	if v := q.Get("timeMax"); v != "" {
		to, _ = time.Parse(time.RFC3339, v)
	}
	items := []any{}
	for _, in := range expand(m.Raw, from, to) {
		raw := in.raw
		if x := cal.Events[in.id]; x != nil {
			if x.Deleted && q.Get("showDeleted") != "true" {
				continue
			}
			raw = f.view(x)
		} else {
			raw["etag"] = m.Raw["etag"]
		}
		items = append(items, raw)
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
