package telemetry_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"

	"calendar/server/internal/telemetry"
	"calendar/server/internal/telemetry/telemetrytest"
)

func TestSanitize(t *testing.T) {
	in := "provider said: user alice.smith+cal@example.com token ya29.A0ARrdaM-secret Bearer abc.def.ghi " +
		"GET https://x.test/cal/v3/calendars/alice@example.com/events?access_token=ZZZ&q=Dentist refresh_token=1//0gAbCdEf0123456789abcdefgh cal_AbC123xyz"
	got := telemetry.Sanitize(in)
	for _, bad := range []string{"alice", "example.com", "ya29", "abc.def.ghi", "access_token", "ZZZ", "Dentist", "1//0g", "cal_AbC"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q survived in %q", bad, got)
		}
	}
	if long := telemetry.Sanitize(strings.Repeat("é", 500)); len(long) > 210 || !strings.HasSuffix(long, "...") {
		t.Errorf("not truncated: %d", len(long))
	}
	if telemetry.Sanitize("db is locked") != "db is locked" {
		t.Error("plain message altered")
	}
}

func TestTemplatePath(t *testing.T) {
	for in, want := range map[string]string{
		"/calendar/v3/calendars/alice@example.com/events/abc123def/instances": "/calendar/v3/calendars/:id/events/:id/instances",
		"/calendar/v3/calendars/primary/events":                               "/calendar/v3/calendars/primary/events",
		"/calendar/v3/users/me/calendarList":                                  "/calendar/v3/users/me/calendarList",
		"/3/device/0123456789abcdef":                                          "/3/device/:id",
		"/secret-feed-token/calendar.ics":                                     "/:id/:id",
		"/":                                                                   "/",
	} {
		if got := telemetry.TemplatePath(in); got != want {
			t.Errorf("%s -> %s want %s", in, got, want)
		}
	}
	if telemetry.TemplatePath(telemetry.TemplatePath("/calendar/v3/calendars/x/events")) != "/calendar/v3/calendars/:id/events" {
		t.Error("not idempotent")
	}
}

