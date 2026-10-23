package authn

import (
	"context"
	"strings"
	"testing"

	"calendar/server/internal/telemetry/telemetrytest"
)

type A = telemetrytest.Attrs

func TestTelemetryAuthOutcomesNeverCarrySecrets(t *testing.T) {
	rec := telemetrytest.Install(t)
	ctx := context.Background()
	s := store(t)
	id, secret, err := s.Create(ctx, "my private phone name", "device", Write)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, secret); err != nil {
		t.Fatal(err)
	}
	s.Verify(ctx, "cal_nottherealthing")
	s.Verify(ctx, "garbage")
	if err := s.Revoke(ctx, id); err != nil {
		t.Fatal(err)
	}
	s.Verify(ctx, secret)
	RecordScopeDenied(ctx, &Principal{Kind: "script"})
	RecordThrottled(ctx)
	for _, c := range []struct {
		a A
		n float64
	}{
		{A{"calendar.outcome": "ok", "calendar.token_kind": "device"}, 1},
		{A{"calendar.outcome": "invalid"}, 2},
		{A{"calendar.outcome": "revoked", "calendar.token_kind": "device"}, 1},
		{A{"calendar.outcome": "scope_denied", "calendar.token_kind": "script"}, 1},
	} {
		if got := rec.Sum(t, "calendar.auth.requests", c.a); got != c.n {
			t.Errorf("auth.requests %v = %v want %v\n%s", c.a, got, c.n, rec.Dump(t))
		}
	}
	if rec.Sum(t, "calendar.auth.throttled", nil) != 1 || rec.Sum(t, "calendar.auth.tokens", A{"calendar.action": "created"}) != 1 || rec.Sum(t, "calendar.auth.tokens", A{"calendar.action": "revoked"}) != 1 {
		t.Fatal("token lifecycle counters")
	}
	d := rec.Dump(t) + rec.Stdout.String()
	for _, bad := range []string{secret, secret[:10], "my private phone name", "nottherealthing"} {
		if strings.Contains(d, bad) {
			t.Fatalf("%q leaked", bad)
		}
	}
}
