package telemetry

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestLogLevels(t *testing.T) {
	all := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	allowed := func(s levelSet) string {
		var out string
		for _, l := range all {
			if s.allows(l) {
				out += map[slog.Level]string{slog.LevelDebug: "D", slog.LevelInfo: "I", slog.LevelWarn: "W", slog.LevelError: "E"}[l]
			}
		}
		return out
	}
	cases := []struct{ level, otel, stdout, otlp string }{
		{"", "", "IWE", "IWE"},
		{"debug", "", "DIWE", "DIWE"},
		{"warn", "", "WE", "WE"},
		{"off", "", "", ""},
		{"info", "off", "IWE", ""},
		{"off", "error", "", "E"},
		{"bogus", "", "IWE", "IWE"},
		{"", "info,error", "IWE", "IE"}, // a list is exactly those levels
		{"info,error", "", "IE", "IE"},
		{"debug, warn", "", "DW", "DW"},
		{"error,error", "", "E", "E"},
		{"info,bogus", "", "I", "I"},     // unknown entries are ignored
		{"bogus,nope", "", "IWE", "IWE"}, // nothing valid: default
		{"off,error", "", "E", "E"},      // off has no meaning inside a list
	}
	for _, c := range cases {
		t.Setenv("CALENDAR_LOG_LEVEL", c.level)
		t.Setenv("CALENDAR_OTEL_LOG_LEVEL", c.otel)
		s, o := LogLevels()
		if allowed(s) != c.stdout || allowed(o) != c.otlp {
			t.Errorf("level=%q otel=%q: got %q/%q, want %q/%q", c.level, c.otel, allowed(s), allowed(o), c.stdout, c.otlp)
		}
	}
}

func TestNewLoggerHonoursLevel(t *testing.T) {
	var buf bytes.Buffer
	t.Setenv("CALENDAR_LOG_LEVEL", "error")
	l := NewLogger(&buf)
	l.Info("hidden")
	l.Error("shown")
	if out := buf.String(); strings.Contains(out, "hidden") || !strings.Contains(out, "shown") {
		t.Fatalf("level error should keep only errors, got %q", out)
	}

	buf.Reset()
	t.Setenv("CALENDAR_LOG_LEVEL", "info,error")
	l = NewLogger(&buf)
	l.Debug("d")
	l.Info("i-shown")
	l.Warn("w-hidden")
	l.Error("e-shown")
	if out := buf.String(); strings.Contains(out, `"d"`) || strings.Contains(out, "w-hidden") || !strings.Contains(out, "i-shown") || !strings.Contains(out, "e-shown") {
		t.Fatalf("list info,error should keep exactly info and error, got %q", out)
	}

	buf.Reset()
	t.Setenv("CALENDAR_LOG_LEVEL", "off")
	l = NewLogger(&buf)
	l.Error("nothing")
	if buf.Len() != 0 {
		t.Fatalf("level off should log nothing, got %q", buf.String())
	}
}

func TestApplyToken(t *testing.T) {
	cases := []struct{ token, headers, want string }{
		{"", "", ""},
		{"abc", "", "Authorization=Bearer%20abc"},
		{"abc", "x-key=1", "x-key=1,Authorization=Bearer%20abc"},
		{"abc", "authorization=Basic%20zzz", "authorization=Basic%20zzz"}, // explicit header wins
		{"a b", "", "Authorization=Bearer%20a%20b"},
	}
	for _, c := range cases {
		t.Setenv("CALENDAR_OTEL_TOKEN", c.token)
		t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", c.headers)
		applyToken()
		if got := os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"); got != c.want {
			t.Errorf("token=%q headers=%q: got %q, want %q", c.token, c.headers, got, c.want)
		}
	}
}

func TestTokenCountsAsHeader(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://x.example")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
	t.Setenv("CALENDAR_OTEL_TOKEN", "abc")
	if n := ConfigFromEnv("t").Headers; n != 1 {
		t.Fatalf("headers = %d, want 1", n)
	}
}
