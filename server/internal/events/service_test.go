package events

import (
	"context"
	"path/filepath"
	gosync "sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"calendar/server/internal/accounts"
	"calendar/server/internal/agenda"
	"calendar/server/internal/db"
	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
	"calendar/server/internal/provider/google"
	"calendar/server/internal/provider/google/gtest"
	"calendar/server/internal/provider/httpx"
	syncer "calendar/server/internal/sync"
	"calendar/server/internal/vault"
)

var testNow = time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

type spySync struct {
	*syncer.Manager
	mu     gosync.Mutex
	nudges []string
}

func (s *spySync) Nudge(id string) {
	s.mu.Lock()
	s.nudges = append(s.nudges, id)
	s.mu.Unlock()
}

type env struct {
	t    *testing.T
	d    *db.DB
	f    *gtest.Fake
	acc  *accounts.Service
	mgr  *spySync
	svc  *Service
	v    *vault.Vault
	a1   string
	cal1 string // local id of A1's primary calendar
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	v, _ := vault.New([]byte("0123456789abcdef0123456789abcdef"))
	e := &env{t: t, d: d, v: v, f: gtest.New(t)}
	e.acc = &accounts.Service{DB: d, Vault: v}
	prov := google.New(google.Config{
		BaseURL: e.f.URL, TokenURL: e.f.URL + "/token", ClientID: "c", ClientSecret: "s", Refresh: e.acc.RefreshToken,
		Transport: &httpx.Transport{Sleep: func(context.Context, time.Duration) error { return nil }, Jitter: func() float64 { return 0 }},
	})
	provs := map[string]provider.Provider{"google": prov}
	mgr := &syncer.Manager{DB: d, Accounts: e.acc, Providers: provs, Now: func() time.Time { return testNow }}
	e.mgr = &spySync{Manager: mgr}
	e.svc = &Service{DB: d, Accounts: e.acc, Providers: provs, Sync: e.mgr,
		Store: &agenda.Store{DB: d, Now: func() time.Time { return testNow }}, Now: func() time.Time { return testNow }}
	e.f.AddCal("primary", "Me", "owner")
	e.f.AddCal("work", "Work", "owner")
	e.f.AddCal("ro", "Holidays", "reader")
	e.a1 = e.addAccount("me@x.test", "rt1")
	return e
}

func (e *env) addAccount(email, refresh string) string {
	e.t.Helper()
	id := ulid.Make().String()
	sealed, err := e.v.Seal([]byte(`{"refresh_token":"`+refresh+`"}`), []byte(id))
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err = e.d.W.Exec(`INSERT INTO accounts(id,provider,external_id,email,color,status,secret,created_at) VALUES (?,?,?,?,?,?,?,?)`,
		id, "google", "sub-"+email, email, "cobalt", "connecting", sealed, time.Now().Unix()); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) sync() {
	e.t.Helper()
	if err := e.mgr.SyncAccount(context.Background(), e.a1, syncer.Manual); err != nil {
		e.t.Fatal(err)
	}
	e.cal1 = e.calID("primary")
}

func (e *env) calID(remote string) string {
	var id string
	if err := e.d.R.QueryRow(`SELECT id FROM calendars WHERE remote_id=?`, remote).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) eventID(remote string) string {
	var id string
	if err := e.d.R.QueryRow(`SELECT id FROM events WHERE remote_id=? AND deleted_at IS NULL`, remote).Scan(&id); err != nil {
		e.t.Fatalf("event %s: %v", remote, err)
	}
	return id
}

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.d.R.QueryRow(q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) str(q string, args ...any) string {
	e.t.Helper()
	var s *string
	if err := e.d.R.QueryRow(q, args...).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	if s == nil {
		return "<null>"
	}
	return *s
}

func ctx() context.Context { return context.Background() }

func wantKind(t *testing.T, err error, k Kind) *Error {
	t.Helper()
	e, ok := AsError(err)
	if !ok || e.Kind != k {
		t.Fatalf("want error kind %d, got %v (%T)", k, err, err)
	}
	return e
}

func str(s string) *string { return &s }

func timedIn(cal, title, start string, dur int) CreateIn {
	return CreateIn{CalendarID: cal, Title: title, When: When{Start: start, TZID: "Europe/London", DurationMin: dur}}
}

