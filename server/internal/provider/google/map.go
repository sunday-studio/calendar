package google

import (
	"sort"
	"strings"
	"time"

	"calendar/server/internal/domain"
)

type gTime struct {
	Date     string `json:"date"`
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type gPerson struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Self        bool   `json:"self"`
}

type gAttendee struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName"`
	ResponseStatus string `json:"responseStatus"`
	Optional       bool   `json:"optional"`
	Resource       bool   `json:"resource"`
	Self           bool   `json:"self"`
	Organizer      bool   `json:"organizer"`
}

type gEvent struct {
	ID              string `json:"id"`
	ICalUID         string `json:"iCalUID"`
	Etag            string `json:"etag"`
	Status          string `json:"status"`
	Summary         string `json:"summary"`
	Description     string `json:"description"`
	Location        string `json:"location"`
	HTMLLink        string `json:"htmlLink"`
	HangoutLink     string `json:"hangoutLink"`
	Transparency    string `json:"transparency"`
	Updated         string `json:"updated"`
	EventType       string `json:"eventType"`
	WorkingLocation *struct {
		Type           string `json:"type"`
		CustomLocation *struct {
			Label string `json:"label"`
		} `json:"customLocation"`
		OfficeLocation *struct {
			Label string `json:"label"`
		} `json:"officeLocation"`
	} `json:"workingLocationProperties"`
	Locked           bool        `json:"locked"`
	GuestsCanModify  bool        `json:"guestsCanModify"`
	RecurringEventID string      `json:"recurringEventId"`
	Recurrence       []string    `json:"recurrence"`
	Start            *gTime      `json:"start"`
	End              *gTime      `json:"end"`
	OriginalStart    *gTime      `json:"originalStartTime"`
	Organizer        *gPerson    `json:"organizer"`
	Attendees        []gAttendee `json:"attendees"`
	ConferenceData   *struct {
		EntryPoints []struct {
			EntryPointType string `json:"entryPointType"`
			URI            string `json:"uri"`
		} `json:"entryPoints"`
	} `json:"conferenceData"`
}

func parseWhen(s, e *gTime, calTZ string) (domain.When, bool) {
	if s == nil {
		return domain.When{}, false
	}
	if s.Date != "" {
		sd, err := domain.ParseDate(s.Date)
		if err != nil {
			return domain.When{}, false
		}
		ed := sd
		if e != nil && e.Date != "" {
			if x, err := domain.ParseDate(e.Date); err == nil {
				ed = x
			}
		}
		if ed.String() <= sd.String() { // exclusive end must be after start
			t := time.Date(sd.Y, time.Month(sd.M), sd.D+1, 0, 0, 0, 0, time.UTC)
			ed = domain.Date{Y: t.Year(), M: int(t.Month()), D: t.Day()}
		}
		return domain.When{AllDay: true, StartDate: sd, EndDate: ed}, true
	}
	st, err := time.Parse(time.RFC3339, s.DateTime)
	if err != nil {
		return domain.When{}, false
	}
	en := st
	if e != nil && e.DateTime != "" {
		if x, err := time.Parse(time.RFC3339, e.DateTime); err == nil {
			en = x
		}
	}
	tz := s.TimeZone
	if tz == "" {
		tz = calTZ
	}
	if tz == "" {
		tz = "UTC"
	}
	if en.Before(st) { // bad data: never let an event end before it starts
		en = st
	}
	return domain.When{Start: st.UTC(), End: en.UTC(), TZID: tz}, true
}

// typedTitle names the special Google event types that often come without a summary, so they do
// not show as blank rows. An ordinary event with no title stays untitled (clients show a placeholder).
func typedTitle(g gEvent) string {
	switch g.EventType {
	case "workingLocation":
		if w := g.WorkingLocation; w != nil {
			switch {
			case w.Type == "homeOffice":
				return "Working from home"
			case w.OfficeLocation != nil && w.OfficeLocation.Label != "":
				return "Working at " + w.OfficeLocation.Label
			case w.CustomLocation != nil && w.CustomLocation.Label != "":
				return "Working from " + w.CustomLocation.Label
			case w.Type == "officeLocation":
				return "Working from the office"
			}
		}
		return "Working location"
	case "outOfOffice":
		return "Out of office"
	case "focusTime":
		return "Focus time"
	}
	return ""
}

func originalKey(o *gTime) *int64 {
	if o == nil {
		return nil
	}
	var u int64
	switch {
	case o.DateTime != "":
		t, err := time.Parse(time.RFC3339, o.DateTime)
		if err != nil {
			return nil
		}
		u = t.Unix()
	case o.Date != "":
		d, err := domain.ParseDate(o.Date)
		if err != nil {
			return nil
		}
		u = time.Date(d.Y, time.Month(d.M), d.D, 0, 0, 0, 0, time.UTC).Unix()
	default:
		return nil
	}
	return &u
}

