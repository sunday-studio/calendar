package agenda

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"calendar/server/internal/db"
	"calendar/server/internal/recur"
	"calendar/server/internal/tz"
)

// Palette is the set of colour keys an account or a calendar override may use.
var Palette = []string{"cobalt", "rose", "ochre", "mauve", "verdigris", "slate", "moss", "clay"}

func validColor(c string) bool {
	for _, p := range Palette {
		if p == c {
			return true
		}
	}
	return false
}

const calSelect = `SELECT c.id, c.account_id, c.name, COALESCE(c.color_override, a.color), c.color_override, COALESCE(c.tzid,'UTC'), c.access, c.is_primary, c.visible
 FROM calendars c JOIN accounts a ON a.id = c.account_id`

func scanCal(rs interface{ Scan(...any) error }) (Calendar, error) {
	var c Calendar
	var ov sql.NullString
	err := rs.Scan(&c.ID, &c.AccountID, &c.Name, &c.Color, &ov, &c.TZID, &c.Access, &c.IsPrimary, &c.Visible)
	if ov.Valid && ov.String != "" {
		v := ov.String
		c.ColorOverride = &v
	}
	return c, err
}

// Calendars lists every calendar grouped by account (account creation order), primary first.
func (s *Store) Calendars(ctx context.Context) ([]Calendar, error) {
	rs, err := s.DB.R.QueryContext(ctx, calSelect+` ORDER BY a.created_at, a.id, c.is_primary DESC, c.name COLLATE NOCASE, c.id`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	out := []Calendar{}
	for rs.Next() {
		c, err := scanCal(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rs.Err()
}

type CalendarPatch struct {
	Visible       *bool
	ColorOverride **string // non-nil = present; inner nil = clear
}

// PatchCalendar applies visible/colorOverride. It returns the calendar, whether anything
// changed (rev bumped) and whether it just became visible.
func (s *Store) PatchCalendar(ctx context.Context, id string, p CalendarPatch) (cal Calendar, becameVisible bool, err error) {
	if p.Visible == nil && p.ColorOverride == nil {
		return cal, false, &ValidationError{"", "visible or colorOverride required"}
	}
	if p.ColorOverride != nil && *p.ColorOverride != nil && !validColor(**p.ColorOverride) {
		return cal, false, &ValidationError{"colorOverride", "must be one of " + strings.Join(Palette, ", ") + " or null"}
	}
	err = s.DB.Write(ctx, func(tx *db.Tx) error {
		var vis bool
		var ov sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT visible, color_override FROM calendars WHERE id=?`, id).Scan(&vis, &ov); err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			return err
		}
		newVis, newOv := vis, ov
		if p.Visible != nil {
			newVis = *p.Visible
		}
		if p.ColorOverride != nil {
			if *p.ColorOverride == nil {
				newOv = sql.NullString{}
			} else {
				newOv = sql.NullString{String: **p.ColorOverride, Valid: true}
			}
		}
		if newVis != vis || newOv != ov {
			if _, err := tx.ExecContext(ctx, `UPDATE calendars SET visible=?, color_override=? WHERE id=?`, newVis, newOv, id); err != nil {
				return err
			}
			if err := tx.Change(ctx, "calendar", id, "upsert"); err != nil {
				return err
			}
			becameVisible = newVis && !vis
		}
		return nil
	})
	if err != nil {
		return cal, false, err
	}
	cal, err = scanCal(s.DB.R.QueryRowContext(ctx, calSelect+` WHERE c.id=?`, id))
	return cal, becameVisible, err
}

// AccountOf returns the account id owning a calendar.
func (s *Store) AccountOf(ctx context.Context, calID string) (string, error) {
	var a string
	err := s.DB.R.QueryRowContext(ctx, `SELECT account_id FROM calendars WHERE id=?`, calID).Scan(&a)
	return a, err
}

// ---- settings ----

var defaultSettings = Settings{
	WeekStart:    1,
	WorkingHours: WorkingHours{Start: "09:00", End: "17:30", Days: []int{1, 2, 3, 4, 5}},
}

func decode(raw string, v any) bool { return json.Unmarshal([]byte(raw), v) == nil }

// Settings returns stored settings merged over defaults.
func (s *Store) Settings(ctx context.Context) (Settings, error) { return loadSettings(ctx, s.DB.R) }

func loadSettings(ctx context.Context, q querier) (Settings, error) {
	out := defaultSettings
	out.WorkingHours.Days = append([]int(nil), defaultSettings.WorkingHours.Days...)
	ut := DefaultUserTZ
	out.UserTZID = &ut
	out.AlertDefaults = &AlertDefaults{AllDayTime: DefaultAllDayTime}
	ten := 10
	out.AlertDefaults.Timed = &ten
	out.MorningSummary = &MorningSummary{Time: "07:30"}
	out.LunchBreak = &LunchBreak{Start: "12:00", End: "13:00"}
	rs, err := q.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return out, err
	}
	defer rs.Close()
	for rs.Next() {
		var k, v string
		if err := rs.Scan(&k, &v); err != nil {
			return out, err
		}
		switch k {
		case "default_calendar":
			var x string
			if decode(v, &x) && x != "" {
				out.DefaultCalendarID = &x
			}
		case "week_start":
			var x int
			if decode(v, &x) {
				out.WeekStart = x
			}
		case "secondary_tz":
			var x string
			if decode(v, &x) && x != "" {
				out.SecondaryTZID = &x
			}
		case "user_tz":
			var x string
			if decode(v, &x) && x != "" {
				out.UserTZID = &x
			} else if v != "" && !strings.HasPrefix(v, "\"") {
				out.UserTZID = &v
			}
		case "working_hours":
			var x WorkingHours
			if decode(v, &x) && x.Days != nil {
				out.WorkingHours = x
			}
		case "lunch_break":
			var x LunchBreak
			if decode(v, &x) {
				a, okA := validHHMM(x.Start)
				b, okB := validHHMM(x.End)
				if !okA || !okB || a >= b {
					x.Start, x.End = "12:00", "13:00"
				}
				out.LunchBreak = &x
			}
		case "morning_summary":
			var x MorningSummary
			if decode(v, &x) {
				if _, ok := validHHMM(x.Time); !ok {
					x.Time = "07:30"
				}
				out.MorningSummary = &x
			}
		case "alert_defaults":
			var x AlertDefaults
			if decode(v, &x) {
				if _, ok := validHHMM(x.AllDayTime); !ok {
					x.AllDayTime = DefaultAllDayTime
				}
				out.AlertDefaults = &x
			}
		}
	}
	return out, rs.Err()
}

func validHHMM(v string) (int, bool) {
	t, err := time.Parse("15:04", v)
	if err != nil || len(v) != 5 {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func validZone(name string) bool {
	if name == "" {
		return false
	}
	_, w := tz.Load(name)
	return w == ""
}

// SettingsInput is the PUT body. defaultCalendarId/secondaryTzid null (or absent) clear them;
// userTzid/alertDefaults absent keep the current value.
type SettingsInput struct {
	DefaultCalendarID *string         `json:"defaultCalendarId"`
	WeekStart         *int            `json:"weekStart"`
	SecondaryTZID     *string         `json:"secondaryTzid"`
	UserTZID          *string         `json:"userTzid"`
	WorkingHours      *WorkingHours   `json:"workingHours"`
	AlertDefaults     *AlertDefaults  `json:"alertDefaults"`
	MorningSummary    *MorningSummary `json:"morningSummary"`
	LunchBreak        *LunchBreak     `json:"lunchBreak"`
}

// DefaultAllDayTime is when all-day reminders fire unless settings say otherwise.
const DefaultAllDayTime = "09:00"

func validateAlert(a *AlertDefaults) error {
	if a.AllDayTime != "" {
		if _, ok := validHHMM(a.AllDayTime); !ok {
			return &ValidationError{"alertDefaults.allDayTime", "must be HH:MM"}
		}
	}
	for f, v := range map[string]*int{"alertDefaults.timed": a.Timed, "alertDefaults.allDay": a.AllDay} {
		if v != nil && (*v < 0 || *v > 40320) {
			return &ValidationError{f, "must be null or minutes between 0 and 40320"}
		}
	}
	return nil
}

// PutSettings validates and stores settings. A user_tz change re-times all-day occurrence
// ordering columns in the same transaction.
func (s *Store) PutSettings(ctx context.Context, in SettingsInput) (Settings, error) {
	switch {
	case in.WeekStart == nil:
		return Settings{}, &ValidationError{"weekStart", "required"}
	case *in.WeekStart < 0 || *in.WeekStart > 6:
		return Settings{}, &ValidationError{"weekStart", "must be 0 (Sunday) to 6"}
	case in.WorkingHours == nil:
		return Settings{}, &ValidationError{"workingHours", "required"}
	}
	wh := *in.WorkingHours
	a, okA := validHHMM(wh.Start)
	b, okB := validHHMM(wh.End)
	switch {
	case !okA:
		return Settings{}, &ValidationError{"workingHours.start", "must be HH:MM"}
	case !okB:
		return Settings{}, &ValidationError{"workingHours.end", "must be HH:MM"}
	case a >= b:
		return Settings{}, &ValidationError{"workingHours.end", "must be after start"}
	}
	seen := map[int]bool{}
	for _, d := range wh.Days {
		if d < 0 || d > 6 || seen[d] {
			return Settings{}, &ValidationError{"workingHours.days", "must be unique integers 0 (Sunday) to 6"}
		}
		seen[d] = true
	}
	if wh.Days == nil {
		wh.Days = []int{}
	}
	if in.SecondaryTZID != nil && !validZone(*in.SecondaryTZID) {
		return Settings{}, &ValidationError{"secondaryTzid", "unknown IANA time zone"}
	}
	if in.UserTZID != nil && !validZone(*in.UserTZID) {
		return Settings{}, &ValidationError{"userTzid", "unknown IANA time zone"}
	}
	if in.AlertDefaults != nil {
		if err := validateAlert(in.AlertDefaults); err != nil {
			return Settings{}, err
		}
	}
	if in.LunchBreak != nil {
		a, okA := validHHMM(in.LunchBreak.Start)
		b, okB := validHHMM(in.LunchBreak.End)
		switch {
		case !okA:
			return Settings{}, &ValidationError{"lunchBreak.start", "must be HH:MM"}
		case !okB:
			return Settings{}, &ValidationError{"lunchBreak.end", "must be HH:MM"}
		case a >= b:
			return Settings{}, &ValidationError{"lunchBreak.end", "must be after start"}
		}
	}
	if in.MorningSummary != nil {
		if _, ok := validHHMM(in.MorningSummary.Time); !ok {
			return Settings{}, &ValidationError{"morningSummary.time", "must be HH:MM"}
		}
	}
	err := s.DB.Write(ctx, func(tx *db.Tx) error {
		if in.DefaultCalendarID != nil {
			var acc string
			if err := tx.QueryRowContext(ctx, `SELECT access FROM calendars WHERE id=?`, *in.DefaultCalendarID).Scan(&acc); err != nil {
				if err == sql.ErrNoRows {
					return &ValidationError{"defaultCalendarId", "no such calendar"}
				}
				return err
			}
			if acc != "owner" && acc != "writer" {
				return &ValidationError{"defaultCalendarId", "calendar is read-only"}
			}
		}
		old, err := loadSettings(ctx, tx)
		if err != nil {
			return err
		}
		put := func(key string, v any) error {
			if v == nil {
				_, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, key)
				return err
			}
			b, _ := json.Marshal(v)
			_, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, string(b))
			return err
		}
		oldRaw := map[string]string{}
		rs, err := tx.QueryContext(ctx, `SELECT key, value FROM settings`)
		if err != nil {
			return err
		}
		for rs.Next() {
			var k, v string
			rs.Scan(&k, &v)
			oldRaw[k] = v
		}
		rs.Close()

		var dc, sec any
		if in.DefaultCalendarID != nil {
			dc = *in.DefaultCalendarID
		}
		if in.SecondaryTZID != nil {
			sec = *in.SecondaryTZID
		}
		newUser := *old.UserTZID
		if in.UserTZID != nil {
			newUser = *in.UserTZID
		}
		alert := old.AlertDefaults
		if in.AlertDefaults != nil {
			a := *in.AlertDefaults
			if a.AllDayTime == "" { // older clients do not send it: keep what is set
				a.AllDayTime = old.AlertDefaults.AllDayTime
			}
			alert = &a
		}
		lunch := old.LunchBreak
		if in.LunchBreak != nil {
			lunch = in.LunchBreak
		}
		morning := old.MorningSummary
		if in.MorningSummary != nil {
			morning = in.MorningSummary
		}
		for _, kv := range []struct {
			k string
			v any
		}{{"lunch_break", lunch}, {"morning_summary", morning}, {"default_calendar", dc}, {"week_start", *in.WeekStart}, {"secondary_tz", sec}, {"user_tz", newUser}, {"working_hours", wh}, {"alert_defaults", alert}} {
			if err := put(kv.k, kv.v); err != nil {
				return err
			}
		}
		// anything different from before? (compare re-read values)
		cur, err := loadSettings(ctx, tx)
		if err != nil {
			return err
		}
		ob, _ := json.Marshal(old)
		cb, _ := json.Marshal(cur)
		_ = oldRaw
		if string(ob) == string(cb) {
			return nil
		}
		if *old.UserTZID != newUser {
			loc, _ := tz.Load(newUser)
			if err := recur.RetimeAllDay(ctx, tx, loc); err != nil {
				return fmt.Errorf("retime all-day: %w", err)
			}
		}
		return tx.Change(ctx, "settings", "settings", "upsert")
	})
	if err != nil {
		return Settings{}, err
	}
	return s.Settings(ctx)
}

// ---- free/busy ----

// FreeBusy returns merged busy intervals clipped to [from,to) from visible calendars:
// busy events only, excluding events the user declined.
func (s *Store) FreeBusy(ctx context.Context, from, to time.Time) (FreeBusy, error) {
	out := FreeBusy{From: from.UTC(), To: to.UTC(), Busy: []Interval{}}
	f, t := from.Unix(), to.Unix()
	rs, err := s.DB.R.QueryContext(ctx, `SELECT o.start_utc, o.end_utc FROM occurrences o
	 JOIN events e ON e.id=o.event_id JOIN calendars c ON c.id=e.calendar_id
	 WHERE c.visible=1 AND `+liveCond+` AND e.busy=1 AND COALESCE(e.self_response,'') <> 'declined'
	 AND o.start_utc < ? AND o.end_utc > ? ORDER BY o.start_utc`, t, f)
	if err != nil {
		return out, err
	}
	defer rs.Close()
	var cs, ce int64
	open := false
	flush := func() {
		if open {
			out.Busy = append(out.Busy, Interval{time.Unix(cs, 0).UTC(), time.Unix(ce, 0).UTC()})
		}
	}
	for rs.Next() {
		var a, b int64
		if err := rs.Scan(&a, &b); err != nil {
			return out, err
		}
		if a < f {
			a = f
		}
		if b > t {
			b = t
		}
		switch {
		case !open:
			cs, ce, open = a, b, true
		case a <= ce:
			if b > ce {
				ce = b
			}
		default:
			flush()
			cs, ce = a, b
		}
	}
	flush()
	return out, rs.Err()
}
