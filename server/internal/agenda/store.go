package agenda

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"calendar/server/internal/db"
	"calendar/server/internal/tz"
)

const DefaultUserTZ = "Europe/London"

// Store serves the read model. Now is injectable for tests.
type Store struct {
	DB  *db.DB
	Now func() time.Time
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// readTx opens a consistent snapshot on the reader pool.
func (s *Store) readTx(ctx context.Context) (*sql.Tx, error) { return s.DB.R.BeginTx(ctx, nil) }

func rawSetting(ctx context.Context, q querier, key string) (string, bool) {
	var v string
	if err := q.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func userLoc(ctx context.Context, q querier) *time.Location {
	name := DefaultUserTZ
	if v, ok := rawSetting(ctx, q, "user_tz"); ok {
		var s string
		if json.Unmarshal([]byte(v), &s) == nil && s != "" {
			name = s
		} else if v != "" && !strings.HasPrefix(v, "\"") {
			name = v
		}
	}
	loc, _ := tz.Load(name)
	return loc
}

func rev(ctx context.Context, q querier) (int64, error) {
	var r int64
	err := q.QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM meta WHERE key='rev'`).Scan(&r)
	return r, err
}

// ---- window ----

type window struct {
	from, to *time.Time
	loc      *time.Location
}

// cond builds the overlap predicate on alias o. Timed rows compare instants; all-day rows
// compare dates in the user's zone (a window covers the local days from the day of `from`
// to the day of the last instant before `to`).
func (w window) cond() (string, []any) {
	var timed, allday []string
	var ta, aa []any
	if w.to != nil {
		timed = append(timed, "o.start_utc < ?")
		ta = append(ta, w.to.Unix())
		last := w.to.Add(-time.Nanosecond).In(w.loc).Format(time.DateOnly)
		allday = append(allday, "o.start_date <= ?")
		aa = append(aa, last)
	}
	if w.from != nil {
		f := w.from.Unix()
		timed = append(timed, "(o.end_utc > ? OR (o.end_utc = o.start_utc AND o.start_utc >= ?))")
		ta = append(ta, f, f)
		allday = append(allday, "o.end_date > ?")
		aa = append(aa, w.from.In(w.loc).Format(time.DateOnly))
	}
	if len(timed) == 0 {
		return "1=1", nil
	}
	t := "(o.all_day=0 AND " + strings.Join(timed, " AND ") + ")"
	a := "(o.all_day=1 AND " + strings.Join(allday, " AND ") + ")"
	return "(" + t + " OR " + a + ")", append(ta, aa...)
}

const occSelect = `SELECT o.event_id, o.rkey, o.all_day, o.start_utc, o.end_utc, o.start_date, o.end_date,
 e.rev, e.calendar_id, c.account_id, COALESCE(c.color_override, a.color), e.title, COALESCE(e.location,''),
 e.conf_url, e.conf_kind, e.self_response, e.status, e.busy, e.kind, e.series_id IS NOT NULL,
 e.can_edit, e.can_rsvp, e.can_add_conf, COALESCE(e.tzid, c.tzid, 'UTC'),
 e.organizer_email, e.organizer_name, e.is_organizer
 FROM occurrences o
 JOIN events e ON e.id = o.event_id
 JOIN calendars c ON c.id = e.calendar_id
 JOIN accounts a ON a.id = c.account_id`

const liveCond = `e.deleted_at IS NULL AND e.status <> 'cancelled'`

type occRow struct {
	AgendaItem
	startUnix int64
	day       string
}

func joinKind(k string) string {
	switch k {
	case "meet", "zoom", "teams":
		return k
	}
	return "other"
}

func scanOcc(rs *sql.Rows, loc *time.Location) (occRow, error) {
	var r occRow
	var rkey, su, eu int64
	var allDay, busy, series, ce, cr, ca bool
	var sd, ed, confURL, confKind, resp, orgEmail, orgName sql.NullString
	var orgSelf bool
	var kind string
	if err := rs.Scan(&r.EventID, &rkey, &allDay, &su, &eu, &sd, &ed, &r.Version, &r.CalendarID, &r.AccountID, &r.Color,
		&r.Title, &r.Location, &confURL, &confKind, &resp, &r.Status, &busy, &kind, &series, &ce, &cr, &ca, &r.TZID, &orgEmail, &orgName, &orgSelf); err != nil {
		return r, err
	}
	r.ID = fmt.Sprintf("%s:%d", r.EventID, rkey)
	r.AllDay, r.Busy, r.CanEdit, r.CanRSVP, r.CanAddConference = allDay, busy, ce, cr, ca
	r.Recurring = series || kind == "master" || kind == "exception" || kind == "instance"
	if confURL.Valid && confURL.String != "" {
		r.Join = &Join{URL: confURL.String, Kind: joinKind(confKind.String)}
	}
	if resp.Valid && resp.String != "" {
		v := resp.String
		r.MyResponse = &v
	}
	if orgEmail.Valid && orgEmail.String != "" {
		r.Organizer = &Person{Email: orgEmail.String, Name: orgName.String, IsSelf: orgSelf}
	}
	r.startUnix = su
	if allDay {
		r.Date, r.EndDate, r.TZID = sd.String, ed.String, ""
		r.day = sd.String
	} else {
		st, en := time.Unix(su, 0).UTC(), time.Unix(eu, 0).UTC()
		r.Start, r.End = &st, &en
		r.day = st.In(loc).Format(time.DateOnly)
	}
	return r, nil
}

// ---- agenda ----

type AgendaResult struct {
	Rev   int64        `json:"rev"`
	From  time.Time    `json:"from"`
	To    time.Time    `json:"to"`
	Items []AgendaItem `json:"items"`
}

// Rev returns the current global revision.
func (s *Store) Rev(ctx context.Context) (int64, error) { return s.DB.Rev(ctx) }

// agenda returns occurrences overlapping [from,to) on visible calendars, optionally limited
// to calendars. Order: by local day, all-day first, then start instant, title, id.
func (s *Store) agenda(ctx context.Context, from, to time.Time, calendars []string) (AgendaResult, error) {
	tx, err := s.readTx(ctx)
	if err != nil {
		return AgendaResult{}, err
	}
	defer tx.Rollback()
	res := AgendaResult{From: from.UTC(), To: to.UTC(), Items: []AgendaItem{}}
	if res.Rev, err = rev(ctx, tx); err != nil {
		return res, err
	}
	loc := userLoc(ctx, tx)
	cond, args := window{&from, &to, loc}.cond()
	q := occSelect + ` WHERE c.visible=1 AND ` + liveCond + ` AND ` + cond
	if len(calendars) > 0 {
		q += ` AND e.calendar_id IN (` + placeholders(len(calendars)) + `)`
		for _, c := range calendars {
			args = append(args, c)
		}
	}
	rs, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return res, err
	}
	defer rs.Close()
	var rows []occRow
	for rs.Next() {
		r, err := scanOcc(rs, loc)
		if err != nil {
			return res, err
		}
		rows = append(rows, r)
	}
	if err := rs.Err(); err != nil {
		return res, err
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.day != b.day {
			return a.day < b.day
		}
		if a.AllDay != b.AllDay {
			return a.AllDay
		}
		if a.startUnix != b.startUnix {
			return a.startUnix < b.startUnix
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.ID < b.ID
	})
	for _, r := range rows {
		res.Items = append(res.Items, r.AgendaItem)
	}
	return res, nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// ---- event detail ----

// event loads one event. id is an eventId or an occurrence id (eventId:rkey); a bare eventId
// resolves to the occurrence nearest to now (ongoing/next, else the latest past one).
func (s *Store) event(ctx context.Context, id string) (Event, error) {
	tx, err := s.readTx(ctx)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	loc := userLoc(ctx, tx)

	eid, rk, hasKey := id, int64(0), false
	if i := strings.LastIndex(id, ":"); i > 0 {
		var n int64
		if _, err := fmt.Sscanf(id[i+1:], "%d", &n); err == nil && fmt.Sprint(n) == id[i+1:] {
			eid, rk, hasKey = id[:i], n, true
		}
	}
	var item *occRow
	pick := func(where string, args ...any) error {
		rs, err := tx.QueryContext(ctx, occSelect+` WHERE `+where, args...)
		if err != nil {
			return err
		}
		defer rs.Close()
		if rs.Next() {
			r, err := scanOcc(rs, loc)
			if err != nil {
				return err
			}
			item = &r
		}
		return rs.Err()
	}
	base := `o.event_id=? AND e.deleted_at IS NULL`
	switch {
	case hasKey:
		err = pick(base+` AND o.rkey=?`, eid, rk)
	default:
		nowU := s.now().Unix()
		if err = pick(base+` AND o.end_utc >= ? ORDER BY o.start_utc LIMIT 1`, eid, nowU); err == nil && item == nil {
			err = pick(base+` ORDER BY o.start_utc DESC LIMIT 1`, eid)
		}
	}
	if err != nil {
		return Event{}, err
	}
	if item == nil && (!hasKey || rk == 0) {
		// no occurrence rows (outside the materialised window): fall back to the event's own times
		item, err = s.ownTimes(ctx, tx, eid, loc)
		if err != nil {
			return Event{}, err
		}
	}
	if item == nil {
		return Event{}, ErrNotFound
	}
	ev := Event{AgendaItem: item.AgendaItem}
	var desc, orgEmail, orgName, rrule, calName, acctName, acctEmail sql.NullString
	var upd sql.NullInt64
	var series sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT e.description, e.organizer_email, e.organizer_name, e.remote_updated_at, e.series_id,
	  COALESCE(e.rrule,''), c.name, a.display_name, a.email FROM events e JOIN calendars c ON c.id=e.calendar_id JOIN accounts a ON a.id=c.account_id WHERE e.id=?`,
		item.EventID).Scan(&desc, &orgEmail, &orgName, &upd, &series, &rrule, &calName, &acctName, &acctEmail)
	if err != nil {
		return Event{}, err
	}
	ev.Description = desc.String
	_, _ = orgEmail, orgName // the organizer already travels on the AgendaItem
	if upd.Valid {
		t := time.Unix(upd.Int64, 0).UTC()
		ev.UpdatedAt = &t
	}
	ev.CalendarName = calName.String
	ev.AccountName = acctName.String
	if ev.AccountName == "" {
		ev.AccountName = acctEmail.String
	}
	rr := rrule.String
	if rr == "" && series.Valid {
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE(rrule,'') FROM events WHERE id=?`, series.String).Scan(&rr)
	}
	if rr != "" {
		st := time.Time{}
		if item.Start != nil {
			l, _ := tz.Load(item.TZID)
			st = item.Start.In(l)
		} else if d, err := time.Parse(time.DateOnly, item.Date); err == nil {
			st = d
		}
		ev.Recurrence = &Recurrence{RRule: rr, Summary: Summarise(rr, st)}
	}
	rs, err := tx.QueryContext(ctx, `SELECT email, COALESCE(name,''), response, role, is_self, is_organizer FROM attendees WHERE event_id=?
	  ORDER BY is_organizer DESC, is_self DESC, COALESCE(NULLIF(name,''), email), email`, item.EventID)
	if err != nil {
		return Event{}, err
	}
	defer rs.Close()
	for rs.Next() {
		var a Attendee
		var role string
		if err := rs.Scan(&a.Email, &a.Name, &a.Response, &role, &a.IsSelf, &a.IsOrganizer); err != nil {
			return Event{}, err
		}
		a.Optional = role == "optional"
		ev.Attendees = append(ev.Attendees, a)
	}
	if err := rs.Err(); err != nil {
		return Event{}, err
	}
	rs.Close()
	var rk2 int64
	fmt.Sscanf(item.ID[strings.LastIndex(item.ID, ":")+1:], "%d", &rk2)
	var rem sql.NullInt64
	// an override on rkey 0 applies to every occurrence of a series unless the occurrence has its own
	if err := tx.QueryRowContext(ctx, `SELECT minutes_before FROM reminder_overrides WHERE event_id=? AND rkey IN (?, 0) ORDER BY rkey DESC LIMIT 1`, item.EventID, rk2).Scan(&rem); err == nil && rem.Valid {
		m := int(rem.Int64)
		ev.ReminderMin = &m
	}
	return ev, nil
}

// ownTimes builds an item from the events row itself (rkey 0).
func (s *Store) ownTimes(ctx context.Context, tx *sql.Tx, eid string, loc *time.Location) (*occRow, error) {
	q := strings.Replace(occSelect, "FROM occurrences o\n JOIN events e ON e.id = o.event_id",
		"FROM (SELECT id AS event_id, 0 AS rkey, all_day, COALESCE(start_utc,0) AS start_utc, COALESCE(end_utc,0) AS end_utc, start_date, end_date FROM events WHERE id=?) o\n JOIN events e ON e.id = o.event_id", 1)
	rs, err := tx.QueryContext(ctx, q+` WHERE e.deleted_at IS NULL`, eid)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	if !rs.Next() {
		return nil, rs.Err()
	}
	r, err := scanOcc(rs, loc)
	return &r, err
}

// ---- event list / search ----

type ListQuery struct {
	From, To *time.Time
	Calendar string
	Q        string
	Cursor   string
	Limit    int
}

type cursor struct {
	S int64  `json:"s"`
	E string `json:"e"`
	R int64  `json:"r"`
}

// MatchExpr converts free text into an FTS5 MATCH expression: every word becomes a quoted
// prefix term, all ANDed. Returns "" when the text holds no searchable word.
func MatchExpr(q string) string {
	words := strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	for i, w := range words {
		words[i] = `"` + w + `"*`
	}
	return strings.Join(words, " ")
}

// list returns events (as occurrences, ascending by start) for a window and/or a text query.
// With q, each event appears once (its first occurrence inside the window).
func (s *Store) list(ctx context.Context, lq ListQuery) (EventPage, error) {
	page := EventPage{Items: []Event{}}
	limit := lq.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	tx, err := s.readTx(ctx)
	if err != nil {
		return page, err
	}
	loc := userLoc(ctx, tx)
	cond, args := window{lq.From, lq.To, loc}.cond()
	where := []string{liveCond, cond}
	if lq.Calendar != "" {
		where = append(where, "e.calendar_id = ?")
		args = append(args, lq.Calendar)
	} else {
		where = append(where, "c.visible = 1")
	}
	if lq.Q != "" {
		m := MatchExpr(lq.Q)
		if m == "" {
			tx.Rollback()
			return page, &ValidationError{"q", "query must contain a letter or digit"}
		}
		where = append(where, "e.rowid IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)")
		args = append(args, m)
		// first occurrence per event within the window
		wc, wa := window{lq.From, lq.To, loc}.cond()
		where = append(where, `o.rkey = (SELECT o.rkey FROM occurrences o WHERE o.event_id = e.id AND `+wc+` ORDER BY o.start_utc LIMIT 1)`)
		args = append(args, wa...)
	}
	if lq.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(lq.Cursor)
		var c cursor
		if err != nil || json.Unmarshal(raw, &c) != nil {
			tx.Rollback()
			return page, &ValidationError{"cursor", "invalid cursor"}
		}
		where = append(where, "(o.start_utc, o.event_id, o.rkey) > (?,?,?)")
		args = append(args, c.S, c.E, c.R)
	}
	args = append(args, limit+1)
	rs, err := tx.QueryContext(ctx, occSelect+` WHERE `+strings.Join(where, " AND ")+` ORDER BY o.start_utc, o.event_id, o.rkey LIMIT ?`, args...)
	if err != nil {
		tx.Rollback()
		return page, err
	}
	var rows []occRow
	for rs.Next() {
		r, err := scanOcc(rs, loc)
		if err != nil {
			rs.Close()
			tx.Rollback()
			return page, err
		}
		rows = append(rows, r)
	}
	err = rs.Err()
	rs.Close()
	tx.Rollback()
	if err != nil {
		return page, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		var rk int64
		fmt.Sscanf(last.ID[strings.LastIndex(last.ID, ":")+1:], "%d", &rk)
		b, _ := json.Marshal(cursor{last.startUnix, last.EventID, rk})
		c := base64.RawURLEncoding.EncodeToString(b)
		page.NextCursor = &c
	}
	for _, r := range rows {
		ev, err := s.Event(ctx, r.ID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return page, err
		}
		page.Items = append(page.Items, ev)
	}
	return page, nil
}
