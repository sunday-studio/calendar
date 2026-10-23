package events

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"calendar/server/internal/provider"
	"calendar/server/internal/telemetry"
)

// opState travels in ctx for the duration of one write so that begin/load can fill in the provider and
// the calendar access level that the span and metrics report.
type opState struct {
	name     string
	scope    string
	prov     string
	access   string
	span     trace.Span
	start    time.Time
	hubOnly  bool
	finished bool
}

type opKey struct{}

func opFrom(ctx context.Context) *opState {
	st, _ := ctx.Value(opKey{}).(*opState)
	return st
}

// startOp opens the span events.<name> for one user write.
func startOp(ctx context.Context, name string, scope provider.Scope) (context.Context, *opState) {
	sc := string(scope)
	if sc == "" {
		sc = "this"
	}
	ctx, span := telemetry.StartSpan(ctx, "events."+name, telemetry.Operation(name), telemetry.Attr("scope", sc))
	st := &opState{name: name, scope: sc, prov: "unknown", span: span, start: time.Now()}
	return context.WithValue(ctx, opKey{}, st), st
}

func (st *opState) setProvider(p string) {
	if st == nil {
		return
	}
	st.prov = p
	st.span.SetAttributes(telemetry.Provider(p))
}

func (st *opState) setAccess(a string) {
	if st == nil || a == "" {
		return
	}
	st.access = a
	st.span.SetAttributes(telemetry.Attr("calendar.access", a))
}

// outcomeOf maps a write result to the calendar.writes outcome label.
func outcomeOf(err error) string {
	if err == nil {
		return "ok"
	}
	e, ok := AsError(err)
	if !ok {
		return "provider_error"
	}
	switch e.Kind {
	case KValidation, KNotFound, KForbidden:
		if strings.HasPrefix(e.Msg, "not supported") {
			return "not_supported"
		}
		return "validation"
	case KPrecondition:
		return "precondition_failed"
	case KConflict:
		return "conflict"
	case KRateLimited:
		return "rate_limited"
	}
	return "provider_error"
}

// done ends the span and records calendar.writes / calendar.write.duration. Call it with the final error.
func (st *opState) done(ctx context.Context, err error) {
	if st == nil || st.finished {
		return
	}
	st.finished = true
	out := outcomeOf(err)
	attrs := []attribute.KeyValue{telemetry.Attr("op", st.name), telemetry.Provider(st.prov)}
	telemetry.Inc(ctx, "writes", append(attrs, telemetry.Outcome(out))...)
	telemetry.Record(ctx, "write.duration", time.Since(st.start).Seconds(), attrs...)
	st.span.SetAttributes(telemetry.Outcome(out))
	if out == "provider_error" {
		st.span.SetStatus(2, "provider_error") // codes.Error; the cause was recorded where it happened
	}
	st.span.End()
}

// RecordReplay counts a write answered from the idempotency record (no provider call, no mirror change).
// The HTTP layer calls it when it serves a stored response.
func RecordReplay(ctx context.Context, op string) {
	telemetry.Inc(ctx, "idempotency.replays", telemetry.Attr("op", op))
	telemetry.Inc(ctx, "writes", telemetry.Attr("op", op), telemetry.Provider("unknown"), telemetry.Outcome("replayed"))
	trace.SpanFromContext(ctx).AddEvent("idempotency.replay", trace.WithAttributes(telemetry.Operation(op)))
}

// providerWrite classifies a failed provider call: expected conditions (conflict, rate limit, not found,
// unsupported) are outcomes only; anything else, and a refused credential, is recorded as an error.
func recordProviderWriteError(ctx context.Context, err error) {
	var rl provider.ErrRateLimited
	var ce *provider.ConflictError
	switch {
	case errors.As(err, &ce), errors.As(err, &rl), errors.Is(err, provider.ErrNotFound),
		errors.Is(err, provider.ErrPreconditionFailed), errors.Is(err, provider.ErrNotSupported):
		return
	case errors.Is(err, provider.ErrNeedsReauth):
		telemetry.RecordErrorKind(ctx, "events", telemetry.KindReauth, err, telemetry.Operation(opName(ctx)))
	default:
		telemetry.RecordErrorKind(ctx, "events", telemetry.KindProvider, err, telemetry.Operation(opName(ctx)))
	}
}

func opName(ctx context.Context) string {
	if st := opFrom(ctx); st != nil {
		return st.name
	}
	return "write"
}

// consistency records the dangerous states: the provider accepted a write but the mirror could not take
// it (or, for a cross-account move, the compensating delete failed). The next sync repairs the mirror in
// the first case; the second needs a human.
func consistency(ctx context.Context, what string, err error) {
	telemetry.Inc(ctx, "writes.provider_rollbacks", telemetry.Attr("op", opName(ctx)), telemetry.Attr("kind", what))
	telemetry.RecordErrorKind(ctx, "events", telemetry.KindConsistency, err, telemetry.Operation(opName(ctx)))
}
