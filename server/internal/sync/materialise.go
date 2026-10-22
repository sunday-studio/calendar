package sync

import (
	"context"
	"time"

	"calendar/server/internal/db"
)

// Materialiser rebuilds the occurrences read model for the given events inside the sync tx.
// Default implementation: recur.Materialiser (T07); SimpleMaterialiser remains for tests.
type Materialiser interface {
	Rebuild(ctx context.Context, tx *db.Tx, eventIDs []string) error
}

// SimpleMaterialiser handles non-recurring events only: one occurrence with rkey 0.
// Masters and exceptions are skipped (T07).
type SimpleMaterialiser struct{}

func (SimpleMaterialiser) Rebuild(ctx context.Context, tx *db.Tx, ids []string) error {
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM occurrences WHERE event_id=?`, id); err != nil {
			return err
		}
		var (
			cal, kind, status string
			allDay            bool
			su, eu            *int64
			sd, ed            *string
			deleted           *int64
		)
		err := tx.QueryRowContext(ctx, `SELECT calendar_id, kind, status, all_day, start_utc, end_utc, start_date, end_date, deleted_at FROM events WHERE id=?`, id).
			Scan(&cal, &kind, &status, &allDay, &su, &eu, &sd, &ed, &deleted)
		if err != nil {
			return err
		}
		if kind != "single" || deleted != nil || status == "cancelled" {
			continue
		}
		var s, e int64
		if allDay {
			if sd == nil || ed == nil {
				continue
			}
			ts, err1 := time.Parse(time.DateOnly, *sd)
			te, err2 := time.Parse(time.DateOnly, *ed)
			if err1 != nil || err2 != nil {
				continue
			}
			s, e = ts.Unix(), te.Unix() // ordering only; dates are authoritative
		} else {
			if su == nil || eu == nil {
				continue
			}
			s, e = *su, *eu
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO occurrences(event_id,rkey,calendar_id,all_day,start_utc,end_utc,start_date,end_date) VALUES (?,0,?,?,?,?,?,?)`,
			id, cal, allDay, s, e, sd, ed); err != nil {
			return err
		}
	}
	return nil
}
