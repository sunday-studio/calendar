package ics

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"calendar/server/internal/telemetry"
)

// traced wraps the network layer with client spans / http.client.* metrics (calendar.provider=ics). The
// feed's path, query and userinfo can hold a secret token no path templating could recognise, so spans
// see only scheme://host/feed.
func traced(base http.RoundTripper) http.RoundTripper {
	return telemetry.WrapClientRedacted(base, "ics", "/feed")
}

func observeFetch(ctx context.Context, start time.Time, status int, err error) {
	out := "ok"
	switch {
	case err != nil:
		out = "error"
	case status == http.StatusNotModified:
		out = "not_modified"
	case status == http.StatusTooManyRequests:
		out = "rate_limited"
	case status/100 != 2:
		out = "error"
	}
	attrs := []attribute.KeyValue{telemetry.Provider("ics"), telemetry.Attr("operation", "fetch")}
	telemetry.Inc(ctx, "provider.requests", append(attrs, telemetry.Attr("status_class", telemetry.StatusClass(status)), telemetry.Outcome(out))...)
	telemetry.Record(ctx, "provider.latency", time.Since(start).Seconds(), attrs...)
	if out == "rate_limited" {
		telemetry.Inc(ctx, "provider.rate_limited", telemetry.Provider("ics"))
	}
}
