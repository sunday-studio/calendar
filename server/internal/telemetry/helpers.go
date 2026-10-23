package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"regexp"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "calendar/server"

// Canonical attribute keys. Add new ones here so names stay consistent across packages.
const (
	KeyAccountID   = attribute.Key("calendar.account.id")   // ULID
	KeyProvider    = attribute.Key("calendar.provider")     // google | ics | ...
	KeyOutcome     = attribute.Key("calendar.outcome")      // ok | error | skipped | ...
	KeyComponent   = attribute.Key("calendar.component")    // sync | events | push | automate | mcp | backup | http | ...
	KeySyncTrigger = attribute.Key("calendar.sync.trigger") // schedule | manual | push | write | startup
	KeyErrorKind   = attribute.Key("calendar.error.kind")
	KeyStatusClass = attribute.Key("calendar.http.status_class") // 2xx | 3xx | 4xx | 5xx
	KeyRoute       = attribute.Key("http.route")
	KeyAuthKind    = attribute.Key("calendar.auth.kind") // device | script
	KeyScope       = attribute.Key("calendar.auth.scope")
	KeyOperation   = attribute.Key("calendar.operation") // low-cardinality verb: create | update | delete | list ...
)

var (
	ulidShape = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	wordShape = regexp.MustCompile(`^[a-z][a-z0-9_.\-]{0,31}$`)
)

// AccountID is calendar.account.id. A value that is not a ULID (an e-mail address, say) is replaced by
// "invalid" so personal data cannot slip in by mistake.
func AccountID(id string) attribute.KeyValue {
	if !ulidShape.MatchString(id) {
		id = "invalid"
	}
	return KeyAccountID.String(id)
}

// Provider is calendar.provider. Only short lowercase words are accepted; anything else becomes "other".
func Provider(p string) attribute.KeyValue { return KeyProvider.String(word(p, "other")) }

// Outcome is calendar.outcome (short lowercase word).
func Outcome(o string) attribute.KeyValue { return KeyOutcome.String(word(o, "other")) }

// Component is calendar.component, bounded to 64 distinct values per process.
func Component(c string) attribute.KeyValue {
	return KeyComponent.String(components.Allow(word(c, "other")))
}

// SyncTrigger is calendar.sync.trigger.
func SyncTrigger(t string) attribute.KeyValue { return KeySyncTrigger.String(word(t, "other")) }

// Operation is calendar.operation.
func Operation(o string) attribute.KeyValue { return KeyOperation.String(word(o, "other")) }

func word(s, fallback string) string {
	s = strings.ToLower(s)
	if !wordShape.MatchString(s) {
		return fallback
	}
	return s
}

// Guard bounds the number of distinct values an attribute can take: after max distinct values every new
// one is reported as "other". It protects metric backends from accidental cardinality explosions.
type Guard struct {
	max  int
	mu   sync.Mutex
	seen map[string]struct{}
}

// NewGuard returns a guard allowing max distinct values.
func NewGuard(max int) *Guard { return &Guard{max: max, seen: map[string]struct{}{}} }

// Allow returns v while the guard has room (or already knows v), otherwise "other".
func (g *Guard) Allow(v string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.seen[v]; ok {
		return v
	}
	if len(g.seen) >= g.max {
		return "other"
	}
	g.seen[v] = struct{}{}
	return v
}

var (
	components = NewGuard(64)
	kindsGuard = NewGuard(32)
)

// Tracer returns a tracer from the active provider (a no-op until Setup enables telemetry).
func Tracer(name string) trace.Tracer {
	if name == "" {
		name = instrumentationName
	}
	return otel.GetTracerProvider().Tracer(name)
}

// Meter returns a meter from the active provider (a no-op until Setup enables telemetry).
func Meter(name string) metric.Meter {
	if name == "" {
		name = instrumentationName
	}
	return otel.GetMeterProvider().Meter(name)
}

// StartSpan starts an internal span with the given attributes. Always `defer span.End()`.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return Tracer(instrumentationName).Start(ctx, name, trace.WithAttributes(attrs...))
}

// ---- instruments ----

func prefixed(name string) string {
	if strings.HasPrefix(name, "calendar.") {
		return name
	}
	return "calendar." + name
}

