package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"calendar/server/internal/domain"
)

type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

var errGone = errors.New("events: not in the mirror")

// row is a mirrored event with the bookkeeping the write path needs.
type row struct {
	domain.Event
	Deleted bool
}

const evCols = `id, calendar_id, remote_id, COALESCE(ical_uid,''), kind, series_id, COALESCE(remote_series_id,''), recurrence_key,
 title, COALESCE(description,''), COALESCE(location,''), all_day, start_utc, end_utc, COALESCE(tzid,''), start_date, end_date,
 rrule, rdates, exdates, status, busy, COALESCE(organizer_email,''), COALESCE(organizer_name,''), is_organizer, self_response,
 conf_url, conf_kind, conf_source, COALESCE(html_link,''), COALESCE(etag,''), can_edit, can_rsvp, can_add_conf, rev, deleted_at`

func scanRow(rs interface{ Scan(...any) error }) (row, error) {
	var r row
	var kind, status string
	var series sql.NullString
	var rk, su, eu, del sql.NullInt64
	var allDay, busy, isOrg, ce, cr, ca bool
	var sd, ed, rrule, rdates, exdates, selfResp, cu, ck, cs sql.NullString
	var orgEmail, orgName string
	if err := rs.Scan(&r.ID, &r.CalendarID, &r.RemoteID, &r.ICalUID, &kind, &series, &r.RemoteSeriesID, &rk,
		&r.Title, &r.Description, &r.Location, &allDay, &su, &eu, &r.When.TZID, &sd, &ed,
		&rrule, &rdates, &exdates, &status, &busy, &orgEmail, &orgName, &isOrg, &selfResp,
		&cu, &ck, &cs, &r.HTMLLink, &r.ETag, &ce, &cr, &ca, &r.Rev, &del); err != nil {
		return r, err
	}
	r.Kind, r.Status, r.Busy, r.IsOrganizer = domain.Kind(kind), domain.Status(status), busy, isOrg
	if series.Valid {
		s := series.String
		r.SeriesID = &s
	}
	if rk.Valid {
		v := rk.Int64
		r.RecurrenceKey = &v
	}
	r.Deleted = del.Valid
	r.When.AllDay = allDay
	if allDay {
		r.When.TZID = ""
		r.When.StartDate, _ = domain.ParseDate(sd.String)
		r.When.EndDate, _ = domain.ParseDate(ed.String)
	} else if su.Valid && eu.Valid {
		r.When.Start, r.When.End = time.Unix(su.Int64, 0).UTC(), time.Unix(eu.Int64, 0).UTC()
	}
	if rrule.Valid && rrule.String != "" || rdates.Valid || exdates.Valid {
		rc := &domain.Recurrence{RRule: rrule.String}
		if rdates.Valid {
			_ = json.Unmarshal([]byte(rdates.String), &rc.RDates)
		}
		if exdates.Valid {
			_ = json.Unmarshal([]byte(exdates.String), &rc.EXDates)
		}
		r.Recurrence = rc
	}
	if orgEmail != "" {
		r.Organizer = &domain.Person{Email: orgEmail, Name: orgName}
	}
	if selfResp.Valid {
		v := domain.Response(selfResp.String)
		r.SelfResponse = &v
	}
	if cu.Valid && cu.String != "" {
		r.Conference = &domain.Conference{URL: cu.String, Kind: ck.String, Source: cs.String}
	}
	r.Caps = domain.EventCaps{CanEdit: ce, CanRSVP: cr, CanAddConference: ca}
	return r, nil
}

func loadRow(ctx context.Context, q querier, id string) (row, error) {
	r, err := scanRow(q.QueryRowContext(ctx, `SELECT `+evCols+` FROM events WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, errGone
	}
	if err != nil {
		return r, err
	}
	rs, err := q.QueryContext(ctx, `SELECT email, COALESCE(name,''), response, role, is_self, is_organizer FROM attendees WHERE event_id=? ORDER BY email`, id)
	if err != nil {
		return r, err
	}
	defer rs.Close()
	for rs.Next() {
		var a domain.Attendee
		var resp string
		if err := rs.Scan(&a.Email, &a.Name, &resp, &a.Role, &a.IsSelf, &a.IsOrganizer); err != nil {
			return r, err
		}
		a.Response = domain.Response(resp)
		r.Attendees = append(r.Attendees, a)
	}
	return r, rs.Err()
}

// loadExceptions returns the live exception rows of a series.
func loadExceptions(ctx context.Context, q querier, masterID string) ([]domain.Event, error) {
	rs, err := q.QueryContext(ctx, `SELECT `+evCols+` FROM events WHERE series_id=? AND kind='exception' AND deleted_at IS NULL`, masterID)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []domain.Event
	for rs.Next() {
		r, err := scanRow(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, r.Event)
	}
	return out, rs.Err()
}

type calRow struct {
	domain.Calendar
}

func loadCalendar(ctx context.Context, q querier, id string) (domain.Calendar, error) {
	var c domain.Calendar
	var tzid, rc, co sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id, account_id, remote_id, name, tzid, access, is_primary, visible, remote_color, color_override FROM calendars WHERE id=?`, id).
		Scan(&c.ID, &c.AccountID, &c.RemoteID, &c.Name, &tzid, &c.Access, &c.IsPrimary, &c.Visible, &rc, &co)
	if errors.Is(err, sql.ErrNoRows) {
		return c, errGone
	}
	c.TZID, c.RemoteColor, c.ColorOverride = tzid.String, rc.String, co.String
	return c, err
}

func writable(c domain.Calendar) bool { return c.Access == "owner" || c.Access == "writer" }

// parseID splits "eventId" / "eventId:rkey".
func parseID(id string) (eid string, rkey int64, hasKey bool) {
	if i := strings.LastIndex(id, ":"); i > 0 {
		if n, err := strconv.ParseInt(id[i+1:], 10, 64); err == nil && strconv.FormatInt(n, 10) == id[i+1:] {
			return id[:i], n, true
		}
	}
	return id, 0, false
}

func occID(eid string, rkey int64) string { return eid + ":" + strconv.FormatInt(rkey, 10) }

// accountOfEvent finds the account owning an event row (unlocked, used to pick the lock).
func accountOfEvent(ctx context.Context, q querier, eid string) (string, error) {
	var a string
	err := q.QueryRowContext(ctx, `SELECT c.account_id FROM events e JOIN calendars c ON c.id=e.calendar_id WHERE e.id=? AND e.deleted_at IS NULL`, eid).Scan(&a)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errGone
	}
	return a, err
}

// keyOf is the occurrence key of an event's own first instance (all-day: UTC midnight of the date).
func keyOf(w domain.When) int64 {
	if w.AllDay {
		return time.Date(w.StartDate.Y, time.Month(w.StartDate.M), w.StartDate.D, 0, 0, 0, 0, time.UTC).Unix()
	}
	return w.Start.Unix()
}
