package google

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
	"calendar/server/internal/recur"
	"calendar/server/internal/telemetry"
	"calendar/server/internal/tz"
)

// errIDExists is Google's 409 on insert: the client-chosen id is already taken.
var errIDExists = errors.New("google: event id already exists")

// Pause between polls while Google finishes creating a Meet link. Replaced in tests.
var meetPoll = 400 * time.Millisecond

// send performs one write request. ifMatch (a Google etag, quotes included) becomes If-Match.
func (p *Provider) send(ctx context.Context, accountID, method, path string, q url.Values, body any, ifMatch string, out any) error {
	ctx = telemetry.WithProvider(ctx, "google")
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		cl, err := p.client(ctx, accountID)
		if err != nil {
			return err
		}
		u := p.cfg.BaseURL + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			return err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		began := time.Now()
		resp, err := cl.Do(req)
		if err != nil {
			observe(ctx, operation(ctx, method, path), began, nil, nil, err)
			return mapTransportErr(err)
		}
		data, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		observe(ctx, operation(ctx, method, path), began, resp, data, rerr)
		if rerr != nil {
			return rerr
		}
		switch {
		case resp.StatusCode == 401 && attempt == 0:
			p.drop(accountID)
			continue
		case resp.StatusCode/100 == 2:
			if out != nil && len(data) > 0 {
				return json.Unmarshal(data, out)
			}
			return nil
		case resp.StatusCode == 404 || resp.StatusCode == 410:
			return provider.ErrNotFound
		case resp.StatusCode == 409:
			return errIDExists
		}
		return classify(resp, data)
	}
}

func calPath(cal domain.Calendar) string {
	return "/calendars/" + url.PathEscape(cal.RemoteID) + "/events"
}
func evPath(cal domain.Calendar, id string) string {
	return calPath(cal) + "/" + url.PathEscape(id)
}

func notify(silent bool) string {
	if silent {
		return "none"
	}
	return "all"
}

// newID makes an id valid for Google (base32hex, lowercase, 5-1024 chars).
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return strings.ToLower(base32.HexEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}

// ---- body builders ----

func whenFields(w domain.When) (start, end map[string]any) {
	if w.AllDay {
		return map[string]any{"date": w.StartDate.String()}, map[string]any{"date": w.EndDate.String()}
	}
	loc, _ := tz.Load(w.TZID)
	tzid := w.TZID
	if tzid == "" {
		tzid = "UTC"
	}
	return map[string]any{"dateTime": w.Start.In(loc).Format(time.RFC3339), "timeZone": tzid},
		map[string]any{"dateTime": w.End.In(loc).Format(time.RFC3339), "timeZone": tzid}
}

func recurrenceLines(r *domain.Recurrence) []string {
	if r == nil {
		return nil
	}
	var out []string
	if r.RRule != "" {
		out = append(out, "RRULE:"+r.RRule)
	}
	out = append(out, r.RDates...)
	out = append(out, r.EXDates...)
	return out
}

func attendeeList(emails []string) []map[string]any {
	out := make([]map[string]any, 0, len(emails))
	for _, e := range emails {
		out = append(out, map[string]any{"email": e})
	}
	return out
}

func meetRequest() map[string]any {
	return map[string]any{"createRequest": map[string]any{
		"requestId":             newID(),
		"conferenceSolutionKey": map[string]any{"type": "hangoutsMeet"},
	}}
}

// ---- results ----

func (p *Provider) mapOne(g gEvent, cal domain.Calendar) domain.Event { return mapEvent(g, cal) }

// ---- create ----

