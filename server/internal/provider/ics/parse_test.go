package ics

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"calendar/server/internal/domain"
	"calendar/server/internal/recur"
)

func load(t *testing.T, name string) *Feed {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "ics", name))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func find(t *testing.T, f *Feed, remoteID string) domain.Event {
	t.Helper()
	for _, e := range f.Events {
		if e.RemoteID == remoteID {
			return e
		}
	}
	t.Fatalf("no event %q in %d events", remoteID, len(f.Events))
	return domain.Event{}
}

func TestWeeklyExdateAndOverrides(t *testing.T) {
	f := load(t, "weekly.ics")
	if f.Name != "Team Cal" || f.TZID != "Europe/London" || len(f.Events) != 3 {
		t.Fatalf("%q %q %d", f.Name, f.TZID, len(f.Events))
	}
	m := find(t, f, "weekly-1@example.test")
	if m.Kind != domain.Master || m.Recurrence.RRule != "FREQ=WEEKLY;BYDAY=MO,WE" || m.When.TZID != "Europe/London" {
		t.Fatalf("%+v", m)
	}
	if got := m.When.Start.Format(time.RFC3339); got != "2026-10-05T09:00:00Z" { // 10:00 BST
		t.Fatal(got)
	}
	if len(m.Recurrence.EXDates) != 1 || m.Recurrence.EXDates[0] != "EXDATE;TZID=Europe/London:20261012T100000" {
		t.Fatalf("%v", m.Recurrence.EXDates)
	}
	// moved occurrence
	key := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC).Unix()
	x := find(t, f, "weekly-1@example.test#"+strconv.FormatInt(key, 10))
	if x.Kind != domain.Exception || x.RemoteSeriesID != "weekly-1@example.test" || *x.RecurrenceKey != key || x.Title != "Standup (moved)" {
		t.Fatalf("%+v", x)
	}
	if x.When.Start.Hour() != 13 || x.When.End.Sub(x.When.Start) != 30*time.Minute || x.RemoteUpdatedAt == nil {
		t.Fatalf("%+v", x.When)
	}
	// cancelled occurrence
	ckey := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC).Unix()
	if c := find(t, f, "weekly-1@example.test#"+strconv.FormatInt(ckey, 10)); c.Status != domain.Cancelled || c.Kind != domain.Exception {
		t.Fatalf("%+v", c)
	}
	// the master expands honouring EXDATE (12th removed); the engine handles overrides itself
	occ, warn := recur.Expand(recur.Series{Start: m.When.Start, End: m.When.End, TZID: m.When.TZID, RRule: m.Recurrence.RRule, EXDates: m.Recurrence.EXDates},
		recur.Window{From: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}, 0)
	if len(warn) > 0 {
		t.Fatal(warn)
	}
	var days []int
	for _, o := range occ {
		days = append(days, o.Start.Day())
	}
	// Mon 5, Wed 7, (Mon 12 excluded by EXDATE), Wed 14: the engine drops overrides itself
	if len(days) != 3 || days[0] != 5 || days[1] != 7 || days[2] != 14 {
		t.Fatalf("days %v", days)
	}
}

func TestAllDay(t *testing.T) {
	f := load(t, "allday.ics")
	o := find(t, f, "offsite@example.test")
	if !o.When.AllDay || o.When.StartDate.String() != "2026-10-14" || o.When.EndDate.String() != "2026-10-16" || !o.Busy {
		t.Fatalf("%+v", o.When)
	}
	h := find(t, f, "holiday@example.test")
	if h.When.EndDate.String() != "2026-11-02" || h.Busy {
		t.Fatalf("%+v busy=%v", h.When, h.Busy)
	}
	if tr := find(t, f, "trip@example.test"); tr.When.EndDate.String() != "2026-11-23" {
		t.Fatalf("%+v", tr.When)
	}
	b := find(t, f, "birthday@example.test")
	if b.Kind != domain.Master || b.Recurrence.EXDates[0] != "EXDATE;VALUE=DATE:20270301" {
		t.Fatalf("%+v", b.Recurrence)
	}
}

func TestFloatingAndUTC(t *testing.T) {
	f := load(t, "floating.ics")
	fl := find(t, f, "float@example.test")
	if fl.When.TZID != "UTC" || fl.When.Start.Format(time.RFC3339) != "2026-10-20T09:00:00Z" {
		t.Fatalf("%+v", fl.When)
	}
	u := find(t, f, "utc@example.test")
	if u.When.End.Sub(u.When.Start) != 90*time.Minute || u.When.TZID != "UTC" {
		t.Fatalf("%+v", u.When)
	}
	// floating times follow X-WR-TIMEZONE when present
	g, err := Parse([]byte("BEGIN:VCALENDAR\nX-WR-TIMEZONE:America/New_York\nBEGIN:VEVENT\nUID:a\nDTSTART:20261020T090000\nEND:VEVENT\nEND:VCALENDAR\n"))
	if err != nil {
		t.Fatal(err)
	}
	if e := g.Events[0]; e.When.TZID != "America/New_York" || e.When.Start.Hour() != 13 {
		t.Fatalf("%+v", e.When)
	}
}

