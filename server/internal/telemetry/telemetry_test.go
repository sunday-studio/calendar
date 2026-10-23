package telemetry_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"calendar/server/internal/telemetry"
	"calendar/server/internal/telemetry/telemetrytest"
)

// app is a small hub-shaped handler chain: telemetry.HTTP > recover > Route(mux).
func app(rec *telemetrytest.Recorder) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/events/{id}", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /v1/missing/{id}", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", 404) })
	mux.HandleFunc("GET /v1/fail", func(w http.ResponseWriter, r *http.Request) {
		telemetry.RecordError(r.Context(), "events", fmt.Errorf("db write failed for a@x.test: %w", context.DeadlineExceeded))
		http.Error(w, "boom", 500)
	})
	mux.HandleFunc("GET /v1/silent500", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", 500) })
	mux.HandleFunc("GET /v1/panic", func(w http.ResponseWriter, r *http.Request) { panic("kaput for a@x.test") })
	mux.HandleFunc("GET /v1/stream", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("data: hi\n\n")) })
	recoverer := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					telemetry.RecordPanic(r.Context(), "http", v, []byte("goroutine 1 [running]:\nmain.f()"))
					rec.Logger.ErrorContext(r.Context(), "panic", "value", fmt.Sprint(v))
					http.Error(w, "internal", 500)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
	return telemetry.HTTP(recoverer(telemetry.Route(mux)))
}

func get(h http.Handler, path string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func attrs(kvs []attribute.KeyValue) map[string]string {
	m := map[string]string{}
	for _, kv := range kvs {
		m[string(kv.Key)] = kv.Value.Emit()
	}
	return m
}

func spanNamed(t *testing.T, rec *telemetrytest.Recorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, s := range rec.Spans() {
		if s.Name() == name {
			return s
		}
	}
	var names []string
	for _, s := range rec.Spans() {
		names = append(names, s.Name())
	}
	t.Fatalf("no span %q in %v", name, names)
	return nil
}

func metricNamed(t *testing.T, rec *telemetrytest.Recorder, name string) (metricdata.Metrics, bool) {
	for _, m := range rec.Metrics(t) {
		if m.Name == name {
			return m, true
		}
	}
	return metricdata.Metrics{}, false
}

func sumOf(t *testing.T, rec *telemetrytest.Recorder, name string, want map[string]string) int64 {
	t.Helper()
	m, ok := metricNamed(t, rec, name)
	if !ok {
		return 0
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("%s is %T", name, m.Data)
	}
	var n int64
	for _, dp := range sum.DataPoints {
		got := attrs(dp.Attributes.ToSlice())
		match := true
		for k, v := range want {
			if got[k] != v {
				match = false
			}
		}
		if match {
			n += dp.Value
		}
	}
	return n
}

func TestServerSpanNamedByRoutePattern(t *testing.T) {
	rec := telemetrytest.Install(t)
	h := app(rec)
	const parent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	w := get(h, "/v1/events/01HZX3K9Q8V7M2N4P6R8S0T1V2?q=secret-title&token=abc", "traceparent", parent)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	s := spanNamed(t, rec, "GET /v1/events/{id}")
	if s.Parent().TraceID().String() != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("inbound traceparent not continued: %v", s.Parent())
	}
	a := attrs(s.Attributes())
	if a["http.route"] != "/v1/events/{id}" || a["http.response.status_code"] != "200" || a["http.request.method"] != "GET" {
		t.Fatalf("%v", a)
	}
	for k, v := range a {
		if strings.Contains(v, "01HZX3K9") || strings.Contains(v, "secret-title") || strings.Contains(v, "abc") {
			t.Fatalf("raw path/query leaked in %s=%s", k, v)
		}
	}
	if _, ok := a["url.query"]; ok {
		t.Fatal("url.query kept")
	}
	if n := sumOf(t, rec, "calendar.http.requests", map[string]string{"http.route": "/v1/events/{id}", "calendar.http.status_class": "2xx"}); n != 1 {
		t.Fatalf("requests counter = %d", n)
	}
	m, ok := metricNamed(t, rec, "http.server.request.duration")
	if !ok {
		t.Fatal("no http.server.request.duration")
	}
	hist := m.Data.(metricdata.Histogram[float64])
	if got := attrs(hist.DataPoints[0].Attributes.ToSlice()); got["http.route"] != "/v1/events/{id}" {
		t.Fatalf("duration attrs %v", got)
	}
}