func (p *Provider) Create(ctx context.Context, acct domain.Account, cal domain.Calendar, in provider.EventInput) (provider.Result, error) {
	id := in.ID
	if id == "" {
		id = newID()
	}
	st, en := whenFields(in.When)
	body := map[string]any{"id": id, "summary": in.Title, "start": st, "end": en}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.Location != "" {
		body["location"] = in.Location
	}
	if len(in.Attendees) > 0 {
		body["attendees"] = attendeeList(in.Attendees)
	}
	if lines := recurrenceLines(in.Recurrence); len(lines) > 0 {
		body["recurrence"] = lines
	}
	q := url.Values{"sendUpdates": {notify(in.Silent)}}
	if in.Conference {
		body["conferenceData"] = meetRequest()
		q.Set("conferenceDataVersion", "1")
	}
	var g gEvent
	err := p.send(ctx, acct.ID, http.MethodPost, calPath(cal), q, body, "", &g)
	if errors.Is(err, errIDExists) {
		// A retried create (same client id): adopt what is already there.
		if err = p.send(ctx, acct.ID, http.MethodGet, evPath(cal, id), nil, nil, "", &g); err != nil {
			return provider.Result{}, err
		}
	} else if err != nil {
		return provider.Result{}, err
	}
	if in.Conference {
		g = p.awaitMeet(ctx, acct.ID, cal, g)
	}
	return provider.Result{Events: []domain.Event{p.mapOne(g, cal)}}, nil
}

// awaitMeet polls briefly for the conference link; the next sync fills it in if Google is slow.
func (p *Provider) awaitMeet(ctx context.Context, acctID string, cal domain.Calendar, g gEvent) gEvent {
	for i := 0; i < 4 && !hasConference(g); i++ {
		t := time.NewTimer(meetPoll)
		select {
		case <-ctx.Done():
			t.Stop()
			return g
		case <-t.C:
		}
		var n gEvent
		if err := p.send(ctx, acctID, http.MethodGet, evPath(cal, g.ID), nil, nil, "", &n); err != nil {
			return g
		}
		g = n
	}
	return g
}

func hasConference(g gEvent) bool {
	if g.HangoutLink != "" {
		return true
	}
	if g.ConferenceData != nil {
		for _, ep := range g.ConferenceData.EntryPoints {
			if ep.URI != "" {
				return true
			}
		}
	}
	return false
}

// ---- target resolution ----

type target struct {
	id      string // remote id to act on
	etag    string // If-Match for the mirrored row ("" for instances we never mirrored)
	cal     domain.Calendar
	master  *domain.Event // series master when known
	instKey *int64        // set for an instance addressed by original start
}

func keyOf(ev domain.Event) int64 {
	if ev.When.AllDay {
		d := ev.When.StartDate
		return time.Date(d.Y, time.Month(d.M), d.D, 0, 0, 0, 0, time.UTC).Unix()
	}
	return ev.When.Start.Unix()
}

func seriesMaster(r provider.Ref) domain.Event {
	if r.Master != nil {
		return *r.Master
	}
	return r.Event
}

// resolve picks the remote event a scope refers to. "following" is handled by callers.
func (p *Provider) resolve(ctx context.Context, ref provider.Ref, scope provider.Scope) (target, error) {
	ev := ref.Event
	switch {
	case ev.Kind == domain.Single:
		return target{id: ev.RemoteID, etag: ev.ETag, cal: ref.Cal}, nil
	case scope == provider.ScopeAll:
		m := seriesMaster(ref)
		if m.Kind != domain.Master {
			return target{}, fmt.Errorf("%w: series master not mirrored", provider.ErrNotSupported)
		}
		return target{id: m.RemoteID, etag: m.ETag, cal: ref.Cal, master: &m}, nil
	case scope == provider.ScopeThis && ev.Kind == domain.Exception:
		return target{id: ev.RemoteID, etag: ev.ETag, cal: ref.Cal}, nil
	case scope == provider.ScopeThis && ev.Kind == domain.Master:
		k := keyOf(ev)
		if ref.OccKey != nil {
			k = *ref.OccKey
		}
		id, err := p.instanceID(ctx, ref.Acct.ID, ref.Cal, ev, k)
		if err != nil {
			return target{}, err
		}
		return target{id: id, cal: ref.Cal, master: &ev, instKey: &k}, nil
	}
	return target{}, fmt.Errorf("%w: scope %q", provider.ErrNotSupported, scope)
}