// occurrences lists "title@start" for a series' agenda window in the user's zone.
func (e *env) agendaTitles(from, to string) []string {
	e.t.Helper()
	f, _ := time.Parse(time.RFC3339, from)
	tt, _ := time.Parse(time.RFC3339, to)
	r, err := e.svc.Store.Agenda(ctx(), f, tt, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, it := range r.Items {
		s := it.Title
		if it.Start != nil {
			s += "@" + it.Start.Format("01-02T15:04")
		} else {
			s += "@" + it.Date
		}
		out = append(out, s)
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

// ---- create ----

func TestCreateTimedMirrorsAndNudges(t *testing.T) {
	e := newEnv(t)
	e.sync()
	rem := 15
	in := timedIn(e.cal1, "Lunch", "2026-10-16T13:00:00", 45)
	in.Attendees = []string{"Sam@x.test", "sam@x.test", "kim@x.test"}
	in.Location = "Kōya"
	in.Conference = "meet"
	in.ReminderMin = &rem
	ev, err := e.svc.Create(ctx(), in, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Title != "Lunch" || ev.Start == nil || !ev.Start.Equal(time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)) || ev.TZID != "Europe/London" {
		t.Fatalf("%+v", ev)
	}
	if ev.End.Sub(*ev.Start) != 45*time.Minute || ev.Join == nil || ev.Join.Kind != "meet" || ev.ReminderMin == nil || *ev.ReminderMin != 15 {
		t.Fatalf("%+v", ev)
	}
	if len(ev.Attendees) != 3 || !ev.CanEdit || ev.Version == 0 {
		t.Fatalf("%+v", ev)
	}
	eq(t, e.agendaTitles("2026-10-16T00:00:00Z", "2026-10-17T00:00:00Z"), []string{"Lunch@10-16T12:00"})
	if len(e.mgr.nudges) != 1 || e.mgr.nudges[0] != e.a1 {
		t.Fatalf("nudges %v", e.mgr.nudges)
	}
	// a later sync agrees with the write (same etag -> nothing to change)
	rev, _ := e.d.Rev(ctx())
	e.sync()
	if r2, _ := e.d.Rev(ctx()); r2 != rev {
		t.Fatalf("sync after write changed rev %d -> %d", rev, r2)
	}
}

func TestCreateIdempotentSeed(t *testing.T) {
	e := newEnv(t)
	e.sync()
	a, err := e.svc.Create(ctx(), timedIn(e.cal1, "Once", "2026-10-16T13:00:00", 60), Opts{Seed: "tok|key1"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.svc.Create(ctx(), timedIn(e.cal1, "Once", "2026-10-16T13:00:00", 60), Opts{Seed: "tok|key1"})
	if err != nil {
		t.Fatal(err)
	}
	if a.EventID != b.EventID || len(e.f.IDs("primary")) != 1 || e.count(`SELECT COUNT(*) FROM events`) != 1 {
		t.Fatalf("duplicate: %s %s %v", a.EventID, b.EventID, e.f.IDs("primary"))
	}
}

func TestCreateAllDayAndRecurring(t *testing.T) {
	e := newEnv(t)
	e.sync()
	ev, err := e.svc.Create(ctx(), CreateIn{CalendarID: e.cal1, Title: "Away", When: When{AllDay: true, Date: "2026-10-20", EndDate: "2026-10-23"}}, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if !ev.AllDay || ev.Date != "2026-10-20" || ev.EndDate != "2026-10-23" || ev.Start != nil {
		t.Fatalf("%+v", ev)
	}
	r := timedIn(e.cal1, "Standup", "2026-10-20T10:00:00", 15)
	r.Recurrence = &RecurrenceIn{RRule: "RRULE:FREQ=WEEKLY;COUNT=3"}
	ev, err = e.svc.Create(ctx(), r, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Recurring || ev.Recurrence == nil || ev.Recurrence.RRule != "FREQ=WEEKLY;COUNT=3" {
		t.Fatalf("%+v", ev)
	}
	eq(t, e.agendaTitles("2026-10-19T00:00:00Z", "2026-11-30T00:00:00Z"), []string{"Away@2026-10-20", "Standup@10-20T09:00", "Standup@10-27T10:00", "Standup@11-03T10:00"})
}

func TestCreateValidation(t *testing.T) {
	e := newEnv(t)
	e.sync()
	ro := e.calID("ro")
	bad := func(in CreateIn, k Kind, field string) {
		t.Helper()
		_, err := e.svc.Create(ctx(), in, Opts{})
		if x := wantKind(t, err, k); field != "" && x.Field != field {
			t.Fatalf("field %q want %q (%v)", x.Field, field, x)
		}
	}
	good := timedIn(e.cal1, "x", "2026-10-16T13:00:00", 30)
	bad(timedIn("", "x", "2026-10-16T13:00:00", 30), KValidation, "calendarId")
	bad(timedIn("nope", "x", "2026-10-16T13:00:00", 30), KValidation, "calendarId")
	bad(timedIn(ro, "x", "2026-10-16T13:00:00", 30), KForbidden, "calendarId")
	b := good
	b.When.Start = "2026-10-16T13:00:00Z"
	bad(b, KValidation, "when.start")
	b = good
	b.When.Start = ""
	bad(b, KValidation, "when.start")
	b = good
	b.When.TZID = "Mars/Olympus"
	bad(b, KValidation, "when.tzid")
	b = good
	b.Attendees = []string{"not an email"}
	bad(b, KValidation, "attendees")
	b = good
	b.Recurrence = &RecurrenceIn{RRule: "FREQ=SOMETIMES"}
	bad(b, KValidation, "recurrence.rrule")
	b = good
	b.Conference = "zoom"
	bad(b, KValidation, "conference")
	m := -5
	b = good
	b.ReminderMin = &m
	bad(b, KValidation, "reminderMin")
	bad(CreateIn{CalendarID: e.cal1, When: When{AllDay: true, Date: "2026-10-20", EndDate: "2026-10-20"}}, KValidation, "when.endDate")
	bad(CreateIn{CalendarID: e.cal1, When: When{AllDay: true, Date: "20-10-2026"}}, KValidation, "when.date")
	if len(e.f.Writes()) != 0 {
		t.Fatal("nothing may reach the provider")
	}
}

func TestCreateProviderFailureLeavesNothing(t *testing.T) {
	e := newEnv(t)
	e.sync()
	e.f.FailNext("POST", "/events", 403, 1, "")
	_, err := e.svc.Create(ctx(), timedIn(e.cal1, "x", "2026-10-16T13:00:00", 30), Opts{})
	wantKind(t, err, KProvider)
	if e.count(`SELECT COUNT(*) FROM events`) != 0 || len(e.mgr.nudges) != 0 {
		t.Fatal("half-saved state")
	}
}

// ---- patch ----

func (e *env) seedSingle(id, title string) Event {
	e.f.Put("primary", id, gtest.Timed(title, "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00"))
	e.sync()
	ev, err := e.svc.Store.Event(ctx(), e.eventID(id))
	if err != nil {
		e.t.Fatal(err)
	}
	return ev
}

type Event = agenda.Event

func TestPatchSingleVersionCheck(t *testing.T) {
	e := newEnv(t)
	ev := e.seedSingle("s1", "Old")
	stale := ev.Version - 1
	_, err := e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("New")}, Opts{IfMatch: &stale})
	x := wantKind(t, err, KPrecondition)
	if x.Current == nil || x.Current.Title != "Old" {
		t.Fatalf("%+v", x.Current)
	}
	if len(e.f.Writes()) != 0 {
		t.Fatal("stale version must not reach the provider")
	}
	cur := ev.Version
	got, err := e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("New"), Location: provider.Some("Room 4")}, Opts{IfMatch: &cur})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New" || got.Location != "Room 4" || got.Version <= ev.Version {
		t.Fatalf("%+v", got)
	}
	if e.f.Get("primary", "s1")["summary"] != "New" {
		t.Fatal("provider not updated")
	}
}

func TestPatchWhenKeepsDurationAndZone(t *testing.T) {
	e := newEnv(t)
	ev := e.seedSingle("s1", "Mtg")
	got, err := e.svc.Patch(ctx(), ev.ID, "", PatchIn{When: &When{Start: "2026-10-16T15:30:00"}}, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Start.Equal(time.Date(2026, 10, 16, 14, 30, 0, 0, time.UTC)) || got.End.Sub(*got.Start) != time.Hour || got.TZID != "Europe/London" {
		t.Fatalf("%+v", got)
	}
}

func TestPatchClearAndNullTitle(t *testing.T) {
	e := newEnv(t)
	e.f.Put("primary", "s1", map[string]any{"summary": "X", "location": "Here", "description": "d",
		"start": map[string]any{"dateTime": "2026-10-16T13:00:00+01:00", "timeZone": "Europe/London"},
		"end":   map[string]any{"dateTime": "2026-10-16T14:00:00+01:00", "timeZone": "Europe/London"}})
	e.sync()
	id := e.eventID("s1")
	got, err := e.svc.Patch(ctx(), id, "", PatchIn{Location: provider.Clear[string](), Description: provider.Clear[string]()}, Opts{})
	if err != nil || got.Location != "" || got.Description != "" {
		t.Fatal(err, got)
	}
	wantKind(t, mustErr(e.svc.Patch(ctx(), id, "", PatchIn{}, Opts{})), KValidation)
	wantKind(t, mustErr(e.svc.Patch(ctx(), id, "sideways", PatchIn{Title: str("a")}, Opts{})), KValidation)
}

func mustErr(_ agenda.Event, err error) error { return err }

func TestPatchConflictMirrorsLatest(t *testing.T) {
	e := newEnv(t)
	ev := e.seedSingle("s1", "Mine")
	e.f.Edit("primary", "s1", func(r map[string]any) { r["summary"] = "Edited elsewhere" })
	cur := ev.Version
	_, err := e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("Mine 2")}, Opts{IfMatch: &cur})
	x := wantKind(t, err, KConflict)
	if x.Current == nil || x.Current.Title != "Edited elsewhere" || x.Current.Version <= cur {
		t.Fatalf("%+v", x.Current)
	}
	if got := e.str(`SELECT title FROM events WHERE id=?`, ev.EventID); got != "Edited elsewhere" {
		t.Fatal("mirror not refreshed:", got)
	}
	if e.f.Get("primary", "s1")["summary"] != "Edited elsewhere" {
		t.Fatal("remote overwritten")
	}
}

func TestPatchEventDeletedElsewhere(t *testing.T) {
	e := newEnv(t)
	ev := e.seedSingle("s1", "Mine")
	e.f.Edit("primary", "s1", func(r map[string]any) { r["status"] = "cancelled" })
	_, err := e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("x")}, Opts{})
	wantKind(t, err, KNotFound) // gone elsewhere is a 404, not a conflict
	// the mirror shows it as cancelled/hidden now
	if e.count(`SELECT COUNT(*) FROM events WHERE id=? AND deleted_at IS NULL`, ev.EventID) != 0 {
		t.Fatal("still live")
	}
}

