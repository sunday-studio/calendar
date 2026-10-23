package google

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"

	"calendar/server/internal/provider"
	"calendar/server/internal/telemetry"
)

type opKey struct{}

// withOp names the provider operation for requests made under ctx (rsvp, move ...) when the HTTP method
// and path alone do not say it.
func withOp(ctx context.Context, op string) context.Context {
	return context.WithValue(ctx, opKey{}, op)
}

// operation is the low-cardinality calendar.provider.requests operation:
// list_events | list_calendars | fetch | insert | patch | delete | rsvp | move | token_refresh.
func operation(ctx context.Context, method, path string) string {
	if op, ok := ctx.Value(opKey{}).(string); ok && op != "" {
		return op
	}
	switch method {
	case http.MethodGet:
		return "fetch" // a single event read before a write; listings use listOp
	case http.MethodPost:
		if strings.HasSuffix(path, "/move") {
			return "move"
		}
		return "insert"
	case http.MethodDelete:
		return "delete"
	}
	return "patch"
}

func listOp(ctx context.Context, path string) string {
	if strings.Contains(path, "/calendarList") {
		return "list_calendars"
	}
	return "list_events"
}

func outcomeOf(status int, err error) string {
	switch {
	case err != nil:
		return "error"
	case status/100 == 2:
		return "ok"
	case status == 401:
		return "needs_reauth"
	case status == 404 || status == 410:
		return "not_found"
	case status == 409 || status == 412:
		return "conflict"
	case status == 429:
		return "rate_limited"
	}
	return "error"
}

// observe records one provider attempt: calendar.provider.requests / .latency, the rate-limit counter and a
// span event on the active operation span. resp may be nil (transport failure).
func observe(ctx context.Context, op string, start time.Time, resp *http.Response, body []byte, terr error) {
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	out := outcomeOf(status, terr)
	if status == http.StatusForbidden {
		var rl provider.ErrRateLimited
		if errors.As(classify(resp, body), &rl) {
			out = "rate_limited"
		}
	}
	attrs := []attribute.KeyValue{telemetry.Provider("google"), telemetry.Attr("operation", op)}
	telemetry.Inc(ctx, "provider.requests", append(attrs, telemetry.Attr("status_class", telemetry.StatusClass(status)), telemetry.Outcome(out))...)
	telemetry.Record(ctx, "provider.latency", time.Since(start).Seconds(), attrs...)
	if out == "rate_limited" {
		telemetry.Inc(ctx, "provider.rate_limited", telemetry.Provider("google"))
		trace.SpanFromContext(ctx).AddEvent("provider.rate_limited", trace.WithAttributes(telemetry.Provider("google"), telemetry.Attr("operation", op)))
	}
}

// meteredSource wraps the OAuth refresh-token source: every real refresh (the reuse source only calls it
// when the access token expired) is counted, and a refusal of the refresh token is an error of kind=reauth.
type meteredSource struct {
	src oauth2.TokenSource
	ctx context.Context
}

func (m meteredSource) Token() (*oauth2.Token, error) {
	ctx, span := telemetry.StartSpan(m.ctx, "google.token_refresh", telemetry.Provider("google"))
	defer span.End()
	start := time.Now()
	tok, err := m.src.Token()
	attrs := []attribute.KeyValue{telemetry.Provider("google"), telemetry.Attr("operation", "token_refresh")}
	telemetry.Record(ctx, "provider.latency", time.Since(start).Seconds(), attrs...)
	switch {
	case err == nil:
		telemetry.Inc(ctx, "provider.token_refresh", telemetry.Provider("google"), telemetry.Outcome("ok"))
		telemetry.Inc(ctx, "provider.requests", append(attrs, telemetry.Attr("status_class", "2xx"), telemetry.Outcome("ok"))...)
	case errors.Is(mapTransportErr(err), provider.ErrNeedsReauth):
		telemetry.Inc(ctx, "provider.token_refresh", telemetry.Provider("google"), telemetry.Outcome("invalid_grant"))
		telemetry.Inc(ctx, "provider.requests", append(attrs, telemetry.Attr("status_class", "4xx"), telemetry.Outcome("needs_reauth"))...)
		telemetry.RecordErrorKind(ctx, "provider.google", telemetry.KindReauth, err, telemetry.Operation("token_refresh"))
	default:
		telemetry.Inc(ctx, "provider.token_refresh", telemetry.Provider("google"), telemetry.Outcome("failed"))
		telemetry.Inc(ctx, "provider.requests", append(attrs, telemetry.Attr("status_class", "none"), telemetry.Outcome("error"))...)
		telemetry.RecordError(ctx, "provider.google", err, telemetry.Operation("token_refresh"))
	}
	return tok, err
}