// Counter creates (or returns) a monotonic int counter named calendar.<name>.
func Counter(name, desc string, unit ...string) metric.Int64Counter {
	opts := []metric.Int64CounterOption{metric.WithDescription(desc)}
	if len(unit) > 0 {
		opts = append(opts, metric.WithUnit(unit[0]))
	}
	c, err := Meter("").Int64Counter(prefixed(name), opts...)
	if err != nil {
		otel.Handle(err)
	}
	return c
}

// UpDownCounter creates an up/down counter (in-flight, active connections, queue depth).
func UpDownCounter(name, desc string, unit ...string) metric.Int64UpDownCounter {
	opts := []metric.Int64UpDownCounterOption{metric.WithDescription(desc)}
	if len(unit) > 0 {
		opts = append(opts, metric.WithUnit(unit[0]))
	}
	c, err := Meter("").Int64UpDownCounter(prefixed(name), opts...)
	if err != nil {
		otel.Handle(err)
	}
	return c
}

// Histogram creates a float histogram. unit is a UCUM unit ("s", "By", "{event}"). Pass explicit bucket
// boundaries for non-latency values or long durations; the default suits seconds up to ~10.
func Histogram(name, desc, unit string, bounds ...float64) metric.Float64Histogram {
	opts := []metric.Float64HistogramOption{metric.WithDescription(desc), metric.WithUnit(unit)}
	if len(bounds) > 0 {
		opts = append(opts, metric.WithExplicitBucketBoundaries(bounds...))
	} else if unit == "s" {
		opts = append(opts, metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60))
	}
	h, err := Meter("").Float64Histogram(prefixed(name), opts...)
	if err != nil {
		otel.Handle(err)
	}
	return h
}

// Gauge registers an observable gauge whose value is read at each export. observe must be cheap and
// non-blocking. Returns the registration so callers can ignore it for process-lifetime gauges.
func Gauge(name, desc, unit string, observe func(ctx context.Context) float64, attrs ...attribute.KeyValue) metric.Float64ObservableGauge {
	g, err := Meter("").Float64ObservableGauge(prefixed(name), metric.WithDescription(desc), metric.WithUnit(unit),
		metric.WithFloat64Callback(func(ctx context.Context, o metric.Float64Observer) error {
			o.Observe(observe(ctx), metric.WithAttributes(attrs...))
			return nil
		}))
	if err != nil {
		otel.Handle(err)
	}
	return g
}

// ---- errors ----

// Error kinds (calendar.error.kind). Keep this list short.
const (
	KindProvider   = "provider"
	KindDB         = "db"
	KindValidation = "validation"
	KindPanic      = "panic"
	KindTimeout    = "timeout"
	KindPush       = "push"
	KindAuth       = "auth"
	KindNetwork    = "network"
	KindCanceled   = "canceled"
	KindInternal   = "internal"
)

type kindMatcher struct {
	kind  string
	match func(error) bool
}

var kindMatchers atomic.Pointer[[]kindMatcher]

// RegisterErrorKind teaches Kind about a typed error, normally with errors.Is / errors.As in the match
// function. It lives here (not imported from the domain packages) so telemetry never depends on them.
// Call it during start-up, before serving.
func RegisterErrorKind(kind string, match func(error) bool) {
	for {
		old := kindMatchers.Load()
		var n []kindMatcher
		if old != nil {
			n = append(n, *old...)
		}
		n = append(n, kindMatcher{kind, match})
		if kindMatchers.CompareAndSwap(old, &n) {
			return
		}
	}
}

// Kind classifies err into a low-cardinality kind. Registered matchers run first (in order), then
// context and network errors; anything else is "internal".
func Kind(err error) string {
	if err == nil {
		return ""
	}
	if ms := kindMatchers.Load(); ms != nil {
		for _, m := range *ms {
			if m.match(err) {
				return kindsGuard.Allow(m.kind)
			}
		}
	}
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return KindTimeout
	case errors.Is(err, context.Canceled):
		return KindCanceled
	case errors.As(err, &ne) && ne.Timeout():
		return KindTimeout
	case errors.As(err, &ne):
		return KindNetwork
	}
	return KindInternal
}

