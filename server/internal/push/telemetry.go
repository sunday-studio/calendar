package push

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"calendar/server/internal/telemetry"
)

func kindOf(m Message) string {
	if m.Kind == "" {
		return "other"
	}
	return m.Kind
}

// sendOne delivers m to one device inside a "push.send" span and returns 1 on success. Outcomes
// (calendar.push.sent): ok | failed | dead_token | throttled. The device token is never an attribute, a
// log field or part of an error message.
func (d *Dispatcher) sendOne(ctx context.Context, e endpoint, m Message) int {
	kind := kindOf(m)
	env := e.Env
	ctx, span := telemetry.StartSpan(ctx, "push.send", telemetry.Attr("kind", kind), telemetry.Attr("env", env))
	defer span.End()
	start := time.Now()
	serr := d.Sender.Send(ctx, e.Target, m)
	telemetry.Record(ctx, "push.send.duration", time.Since(start).Seconds(), telemetry.Attr("kind", kind))
	out := "ok"
	switch {
	case serr == nil:
	case errors.Is(serr, ErrUnregistered):
		out = "dead_token"
		span.AddEvent("push.dead_token", trace.WithAttributes(telemetry.Attr("kind", kind)))
		res, err := d.DB.W.ExecContext(ctx, `DELETE FROM push_endpoints WHERE token_id=? AND apns_token=?`, e.tokenID, e.Token)
		if err != nil {
			telemetry.RecordError(ctx, "push", err, telemetry.Operation("drop_dead_token"))
		} else if n, _ := res.RowsAffected(); n > 0 {
			telemetry.Add(ctx, "push.dead_tokens_removed", n)
		}
		d.log().InfoContext(ctx, "push: dropped a dead device token", "token_id", e.tokenID)
	case errors.Is(serr, ErrThrottled):
		out = "throttled"
		telemetry.RecordErrorKind(ctx, "push", telemetry.KindPush, serr, telemetry.Operation("send"), attribute.String("calendar.apns.status_class", "4xx"))
	default:
		out = "failed"
		cls := "none"
		if errors.Is(serr, ErrTransient) {
			cls = "5xx"
		} else if !errors.Is(serr, context.Canceled) {
			cls = "4xx"
		}
		telemetry.RecordErrorKind(ctx, "push", telemetry.KindPush, serr, telemetry.Operation("send"), attribute.String("calendar.apns.status_class", cls))
	}
	telemetry.Inc(ctx, "push.sent", telemetry.Attr("kind", kind), telemetry.Outcome(out))
	d.log().InfoContext(ctx, "push sent", "kind", kind, "env", env, "outcome", out)
	span.SetAttributes(telemetry.Outcome(out))
	if out == "ok" {
		return 1
	}
	return 0
}
