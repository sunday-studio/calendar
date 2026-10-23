package automate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"calendar/server/internal/agenda"
	"calendar/server/internal/db"
	"calendar/server/internal/telemetry"
)

const (
	startWindow  = 10 * time.Minute // a start trigger is still honoured this long after its moment (restart, slow tick)
	cronWindow   = time.Hour        // a cron time missed by more than this is skipped, not replayed
	autoCursor   = "auto_cursor"
	defaultEvery = 30 * time.Second
)

// Run evaluates triggers every `every` (default 30 s) until ctx ends.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = defaultEvery
	}
	tk := time.NewTicker(every)
	defer tk.Stop()
	for {
		telemetry.Safe(ctx, "automate.tick", func() {
			tctx, span := telemetry.StartSpan(ctx, "automation.tick")
			defer span.End()
			if err := s.Tick(tctx); err != nil && ctx.Err() == nil {
				telemetry.RecordError(tctx, "automate", err, telemetry.Operation("tick"))
			}
		})
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
	}
}

// Tick evaluates every trigger once and delivers what is due (synchronously).
func (s *Service) Tick(ctx context.Context) error {
	now := s.now()
	created, err := s.newEvents(ctx)
	if err != nil {
		return err
	}
	rs, err := s.rows(ctx, "WHERE enabled=1")
	if err != nil {
		return err
	}
	loc := s.userLoc(ctx)
	for _, r := range rs {
		switch r.trig.Type {
		case "event.created":
			for _, c := range created {
				if r.trig.CalendarID != "" && r.trig.CalendarID != c.calendarID {
					continue
				}
				s.fireEvent(ctx, r, "event.created", c.eventID+":"+strconv.FormatInt(c.rkey, 10), loc)
			}
		case "event.start":
			if err := s.fireStarts(ctx, r, now, loc); err != nil {
				return err
			}
		case "cron":
			if err := s.fireCron(ctx, r, now, loc); err != nil {
				return err
			}
		}
	}
	return nil
}

type newEvent struct {
	eventID, calendarID string
	rkey                int64
}

