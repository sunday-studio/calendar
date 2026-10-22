package google_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
	"calendar/server/internal/provider/google"
	"calendar/server/internal/provider/google/gtest"
	"calendar/server/internal/provider/httpx"
)

type env struct {
	t    *testing.T
	p    *google.Provider
	f    *gtest.Fake
	acct domain.Account
	cal  domain.Calendar
}

func setup(t *testing.T) *env {
	google.SetMeetPoll(time.Millisecond)
	f := gtest.New(t)
	f.AddCal("primary", "Me", "owner")
	f.AddCal("other", "Other", "owner")
	p := google.New(google.Config{
		BaseURL: f.URL, TokenURL: f.URL + "/token", ClientID: "c", ClientSecret: "s",
		Refresh: func(context.Context, string) (string, error) { return "rt", nil },
		Transport: &httpx.Transport{
			Sleep:  func(context.Context, time.Duration) error { return nil },
			Jitter: func() float64 { return 0 },
		},
	})
	return &env{t: t, p: p, f: f,
		acct: domain.Account{ID: "A1", Provider: "google", Email: "me@x.test"},
		cal:  domain.Calendar{ID: "C1", AccountID: "A1", RemoteID: "primary", TZID: "Europe/London", Access: "owner"}}
}

// events mirrors what a sync would see, keyed by remote id.
func (e *env) events() map[string]domain.Event {
	e.t.Helper()
	b, err := e.p.Sync(context.Background(), e.acct, e.cal, provider.SyncRequest{})
	if err != nil {
		e.t.Fatal(err)
	}
	m := map[string]domain.Event{}
	for _, ev := range b.Events {
		m[ev.RemoteID] = ev
	}
	return m
}

func (e *env) ref(ev domain.Event) provider.Ref {
	r := provider.Ref{Acct: e.acct, Cal: e.cal, Event: ev}
	for _, x := range e.events() {
		if x.Kind == domain.Exception && x.RemoteSeriesID == ev.RemoteID && x.Status != domain.Cancelled {
			r.Exceptions = append(r.Exceptions, x)
		}
	}
	return r
}

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func str(s string) *string { return &s }

func TestCreateTimedWithGuestsAndMeet(t *testing.T) {
	e := setup(t)
	in := provider.EventInput{
		Title: "Lunch", Location: "Kōya", Attendees: []string{"sam@x.test"}, Conference: true,
		When: domain.When{Start: utc("2026-10-16T12:00:00Z"), End: utc("2026-10-16T13:00:00Z"), TZID: "Europe/London"},
	}
	res, err := e.p.Create(context.Background(), e.acct, e.cal, in)
	if err != nil || len(res.Events) != 1 {
		t.Fatal(err, res)
	}
	ev := res.Events[0]
	if ev.Title != "Lunch" || ev.Conference == nil || ev.Conference.Kind != "meet" || !ev.When.Start.Equal(in.When.Start) || ev.When.TZID != "Europe/London" {
		t.Fatalf("%+v", ev)
	}
	if !ev.Caps.CanEdit || len(ev.Attendees) != 2 {
		t.Fatalf("%+v", ev)
	}
	w := e.f.Writes()
	if len(w) != 1 || w[0].Query["sendUpdates"][0] != "all" || w[0].Query["conferenceDataVersion"][0] != "1" {
		t.Fatalf("%+v", w)
	}
	st := w[0].Body["start"].(map[string]any)
	if st["dateTime"] != "2026-10-16T13:00:00+01:00" || st["timeZone"] != "Europe/London" {
		t.Fatalf("%v", st)
	}
}

func TestCreateAllDay(t *testing.T) {
	e := setup(t)
	res, err := e.p.Create(context.Background(), e.acct, e.cal, provider.EventInput{Title: "Holiday",
		When: domain.When{AllDay: true, StartDate: domain.Date{Y: 2026, M: 10, D: 20}, EndDate: domain.Date{Y: 2026, M: 10, D: 22}}})
	if err != nil {
		t.Fatal(err)
	}
	w := res.Events[0].When
	if !w.AllDay || w.StartDate.String() != "2026-10-20" || w.EndDate.String() != "2026-10-22" {
		t.Fatalf("%+v", w)
	}
}

