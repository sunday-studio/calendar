// Package domain holds provider-neutral calendar types (ENGINEERING §4.4).
package domain

import "time"

// Date is a civil date (no timezone), formatted YYYY-MM-DD.
type Date struct{ Y, M, D int }

func (d Date) String() string {
	return time.Date(d.Y, time.Month(d.M), d.D, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
}

func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, err
	}
	return Date{t.Year(), int(t.Month()), t.Day()}, nil
}

// When is a sum type: timed (UTC instants + tzid) or all-day (civil dates, end exclusive).
type When struct {
	AllDay     bool
	Start, End time.Time // !AllDay
	TZID       string    // !AllDay
	StartDate  Date      // AllDay, inclusive
	EndDate    Date      // AllDay, EXCLUSIVE
}

type Kind string

const (
	Single    Kind = "single"
	Master    Kind = "master"
	Exception Kind = "exception"
	Instance  Kind = "instance"
)

type Status string

const (
	Confirmed Status = "confirmed"
	Tentative Status = "tentative"
	Cancelled Status = "cancelled"
)

type Response string

const (
	NeedsAction       Response = "needsAction"
	Accepted          Response = "accepted"
	Declined          Response = "declined"
	TentativeResponse Response = "tentative"
)

type Person struct{ Email, Name string }

type Attendee struct {
	Email, Name string
	Response    Response
	Role        string // required|optional|resource
	IsSelf      bool
	IsOrganizer bool
}

type Recurrence struct {
	RRule   string
	RDates  []string
	EXDates []string
}

type Conference struct{ URL, Kind, Source string }

type EventCaps struct{ CanEdit, CanRSVP, CanAddConference bool }

type Event struct {
	ID, CalendarID, RemoteID, ICalUID string
	Kind                              Kind
	SeriesID                          *string
	RecurrenceKey                     *int64
	Title, Description, Location      string
	When                              When
	Recurrence                        *Recurrence
	Status                            Status
	Busy                              bool
	Organizer                         *Person
	Attendees                         []Attendee
	SelfResponse                      *Response
	Conference                        *Conference
	Caps                              EventCaps
	IsOrganizer                       bool // the account owner organises this event
	HTMLLink                          string
	RemoteSeriesID                    string // exception/instance -> remote id of its master
	RemoteUpdatedAt                   *time.Time
	ETag                              string // never leaves the server
	Rev                               int64  // exposed to clients as "version"
}

type Account struct {
	ID, Provider, ExternalID, Email, DisplayName string
	Color                                        string
	Status, StatusDetail                         string
	LastSyncAt, LastOKAt                         *time.Time
	CreatedAt                                    time.Time
}

type Calendar struct {
	ID, AccountID, RemoteID, Name string
	RemoteColor, ColorOverride    string
	TZID                          string
	Access                        string // owner|writer|reader|freebusy
	IsPrimary, Visible            bool
}
