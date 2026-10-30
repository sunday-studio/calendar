package telemetry

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/trace"
)

// NewLogger builds the hub's logger. Output on w is the existing JSON slog format; when a span is active
// in the context given to the *Context log methods, trace_id and span_id are added. When logs export is
// enabled, every record is additionally sent to the OTLP log pipeline (a second handler) with messages
// scrubbed and sensitive keys dropped. Call it after Setup.
func NewLogger(w io.Writer) *slog.Logger {
	stdoutLevel, otlpLevel := LogLevels()
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: stdoutLevel})
	plain := slog.New(traceHandler{base})
	plainLog.Store(plain)
	var h slog.Handler = traceHandler{base}
	if lp := logProv.Load(); lp != nil {
		bridge := scrubHandler{otelslog.NewHandler(instrumentationName, otelslog.WithLoggerProvider(lp.p))}
		h = fanout{h, levelHandler{otlpLevel, bridge}}
	}
	l := slog.New(h)
	appLogger.Store(l)
	return l
}

// traceHandler adds trace_id/span_id from the context to every record.
type traceHandler struct{ slog.Handler }

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return h.Handler.Handle(ctx, r)
}
func (h traceHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(a)}
}
func (h traceHandler) WithGroup(n string) slog.Handler { return traceHandler{h.Handler.WithGroup(n)} }

// fanout sends each record to all handlers.
type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	var first error
	for _, h := range f {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func (f fanout) WithAttrs(a []slog.Attr) slog.Handler {
	n := make(fanout, len(f))
	for i, h := range f {
		n[i] = h.WithAttrs(a)
	}
	return n
}

func (f fanout) WithGroup(name string) slog.Handler {
	n := make(fanout, len(f))
	for i, h := range f {
		n[i] = h.WithGroup(name)
	}
	return n
}

// scrubHandler is the privacy gate in front of the OTel log bridge.
type scrubHandler struct{ next slog.Handler }

func (h scrubHandler) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h scrubHandler) Handle(ctx context.Context, r slog.Record) error {
	n := slog.NewRecord(r.Time, r.Level, Sanitize(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if a, ok := scrubAttr(a); ok {
			n.AddAttrs(a)
		}
		return true
	})
	return h.next.Handle(ctx, n)
}

func (h scrubHandler) WithAttrs(as []slog.Attr) slog.Handler {
	var keep []slog.Attr
	for _, a := range as {
		if a, ok := scrubAttr(a); ok {
			keep = append(keep, a)
		}
	}
	return scrubHandler{h.next.WithAttrs(keep)}
}
func (h scrubHandler) WithGroup(n string) slog.Handler { return scrubHandler{h.next.WithGroup(n)} }

func scrubAttr(a slog.Attr) (slog.Attr, bool) {
	if denied(a.Key) {
		return a, false
	}
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindGroup:
		var g []slog.Attr
		for _, x := range v.Group() {
			if x, ok := scrubAttr(x); ok {
				g = append(g, x)
			}
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(g...)}, true
	case slog.KindString:
		return slog.String(a.Key, Sanitize(v.String())), true
	case slog.KindAny:
		// errors and Stringers can echo content: stringify and scrub, never ship the raw value
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, Sanitize(err.Error())), true
		}
		if s, ok := v.Any().(interface{ String() string }); ok {
			return slog.String(a.Key, Sanitize(s.String())), true
		}
		return slog.String(a.Key, Sanitize(strings.TrimSpace(v.String()))), true
	}
	return a, true
}

// Log is the hub's logger for code that has no logger of its own: stdout JSON with trace ids (use the
// *Context methods) and the OTLP log bridge, as built by NewLogger. Before NewLogger has run (unit tests,
// CLI sub-commands) it discards, so library code can log lifecycle events without ever writing to stderr
// in a different format.
func Log() *slog.Logger {
	if l := appLogger.Load(); l != nil {
		return l
	}
	return discardLog
}

var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))
