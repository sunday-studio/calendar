package push

import (
	"context"
	"strings"
	"testing"

	"calendar/server/internal/telemetry/telemetrytest"
)

type A = telemetrytest.Attrs

func TestTelemetryPushOutcomes(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newDEnv(t)
	// dead token
	e.rec.err = func(Target) error { return ErrUnregistered }
	e.move("E1", "2026-10-15T13:00:00Z", "2026-10-15T14:00:00Z", false)
	e.process()
	if rec.Sum(t, "calendar.push.sent", A{"calendar.outcome": "dead_token"}) < 1 {
		t.Fatalf("dead_token outcome missing:\n%s", rec.Dump(t))
	}
	if rec.Sum(t, "calendar.push.dead_tokens_removed", nil) != 1 {
		t.Fatal("dead token removal not counted")
	}
	sp := rec.SpansNamed("push.send")
	if len(sp) == 0 || telemetrytest.SpanAttr(sp[0], "calendar.env") != "sandbox" || telemetrytest.SpanAttr(sp[0], "calendar.kind") == "" {
		t.Fatalf("push.send span: %v", rec.SpanNames())
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatal("a dead token is expected housekeeping, not an error")
	}
	// throttled and failed are errors with kind=push
	e.register(e.token, "bbbb", "production")
	e.rec.err = func(Target) error { return ErrThrottled }
	e.move("E1", "2026-10-15T14:00:00Z", "2026-10-15T15:00:00Z", false)
	e.process()
	if rec.Sum(t, "calendar.push.sent", A{"calendar.outcome": "throttled"}) < 1 {
		t.Fatal("throttled outcome missing")
	}
	e.rec.err = func(Target) error { return ErrTransient }
	e.move("E1", "2026-10-15T15:00:00Z", "2026-10-15T16:00:00Z", false)
	e.process()
	if rec.Sum(t, "calendar.push.sent", A{"calendar.outcome": "failed"}) < 1 {
		t.Fatal("failed outcome missing")
	}
	if rec.Sum(t, "calendar.errors", A{"calendar.component": "push", "calendar.error.kind": "push"}) < 2 {
		t.Fatalf("push errors missing:\n%s", rec.Dump(t))
	}
	e.rec.err = nil
	e.register(e.token, "cccc", "sandbox")
	e.move("E1", "2026-10-15T16:00:00Z", "2026-10-15T17:00:00Z", false)
	e.process()
	if rec.Sum(t, "calendar.push.sent", A{"calendar.outcome": "ok"}) < 1 {
		t.Fatal("ok outcome missing")
	}
}

func TestTelemetryAPNsNeverCarriesDeviceToken(t *testing.T) {
	rec := telemetrytest.Install(t)
	_, pemBytes := testKey(t)
	f := newAPNsFake(t, 410, 500, 429)
	ap, err := NewAPNs(APNsConfig{KeyPEM: pemBytes, KeyID: "K", TeamID: "T", Topic: "b", BaseURL: map[string]string{"sandbox": f.URL}})
	if err != nil {
		t.Fatal(err)
	}
	e := newDEnv(t)
	e.disp.Sender = ap
	const dev = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	e.register(e.token, dev, "sandbox")
	for i := 0; i < 3; i++ {
		e.move("E1", "2026-10-15T1"+string(rune('3'+i))+":00:00Z", "2026-10-15T1"+string(rune('4'+i))+":00:00Z", false)
		if err := e.disp.Process(context.Background()); err != nil {
			t.Fatal(err)
		}
		e.register(e.token, dev+string(rune('a'+i)), "sandbox") // a fresh endpoint each round: the last one was dropped or kept
	}
	d := rec.Dump(t) + rec.Stdout.String()
	if strings.Contains(d, dev[:20]) {
		t.Fatalf("device token leaked:\n%s", d)
	}
	var clients int
	for _, s := range rec.Spans() {
		if s.Name() != "" && strings.HasPrefix(s.Name(), "apns POST") {
			clients++
			if strings.Contains(s.Name(), "9f86") {
				t.Fatal("device token in client span name")
			}
		}
	}
	if clients == 0 {
		t.Fatalf("APNs calls got no client spans: %v", rec.SpanNames())
	}
}