func TestServerErrorsAndPanics(t *testing.T) {
	rec := telemetrytest.Install(t)
	h := app(rec)

	if w := get(h, "/v1/fail"); w.Code != 500 {
		t.Fatal(w.Code)
	}
	s := spanNamed(t, rec, "GET /v1/fail")
	if s.Status().Code != codes.Error {
		t.Fatalf("status %v", s.Status())
	}
	var ex bool
	for _, e := range s.Events() {
		if e.Name == "exception" {
			ex = true
			a := attrs(e.Attributes)
			if a["calendar.error.kind"] != "timeout" || a["calendar.component"] != "events" || strings.Contains(a["exception.message"], "a@x.test") {
				t.Fatalf("%v", a)
			}
		}
	}
	if !ex {
		t.Fatal("no exception event")
	}
	// recorded once by the handler: the middleware must not count it a second time
	if n := sumOf(t, rec, "calendar.errors", map[string]string{"calendar.component": "events", "calendar.error.kind": "timeout"}); n != 1 {
		t.Fatalf("errors = %d", n)
	}
	if n := sumOf(t, rec, "calendar.errors", map[string]string{"calendar.component": "http"}); n != 0 {
		t.Fatalf("5xx double counted: %d", n)
	}
	// error log carries the trace id, on stdout and on the OTLP log pipeline
	var line map[string]any
	for _, l := range strings.Split(strings.TrimSpace(rec.Stdout.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil && m["msg"] == "error" {
			line = m
		}
	}
	if line == nil || line["trace_id"] != s.SpanContext().TraceID().String() || line["span_id"] != s.SpanContext().SpanID().String() || line["level"] != "ERROR" {
		t.Fatalf("stdout error log: %v", line)
	}
	if strings.Contains(rec.Stdout.String(), "a@x.test") {
		t.Fatal("e-mail in the recorded error log")
	}
	var otelErr bool
	for _, r := range rec.LogRecords() {
		if r.Body().String() == "error" && r.TraceID() == s.SpanContext().TraceID() {
			otelErr = true
		}
	}
	if !otelErr {
		t.Fatal("error log did not reach the OTLP log pipeline with its trace id")
	}

	// 5xx that nobody recorded: the middleware marks the span and counts it once
	get(h, "/v1/silent500")
	if s := spanNamed(t, rec, "GET /v1/silent500"); s.Status().Code != codes.Error {
		t.Fatal("silent 500 not an error span")
	}
	if n := sumOf(t, rec, "calendar.errors", map[string]string{"calendar.component": "http", "calendar.error.kind": "internal"}); n != 1 {
		t.Fatalf("silent 500 errors = %d", n)
	}

	// panic
	if w := get(h, "/v1/panic"); w.Code != 500 {
		t.Fatal(w.Code)
	}
	s = spanNamed(t, rec, "GET /v1/panic")
	if s.Status().Code != codes.Error {
		t.Fatal("panic span not an error")
	}
	var stack bool
	for _, e := range s.Events() {
		a := attrs(e.Attributes)
		if e.Name == "exception" && a["exception.type"] == "panic" && strings.Contains(a["exception.stacktrace"], "main.f()") && !strings.Contains(a["exception.message"], "a@x.test") {
			stack = true
		}
	}
	if !stack {
		t.Fatalf("no panic exception event with stack: %+v", s.Events())
	}
	if n := sumOf(t, rec, "calendar.errors", map[string]string{"calendar.component": "http", "calendar.error.kind": "panic"}); n != 1 {
		t.Fatalf("panic errors = %d", n)
	}
	if !strings.Contains(rec.Stdout.String(), `"msg":"panic"`) || !strings.Contains(rec.Stdout.String(), s.SpanContext().TraceID().String()) {
		t.Fatalf("panic log without trace id: %s", rec.Stdout.String())
	}
}

func TestClientErrors4xxAreNotErrors(t *testing.T) {
	rec := telemetrytest.Install(t)
	h := app(rec)
	if w := get(h, "/v1/missing/01HZX3K9Q8V7M2N4P6R8S0T1V2"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	get(h, "/no/such/route/with/an/id/01HZX3K9Q8V7M2N4P6R8S0T1V2")
	s := spanNamed(t, rec, "GET /v1/missing/{id}")
	if s.Status().Code == codes.Error {
		t.Fatal("4xx marked as error span")
	}
	for _, e := range s.Events() {
		if e.Name == "exception" {
			t.Fatal("exception event on a 4xx")
		}
	}
	if n := sumOf(t, rec, "calendar.http.requests", map[string]string{"http.route": "/v1/missing/{id}", "calendar.http.status_class": "4xx"}); n != 1 {
		t.Fatalf("4xx not counted: %d", n)
	}
	if n := sumOf(t, rec, "calendar.errors", nil); n != 0 {
		t.Fatalf("4xx counted as errors: %d", n)
	}
	// scanner traffic collapses into one low-cardinality route
	spanNamed(t, rec, "GET unmatched")
}

func TestLongLivedRequestsGetNoLifetimeSpan(t *testing.T) {
	rec := telemetrytest.Install(t)
	h := app(rec)
	get(h, "/v1/stream")
	s := spanNamed(t, rec, "GET /v1/stream")
	if attrs(s.Attributes())["calendar.long_lived"] != "true" {
		t.Fatalf("%v", attrs(s.Attributes()))
	}
	if _, ok := metricNamed(t, rec, "calendar.http.long_lived.duration"); !ok {
		t.Fatal("no long-lived duration histogram")
	}
	if m, ok := metricNamed(t, rec, "http.server.request.duration"); ok {
		t.Fatalf("stream request polluted the request-duration histogram: %+v", m)
	}
	if n := sumOf(t, rec, "calendar.http.long_lived.active", map[string]string{"http.route": "/v1/stream"}); n != 0 {
		t.Fatalf("active connections leaked: %d", n)
	}
}

func TestDisabledModeIsInert(t *testing.T) {
	for _, k := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "OTEL_SDK_DISABLED"} {
		t.Setenv(k, "")
	}
	cfg := telemetry.ConfigFromEnv("test")
	if cfg.Enabled() || cfg.Summary() != "telemetry disabled" {
		t.Fatalf("%+v %s", cfg, cfg.Summary())
	}
	runtime.GC()
	before := runtime.NumGoroutine()
	shutdown, err := telemetry.Setup(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	// exercise every helper and a request
	ctx, span := telemetry.StartSpan(context.Background(), "x", telemetry.Provider("google"))
	telemetry.RecordError(ctx, "sync", errors.New("e"))
	telemetry.Counter("c", "d").Add(ctx, 1)
	telemetry.Histogram("h", "d", "s").Record(ctx, 1)
	telemetry.Gauge("g", "d", "1", func(context.Context) float64 { return 1 })
	span.End()
	if telemetry.Enabled() {
		t.Fatal("enabled")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /h", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	if w := get(telemetry.HTTP(telemetry.Route(mux)), "/h"); w.Code != 200 || w.Body.String() != "ok" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines started in disabled mode: %d -> %d", before, after)
	}
}

func TestUnreachableCollectorNeverBlocks(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1") // nothing listens
	t.Setenv("OTEL_SDK_DISABLED", "")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "100")
	cfg := telemetry.ConfigFromEnv("test")
	if !cfg.Enabled() {
		t.Fatal("not enabled")
	}
	start := time.Now()
	shutdown, err := telemetry.Setup(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Setup blocked for %v", d)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /h", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	h := telemetry.HTTP(telemetry.Route(mux))
	start = time.Now()
	for i := 0; i < 200; i++ {
		if w := get(h, "/h"); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("requests slowed by an unreachable collector: %v", d)
	}
	time.Sleep(300 * time.Millisecond) // let a metric export attempt fail in the background
	start = time.Now()
	_ = shutdown(context.Background()) // may report the failed flush; must return within the 5 s budget
	if d := time.Since(start); d > 6*time.Second {
		t.Fatalf("shutdown took %v", d)
	}
}

func TestInFlightGaugeReturnsToZero(t *testing.T) {
	rec := telemetrytest.Install(t)
	h := app(rec)
	get(h, "/v1/events/01HZX3K9Q8V7M2N4P6R8S0T1V2")
	get(h, "/v1/panic")
	m, ok := metricNamed(t, rec, "calendar.http.in_flight")
	if !ok {
		t.Fatal("no in-flight metric")
	}
	for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
		if dp.Value != 0 {
			t.Fatalf("in flight = %d", dp.Value)
		}
	}
}
