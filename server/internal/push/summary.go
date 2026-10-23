package push

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"calendar/server/internal/agenda"
	"calendar/server/internal/telemetry"
	"calendar/server/internal/tz"
)

// SummaryTick sends the morning summary when it is due: enabled, local time past the chosen
// time (within 3 hours, so a late restart does not announce breakfast at tea time) and not yet
// sent today. The payload carries only the date; the app fetches GET /v1/summary.
func (d *Dispatcher) SummaryTick(ctx context.Context, store *agenda.Store) error {
	st, err := store.Settings(ctx)
	if err != nil {
		return err
	}
	if st.MorningSummary == nil || !st.MorningSummary.Enabled || st.UserTZID == nil {
		return nil
	}
	loc, _ := tz.Load(*st.UserTZID)
	now := d.now().In(loc)
	t, err := time.Parse("15:04", st.MorningSummary.Time)
	if err != nil {
		return nil
	}
	at := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	if now.Before(at) || now.After(at.Add(3*time.Hour)) {
		return nil
	}
	date := now.Format(time.DateOnly)
	var sent string
	err = d.DB.R.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, summaryKey).Scan(&sent)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if sent == date {
		return nil
	}
	// mark first: a crash after sending must not send twice
	if _, err := d.DB.W.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, summaryKey, date); err != nil {
		return err
	}
	ctx, span := telemetry.StartSpan(ctx, "push.summary")
	defer span.End()
	n, err := d.deliver(ctx, alertMsg("summary", map[string]any{"date": date}, "summary", "summary-"+date))
	if err == nil {
		telemetry.Add(ctx, "push.summary_sent", 1)
		span.SetAttributes(attribute.Int("calendar.push.devices", n))
	}
	return err
}

// RunSummary checks once a minute.
func (d *Dispatcher) RunSummary(ctx context.Context, store *agenda.Store) {
	tk := time.NewTicker(time.Minute)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			telemetry.Safe(ctx, "push.summary", func() {
				if err := d.SummaryTick(ctx, store); err != nil && ctx.Err() == nil {
					telemetry.RecordError(ctx, "push", err, telemetry.Operation("summary_tick"))
				}
				if err := d.ReminderTick(ctx, store); err != nil && ctx.Err() == nil {
					telemetry.RecordError(ctx, "push", err, telemetry.Operation("reminder_tick"))
				}
			})
		}
	}
}
