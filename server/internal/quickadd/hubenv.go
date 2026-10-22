package quickadd

import (
	"context"
	"strings"
	"time"

	"calendar/server/internal/agenda"
	"calendar/server/internal/db"
)

// HubEnv answers from the hub's own database: people seen on earlier events and writable calendars.
type HubEnv struct {
	DB    *db.DB
	Store *agenda.Store
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(strings.ToLower(s))
}

// Contacts finds people whose name (any word) or email starts with name, most recently seen first.
func (h HubEnv) Contacts(ctx context.Context, name string) ([]Contact, error) {
	n := likeEscape(strings.TrimSpace(name))
	if n == "" {
		return nil, nil
	}
	rs, err := h.DB.R.QueryContext(ctx, `SELECT a.email, COALESCE(MAX(NULLIF(a.name,'')),''), COUNT(*), COALESCE(MAX(e.start_utc),0)
		FROM attendees a JOIN events e ON e.id=a.event_id
		WHERE e.deleted_at IS NULL AND a.is_self=0 AND a.role<>'resource'
		  AND (lower(a.name) LIKE ? ESCAPE '\' OR lower(a.name) LIKE ? ESCAPE '\' OR lower(a.email) LIKE ? ESCAPE '\')
		GROUP BY lower(a.email) ORDER BY MAX(e.start_utc) DESC, COUNT(*) DESC LIMIT 6`, n+"%", "% "+n+"%", n+"%")
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []Contact
	for rs.Next() {
		var c Contact
		var last int64
		if err := rs.Scan(&c.Email, &c.Name, &c.Count, &last); err != nil {
			return nil, err
		}
		c.Last = time.Unix(last, 0).UTC()
		out = append(out, c)
	}
	return out, rs.Err()
}

// Calendars lists writable calendars with the default one flagged.
func (h HubEnv) Calendars(ctx context.Context) ([]Calendar, error) {
	all, err := h.Store.Calendars(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.Store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	var out []Calendar
	for _, c := range all {
		if c.Access != "owner" && c.Access != "writer" {
			continue
		}
		def := st.DefaultCalendarID != nil && *st.DefaultCalendarID == c.ID
		if st.DefaultCalendarID == nil && c.IsPrimary && len(out) == 0 {
			def = true
		}
		out = append(out, Calendar{ID: c.ID, Name: c.Name, IsDefault: def})
	}
	return out, nil
}
