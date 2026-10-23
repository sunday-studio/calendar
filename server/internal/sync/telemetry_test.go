package sync

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
	"calendar/server/internal/telemetry/telemetrytest"
)

type A = telemetrytest.Attrs

func TestTelemetrySuccessfulRunSpanTreeAndMetrics(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newTEnv(t)
	seedCal(e)
	id := e.addAccount("secret-person@x.com", "rt1")
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	runs := rec.SpansNamed("sync.run")
	if len(runs) != 1 {
		t.Fatalf("sync.run spans: %v", rec.SpanNames())
	}
	run := runs[0]
	if telemetrytest.SpanAttr(run, "calendar.account.id") != id || telemetrytest.SpanAttr(run, "calendar.provider") != "google" ||
		telemetrytest.SpanAttr(run, "calendar.sync.trigger") != "manual" {
		t.Fatalf("run attrs: %v", run.Attributes())
	}
	cals := rec.SpansNamed("sync.calendar")
	if len(cals) == 0 {
		t.Fatal("no sync.calendar spans")
	}
	for _, c := range cals {
		if c.Parent().SpanID() != run.SpanContext().SpanID() {
			t.Fatal("sync.calendar must be a child of sync.run")
		}
	}
	if len(rec.SpansNamed("db.write")) == 0 {
		t.Fatal("no db.write spans")
	}
	if got := rec.Sum(t, "calendar.sync.runs", A{"calendar.outcome": "ok", "calendar.provider": "google", "calendar.sync.trigger": "manual"}); got != 1 {
		t.Fatalf("sync.runs ok = %v", got)
	}
	if rec.HistCount(t, "calendar.sync.duration", A{"calendar.provider": "google"}) != 1 {
		t.Fatal("sync.duration not recorded")
	}
	if rec.Sum(t, "calendar.sync.events", A{"calendar.change": "added", "calendar.provider": "google"}) < 1 {
		t.Fatal("sync.events added missing")
	}
	if rec.Sum(t, "calendar.sync.pages", A{"calendar.provider": "google"}) < 1 {
		t.Fatal("sync.pages missing")
	}
	if rec.Sum(t, "calendar.provider.requests", A{"calendar.operation": "list_events", "calendar.outcome": "ok", "calendar.status_class": "2xx"}) < 1 {
		t.Fatal("provider.requests list_events missing")
	}
	if rec.Sum(t, "calendar.provider.requests", A{"calendar.operation": "token_refresh", "calendar.outcome": "ok"}) < 1 {
		t.Fatal("token refresh not counted")
	}
	if rec.HistCount(t, "calendar.db.write.duration", nil) == 0 {
		t.Fatal("db.write.duration missing")
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatalf("a clean run recorded errors:\n%s", rec.Dump(t))
	}
}

func TestTelemetryCursorReset410(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newTEnv(t)
	seedCal(e)
	id := e.addAccount("me@x.com", "rt1")
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	e.fake.mu.Lock()
	e.fake.expireAll = true
	e.fake.mu.Unlock()
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	if rec.Sum(t, "calendar.sync.cursor_resets", A{"calendar.reason": "410", "calendar.provider": "google"}) < 1 {
		t.Fatal("cursor reset not counted")
	}
	found := false
	for _, s := range rec.SpansNamed("sync.calendar") {
		found = found || telemetrytest.HasEvent(s, "sync.cursor_reset")
	}
	if !found {
		t.Fatal("no sync.cursor_reset span event")
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatal("a 410 recovery is not an error")
	}
}

func TestTelemetryNeedsReauthIsRecordedAsReauthError(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newTEnv(t)
	seedCal(e)
	a := e.addAccount("a@x.com", "rt-a")
	e.fake.mu.Lock()
	e.fake.revoked["rt-a"] = true
	e.fake.mu.Unlock()
	if err := e.sync(a); !errors.Is(err, provider.ErrNeedsReauth) {
		t.Fatal(err)
	}
	if rec.Sum(t, "calendar.sync.runs", A{"calendar.outcome": "needs_reauth"}) != 1 {
		t.Fatal("outcome needs_reauth missing")
	}
	if rec.Sum(t, "calendar.errors", A{"calendar.error.kind": "reauth"}) < 1 {
		t.Fatalf("no kind=reauth error:\n%s", rec.Dump(t))
	}
	if rec.Sum(t, "calendar.provider.token_refresh", A{"calendar.outcome": "invalid_grant"}) < 1 {
		t.Fatal("invalid_grant refresh not counted")
	}
	run := rec.SpansNamed("sync.run")[0]
	if !telemetrytest.HasEvent(run, "account.needs_reauth") || !telemetrytest.HasEvent(run, "exception") {
		t.Fatalf("run span events: %v", run.Events())
	}
	if rec.Sum(t, "calendar.account.events", A{"calendar.event": "needs_reauth"}) != 1 {
		t.Fatal("lifecycle counter missing")
	}
	if !strings.Contains(rec.Stdout.String(), `"account needs_reauth"`) && !strings.Contains(rec.Stdout.String(), "account needs_reauth") {
		t.Fatalf("lifecycle log line missing:\n%s", rec.Stdout.String())
	}
}

