package recur

import (
	"context"
	"testing"
	"time"

	"calendar/server/internal/db"
	"calendar/server/internal/telemetry/telemetrytest"
)

func TestTelemetryRebuildsCapHitsAndExtendWindow(t *testing.T) {
	rec := telemetrytest.Install(t)
	ctx := context.Background()
	d := openDB(t)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	m := &Materialiser{Now: func() time.Time { return now }, MaxPerSerie: 10}
	if _, err := d.W.Exec(`INSERT INTO events(id,calendar_id,remote_id,kind,start_utc,end_utc,tzid,rrule,rev) VALUES ('m','c','m','master',1791795600,1791799200,'UTC','FREQ=DAILY',1)`); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(ctx, func(tx *db.Tx) error { return m.Rebuild(ctx, tx, []string{"m"}) }); err != nil {
		t.Fatal(err)
	}
	if rec.Sum(t, "calendar.recur.rebuilds", nil) != 1 || rec.HistCount(t, "calendar.recur.rebuild.duration", nil) != 1 || rec.HistCount(t, "calendar.recur.occurrences", nil) != 1 {
		t.Fatalf("rebuild accounting:\n%s", rec.Dump(t))
	}
	if rec.Sum(t, "calendar.recur.cap_hits", nil) != 1 {
		t.Fatal("cap hit not counted")
	}
	sp := rec.SpansNamed("recur.rebuild")
	if len(sp) != 1 || !telemetrytest.HasEvent(sp[0], "recur.cap_hit") {
		t.Fatalf("spans %v", rec.SpanNames())
	}
	m.extendOnce(ctx, d)
	if rec.Sum(t, "calendar.recur.extend_window.runs", telemetrytest.Attrs{"calendar.outcome": "ok"}) != 1 || len(rec.SpansNamed("recur.extend_window")) != 1 {
		t.Fatal("extend window run not recorded")
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatal("a cap hit is a warning metric, not an error")
	}
}
