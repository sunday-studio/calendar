package sync

import (
	"context"
	"errors"
	gosync "sync"
	"sync/atomic"
	"testing"
	"time"

	"calendar/server/internal/provider"
)

type evlog struct {
	mu gosync.Mutex
	l  []string
}

func (e *evlog) add(s string) { e.mu.Lock(); e.l = append(e.l, s); e.mu.Unlock() }
func (e *evlog) get() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.l...)
}

func TestSyncAndAccountEvents(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	var ev evlog
	e.mgr.OnSync = func(id, st string) { ev.add("sync:" + st) }
	e.acc.OnStatus = func(id, st string) { ev.add("account:" + st) }
	a := e.addAccount("a@x.com", "rt-a")

	if err := e.sync(a); err != nil {
		t.Fatal(err)
	}
	// connecting -> ok is announced before the pass is reported finished
	want := []string{"sync:started", "account:ok", "sync:finished"}
	if got := ev.get(); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("%v", got)
	}

	e.fake.mu.Lock()
	e.fake.revoked["rt-a"] = true
	e.fake.mu.Unlock()
	if err := e.sync(a); !errors.Is(err, provider.ErrNeedsReauth) {
		t.Fatal(err)
	}
	got := ev.get()[3:]
	want = []string{"sync:started", "account:needs_reauth", "sync:failed"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("%v", got)
	}

	// a skipped (ineligible) account emits nothing
	n := len(ev.get())
	_ = e.sync(a)
	if len(ev.get()) != n {
		t.Fatalf("ineligible account emitted %v", ev.get()[n:])
	}
}

func TestCadenceFollowsSSEClients(t *testing.T) {
	e := newTEnv(t)
	e.mgr.Fast, e.mgr.Slow = time.Minute, 5*time.Minute
	clients := 0
	e.mgr.SSEClients = func() int { return clients }
	if e.mgr.cadence() != 5*time.Minute {
		t.Fatal(e.mgr.cadence())
	}
	clients = 2
	if e.mgr.cadence() != time.Minute {
		t.Fatal(e.mgr.cadence())
	}
}

func TestPruneRuns(t *testing.T) {
	e := newTEnv(t)
	id := e.addAccount("a@x.com", "rt")
	now := time.Now()
	for _, age := range []time.Duration{time.Hour, 29 * 24 * time.Hour, 31 * 24 * time.Hour, 90 * 24 * time.Hour} {
		if _, err := e.d.W.Exec(`INSERT INTO sync_runs(account_id,started_at) VALUES (?,?)`, id, now.Add(-age).Unix()); err != nil {
			t.Fatal(err)
		}
	}
	n, err := e.mgr.PruneRuns(context.Background())
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if c := e.count(`SELECT COUNT(*) FROM sync_runs`); c != 2 {
		t.Fatal(c)
	}
}

// A client connecting after a pass ended must shorten the wait for the next pass: the cadence is read on every wake,
// not fixed when the pass ends (phones reconnect often, and a pass that ended while none was connected used to be
// followed by a full Slow wait).
func TestCadenceIsReadOnEveryWake(t *testing.T) {
	e := newTEnv(t)
	seedCal(e)
	e.addAccount("a@x.com", "rt-a")
	var clients atomic.Int32
	e.mgr.Fast, e.mgr.Slow = 150*time.Millisecond, time.Hour
	e.mgr.SSEClients = func() int { return int(clients.Load()) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() { e.mgr.Run(ctx); close(done) }()
	ticks := func() int { return e.count(`SELECT COUNT(*) FROM sync_runs WHERE calendar_id IS NULL`) }
	waitFor(t, func() bool { return ticks() >= 1 })
	// let the start-up passes finish (a loaded machine can run more than one), then expect quiet
	n := ticks()
	for settled := 0; settled < 3; {
		time.Sleep(200 * time.Millisecond)
		if m := ticks(); m == n {
			settled++
		} else {
			n, settled = m, 0
		}
	}
	time.Sleep(400 * time.Millisecond)
	if ticks() != n {
		t.Fatal("polled again with no client connected and an hour-long Slow cadence")
	}
	clients.Store(1)
	waitFor(t, func() bool { return ticks() > n })
}

// Google answers 410 fullSyncRequired to the sync token of its public holiday calendars on every request. They are read
// in full, without a token, and then left alone until they are due, so they neither fail nor flip the account to syncing.
func TestGoogleHolidayCalendarsAreReadWithoutASyncTokenAndLeftAlone(t *testing.T) {
	e := newTEnv(t)
	e.fake.addCal("en.gh#holiday@group.v.calendar.google.com", "Holidays in Ghana", "reader")
	e.fake.put("en.gh#holiday@group.v.calendar.google.com", "h1", allDay("Independence Day", "2026-03-06", "2026-03-07"))
	e.fake.mu.Lock()
	e.fake.expireAll = true
	e.fake.mu.Unlock()
	a := e.addAccount("a@x.com", "rt-a")

	if err := e.sync(a); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`); n != 1 {
		t.Fatalf("events after the first read: %d", n)
	}
	if h := e.fake.hit("events"); h != 1 {
		t.Fatalf("first read made %d events.list calls, want 1 (no sync token, so no 410 and no retry)", h)
	}

	// a routine poll skips it: it is not due for hours
	if err := e.mgr.SyncAccount(context.Background(), a, Tick); err != nil {
		t.Fatal(err)
	}
	if h := e.fake.hit("events"); h != 1 {
		t.Fatalf("a routine poll read the holiday calendar again (%d calls)", h)
	}

	// a manual sync still reads it, again without a token
	if err := e.sync(a); err != nil {
		t.Fatal(err)
	}
	if h := e.fake.hit("events"); h != 2 {
		t.Fatalf("manual sync made %d calls in total, want 2", h)
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`); n != 1 {
		t.Fatalf("events after the second read: %d", n)
	}
}
