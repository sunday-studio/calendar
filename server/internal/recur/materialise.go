package recur

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"calendar/server/internal/db"
	"calendar/server/internal/tz"
)

const (
	DefaultBackDays    = 30
	DefaultForwardDays = 400
	DefaultMaxPerSerie = 5000
	DefaultUserTZ      = "Europe/London"
)

// Materialiser implements sync.Materialiser: it rebuilds the occurrences read model.
type Materialiser struct {
	Now         func() time.Time // injectable clock
	BackDays    int
	ForwardDays int
	MaxPerSerie int
	Log         *slog.Logger

	win *Window // test override
}

func (m *Materialiser) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}
func (m *Materialiser) log() *slog.Logger {
	if m.Log != nil {
		return m.Log
	}
	return slog.Default()
}
func orDef(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}

type evRow struct {
	id, cal, kind, status string
	series                *string
	rkey                  *int64
	allDay                bool
	su, eu                *int64
	tzid                  string
	sd, ed                *string
	rrule                 string
	rdates, exdates       []string
	deleted               bool
}

const evCols = `id, calendar_id, kind, status, series_id, recurrence_key, all_day, start_utc, end_utc, COALESCE(tzid,''), start_date, end_date, COALESCE(rrule,''), COALESCE(rdates,''), COALESCE(exdates,''), deleted_at IS NOT NULL`

type rowScanner interface{ Scan(...any) error }

func scanEv(r rowScanner) (evRow, error) {
	var e evRow
	var rd, xd string
	err := r.Scan(&e.id, &e.cal, &e.kind, &e.status, &e.series, &e.rkey, &e.allDay, &e.su, &e.eu, &e.tzid, &e.sd, &e.ed, &e.rrule, &rd, &xd, &e.deleted)
	if rd != "" {
		_ = json.Unmarshal([]byte(rd), &e.rdates)
	}
	if xd != "" {
		_ = json.Unmarshal([]byte(xd), &e.exdates)
	}
	return e, err
}

func (m *Materialiser) userTZ(ctx context.Context, tx *db.Tx) *time.Location {
	var v string
	name := DefaultUserTZ
	if err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='user_tz'`).Scan(&v); err == nil {
		var s string
		if json.Unmarshal([]byte(v), &s) == nil && s != "" {
			name = s
		} else if v != "" {
			name = v
		}
	}
	return tz.MustLoad(name, m.log())
}

func (m *Materialiser) window(loc *time.Location) Window {
	if m.win != nil {
		return *m.win
	}
	n := m.now().In(loc)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	return Window{
		From: today.AddDate(0, 0, -orDef(m.BackDays, DefaultBackDays)),
		To:   today.AddDate(0, 0, orDef(m.ForwardDays, DefaultForwardDays)),
	}
}

// Rebuild rebuilds occurrences for the given events. Exceptions map to their series; a series
// is rebuilt whole. Idempotent: delete + insert within tx.
func (m *Materialiser) Rebuild(ctx context.Context, tx *db.Tx, eventIDs []string) error {
	loc := m.userTZ(ctx, tx)
	win := m.window(loc)
	done := map[string]bool{}
	for _, id := range eventIDs {
		e, err := scanEv(tx.QueryRowContext(ctx, `SELECT `+evCols+` FROM events WHERE id=?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := tx.ExecContext(ctx, `DELETE FROM occurrences WHERE event_id=?`, id); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		root := e.id
		if e.kind == "exception" && e.series != nil {
			root = *e.series
		}
		if done[root] {
			continue
		}
		done[root] = true
		if root != e.id {
			if e, err = scanEv(tx.QueryRowContext(ctx, `SELECT `+evCols+` FROM events WHERE id=?`, root)); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return err
			}
		}
		if err := m.rebuildOne(ctx, tx, e, loc, win); err != nil {
			return err
		}
	}
	return nil
}

