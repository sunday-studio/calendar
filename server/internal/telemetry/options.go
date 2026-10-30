package telemetry

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

// levelOff is above every real slog level, so a handler with this minimum drops all records.
const levelOff = slog.Level(1 << 20)

// parseLevel reads debug|info|warn|error|off (case-insensitive). ok is false for an empty or unknown value.
func parseLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	case "off", "none":
		return levelOff, true
	}
	return slog.LevelInfo, false
}

// LogLevels returns the minimum level for the stdout log and for the OTLP log export.
// CALENDAR_LOG_LEVEL sets both (default info); CALENDAR_OTEL_LOG_LEVEL overrides the OTLP side only.
// Each accepts debug, info, warn, error or off. An unknown value falls back to the default.
func LogLevels() (stdout, otlp slog.Level) {
	stdout, _ = parseLevel(os.Getenv("CALENDAR_LOG_LEVEL"))
	otlp = stdout
	if l, ok := parseLevel(os.Getenv("CALENDAR_OTEL_LOG_LEVEL")); ok {
		otlp = l
	}
	return stdout, otlp
}

// levelHandler drops records below min before they reach next.
type levelHandler struct {
	min  slog.Level
	next slog.Handler
}

func (h levelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.min && h.next.Enabled(ctx, l)
}
func (h levelHandler) Handle(ctx context.Context, r slog.Record) error { return h.next.Handle(ctx, r) }
func (h levelHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return levelHandler{h.min, h.next.WithAttrs(a)}
}
func (h levelHandler) WithGroup(n string) slog.Handler {
	return levelHandler{h.min, h.next.WithGroup(n)}
}

// tokenHeader is the header CALENDAR_OTEL_TOKEN produces.
const tokenHeader = "Authorization"

// hasAuthHeader reports whether an OTEL_EXPORTER_OTLP_HEADERS value already sets Authorization.
func hasAuthHeader(headers string) bool {
	for _, kv := range strings.Split(headers, ",") {
		if k, _, _ := strings.Cut(kv, "="); strings.EqualFold(strings.TrimSpace(k), tokenHeader) {
			return true
		}
	}
	return false
}

// applyToken turns CALENDAR_OTEL_TOKEN into an "Authorization=Bearer <token>" entry in
// OTEL_EXPORTER_OTLP_HEADERS, which the OpenTelemetry exporters read. It is a shortcut for the common case:
// an explicit Authorization header already in OTEL_EXPORTER_OTLP_HEADERS wins, and nothing happens without a token.
func applyToken() {
	tok := strings.TrimSpace(os.Getenv("CALENDAR_OTEL_TOKEN"))
	if tok == "" {
		return
	}
	existing := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"))
	if hasAuthHeader(existing) {
		return
	}
	h := tokenHeader + "=Bearer%20" + url.PathEscape(tok)
	if existing != "" {
		h = existing + "," + h
	}
	os.Setenv("OTEL_EXPORTER_OTLP_HEADERS", h)
}
