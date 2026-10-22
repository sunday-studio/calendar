package events

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/teambition/rrule-go"

	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
	"calendar/server/internal/tz"
)

// When is the wire form of an event time (openapi When). Start is local wall time in TZID.
type When struct {
	AllDay      bool   `json:"allDay"`
	Start       string `json:"start,omitempty"`
	TZID        string `json:"tzid,omitempty"`
	DurationMin int    `json:"durationMin,omitempty"`
	Date        string `json:"date,omitempty"`
	EndDate     string `json:"endDate,omitempty"`
}

type RecurrenceIn struct {
	RRule string `json:"rrule"`
}

// CreateIn is EventInput.
type CreateIn struct {
	CalendarID  string        `json:"calendarId"`
	Title       string        `json:"title"`
	When        When          `json:"when"`
	Location    string        `json:"location,omitempty"`
	Description string        `json:"description,omitempty"`
	Attendees   []string      `json:"attendees"`
	Recurrence  *RecurrenceIn `json:"recurrence"`
	Conference  string        `json:"conference,omitempty"`
	ReminderMin *int          `json:"reminderMin"`
}

// PatchIn is Patch: a field that is absent is untouched; null clears it.
type PatchIn struct {
	Title       *string
	When        *When
	Location    provider.Opt[string]
	Description provider.Opt[string]
	Attendees   provider.Opt[[]string]
	Recurrence  provider.Opt[RecurrenceIn]
	Conference  provider.Opt[string]
	ReminderMin provider.Opt[int]
}

func decodeOpt[T any](raw json.RawMessage) (provider.Opt[T], error) {
	if string(raw) == "null" {
		return provider.Clear[T](), nil
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return provider.Opt[T]{}, err
	}
	return provider.Some(v), nil
}

func (p *PatchIn) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for k, raw := range m {
		var err error
		switch k {
		case "title":
			var s string
			if string(raw) == "null" {
				return fmt.Errorf("title cannot be null")
			}
			if err = json.Unmarshal(raw, &s); err == nil {
				p.Title = &s
			}
		case "when":
			if string(raw) == "null" {
				return fmt.Errorf("when cannot be null")
			}
			var w When
			if err = json.Unmarshal(raw, &w); err == nil {
				p.When = &w
			}
		case "location":
			p.Location, err = decodeOpt[string](raw)
		case "description":
			p.Description, err = decodeOpt[string](raw)
		case "attendees":
			p.Attendees, err = decodeOpt[[]string](raw)
		case "recurrence":
			p.Recurrence, err = decodeOpt[RecurrenceIn](raw)
		case "conference":
			p.Conference, err = decodeOpt[string](raw)
		case "reminderMin":
			p.ReminderMin, err = decodeOpt[int](raw)
		default:
			// unknown fields are ignored (additive changes within v1)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	return nil
}

// Empty reports whether nothing at all was set.
func (p PatchIn) Empty() bool {
	return p.Title == nil && p.When == nil && !p.Location.Set && !p.Description.Set && !p.Attendees.Set &&
		!p.Recurrence.Set && !p.Conference.Set && !p.ReminderMin.Set
}

const (
	maxAttendees = 200
	maxTitle     = 1024
	maxText      = 16 * 1024
	maxReminder  = 40320 // 4 weeks
)

func loadLoc(name string) (*time.Location, bool) {
	loc, warn := tz.Load(name)
	return loc, warn == ""
}

func parseLocal(s string, loc *time.Location) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("must be a local time like 2026-10-16T13:00:00 (no zone suffix)")
}

func toDate(t time.Time) domain.Date { return domain.Date{Y: t.Year(), M: int(t.Month()), D: t.Day()} }

