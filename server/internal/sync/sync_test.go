package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"calendar/server/internal/db"
	"calendar/server/internal/provider"
)

func seedCal(e *tenv) {
	e.fake.addCal("primary@x.com", "Me", "owner")
	e.fake.addCal("team", "Team", "reader")
	e.fake.put("primary@x.com", "e1", timed("Standup", "2026-10-12T09:00:00+01:00", "2026-10-12T09:30:00+01:00"))
	e.fake.put("primary@x.com", "e2", allDay("Offsite", "2026-10-14", "2026-10-16"))
	lunch := timed("Lunch with Ama", "2026-10-13T12:00:00+01:00", "2026-10-13T13:00:00+01:00")
	lunch["location"] = "Cafe Zoom https://us02web.zoom.us/j/12345?pwd=abc"
	lunch["attendees"] = []any{
		map[string]any{"email": "me@x.com", "self": true, "responseStatus": "needsAction"},
		map[string]any{"email": "ama@y.com", "displayName": "Ama Mensah", "responseStatus": "accepted", "organizer": true},
	}
	lunch["organizer"] = map[string]any{"email": "ama@y.com", "displayName": "Ama Mensah"}
	e.fake.put("primary@x.com", "e3", lunch)
	weekly := timed("Weekly", "2026-10-05T10:00:00+01:00", "2026-10-05T11:00:00+01:00")
	weekly["recurrence"] = []any{"RRULE:FREQ=WEEKLY;BYDAY=MO", "EXDATE;TZID=Europe/London:20261019T100000"}
	weekly["organizer"] = map[string]any{"email": "me@x.com", "self": true}
	e.fake.put("primary@x.com", "e4", weekly)
	exc := timed("Weekly (moved)", "2026-10-12T14:00:00+00:00", "2026-10-12T15:00:00+00:00")
	exc["recurringEventId"] = "e4"
	exc["originalStartTime"] = map[string]any{"dateTime": "2026-10-12T10:00:00+01:00", "timeZone": "Europe/London"}
	e.fake.put("primary@x.com", "e4_20261012T090000Z", exc)
	e.fake.put("team", "t1", timed("Team sync", "2026-10-15T15:00:00Z", "2026-10-15T16:00:00Z"))
}

