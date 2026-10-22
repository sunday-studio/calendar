package google

import (
	"encoding/json"
	"testing"

	"calendar/server/internal/domain"
)

func ev(t *testing.T, js string, access string) domain.Event {
	t.Helper()
	var g gEvent
	if err := json.Unmarshal([]byte(js), &g); err != nil {
		t.Fatal(err)
	}
	return mapEvent(g, domain.Calendar{ID: "c", Access: access, TZID: "Europe/London"})
}

func TestJoinLinks(t *testing.T) {
	cases := map[string]string{
		"join https://meet.google.com/abc-defg-hij now":                                   "meet",
		"https://us02web.zoom.us/j/123456789?pwd=xyz.":                                    "zoom",
		`<a href="https://teams.microsoft.com/l/meetup-join/19%3ameeting_x?a=1&amp;b=2">`: "teams",
		"https://acme.webex.com/meet/jane":                                                "webex",
		"https://example.com/zoom.us/j/1":                                                 "",
		"https://zoom.us/pricing":                                                         "",
	}
	for in, want := range cases {
		c := ParseJoinLink(in)
		got := ""
		if c != nil {
			got = c.Kind
		}
		if got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	if c := ParseJoinLink(`x <a href="https://teams.microsoft.com/l/meetup-join/x?a=1&amp;b=2">`); c == nil || c.URL != "https://teams.microsoft.com/l/meetup-join/x?a=1&b=2" {
		t.Fatalf("entities not unescaped: %+v", c)
	}
}

func TestMapStructuredConferenceAndCaps(t *testing.T) {
	e := ev(t, `{"id":"a","status":"confirmed","summary":"x","start":{"dateTime":"2026-10-12T09:00:00+01:00"},"end":{"dateTime":"2026-10-12T10:00:00+01:00"},
	 "organizer":{"email":"me@x.com","self":true},
	 "attendees":[{"email":"me@x.com","self":true,"organizer":true,"responseStatus":"accepted"},{"email":"b@y.com","responseStatus":"declined","optional":true}],
	 "conferenceData":{"entryPoints":[{"entryPointType":"video","uri":"https://meet.google.com/aaa-bbbb-ccc"}]},
	 "location":"https://zoom.us/j/1"}`, "owner")
	if e.Conference == nil || e.Conference.Source != "structured" || e.Conference.Kind != "meet" {
		t.Fatalf("%+v", e.Conference)
	}
	if !e.Caps.CanEdit || e.Caps.CanRSVP || e.Caps.CanAddConference || !e.IsOrganizer {
		t.Fatalf("%+v", e.Caps)
	}
	if e.When.TZID != "Europe/London" || e.Attendees[0].Role != "optional" {
		t.Fatalf("%+v", e)
	}
}

func TestMapInviteOnWritableAndReadOnly(t *testing.T) {
	js := `{"id":"a","summary":"x","start":{"dateTime":"2026-10-12T09:00:00Z"},"end":{"dateTime":"2026-10-12T10:00:00Z"},
	 "organizer":{"email":"o@y.com"},"attendees":[{"email":"me@x.com","self":true,"responseStatus":"needsAction"},{"email":"o@y.com","organizer":true}]}`
	e := ev(t, js, "owner")
	if e.Caps.CanEdit || !e.Caps.CanRSVP || e.Caps.CanAddConference || e.SelfResponse == nil || *e.SelfResponse != domain.NeedsAction {
		t.Fatalf("%+v", e.Caps)
	}
	e = ev(t, js[:len(js)-1]+`,"guestsCanModify":true}`, "writer")
	if !e.Caps.CanEdit || !e.Caps.CanAddConference {
		t.Fatalf("guestsCanModify: %+v", e.Caps)
	}
	e = ev(t, js, "reader")
	if e.Caps.CanEdit || e.Caps.CanRSVP {
		t.Fatalf("reader: %+v", e.Caps)
	}
}

func TestMapAllDayAndCancelledInstance(t *testing.T) {
	e := ev(t, `{"id":"a","summary":"hol","start":{"date":"2026-12-25"},"end":{"date":"2026-12-26"},"transparency":"transparent"}`, "owner")
	if !e.When.AllDay || e.When.StartDate.String() != "2026-12-25" || e.When.EndDate.String() != "2026-12-26" || e.Busy {
		t.Fatalf("%+v", e.When)
	}
	c := ev(t, `{"id":"m_20261012T090000Z","status":"cancelled","recurringEventId":"m","originalStartTime":{"dateTime":"2026-10-12T10:00:00+01:00"}}`, "owner")
	if c.Status != domain.Cancelled || c.Kind != domain.Exception || c.RemoteSeriesID != "m" || c.RecurrenceKey == nil || *c.RecurrenceKey != 1791795600 {
		t.Fatalf("%+v", c)
	}
}
