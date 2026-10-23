// Package telemetrytest installs in-memory OpenTelemetry providers for tests: recorded spans (after the
// production privacy scrubber), metrics and log records, plus a logger wired like the hub's.
package telemetrytest

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"calendar/server/internal/telemetry"
)

type Recorder struct {
	spans  *tracetest.SpanRecorder
	reader *sdkmetric.ManualReader
	logs   *logExporter
	// Logger is telemetry.NewLogger over Stdout.
	Logger *slog.Logger
	// Stdout receives the JSON log lines (the hub's stderr/stdout stream).
	Stdout *bytes.Buffer
}

type logExporter struct {
	mu   sync.Mutex
	recs []log.Record
}

func (e *logExporter) Export(_ context.Context, rs []log.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range rs {
		e.recs = append(e.recs, r.Clone())
	}
	return nil
}
func (e *logExporter) Shutdown(context.Context) error   { return nil }
func (e *logExporter) ForceFlush(context.Context) error { return nil }

// Install turns telemetry on with in-memory exporters until the test ends.
func Install(t testing.TB) *Recorder {
	t.Helper()
	r := &Recorder{spans: tracetest.NewSpanRecorder(), reader: sdkmetric.NewManualReader(), logs: &logExporter{}, Stdout: &bytes.Buffer{}}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(telemetry.NewScrubProcessor(r.spans)))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r.reader))
	lp := log.NewLoggerProvider(log.WithProcessor(log.NewSimpleProcessor(r.logs)))
	uninstall := telemetry.InstallProviders(tp, mp, lp)
	r.Logger = telemetry.NewLogger(r.Stdout)
	t.Cleanup(func() {
		uninstall()
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
		_ = lp.Shutdown(context.Background())
		otel.SetTracerProvider(sdktrace.NewTracerProvider()) // detach test providers from the globals
	})
	return r
}

// Spans are the ended spans, scrubbed exactly as they would be exported.
func (r *Recorder) Spans() []sdktrace.ReadOnlySpan { return r.spans.Ended() }

// Metrics collects every metric now.
func (r *Recorder) Metrics(t testing.TB) []metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	var out []metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		out = append(out, sm.Metrics...)
	}
	return out
}

// LogRecords are the records that reached the OTLP log pipeline.
func (r *Recorder) LogRecords() []log.Record {
	r.logs.mu.Lock()
	defer r.logs.mu.Unlock()
	return append([]log.Record(nil), r.logs.recs...)
}

// Dump renders everything exported (spans with attributes/events/status, metrics, log records) as one
// string, for substring assertions (privacy tests).
func (r *Recorder) Dump(t testing.TB) string {
	var b strings.Builder
	for _, s := range r.Spans() {
		fmt.Fprintf(&b, "SPAN %s kind=%v status=%v %s\n", s.Name(), s.SpanKind(), s.Status().Code, s.Status().Description)
		for _, a := range s.Attributes() {
			fmt.Fprintf(&b, "  attr %s=%s\n", a.Key, a.Value.Emit())
		}
		for _, e := range s.Events() {
			fmt.Fprintf(&b, "  event %s\n", e.Name)
			for _, a := range e.Attributes {
				fmt.Fprintf(&b, "    %s=%s\n", a.Key, a.Value.Emit())
			}
		}
	}
	for _, m := range r.Metrics(t) {
		fmt.Fprintf(&b, "METRIC %s %s\n%+v\n", m.Name, m.Description, m.Data)
	}
	for _, rec := range r.LogRecords() {
		fmt.Fprintf(&b, "LOG %s\n", rec.Body().String())
		rec.WalkAttributes(func(kv attribute.KeyValue) bool { fmt.Fprintf(&b, "  %s=%s\n", kv.Key, kv.Value.Emit()); return true })
	}
	return b.String()
}
