package quickadd

import (
	"context"
	"strings"
	"testing"

	"calendar/server/internal/telemetry/telemetrytest"
)

func TestTelemetryRuleParserRequestsAndAmbiguity(t *testing.T) {
	rec := telemetrytest.Install(t)
	run(t, "Lunch with Sam tomorrow 1pm")
	d := run(t, "Coffee with Alex tomorrow") // two Alexes: ambiguous
	if len(d.Ambiguities) == 0 {
		t.Fatal("expected an ambiguity")
	}
	a := telemetrytest.Attrs{"calendar.source": "rules", "calendar.degraded": "false"}
	if rec.Sum(t, "calendar.quickadd.requests", a) != 2 {
		t.Fatalf("requests:\n%s", rec.Dump(t))
	}
	if rec.Sum(t, "calendar.quickadd.requests", telemetrytest.Attrs{"calendar.outcome": "ambiguous"}) != 1 || rec.Sum(t, "calendar.quickadd.ambiguities", nil) != 1 {
		t.Fatal("ambiguity not counted")
	}
	// a request handled as a fallback is not counted by the rule parser itself
	(&RuleParser{Env: env()}).Parse(WithoutMetrics(context.Background()), Request{Text: "x", Now: now, TZID: "Europe/London"})
	if rec.Sum(t, "calendar.quickadd.requests", nil) != 2 {
		t.Fatal("silent parse was counted")
	}
	if strings.Contains(rec.Dump(t), "Sam") || strings.Contains(rec.Dump(t), "Alex") {
		t.Fatal("the sentence leaked into telemetry")
	}
}