func TestPatchProviderErrorsAreAtomic(t *testing.T) {
	e := newEnv(t)
	ev := e.seedSingle("s1", "Mine")
	rev, _ := e.d.Rev(ctx())
	e.f.FailNext("PATCH", "/events/s1", 403, 1, "")
	_, err := e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("x")}, Opts{})
	wantKind(t, err, KProvider)
	e.f.FailNext("PATCH", "/events/s1", 429, 9, "120")
	_, err = e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("x")}, Opts{})
	if x := wantKind(t, err, KRateLimited); x.RetryAfter != 120*time.Second {
		t.Fatal(x.RetryAfter)
	}
	if r, _ := e.d.Rev(ctx()); r != rev || e.str(`SELECT title FROM events WHERE id=?`, ev.EventID) != "Mine" {
		t.Fatal("mirror changed")
	}
}

func TestReadOnlyEventForbidden(t *testing.T) {
	e := newEnv(t)
	e.f.Put("ro", "h1", gtest.AllDay("Bank holiday", "2026-12-25", "2026-12-26"))
	e.sync()
	id := e.eventID("h1")
	wantKind(t, mustErr(e.svc.Patch(ctx(), id, "", PatchIn{Title: str("x")}, Opts{})), KForbidden)
	wantKind(t, e.svc.Delete(ctx(), id, "", Opts{}), KForbidden)
	if len(e.f.Writes()) != 0 {
		t.Fatal("must not reach provider")
	}
}

