package telemetry

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestLogLevels(t *testing.T) {
	cases := []struct {
		level, otel  string
		stdout, otlp slog.Level
	}{
		{"", "", slog.LevelInfo, slog.LevelInfo},
		{"debug", "", slog.LevelDebug, slog.LevelDebug},
		{"warn", "", slog.LevelWarn, slog.LevelWarn},
		{"off", "", levelOff, levelOff},
		{"info", "off", slog.LevelInfo, levelOff},
		{"off", "error", levelOff, slog.LevelError},
		{"bogus", "", slog.LevelInfo, slog.LevelInfo},
	}
	for _, c := range cases {
		t.Setenv("CALENDAR_LOG_LEVEL", c.level)
		t.Setenv("CALENDAR_OTEL_LOG_LEVEL", c.otel)
		s, o := LogLevels()
		if s != c.stdout || o != c.otlp {
			t.Errorf("level=%q otel=%q: got %v/%v, want %v/%v", c.level, c.otel, s, o, c.stdout, c.otlp)
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
