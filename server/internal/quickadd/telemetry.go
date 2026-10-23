package quickadd

import (
	"calendar/server/internal/events"
	"context"
	"errors"
	"strconv"

	"calendar/server/internal/telemetry"
)

type silentKey struct{}

// WithoutMetrics marks ctx so that a parser called as a fallback does not count the request a second
// time (the outer parser reports the final answer, with source=rules degraded=true).
func WithoutMetrics(ctx context.Context) context.Context {
	return context.WithValue(ctx, silentKey{}, true)
}

func silent(ctx context.Context) bool { v, _ := ctx.Value(silentKey{}).(bool); return v }

// RecordParse counts one quick-add request: calendar.quickadd.requests{source, degraded, outcome} and, when
// the draft needs answers, calendar.quickadd.ambiguities. Outcome is ok | ambiguous | error; an invalid
// sentence is an outcome, not an error. The text itself is never recorded.
func RecordParse(ctx context.Context, d Draft, err error) {
	out := "ok"
	switch {
	case err != nil:
		out = "error"
		var ve *events.Error
		if !errors.As(err, &ve) {
			telemetry.RecordError(ctx, "quickadd", err, telemetry.Operation("parse"))
		}
	case len(d.Ambiguities) > 0:
		out = "ambiguous"
		telemetry.Add(ctx, "quickadd.ambiguities", 1)
	}
	src := d.Source
	if src == "" {
		src = "rules"
	}
	telemetry.Inc(ctx, "quickadd.requests", telemetry.Attr("source", src), telemetry.Attr("degraded", strconv.FormatBool(d.Degraded)), telemetry.Outcome(out))
}
