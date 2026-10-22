package push

import (
	"context"
	"fmt"
	"time"

	"calendar/server/internal/agenda"
	"calendar/server/internal/tz"
)

const (
	reminderLate = 6 * time.Hour // a reminder this long overdue (hub was off) is skipped, not sent late
	reminderSpan = 30            // days ahead to look: leads can be up to four weeks
)

type due struct {
	eventID string
	rkey    int64
}

// ReminderTick sends a push for every all-day occurrence whose alert time has come (see
// agenda.AlertDefaults): the lead is the per-event override (hub-only reminder) or, failing that,
// alertDefaults.allDay; no lead, no reminder. Timed events are reminded by local notifications on the
// phone, which need no hub. One push per occurrence, ever.
func (d *Dispatcher) ReminderTick(ctx context.Context, store *agenda.Store) error {
	st, err := store.Settings(ctx)
	if err != nil {
		return err
	}
	loc, _ := tz.Load(*st.UserTZID)
	now := d.now()
	today := now.In(loc)
	from := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1)
	rs, err := d.DB.R.QueryContext(ctx, `SELECT o.event_id, o.rkey, o.start_date,
		COALESCE((SELECT r.minutes_before FROM reminder_overrides r WHERE r.event_id=o.event_id AND r.rkey IN (o.rkey,0) ORDER BY r.rkey DESC LIMIT 1), -1)
		FROM occurrences o JOIN events e ON e.id=o.event_id JOIN calendars c ON c.id=e.calendar_id
		WHERE o.all_day=1 AND o.start_date>=? AND o.start_date<=? AND e.deleted_at IS NULL AND e.status<>'cancelled' AND c.visible=1
		  AND COALESCE(e.self_response,'')<>'declined'
		  AND NOT EXISTS (SELECT 1 FROM push_reminded p WHERE p.event_id=o.event_id AND p.rkey=o.rkey)`,
		from.Format(time.DateOnly), from.AddDate(0, 0, reminderSpan).Format(time.DateOnly))
	if err != nil {
		return err
	}
	var todo []due
	for rs.Next() {
		var x due
		var date string
		var lead int
		if err := rs.Scan(&x.eventID, &x.rkey, &date, &lead); err != nil {
			rs.Close()
			return err
		}
		if lead < 0 {
			if st.AlertDefaults == nil || st.AlertDefaults.AllDay == nil {
				continue
			}
			lead = *st.AlertDefaults.AllDay
		}
		at, ok := agenda.AllDayAlertAt(date, lead, st.AlertDefaults.AllDayTime, loc)
		if !ok || now.Before(at) || !now.Before(at.Add(reminderLate)) {
			continue
		}
		todo = append(todo, x)
	}
	rs.Close()
	for _, x := range todo {
		// mark first: a crash after sending must not send twice
		if _, err := d.DB.W.ExecContext(ctx, `INSERT OR IGNORE INTO push_reminded(event_id,rkey,sent_at) VALUES (?,?,?)`, x.eventID, x.rkey, now.Unix()); err != nil {
			return err
		}
		id := fmt.Sprintf("%s:%d", x.eventID, x.rkey)
		if _, err := d.deliver(ctx, alertMsg("reminder", map[string]any{"eventId": x.eventID, "occurrenceId": id}, "reminders", "rem-"+id)); err != nil {
			return err
		}
	}
	_, err = d.DB.W.ExecContext(ctx, `DELETE FROM push_reminded WHERE sent_at < ?`, now.Add(-7*24*time.Hour).Unix())
	return err
}