func (m *Materialiser) rebuildOne(ctx context.Context, tx *db.Tx, e evRow, loc *time.Location, win Window) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM occurrences WHERE event_id=? OR event_id IN (SELECT id FROM events WHERE series_id=?)`, e.id, e.id); err != nil {
		return err
	}
	if e.deleted || e.status == "cancelled" {
		return nil
	}
	switch e.kind {
	case "master":
		return m.rebuildSeries(ctx, tx, e, loc, win)
	case "exception":
		// orphan exception (master not mirrored): show it on its own
		return m.insertEvent(ctx, tx, e, loc, e.rkey)
	case "instance":
		return m.insertEvent(ctx, tx, e, loc, e.rkey)
	default:
		return m.insertEvent(ctx, tx, e, loc, nil)
	}
}

// occTimes returns the stored occurrence row for an event's own times.
func (m *Materialiser) insertEvent(ctx context.Context, tx *db.Tx, e evRow, loc *time.Location, rkey *int64) error {
	var key int64
	if rkey != nil {
		key = *rkey
	}
	var s, en int64
	if e.allDay {
		if e.sd == nil || e.ed == nil {
			return nil
		}
		var err error
		if s, en, err = allDayInstants(*e.sd, *e.ed, loc); err != nil {
			return nil
		}
	} else {
		if e.su == nil || e.eu == nil {
			return nil
		}
		s, en = *e.su, *e.eu
	}
	return insertOcc(ctx, tx, e.id, key, e.cal, e.allDay, s, en, e.sd, e.ed)
}

func allDayInstants(sd, ed string, loc *time.Location) (int64, int64, error) {
	a, err := time.ParseInLocation(time.DateOnly, sd, loc)
	if err != nil {
		return 0, 0, err
	}
	b, err := time.ParseInLocation(time.DateOnly, ed, loc)
	if err != nil {
		return 0, 0, err
	}
	return a.Unix(), b.Unix(), nil
}

func insertOcc(ctx context.Context, tx *db.Tx, id string, rkey int64, cal string, allDay bool, s, e int64, sd, ed *string) error {
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO occurrences(event_id,rkey,calendar_id,all_day,start_utc,end_utc,start_date,end_date) VALUES (?,?,?,?,?,?,?,?)`,
		id, rkey, cal, allDay, s, e, sd, ed)
	return err
}