// instanceID finds the instance of a series whose original start is key.
func (p *Provider) instanceID(ctx context.Context, acctID string, cal domain.Calendar, master domain.Event, key int64) (string, error) {
	lo, hi := time.Unix(key-1, 0).UTC(), time.Unix(key+2, 0).UTC()
	if master.When.AllDay {
		lo, hi = time.Unix(key-86400, 0).UTC(), time.Unix(key+2*86400, 0).UTC()
	}
	var r struct {
		Items []gEvent `json:"items"`
	}
	q := url.Values{"timeMin": {lo.Format(time.RFC3339)}, "timeMax": {hi.Format(time.RFC3339)}, "showDeleted": {"true"}, "maxResults": {"50"}}
	if err := p.send(ctx, acctID, http.MethodGet, evPath(cal, master.RemoteID)+"/instances", q, nil, "", &r); err != nil {
		return "", err
	}
	for _, g := range r.Items {
		if k := originalKey(g.OriginalStart); k != nil && *k == key {
			return g.ID, nil
		}
	}
	return "", provider.ErrNotFound
}

// conflict builds the error the service turns into a 409: the current remote state of ids.
func (p *Provider) conflict(ctx context.Context, acctID string, cal domain.Calendar, ids ...string) error {
	var res provider.Result
	for _, id := range ids {
		var g gEvent
		err := p.send(ctx, acctID, http.MethodGet, evPath(cal, id), nil, nil, "", &g)
		switch {
		case errors.Is(err, provider.ErrNotFound):
			res.Events = append(res.Events, domain.Event{RemoteID: id, Status: domain.Cancelled})
		case err != nil:
			return err
		default:
			res.Events = append(res.Events, p.mapOne(g, cal))
		}
	}
	return &provider.ConflictError{Latest: res}
}

func wrapPrecondition(err error, fetch func() error) error {
	if errors.Is(err, provider.ErrPreconditionFailed) {
		if cerr := fetch(); cerr != nil {
			return cerr
		}
	}
	return err
}

// ---- update ----

func (p *Provider) patchBody(ref provider.Ref, tg target, pt provider.Patch) (map[string]any, bool) {
	b := map[string]any{}
	if pt.Title != nil {
		b["summary"] = *pt.Title
	}
	if pt.Description.Set {
		b["description"] = nullable(pt.Description.Val)
	}
	if pt.Location.Set {
		b["location"] = nullable(pt.Location.Val)
	}
	if pt.When != nil {
		b["start"], b["end"] = whenFields(*pt.When)
	}
	if pt.Attendees.Set {
		var emails []string
		if pt.Attendees.Val != nil {
			emails = *pt.Attendees.Val
		}
		cur := ref.Event
		if tg.master != nil && tg.id == tg.master.RemoteID {
			cur = *tg.master
		}
		b["attendees"] = mergeAttendees(cur.Attendees, emails)
	}
	if pt.Recurrence.Set {
		if pt.Recurrence.Val == nil {
			b["recurrence"] = nil
		} else {
			b["recurrence"] = recurrenceLines(pt.Recurrence.Val)
		}
	}
	if pt.Conference.Set {
		if pt.Conference.Val != nil && *pt.Conference.Val == "meet" {
			b["conferenceData"] = meetRequest()
		} else {
			b["conferenceData"] = nil
		}
	}
	return b, len(b) > 0
}