func TestConfigFromEnv(t *testing.T) {
	clear := func() {
		for _, k := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
			"OTEL_SDK_DISABLED", "OTEL_TRACES_EXPORTER", "OTEL_METRICS_EXPORTER", "OTEL_LOGS_EXPORTER", "OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_EXPORTER_OTLP_HEADERS", "OTEL_SERVICE_NAME", "OTEL_TRACES_SAMPLER", "OTEL_TRACES_SAMPLER_ARG"} {
			t.Setenv(k, "")
		}
	}
	clear()
	if c := telemetry.ConfigFromEnv("v"); c.Enabled() || c.ServiceName != "calendard" || c.Sampler != "parentbased_always_on" || !strings.Contains(c.Describe(), "disabled") {
		t.Fatalf("%+v", c)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://user:pw@otlp-gateway.grafana.net/otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Basic c2VjcmV0,x-other=1")
	t.Setenv("OTEL_TRACES_SAMPLER", "parentbased_traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	c := telemetry.ConfigFromEnv("v")
	if !c.Enabled() || c.EndpointHost() != "otlp-gateway.grafana.net" || c.Protocol() != telemetry.ProtoHTTP || len(c.Signals()) != 3 {
		t.Fatalf("%+v", c)
	}
	for _, s := range []string{c.Describe(), c.Summary()} {
		for _, bad := range []string{"pw", "c2VjcmV0", "Basic", "user", "/otlp"} {
			if strings.Contains(s, bad) {
				t.Errorf("%q leaked in %q", bad, s)
			}
		}
	}
	if c.Summary() != "telemetry enabled endpoint=otlp-gateway.grafana.net protocol=http/protobuf signals=traces,metrics,logs" {
		t.Fatal(c.Summary())
	}
	if !strings.Contains(c.Describe(), "parentbased_traceidratio(0.25)") || !strings.Contains(c.Describe(), "2 header(s)") {
		t.Fatal(c.Describe())
	}
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	if c := telemetry.ConfigFromEnv("v"); c.Logs.Enabled || !c.Traces.Enabled || c.Protocol() != telemetry.ProtoGRPC {
		t.Fatalf("%+v", c)
	}
	t.Setenv("OTEL_SDK_DISABLED", "true")
	if c := telemetry.ConfigFromEnv("v"); c.Enabled() {
		t.Fatal("OTEL_SDK_DISABLED ignored")
	}
	clear()
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://c:4318/v1/traces")
	if c := telemetry.ConfigFromEnv("v"); !c.Traces.Enabled || c.Metrics.Enabled || c.Logs.Enabled {
		t.Fatalf("per-signal endpoint: %+v", c)
	}
}

func TestProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(405) }))
	defer srv.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	if err := telemetry.ConfigFromEnv("v").Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	err := telemetry.ConfigFromEnv("v").Probe(context.Background())
	if err == nil || strings.Contains(err.Error(), "127.0.0.1:1/") {
		t.Fatalf("%v", err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+srv.Listener.Addr().String())
	if err := telemetry.ConfigFromEnv("v").Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAttributeHelpersAndGuard(t *testing.T) {
	if got := telemetry.AccountID("01HZX3K9Q8V7M2N4P6R8S0T1V2"); got.Value.AsString() != "01HZX3K9Q8V7M2N4P6R8S0T1V2" {
		t.Fatal(got)
	}
	if got := telemetry.AccountID("alice@example.com"); got.Value.AsString() != "invalid" {
		t.Fatal("e-mail accepted as account id")
	}
	if telemetry.Provider("Google Calendar!").Value.AsString() != "other" || telemetry.Provider("google").Value.AsString() != "google" {
		t.Fatal("provider guard")
	}
	g := telemetry.NewGuard(3)
	for _, v := range []string{"a", "b", "c", "d", "a"} {
		want := v
		if v == "d" {
			want = "other"
		}
		if g.Allow(v) != want {
			t.Fatalf("%s", v)
		}
	}
}

type typedErr struct{}

func (typedErr) Error() string { return "typed" }

func TestKind(t *testing.T) {
	telemetry.RegisterErrorKind(telemetry.KindPush, func(err error) bool { return errors.As(err, new(typedErr)) })
	for err, want := range map[error]string{
		fmt.Errorf("x: %w", context.DeadlineExceeded): "timeout",
		context.Canceled:                                  "canceled",
		fmt.Errorf("w: %w", typedErr{}):                   "push",
		&net.OpError{Op: "dial", Err: errors.New("down")}: "network",
		sql.ErrNoRows:                                     "internal", // db kind is registered by main, not here
		errors.New("boom"):                                "internal",
	} {
		if got := telemetry.Kind(err); got != want {
			t.Errorf("%v -> %s want %s", err, got, want)
		}
	}
}

func TestLoggerAddsTraceIDsAndScrubsBridge(t *testing.T) {
	rec := telemetrytest.Install(t)
	ctx, span := telemetry.StartSpan(context.Background(), "op")
	rec.Logger.InfoContext(ctx, "hello from alice@example.com", "path", "/v1/events/01HZ", "title", "Dentist", "err", errors.New("token=abc123 failed"), "n", 3,
		slog.Group("inner", "email", "x@y.zz", "ok", 1))
	rec.Logger.Info("no span here")
	span.End()
	lines := strings.Split(strings.TrimSpace(rec.Stdout.String()), "\n")
	var first map[string]any
	json.Unmarshal([]byte(lines[0]), &first)
	if first["trace_id"] != span.SpanContext().TraceID().String() || first["span_id"] != span.SpanContext().SpanID().String() {
		t.Fatalf("stdout: %v", first)
	}
	if first["title"] != "Dentist" || first["path"] != "/v1/events/01HZ" { // stdout format is untouched
		t.Fatalf("stdout altered: %v", first)
	}
	if strings.Contains(lines[1], "trace_id") {
		t.Fatal("trace id without a span")
	}
	var found bool
	for _, r := range rec.LogRecords() {
		if strings.HasPrefix(r.Body().String(), "hello") {
			found = true
			if strings.Contains(r.Body().String(), "alice") {
				t.Fatal("e-mail in OTLP log body")
			}
			r.WalkAttributes(func(kv attribute.KeyValue) bool {
				switch string(kv.Key) {
				case "path", "title", "secret_token", "inner.email":
					t.Fatalf("denied key %s exported", kv.Key)
				case "err":
					if strings.Contains(kv.Value.Emit(), "abc123") {
						t.Fatal("secret in err")
					}
				}
				return true
			})
			if r.TraceID() != span.SpanContext().TraceID() {
				t.Fatal("OTLP log without trace id")
			}
		}
	}
	if !found {
		t.Fatal("record not bridged")
	}
}