func TestAccountStates(t *testing.T) {
	e := newEnv(t)
	ev := e.seedSingle("s1", "Mine")
	e.d.W.Exec(`UPDATE accounts SET status='disabled'`)
	wantKind(t, mustErr(e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("x")}, Opts{})), KForbidden)
	e.d.W.Exec(`UPDATE accounts SET status='needs_reauth'`)
	wantKind(t, mustErr(e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("x")}, Opts{})), KNeedsReauth)
	// a revoked token found at write time flips the account
	e.d.W.Exec(`UPDATE accounts SET status='ok'`)
	e.f.Revoke("rt1")
	_, err := e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("x")}, Opts{})
	wantKind(t, err, KNeedsReauth)
	if got := e.str(`SELECT status FROM accounts`); got != "needs_reauth" {
		t.Fatal(got)
	}
}

func TestWriteHoldsTheSyncLock(t *testing.T) {
	e := newEnv(t)
	ev := e.seedSingle("s1", "Mine")
	l := e.mgr.Lock(e.a1)
	l.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("x")}, Opts{})
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("write ran while sync held the lock")
	case <-time.After(150 * time.Millisecond):
	}
	l.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// ---- recurring scopes ----

func (e *env) seedSeries() string {
	r := gtest.Timed("Standup", "2026-10-06T10:00:00+01:00", "2026-10-06T10:30:00+01:00")
	r["recurrence"] = []any{"RRULE:FREQ=WEEKLY;COUNT=6"}
	e.f.Put("primary", "ser", r)
	e.sync()
	return e.eventID("ser")
}

