package agenda

import (
	"context"
	"errors"
	"time"

	"calendar/server/internal/telemetry"
)

// timed opens a span "agenda.<kind>" and returns a function that ends it, records
// calendar.agenda.query.duration{kind} and, for a failure that is not an expected condition (unknown id,
// invalid query), records the error. kind: agenda | events | search | freebusy | freetime | summary.
func timed(ctx context.Context, kind string) (context.Context, func(error)) {
	ctx, span := telemetry.StartSpan(ctx, "agenda."+kind, telemetry.Attr("kind", kind))
	start := time.Now()
	return ctx, func(err error) {
		telemetry.Record(ctx, "agenda.query.duration", time.Since(start).Seconds(), telemetry.Attr("kind", kind))
		if kind == "search" {
			telemetry.Inc(ctx, "search.queries", telemetry.Outcome(map[bool]string{true: "error", false: "ok"}[err != nil]))
		}
		if err != nil {
			var ve *ValidationError
			if !errors.Is(err, ErrNotFound) && !errors.As(err, &ve) && ctx.Err() == nil {
				telemetry.RecordError(ctx, "agenda", err, telemetry.Operation(kind))
			}
		}
		span.End()
	}
}

// Agenda returns occurrences overlapping [from,to) on visible calendars, optionally limited
// to calendars. Order: by local day, all-day first, then start instant, title, id.
func (s *Store) Agenda(ctx context.Context, from, to time.Time, calendars []string) (res AgendaResult, err error) {
	ctx, end := timed(ctx, "agenda")
	defer func() { end(err) }()
	return s.agenda(ctx, from, to, calendars)
}

// Event loads one event. id is an eventId or an occurrence id (eventId:rkey); a bare eventId
// resolves to the occurrence nearest to now (ongoing/next, else the latest past one).
func (s *Store) Event(ctx context.Context, id string) (ev Event, err error) {
	ctx, end := timed(ctx, "events")
	defer func() { end(err) }()
	return s.event(ctx, id)
}

// List returns events (as occurrences, ascending by start) for a window and/or a text query.
// With q, each event appears once (its first occurrence inside the window).
func (s *Store) List(ctx context.Context, lq ListQuery) (page EventPage, err error) {
	kind := "events"
	if lq.Q != "" {
		kind = "search"
	}
	ctx, end := timed(ctx, kind)
	defer func() { end(err) }()
	return s.list(ctx, lq)
}

// FreeBusy returns merged busy intervals clipped to [from,to) from visible calendars:
// busy events only, excluding events the user declined.
func (s *Store) FreeBusy(ctx context.Context, from, to time.Time) (fb FreeBusy, err error) {
	ctx, end := timed(ctx, "freebusy")
	defer func() { end(err) }()
	return s.freeBusy(ctx, from, to)
}

// FreeTime finds gaps from the busy occurrences of visible calendars, inside working hours
// unless told otherwise. It is deterministic; nothing here involves a language model.
func (s *Store) FreeTime(ctx context.Context, q FreeTimeQuery) (ft FreeTime, err error) {
	ctx, end := timed(ctx, "freetime")
	defer func() { end(err) }()
	return s.freeTime(ctx, q)
}

// DaySummary summarises the local day `date` (YYYY-MM-DD, user zone; "" = today).
func (s *Store) DaySummary(ctx context.Context, date string) (ds DaySummary, err error) {
	ctx, end := timed(ctx, "summary")
	defer func() { end(err) }()
	return s.daySummary(ctx, date)
}
