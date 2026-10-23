package automate

import (
	"context"
	"regexp"
	"time"

	"calendar/server/internal/telemetry"
)

var ulidRe = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// safeID passes automation ULIDs and replaces anything else.
func safeID(id string) string {
	if ulidRe.MatchString(id) {
		return id
	}
	return "invalid"
}

// observeSend records one delivery attempt: status class and latency, never the URL or host.
func (s *Service) observeSend(ctx context.Context, r row, began time.Time, status int, err error) {
	out := "ok"
	if err != nil {
		out = "failed"
	}
	cls := telemetry.StatusClass(status)
	telemetry.Inc(ctx, "automation.webhook.requests", telemetry.Attr("action", r.act.Type), telemetry.Attr("status_class", cls), telemetry.Outcome(out))
	telemetry.Record(ctx, "automation.webhook.duration", time.Since(began).Seconds(), telemetry.Attr("action", r.act.Type), telemetry.Attr("status_class", cls))
}
