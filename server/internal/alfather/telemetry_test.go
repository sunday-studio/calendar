package alfather

import (
	"context"
	"strings"
	"testing"
	"time"

	"calendar/server/internal/telemetry/telemetrytest"
)

type A = telemetrytest.Attrs

func TestTelemetryQuickAddSuccessThenDegradedOnTimeout(t *testing.T) {
	rec := telemetrytest.Install(t)
	f := newFake(t, "parse_event", text(good))
	rp := &rules{}
	if _, err := parser(f, rp, time.Second).Parse(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if rec.Sum(t, "calendar.quickadd.requests", A{"calendar.source": "alfather", "calendar.degraded": "false"}) != 1 {
		t.Fatalf("alfather request missing:\n%s", rec.Dump(t))
	}
	if rec.HistCount(t, "calendar.alfather.latency", nil) != 1 {
		t.Fatal("latency missing")
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatal("success recorded errors")
	}

	f.delay = 1500 * time.Millisecond
	d, err := parser(f, rp, 200*time.Millisecond).Parse(context.Background(), req())
	degraded(t, d, err, rp)
	if rec.Sum(t, "calendar.quickadd.requests", A{"calendar.source": "rules", "calendar.degraded": "true"}) != 1 {
		t.Fatalf("degraded request missing:\n%s", rec.Dump(t))
	}
	// the fallback must not count the same request a second time
	if rec.Sum(t, "calendar.quickadd.requests", nil) != 2 {
		t.Fatalf("requests counted %v", rec.Sum(t, "calendar.quickadd.requests", nil))
	}
	if rec.Sum(t, "calendar.alfather.timeouts", nil) != 1 {
		t.Fatal("timeout not counted")
	}
	if rec.Sum(t, "calendar.errors", A{"calendar.component": "alfather", "calendar.error.kind": "timeout"}) != 1 {
		t.Fatalf("timeout error missing:\n%s", rec.Dump(t))
	}
	sp := rec.SpansNamed("quickadd.parse")
	if len(sp) != 2 {
		t.Fatal(rec.SpanNames())
	}
	if d := rec.Dump(t); strings.Contains(d, "Lunch with Sam") || strings.Contains(d, "Bearer tok") {
		t.Fatal("request text or bearer token in telemetry")
	}
}
