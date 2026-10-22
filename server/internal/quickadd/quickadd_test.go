package quickadd

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeEnv struct {
	people map[string][]Contact
	cals   []Calendar
}

func (f fakeEnv) Contacts(_ context.Context, name string) ([]Contact, error) {
	return f.people[strings.ToLower(name)], nil
}
func (f fakeEnv) Calendars(context.Context) ([]Calendar, error) { return f.cals, nil }

var london, _ = time.LoadLocation("Europe/London")

// Thursday 15 Oct 2026, 08:00 London.
var now = time.Date(2026, 10, 15, 8, 0, 0, 0, london)

func env() fakeEnv {
	return fakeEnv{
		people: map[string][]Contact{
			"sam":    {{Email: "sam@northwind.example", Name: "Sam Okafor", Count: 5}},
			"priya":  {{Email: "priya@northwind.example", Name: "Priya Nair"}},
			"alex":   {{Email: "alex.b@x.test", Name: "Alex Brown"}, {Email: "alex.c@x.test", Name: "Alex Chen"}},
			"sammie": nil,
		},
		cals: []Calendar{{ID: "C-WORK", Name: "Work", IsDefault: true}, {ID: "C-HOME", Name: "Home"}, {ID: "C-HOME2", Name: "Home projects"}},
	}
}

