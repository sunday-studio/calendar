package agenda_test

import (
	"strings"
	"testing"
	"time"

	"calendar/server/internal/agenda"
	"calendar/server/internal/telemetry/telemetrytest"
)

func TestTelemetryQueryKinds(t *testing.T) {
	rec := telemetrytest.Install(t)
	st, _ := world(t)
	from, to := time.Unix(u("2026-10-12T00:00:00Z"), 0).UTC(), time.Unix(u("2026-10-20T00:00:00Z"), 0).UTC()
	if _, err := st.Agenda(ctx, from, to, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(ctx, agenda.ListQuery{From: &from, To: &to}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(ctx, agenda.ListQuery{From: &from, To: &to, Q: "stand"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FreeBusy(ctx, from, to); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FreeTime(ctx, agenda.FreeTimeQuery{DurationMin: 30, From: from, To: to}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DaySummary(ctx, "2026-10-15"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Event(ctx, "NOPE"); err == nil {
		t.Fatal("expected not found")
	}
	for kind, want := range map[string]uint64{"agenda": 2, "events": 2, "search": 1, "freebusy": 1, "freetime": 1, "summary": 1} {
		// agenda is 2 because DaySummary reads the agenda too; events is 2 (List without q, Event)
		if got := rec.HistCount(t, "calendar.agenda.query.duration", telemetrytest.Attrs{"calendar.kind": kind}); got < want {
			t.Errorf("kind %s: %d samples, want >= %d", kind, got, want)
		}
	}
	if rec.Sum(t, "calendar.search.queries", nil) != 1 {
		t.Fatal("search.queries")
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatalf("not found is an outcome, not an error:\n%s", rec.Dump(t))
	}
	if d := rec.Dump(t); strings.Contains(d, "stand") && strings.Contains(d, "stand\n") {
		t.Fatal("the search text leaked")
	}
}
