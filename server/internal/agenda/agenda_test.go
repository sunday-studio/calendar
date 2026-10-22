package agenda_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"calendar/server/internal/agenda"
	. "calendar/server/internal/agenda/agendatest"
	"calendar/server/internal/db"
)

var ctx = context.Background()

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
func u(s string) int64 { return ts(s).Unix() }

// world: two accounts, three calendars (one hidden), a mix of events around 2026-10-15.
func world(t testing.TB) (*agenda.Store, *db.DB) {
	d := Open(t)
	Do(t, d, func(s *Seed) {
		s.Account("A1", "cobalt", "a@x.test")
		s.Account("A2", "rose", "b@x.test")
		s.Calendar("C1", "A1", "Work", true, "owner", true)
		s.Calendar("C2", "A2", "Home", true, "owner", true)
		s.Calendar("CH", "A2", "Hidden", false, "reader", false)
		s.Event(Ev{ID: "E1", Cal: "C1", Title: "Standup", Start: u("2026-10-15T09:30:00Z"), End: u("2026-10-15T09:45:00Z"), Conf: "https://meet.google.com/abc", ConfKind: "meet", Self: "accepted", Kind: "master", RRule: "FREQ=WEEKLY;BYDAY=TU,TH",
			Occ: []Occ{{RKey: 1, Start: u("2026-10-15T09:30:00Z"), End: u("2026-10-15T09:45:00Z")}, {RKey: 2, Start: u("2026-10-20T09:30:00Z"), End: u("2026-10-20T09:45:00Z")}}})
		s.Event(Ev{ID: "E2", Cal: "C2", Title: "Lunch", Start: u("2026-10-15T12:00:00Z"), End: u("2026-10-15T13:00:00Z"), Location: "Kōya"})
		s.Event(Ev{ID: "E3", Cal: "C2", Title: "Birthday", SD: "2026-10-15", ED: "2026-10-16", NotBusy: true})
		s.Event(Ev{ID: "E4", Cal: "CH", Title: "Hidden thing", Start: u("2026-10-15T10:00:00Z"), End: u("2026-10-15T11:00:00Z")})
		s.Event(Ev{ID: "E5", Cal: "C1", Title: "Cancelled", Status: "cancelled", Start: u("2026-10-15T14:00:00Z"), End: u("2026-10-15T15:00:00Z")})
		s.Event(Ev{ID: "E6", Cal: "C1", Title: "Deleted", Deleted: true, Start: u("2026-10-15T16:00:00Z"), End: u("2026-10-15T17:00:00Z")})
	})
	if _, err := d.W.Exec(`UPDATE calendars SET color_override='ochre' WHERE id='C2'`); err != nil {
		t.Fatal(err)
	}
	return &agenda.Store{DB: d, Now: func() time.Time { return ts("2026-10-15T08:00:00Z") }}, d
}

func ids(items []agenda.AgendaItem) []string {
	var o []string
	for _, i := range items {
		o = append(o, i.ID)
	}
	return o
}

func TestAgendaFiltersAndMapping(t *testing.T) {
	st, _ := world(t)
	res, err := st.Agenda(ctx, ts("2026-10-15T00:00:00Z"), ts("2026-10-16T00:00:00Z"), nil)
	if err != nil {
		t.Fatal(err)
	}
	// all-day first, then by start; hidden/cancelled/deleted absent
	want := []string{"E3:0", "E1:1", "E2:0"}
	got := ids(res.Items)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	bday, standup, lunch := res.Items[0], res.Items[1], res.Items[2]
	if !bday.AllDay || bday.Date != "2026-10-15" || bday.EndDate != "2026-10-16" || bday.Start != nil || bday.TZID != "" || bday.Busy {
		t.Fatalf("all-day mapping %+v", bday)
	}
	if bday.Color != "ochre" || lunch.Color != "ochre" {
		t.Fatalf("override colour: %s %s", bday.Color, lunch.Color)
	}
	if standup.Color != "cobalt" || standup.AccountID != "A1" || standup.CalendarID != "C1" || !standup.Recurring || standup.Version != 7 {
		t.Fatalf("standup %+v", standup)
	}
	if standup.Join == nil || standup.Join.Kind != "meet" || standup.MyResponse == nil || *standup.MyResponse != "accepted" || standup.TZID != "Europe/London" {
		t.Fatalf("standup join/response %+v", standup)
	}
	if lunch.Recurring || lunch.Join != nil || lunch.MyResponse != nil || lunch.Location != "Kōya" || !lunch.CanEdit || !lunch.CanRSVP || lunch.CanAddConference {
		t.Fatalf("lunch %+v", lunch)
	}
	if res.Rev != 0 || res.Items == nil {
		t.Fatalf("rev %d", res.Rev)
	}

	// calendars filter intersects with visibility
	res, _ = st.Agenda(ctx, ts("2026-10-15T00:00:00Z"), ts("2026-10-16T00:00:00Z"), []string{"C1", "CH"})
	if fmt.Sprint(ids(res.Items)) != "[E1:1]" {
		t.Fatalf("filter: %v", ids(res.Items))
	}
	// empty window still returns [] not null
	res, _ = st.Agenda(ctx, ts("2030-01-01T00:00:00Z"), ts("2030-01-02T00:00:00Z"), nil)
	if res.Items == nil || len(res.Items) != 0 {
		t.Fatal("want empty non-nil")
	}
}

