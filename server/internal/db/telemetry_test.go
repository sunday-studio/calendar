package db

import (
	"context"
	"sync"
	"testing"
	"time"

	"calendar/server/internal/telemetry/telemetrytest"
)

type A = telemetrytest.Attrs

func TestTelemetryMigrationSpanAndWriteMetrics(t *testing.T) {
	rec := telemetrytest.Install(t)
	d, _ := open(t)
	sp := rec.SpansNamed("db.migrate")
	if len(sp) != 1 || telemetrytest.SpanAttr(sp[0], "calendar.migrations.applied") != telemetrytest.SpanAttr(sp[0], "calendar.migrations.known") {
		t.Fatalf("migrate span: %v", rec.SpanNames())
	}
	if got := rec.Sum(t, "calendar.db.migrations.applied", nil); got != float64(len(MigrationNames())) {
		t.Fatalf("migrations applied %v", got)
	}
	ctx := context.Background()
	if err := d.Write(ctx, func(tx *Tx) error { return tx.Change(ctx, "event", "x", "upsert") }); err != nil {
		t.Fatal(err)
	}
	if rec.Sum(t, "calendar.db.write.transactions", A{"calendar.outcome": "committed"}) < 1 || rec.HistCount(t, "calendar.db.write.duration", nil) < 1 {
		t.Fatal("write metrics missing")
	}
	// a rolled-back transaction is its own outcome (and not an error: the caller owns the reason)
	d.Write(ctx, func(tx *Tx) error { return context.Canceled })
	if rec.Sum(t, "calendar.db.write.transactions", A{"calendar.outcome": "rolled_back"}) != 1 {
		t.Fatal("rollback not counted")
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatal("unexpected errors")
	}
}

func TestTelemetrySlowTransactionAndWriterWait(t *testing.T) {
	rec := telemetrytest.Install(t)
	d, _ := open(t)
	ctx := context.Background()
	hold := make(chan struct{})
	started := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.Write(ctx, func(tx *Tx) error { close(started); <-hold; return nil })
	}()
	<-started
	waited := make(chan struct{})
	go func() { d.Write(ctx, func(tx *Tx) error { return nil }); close(waited) }()
	time.Sleep(120 * time.Millisecond)
	close(hold)
	wg.Wait()
	<-waited
	if rec.Sum(t, "calendar.db.busy", A{"calendar.reason": "wait"}) != 1 {
		t.Fatalf("writer wait not counted:\n%s", rec.Dump(t))
	}
	if rec.Sum(t, "calendar.db.slow_queries", nil) < 2 {
		t.Fatal("slow transactions not counted")
	}
	found := false
	for _, s := range rec.SpansNamed("db.write") {
		found = found || telemetrytest.HasEvent(s, "slow write transaction")
	}
	if !found {
		t.Fatal("no slow-write span event")
	}
}
