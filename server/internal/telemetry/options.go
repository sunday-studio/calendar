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

// levelSet decides which log levels pass. A single level is a minimum ("info" lets info, warn and error through).
// A comma-separated list is exactly those levels ("info,error" lets info and error through, not warn or debug).
type levelSet struct {
	min   slog.Level // single value: the minimum
	bands uint8      // list: bit per band, see band()
	list  bool
}

var defaultLevels = levelSet{min: slog.LevelInfo}

// band maps any slog level to 0 debug, 1 info, 2 warn, 3 error.
func band(l slog.Level) uint {
	switch {
	case l >= slog.LevelError:
		return 3
	case l >= slog.LevelWarn:
		return 2
	case l >= slog.LevelInfo:
		return 1
	}
	return 0
}

func (s levelSet) allows(l slog.Level) bool {
	if s.list {
		return s.bands&(1<<band(l)) != 0
	}
	return l >= s.min
}

func parseOne(s string) (slog.Level, bool) {
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

// parseLevels reads debug|info|warn|error|off, or a comma-separated list of the first four. ok is false for an
// empty value or one with no known level.
func parseLevels(s string) (levelSet, bool) {
	if !strings.Contains(s, ",") {
		l, ok := parseOne(s)
		return levelSet{min: l}, ok
	}
	set := levelSet{list: true}
	for _, part := range strings.Split(s, ",") {
		if l, ok := parseOne(part); ok && l != levelOff {
			set.bands |= 1 << band(l)
		}
	}
	return set, set.bands != 0
}

// LogLevels returns the levels that pass for the stdout log and for the OTLP log export.
// CALENDAR_LOG_LEVEL sets both (default info); CALENDAR_OTEL_LOG_LEVEL overrides the OTLP side only.
// Each is debug, info, warn, error or off (a minimum), or a list such as "info,error" (exactly those levels).
// An unknown value falls back to the default.
func LogLevels() (stdout, otlp levelSet) {
	stdout = defaultLevels
	if l, ok := parseLevels(os.Getenv("CALENDAR_LOG_LEVEL")); ok {
		stdout = l
	}
	otlp = stdout
	if l, ok := parseLevels(os.Getenv("CALENDAR_OTEL_LOG_LEVEL")); ok {
		otlp = l
	}
	return stdout, otlp
}

// levelHandler drops records whose level the set does not allow before they reach next.
type levelHandler struct {
	set  levelSet
	next slog.Handler
}

func (h levelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.set.allows(l) && h.next.Enabled(ctx, l)
}
func (h levelHandler) Handle(ctx context.Context, r slog.Record) error { return h.next.Handle(ctx, r) }
func (h levelHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return levelHandler{h.set, h.next.WithAttrs(a)}
}
func (h levelHandler) WithGroup(n string) slog.Handler {
	return levelHandler{h.set, h.next.WithGroup(n)}
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