const win = "2026-10-19T00:00:00Z"
const winEnd = "2026-11-12T00:00:00Z"

func TestSeriesBaseline(t *testing.T) {
	e := newEnv(t)
	e.seedSeries()
	eq(t, e.agendaTitles(win, winEnd), []string{"Standup@10-20T09:00", "Standup@10-27T10:00", "Standup@11-03T10:00", "Standup@11-10T10:00"})
}

func TestPatchThisCreatesException(t *testing.T) {
	e := newEnv(t)
	m := e.seedSeries()
	id := occID(m, time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC).Unix())
	got, err := e.svc.Patch(ctx(), id, "this", PatchIn{Title: str("Special"), When: &When{Start: "2026-10-27T14:00:00"}}, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Special" || got.EventID == m || !got.Recurring {
		t.Fatalf("%+v", got)
	}
	eq(t, e.agendaTitles(win, winEnd), []string{"Standup@10-20T09:00", "Special@10-27T14:00", "Standup@11-03T10:00", "Standup@11-10T10:00"})
	// the master and remote series are intact
	if e.f.Get("primary", "ser")["summary"] != "Standup" {
		t.Fatal("master changed")
	}
	// editing the exception again works through its own id
	got2, err := e.svc.Patch(ctx(), got.ID, "this", PatchIn{Location: provider.Some("HQ")}, Opts{})
	if err != nil || got2.Location != "HQ" || got2.EventID != got.EventID {
		t.Fatal(err, got2)
	}
}

func TestPatchAllShiftsSeries(t *testing.T) {
	e := newEnv(t)
	m := e.seedSeries()
	// edit the 27 Oct occurrence to 11:00 for everyone: the whole series moves +2h (wall clock)
	id := occID(m, time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC).Unix())
	_, err := e.svc.Patch(ctx(), id, "all", PatchIn{When: &When{Start: "2026-10-27T12:00:00"}, Title: str("Sync")}, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, e.agendaTitles(win, winEnd), []string{"Sync@10-20T11:00", "Sync@10-27T12:00", "Sync@11-03T12:00", "Sync@11-10T12:00"})
}

func TestPatchAllFromException(t *testing.T) {
	e := newEnv(t)
	m := e.seedSeries()
	key := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC).Unix()
	ex, err := e.svc.Patch(ctx(), occID(m, key), "this", PatchIn{Title: str("One")}, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Patch(ctx(), ex.ID, "all", PatchIn{Location: provider.Some("HQ")}, Opts{}); err != nil {
		t.Fatal(err)
	}
	if e.f.Get("primary", "ser")["location"] != "HQ" {
		t.Fatal("master not updated")
	}
}

func TestPatchFollowingSplit(t *testing.T) {
	e := newEnv(t)
	m := e.seedSeries()
	// an override after the split disappears, the one before survives
	for _, k := range []time.Time{time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)} {
		if _, err := e.svc.Patch(ctx(), occID(m, k.Unix()), "this", PatchIn{Title: str("ov")}, Opts{}); err != nil {
			t.Fatal(err)
		}
	}
	id := occID(m, time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC).Unix())
	got, err := e.svc.Patch(ctx(), id, "following", PatchIn{Title: str("Retro"), When: &When{Start: "2026-10-20T11:00:00"}}, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Retro" || got.EventID == m || got.Start == nil || !got.Start.Equal(time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v", got)
	}
	eq(t, e.agendaTitles("2026-10-12T00:00:00Z", winEnd), []string{"ov@10-13T09:00", "Retro@10-20T10:00", "Retro@10-27T11:00", "Retro@11-03T11:00", "Retro@11-10T11:00"})
	if e.count(`SELECT COUNT(*) FROM events WHERE kind='master' AND deleted_at IS NULL`) != 2 {
		t.Fatal("expected two masters")
	}
	// a later sync reproduces the same picture
	before := e.agendaTitles("2026-10-01T00:00:00Z", "2026-12-30T00:00:00Z")
	e.sync()
	eq(t, e.agendaTitles("2026-10-01T00:00:00Z", "2026-12-30T00:00:00Z"), before)
}

