package sync

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"calendar/server/internal/db"
	"calendar/server/internal/domain"
	"calendar/server/internal/recur"
)

// pageOf builds n Google-like events: ids base..base+n, a third with two guests, every 25th a weekly series.
func pageOf(base, n int, etag string) []domain.Event {
	out := make([]domain.Event, 0, n)
	t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	for i := base; i < base+n; i++ {
		st := t0.Add(time.Duration(i%(420*24)) * time.Hour)
		ev := domain.Event{
			RemoteID: fmt.Sprintf("r%07d", i), ICalUID: fmt.Sprintf("r%07d@google.com", i), ETag: etag, Kind: domain.Single, Status: domain.Confirmed, Busy: true,
			Title: fmt.Sprintf("Event %d review", i), Location: "Room 4", Description: "Agenda and notes for the weekly review.",
			When: domain.When{Start: st, End: st.Add(30 * time.Minute), TZID: "Europe/London"},
			Caps: domain.EventCaps{CanEdit: true},
		}
		if i%3 == 0 {
			ev.Attendees = []domain.Attendee{{Email: "a@x.test", Name: "Alex", Response: domain.Accepted, Role: "required", IsSelf: true}, {Email: fmt.Sprintf("g%d@x.test", i%50), Name: "Guest", Response: domain.NeedsAction, Role: "required"}}
		}
		if i%25 == 0 {
			ev.Kind = domain.Master
			ev.Recurrence = &domain.Recurrence{RRule: "FREQ=WEEKLY;COUNT=20"}
		}
		out = append(out, ev)
	}
	return out
}

func benchDB(tb testing.TB) (*db.DB, domain.Calendar) {
	d, err := db.Open(context.Background(), filepath.Join(tb.TempDir(), "b.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { d.Close() })
	now := time.Now().Unix()
	if _, err := d.W.Exec(`INSERT INTO accounts(id,provider,external_id,email,color,status,secret,created_at) VALUES ('A','google','x','a@x.test','cobalt','ok',x'00',?)`, now); err != nil {
		tb.Fatal(err)
	}
	if _, err := d.W.Exec(`INSERT INTO calendars(id,account_id,remote_id,name,tzid,access,is_primary,visible) VALUES ('C','A','primary','Me','Europe/London','owner',1,1)`); err != nil {
		tb.Fatal(err)
	}
	return d, domain.Calendar{ID: "C", AccountID: "A", RemoteID: "primary", TZID: "Europe/London", Access: "owner", Visible: true}
}

func applyPage(tb testing.TB, d *db.DB, cal domain.Calendar, evs []domain.Event) {
	tb.Helper()
	err := d.Write(context.Background(), func(tx *db.Tx) error {
		return Apply(context.Background(), tx, &recur.Materialiser{Now: func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }}, cal, evs, nil, time.Now())
	})
	if err != nil {
		tb.Fatal(err)
	}
}

// BenchmarkSyncUpsertPage measures one 250-event page: first insert, then the same page changed.
func BenchmarkSyncUpsertPage(b *testing.B) {
	d, cal := benchDB(b)
	b.Run("insert", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			applyPage(b, d, cal, pageOf(i*250+1_000_000, 250, `"e1"`))
		}
	})
	applyPage(b, d, cal, pageOf(0, 250, `"e1"`))
	b.Run("update", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			applyPage(b, d, cal, pageOf(0, 250, fmt.Sprintf(`"e%d"`, i+2)))
		}
	})
	b.Run("unchanged", func(b *testing.B) {
		cur := pageOf(0, 250, `"e1"`)
		applyPage(b, d, cal, cur)
		for i := 0; i < b.N; i++ {
			applyPage(b, d, cal, cur)
		}
	})
}

// TestInitialSyncBudget: a 20k-event first sync (80 pages of 250) must stay comfortably short.
func TestInitialSyncBudget(t *testing.T) {
	n := 20000
	if testing.Short() {
		n = 2000
	}
	budget := 30 * time.Second
	if raceEnabled {
		n, budget = 500, 60*time.Second
	}
	d, cal := benchDB(t)
	t0 := time.Now()
	for base := 0; base < n; base += 250 {
		applyPage(t, d, cal, pageOf(base, 250, `"e1"`))
	}
	el := time.Since(t0)
	t.Logf("%d events in %v (%.0f events/s)", n, el, float64(n)/el.Seconds())
	if el > budget {
		t.Fatalf("initial sync too slow: %v", el)
	}
	// a quiet re-sync of the same data is nearly free
	t1 := time.Now()
	for base := 0; base < n; base += 250 {
		applyPage(t, d, cal, pageOf(base, 250, `"e1"`))
	}
	if el2 := time.Since(t1); el2 > el/2+time.Second {
		t.Fatalf("unchanged pages cost %v vs first sync %v", el2, el)
	}
	var live int
	d.R.QueryRow(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`).Scan(&live)
	if live != n {
		t.Fatalf("%d events stored, want %d", live, n)
	}
}