// errState lets the HTTP layer know whether a handler already recorded the failure.
type errState struct{ recorded atomic.Bool }

type errStateKey struct{}

func withErrState(ctx context.Context) (context.Context, *errState) {
	st := &errState{}
	return context.WithValue(ctx, errStateKey{}, st), st
}

func markRecorded(ctx context.Context) {
	if st, ok := ctx.Value(errStateKey{}).(*errState); ok {
		st.recorded.Store(true)
	}
}

type recordedError struct {
	typ, msg string
}

func (e recordedError) Error() string { return e.msg }

// errorsCounterFor returns the counter bound to the currently installed provider. It is rebuilt when
// providers change (tests); in production Setup runs once before any error is recorded.
func errorsCounterFor() metric.Int64Counter {
	if c := counterCache.Load(); c != nil && c.gen == generation.Load() {
		return c.c
	}
	c := Counter("errors", "Errors recorded by the hub, by component and kind", "{error}")
	counterCache.Store(&cachedCounter{gen: generation.Load(), c: c})
	return c
}

type cachedCounter struct {
	gen uint64
	c   metric.Int64Counter
}

var (
	counterCache atomic.Pointer[cachedCounter]
	generation   atomic.Uint64 // bumped by install()
)

// RecordError is the one call for "something failed": it marks the active span as failed with an
// "exception" event (typed kind + scrubbed, truncated message), increments calendar.errors{component,kind},
// and (when telemetry is enabled) writes an error log line carrying trace_id/span_id. It is safe to call
// with a nil error (no-op) and with no active span. Callers should not also log the same failure.
// Never pass user content in attrs.
func RecordError(ctx context.Context, component string, err error, attrs ...attribute.KeyValue) {
	kind := recordSpanError(ctx, component, err, attrs...)
	if kind == "" || !Enabled() {
		return
	}
	log := logger()
	log.ErrorContext(ctx, "error", "component", word(component, "other"), "kind", kind, "err", Sanitize(err.Error()))
}

// RecordSpanError is RecordError without the log line, for call sites that already log the failure.
func RecordSpanError(ctx context.Context, component string, err error, attrs ...attribute.KeyValue) {
	recordSpanError(ctx, component, err, attrs...)
}

func recordSpanError(ctx context.Context, component string, err error, attrs ...attribute.KeyValue) string {
	if err == nil {
		return ""
	}
	kind := Kind(err)
	addErrorTelemetry(ctx, word(component, "other"), kind, typeName(err), Sanitize(err.Error()), "", attrs)
	return kind
}

// RecordPanic records a recovered panic: exception event with the (truncated) stack, span status Error,
// calendar.errors{component, kind=panic}. The caller still logs and answers the request.
func RecordPanic(ctx context.Context, component string, v any, stack []byte) {
	// a panic value can be any text, including user content; only Go runtime errors ("index out of range
	// [5] with length 3") are safe and useful, everything else is reduced to its type
	msg := "panic: " + fmt.Sprintf("%T", v)
	if re, ok := v.(goruntime.Error); ok {
		msg = re.Error()
	}
	st := string(stack)
	if len(st) > 8<<10 {
		st = st[:8<<10]
	}
	addErrorTelemetry(ctx, word(component, "other"), KindPanic, "panic", Sanitize(msg), st, nil)
}

func addErrorTelemetry(ctx context.Context, component, kind, typ, msg, stack string, attrs []attribute.KeyValue) {
	markRecorded(ctx)
	base := []attribute.KeyValue{KeyComponent.String(components.Allow(component)), KeyErrorKind.String(kind)}
	errorsCounterFor().Add(ctx, 1, metric.WithAttributes(base...))
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	ev := append([]attribute.KeyValue{
		attribute.String("exception.type", typ),
		attribute.String("exception.message", msg),
	}, base...)
	if stack != "" {
		ev = append(ev, attribute.String("exception.stacktrace", stack))
	}
	ev = append(ev, attrs...)
	span.AddEvent("exception", trace.WithAttributes(ev...))
	span.SetAttributes(KeyErrorKind.String(kind))
	span.SetStatus(codes.Error, kind+": "+msg)
}

func typeName(err error) string {
	t := reflect.TypeOf(err)
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil {
		return "error"
	}
	return t.String()
}