func TestPatchRecurrenceRuleRules(t *testing.T) {
	e := newEnv(t)
	m := e.seedSeries()
	id := occID(m, time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC).Unix())
	_, err := e.svc.Patch(ctx(), id, "this", PatchIn{Recurrence: provider.Some(RecurrenceIn{RRule: "FREQ=DAILY"})}, Opts{})
	wantKind(t, err, KValidation)
	if _, err := e.svc.Patch(ctx(), id, "all", PatchIn{Recurrence: provider.Some(RecurrenceIn{RRule: "FREQ=WEEKLY;COUNT=2"})}, Opts{}); err != nil {
		t.Fatal(err)
	}
	if n := len(e.agendaTitles("2026-10-01T00:00:00Z", "2026-12-30T00:00:00Z")); n != 2 {
		t.Fatalf("%d occurrences", n)
	}
}

func TestNoSuchOccurrence(t *testing.T) {
	e := newEnv(t)
	m := e.seedSeries()
	wantKind(t, mustErr(e.svc.Patch(ctx(), occID(m, 12345), "this", PatchIn{Title: str("x")}, Opts{})), KNotFound)
	wantKind(t, mustErr(e.svc.Patch(ctx(), "nope", "this", PatchIn{Title: str("x")}, Opts{})), KNotFound)
}

// ---- delete ----

func TestDeleteScopes(t *testing.T) {
	e := newEnv(t)
	m := e.seedSeries()
	k := func(d int, h int) int64 { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC).Unix() }
	if err := e.svc.Delete(ctx(), occID(m, k(27, 10)), "this", Opts{}); err != nil {
		t.Fatal(err)
	}
	eq(t, e.agendaTitles(win, winEnd), []string{"Standup@10-20T09:00", "Standup@11-03T10:00", "Standup@11-10T10:00"})
	if err := e.svc.Delete(ctx(), occID(m, time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC).Unix()), "following", Opts{}); err != nil {
		t.Fatal(err)
	}
	eq(t, e.agendaTitles(win, winEnd), []string{"Standup@10-20T09:00"})
	// survives a resync
	e.sync()
	eq(t, e.agendaTitles(win, winEnd), []string{"Standup@10-20T09:00"})
	if err := e.svc.Delete(ctx(), m, "all", Opts{}); err != nil {
		t.Fatal(err)
	}
	eq(t, e.agendaTitles("2026-10-01T00:00:00Z", "2026-12-30T00:00:00Z"), nil)
	_, err := e.svc.Store.Event(ctx(), m)
	if err == nil {
		t.Fatal("deleted series still readable")
	}
}

func TestDeleteSingleAndStaleVersion(t *testing.T) {
	e := newEnv(t)
	ev := e.seedSingle("s1", "Mine")
	stale := ev.Version - 1
	x := wantKind(t, e.svc.Delete(ctx(), ev.ID, "", Opts{IfMatch: &stale}), KPrecondition)
	if x.Current == nil {
		t.Fatal("no current")
	}
	if err := e.svc.Delete(ctx(), ev.ID, "", Opts{}); err != nil {
		t.Fatal(err)
	}
	if len(e.f.IDs("primary")) != 0 {
		t.Fatal("remote not deleted")
	}
	wantKind(t, e.svc.Delete(ctx(), ev.ID, "", Opts{}), KNotFound)
}

func TestDeleteConflict(t *testing.T) {
	e := newEnv(t)
	ev := e.seedSingle("s1", "Mine")
	e.f.Edit("primary", "s1", func(r map[string]any) { r["summary"] = "Changed" })
	x := wantKind(t, e.svc.Delete(ctx(), ev.ID, "", Opts{}), KConflict)
	if x.Current == nil || x.Current.Title != "Changed" || len(e.f.IDs("primary")) != 1 {
		t.Fatalf("%+v", x)
	}
}

// ---- RSVP, Meet, reminders ----