func TestCreateRetryAdoptsExisting(t *testing.T) {
	e := setup(t)
	in := provider.EventInput{ID: "abcde12345", Title: "Once",
		When: domain.When{Start: utc("2026-10-16T12:00:00Z"), End: utc("2026-10-16T13:00:00Z"), TZID: "Europe/London"}}
	a, err := e.p.Create(context.Background(), e.acct, e.cal, in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.p.Create(context.Background(), e.acct, e.cal, in)
	if err != nil {
		t.Fatal(err)
	}
	if a.Events[0].RemoteID != b.Events[0].RemoteID || len(e.f.IDs("primary")) != 1 {
		t.Fatalf("duplicate created: %v", e.f.IDs("primary"))
	}
}

func TestUpdateSingleAndClear(t *testing.T) {
	e := setup(t)
	e.f.Put("primary", "s1", gtest.Timed("Old", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00"))
	e.f.Edit("primary", "s1", func(r map[string]any) { r["location"] = "Room"; r["description"] = "d" })
	ev := e.events()["s1"]
	res, err := e.p.Update(context.Background(), e.ref(ev), provider.Patch{
		Title: str("New"), Location: provider.Clear[string](), Description: provider.Some("hello"),
		When: &domain.When{Start: utc("2026-10-16T14:00:00Z"), End: utc("2026-10-16T14:30:00Z"), TZID: "Europe/London"},
	}, provider.ScopeThis)
	if err != nil {
		t.Fatal(err)
	}
	got := res.Events[0]
	if got.Title != "New" || got.Location != "" || got.Description != "hello" || got.When.End.Sub(got.When.Start) != 30*time.Minute {
		t.Fatalf("%+v", got)
	}
	if w := e.f.Writes(); w[0].IfMatch != ev.ETag {
		t.Fatalf("If-Match %q want %q", w[0].IfMatch, ev.ETag)
	}
}

func TestUpdateConflictReturnsLatest(t *testing.T) {
	e := setup(t)
	e.f.Put("primary", "s1", gtest.Timed("Mine", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00"))
	ev := e.events()["s1"]
	e.f.Edit("primary", "s1", func(r map[string]any) { r["summary"] = "Edited elsewhere" })
	_, err := e.p.Update(context.Background(), e.ref(ev), provider.Patch{Title: str("Mine 2")}, provider.ScopeThis)
	var ce *provider.ConflictError
	if !errors.Is(err, provider.ErrPreconditionFailed) || !errors.As(err, &ce) {
		t.Fatal(err)
	}
	if len(ce.Latest.Events) != 1 || ce.Latest.Events[0].Title != "Edited elsewhere" {
		t.Fatalf("%+v", ce.Latest)
	}
	if e.f.Get("primary", "s1")["summary"] != "Edited elsewhere" {
		t.Fatal("remote must be untouched")
	}
}

func TestUpdateGoneIsConflictWithTombstone(t *testing.T) {
	e := setup(t)
	e.f.Put("primary", "s1", gtest.Timed("X", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00"))
	ev := e.events()["s1"]
	e.f.Edit("primary", "s1", func(r map[string]any) { r["summary"] = "bump" })
	e.f.Put("primary", "s1", gtest.Timed("X2", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00"))
	_, err := e.p.Update(context.Background(), e.ref(ev), provider.Patch{Title: str("Y")}, provider.ScopeThis)
	if !errors.Is(err, provider.ErrPreconditionFailed) {
		t.Fatal(err)
	}
}

func TestAttendeesKeepStateAndSelf(t *testing.T) {
	e := setup(t)
	raw := gtest.Timed("Sync", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00")
	raw["organizer"] = map[string]any{"email": "me@x.test", "self": true}
	raw["attendees"] = []any{
		map[string]any{"email": "me@x.test", "self": true, "organizer": true, "responseStatus": "accepted"},
		map[string]any{"email": "a@x.test", "responseStatus": "accepted"},
		map[string]any{"email": "b@x.test", "responseStatus": "declined"},
	}
	e.f.Put("primary", "s1", raw)
	ev := e.events()["s1"]
	list := []string{"a@x.test", "c@x.test"}
	res, err := e.p.Update(context.Background(), e.ref(ev), provider.Patch{Attendees: provider.Some(list)}, provider.ScopeThis)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]domain.Response{}
	for _, a := range res.Events[0].Attendees {
		got[a.Email] = a.Response
	}
	if len(got) != 3 || got["me@x.test"] != domain.Accepted || got["a@x.test"] != domain.Accepted || got["c@x.test"] != domain.NeedsAction {
		t.Fatalf("%v", got)
	}
}

func weekly(t *testing.T, e *env, id, rule string) domain.Event {
	r := gtest.Timed("Standup", "2026-10-06T10:00:00+01:00", "2026-10-06T10:30:00+01:00")
	r["recurrence"] = []any{"RRULE:" + rule}
	e.f.Put("primary", id, r)
	return e.events()[id]
}

func TestScopeThisOnOccurrenceCreatesException(t *testing.T) {
	e := setup(t)
	m := weekly(t, e, "ser", "FREQ=WEEKLY;COUNT=6")
	key := utc("2026-10-20T09:00:00Z").Unix()
	ref := e.ref(m)
	ref.OccKey = &key
	res, err := e.p.Update(context.Background(), ref, provider.Patch{Title: str("Only this")}, provider.ScopeThis)
	if err != nil {
		t.Fatal(err)
	}
	x := res.Events[0]
	if x.Kind != domain.Exception || x.RemoteSeriesID != "ser" || x.RecurrenceKey == nil || *x.RecurrenceKey != key || x.Title != "Only this" {
		t.Fatalf("%+v", x)
	}
	if x.RemoteID != "ser_20261020T090000Z" {
		t.Fatal(x.RemoteID)
	}
	if e.f.Get("primary", "ser")["summary"] != "Standup" {
		t.Fatal("master must not change")
	}
}

func TestScopeThisOnExistingException(t *testing.T) {
	e := setup(t)
	m := weekly(t, e, "ser", "FREQ=WEEKLY;COUNT=6")
	key := utc("2026-10-20T09:00:00Z").Unix()
	ref := e.ref(m)
	ref.OccKey = &key
	if _, err := e.p.Update(context.Background(), ref, provider.Patch{Title: str("One")}, provider.ScopeThis); err != nil {
		t.Fatal(err)
	}
	ex := e.events()["ser_20261020T090000Z"]
	r2 := provider.Ref{Acct: e.acct, Cal: e.cal, Event: ex, Master: &m}
	res, err := e.p.Update(context.Background(), r2, provider.Patch{Title: str("Two")}, provider.ScopeThis)
	if err != nil || res.Events[0].Title != "Two" {
		t.Fatal(err, res)
	}
}

func TestScopeAllFromException(t *testing.T) {
	e := setup(t)
	m := weekly(t, e, "ser", "FREQ=WEEKLY;COUNT=6")
	key := utc("2026-10-20T09:00:00Z").Unix()
	ref := e.ref(m)
	ref.OccKey = &key
	_, _ = e.p.Update(context.Background(), ref, provider.Patch{Title: str("One")}, provider.ScopeThis)
	ex := e.events()["ser_20261020T090000Z"]
	res, err := e.p.Update(context.Background(), provider.Ref{Acct: e.acct, Cal: e.cal, Event: ex, Master: &m}, provider.Patch{Location: provider.Some("HQ")}, provider.ScopeAll)
	if err != nil || res.Events[0].RemoteID != "ser" || res.Events[0].Location != "HQ" {
		t.Fatal(err, res)
	}
}

func TestDeleteScopes(t *testing.T) {
	e := setup(t)
	m := weekly(t, e, "ser", "FREQ=WEEKLY;COUNT=6")
	key := utc("2026-10-13T09:00:00Z").Unix()
	ref := e.ref(m)
	ref.OccKey = &key
	res, err := e.p.Delete(context.Background(), ref, provider.ScopeThis)
	if err != nil || len(res.Events) != 1 {
		t.Fatal(err, res)
	}
	c := res.Events[0]
	if c.Status != domain.Cancelled || c.Kind != domain.Exception || *c.RecurrenceKey != key || c.RemoteSeriesID != "ser" {
		t.Fatalf("%+v", c)
	}
	// the cancelled instance is visible to sync as a tombstone
	if got := e.events()["ser_20261013T090000Z"]; got.Status != domain.Cancelled {
		t.Fatalf("%+v", got)
	}
	// deleting the whole series removes master and exception rows
	res, err = e.p.Delete(context.Background(), e.ref(m), provider.ScopeAll)
	if err != nil || len(res.Deleted) < 1 || res.Deleted[0] != "ser" {
		t.Fatal(err, res)
	}
	if len(e.f.IDs("primary")) != 0 {
		t.Fatal(e.f.IDs("primary"))
	}
}

func TestDeleteGoneIsOK(t *testing.T) {
	e := setup(t)
	e.f.Put("primary", "s1", gtest.Timed("X", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00"))
	ev := e.events()["s1"]
	e.f.Edit("primary", "s1", func(r map[string]any) { r["summary"] = "y" })
	if _, err := e.p.Delete(context.Background(), e.ref(ev), provider.ScopeThis); !errors.Is(err, provider.ErrPreconditionFailed) {
		t.Fatal(err)
	}
	ev = e.events()["s1"]
	if _, err := e.p.Delete(context.Background(), e.ref(ev), provider.ScopeThis); err != nil {
		t.Fatal(err)
	}
	if _, err := e.p.Delete(context.Background(), e.ref(ev), provider.ScopeThis); err != nil {
		t.Fatal("already gone must succeed:", err)
	}
}

func ruleOf(raw map[string]any) string {
	for _, l := range raw["recurrence"].([]any) {
		if s := l.(string); strings.HasPrefix(s, "RRULE:") {
			return s
		}
	}
	return ""
}

func TestFollowingSplitsSeries(t *testing.T) {
	e := setup(t)
	m := weekly(t, e, "ser", "FREQ=WEEKLY;COUNT=6")
	// an override after the split is dropped, one before survives
	for _, d := range []string{"20261013T090000Z", "20261103T100000Z"} {
		k := utc(map[string]string{"20261013T090000Z": "2026-10-13T09:00:00Z", "20261103T100000Z": "2026-11-03T10:00:00Z"}[d]).Unix()
		r := e.ref(m)
		r.OccKey = &k
		if _, err := e.p.Update(context.Background(), r, provider.Patch{Title: str("ov " + d)}, provider.ScopeThis); err != nil {
			t.Fatal(err)
		}
	}
	m = e.events()["ser"]
	key := utc("2026-10-20T09:00:00Z").Unix()
	ref := e.ref(m)
	ref.OccKey = &key
	newWhen := domain.When{Start: utc("2026-10-20T10:00:00Z"), End: utc("2026-10-20T11:00:00Z"), TZID: "Europe/London"}
	res, err := e.p.Update(context.Background(), ref, provider.Patch{Title: str("Retro"), When: &newWhen}, provider.ScopeFollowing)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 2 || len(res.Deleted) != 1 || res.Deleted[0] != "ser_20261103T100000Z" {
		t.Fatalf("%+v", res)
	}
	old := e.f.Get("primary", "ser")
	if r := ruleOf(old); r != "RRULE:FREQ=WEEKLY;UNTIL=20261020T085959Z" {
		t.Fatal(r)
	}
	var nw map[string]any
	for _, id := range e.f.IDs("primary") {
		if id != "ser" && !strings.HasPrefix(id, "ser_") {
			nw = e.f.Get("primary", id)
		}
	}
	if nw == nil || nw["summary"] != "Retro" {
		t.Fatalf("no new series: %v", e.f.IDs("primary"))
	}
	if r := ruleOf(nw); r != "RRULE:FREQ=WEEKLY;COUNT=4" {
		t.Fatal(r)
	}
	st := nw["start"].(map[string]any)["dateTime"]
	if st != "2026-10-20T11:00:00+01:00" {
		t.Fatal(st)
	}
	if e.f.Get("primary", "ser_20261103T100000Z") != nil {
		t.Fatal("later override should be gone")
	}
	if e.f.Get("primary", "ser_20261013T090000Z") == nil {
		t.Fatal("earlier override must stay")
	}
	// the old master was patched with the mirrored etag
	var patchIM string
	for _, w := range e.f.Writes() {
		if w.Method == "PATCH" && strings.HasSuffix(w.Path, "/events/ser") {
			patchIM = w.IfMatch
		}
	}
	if patchIM != m.ETag {
		t.Fatalf("If-Match %q want %q", patchIM, m.ETag)
	}
}

func TestFollowingKeepsUntilAndExdates(t *testing.T) {
	e := setup(t)
	r := gtest.Timed("Daily", "2026-10-06T10:00:00+01:00", "2026-10-06T10:30:00+01:00")
	r["recurrence"] = []any{"RRULE:FREQ=DAILY;UNTIL=20261231T000000Z",
		"EXDATE;TZID=Europe/London:20261008T100000,20261022T100000"}
	e.f.Put("primary", "ser", r)
	m := e.events()["ser"]
	key := utc("2026-10-15T09:00:00Z").Unix()
	ref := e.ref(m)
	ref.OccKey = &key
	if _, err := e.p.Update(context.Background(), ref, provider.Patch{Title: str("Later")}, provider.ScopeFollowing); err != nil {
		t.Fatal(err)
	}
	old := e.f.Get("primary", "ser")["recurrence"].([]any)
	if old[0] != "RRULE:FREQ=DAILY;UNTIL=20261015T085959Z" || old[1] != "EXDATE;TZID=Europe/London:20261008T100000" {
		t.Fatalf("%v", old)
	}
	for _, id := range e.f.IDs("primary") {
		if id == "ser" {
			continue
		}
		nw := e.f.Get("primary", id)["recurrence"].([]any)
		if nw[0] != "RRULE:FREQ=DAILY;UNTIL=20261231T000000Z" || nw[1] != "EXDATE;TZID=Europe/London:20261022T100000" {
			t.Fatalf("%v", nw)
		}
	}
}

func TestFollowingAtFirstOccurrenceIsAll(t *testing.T) {
	e := setup(t)
	m := weekly(t, e, "ser", "FREQ=WEEKLY")
	key := utc("2026-10-06T09:00:00Z").Unix()
	ref := e.ref(m)
	ref.OccKey = &key
	res, err := e.p.Update(context.Background(), ref, provider.Patch{Title: str("Whole")}, provider.ScopeFollowing)
	if err != nil || len(res.Events) != 1 || res.Events[0].RemoteID != "ser" || len(e.f.IDs("primary")) != 1 {
		t.Fatal(err, res)
	}
}

func TestFollowingAllDay(t *testing.T) {
	e := setup(t)
	r := gtest.AllDay("Trip", "2026-10-05", "2026-10-06")
	r["recurrence"] = []any{"RRULE:FREQ=WEEKLY;COUNT=4"}
	e.f.Put("primary", "ser", r)
	m := e.events()["ser"]
	key := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC).Unix()
	ref := e.ref(m)
	ref.OccKey = &key
	if _, err := e.p.Update(context.Background(), ref, provider.Patch{Title: str("Trip B")}, provider.ScopeFollowing); err != nil {
		t.Fatal(err)
	}
	if r := ruleOf(e.f.Get("primary", "ser")); r != "RRULE:FREQ=WEEKLY;UNTIL=20261018" {
		t.Fatal(r)
	}
	for _, id := range e.f.IDs("primary") {
		if id != "ser" {
			nw := e.f.Get("primary", id)
			if ruleOf(nw) != "RRULE:FREQ=WEEKLY;COUNT=2" || nw["start"].(map[string]any)["date"] != "2026-10-19" || nw["end"].(map[string]any)["date"] != "2026-10-20" {
				t.Fatalf("%v", nw)
			}
		}
	}
}

func TestFollowingConflictUndoesNewSeries(t *testing.T) {
	e := setup(t)
	m := weekly(t, e, "ser", "FREQ=WEEKLY;COUNT=6")
	e.f.Edit("primary", "ser", func(r map[string]any) { r["summary"] = "elsewhere" })
	key := utc("2026-10-20T09:00:00Z").Unix()
	ref := e.ref(m)
	ref.OccKey = &key
	_, err := e.p.Update(context.Background(), ref, provider.Patch{Title: str("x")}, provider.ScopeFollowing)
	if !errors.Is(err, provider.ErrPreconditionFailed) {
		t.Fatal(err)
	}
	if ids := e.f.IDs("primary"); len(ids) != 1 {
		t.Fatalf("new series must be rolled back: %v", ids)
	}
}

func TestDeleteFollowing(t *testing.T) {
	e := setup(t)
	m := weekly(t, e, "ser", "FREQ=WEEKLY;COUNT=6")
	k := utc("2026-11-03T10:00:00Z").Unix()
	r := e.ref(m)
	r.OccKey = &k
	_, _ = e.p.Update(context.Background(), r, provider.Patch{Title: str("ov")}, provider.ScopeThis)
	m = e.events()["ser"]
	key := utc("2026-10-20T09:00:00Z").Unix()
	ref := e.ref(m)
	ref.OccKey = &key
	res, err := e.p.Delete(context.Background(), ref, provider.ScopeFollowing)
	if err != nil || len(res.Events) != 1 || len(res.Deleted) != 1 {
		t.Fatal(err, res)
	}
	if r := ruleOf(e.f.Get("primary", "ser")); r != "RRULE:FREQ=WEEKLY;UNTIL=20261020T085959Z" {
		t.Fatal(r)
	}
	if len(e.f.IDs("primary")) != 1 {
		t.Fatal(e.f.IDs("primary"))
	}
}

func invite(e *env, id string) {
	r := gtest.Timed("Invite", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00")
	r["organizer"] = map[string]any{"email": "boss@x.test"}
	r["attendees"] = []any{
		map[string]any{"email": "boss@x.test", "organizer": true, "responseStatus": "accepted"},
		map[string]any{"email": "me@x.test", "self": true, "responseStatus": "needsAction"},
	}
	e.f.Put("primary", id, r)
}

func TestRSVPWithNote(t *testing.T) {
	e := setup(t)
	invite(e, "inv")
	ev := e.events()["inv"]
	res, err := e.p.RSVP(context.Background(), e.ref(ev), domain.Accepted, "see you", provider.ScopeThis)
	if err != nil {
		t.Fatal(err)
	}
	got := res.Events[0]
	if got.SelfResponse == nil || *got.SelfResponse != domain.Accepted || got.Title != "Invite" {
		t.Fatalf("%+v", got)
	}
	at := e.f.Get("primary", "inv")["attendees"].([]any)
	var self map[string]any
	for _, a := range at {
		if a.(map[string]any)["self"] == true {
			self = a.(map[string]any)
		}
	}
	if self["comment"] != "see you" || self["responseStatus"] != "accepted" {
		t.Fatalf("%v", at)
	}
	if _, err := e.p.RSVP(context.Background(), e.ref(ev), domain.Declined, "", provider.ScopeFollowing); !errors.Is(err, provider.ErrNotSupported) {
		t.Fatal(err)
	}
}

func TestRSVPNotAGuest(t *testing.T) {
	e := setup(t)
	e.f.Put("primary", "s1", gtest.Timed("Mine", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00"))
	if _, err := e.p.RSVP(context.Background(), e.ref(e.events()["s1"]), domain.Accepted, "", provider.ScopeThis); !errors.Is(err, provider.ErrNotSupported) {
		t.Fatal(err)
	}
}

func TestRSVPInstanceOfSeries(t *testing.T) {
	e := setup(t)
	r := gtest.Timed("Weekly invite", "2026-10-06T10:00:00+01:00", "2026-10-06T10:30:00+01:00")
	r["recurrence"] = []any{"RRULE:FREQ=WEEKLY;COUNT=4"}
	r["organizer"] = map[string]any{"email": "boss@x.test"}
	r["attendees"] = []any{map[string]any{"email": "boss@x.test", "organizer": true, "responseStatus": "accepted"},
		map[string]any{"email": "me@x.test", "self": true, "responseStatus": "needsAction"}}
	e.f.Put("primary", "ser", r)
	m := e.events()["ser"]
	key := utc("2026-10-13T09:00:00Z").Unix()
	ref := e.ref(m)
	ref.OccKey = &key
	res, err := e.p.RSVP(context.Background(), ref, domain.TentativeResponse, "", provider.ScopeThis)
	if err != nil {
		t.Fatal(err)
	}
	if x := res.Events[0]; x.Kind != domain.Exception || *x.SelfResponse != domain.TentativeResponse {
		t.Fatalf("%+v", x)
	}
	// the series itself is untouched
	for _, a := range e.f.Get("primary", "ser")["attendees"].([]any) {
		if a.(map[string]any)["self"] == true && a.(map[string]any)["responseStatus"] != "needsAction" {
			t.Fatal("series changed")
		}
	}
}

func TestAddConferenceWaitsForLink(t *testing.T) {
	e := setup(t)
	e.f.MeetDelay = 2
	e.f.Put("primary", "s1", gtest.Timed("Mine", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00"))
	res, err := e.p.AddConference(context.Background(), e.ref(e.events()["s1"]))
	if err != nil {
		t.Fatal(err)
	}
	if c := res.Events[0].Conference; c == nil || c.Kind != "meet" {
		t.Fatalf("%+v", res.Events[0])
	}
}

func TestMove(t *testing.T) {
	e := setup(t)
	e.f.Put("primary", "s1", gtest.Timed("Mine", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00"))
	to := domain.Calendar{ID: "C2", AccountID: "A1", RemoteID: "other", Access: "owner", TZID: "Europe/London"}
	res, err := e.p.Move(context.Background(), e.ref(e.events()["s1"]), to)
	if err != nil || len(res.Events) != 1 {
		t.Fatal(err, res)
	}
	if res.Events[0].CalendarID != "C2" || e.f.Get("other", "s1") == nil || e.f.Get("primary", "s1") != nil {
		t.Fatal("not moved")
	}
}

func TestErrorMapping(t *testing.T) {
	e := setup(t)
	e.f.Put("primary", "s1", gtest.Timed("Mine", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00"))
	ev := e.events()["s1"]
	e.f.FailNext("PATCH", "/events/s1", 403, 1, "")
	_, err := e.p.Update(context.Background(), e.ref(ev), provider.Patch{Title: str("x")}, provider.ScopeThis)
	var he *google.HTTPError
	if !errors.As(err, &he) || he.Status != 403 {
		t.Fatal(err)
	}
	e.f.FailNext("PATCH", "/events/s1", 429, 5, "600")
	_, err = e.p.Update(context.Background(), e.ref(ev), provider.Patch{Title: str("x")}, provider.ScopeThis)
	var rl provider.ErrRateLimited
	if !errors.As(err, &rl) || rl.RetryAfter != 600*time.Second {
		t.Fatal(err)
	}
	ref := e.ref(ev)
	e.f.Revoke("rt")
	_, err = e.p.Update(context.Background(), ref, provider.Patch{Title: str("x")}, provider.ScopeThis)
	if !errors.Is(err, provider.ErrNeedsReauth) {
		t.Fatal(err)
	}
}