func TestTZIDAcrossDST(t *testing.T) {
	f := load(t, "dst.ics")
	m := find(t, f, "sunday@example.test")
	if m.When.TZID != "America/New_York" || m.When.Start.Format(time.RFC3339) != "2026-10-25T13:00:00Z" { // EDT
		t.Fatalf("%+v", m.When)
	}
	occ, _ := recur.Expand(recur.Series{Start: m.When.Start, End: m.When.End, TZID: m.When.TZID, RRule: m.Recurrence.RRule},
		recur.Window{From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)}, 0)
	if len(occ) != 3 || occ[0].Start.Hour() != 13 || occ[1].Start.Format(time.RFC3339) != "2026-11-01T14:00:00Z" { // EST after Nov 1
		t.Fatalf("%+v", occ)
	}
	w := find(t, f, "windows-tz@example.test")
	if w.When.TZID != "Eastern Standard Time" || w.When.Start.Format(time.RFC3339) != "2026-10-26T13:00:00Z" {
		t.Fatalf("%+v", w.When)
	}
}

func TestCancelledAndTentative(t *testing.T) {
	f := load(t, "cancelled.ics")
	if find(t, f, "gone@example.test").Status != domain.Cancelled || find(t, f, "maybe@example.test").Status != domain.Tentative {
		t.Fatal("status")
	}
}

func TestFoldedUTF8PeopleAndLinks(t *testing.T) {
	f := load(t, "folded_utf8.ics")
	e := find(t, f, "folded@example.test")
	want := "Café ☕ planning with a very long title that has to be folded across several lines so that the parser must unfold them properly 日本語 🎉"
	if e.Title != want {
		t.Fatalf("title %q", e.Title)
	}
	if e.Location != "Room 4, Floor 2" || !strings.HasPrefix(e.Description, "Agenda\n1. Intro, goals; scope\nJoin:") {
		t.Fatalf("%q %q", e.Location, e.Description)
	}
	if e.HTMLLink != "https://example.test/events/42" {
		t.Fatal(e.HTMLLink)
	}
	if e.Organizer == nil || e.Organizer.Email != "ama@example.test" || e.Organizer.Name != "Ama Mensah" {
		t.Fatalf("%+v", e.Organizer)
	}
	if len(e.Attendees) != 4 {
		t.Fatalf("%+v", e.Attendees)
	}
	by := map[string]domain.Attendee{}
	for _, a := range e.Attendees {
		by[a.Email] = a
	}
	if by["kofi@example.test"].Response != domain.Accepted || by["esi@example.test"].Role != "optional" || by["esi@example.test"].Response != domain.TentativeResponse ||
		by["room4@example.test"].Role != "resource" || !by["ama@example.test"].IsOrganizer {
		t.Fatalf("%+v", by)
	}
	if e.Conference == nil || e.Conference.Kind != "zoom" || e.Conference.Source != "parsed" {
		t.Fatalf("%+v", e.Conference)
	}
	m := find(t, f, "meet@example.test")
	if m.Conference == nil || m.Conference.Kind != "meet" || m.Conference.Source != "structured" {
		t.Fatalf("%+v", m.Conference)
	}
	// every event is read-only
	for _, ev := range f.Events {
		if ev.Caps != (domain.EventCaps{}) {
			t.Fatalf("caps %+v", ev.Caps)
		}
		if ev.ETag == "" {
			t.Fatal("etag")
		}
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for name, body := range map[string]string{
		"html":      "<!doctype html><html>nope</html>",
		"empty":     "",
		"truncated": "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:x\nDTSTART:20261020T090000Z\n",
		"deep":      "BEGIN:VCALENDAR\n" + strings.Repeat("BEGIN:X\n", 50),
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: want error", name)
		} else if _, ok := err.(*UserError); !ok {
			t.Errorf("%s: want UserError, got %T", name, err)
		}
	}
	// an exception whose master is not in the feed stands alone
	g, err := Parse([]byte("BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:solo\nRECURRENCE-ID:20261020T090000Z\nDTSTART:20261020T100000Z\nSUMMARY:One occurrence\nEND:VEVENT\nEND:VCALENDAR\n"))
	if err != nil || g.Events[0].Kind != domain.Single || g.Events[0].RemoteSeriesID != "" {
		t.Fatalf("%v %+v", err, g)
	}
}
