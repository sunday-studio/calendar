package telemetry_test

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"calendar/server/internal/telemetry"
	"calendar/server/internal/telemetry/telemetrytest"
)

func TestSafeRecoversRecordsAndCountsPanics(t *testing.T) {
	rec := telemetrytest.Install(t)
	ctx, span := telemetry.StartSpan(context.Background(), "worker.pass")
	ran := false
	if !telemetry.Safe(ctx, "test.worker", func() { panic("user text: Dinner with Alice") }) {
		t.Fatal("Safe must report the panic")
	}
	if telemetry.Safe(ctx, "test.worker", func() { ran = true }) || !ran {
		t.Fatal("a clean run is not a panic")
	}
	span.End()
	if rec.Sum(t, "calendar.goroutine.panics", telemetrytest.Attrs{"calendar.goroutine": "test.worker"}) != 1 {
		t.Fatal("panic not counted")
	}
	if rec.Sum(t, "calendar.errors", telemetrytest.Attrs{"calendar.error.kind": "panic", "calendar.component": "test.worker"}) != 1 {
		t.Fatal("panic not recorded as an error")
	}
	d := rec.Dump(t) + rec.Stdout.String()
	if strings.Contains(d, "Dinner with Alice") {
		t.Fatal("panic value leaked")
	}
	if !strings.Contains(d, "exception.stacktrace") {
		t.Fatal("no stack on the exception event")
	}
}

func TestSuperviseRestartsAfterPanicUntilCancelled(t *testing.T) {
	rec := telemetrytest.Install(t)
	ctx, cancel := context.WithCancel(context.Background())
	var runs atomic.Int32
	done := make(chan struct{})
	go func() {
		telemetry.Supervise(ctx, "test.loop", func(ctx context.Context) {
			if runs.Add(1) < 3 {
				panic("again")
			}
			<-ctx.Done()
		})
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for runs.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runs.Load() < 3 {
		t.Fatalf("worker restarted %d times", runs.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Supervise did not stop")
	}
	if rec.Sum(t, "calendar.goroutine.restarts", nil) != 2 || rec.Sum(t, "calendar.goroutine.panics", nil) != 2 {
		t.Fatalf("restarts/panics: %v/%v", rec.Sum(t, "calendar.goroutine.restarts", nil), rec.Sum(t, "calendar.goroutine.panics", nil))
	}
}

func TestGoDoesNotKillTheProcess(t *testing.T) {
	telemetrytest.Install(t)
	done := make(chan struct{})
	telemetry.Go("test.go", func() { defer close(done); panic("x") })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine never ran")
	}
	time.Sleep(50 * time.Millisecond) // still alive
}

func TestInstrumentsFollowTheInstalledProviders(t *testing.T) {
	// the same package-level call sites must keep working across provider changes (tests, restarts)
	for i := 0; i < 2; i++ {
		rec := telemetrytest.Install(t)
		telemetry.Inc(context.Background(), "sync.runs", telemetry.Provider("google"), telemetry.Outcome("ok"))
		telemetry.Record(context.Background(), "sync.duration", 0.2, telemetry.Provider("google"))
		if rec.Sum(t, "calendar.sync.runs", nil) != 1 || rec.HistCount(t, "calendar.sync.duration", nil) != 1 {
			t.Fatalf("round %d: instruments bound to a stale provider", i)
		}
	}
}

func TestAttrKeepsFreeTextOut(t *testing.T) {
	if got := telemetry.Attr("reason", "410").Value.AsString(); got != "410" {
		t.Fatal(got)
	}
	for _, bad := range []string{"Dinner with Alice", "a@b.c", strings.Repeat("x", 40), ""} {
		if got := telemetry.Attr("x", bad).Value.AsString(); got != "other" {
			t.Fatalf("%q -> %q", bad, got)
		}
	}
}

func TestDisabledModeNewHelpersAreInert(t *testing.T) {
	// no Install: the global no-op providers are in place
	ctx := context.Background()
	telemetry.Inc(ctx, "sync.runs", telemetry.Provider("google"))
	telemetry.Record(ctx, "sync.duration", 1)
	telemetry.UpDown(ctx, "http.in_flight", 1)
	off := telemetry.GaugeSet("x.y", "d", "1", func(context.Context) []telemetry.Observation { return nil })
	off()
	telemetry.RegisterProcessGauges("v")
	telemetry.RecordErrorKind(ctx, "x", telemetry.KindConsistency, context.Canceled)
	if telemetry.Safe(ctx, "x", func() {}) {
		t.Fatal("no panic expected")
	}
	if telemetry.Log() == nil {
		t.Fatal("Log must never be nil")
	}
	if telemetry.WrapClientRedacted(nil, "x", "/x") == nil {
		t.Fatal("nil transport must default")
	}
}

func TestProcessGauges(t *testing.T) {
	rec := telemetrytest.Install(t)
	telemetry.RegisterProcessGauges("1.2.3")
	if g := rec.Gauge(t, "calendar.process.uptime_seconds", nil); len(g) != 1 || g[0] < 0 {
		t.Fatal(g)
	}
	if g := rec.Gauge(t, "calendar.build.info", telemetrytest.Attrs{"version": "1.2.3"}); len(g) != 1 || g[0] != 1 {
		t.Fatalf("build.info %v", g)
	}
}

func TestSafeErrorDropsRequestURL(t *testing.T) {
	_, err := (&httpGet{}).do("http://127.0.0.1:1/hook/SECRETPATH?token=Q")
	if err == nil {
		t.Fatal("expected failure")
	}
	if got := telemetry.SafeError(err).Error(); strings.Contains(got, "SECRETPATH") || strings.Contains(got, "token") {
		t.Fatalf("url survived: %s", got)
	}
}

// TestCatalogueIsDocumented keeps docs/engineering/observability.md and the metric catalogue in step.
func TestCatalogueIsDocumented(t *testing.T) {
	b, err := os.ReadFile("../../../docs/engineering/observability.md")
	if err != nil {
		t.Skip("docs not found:", err)
	}
	doc := string(b)
	for name := range telemetry.Instruments() {
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("metric %s is missing from docs/engineering/observability.md", name)
		}
	}
}
