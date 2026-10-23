package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDoctorTelemetryLine(t *testing.T) {
	for _, k := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "OTEL_SDK_DISABLED"} {
		t.Setenv(k, "")
	}
	if d, w := doctorTelemetry(context.Background()); w || !strings.HasPrefix(d, "disabled") {
		t.Fatal(d, w)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer topsecret")
	d, w := doctorTelemetry(context.Background())
	if !w || !strings.Contains(d, "127.0.0.1:1") || strings.Contains(d, "topsecret") || !strings.Contains(d, "not reachable") {
		t.Fatal(d, w)
	}
}

func TestStartTelemetryDisabledLogsOneLine(t *testing.T) {
	for _, k := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "OTEL_SDK_DISABLED"} {
		t.Setenv(k, "")
	}
	var buf bytes.Buffer
	_, stop := startTelemetry(context.Background(), &buf)
	stop()
	if !strings.Contains(buf.String(), `"msg":"telemetry disabled"`) || strings.Count(buf.String(), "\n") != 1 {
		t.Fatal(buf.String())
	}
}