func (m *Materialiser) rebuildSeries(ctx context.Context, tx *db.Tx, e evRow, loc *time.Location, win Window) error {
	s := Series{AllDay: e.allDay, TZID: e.tzid, RRule: e.rrule, RDates: e.rdates, EXDates: e.exdates}
	if e.allDay {
		if e.sd == nil || e.ed == nil {
			return nil
		}
		s.StartDate, s.EndDate = *e.sd, *e.ed
	} else {
		if e.su == nil || e.eu == nil {
			return nil
		}
		s.Start, s.End = time.Unix(*e.su, 0).UTC(), time.Unix(*e.eu, 0).UTC()
	}
	occs, warns := Expand(s, win, 0)
	for _, w := range warns {
		m.log().Warn("recurrence", "event", e.id, "msg", w)
	}

	// exceptions
	rows, err := tx.QueryContext(ctx, `SELECT `+evCols+` FROM events WHERE series_id=? AND kind='exception'`, e.id)
	if err != nil {
		return err
	}
	var excs []evRow
	for rows.Next() {
		x, err := scanEv(rows)
		if err != nil {
			rows.Close()
			return err
		}
		excs = append(excs, x)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	replaced := map[int64]bool{}
	for _, x := range excs {
		if x.rkey != nil {
			replaced[*x.rkey] = true
		}
	}

	type outRow struct {
		id, cal string
		key     int64
		allDay  bool
		s, e    int64
		sd, ed  *string
	}
	var rowsOut []outRow
	for _, o := range occs {
		if replaced[o.Key] {
			continue
		}
		r := outRow{id: e.id, cal: e.cal, key: o.Key, allDay: o.AllDay}
		if o.AllDay {
			sd, ed := o.StartDate, o.EndDate
			r.sd, r.ed = &sd, &ed
			r.s, r.e, _ = allDayInstants(sd, ed, loc)
		} else {
			r.s, r.e = o.Start.Unix(), o.End.Unix()
		}
		rowsOut = append(rowsOut, r)
	}
	for _, x := range excs {
		if x.deleted || x.status == "cancelled" || x.rkey == nil {
			continue // tombstoned/cancelled exception = excluded occurrence
		}
		r := outRow{id: x.id, cal: x.cal, key: *x.rkey, allDay: x.allDay, sd: x.sd, ed: x.ed}
		if x.allDay {
			if x.sd == nil || x.ed == nil {
				continue
			}
			var err error
			if r.s, r.e, err = allDayInstants(*x.sd, *x.ed, loc); err != nil {
				continue
			}
		} else {
			if x.su == nil || x.eu == nil {
				continue
			}
			r.s, r.e = *x.su, *x.eu
		}
		// keep only exceptions that land in the window
		if r.s >= win.To.Unix() || (r.e <= win.From.Unix() && r.s < win.From.Unix()) {
			continue
		}
		rowsOut = append(rowsOut, r)
	}
	max := orDef(m.MaxPerSerie, DefaultMaxPerSerie)
	if len(rowsOut) > max {
		sortByStart(rowsOut, func(r outRow) int64 { return r.s })
		rowsOut = rowsOut[:max]
		m.log().Warn("recurrence: series truncated", "event", e.id, "max", max)
	}
	for _, r := range rowsOut {
		if err := insertOcc(ctx, tx, r.id, r.key, r.cal, r.allDay, r.s, r.e, r.sd, r.ed); err != nil {
			return err
		}
	}
	return nil
}

func sortByStart[T any](v []T, f func(T) int64) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && f(v[j]) < f(v[j-1]); j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

// ExtendWindow re-materialises every live master (the window moves with the clock).
func (m *Materialiser) ExtendWindow(ctx context.Context, d *db.DB) error {
	rows, err := d.R.QueryContext(ctx, `SELECT id FROM events WHERE kind='master' AND deleted_at IS NULL`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	const batch = 50
	for i := 0; i < len(ids); i += batch {
		j := min(i+batch, len(ids))
		if err := d.Write(ctx, func(tx *db.Tx) error { return m.Rebuild(ctx, tx, ids[i:j]) }); err != nil {
			return err
		}
	}
	return nil
}

// RunDaily extends the window now and then every interval (default 24h) until ctx ends.
func (m *Materialiser) RunDaily(ctx context.Context, d *db.DB, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := m.ExtendWindow(ctx, d); err != nil && ctx.Err() == nil {
			m.log().Error("extend window", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RetimeAllDay recomputes the ordering instants (start_utc/end_utc) of every all-day
// occurrence for a new user zone. Dates stay authoritative and are untouched.
func RetimeAllDay(ctx context.Context, tx *db.Tx, loc *time.Location) error {
	rs, err := tx.QueryContext(ctx, `SELECT event_id, rkey, start_date, end_date FROM occurrences WHERE all_day=1 AND start_date IS NOT NULL AND end_date IS NOT NULL`)
	if err != nil {
		return err
	}
	type row struct {
		id     string
		rkey   int64
		sd, ed string
	}
	var all []row
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.id, &r.rkey, &r.sd, &r.ed); err != nil {
			rs.Close()
			return err
		}
		all = append(all, r)
	}
	rs.Close()
	for _, r := range all {
		s, e, err := allDayInstants(r.sd, r.ed, loc)
		if err != nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE occurrences SET start_utc=?, end_utc=? WHERE event_id=? AND rkey=?`, s, e, r.id, r.rkey); err != nil {
			return err
		}
	}
	return nil
}
