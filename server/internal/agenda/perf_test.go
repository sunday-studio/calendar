package agenda_test

import (
	"fmt"
	"testing"
	"time"

	"calendar/server/internal/agenda"
	. "calendar/server/internal/agenda/agendatest"
	"calendar/server/internal/db"
)

// A big household: 8 accounts, 40 calendars, 20k events over ~14 months (a quarter all-day,
// one in ten with guests, one in thirty a repeating series with 12 occurrences).
const (
	perfAccounts  = 8
	perfCalendars = 40
)

var words = []string{"standup", "review", "lunch", "dentist", "planning", "budget", "workshop", "interview", "retro", "call"}

func seedBig(tb testing.TB, n int) *db.DB {
	d := Open(tb)
	base := u("2026-06-01T00:00:00Z")
	Do(tb, d, func(s *Seed) {
		for a := 0; a < perfAccounts; a++ {
			s.Account(fmt.Sprintf("A%d", a), "cobalt", fmt.Sprintf("a%d@x.test", a))
		}
		for c := 0; c < perfCalendars; c++ {
			s.Calendar(fmt.Sprintf("C%02d", c), fmt.Sprintf("A%d", c%perfAccounts), fmt.Sprintf("Calendar %d", c), c%10 != 9, "owner", c < perfAccounts)
		}
		for i := 0; i < n; i++ {
			st := base + int64(i%(420*24))*3600 + int64(i/(420*24))*900
			ev := Ev{ID: fmt.Sprintf("E%06d", i), Cal: fmt.Sprintf("C%02d", i%perfCalendars), Title: fmt.Sprintf("%s %d", words[i%len(words)], i),
				Location: fmt.Sprintf("Room %d", i%50), Start: st, End: st + 1800}
			switch {
			case i%4 == 0:
				day := time.Unix(st, 0).UTC().Format(time.DateOnly)
				ev.Start, ev.End, ev.SD, ev.ED = 0, 0, day, time.Unix(st, 0).UTC().AddDate(0, 0, 1).Format(time.DateOnly)
			case i%30 == 1:
				ev.Kind, ev.RRule = "master", "FREQ=WEEKLY"
				for k := 0; k < 12; k++ {
					o := st + int64(k)*7*86400
					ev.Occ = append(ev.Occ, Occ{RKey: o, Start: o, End: o + 1800})
				}
			case i%10 == 3:
				ev.Attendees = [][2]string{{fmt.Sprintf("guest%d@x.test", i%200), fmt.Sprintf("Guest %d", i%200)}}
			}
			s.Event(ev)
		}
	})
	return d
}

func perfN() int {
	if raceEnabled {
		return 3000 // seeding is ~30x slower under the race detector
	}
	return 20000
}

func limit(normal time.Duration) time.Duration {
	if raceEnabled {
		return normal * 10
	}
	return normal
}

func best(f func()) time.Duration {
	var b time.Duration
	for i := 0; i < 5; i++ {
		t0 := time.Now()
		f()
		if el := time.Since(t0); i == 0 || el < b {
			b = el
		}
	}
	return b
}

func TestPerformanceBudgets20k(t *testing.T) {
	d := seedBig(t, perfN())
	st := &agenda.Store{DB: d, Now: func() time.Time { return u2("2026-10-15T08:00:00Z") }}
	check := func(name string, budget time.Duration, f func()) {
		t.Helper()
		el := best(f)
		t.Logf("%-28s %v (budget %v)", name, el, limit(budget))
		if el > limit(budget) {
			t.Errorf("%s too slow: %v > %v", name, el, limit(budget))
		}
	}
	from, to := u2("2026-10-05T00:00:00Z"), u2("2026-10-19T00:00:00Z")
	check("agenda 14 days", 50*time.Millisecond, func() {
		if r, err := st.Agenda(ctx, from, to, nil); err != nil || len(r.Items) == 0 {
			t.Fatal(err, len(r.Items))
		}
	})
	check("agenda 35 days (month)", 120*time.Millisecond, func() {
		if _, err := st.Agenda(ctx, u2("2026-10-01T00:00:00Z"), u2("2026-11-05T00:00:00Z"), nil); err != nil {
			t.Fatal(err)
		}
	})
	check("agenda one calendar", 30*time.Millisecond, func() {
		if _, err := st.Agenda(ctx, from, to, []string{"C03"}); err != nil {
			t.Fatal(err)
		}
	})
	check("search common word", 100*time.Millisecond, func() {
		if p, err := st.List(ctx, agenda.ListQuery{Q: "standup", Limit: 50}); err != nil || len(p.Items) == 0 {
			t.Fatal(err, len(p.Items))
		}
	})
	check("search prefix + guest", 100*time.Millisecond, func() {
		if _, err := st.List(ctx, agenda.ListQuery{Q: "gue", Limit: 50}); err != nil {
			t.Fatal(err)
		}
	})
	check("search rare word", 60*time.Millisecond, func() {
		if _, err := st.List(ctx, agenda.ListQuery{Q: "room 17 dentist", Limit: 50}); err != nil {
			t.Fatal(err)
		}
	})
	check("list window", 80*time.Millisecond, func() {
		f, e := from, to
		if _, err := st.List(ctx, agenda.ListQuery{From: &f, To: &e, Limit: 50}); err != nil {
			t.Fatal(err)
		}
	})
	check("freetime 7 days 2h", 100*time.Millisecond, func() {
		if _, err := st.FreeTime(ctx, agenda.FreeTimeQuery{DurationMin: 120, From: u2("2026-10-15T08:00:00Z"), To: u2("2026-10-22T00:00:00Z")}); err != nil {
			t.Fatal(err)
		}
	})
	check("freetime 4 weeks 1h", 150*time.Millisecond, func() {
		if _, err := st.FreeTime(ctx, agenda.FreeTimeQuery{DurationMin: 60, From: u2("2026-10-15T08:00:00Z"), To: u2("2026-11-12T00:00:00Z")}); err != nil {
			t.Fatal(err)
		}
	})
	check("freebusy 14 days", 40*time.Millisecond, func() {
		if _, err := st.FreeBusy(ctx, from, to); err != nil {
			t.Fatal(err)
		}
	})
	check("event detail", 10*time.Millisecond, func() {
		if _, err := st.Event(ctx, "E000001:"+fmt.Sprint(u("2026-06-01T01:00:00Z"))); err != nil && err != agenda.ErrNotFound {
			t.Fatal(err)
		}
	})
	check("day summary", 80*time.Millisecond, func() {
		if _, err := st.DaySummary(ctx, "2026-10-15"); err != nil {
			t.Fatal(err)
		}
	})
}

func u2(s string) time.Time { return time.Unix(u(s), 0).UTC() }

func BenchmarkAgenda14d(b *testing.B) {
	d := seedBig(b, perfN())
	st := &agenda.Store{DB: d}
	from, to := u2("2026-10-05T00:00:00Z"), u2("2026-10-19T00:00:00Z")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.Agenda(ctx, from, to, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearch(b *testing.B) {
	d := seedBig(b, perfN())
	st := &agenda.Store{DB: d}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.List(ctx, agenda.ListQuery{Q: "standup", Limit: 50}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFreeTime7d(b *testing.B) {
	d := seedBig(b, perfN())
	st := &agenda.Store{DB: d, Now: func() time.Time { return u2("2026-10-15T08:00:00Z") }}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.FreeTime(ctx, agenda.FreeTimeQuery{DurationMin: 120, From: u2("2026-10-15T08:00:00Z"), To: u2("2026-10-22T00:00:00Z")}); err != nil {
			b.Fatal(err)
		}
	}
}