func nullable(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// mergeAttendees returns the wanted guests keeping known state, plus the organizer and self.
func mergeAttendees(cur []domain.Attendee, want []string) []map[string]any {
	have := map[string]domain.Attendee{}
	for _, a := range cur {
		have[strings.ToLower(a.Email)] = a
	}
	out := []map[string]any{}
	seen := map[string]bool{}
	add := func(a domain.Attendee) {
		k := strings.ToLower(a.Email)
		if seen[k] {
			return
		}
		seen[k] = true
		m := map[string]any{"email": a.Email}
		if a.Name != "" {
			m["displayName"] = a.Name
		}
		if a.Response != "" {
			m["responseStatus"] = string(a.Response)
		}
		if a.Role == "optional" {
			m["optional"] = true
		}
		if a.IsSelf {
			m["self"] = true
		}
		if a.IsOrganizer {
			m["organizer"] = true
		}
		out = append(out, m)
	}
	for _, a := range cur {
		if a.IsSelf || a.IsOrganizer {
			add(a)
		}
	}
	for _, e := range want {
		if a, ok := have[strings.ToLower(e)]; ok {
			add(a)
		} else {
			add(domain.Attendee{Email: e})
		}
	}
	return out
}

func (p *Provider) Update(ctx context.Context, ref provider.Ref, pt provider.Patch, scope provider.Scope) (provider.Result, error) {
	if scope == provider.ScopeFollowing && ref.Event.Kind != domain.Single {
		return p.updateFollowing(ctx, ref, pt)
	}
	tg, err := p.resolve(ctx, ref, scope)
	if err != nil {
		return provider.Result{}, err
	}
	body, ok := p.patchBody(ref, tg, pt)
	if !ok {
		return provider.Result{}, nil
	}
	q := url.Values{"sendUpdates": {"all"}}
	if pt.Conference.Set {
		q.Set("conferenceDataVersion", "1")
	}
	var g gEvent
	err = p.send(ctx, ref.Acct.ID, http.MethodPatch, evPath(tg.cal, tg.id), q, body, tg.etag, &g)
	if err != nil {
		return provider.Result{}, wrapPrecondition(err, func() error { return p.conflict(ctx, ref.Acct.ID, tg.cal, tg.id) })
	}
	if pt.Conference.Set && pt.Conference.Val != nil {
		g = p.awaitMeet(ctx, ref.Acct.ID, tg.cal, g)
	}
	return provider.Result{Events: []domain.Event{p.mapOne(g, tg.cal)}}, nil
}

// ---- delete ----

func (p *Provider) Delete(ctx context.Context, ref provider.Ref, scope provider.Scope) (provider.Result, error) {
	if scope == provider.ScopeFollowing && ref.Event.Kind != domain.Single {
		return p.deleteFollowing(ctx, ref)
	}
	tg, err := p.resolve(ctx, ref, scope)
	if err != nil {
		return provider.Result{}, err
	}
	q := url.Values{"sendUpdates": {"all"}}
	err = p.send(ctx, ref.Acct.ID, http.MethodDelete, evPath(tg.cal, tg.id), q, nil, tg.etag, nil)
	if err != nil && !errors.Is(err, provider.ErrNotFound) {
		return provider.Result{}, wrapPrecondition(err, func() error { return p.conflict(ctx, ref.Acct.ID, tg.cal, tg.id) })
	}
	var res provider.Result
	switch {
	case tg.instKey != nil: // cancelled instance of a series
		res.Events = append(res.Events, cancelledInstance(tg.id, tg.master.RemoteID, *tg.instKey))
	case ref.Event.Kind == domain.Exception && scope == provider.ScopeThis:
		k := ref.Event.RecurrenceKey
		ev := domain.Event{RemoteID: tg.id, Kind: domain.Exception, Status: domain.Cancelled, RemoteSeriesID: ref.Event.RemoteSeriesID, RecurrenceKey: k}
		res.Events = append(res.Events, ev)
	default:
		res.Deleted = append(res.Deleted, tg.id)
		if tg.master != nil || ref.Event.Kind == domain.Master { // a series goes with its exceptions
			for _, x := range ref.Exceptions {
				res.Deleted = append(res.Deleted, x.RemoteID)
			}
		}
	}
	return res, nil
}

func cancelledInstance(id, masterID string, key int64) domain.Event {
	k := key
	return domain.Event{RemoteID: id, Kind: domain.Exception, Status: domain.Cancelled, RemoteSeriesID: masterID, RecurrenceKey: &k}
}

// ---- RSVP ----

func (p *Provider) RSVP(ctx context.Context, ref provider.Ref, r domain.Response, note string, scope provider.Scope) (provider.Result, error) {
	ctx = withOp(ctx, "rsvp")
	if scope == provider.ScopeFollowing {
		return provider.Result{}, fmt.Errorf("%w: replies cannot target following events", provider.ErrNotSupported)
	}
	tg, err := p.resolve(ctx, ref, scope)
	if err != nil {
		return provider.Result{}, err
	}
	// Read the target's current guest list, change only our own entry, write it back guarded by the fresh etag.
	var cur gEvent
	if err := p.send(ctx, ref.Acct.ID, http.MethodGet, evPath(tg.cal, tg.id), nil, nil, "", &cur); err != nil {
		return provider.Result{}, err
	}
	found := false
	list := make([]map[string]any, 0, len(cur.Attendees))
	for _, a := range cur.Attendees {
		m := map[string]any{"email": a.Email}
		if a.DisplayName != "" {
			m["displayName"] = a.DisplayName
		}
		m["responseStatus"] = a.ResponseStatus
		if a.Optional {
			m["optional"] = true
		}
		if a.Self {
			found = true
			m["self"] = true
			m["responseStatus"] = string(r)
			if note != "" {
				m["comment"] = note
			}
		}
		if a.Organizer {
			m["organizer"] = true
		}
		list = append(list, m)
	}
	if !found {
		return provider.Result{}, fmt.Errorf("%w: you are not a guest of this event", provider.ErrNotSupported)
	}
	var g gEvent
	err = p.send(ctx, ref.Acct.ID, http.MethodPatch, evPath(tg.cal, tg.id), url.Values{"sendUpdates": {"all"}}, map[string]any{"attendees": list}, cur.Etag, &g)
	if err != nil {
		return provider.Result{}, wrapPrecondition(err, func() error { return p.conflict(ctx, ref.Acct.ID, tg.cal, tg.id) })
	}
	return provider.Result{Events: []domain.Event{p.mapOne(g, tg.cal)}}, nil
}

// ---- Meet ----

func (p *Provider) AddConference(ctx context.Context, ref provider.Ref) (provider.Result, error) {
	tg, err := p.resolve(ctx, ref, provider.ScopeAll)
	if err != nil {
		return provider.Result{}, err
	}
	var g gEvent
	q := url.Values{"conferenceDataVersion": {"1"}, "sendUpdates": {"all"}}
	err = p.send(ctx, ref.Acct.ID, http.MethodPatch, evPath(tg.cal, tg.id), q, map[string]any{"conferenceData": meetRequest()}, tg.etag, &g)
	if err != nil {
		return provider.Result{}, wrapPrecondition(err, func() error { return p.conflict(ctx, ref.Acct.ID, tg.cal, tg.id) })
	}
	g = p.awaitMeet(ctx, ref.Acct.ID, tg.cal, g)
	return provider.Result{Events: []domain.Event{p.mapOne(g, tg.cal)}}, nil
}

// ---- move (same account) ----

func (p *Provider) Move(ctx context.Context, ref provider.Ref, to domain.Calendar) (provider.Result, error) {
	ctx = withOp(ctx, "move")
	tg, err := p.resolve(ctx, ref, provider.ScopeAll)
	if err != nil {
		return provider.Result{}, err
	}
	var g gEvent
	q := url.Values{"destination": {to.RemoteID}, "sendUpdates": {"all"}}
	err = p.send(ctx, ref.Acct.ID, http.MethodPost, evPath(tg.cal, tg.id)+"/move", q, nil, tg.etag, &g)
	if err != nil {
		return provider.Result{}, wrapPrecondition(err, func() error { return p.conflict(ctx, ref.Acct.ID, tg.cal, tg.id) })
	}
	return provider.Result{Events: []domain.Event{p.mapOne(g, to)}}, nil
}

// ---- "this and following" ----

const (
	untilTimed  = "20060102T150405Z"
	untilAllDay = "20060102"
)

// ruleParts splits an RRULE value into KEY=VALUE parts.
func ruleParts(rule string) []string {
	var out []string
	for _, s := range strings.Split(rule, ";") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func partValue(parts []string, key string) (string, bool) {
	for _, s := range parts {
		if k, v, ok := strings.Cut(s, "="); ok && strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

func without(parts []string, keys ...string) []string {
	var out []string
	for _, s := range parts {
		k, _, _ := strings.Cut(s, "=")
		drop := false
		for _, x := range keys {
			if strings.EqualFold(k, x) {
				drop = true
			}
		}
		if !drop {
			out = append(out, s)
		}
	}
	return out
}

// endBefore ends a rule just before the occurrence at key (COUNT is replaced by UNTIL).
func endBefore(rule string, key int64, allDay bool) string {
	parts := without(ruleParts(rule), "COUNT", "UNTIL")
	t := time.Unix(key, 0).UTC()
	if allDay {
		parts = append(parts, "UNTIL="+t.AddDate(0, 0, -1).Format(untilAllDay))
	} else {
		parts = append(parts, "UNTIL="+t.Add(-time.Second).Format(untilTimed))
	}
	return strings.Join(parts, ";")
}

// tailRule is the rule for the series that starts at the split: a COUNT shrinks by the
// occurrences that stay in the old series; UNTIL carries over.
func tailRule(rule string, before int) string {
	parts := ruleParts(rule)
	if v, ok := partValue(parts, "COUNT"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			parts = append(without(parts, "COUNT"), "COUNT="+strconv.Itoa(max(n-before, 1)))
		}
	}
	return strings.Join(parts, ";")
}

func seriesOf(m domain.Event) recur.Series {
	s := recur.Series{AllDay: m.When.AllDay, TZID: m.When.TZID, Start: m.When.Start, End: m.When.End}
	if m.When.AllDay {
		s.StartDate, s.EndDate = m.When.StartDate.String(), m.When.EndDate.String()
	}
	if m.Recurrence != nil {
		s.RRule = m.Recurrence.RRule
	}
	return s
}

// occurrencesBefore counts how many occurrences of the rule start before key.
func occurrencesBefore(m domain.Event, key int64) int {
	occ, _ := recur.Expand(seriesOf(m), recur.Window{From: time.Unix(0, 0).UTC(), To: time.Unix(key, 0).UTC()}, 0)
	n := 0
	for _, o := range occ {
		if o.Key < key {
			n++
		}
	}
	return n
}

// splitKey is the original start of the occurrence a "following" write begins at.
func splitKey(ref provider.Ref) (int64, error) {
	switch {
	case ref.Event.Kind == domain.Exception && ref.Event.RecurrenceKey != nil:
		return *ref.Event.RecurrenceKey, nil
	case ref.OccKey != nil:
		return *ref.OccKey, nil
	case ref.Event.Kind == domain.Master:
		return keyOf(ref.Event), nil
	}
	return 0, fmt.Errorf("%w: cannot tell which occurrence to split at", provider.ErrNotSupported)
}

func masterOf(ref provider.Ref) (domain.Event, error) {
	m := ref.Event
	if ref.Master != nil {
		m = *ref.Master
	}
	if m.Kind != domain.Master || m.Recurrence == nil || m.Recurrence.RRule == "" {
		return m, fmt.Errorf("%w: series master not mirrored", provider.ErrNotSupported)
	}
	return m, nil
}

// laterLines keeps the EXDATE/RDATE lines on the requested side of the split.
func splitLines(lines []string, def *time.Location, key int64, after bool) []string {
	var out []string
	cut := time.Unix(key, 0)
	for _, l := range lines {
		k := recur.FilterDateLine(l, def, func(t time.Time) bool { return t.Before(cut) != after })
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}

func laterExceptions(ref provider.Ref, key int64) []domain.Event {
	var out []domain.Event
	for _, x := range ref.Exceptions {
		if x.RecurrenceKey != nil && *x.RecurrenceKey >= key && x.Status != domain.Cancelled {
			out = append(out, x)
		}
	}
	return out
}

func (p *Provider) updateFollowing(ctx context.Context, ref provider.Ref, pt provider.Patch) (provider.Result, error) {
	m, err := masterOf(ref)
	if err != nil {
		return provider.Result{}, err
	}
	key, err := splitKey(ref)
	if err != nil {
		return provider.Result{}, err
	}
	if key <= keyOf(m) { // from the first occurrence on = the whole series
		r2 := ref
		r2.Event, r2.Master = m, nil
		return p.Update(ctx, r2, pt, provider.ScopeAll)
	}
	loc, _ := tz.Load(m.When.TZID)
	// 1. the new series, starting at the (possibly edited) occurrence
	nw := m.When
	if pt.When != nil {
		nw = *pt.When
	} else if m.When.AllDay {
		days := int(dateTime(m.When.EndDate).Sub(dateTime(m.When.StartDate)).Hours() / 24)
		sd := time.Unix(key, 0).UTC()
		ed := sd.AddDate(0, 0, days)
		nw.StartDate = domain.Date{Y: sd.Year(), M: int(sd.Month()), D: sd.Day()}
		nw.EndDate = domain.Date{Y: ed.Year(), M: int(ed.Month()), D: ed.Day()}
	} else {
		d := m.When.End.Sub(m.When.Start)
		nw.Start = time.Unix(key, 0).UTC()
		nw.End = nw.Start.Add(d)
	}
	rule := tailRule(m.Recurrence.RRule, occurrencesBefore(m, key))
	if pt.Recurrence.Set && pt.Recurrence.Val != nil && pt.Recurrence.Val.RRule != "" {
		rule = pt.Recurrence.Val.RRule
	}
	lines := []string{"RRULE:" + rule}
	lines = append(lines, splitLines(m.Recurrence.RDates, loc, key, true)...)
	lines = append(lines, splitLines(m.Recurrence.EXDates, loc, key, true)...)
	st, en := whenFields(nw)
	body := map[string]any{"id": newID(), "summary": m.Title, "start": st, "end": en, "recurrence": lines}
	if pt.Title != nil {
		body["summary"] = *pt.Title
	}
	desc, loc2 := m.Description, m.Location
	if pt.Description.Set {
		desc = deref(pt.Description.Val)
	}
	if pt.Location.Set {
		loc2 = deref(pt.Location.Val)
	}
	if desc != "" {
		body["description"] = desc
	}
	if loc2 != "" {
		body["location"] = loc2
	}
	emails := attendeeEmails(m)
	if pt.Attendees.Set {
		emails = nil
		if pt.Attendees.Val != nil {
			emails = *pt.Attendees.Val
		}
	}
	if len(emails) > 0 {
		body["attendees"] = attendeeList(emails)
	}
	q := url.Values{"sendUpdates": {"all"}}
	wantMeet := m.Conference != nil && m.Conference.Kind == "meet"
	if pt.Conference.Set {
		wantMeet = pt.Conference.Val != nil && *pt.Conference.Val == "meet"
	}
	if wantMeet {
		body["conferenceData"] = meetRequest()
		q.Set("conferenceDataVersion", "1")
	}
	var ng gEvent
	if err := p.send(ctx, ref.Acct.ID, http.MethodPost, calPath(ref.Cal), q, body, "", &ng); err != nil {
		return provider.Result{}, err
	}
	// 2. shorten the old series; if it changed under us, undo the new one
	var og gEvent
	err = p.send(ctx, ref.Acct.ID, http.MethodPatch, evPath(ref.Cal, m.RemoteID), url.Values{"sendUpdates": {"none"}},
		map[string]any{"recurrence": append([]string{"RRULE:" + endBefore(m.Recurrence.RRule, key, m.When.AllDay)},
			append(splitLines(m.Recurrence.RDates, loc, key, false), splitLines(m.Recurrence.EXDates, loc, key, false)...)...)}, m.ETag, &og)
	if err != nil {
		if uerr := p.send(ctx, ref.Acct.ID, http.MethodDelete, evPath(ref.Cal, ng.ID), url.Values{"sendUpdates": {"none"}}, nil, "", nil); uerr != nil {
			// the undo failed: the split-off series now exists next to the original (a duplicate)
			telemetry.Inc(ctx, "writes.provider_rollbacks", telemetry.Attr("op", "patch"), telemetry.Attr("kind", "split_undo_failed"))
			telemetry.RecordErrorKind(ctx, "provider.google", telemetry.KindConsistency, uerr, telemetry.Operation("split_undo"))
		}
		return provider.Result{}, wrapPrecondition(err, func() error { return p.conflict(ctx, ref.Acct.ID, ref.Cal, m.RemoteID) })
	}
	res := provider.Result{Events: []domain.Event{p.mapOne(og, ref.Cal)}}
	if wantMeet {
		ng = p.awaitMeet(ctx, ref.Acct.ID, ref.Cal, ng)
	}
	res.Events = append(res.Events, p.mapOne(ng, ref.Cal))
	// 3. overrides from the split on belong to the old rule; Google drops them too
	for _, x := range laterExceptions(ref, key) {
		if err := p.send(ctx, ref.Acct.ID, http.MethodDelete, evPath(ref.Cal, x.RemoteID), url.Values{"sendUpdates": {"none"}}, nil, "", nil); err != nil && !errors.Is(err, provider.ErrNotFound) {
			return res, err
		}
		res.Deleted = append(res.Deleted, x.RemoteID)
	}
	return res, nil
}

func (p *Provider) deleteFollowing(ctx context.Context, ref provider.Ref) (provider.Result, error) {
	m, err := masterOf(ref)
	if err != nil {
		return provider.Result{}, err
	}
	key, err := splitKey(ref)
	if err != nil {
		return provider.Result{}, err
	}
	if key <= keyOf(m) {
		r2 := ref
		r2.Event, r2.Master = m, nil
		return p.Delete(ctx, r2, provider.ScopeAll)
	}
	loc, _ := tz.Load(m.When.TZID)
	var og gEvent
	err = p.send(ctx, ref.Acct.ID, http.MethodPatch, evPath(ref.Cal, m.RemoteID), url.Values{"sendUpdates": {"all"}},
		map[string]any{"recurrence": append([]string{"RRULE:" + endBefore(m.Recurrence.RRule, key, m.When.AllDay)},
			append(splitLines(m.Recurrence.RDates, loc, key, false), splitLines(m.Recurrence.EXDates, loc, key, false)...)...)}, m.ETag, &og)
	if err != nil {
		return provider.Result{}, wrapPrecondition(err, func() error { return p.conflict(ctx, ref.Acct.ID, ref.Cal, m.RemoteID) })
	}
	res := provider.Result{Events: []domain.Event{p.mapOne(og, ref.Cal)}}
	for _, x := range laterExceptions(ref, key) {
		if err := p.send(ctx, ref.Acct.ID, http.MethodDelete, evPath(ref.Cal, x.RemoteID), url.Values{"sendUpdates": {"none"}}, nil, "", nil); err != nil && !errors.Is(err, provider.ErrNotFound) {
			return res, err
		}
		res.Deleted = append(res.Deleted, x.RemoteID)
	}
	return res, nil
}

func dateTime(d domain.Date) time.Time {
	return time.Date(d.Y, time.Month(d.M), d.D, 0, 0, 0, 0, time.UTC)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func attendeeEmails(m domain.Event) []string {
	var out []string
	for _, a := range m.Attendees {
		if a.IsSelf || a.IsOrganizer || a.Role == "resource" {
			continue
		}
		out = append(out, a.Email)
	}
	return out
}
