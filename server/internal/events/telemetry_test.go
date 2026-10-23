package events

import (
	"context"
	"errors"
	"testing"

	"calendar/server/internal/db"
	"calendar/server/internal/provider"
	"calendar/server/internal/telemetry/telemetrytest"
)

type A = telemetrytest.Attrs

func TestTelemetryWriteOutcomes(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newEnv(t)
	e.sync()

	// ok
	if _, err := e.svc.Create(ctx(), timedIn(e.cal1, "Lunch", "2026-10-16T13:00:00", 45), Opts{}); err != nil {
		t.Fatal(err)
	}
	if rec.Sum(t, "calendar.writes", A{"calendar.op": "create", "calendar.outcome": "ok", "calendar.provider": "google"}) != 1 {
		t.Fatalf("create ok missing:\n%s", rec.Dump(t))
	}
	sp := rec.SpansNamed("events.create")
	if len(sp) != 1 || telemetrytest.SpanAttr(sp[0], "calendar.provider") != "google" || telemetrytest.SpanAttr(sp[0], "calendar.outcome") != "ok" {
		t.Fatalf("events.create span: %v", rec.SpanNames())
	}
	if rec.HistCount(t, "calendar.write.duration", A{"calendar.op": "create"}) != 1 {
		t.Fatal("write.duration missing")
	}

	// validation
	_, err := e.svc.Create(ctx(), CreateIn{}, Opts{})
	wantKind(t, err, KValidation)
	if rec.Sum(t, "calendar.writes", A{"calendar.op": "create", "calendar.outcome": "validation"}) != 1 {
		t.Fatal("validation outcome missing")
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatal("validation is not an error")
	}

	// conflict + precondition
	ev := e.seedSingle("s1", "Mine")
	e.f.Edit("primary", "s1", func(r map[string]any) { r["summary"] = "Edited elsewhere" })
	cur := ev.Version
	_, err = e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("Mine 2")}, Opts{IfMatch: &cur})
	wantKind(t, err, KConflict)
	if rec.Sum(t, "calendar.writes", A{"calendar.op": "patch", "calendar.outcome": "conflict"}) != 1 {
		t.Fatal("conflict outcome missing")
	}
	stale := cur
	ev2, _ := e.svc.Store.Event(ctx(), ev.ID)
	_, err = e.svc.Patch(ctx(), ev.ID, "", PatchIn{Title: str("x")}, Opts{IfMatch: &stale})
	if ev2.Version != stale {
		wantKind(t, err, KPrecondition)
		if rec.Sum(t, "calendar.writes", A{"calendar.outcome": "precondition_failed"}) != 1 {
			t.Fatal("precondition outcome missing")
		}
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatalf("conflicts are outcomes, not errors:\n%s", rec.Dump(t))
	}

	// provider error -> outcome + RecordError(kind=provider)
	ev3 := e.seedSingle("s3", "Third")
	e.f.FailNext("PATCH", "/events/s3", 403, 1, "")
	_, err = e.svc.Patch(ctx(), ev3.ID, "", PatchIn{Title: str("x")}, Opts{})
	wantKind(t, err, KProvider)
	if rec.Sum(t, "calendar.writes", A{"calendar.op": "patch", "calendar.outcome": "provider_error"}) != 1 {
		t.Fatal("provider_error outcome missing")
	}
	if rec.Sum(t, "calendar.errors", A{"calendar.component": "events", "calendar.error.kind": "provider"}) != 1 {
		t.Fatalf("no events/provider error:\n%s", rec.Dump(t))
	}

	// rate limited is an outcome only
	e.f.FailNext("PATCH", "/events/s3", 429, 9, "120")
	_, err = e.svc.Patch(ctx(), ev3.ID, "", PatchIn{Title: str("x")}, Opts{})
	wantKind(t, err, KRateLimited)
	if rec.Sum(t, "calendar.writes", A{"calendar.outcome": "rate_limited"}) != 1 {
		t.Fatal("rate_limited outcome missing")
	}
	if rec.Sum(t, "calendar.errors", nil) != 1 {
		t.Fatal("rate limit counted as an error")
	}

	// replay (called by the HTTP layer)
	RecordReplay(ctx(), "create")
	if rec.Sum(t, "calendar.idempotency.replays", nil) != 1 || rec.Sum(t, "calendar.writes", A{"calendar.outcome": "replayed"}) != 1 {
		t.Fatal("replay not counted")
	}
}

type failMat struct{}

func (failMat) Rebuild(context.Context, *db.Tx, []string) error {
	return errors.New("rebuild exploded")
}

func TestTelemetryProviderOKMirrorFailedIsConsistencyError(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newEnv(t)
	e.sync()
	e.mgr.Manager.Mat = failMat{} // the provider accepts the write, the mirror cannot take it
	_, err := e.svc.Create(ctx(), timedIn(e.cal1, "Lunch", "2026-10-16T13:00:00", 45), Opts{})
	if err == nil {
		t.Fatal("expected a failure")
	}
	if rec.Sum(t, "calendar.errors", A{"calendar.component": "events", "calendar.error.kind": "consistency"}) != 1 {
		t.Fatalf("no consistency error:\n%s", rec.Dump(t))
	}
	if rec.Sum(t, "calendar.writes.provider_rollbacks", nil) != 1 {
		t.Fatal("provider_rollbacks not counted")
	}
	if rec.Sum(t, "calendar.writes", A{"calendar.op": "create", "calendar.outcome": "provider_error"}) != 1 {
		t.Fatal("outcome")
	}
	_ = provider.ErrNotFound
}