func TestAgendaTimedBoundaries(t *testing.T) {
	st, _ := world(t)
	n := func(from, to string) int {
		r, err := st.Agenda(ctx, ts(from), ts(to), []string{"C2"})
		if err != nil {
			t.Fatal(err)
		}
		c := 0
		for _, i := range r.Items {
			if i.ID == "E2:0" {
				c++
			}
		}
		return c
	}
	// lunch 12:00-13:00Z
	if n("2026-10-15T13:00:00Z", "2026-10-15T14:00:00Z") != 0 {
		t.Fatal("event ending exactly at from must be excluded")
	}
	if n("2026-10-15T11:00:00Z", "2026-10-15T12:00:00Z") != 0 {
		t.Fatal("event starting exactly at to must be excluded")
	}
	if n("2026-10-15T12:59:59Z", "2026-10-15T14:00:00Z") != 1 || n("2026-10-15T11:00:00Z", "2026-10-15T12:00:01Z") != 1 {
		t.Fatal("1s overlap must be included")
	}
	if n("2026-10-15T12:10:00Z", "2026-10-15T12:20:00Z") != 1 {
		t.Fatal("window inside event must include it")
	}
}

func TestAgendaZeroLength(t *testing.T) {
	st, d := world(t)
	Do(t, d, func(s *Seed) {
		s.Event(Ev{ID: "Z", Cal: "C1", Title: "Marker", Start: u("2026-10-15T18:00:00Z"), End: u("2026-10-15T18:00:00Z")})
	})
	r, _ := st.Agenda(ctx, ts("2026-10-15T18:00:00Z"), ts("2026-10-15T19:00:00Z"), []string{"C1"})
	if len(r.Items) != 1 || r.Items[0].ID != "Z:0" {
		t.Fatalf("%v", ids(r.Items))
	}
	r, _ = st.Agenda(ctx, ts("2026-10-15T17:00:00Z"), ts("2026-10-15T18:00:00Z"), []string{"C1"})
	if len(r.Items) != 0 {
		t.Fatalf("zero-length at to must be excluded: %v", ids(r.Items))
	}
}

func TestAgendaAllDayAtTZBoundaries(t *testing.T) {
	st, d := world(t)
	Do(t, d, func(s *Seed) {
		s.Setting("user_tz", `"America/New_York"`) // EDT = UTC-4 in October
		s.Event(Ev{ID: "D", Cal: "C1", Title: "Oct15", SD: "2026-10-20", ED: "2026-10-21"})
	})
	has := func(from, to string) bool {
		r, err := st.Agenda(ctx, ts(from), ts(to), []string{"C1"})
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range r.Items {
			if i.ID == "D:0" {
				return true
			}
		}
		return false
	}
	// local day 2026-10-20 in New York = 04:00Z .. 04:00Z next day
	if !has("2026-10-20T04:00:00Z", "2026-10-21T04:00:00Z") {
		t.Fatal("exact local day must include")
	}
	if has("2026-10-21T04:00:00Z", "2026-10-22T04:00:00Z") {
		t.Fatal("next local day must exclude (end date exclusive)")
	}
	if !has("2026-10-21T03:59:59Z", "2026-10-22T04:00:00Z") {
		t.Fatal("from still inside local 10-20 must include")
	}
	if has("2026-10-19T04:00:00Z", "2026-10-20T04:00:00Z") {
		t.Fatal("window ending at local midnight of the start date must exclude")
	}
	if !has("2026-10-19T04:00:00Z", "2026-10-20T04:00:01Z") {
		t.Fatal("window reaching 1s into the day must include")
	}
	// UTC midnight would have said "no" here: 2026-10-20T00:00Z..03:00Z is still local Oct 19
	if has("2026-10-20T00:00:00Z", "2026-10-20T03:00:00Z") {
		t.Fatal("00:00-03:00Z is still local Oct 19 in New York")
	}
	// DST end in London 2026-10-25: all-day compares on dates, not 24h arithmetic
	Do(t, d, func(s *Seed) {
		s.Setting("user_tz", `"Europe/London"`)
		s.Event(Ev{ID: "L", Cal: "C1", Title: "Clocks", SD: "2026-10-25", ED: "2026-10-26"})
	})
	r, _ := st.Agenda(ctx, ts("2026-10-25T00:00:00+01:00"), ts("2026-10-26T00:00:00Z"), []string{"C1"})
	found := false
	for _, i := range r.Items {
		found = found || i.ID == "L:0"
	}
	if !found {
		t.Fatal("25-hour day must still include the all-day event")
	}
}

func TestAgendaSortOrder(t *testing.T) {
	st, d := world(t)
	Do(t, d, func(s *Seed) {
		s.Event(Ev{ID: "S1", Cal: "C1", Title: "Late all-day-b", SD: "2026-10-15", ED: "2026-10-17"})
		s.Event(Ev{ID: "S2", Cal: "C1", Title: "Next day timed", Start: u("2026-10-16T00:30:00Z"), End: u("2026-10-16T01:00:00Z")})
		s.Event(Ev{ID: "S3", Cal: "C1", Title: "Next day all-day", SD: "2026-10-16", ED: "2026-10-17"})
	})
	r, _ := st.Agenda(ctx, ts("2026-10-15T00:00:00Z"), ts("2026-10-17T00:00:00Z"), []string{"C1", "C2"})
	got := ids(r.Items)
	if fmt.Sprint(got) != "[E3:0 S1:0 E1:1 E2:0 S3:0 S2:0]" {
		t.Fatalf("order %v", got)
	}
}