func TestTelemetryRateLimitIsAnOutcomeNotAnError(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newTEnv(t)
	seedCal(e)
	id := e.addAccount("me@x.com", "rt1")
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	e.fake.mu.Lock()
	e.fake.failN, e.fake.failCode, e.fake.retryAfter = 1000, 429, "100"
	e.fake.mu.Unlock()
	if err := e.sync(id); err == nil {
		t.Fatal("expected a rate limit error")
	}
	if rec.Sum(t, "calendar.sync.runs", A{"calendar.outcome": "rate_limited"}) != 1 {
		t.Fatalf("outcome rate_limited missing:\n%s", rec.Dump(t))
	}
	if rec.Sum(t, "calendar.provider.rate_limited", A{"calendar.provider": "google"}) < 1 {
		t.Fatal("provider.rate_limited missing")
	}
	if rec.Sum(t, "calendar.sync.backoff", A{"calendar.provider": "google"}) != 1 {
		t.Fatal("backoff not counted")
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatal("rate limiting is expected and must not count as an error")
	}
	// the backoff gauge reports the scheduled delay
	rec2 := rec.Gauge // keep the recorder alive for the gauge check below
	_ = rec2
	off := e.mgr.RegisterGauges()
	defer off()
	if g := rec.Gauge(t, "calendar.sync.backoff_seconds", nil); len(g) != 1 || g[0] < 60 {
		t.Fatalf("backoff_seconds %v", g)
	}
}

func TestTelemetryGaugesAfterSeededSync(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newTEnv(t)
	seedCal(e)
	id := e.addAccount("me@x.com", "rt1")
	if err := e.sync(id); err != nil {
		t.Fatal(err)
	}
	off := e.mgr.RegisterGauges()
	defer off()
	off2 := e.d.RegisterGauges()
	defer off2()
	age := rec.Gauge(t, "calendar.account.last_sync_age_seconds", A{"calendar.account.id": id})
	if len(age) != 1 || age[0] < 0 || age[0] > 30 {
		t.Fatalf("last_sync_age %v", age)
	}
	if st := rec.Gauge(t, "calendar.account.status", A{"calendar.account.id": id, "calendar.status": "active"}); len(st) != 1 || st[0] != 1 {
		t.Fatalf("status gauge %v / %v", st, rec.GaugeAttrs(t, "calendar.account.status"))
	}
	if n := rec.Gauge(t, "calendar.accounts", A{"calendar.provider": "google", "calendar.status": "active"}); len(n) != 1 || n[0] != 1 {
		t.Fatalf("accounts gauge %v", n)
	}
	if v := rec.Gauge(t, "calendar.db.rev", nil); len(v) != 1 || v[0] < 1 {
		t.Fatalf("db.rev %v", v)
	}
	if v := rec.Gauge(t, "calendar.db.size_bytes", nil); len(v) != 1 || v[0] <= 0 {
		t.Fatalf("db.size_bytes %v", v)
	}
	if v := rec.Gauge(t, "calendar.events", nil); len(v) != 1 || v[0] < 1 {
		t.Fatalf("events %v", v)
	}
	if v := rec.Gauge(t, "calendar.occurrences", nil); len(v) != 1 || v[0] < 1 {
		t.Fatalf("occurrences %v", v)
	}
	// metric attributes never carry anything but the account ULID on these two bounded per-account gauges
	for _, name := range []string{"calendar.account.status", "calendar.account.last_sync_age_seconds"} {
		for _, at := range rec.GaugeAttrs(t, name) {
			if strings.Contains(at["calendar.account.id"], "@") {
				t.Fatalf("%s leaks %v", name, at)
			}
		}
	}
}

// panicProv panics in Discover, to prove a bug in one pass cannot take the hub down.
type panicProv struct{ provider.Provider }

func (panicProv) Discover(context.Context, domain.Account) ([]provider.RemoteCalendar, error) {
	panic("boom with user text: Dinner with Alice")
}

func TestTelemetryWorkerPanicRecoveredCountedAndRunContinues(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newTEnv(t)
	seedCal(e)
	e.mgr.Providers["google"] = panicProv{e.mgr.Providers["google"]}
	e.addAccount("me@x.com", "rt1")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.mgr.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for rec.Sum(t, "calendar.goroutine.panics", nil) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("Run exited after a worker panic")
	default:
	}
	if got := rec.Sum(t, "calendar.goroutine.panics", A{"calendar.goroutine": "sync.pass"}); got < 2 {
		t.Fatalf("panics counted %v (a worker must keep retrying)", got)
	}
	if rec.Sum(t, "calendar.errors", A{"calendar.error.kind": "panic"}) < 2 {
		t.Fatal("panic not recorded as an error")
	}
	if strings.Contains(rec.Dump(t), "Dinner with Alice") {
		d := rec.Dump(t)
		i := strings.Index(d, "Dinner with Alice")
		t.Fatalf("panic value (user text) leaked into telemetry: ...%s...", d[max(0, i-300):i+100])
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}