// newEvents returns events that appeared since the last tick. The first tick only records
// what already exists, so enabling an automation never announces your whole calendar.
func (s *Service) newEvents(ctx context.Context) ([]newEvent, error) {
	var maxRev int64
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COALESCE(MAX(rev),0) FROM changes`).Scan(&maxRev); err != nil {
		return nil, err
	}
	cur, ok := s.getMeta(ctx, autoCursor)
	if !ok {
		err := s.DB.Write(ctx, func(tx *db.Tx) error {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO automation_seen(event_id) SELECT id FROM events`); err != nil {
				return err
			}
			return s.setMeta(ctx, tx, autoCursor, strconv.FormatInt(maxRev, 10))
		})
		return nil, err
	}
	last, _ := strconv.ParseInt(cur, 10, 64)
	if maxRev <= last {
		return nil, nil
	}
	rs, err := s.DB.R.QueryContext(ctx, `SELECT DISTINCT c.entity_id FROM changes c WHERE c.rev>? AND c.rev<=? AND c.entity='event' AND c.op='upsert'`, last, maxRev)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rs.Next() {
		var id string
		if err := rs.Scan(&id); err != nil {
			rs.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rs.Close()
	var out []newEvent
	err = s.DB.Write(ctx, func(tx *db.Tx) error {
		for _, id := range ids {
			res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO automation_seen(event_id) VALUES (?)`, id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue // seen before: an edit, not a creation
			}
			var cal string
			var kind, status string
			var del sql.NullInt64
			err = tx.QueryRowContext(ctx, `SELECT calendar_id, kind, status, deleted_at FROM events WHERE id=?`, id).Scan(&cal, &kind, &status, &del)
			if err != nil || del.Valid || status == "cancelled" || kind == "exception" {
				continue
			}
			rkey := int64(0)
			if kind == "master" {
				_ = tx.QueryRowContext(ctx, `SELECT rkey FROM occurrences WHERE event_id=? AND end_utc>=? ORDER BY start_utc LIMIT 1`, id, s.now().Unix()).Scan(&rkey)
			}
			out = append(out, newEvent{id, cal, rkey})
		}
		return s.setMeta(ctx, tx, autoCursor, strconv.FormatInt(maxRev, 10))
	})
	return out, err
}

func (s *Service) fireEvent(ctx context.Context, r row, trig, occID string, loc *time.Location) {
	ev, err := s.Store.Event(ctx, occID)
	if err != nil {
		if !errors.Is(err, agenda.ErrNotFound) { // an event deleted since the trigger is expected
			telemetry.RecordError(ctx, "automate", err, telemetry.Operation("load_event"))
		}
		return
	}
	p := s.payload(r, trig)
	p.Event = &ev
	s.deliver(ctx, r, p, loc)
}

func (s *Service) fireStarts(ctx context.Context, r row, now time.Time, loc *time.Location) error {
	off := int64(r.trig.OffsetMin) * 60
	q := `SELECT o.event_id, o.rkey FROM occurrences o JOIN events e ON e.id=o.event_id JOIN calendars c ON c.id=e.calendar_id
		WHERE o.all_day=0 AND e.deleted_at IS NULL AND e.status<>'cancelled' AND c.visible=1 AND COALESCE(e.self_response,'')<>'declined'
		  AND o.start_utc + ? <= ? AND o.start_utc + ? > ?
		  AND NOT EXISTS (SELECT 1 FROM automation_fired f WHERE f.automation_id=? AND f.event_id=o.event_id AND f.rkey=o.rkey)`
	args := []any{off, now.Unix(), off, now.Add(-startWindow).Unix(), r.id}
	if r.trig.CalendarID != "" {
		q += ` AND e.calendar_id=?`
		args = append(args, r.trig.CalendarID)
	}
	q += ` ORDER BY o.start_utc LIMIT 50`
	rs, err := s.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	type occ struct {
		id   string
		rkey int64
	}
	var due []occ
	for rs.Next() {
		var o occ
		if err := rs.Scan(&o.id, &o.rkey); err != nil {
			rs.Close()
			return err
		}
		due = append(due, o)
	}
	rs.Close()
	for _, o := range due {
		// mark first: a crash after sending must not send again
		if err := s.DB.Write(ctx, func(tx *db.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO automation_fired(automation_id,event_id,rkey,fired_at) VALUES (?,?,?,?)`, r.id, o.id, o.rkey, now.Unix())
			return err
		}); err != nil {
			return err
		}
		s.fireEvent(ctx, r, "event.start", fmt.Sprintf("%s:%d", o.id, o.rkey), loc)
	}
	// forget old markers
	_, err = s.DB.W.ExecContext(ctx, `DELETE FROM automation_fired WHERE fired_at < ?`, now.Add(-72*time.Hour).Unix())
	return err
}

func (s *Service) fireCron(ctx context.Context, r row, now time.Time, loc *time.Location) error {
	c, err := ParseCron(r.trig.Expr)
	if err != nil {
		telemetry.RecordError(ctx, "automate", err, telemetry.Operation("parse_cron")) // validated on save: a corrupt row
		return nil
	}
	lastS, ok := s.getMeta(ctx, cronKey(r.id))
	last := now
	if ok {
		if n, err := strconv.ParseInt(lastS, 10, 64); err == nil {
			last = time.Unix(n, 0)
		}
	}
	next := c.Next(last, loc)
	if next.IsZero() || next.After(now) {
		if !ok {
			return s.DB.Write(ctx, func(tx *db.Tx) error { return s.setMeta(ctx, tx, cronKey(r.id), strconv.FormatInt(now.Unix(), 10)) })
		}
		return nil
	}
	if err := s.DB.Write(ctx, func(tx *db.Tx) error {
		return s.setMeta(ctx, tx, cronKey(r.id), strconv.FormatInt(now.Unix(), 10))
	}); err != nil {
		return err
	}
	if now.Sub(next) > cronWindow {
		return nil // missed by too long (server was off): wait for the next one
	}
	p := s.payload(r, "cron")
	if sum, err := s.Store.DaySummary(ctx, ""); err == nil {
		p.Summary = &sum
	} else {
		telemetry.RecordError(ctx, "automate", err, telemetry.Operation("day_summary"))
	}
	s.deliver(ctx, r, p, loc)
	return nil
}
