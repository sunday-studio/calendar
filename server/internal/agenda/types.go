// Package agenda is the DB-backed read model behind /v1/agenda, /v1/events, /v1/calendars,
// /v1/settings and /v1/freebusy. Wire types live here (internal/api aliases them) so the
// package has no dependency on the HTTP layer.
package agenda

import "time"

type Join struct {
	URL  string `json:"url"`
	Kind string `json:"kind"` // meet|zoom|teams|other
}

// AgendaItem is what Home, Day, Week and widgets render.
// Timed: Start/End (UTC) + TZID. All-day: Date/EndDate (EndDate exclusive).
type AgendaItem struct {
	ID               string     `json:"id"`
	EventID          string     `json:"eventId"`
	Version          int64      `json:"version"`
	CalendarID       string     `json:"calendarId"`
	AccountID        string     `json:"accountId"`
	Color            string     `json:"color"`
	Title            string     `json:"title"`
	AllDay           bool       `json:"allDay"`
	Start            *time.Time `json:"start,omitempty"`
	End              *time.Time `json:"end,omitempty"`
	TZID             string     `json:"tzid,omitempty"`
	Date             string     `json:"date,omitempty"`
	EndDate          string     `json:"endDate,omitempty"`
	Location         string     `json:"location,omitempty"`
	Join             *Join      `json:"join,omitempty"`
	MyResponse       *string    `json:"myResponse,omitempty"` // needsAction|accepted|declined|tentative
	Status           string     `json:"status"`               // confirmed|tentative|cancelled
	Busy             bool       `json:"busy"`
	Recurring        bool       `json:"recurring"`
	CanEdit          bool       `json:"canEdit"`
	CanRSVP          bool       `json:"canRSVP"`
	CanAddConference bool       `json:"canAddConference"`
	// Organizer is who sent the invitation ("from Ana"); absent when unknown.
	Organizer *Person `json:"organizer,omitempty"`
}

type Attendee struct {
	Email       string `json:"email"`
	Name        string `json:"name,omitempty"`
	Response    string `json:"response"`
	Optional    bool   `json:"optional,omitempty"`
	IsSelf      bool   `json:"isSelf,omitempty"`
	IsOrganizer bool   `json:"isOrganizer,omitempty"`
}

type Person struct {
	Email  string `json:"email"`
	Name   string `json:"name,omitempty"`
	IsSelf bool   `json:"isSelf,omitempty"` // the account owner organises the event
}

type Recurrence struct {
	RRule   string `json:"rrule"`
	Summary string `json:"summary,omitempty"` // human text, e.g. "Weekly on Tue, Thu"
}

// Event is an AgendaItem plus detail fields.
type Event struct {
	AgendaItem
	Description  string      `json:"description,omitempty"`
	Attendees    []Attendee  `json:"attendees,omitempty"`
	Recurrence   *Recurrence `json:"recurrence,omitempty"`
	ReminderMin  *int        `json:"reminderMin"`
	UpdatedAt    *time.Time  `json:"updatedAt,omitempty"`
	CalendarName string      `json:"calendarName,omitempty"`
	AccountName  string      `json:"accountName,omitempty"`
}

type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

type Calendar struct {
	ID            string  `json:"id"`
	AccountID     string  `json:"accountId"`
	Name          string  `json:"name"`
	Color         string  `json:"color"`
	ColorOverride *string `json:"colorOverride"`
	TZID          string  `json:"tzid"`
	Access        string  `json:"access"` // owner|writer|reader|freebusy
	IsPrimary     bool    `json:"isPrimary"`
	Visible       bool    `json:"visible"`
}

type WorkingHours struct {
	Start string `json:"start"` // HH:MM
	End   string `json:"end"`
	Days  []int  `json:"days"` // 0 = Sunday
}

// AlertDefaults are default reminder lead times in minutes (null = no default alert).
//
// Timed leads count back from the start. An all-day event has no clock time, so its lead is
// counted in whole days from midnight of its first day and fires at AllDayTime ("HH:MM", default
// 09:00): AllDay 1440 = the previous day at 09:00, 2880 = two days before, 0 = the day itself at
// 09:00. A lead that is not a whole number of days is subtracted from midnight as it is.
type AlertDefaults struct {
	Timed      *int   `json:"timed"`
	AllDay     *int   `json:"allDay"`
	AllDayTime string `json:"allDayTime,omitempty"`
}

// MorningSummary configures the daily summary push (off by default).
type MorningSummary struct {
	Enabled bool   `json:"enabled"`
	Time    string `json:"time"` // HH:MM in the user's zone
}

// LunchBreak keeps free-time search away from a daily window (off by default).
type LunchBreak struct {
	Enabled bool   `json:"enabled"`
	Start   string `json:"start"` // HH:MM in the user's zone
	End     string `json:"end"`
}

type Settings struct {
	DefaultCalendarID *string         `json:"defaultCalendarId"`
	WeekStart         int             `json:"weekStart"`
	SecondaryTZID     *string         `json:"secondaryTzid"`
	UserTZID          *string         `json:"userTzid,omitempty"`
	WorkingHours      WorkingHours    `json:"workingHours"`
	AlertDefaults     *AlertDefaults  `json:"alertDefaults,omitempty"`
	MorningSummary    *MorningSummary `json:"morningSummary,omitempty"`
	LunchBreak        *LunchBreak     `json:"lunchBreak,omitempty"`
}

type Interval struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type FreeBusy struct {
	From time.Time  `json:"from"`
	To   time.Time  `json:"to"`
	Busy []Interval `json:"busy"`
}

// ValidationError maps to HTTP 400 `validation` with the offending field.
type ValidationError struct{ Field, Msg string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Msg }

// ErrNotFound is returned for unknown ids.
type notFound struct{}

func (notFound) Error() string { return "not found" }

var ErrNotFound error = notFound{}
