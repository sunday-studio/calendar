package agenda

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// briefKeep is how long after an event was cancelled or deleted its brief can still be read. It is what a "cancelled"
// notification needs to name the event: the hub keeps the row, the phone may never have had it.
const briefKeep = 48 * time.Hour

// Brief is the little a notification needs to name an event. Cancelled and deleted events keep answering for briefKeep.
type Brief struct {
	Title     string     `json:"title"`
	Start     *time.Time `json:"start,omitempty"`
	AllDay    bool       `json:"allDay"`
	Date      string     `json:"date,omitempty"`
	Place     string     `json:"place,omitempty"`
	Organizer string     `json:"organizer,omitempty"`
	Cancelled bool       `json:"cancelled"`
}

// Brief returns the brief of an event (`id` may be an eventId or an occurrence id eventId:rkey).
func (s *Store) Brief(ctx context.Context, id string) (Brief, error) {
	eid := id
	if i := strings.LastIndex(id, ":"); i > 0 {
		var n int64
		if _, err := fmt.Sscanf(id[i+1:], "%d", &n); err == nil && fmt.Sprint(n) == id[i+1:] {
			eid = id[:i]
		}
	}
	tx, err := s.readTx(ctx)
	if err != nil {
		return Brief{}, err
	}
	defer tx.Rollback()
	var b Brief
	var status string
	var allDay int
	var startUTC, deletedAt, updatedAt sql.NullInt64
	var startDate, location, calName, orgName, orgEmail sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT e.title, e.status, e.all_day, e.start_utc, e.start_date, e.location, c.name,
		e.organizer_name, e.organizer_email, e.deleted_at, e.remote_updated_at
		FROM events e JOIN calendars c ON c.id=e.calendar_id WHERE e.id=?`, eid).
		Scan(&b.Title, &status, &allDay, &startUTC, &startDate, &location, &calName, &orgName, &orgEmail, &deletedAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Brief{}, ErrNotFound
	}
	if err != nil {
		return Brief{}, err
	}
	now := s.now()
	b.Cancelled = deletedAt.Valid || status == "cancelled"
	if b.Cancelled {
		since := deletedAt.Int64
		if !deletedAt.Valid {
			since = updatedAt.Int64
		}
		if since == 0 || now.Sub(time.Unix(since, 0)) > briefKeep {
			return Brief{}, ErrNotFound
		}
	}
	b.AllDay = allDay == 1
	if b.AllDay && startDate.Valid {
		b.Date = startDate.String
	}
	var next sql.NullInt64
	_ = tx.QueryRowContext(ctx, `SELECT MIN(start_utc) FROM occurrences WHERE event_id=? AND end_utc>=?`, eid, now.Unix()).Scan(&next)
	switch {
	case next.Valid:
		t := time.Unix(next.Int64, 0).UTC()
		b.Start = &t
	case startUTC.Valid:
		t := time.Unix(startUTC.Int64, 0).UTC()
		b.Start = &t
	}
	b.Place = strings.TrimSpace(location.String)
	if b.Place == "" {
		b.Place = calName.String
	}
	b.Organizer = strings.TrimSpace(orgName.String)
	if b.Organizer == "" {
		b.Organizer = orgEmail.String
	}
	return b, nil
}
