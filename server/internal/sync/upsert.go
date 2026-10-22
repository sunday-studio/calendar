package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"calendar/server/internal/db"
	"calendar/server/internal/domain"
)

type stats struct{ added, changed, removed int }

func (s *stats) add(o stats) { s.added += o.added; s.changed += o.changed; s.removed += o.removed }

type planItem struct {
	ev          domain.Event
	id          string // existing or newly assigned
	exists      bool
	wasDeleted  bool
	tomb        bool
	oldTitle    string
	oldLocation string
	oldDesc     string
}

func nstr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// attendeesText is what events_fts indexes for attendees; the same function builds the
// text for insert and for the contentless 'delete' command, so both always agree.
func attendeesText(rows []domain.Attendee) string {
	parts := make([]string, 0, len(rows))
	for _, a := range rows {
		parts = append(parts, strings.TrimSpace(a.Name+" "+a.Email))
	}
	return strings.Join(parts, " ")
}

func loadAttendees(ctx context.Context, tx *db.Tx, id string) ([]domain.Attendee, error) {
	rows, err := tx.QueryContext(ctx, `SELECT email, COALESCE(name,'') FROM attendees WHERE event_id=? ORDER BY email`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Attendee
	for rows.Next() {
		var a domain.Attendee
		if err := rows.Scan(&a.Email, &a.Name); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func ftsDelete(ctx context.Context, tx *db.Tx, id string, p planItem) error {
	var rowid int64
	if err := tx.QueryRowContext(ctx, `SELECT rowid FROM events WHERE id=?`, id).Scan(&rowid); err != nil {
		return err
	}
	at, err := loadAttendees(ctx, tx, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO events_fts(events_fts, rowid, title, location, description, attendees) VALUES('delete',?,?,?,?,?)`,
		rowid, p.oldTitle, p.oldLocation, p.oldDesc, attendeesText(at))
	return err
}

// applyBatch writes one provider page. It runs inside the caller's transaction; the cursor
// (when given) is written in that same transaction. seen is non-nil for a full sync and is
// extended with every remote id returned; on the final page, unseen live events are tombstoned.
func applyBatch(ctx context.Context, tx *db.Tx, mat Materialiser, cal domain.Calendar, events []domain.Event, deleted []string,
	seen map[string]struct{}, finalPage bool, cursor string, now time.Time) (stats, error) {
	var st stats
	var plan []planItem
	for _, ev := range events {
		if seen != nil {
			seen[ev.RemoteID] = struct{}{}
		}
		p := planItem{ev: ev, tomb: ev.Status == domain.Cancelled}
		var etag, title, loc, desc sql.NullString
		var del sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT id, etag, deleted_at, title, location, description FROM events WHERE calendar_id=? AND remote_id=?`, cal.ID, ev.RemoteID).
			Scan(&p.id, &etag, &del, &title, &loc, &desc)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return st, err
		default:
			p.exists, p.wasDeleted = true, del.Valid
			p.oldTitle, p.oldLocation, p.oldDesc = title.String, loc.String, desc.String
		}
		if p.tomb {
			if p.exists && p.wasDeleted {
				continue
			}
			if !p.exists && ev.RemoteSeriesID == "" {
				continue
			}
		} else if p.exists && !p.wasDeleted && ev.ETag != "" && etag.String == ev.ETag {
			continue // unchanged
		}
		if !p.exists {
			p.id = ulid.Make().String()
		}
		plan = append(plan, p)
	}
	for _, rid := range deleted {
		if seen != nil {
			seen[rid] = struct{}{}
		}
		var p planItem
		p.tomb = true
		p.ev = domain.Event{RemoteID: rid, Status: domain.Cancelled}
		var del sql.NullInt64
		var title, loc, desc sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT id, deleted_at, title, location, description FROM events WHERE calendar_id=? AND remote_id=?`, cal.ID, rid).Scan(&p.id, &del, &title, &loc, &desc)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && del.Valid) {
			continue
		}
		if err != nil {
			return st, err
		}
		p.exists = true
		p.oldTitle, p.oldLocation, p.oldDesc = title.String, loc.String, desc.String
		plan = append(plan, p)
	}
	if seen != nil && finalPage {
		rows, err := tx.QueryContext(ctx, `SELECT id, remote_id, title, COALESCE(location,''), COALESCE(description,'') FROM events WHERE calendar_id=? AND deleted_at IS NULL`, cal.ID)
		if err != nil {
			return st, err
		}
		var stale []planItem
		for rows.Next() {
			var p planItem
			var rid string
			if err := rows.Scan(&p.id, &rid, &p.oldTitle, &p.oldLocation, &p.oldDesc); err != nil {
				rows.Close()
				return st, err
			}
			if _, ok := seen[rid]; !ok {
				p.tomb, p.exists = true, true
				p.ev = domain.Event{RemoteID: rid, Status: domain.Cancelled}
				stale = append(stale, p)
			}
		}
		rows.Close()
		plan = append(plan, stale...)
	}

	if len(plan) > 0 {
		for _, p := range plan { // first Change() assigns the tx rev used below
			op := "upsert"
			if p.tomb {
				op = "delete"
			}
			if err := tx.Change(ctx, "event", p.id, op); err != nil {
				return st, err
			}
		}
		rev := tx.Rev()
		affected := make([]string, 0, len(plan))
		for _, p := range plan {
			var err error
			if p.tomb {
				err = tombstone(ctx, tx, cal, p, rev, now)
				st.removed++
			} else {
				err = upsertEvent(ctx, tx, cal, p, rev)
				if p.exists {
					st.changed++
				} else {
					st.added++
				}
			}
			if err != nil {
				return st, err
			}
			affected = append(affected, p.id)
			// the master of an exception must be re-materialised too
			if p.ev.RemoteSeriesID != "" {
				var mid sql.NullString
				if err := tx.QueryRowContext(ctx, `SELECT series_id FROM events WHERE id=?`, p.id).Scan(&mid); err != nil {
					return st, err
				}
				if mid.Valid {
					affected = append(affected, mid.String)
				}
			}
		}
		if err := mat.Rebuild(ctx, tx, dedupe(affected)); err != nil {
			return st, err
		}
	}
	if finalPage && cursor != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE calendars SET sync_cursor=?, last_sync_at=? WHERE id=?`, cursor, now.Unix(), cal.ID); err != nil {
			return st, err
		}
	}
	return st, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func tombstone(ctx context.Context, tx *db.Tx, cal domain.Calendar, p planItem, rev int64, now time.Time) error {
	ev := p.ev
	if !p.exists { // cancelled instance we never saw: keep a marker so the materialiser can exclude it
		var rk any
		if ev.RecurrenceKey != nil {
			rk = *ev.RecurrenceKey
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO events(id,calendar_id,remote_id,ical_uid,kind,remote_series_id,recurrence_key,title,status,rev,deleted_at,etag)
			VALUES (?,?,?,?,?,?,?,'','cancelled',?,?,?)`, p.id, cal.ID, ev.RemoteID, nstr(ev.ICalUID), "exception", nstr(ev.RemoteSeriesID), rk, rev, now.Unix(), nstr(ev.ETag))
		if err != nil {
			return err
		}
		return linkSeries(ctx, tx, cal, p)
	}
	if !p.wasDeleted {
		if err := ftsDelete(ctx, tx, p.id, p); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE events SET deleted_at=?, status='cancelled', rev=? WHERE id=?`, now.Unix(), rev, p.id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM attendees WHERE event_id=?`, p.id)
	return err
}

func linkSeries(ctx context.Context, tx *db.Tx, cal domain.Calendar, p planItem) error {
	ev := p.ev
	if ev.RemoteSeriesID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE events SET series_id=(SELECT m.id FROM events m WHERE m.calendar_id=? AND m.remote_id=? AND m.kind='master') WHERE id=?`,
			cal.ID, ev.RemoteSeriesID, p.id); err != nil {
			return err
		}
	}
	if ev.Kind == domain.Master {
		if _, err := tx.ExecContext(ctx, `UPDATE events SET series_id=? WHERE calendar_id=? AND remote_series_id=? AND series_id IS NULL`, p.id, cal.ID, ev.RemoteID); err != nil {
			return err
		}
	}
	return nil
}

func upsertEvent(ctx context.Context, tx *db.Tx, cal domain.Calendar, p planItem, rev int64) error {
	ev := p.ev
	if p.exists && !p.wasDeleted {
		if err := ftsDelete(ctx, tx, p.id, p); err != nil {
			return err
		}
	}
	var su, eu, rk any
	var sd, ed, tz any
	if ev.When.AllDay {
		sd, ed = ev.When.StartDate.String(), ev.When.EndDate.String()
	} else if !ev.When.Start.IsZero() {
		su, eu, tz = ev.When.Start.Unix(), ev.When.End.Unix(), nstr(ev.When.TZID)
	}
	if ev.RecurrenceKey != nil {
		rk = *ev.RecurrenceKey
	}
	var rrule, rdates, exdates any
	if r := ev.Recurrence; r != nil {
		rrule = nstr(r.RRule)
		if len(r.RDates) > 0 {
			b, _ := json.Marshal(r.RDates)
			rdates = string(b)
		}
		if len(r.EXDates) > 0 {
			b, _ := json.Marshal(r.EXDates)
			exdates = string(b)
		}
	}
	var oe, on, sr, cu, ck, cs, ru any
	if ev.Organizer != nil {
		oe, on = nstr(ev.Organizer.Email), nstr(ev.Organizer.Name)
	}
	if ev.SelfResponse != nil {
		sr = string(*ev.SelfResponse)
	}
	if ev.Conference != nil {
		cu, ck, cs = ev.Conference.URL, ev.Conference.Kind, ev.Conference.Source
	}
	if ev.RemoteUpdatedAt != nil {
		ru = ev.RemoteUpdatedAt.Unix()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO events(id,calendar_id,remote_id,ical_uid,kind,remote_series_id,recurrence_key,title,description,location,
		all_day,start_utc,end_utc,tzid,start_date,end_date,rrule,rdates,exdates,status,busy,organizer_email,organizer_name,is_organizer,self_response,
		conf_url,conf_kind,conf_source,html_link,etag,remote_updated_at,can_edit,can_rsvp,can_add_conf,rev,deleted_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,NULL)
		ON CONFLICT(calendar_id, remote_id) DO UPDATE SET
		ical_uid=excluded.ical_uid, kind=excluded.kind, remote_series_id=excluded.remote_series_id, recurrence_key=excluded.recurrence_key,
		title=excluded.title, description=excluded.description, location=excluded.location, all_day=excluded.all_day,
		start_utc=excluded.start_utc, end_utc=excluded.end_utc, tzid=excluded.tzid, start_date=excluded.start_date, end_date=excluded.end_date,
		rrule=excluded.rrule, rdates=excluded.rdates, exdates=excluded.exdates, status=excluded.status, busy=excluded.busy,
		organizer_email=excluded.organizer_email, organizer_name=excluded.organizer_name, is_organizer=excluded.is_organizer,
		self_response=excluded.self_response, conf_url=excluded.conf_url, conf_kind=excluded.conf_kind, conf_source=excluded.conf_source,
		html_link=excluded.html_link, etag=excluded.etag, remote_updated_at=excluded.remote_updated_at,
		can_edit=excluded.can_edit, can_rsvp=excluded.can_rsvp, can_add_conf=excluded.can_add_conf, rev=excluded.rev, deleted_at=NULL`,
		p.id, cal.ID, ev.RemoteID, nstr(ev.ICalUID), string(ev.Kind), nstr(ev.RemoteSeriesID), rk, ev.Title, nstr(ev.Description), nstr(ev.Location),
		b2i(ev.When.AllDay), su, eu, tz, sd, ed, rrule, rdates, exdates, string(ev.Status), b2i(ev.Busy), oe, on, b2i(ev.IsOrganizer), sr,
		cu, ck, cs, nstr(ev.HTMLLink), nstr(ev.ETag), ru, b2i(ev.Caps.CanEdit), b2i(ev.Caps.CanRSVP), b2i(ev.Caps.CanAddConference), rev)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM attendees WHERE event_id=?`, p.id); err != nil {
		return err
	}
	ats := append([]domain.Attendee(nil), ev.Attendees...)
	sort.Slice(ats, func(i, j int) bool { return ats[i].Email < ats[j].Email })
	for _, a := range ats {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO attendees(event_id,email,name,response,role,is_self,is_organizer) VALUES (?,?,?,?,?,?,?)`,
			p.id, a.Email, nstr(a.Name), string(a.Response), a.Role, b2i(a.IsSelf), b2i(a.IsOrganizer)); err != nil {
			return err
		}
	}
	var rowid int64
	if err := tx.QueryRowContext(ctx, `SELECT rowid FROM events WHERE id=?`, p.id).Scan(&rowid); err != nil {
		return err
	}
	// re-read attendees so the indexed text equals what a later 'delete' will reconstruct
	stored, err := loadAttendees(ctx, tx, p.id)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events_fts(rowid,title,location,description,attendees) VALUES (?,?,?,?,?)`,
		rowid, ev.Title, ev.Location, ev.Description, attendeesText(stored)); err != nil {
		return err
	}
	return linkSeries(ctx, tx, cal, p)
}