func TestRSVP(t *testing.T) {
	e := newEnv(t)
	r := gtest.Timed("Invite", "2026-10-16T13:00:00+01:00", "2026-10-16T14:00:00+01:00")
	r["organizer"] = map[string]any{"email": "boss@x.test"}
	r["attendees"] = []any{map[string]any{"email": "boss@x.test", "organizer": true, "responseStatus": "accepted"},
		map[string]any{"email": "me@x.test", "self": true, "responseStatus": "needsAction"}}
	e.f.Put("primary", "inv", r)
	e.sync()
	id := e.eventID("inv")
	ev, err := e.svc.RSVP(ctx(), id, domain.Accepted, "see you", "", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if ev.MyResponse == nil || *ev.MyResponse != "accepted" {
		t.Fatalf("%+v", ev)
	}
	wantKind(t, mustErr(e.svc.RSVP(ctx(), id, "maybe", "", "", Opts{})), KValidation)
	wantKind(t, mustErr(e.svc.RSVP(ctx(), id, domain.Declined, "", "following", Opts{})), KValidation)
	own := e.seedSingle("mine", "Mine")
	wantKind(t, mustErr(e.svc.RSVP(ctx(), own.ID, domain.Accepted, "", "", Opts{})), KForbidden)
}

func TestAddMeet(t *testing.T) {
	e := newEnv(t)
	e.f.MeetDelay = 1
	ev := e.seedSingle("s1", "Mine")
	if !ev.CanAddConference {
		t.Fatal("expected canAddConference")
	}
	got, err := e.svc.AddMeet(ctx(), ev.ID, Opts{})
	if err != nil || got.Join == nil || got.Join.Kind != "meet" || got.CanAddConference {
		t.Fatal(err, got)
	}
	wantKind(t, mustErr(e.svc.AddMeet(ctx(), ev.ID, Opts{})), KForbidden)
}

func TestReminderOverride(t *testing.T) {
	e := newEnv(t)
	m := e.seedSeries()
	ten := 10
	k := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC).Unix()
	if _, err := e.svc.SetReminder(ctx(), m, &ten); err != nil { // series-wide
		t.Fatal(err)
	}
	five := 5
	ev, err := e.svc.SetReminder(ctx(), occID(m, k), &five) // one occurrence
	if err != nil || ev.ReminderMin == nil || *ev.ReminderMin != 5 {
		t.Fatal(err, ev)
	}
	other, _ := e.svc.Store.Event(ctx(), occID(m, time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC).Unix()))
	if other.ReminderMin == nil || *other.ReminderMin != 10 {
		t.Fatalf("series-wide override not applied: %+v", other.ReminderMin)
	}
	ev, err = e.svc.SetReminder(ctx(), occID(m, k), nil)
	if err != nil || ev.ReminderMin == nil || *ev.ReminderMin != 10 {
		t.Fatal(err, ev.ReminderMin)
	}
	neg := -1
	wantKind(t, mustErr(e.svc.SetReminder(ctx(), m, &neg)), KValidation)
	wantKind(t, mustErr(e.svc.SetReminder(ctx(), "nope", &ten)), KNotFound)
	if len(e.f.Writes()) != 0 {
		t.Fatal("reminder is hub-only")
	}
	// via PATCH with other fields it still applies
	ev2, err := e.svc.Patch(ctx(), e.seedSingle("s9", "Z").ID, "", PatchIn{Title: str("Z2"), ReminderMin: provider.Some(30)}, Opts{})
	if err != nil || ev2.ReminderMin == nil || *ev2.ReminderMin != 30 {
		t.Fatal(err, ev2)
	}
}

// ---- move ----