func run(t *testing.T, text string) Draft {
	t.Helper()
	d, err := (&RuleParser{Env: env()}).Parse(context.Background(), Request{Text: text, Now: now, TZID: "Europe/London"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func amb(d Draft, field string) *Ambiguity {
	for i := range d.Ambiguities {
		if d.Ambiguities[i].Field == field {
			return &d.Ambiguities[i]
		}
	}
	return nil
}

func TestLunchWithSamFriAtPlace(t *testing.T) {
	d := run(t, "lunch with Sam fri 1pm at Kōya, 49 Frith St")
	e := d.Event
	if e.Title != "Lunch with Sam" || e.Location != "Kōya, 49 Frith St" || e.When.Start != "2026-10-16T13:00:00" || e.When.DurationMin != 60 || e.When.TZID != "Europe/London" {
		t.Fatalf("%+v", e)
	}
	if len(e.Attendees) != 1 || e.Attendees[0] != "sam@northwind.example" || e.CalendarID != "C-WORK" || d.CalendarName != "Work" {
		t.Fatalf("%+v", e)
	}
	// a bare weekday is a question, with the nearest preselected
	a := amb(d, "when.date")
	if a == nil || a.Question != "Which Friday?" || a.Default != 0 || len(a.Options) != 2 ||
		a.Options[0].Value != "2026-10-16" || a.Options[0].Label != "This Friday, 16 Oct" || a.Options[1].Value != "2026-10-23" {
		t.Fatalf("%+v", d.Ambiguities)
	}
	if len(d.Ambiguities) != 1 || d.Source != "rules" || d.Confidence < 0.6 {
		t.Fatalf("%v %v", d.Ambiguities, d.Confidence)
	}
	// spans: who / when / where, as UTF-16 ranges into the original text
	text := "lunch with Sam fri 1pm at Kōya, 49 Frith St"
	got := map[string]string{}
	u := []rune(text)
	for _, s := range d.Spans {
		got[s.Kind] += string(u[s.From:s.To]) + "|"
	}
	if got["who"] != "Sam|" || got["when"] != "fri|1pm|" || got["where"] != "at Kōya, 49 Frith St|" {
		t.Fatalf("%v", got)
	}
}

func TestQualifiedWeekdaysAreNotAmbiguous(t *testing.T) {
	d := run(t, "dentist next friday 9:30")
	if d.Event.When.Start != "2026-10-23T09:30:00" || len(d.Ambiguities) != 0 || d.Event.Title != "Dentist" {
		t.Fatalf("%+v %v", d.Event, d.Ambiguities)
	}
	d = run(t, "dentist this friday 9:30")
	if d.Event.When.Start != "2026-10-16T09:30:00" || len(d.Ambiguities) != 0 {
		t.Fatalf("%+v", d.Event)
	}
	d = run(t, "gym today 18:00")
	if d.Event.When.Start != "2026-10-15T18:00:00" || len(d.Ambiguities) != 0 {
		t.Fatalf("%+v", d.Event)
	}
}

func TestRelativeDays(t *testing.T) {
	for text, want := range map[string]string{
		"call tomorrow 3pm":                "2026-10-16T15:00:00",
		"call day after tomorrow at 10:15": "2026-10-17T10:15:00",
		"call in 3 days 11am":              "2026-10-18T11:00:00",
		"call in 2 weeks 11am":             "2026-10-29T11:00:00",
		"review 16 oct 2pm":                "2026-10-16T14:00:00",
		"review october 20th 2pm":          "2026-10-20T14:00:00",
		"review 2026-11-02 14:00":          "2026-11-02T14:00:00",
		"review 3/11 14:00":                "2026-11-03T14:00:00",
		"review 1 oct 14:00":               "2027-10-01T14:00:00", // already past: next year
	} {
		d := run(t, text)
		if d.Event.When.Start != want {
			t.Errorf("%q: got %s want %s (%v)", text, d.Event.When.Start, want, d.Ambiguities)
		}
		if a := amb(d, "when.date"); a != nil {
			t.Errorf("%q: unexpected date question %v", text, a)
		}
	}
}

func TestInHoursUsesNow(t *testing.T) {
	d := run(t, "call in 90 minutes")
	if d.Event.When.Start != "2026-10-15T09:30:00" {
		t.Fatalf("%+v", d.Event.When)
	}
}

func TestDurationsAndRanges(t *testing.T) {
	cases := map[string]struct {
		start string
		dur   int
	}{
		"workshop tomorrow 10am for 2 hours":      {"2026-10-16T10:00:00", 120},
		"workshop tomorrow 10am for 45 min":       {"2026-10-16T10:00:00", 45},
		"workshop tomorrow 10am for an hour":      {"2026-10-16T10:00:00", 60},
		"workshop tomorrow 10am for half an hour": {"2026-10-16T10:00:00", 30},
		"workshop tomorrow 10am 90min":            {"2026-10-16T10:00:00", 90},
		"workshop tomorrow 1-2pm":                 {"2026-10-16T13:00:00", 60},
		"workshop tomorrow 11-1pm":                {"2026-10-16T11:00:00", 120},
		"workshop tomorrow from 9:30 to 11:00":    {"2026-10-16T09:30:00", 90},
		"workshop tomorrow 13:00-14:30":           {"2026-10-16T13:00:00", 90},
		"workshop tomorrow noon":                  {"2026-10-16T12:00:00", 60},
	}
	for text, want := range cases {
		d := run(t, text)
		if d.Event.When.Start != want.start || d.Event.When.DurationMin != want.dur {
			t.Errorf("%q: got %s/%d want %s/%d (%+v)", text, d.Event.When.Start, d.Event.When.DurationMin, want.start, want.dur, d.Ambiguities)
		}
	}
}

func TestBareHourAsksMorningOrAfternoon(t *testing.T) {
	d := run(t, "lunch tomorrow at 1")
	a := amb(d, "when.time")
	if a == nil || a.Default != 1 || a.Options[0].Value != "01:00" || a.Options[1].Value != "13:00" {
		t.Fatalf("%+v", d.Ambiguities)
	}
	d = run(t, "coffee tomorrow at 9")
	if a := amb(d, "when.time"); a == nil || a.Default != 0 {
		t.Fatalf("%+v", d.Ambiguities)
	}
	if d := run(t, "coffee tomorrow at 15"); len(d.Ambiguities) != 0 || d.Event.When.Start != "2026-10-16T15:00:00" {
		t.Fatalf("%+v", d)
	}
}

func TestNoTimeNoDate(t *testing.T) {
	d := run(t, "call the bank")
	if a := amb(d, "when.date"); a == nil || !a.Optional {
		t.Fatalf("%+v", d.Ambiguities)
	}
	if a := amb(d, "when.time"); a == nil || !a.Optional || len(a.Options) != 0 {
		t.Fatalf("%+v", d.Ambiguities)
	}
	d = run(t, "Standup friday")
	if a := amb(d, "when.time"); a == nil || a.Question != "What time?" {
		t.Fatalf("%+v", d.Ambiguities)
	}
	if d.Confidence >= 0.6 {
		t.Fatalf("confidence %v should be low", d.Confidence)
	}
}

func TestTimePassedTodayRollsToTomorrow(t *testing.T) {
	d := run(t, "gym 7am")
	if d.Event.When.Start != "2026-10-16T07:00:00" {
		t.Fatalf("%+v", d.Event.When)
	}
	if d := run(t, "gym 9am"); d.Event.When.Start != "2026-10-15T09:00:00" {
		t.Fatalf("%+v", d.Event.When)
	}
}

func TestAllDayAndRecurrence(t *testing.T) {
	d := run(t, "Mum's birthday 20 oct all day")
	w := d.Event.When
	if !w.AllDay || w.Date != "2026-10-20" || w.Start != "" || len(d.Ambiguities) != 0 {
		t.Fatalf("%+v %v", w, d.Ambiguities)
	}
	d = run(t, "standup every weekday 9:30")
	if d.Event.Recurrence == nil || d.Event.Recurrence.RRule != "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR" || d.Event.Title != "Standup" {
		t.Fatalf("%+v", d.Event)
	}
	d = run(t, "yoga every tuesday 6:30pm")
	if d.Event.Recurrence.RRule != "FREQ=WEEKLY;BYDAY=TU" || d.Event.When.Start != "2026-10-20T18:30:00" || len(d.Ambiguities) != 0 {
		t.Fatalf("%+v %v", d.Event, d.Ambiguities)
	}
	if d := run(t, "pay rent monthly 1st"); d.Event.Recurrence == nil || d.Event.Recurrence.RRule != "FREQ=MONTHLY" {
		t.Fatalf("%+v", d.Event)
	}
}

func TestPeople(t *testing.T) {
	d := run(t, "1:1 with Priya and Sam tomorrow 11:00")
	if len(d.Event.Attendees) != 2 || d.Event.Title != "1:1 with Priya and Sam" || len(d.Ambiguities) != 0 {
		t.Fatalf("%+v %v", d.Event, d.Ambiguities)
	}
	d = run(t, "coffee with Alex tomorrow 10am")
	a := amb(d, "attendees")
	if a == nil || a.Question != "Which Alex?" || len(a.Options) != 2 || a.Options[0].Value != "alex.b@x.test" || len(d.Event.Attendees) != 0 {
		t.Fatalf("%+v %v", d.Event, d.Ambiguities)
	}
	// unknown names and explicit emails
	d = run(t, "lunch with Zed tomorrow 1pm")
	if len(d.People) != 1 || d.People[0].Email != "" || len(d.Event.Attendees) != 0 || len(d.Ambiguities) != 0 {
		t.Fatalf("%+v", d)
	}
	d = run(t, "call kim@example.com tomorrow 4pm")
	if len(d.Event.Attendees) != 1 || d.Event.Attendees[0] != "kim@example.com" || d.Event.Title != "Call kim@example.com" {
		t.Fatalf("%+v", d.Event)
	}
}

func TestCalendarHints(t *testing.T) {
	d := run(t, "dentist tomorrow 9am on my home calendar")
	if a := amb(d, "calendarId"); a == nil || len(a.Options) != 2 {
		t.Fatalf("home matches two calendars: %+v", d.Ambiguities)
	}
	d = run(t, "dentist tomorrow 9am #work")
	if d.Event.CalendarID != "C-WORK" || d.Event.Title != "Dentist" || len(d.Ambiguities) != 0 {
		t.Fatalf("%+v %v", d.Event, d.Ambiguities)
	}
	d = run(t, "dentist tomorrow 9am in the studio calendar")
	if a := amb(d, "calendarId"); a == nil || !strings.Contains(a.Question, "studio") {
		t.Fatalf("%+v", d.Ambiguities)
	}
}

func TestPlaceVersusTime(t *testing.T) {
	d := run(t, "dinner at Padella at 7:30pm tomorrow")
	if d.Event.Location != "Padella" || d.Event.When.Start != "2026-10-16T19:30:00" || d.Event.Title != "Dinner" {
		t.Fatalf("%+v", d.Event)
	}
	d = run(t, "meet at 49 Frith St tomorrow 3pm")
	if d.Event.Location != "49 Frith St" || d.Event.When.Start != "2026-10-16T15:00:00" {
		t.Fatalf("%+v", d.Event)
	}
	d = run(t, "call at 3pm tomorrow")
	if d.Event.Location != "" || d.Event.When.Start != "2026-10-16T15:00:00" {
		t.Fatalf("%+v", d.Event)
	}
	d = run(t, "lunch @ Kōya friday 12:30")
	if d.Event.Location != "Kōya" {
		t.Fatalf("%+v", d.Event)
	}
}

func TestEmptyAndBadInput(t *testing.T) {
	p := &RuleParser{Env: env()}
	if _, err := p.Parse(context.Background(), Request{Text: "  "}); err == nil {
		t.Fatal("empty text must fail")
	}
	if _, err := p.Parse(context.Background(), Request{Text: strings.Repeat("a", 600)}); err == nil {
		t.Fatal("long text must fail")
	}
	if _, err := p.Parse(context.Background(), Request{Text: "x", TZID: "Mars/Base"}); err == nil {
		t.Fatal("bad tz must fail")
	}
	d := run(t, "tomorrow 3pm")
	if a := amb(d, "title"); a == nil || d.Event.Title != "" {
		t.Fatalf("title question expected: %+v", d)
	}
}

func TestRespectsRequestZone(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	d, err := (&RuleParser{Env: env()}).Parse(context.Background(), Request{Text: "call tomorrow 9am", Now: time.Date(2026, 10, 15, 23, 30, 0, 0, time.UTC), TZID: "America/New_York"})
	if err != nil {
		t.Fatal(err)
	}
	// 23:30Z is 19:30 in New York on the 15th, so tomorrow is the 16th there
	_ = ny
	if d.Event.When.Start != "2026-10-16T09:00:00" || d.Event.When.TZID != "America/New_York" {
		t.Fatalf("%+v", d.Event.When)
	}
}