func mapResponse(s string) domain.Response {
	switch s {
	case "accepted":
		return domain.Accepted
	case "declined":
		return domain.Declined
	case "tentative":
		return domain.TentativeResponse
	}
	return domain.NeedsAction
}

// mapEvent converts a Google event. cal supplies the access role and default tz.
func mapEvent(g gEvent, cal domain.Calendar) domain.Event {
	ev := domain.Event{
		CalendarID: cal.ID, RemoteID: g.ID, ICalUID: g.ICalUID, ETag: g.Etag,
		Title: g.Summary, Description: g.Description, Location: g.Location, HTMLLink: g.HTMLLink,
		Busy: g.Transparency != "transparent",
	}
	switch g.Status {
	case "cancelled":
		ev.Status = domain.Cancelled
	case "tentative":
		ev.Status = domain.Tentative
	default:
		ev.Status = domain.Confirmed
	}
	if ev.Title == "" {
		ev.Title = typedTitle(g)
	}
	if g.EventType == "workingLocation" { // where you work never makes you busy
		ev.Busy = false
	}
	if t, err := time.Parse(time.RFC3339, g.Updated); err == nil {
		t = t.UTC()
		ev.RemoteUpdatedAt = &t
	}
	switch {
	case len(g.Recurrence) > 0:
		ev.Kind = domain.Master
	case g.RecurringEventID != "":
		ev.Kind = domain.Exception
		ev.RemoteSeriesID = g.RecurringEventID
		ev.RecurrenceKey = originalKey(g.OriginalStart)
	default:
		ev.Kind = domain.Single
	}
	if w, ok := parseWhen(g.Start, g.End, cal.TZID); ok {
		ev.When = w
	}
	if ev.Kind == domain.Master {
		r := &domain.Recurrence{}
		for _, l := range g.Recurrence {
			up := strings.ToUpper(l)
			switch {
			case strings.HasPrefix(up, "RRULE:"):
				if r.RRule == "" {
					r.RRule = l[len("RRULE:"):]
				}
			case strings.HasPrefix(up, "EXDATE"):
				r.EXDates = append(r.EXDates, l)
			case strings.HasPrefix(up, "RDATE"):
				r.RDates = append(r.RDates, l)
			}
		}
		ev.Recurrence = r
	}
	if g.Organizer != nil {
		ev.Organizer = &domain.Person{Email: g.Organizer.Email, Name: g.Organizer.DisplayName}
		ev.IsOrganizer = g.Organizer.Self
	} else if len(g.Attendees) == 0 {
		ev.IsOrganizer = true
	}
	hasSelf := false
	for _, a := range g.Attendees {
		if a.Email == "" {
			continue
		}
		role := "required"
		if a.Resource {
			role = "resource"
		} else if a.Optional {
			role = "optional"
		}
		da := domain.Attendee{Email: a.Email, Name: a.DisplayName, Response: mapResponse(a.ResponseStatus), Role: role, IsSelf: a.Self, IsOrganizer: a.Organizer}
		ev.Attendees = append(ev.Attendees, da)
		if a.Self {
			hasSelf = true
			r := da.Response
			ev.SelfResponse = &r
		}
	}
	sort.Slice(ev.Attendees, func(i, j int) bool { return ev.Attendees[i].Email < ev.Attendees[j].Email })
	// conference: structured first, then parsed from location/description
	if g.ConferenceData != nil {
		for _, ep := range g.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" && ep.URI != "" {
				k := KindForURL(ep.URI)
				if k == "" {
					k = "other"
				}
				ev.Conference = &domain.Conference{URL: ep.URI, Kind: k, Source: "structured"}
				break
			}
		}
	}
	if ev.Conference == nil && g.HangoutLink != "" {
		ev.Conference = &domain.Conference{URL: g.HangoutLink, Kind: "meet", Source: "structured"}
	}
	if ev.Conference == nil {
		ev.Conference = ParseJoinLink(g.Location, g.Description)
	}
	// capabilities
	writable := cal.Access == "owner" || cal.Access == "writer"
	plainType := g.EventType == "" || g.EventType == "default"
	canEdit := writable && plainType && !g.Locked && (ev.IsOrganizer || g.GuestsCanModify || len(ev.Attendees) == 0)
	ev.Caps = domain.EventCaps{
		CanEdit:          canEdit,
		CanRSVP:          writable && hasSelf && !ev.IsOrganizer,
		CanAddConference: canEdit && ev.Conference == nil,
	}
	return ev
}