func TestMoveWithinAccountKeepsID(t *testing.T) {
	e := newEnv(t)
	ev := e.seedSingle("s1", "Mine")
	work := e.calID("work")
	got, err := e.svc.Move(ctx(), ev.ID, work, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if got.EventID != ev.EventID || got.CalendarID != work {
		t.Fatalf("%+v", got)
	}
	if e.f.Get("work", "s1") == nil || e.f.Get("primary", "s1") != nil {
		t.Fatal("not moved remotely")
	}
	eq(t, e.agendaTitles("2026-10-16T00:00:00Z", "2026-10-17T00:00:00Z"), []string{"Mine@10-16T12:00"})
	e.sync()
	if e.count(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`) != 1 {
		t.Fatalf("resync duplicated the event")
	}
	wantKind(t, mustErr(e.svc.Move(ctx(), ev.ID, e.calID("ro"), Opts{})), KForbidden)
	wantKind(t, mustErr(e.svc.Move(ctx(), ev.ID, "nope", Opts{})), KValidation)
}

func TestMoveSeriesKeepsExceptions(t *testing.T) {
	e := newEnv(t)
	m := e.seedSeries()
	if _, err := e.svc.Patch(ctx(), occID(m, time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC).Unix()), "this", PatchIn{Title: str("Special")}, Opts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Move(ctx(), m, e.calID("work"), Opts{}); err != nil {
		t.Fatal(err)
	}
	eq(t, e.agendaTitles(win, winEnd), []string{"Standup@10-20T09:00", "Special@10-27T10:00", "Standup@11-03T10:00", "Standup@11-10T10:00"})
	if e.count(`SELECT COUNT(*) FROM occurrences WHERE calendar_id=?`, e.calID("work")) != e.count(`SELECT COUNT(*) FROM occurrences`) {
		t.Fatal("occurrences left on the old calendar")
	}
}

func TestMoveAcrossAccounts(t *testing.T) {
	e := newEnv(t)
	ev := e.seedSingle("s1", "Mine")
	// second account: its own fake calendar set lives on the same fake (distinct calendar id)
	e.f.AddCal("acct2", "Second", "owner")
	a2 := e.addAccount("two@x.test", "rt2")
	if err := e.mgr.SyncAccount(ctx(), a2, syncer.Manual); err != nil {
		t.Fatal(err)
	}
	// the fake serves every calendar to both accounts; keep only the one under test for account 2
	e.d.W.Exec(`UPDATE calendars SET visible=0 WHERE account_id=? AND remote_id<>'acct2'`, a2)
	dest := ""
	e.d.R.QueryRow(`SELECT id FROM calendars WHERE account_id=? AND remote_id='acct2'`, a2).Scan(&dest)
	got, err := e.svc.Move(ctx(), ev.ID, dest, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if got.EventID == ev.EventID || got.AccountID != a2 || got.Title != "Mine" {
		t.Fatalf("%+v", got)
	}
	if e.f.Get("acct2", got.EventID) != nil || len(e.f.IDs("acct2")) != 1 || e.f.Get("primary", "s1") != nil {
		t.Fatalf("remote state: %v %v", e.f.IDs("acct2"), e.f.IDs("primary"))
	}
	if _, err := e.svc.Store.Event(ctx(), ev.ID); err == nil {
		t.Fatal("old id must be gone")
	}
	// repeating events refuse to cross accounts
	m := e.seedSeries()
	wantKind(t, mustErr(e.svc.Move(ctx(), m, dest, Opts{})), KValidation)
}

// ---- search index is maintained by the write path ----

func (e *env) search(q string) []string {
	e.t.Helper()
	pg, err := e.svc.Store.List(ctx(), agenda.ListQuery{Q: q, Limit: 50})
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, it := range pg.Items {
		out = append(out, it.Title)
	}
	return out
}

func TestSearchIndexFollowsWrites(t *testing.T) {
	e := newEnv(t)
	e.sync()
	in := timedIn(e.cal1, "Quarterly roadmap", "2026-10-16T13:00:00", 60)
	in.Location = "Kōya"
	in.Attendees = []string{"samantha@x.test"}
	ev, err := e.svc.Create(ctx(), in, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]int{"roadmap": 1, "road": 1, "koya": 1, "samantha": 1, "nonsense": 0} {
		if got := e.search(q); len(got) != want {
			t.Errorf("after create, %q: %v", q, got)
		}
	}
	if _, err := e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("Budget review"), Location: provider.Clear[string]()}, Opts{}); err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]int{"roadmap": 0, "budget": 1, "koya": 0, "samantha": 1} {
		if got := e.search(q); len(got) != want {
			t.Errorf("after patch, %q: %v", q, got)
		}
	}
	if err := e.svc.Delete(ctx(), ev.ID, "", Opts{}); err != nil {
		t.Fatal(err)
	}
	if got := e.search("budget"); len(got) != 0 {
		t.Fatalf("deleted event still found: %v", got)
	}
	// a recurring series is found once, not once per occurrence
	r := timedIn(e.cal1, "Weekly planning", "2026-10-20T10:00:00", 30)
	r.Recurrence = &RecurrenceIn{RRule: "FREQ=WEEKLY;COUNT=4"}
	if _, err := e.svc.Create(ctx(), r, Opts{}); err != nil {
		t.Fatal(err)
	}
	if got := e.search("planning"); len(got) != 1 {
		t.Fatalf("%v", got)
	}
}
