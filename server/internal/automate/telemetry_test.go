package automate

import (
	"context"
	"strings"
	"testing"

	"calendar/server/internal/telemetry/telemetrytest"
)

type A = telemetrytest.Attrs

func TestTelemetryAutomationFailureAndRetries(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newAEnv(t)
	h := newHook(t, 500, 502, 200)
	a := e.create(Input{Name: "very private name", Trigger: &Trigger{Type: "event.created"}, Action: &Action{Type: "webhook", URL: h.URL + "/hook/SECRETPATH987?token=QUERYSECRET"}})
	run, err := e.svc.Test(context.Background(), a.ID)
	if err != nil || !run.OK {
		t.Fatal(err, run)
	}
	if rec.Sum(t, "calendar.automation.fires", A{"calendar.outcome": "ok", "calendar.action": "webhook"}) != 1 {
		t.Fatalf("fires ok missing:\n%s", rec.Dump(t))
	}
	if rec.Sum(t, "calendar.automation.retries", nil) != 2 {
		t.Fatal("retries not counted")
	}
	if rec.Sum(t, "calendar.automation.webhook.requests", A{"calendar.status_class": "5xx"}) != 2 || rec.HistCount(t, "calendar.automation.webhook.duration", nil) != 3 {
		t.Fatal("webhook request metrics missing")
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatal("a delivery that succeeded after retries is not an error")
	}

	// final failure (404) and an unreachable target
	h2 := newHook(t, 404)
	b := e.create(Input{Name: "y", Trigger: &Trigger{Type: "event.created"}, Action: &Action{Type: "webhook", URL: h2.URL + "/hook/SECRETPATH987?token=QUERYSECRET"}})
	if run, _ = e.svc.Test(context.Background(), b.ID); run.OK {
		t.Fatal("expected failure")
	}
	dead := newHook(t)
	u := dead.URL + "/hook/SECRETPATH987?token=QUERYSECRET"
	dead.Close()
	c := e.create(Input{Name: "z", Trigger: &Trigger{Type: "event.created"}, Action: &Action{Type: "webhook", URL: u}})
	e.svc.Test(context.Background(), c.ID)
	if rec.Sum(t, "calendar.automation.fires", A{"calendar.outcome": "failed"}) != 2 || rec.Sum(t, "calendar.automation.dead_letters", nil) != 2 {
		t.Fatalf("failure accounting:\n%s", rec.Dump(t))
	}
	if rec.Sum(t, "calendar.errors", A{"calendar.component": "automate"}) != 2 {
		t.Fatal("failures must be recorded as errors")
	}
	spans := rec.SpansNamed("automation.fire")
	if len(spans) != 3 {
		t.Fatal(rec.SpanNames())
	}
	d := rec.Dump(t) + rec.Stdout.String()
	for _, secret := range []string{"SECRETPATH987", "QUERYSECRET", "very private name"} {
		if strings.Contains(d, secret) {
			t.Fatalf("%q leaked into telemetry:\n%s", secret, d)
		}
	}
}