func TestInitialSyncMultiPage(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	id := e.addAccount("me@x.com", "rt1")
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	if got := e.status(id); got != "ok" {
		t.Fatalf("status %s", got)
	}
	if n := e.count(`SELECT COUNT(*) FROM calendars WHERE account_id=?`, id); n != 2 {
		t.Fatalf("calendars %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`); n != 6 {
		t.Fatalf("events %d", n)
	}
	if e.fake.hit("events") < 4 { // 5 events @ pageSize 2 = 3 pages + team calendar
		t.Fatalf("expected paging, hits=%d", e.fake.hit("events"))
	}
	// cursors committed
	if n := e.count(`SELECT COUNT(*) FROM calendars WHERE sync_cursor LIKE 'st-%'`); n != 2 {
		t.Fatalf("cursors %d", n)
	}
	// field mapping
	if got := e.str(`SELECT kind FROM events WHERE remote_id='e4'`); got != "master" {
		t.Fatal(got)
	}
	if got := e.str(`SELECT rrule FROM events WHERE remote_id='e4'`); got != "FREQ=WEEKLY;BYDAY=MO" {
		t.Fatal(got)
	}
	if got := e.str(`SELECT exdates FROM events WHERE remote_id='e4'`); got != `["EXDATE;TZID=Europe/London:20261019T100000"]` {
		t.Fatal(got)
	}
	if got := e.str(`SELECT kind||'/'||recurrence_key||'/'||(SELECT remote_id FROM events m WHERE m.id=x.series_id) FROM events x WHERE remote_id='e4_20261012T090000Z'`); got != "exception/1791795600/e4" {
		t.Fatal(got)
	}
	if got := e.str(`SELECT start_date||'..'||end_date FROM events WHERE remote_id='e2' AND all_day=1`); got != "2026-10-14..2026-10-16" {
		t.Fatal(got)
	}
	if got := e.str(`SELECT start_utc FROM events WHERE remote_id='e1'`); got != "1791792000" { // 08:00Z
		t.Fatal(got)
	}
	if got := e.str(`SELECT self_response||'/'||conf_kind||'/'||conf_source||'/'||can_rsvp||can_edit FROM events WHERE remote_id='e3'`); got != "needsAction/zoom/parsed/10" {
		t.Fatal(got)
	}
	if got := e.str(`SELECT can_edit||can_rsvp FROM events WHERE remote_id='t1'`); got != "00" {
		t.Fatalf("reader calendar must be read-only: %s", got)
	}
	if n := e.count(`SELECT COUNT(*) FROM attendees`); n != 2 {
		t.Fatalf("attendees %d", n)
	}
	// occurrences: 4 singles (rkey 0) plus the expanded series (T07)
	if n := e.count(`SELECT COUNT(*) FROM occurrences WHERE rkey=0`); n != 4 {
		t.Fatalf("occurrences %d", n)
	}
	// fts
	if n := e.count(`SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH 'mensah'`); n != 1 {
		t.Fatalf("fts attendee match %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH 'standup'`); n != 1 {
		t.Fatalf("fts title match %d", n)
	}
	// rev log and runs
	if r, _ := e.d.Rev(context.Background()); r < 2 {
		t.Fatalf("rev %d", r)
	}
	if n := e.count(`SELECT COUNT(*) FROM changes WHERE entity='event'`); n != 6 {
		t.Fatalf("event changes %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM sync_runs WHERE account_id=? AND calendar_id IS NULL AND error_kind IS NULL AND added=6`, id); n != 1 {
		t.Fatalf("sync_runs summary %d", n)
	}
	// second pass is a no-op: no rev bump
	r1, _ := e.d.Rev(context.Background())
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	if r2, _ := e.d.Rev(context.Background()); r2 != r1 {
		t.Fatalf("no-op sync bumped rev %d -> %d", r1, r2)
	}
}

func TestIncrementalChangesAndDeletes(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	id := e.addAccount("me@x.com", "rt1")
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	revStandup := e.str(`SELECT rev FROM events WHERE remote_id='e1'`)
	cur1 := e.str(`SELECT sync_cursor FROM calendars WHERE remote_id='primary@x.com'`)

	e.fake.put("primary@x.com", "e3", timed("Dinner with Ama", "2026-10-13T19:00:00+01:00", "2026-10-13T20:00:00+01:00")) // changed, attendees gone
	e.fake.del("primary@x.com", "e2")                                                                                     // all-day deleted
	e.fake.put("primary@x.com", "e5", timed("Dentist", "2026-10-16T08:00:00Z", "2026-10-16T09:00:00Z"))                   // new
	e.fake.del("primary@x.com", "e4_20261012T090000Z")                                                                    // exception cancelled
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	if got := e.str(`SELECT title FROM events WHERE remote_id='e3'`); got != "Dinner with Ama" {
		t.Fatal(got)
	}
	if n := e.count(`SELECT COUNT(*) FROM attendees`); n != 0 {
		t.Fatalf("attendees should be replaced: %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE remote_id='e2' AND deleted_at IS NOT NULL AND status='cancelled'`); n != 1 {
		t.Fatal("e2 not tombstoned")
	}
	if n := e.count(`SELECT COUNT(*) FROM occurrences o JOIN events x ON x.id=o.event_id WHERE x.remote_id='e2'`); n != 0 {
		t.Fatal("tombstone kept occurrence")
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE remote_id='e4_20261012T090000Z' AND deleted_at IS NOT NULL AND recurrence_key IS NOT NULL`); n != 1 {
		t.Fatal("cancelled exception must stay as marker with its key")
	}
	if n := e.count(`SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH 'offsite'`); n != 0 {
		t.Fatal("fts kept deleted event")
	}
	if n := e.count(`SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH 'dentist'`); n != 1 {
		t.Fatal("fts missing new event")
	}
	if n := e.count(`SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH 'lunch'`); n != 0 {
		t.Fatal("fts kept old title")
	}
	if n := e.count(`SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH 'mensah'`); n != 0 {
		t.Fatal("fts kept old attendee text")
	}
	if got := e.str(`SELECT rev FROM events WHERE remote_id='e1'`); got != revStandup {
		t.Fatal("untouched event rev changed")
	}
	if cur2 := e.str(`SELECT sync_cursor FROM calendars WHERE remote_id='primary@x.com'`); cur2 == cur1 {
		t.Fatal("cursor did not advance")
	}
	if n := e.count(`SELECT COUNT(*) FROM changes WHERE op='delete' AND entity='event'`); n != 2 {
		t.Fatalf("delete changes %d", n)
	}
}

func TestCursorExpiredFullResync(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	id := e.addAccount("me@x.com", "rt1")
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	// while the token is stale: one event vanishes silently, one is added
	e.fake.forget("primary@x.com", "e1")
	e.fake.put("primary@x.com", "e9", timed("New thing", "2026-10-20T10:00:00Z", "2026-10-20T11:00:00Z"))
	e.fake.mu.Lock()
	e.fake.expireAll = true
	e.fake.mu.Unlock()
	// expireAll also 410s the (cursorless) retry? No: only syncToken requests do.
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE remote_id='e1' AND deleted_at IS NOT NULL`); n != 1 {
		t.Fatal("event missing from full resync must be tombstoned")
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE remote_id='e9' AND deleted_at IS NULL`); n != 1 {
		t.Fatal("new event missing after resync")
	}
	if got := e.str(`SELECT sync_cursor FROM calendars WHERE remote_id='primary@x.com'`); got == "<null>" {
		t.Fatal("cursor not re-established")
	}
	if e.status(id) != "ok" {
		t.Fatal(e.status(id))
	}
}

func TestRevokedTokenNeedsReauthOthersContinue(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	a := e.addAccount("a@x.com", "rt-a")
	b := e.addAccount("b@x.com", "rt-b")
	e.fake.mu.Lock()
	e.fake.revoked["rt-a"] = true
	e.fake.mu.Unlock()
	err := e.sync(a)
	if !errors.Is(err, provider.ErrNeedsReauth) {
		t.Fatalf("want ErrNeedsReauth, got %v", err)
	}
	if e.status(a) != "needs_reauth" {
		t.Fatal(e.status(a))
	}
	if err := e.sync(b); err != nil || e.status(b) != "ok" {
		t.Fatalf("other account affected: %v %s", err, e.status(b))
	}
	// needs_reauth accounts are skipped silently
	hits := e.fake.hit("token")
	if err := e.sync(a); err != nil || e.fake.hit("token") != hits {
		t.Fatal("needs_reauth account must not be synced")
	}
	// revocation of an already-working account (API 401, then invalid_grant on refresh)
	e.fake.mu.Lock()
	e.fake.revoked["rt-b"] = true
	e.fake.mu.Unlock()
	if err := e.sync(b); !errors.Is(err, provider.ErrNeedsReauth) || e.status(b) != "needs_reauth" {
		t.Fatalf("revoked mid-life: %v %s", err, e.status(b))
	}
	if n := e.count(`SELECT COUNT(*) FROM changes WHERE entity='account' AND op='status'`); n < 2 {
		t.Fatalf("status changes %d", n)
	}
}

func TestManagerWorkersIsolateFailuresAndReauth(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	a := e.addAccount("a@x.com", "rt-a")
	b := e.addAccount("b@x.com", "rt-b")
	e.fake.mu.Lock()
	e.fake.revoked["rt-a"] = true
	e.fake.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() { e.mgr.Run(ctx); close(done) }()
	waitFor(t, func() bool { return e.status(a) == "needs_reauth" && e.status(b) == "ok" })

	// reconnect: oauth callback flips status to connecting; the supervisor restarts a worker
	e.fake.mu.Lock()
	delete(e.fake.revoked, "rt-a")
	e.fake.mu.Unlock()
	if err := e.acc.MarkConnecting(ctx, a); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return e.status(a) == "ok" })

	// manual trigger coalesces and reports queue size
	n, err := e.mgr.TriggerManual(ctx, "")
	if err != nil || n != 2 {
		t.Fatalf("queued %d %v", n, err)
	}
	if _, err := e.mgr.TriggerManual(ctx, "nope"); err == nil {
		t.Fatal("unknown account must error")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

type failingMat struct{ fail bool }

func (f *failingMat) Rebuild(ctx context.Context, tx *db.Tx, ids []string) error {
	if f.fail {
		return errors.New("boom")
	}
	return SimpleMaterialiser{}.Rebuild(ctx, tx, ids)
}

func TestCrashSafetyCursorNotAdvanced(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	fm := &failingMat{fail: true}
	e.mgr.Mat = fm
	id := e.addAccount("me@x.com", "rt1")
	err := e.sync(id)
	if err == nil {
		t.Fatal("expected failure")
	}
	if n := e.count(`SELECT COUNT(*) FROM calendars WHERE sync_cursor IS NOT NULL`); n != 0 {
		t.Fatal("cursor advanced although tx failed")
	}
	if n := e.count(`SELECT COUNT(*) FROM events`); n != 0 {
		t.Fatalf("partial data committed: %d", n)
	}
	if e.status(id) != "error" { // connecting -> error on first failure
		t.Fatal(e.status(id))
	}
	fm.fail = false
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT COUNT(*) FROM events`) != 6 || e.status(id) != "ok" {
		t.Fatal("recovery failed")
	}
	// incremental failure: cursor and data both stay at the old state, change picked up later
	cur := e.str(`SELECT sync_cursor FROM calendars WHERE remote_id='primary@x.com'`)
	e.fake.put("primary@x.com", "e1", timed("Standup v2", "2026-10-12T09:00:00+01:00", "2026-10-12T09:30:00+01:00"))
	fm.fail = true
	if err := e.sync(id); err == nil {
		t.Fatal("expected failure")
	}
	if e.str(`SELECT sync_cursor FROM calendars WHERE remote_id='primary@x.com'`) != cur || e.str(`SELECT title FROM events WHERE remote_id='e1'`) != "Standup" {
		t.Fatal("state moved despite rollback")
	}
	fm.fail = false
	if err := e.sync(id); err != nil || e.str(`SELECT title FROM events WHERE remote_id='e1'`) != "Standup v2" {
		t.Fatalf("change skipped after failure: %v", err)
	}
	if n := e.count(`SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH 'v2'`); n != 1 {
		t.Fatal("fts not rebuilt")
	}
}

func TestRateLimitSleepsAndRetries(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	id := e.addAccount("me@x.com", "rt1")
	// Retry-After beyond the transport cap -> surfaces to the engine, which sleeps itself
	e.fake.mu.Lock()
	e.fake.failN, e.fake.failCode, e.fake.retryAfter = 2, 429, "90"
	e.fake.mu.Unlock()
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	if len(e.slept) != 2 || e.slept[0] != 90*time.Second {
		t.Fatalf("engine sleeps %v", e.slept)
	}
	if e.status(id) != "ok" || e.count(`SELECT COUNT(*) FROM events`) != 6 {
		t.Fatal("not recovered")
	}
	// short Retry-After is absorbed by the transport (no engine sleep)
	e.slept = nil
	e.fake.put("primary@x.com", "e1", timed("S2", "2026-10-12T09:00:00Z", "2026-10-12T09:30:00Z"))
	e.fake.mu.Lock()
	e.fake.failN, e.fake.failCode, e.fake.retryAfter = 1, 429, "2"
	e.fake.mu.Unlock()
	if err := e.sync(id); err != nil || len(e.slept) != 0 {
		t.Fatalf("%v %v", err, e.slept)
	}
}

func TestPersistentRateLimitBacksOffAndErrorsAfterThree(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	id := e.addAccount("me@x.com", "rt1")
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	e.fake.mu.Lock()
	e.fake.failN, e.fake.failCode, e.fake.retryAfter = 1000, 429, "100"
	e.fake.mu.Unlock()
	for i := 1; i <= 3; i++ {
		err := e.sync(id)
		var rl provider.ErrRateLimited
		if !errors.As(err, &rl) {
			t.Fatalf("pass %d: %v", i, err)
		}
		want := "ok"
		if i == 3 {
			want = "error"
		}
		if got := e.status(id); got != want {
			t.Fatalf("pass %d status %s want %s", i, got, want)
		}
		if n := e.count(`SELECT fail_count FROM accounts WHERE id=?`, id); n != i {
			t.Fatalf("pass %d fail_count %d", i, n)
		}
	}
	if n := e.count(`SELECT COUNT(*) FROM accounts WHERE id=? AND next_sync_at > ?`, id, time.Now().Unix()+60); n != 1 {
		t.Fatal("next_sync_at must reflect backoff")
	}
	if n := e.count(`SELECT COUNT(*) FROM sync_runs WHERE error_kind='rate_limited' AND calendar_id IS NULL`); n != 3 {
		t.Fatalf("runs %d", n)
	}
	// recovery
	e.fake.mu.Lock()
	e.fake.failN = 0
	e.fake.mu.Unlock()
	if err := e.sync(id); err != nil || e.status(id) != "ok" || e.count(`SELECT fail_count FROM accounts WHERE id=?`, id) != 0 {
		t.Fatal("did not return to ok")
	}
}

func TestServerErrorsRetriedByTransport(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	id := e.addAccount("me@x.com", "rt1")
	e.fake.mu.Lock()
	e.fake.failN, e.fake.failCode = 2, 503
	e.fake.mu.Unlock()
	if err := e.sync(id); err != nil || e.status(id) != "ok" {
		t.Fatalf("%v %s", err, e.status(id))
	}
}

func TestDiscoverPreservesUserToggles(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	id := e.addAccount("me@x.com", "rt1")
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	e.d.W.Exec(`UPDATE calendars SET visible=0, color_override='rose' WHERE remote_id='team'`)
	e.fake.mu.Lock()
	e.fake.cals[1].summary = "Team renamed"
	e.fake.mu.Unlock()
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	if got := e.str(`SELECT name||'/'||visible||'/'||color_override FROM calendars WHERE remote_id='team'`); got != "Team renamed/0/rose" {
		t.Fatal(got)
	}
	// calendar removed remotely disappears with its events
	e.fake.mu.Lock()
	e.fake.cals = e.fake.cals[:1]
	e.fake.mu.Unlock()
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT COUNT(*) FROM calendars`) != 1 || e.count(`SELECT COUNT(*) FROM events WHERE remote_id='t1'`) != 0 {
		t.Fatal("removed calendar not cleaned up")
	}
}