// resolveWhen converts the wire When to a complete domain.When. base supplies defaults for
// omitted tzid/duration (the event or occurrence being edited); defTZ is used when base is nil.
func resolveWhen(w When, base *domain.When, defTZ string, defDur time.Duration) (domain.When, *Error) {
	if w.AllDay {
		sd, err := time.Parse(time.DateOnly, w.Date)
		if err != nil {
			return domain.When{}, fail(KValidation, "when.date", "all-day events need when.date as YYYY-MM-DD")
		}
		ed := sd.AddDate(0, 0, 1)
		switch {
		case w.EndDate != "":
			if ed, err = time.Parse(time.DateOnly, w.EndDate); err != nil {
				return domain.When{}, fail(KValidation, "when.endDate", "must be YYYY-MM-DD")
			}
			if !ed.After(sd) {
				return domain.When{}, fail(KValidation, "when.endDate", "endDate is exclusive and must be after date")
			}
		case base != nil && base.AllDay:
			ed = sd.AddDate(0, 0, int(dayNum(base.EndDate)-dayNum(base.StartDate)))
		}
		if ed.Sub(sd) > 366*24*time.Hour {
			return domain.When{}, fail(KValidation, "when.endDate", "all-day events may span at most a year")
		}
		return domain.When{AllDay: true, StartDate: toDate(sd), EndDate: toDate(ed)}, nil
	}
	tzid := w.TZID
	if tzid == "" && base != nil && !base.AllDay {
		tzid = base.TZID
	}
	if tzid == "" {
		tzid = defTZ
	}
	loc, ok := loadLoc(tzid)
	if !ok {
		return domain.When{}, fail(KValidation, "when.tzid", "unknown time zone "+tzid)
	}
	if w.Start == "" {
		return domain.When{}, fail(KValidation, "when.start", "timed events need when.start")
	}
	st, err := parseLocal(w.Start, loc)
	if err != nil {
		return domain.When{}, fail(KValidation, "when.start", err.Error())
	}
	dur := defDur
	switch {
	case w.DurationMin < 0 || w.DurationMin > 366*24*60:
		return domain.When{}, fail(KValidation, "when.durationMin", "out of range")
	case w.DurationMin > 0:
		dur = time.Duration(w.DurationMin) * time.Minute
	case base != nil && !base.AllDay:
		dur = base.End.Sub(base.Start)
	}
	if dur <= 0 {
		dur = time.Hour
	}
	return domain.When{Start: st.UTC(), End: st.Add(dur).UTC(), TZID: tzid}, nil
}

func dayNum(d domain.Date) int64 {
	return time.Date(d.Y, time.Month(d.M), d.D, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

func cleanAttendees(in []string) ([]string, *Error) {
	if len(in) > maxAttendees {
		return nil, fail(KValidation, "attendees", "too many guests")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, a := range in {
		addr, err := mail.ParseAddress(strings.TrimSpace(a))
		if err != nil || addr.Address == "" {
			return nil, fail(KValidation, "attendees", "not an email address: "+short(a))
		}
		k := strings.ToLower(addr.Address)
		if !seen[k] {
			seen[k] = true
			out = append(out, addr.Address)
		}
	}
	return out, nil
}

func cleanRule(in RecurrenceIn) (string, *Error) {
	r := strings.TrimSpace(in.RRule)
	r = strings.TrimPrefix(strings.TrimPrefix(r, "RRULE:"), "rrule:")
	if r == "" {
		return "", fail(KValidation, "recurrence.rrule", "rrule is empty")
	}
	if _, err := rrule.StrToROption(r); err != nil {
		return "", fail(KValidation, "recurrence.rrule", "invalid RRULE: "+short(err.Error()))
	}
	return r, nil
}

func checkText(field, s string, max int) *Error {
	if len(s) > max {
		return fail(KValidation, field, "too long")
	}
	return nil
}

func checkReminder(m *int) *Error {
	if m != nil && (*m < 0 || *m > maxReminder) {
		return fail(KValidation, "reminderMin", fmt.Sprintf("must be between 0 and %d minutes", maxReminder))
	}
	return nil
}

func checkConference(v string) *Error {
	if v != "" && v != "meet" && v != "none" {
		return fail(KValidation, "conference", `must be "meet" or "none"`)
	}
	return nil
}
