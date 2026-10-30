package agenda_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"calendar/server/internal/agenda"
	. "calendar/server/internal/agenda/agendatest"
)

func TestEventDetail(t *testing.T) {
	st, d := world(t)
	Do(t, d, func(s *Seed) {
		s.Event(Ev{ID: "EX", Cal: "C1", Title: "Moved standup", Kind: "exception", Series: "E1", Start: u("2026-10-22T10:00:00Z"), End: u("2026-10-22T10:15:00Z"),
			Occ: []Occ{{RKey: 3, Start: u("2026-10-22T10:00:00Z"), End: u("2026-10-22T10:15:00Z")}}})
		s.Event(Ev{ID: "D1", Cal: "C1", Title: "Review", Description: "Walk through", Org: [2]string{"boss@x.test", "Boss"},
			Attendees: [][2]string{{"zed@x.test", "Zed"}, {"amy@x.test", "Amy"}}, Start: u("2026-10-16T10:00:00Z"), End: u("2026-10-16T11:00:00Z")})
	})
	if _, err := d.W.Exec(`UPDATE events SET rrule='FREQ=WEEKLY;BYDAY=TU,TH' WHERE id='E1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`UPDATE attendees SET is_organizer=1 WHERE email='zed@x.test'`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`INSERT INTO reminder_overrides(event_id,rkey,minutes_before) VALUES ('D1',0,15)`); err != nil {
		t.Fatal(err)
	}

	ev, err := st.Event(ctx, "D1")
	if err != nil {
		t.Fatal(err)
	}
	if ev.ID != "D1:0" || ev.Description != "Walk through" || ev.Organizer == nil || ev.Organizer.Email != "boss@x.test" ||
		ev.CalendarName != "Work" || ev.AccountName != "Acct A1" || ev.Recurrence != nil {
		t.Fatalf("%+v", ev)
	}
	if len(ev.Attendees) != 2 || ev.Attendees[0].Email != "zed@x.test" || !ev.Attendees[0].IsOrganizer || ev.Attendees[1].Email != "amy@x.test" {
		t.Fatalf("attendees %+v", ev.Attendees)
	}
	if ev.ReminderMin == nil || *ev.ReminderMin != 15 {
		t.Fatal("reminder")
	}
	// bare id of a series: nearest upcoming (now = 2026-10-15T08:00Z) -> rkey 1
	ev, _ = st.Event(ctx, "E1")
	if ev.ID != "E1:1" || ev.Recurrence == nil || ev.Recurrence.Summary != "Weekly on Tue, Thu" || ev.Recurrence.RRule != "FREQ=WEEKLY;BYDAY=TU,TH" {
		t.Fatalf("%+v %+v", ev.ID, ev.Recurrence)
	}
	ev, _ = st.Event(ctx, "E1:2")
	if ev.ID != "E1:2" || ev.Start.UTC().Format(time.RFC3339) != "2026-10-20T09:30:00Z" {
		t.Fatalf("%+v", ev.AgendaItem)
	}
	// exception inherits the master's rule
	ev, _ = st.Event(ctx, "EX:3")
	if ev.Recurrence == nil || ev.Recurrence.Summary != "Weekly on Tue, Thu" || !ev.Recurring {
		t.Fatalf("exception recurrence %+v", ev.Recurrence)
	}
	// hidden calendars stay readable by id; cancelled too; deleted / unknown / wrong rkey do not
	if _, err := st.Event(ctx, "E4"); err != nil {
		t.Fatal(err)
	}
	if ev, err := st.Event(ctx, "E5"); err != nil || ev.Status != "cancelled" {
		t.Fatalf("cancelled: %v", err)
	}
	for _, id := range []string{"E6", "nope", "E1:99", "E2:7"} {
		if _, err := st.Event(ctx, id); !errors.Is(err, agenda.ErrNotFound) {
			t.Fatalf("%s: %v", id, err)
		}
	}
	// no occurrence rows: fall back to the event's own times
	Do(t, d, func(s *Seed) {
		s.Event(Ev{ID: "FAR", Cal: "C1", Title: "Far future", NoOcc: true, Start: u("2030-01-01T10:00:00Z"), End: u("2030-01-01T11:00:00Z")})
	})
	if ev, err := st.Event(ctx, "FAR"); err != nil || ev.ID != "FAR:0" || ev.Start == nil {
		t.Fatalf("fallback %v %+v", err, ev.AgendaItem)
	}
}

func titles(p agenda.EventPage) []string {
	var o []string
	for _, e := range p.Items {
		o = append(o, e.ID)
	}
	return o
}

func TestListSearchAndPagination(t *testing.T) {
	st, d := world(t)
	Do(t, d, func(s *Seed) {
		for i := 0; i < 7; i++ {
			s.Event(Ev{ID: fmt.Sprintf("P%d", i), Cal: "C1", Title: fmt.Sprintf("Planning session %d", i),
				Start: u("2026-11-01T10:00:00Z") + int64(i)*3600, End: u("2026-11-01T10:30:00Z") + int64(i)*3600})
		}
		s.Event(Ev{ID: "AT", Cal: "C1", Title: "Coffee", Attendees: [][2]string{{"mensah@x.test", "Kwame Mensah"}}, Start: u("2026-11-02T10:00:00Z"), End: u("2026-11-02T10:30:00Z")})
		s.Event(Ev{ID: "HQ", Cal: "CH", Title: "Planning hidden", Start: u("2026-11-03T10:00:00Z"), End: u("2026-11-03T10:30:00Z")})
	})
	// prefix matching, visible calendars only, one hit per event
	p, err := st.List(ctx, agenda.ListQuery{Q: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 7 || p.NextCursor != nil {
		t.Fatalf("%v", titles(p))
	}
	// multi-word AND + prefix
	p, _ = st.List(ctx, agenda.ListQuery{Q: "plann sess"})
	if len(p.Items) != 7 {
		t.Fatalf("and: %d", len(p.Items))
	}
	// attendee name via prefix, diacritics-insensitive tokenizer, case-insensitive
	if p, _ = st.List(ctx, agenda.ListQuery{Q: "MENS"}); len(p.Items) != 1 || p.Items[0].EventID != "AT" {
		t.Fatalf("attendee: %v", titles(p))
	}
	if p, _ = st.List(ctx, agenda.ListQuery{Q: "koya"}); len(p.Items) != 1 || p.Items[0].EventID != "E2" {
		t.Fatalf("diacritics: %v", titles(p))
	}
	// explicit calendar may be hidden
	if p, _ = st.List(ctx, agenda.ListQuery{Q: "planning", Calendar: "CH"}); len(p.Items) != 1 || p.Items[0].EventID != "HQ" {
		t.Fatalf("hidden calendar: %v", titles(p))
	}
	// FTS syntax characters are neutralised
	if _, err := st.List(ctx, agenda.ListQuery{Q: `plan" OR * (`}); err != nil {
		t.Fatalf("injection: %v", err)
	}
	var ve *agenda.ValidationError
	if _, err := st.List(ctx, agenda.ListQuery{Q: "!!!"}); !errors.As(err, &ve) {
		t.Fatalf("want validation, got %v", err)
	}
	if _, err := st.List(ctx, agenda.ListQuery{Q: "x", Cursor: "garbage!"}); !errors.As(err, &ve) {
		t.Fatalf("cursor: %v", err)
	}
	// window + q: only events inside
	from, to := ts("2026-11-01T11:30:00Z"), ts("2026-11-01T13:30:00Z")
	if p, _ = st.List(ctx, agenda.ListQuery{Q: "plan", From: &from, To: &to}); len(p.Items) != 2 {
		t.Fatalf("window q: %v", titles(p))
	}
	// pagination over the window form, limit 3 -> pages of 3,3,1, no dup / gap
	from, to = ts("2026-11-01T00:00:00Z"), ts("2026-11-02T00:00:00Z")
	var all []string
	cur := ""
	pages := 0
	for {
		p, err := st.List(ctx, agenda.ListQuery{From: &from, To: &to, Limit: 3, Cursor: cur})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		all = append(all, titles(p)...)
		if p.NextCursor == nil {
			break
		}
		cur = *p.NextCursor
	}
	want := "[P0:0 P1:0 P2:0 P3:0 P4:0 P5:0 P6:0]"
	if fmt.Sprint(all) != want || pages != 3 {
		t.Fatalf("%v pages=%d", all, pages)
	}
	// exact multiple of the page size yields no empty trailing page
	p, _ = st.List(ctx, agenda.ListQuery{From: &from, To: &to, Limit: 7})
	if len(p.Items) != 7 || p.NextCursor != nil {
		t.Fatal("exact page")
	}
	// calendar filter on window form; recurring series list one row per occurrence
	from, to = ts("2026-10-15T00:00:00Z"), ts("2026-10-21T00:00:00Z")
	p, _ = st.List(ctx, agenda.ListQuery{From: &from, To: &to, Calendar: "C1"})
	if fmt.Sprint(titles(p)) != "[E1:1 E1:2]" {
		t.Fatalf("%v", titles(p))
	}
	// with q a series appears once (first occurrence in window)
	p, _ = st.List(ctx, agenda.ListQuery{Q: "standup", From: &from, To: &to})
	if fmt.Sprint(titles(p)) != "[E1:1]" {
		t.Fatalf("q series: %v", titles(p))
	}
}

func TestCalendarsAndPatch(t *testing.T) {
	st, d := world(t)
	cals, err := st.Calendars(ctx)
	if err != nil || len(cals) != 3 {
		t.Fatal(err, len(cals))
	}
	// grouped by account, primary first
	if cals[0].ID != "C1" || cals[1].AccountID != "A2" {
		t.Fatalf("%+v", cals)
	}
	var home, work agenda.Calendar
	for _, c := range cals {
		if c.ID == "C2" {
			home = c
		}
		if c.ID == "C1" {
			work = c
		}
	}
	if home.Color != "ochre" || home.ColorOverride == nil || *home.ColorOverride != "ochre" || work.Color != "cobalt" || work.ColorOverride != nil || work.TZID != "Europe/London" {
		t.Fatalf("colours %+v %+v", home, work)
	}
	rev0, _ := d.Rev(ctx)
	f := false
	c, became, err := st.PatchCalendar(ctx, "C1", agenda.CalendarPatch{Visible: &f})
	if err != nil || c.Visible || became {
		t.Fatal(err, c)
	}
	if r, _ := d.Rev(ctx); r != rev0+1 {
		t.Fatalf("rev %d->%d", rev0, r)
	}
	// hidden everywhere
	res, _ := st.Agenda(ctx, ts("2026-10-15T00:00:00Z"), ts("2026-10-16T00:00:00Z"), nil)
	for _, i := range res.Items {
		if i.CalendarID == "C1" {
			t.Fatal("hidden calendar leaked")
		}
	}
	// no-op patch does not bump rev
	if _, _, err := st.PatchCalendar(ctx, "C1", agenda.CalendarPatch{Visible: &f}); err != nil {
		t.Fatal(err)
	}
	if r, _ := d.Rev(ctx); r != rev0+1 {
		t.Fatal("no-op bumped rev")
	}
	tr := true
	cobalt := "moss"
	co := &cobalt
	c, became, err = st.PatchCalendar(ctx, "C1", agenda.CalendarPatch{Visible: &tr, ColorOverride: &co})
	if err != nil || !became || c.Color != "moss" || !c.Visible {
		t.Fatal(err, c, became)
	}
	var nilStr *string
	if c, _, _ = st.PatchCalendar(ctx, "C1", agenda.CalendarPatch{ColorOverride: &nilStr}); c.Color != "cobalt" || c.ColorOverride != nil {
		t.Fatalf("clear override %+v", c)
	}
	bad := "neon"
	bp := &bad
	var ve *agenda.ValidationError
	if _, _, err := st.PatchCalendar(ctx, "C1", agenda.CalendarPatch{ColorOverride: &bp}); !errors.As(err, &ve) {
		t.Fatal(err)
	}
	if _, _, err := st.PatchCalendar(ctx, "C1", agenda.CalendarPatch{}); !errors.As(err, &ve) {
		t.Fatal(err)
	}
	if _, _, err := st.PatchCalendar(ctx, "nope", agenda.CalendarPatch{Visible: &tr}); !errors.Is(err, agenda.ErrNotFound) {
		t.Fatal(err)
	}
}

func validInput() agenda.SettingsInput {
	ws := 0
	cal := "C1"
	return agenda.SettingsInput{DefaultCalendarID: &cal, WeekStart: &ws, WorkingHours: &agenda.WorkingHours{Start: "08:00", End: "16:00", Days: []int{1, 2, 3}}}
}

func TestSettings(t *testing.T) {
	st, d := world(t)
	def, err := st.Settings(ctx)
	if err != nil || def.WeekStart != 1 || def.UserTZID == nil || *def.UserTZID != "Europe/London" || def.WorkingHours.End != "17:30" || def.DefaultCalendarID != nil {
		t.Fatalf("defaults %+v %v", def, err)
	}
	in := validInput()
	sec := "America/New_York"
	in.SecondaryTZID = &sec
	rev0, _ := d.Rev(ctx)
	got, err := st.PutSettings(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if *got.DefaultCalendarID != "C1" || got.WeekStart != 0 || *got.SecondaryTZID != sec || got.WorkingHours.Start != "08:00" || *got.UserTZID != "Europe/London" {
		t.Fatalf("%+v", got)
	}
	if r, _ := d.Rev(ctx); r != rev0+1 {
		t.Fatal("rev not bumped")
	}
	if _, err := st.PutSettings(ctx, in); err != nil {
		t.Fatal(err)
	}
	if r, _ := d.Rev(ctx); r != rev0+1 {
		t.Fatal("identical PUT bumped rev")
	}

	bad := func(name string, mut func(*agenda.SettingsInput), field string) {
		t.Helper()
		x := validInput()
		mut(&x)
		_, err := st.PutSettings(ctx, x)
		var ve *agenda.ValidationError
		if !errors.As(err, &ve) || ve.Field != field {
			t.Errorf("%s: want validation on %s, got %v", name, field, err)
		}
	}
	str := func(s string) *string { return &s }
	num := func(n int) *int { return &n }
	bad("weekStart 7", func(x *agenda.SettingsInput) { x.WeekStart = num(7) }, "weekStart")
	bad("weekStart missing", func(x *agenda.SettingsInput) { x.WeekStart = nil }, "weekStart")
	bad("wh missing", func(x *agenda.SettingsInput) { x.WorkingHours = nil }, "workingHours")
	bad("bad time", func(x *agenda.SettingsInput) { x.WorkingHours.Start = "25:00" }, "workingHours.start")
	bad("bad time fmt", func(x *agenda.SettingsInput) { x.WorkingHours.End = "5pm" }, "workingHours.end")
	bad("end<=start", func(x *agenda.SettingsInput) { x.WorkingHours.End = "08:00" }, "workingHours.end")
	bad("dup day", func(x *agenda.SettingsInput) { x.WorkingHours.Days = []int{1, 1} }, "workingHours.days")
	bad("day 7", func(x *agenda.SettingsInput) { x.WorkingHours.Days = []int{7} }, "workingHours.days")
	bad("secondary tz", func(x *agenda.SettingsInput) { x.SecondaryTZID = str("Mars/Olympus") }, "secondaryTzid")
	bad("user tz", func(x *agenda.SettingsInput) { x.UserTZID = str("nowhere") }, "userTzid")
	bad("default cal missing", func(x *agenda.SettingsInput) { x.DefaultCalendarID = str("zzz") }, "defaultCalendarId")
	bad("default cal read-only", func(x *agenda.SettingsInput) { x.DefaultCalendarID = str("CH") }, "defaultCalendarId")
	bad("alert", func(x *agenda.SettingsInput) { x.AlertDefaults = &agenda.AlertDefaults{Timed: num(-5)} }, "alertDefaults.timed")
	if after, _ := st.Settings(ctx); *after.DefaultCalendarID != "C1" {
		t.Fatal("invalid PUT must not change anything")
	}
	// null clears defaultCalendar / secondary tz; alert defaults round-trip
	x := validInput()
	x.DefaultCalendarID = nil
	x.AlertDefaults = &agenda.AlertDefaults{Timed: num(5), AllDay: num(540)}
	got, err = st.PutSettings(ctx, x)
	if err != nil || got.DefaultCalendarID != nil || got.SecondaryTZID != nil || *got.AlertDefaults.AllDay != 540 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestUserTZChangeRetimesAllDay(t *testing.T) {
	st, d := world(t)
	x := validInput()
	ny := "America/New_York"
	x.UserTZID = &ny
	if _, err := st.PutSettings(ctx, x); err != nil {
		t.Fatal(err)
	}
	var s, e int64
	if err := d.R.QueryRow(`SELECT start_utc, end_utc FROM occurrences WHERE event_id='E3'`).Scan(&s, &e); err != nil {
		t.Fatal(err)
	}
	if s != u("2026-10-15T04:00:00Z") || e != u("2026-10-16T04:00:00Z") {
		t.Fatalf("not retimed: %s %s", time.Unix(s, 0).UTC(), time.Unix(e, 0).UTC())
	}
	// timed occurrences are untouched
	if err := d.R.QueryRow(`SELECT start_utc FROM occurrences WHERE event_id='E2'`).Scan(&s); err != nil || s != u("2026-10-15T12:00:00Z") {
		t.Fatal("timed changed", s)
	}
	// agenda now windows all-day by New York days
	res, _ := st.Agenda(ctx, ts("2026-10-16T04:00:00Z"), ts("2026-10-17T04:00:00Z"), []string{"C2"})
	for _, i := range res.Items {
		if i.ID == "E3:0" {
			t.Fatal("birthday leaked into next local day")
		}
	}
}

func TestFreeBusy(t *testing.T) {
	st, d := world(t)
	Do(t, d, func(s *Seed) {
		s.Event(Ev{ID: "B1", Cal: "C1", Title: "a", Start: u("2026-10-20T10:00:00Z"), End: u("2026-10-20T11:00:00Z")})
		s.Event(Ev{ID: "B2", Cal: "C2", Title: "b", Start: u("2026-10-20T10:30:00Z"), End: u("2026-10-20T12:00:00Z")}) // overlaps -> merged
		s.Event(Ev{ID: "B3", Cal: "C2", Title: "c", Start: u("2026-10-20T12:00:00Z"), End: u("2026-10-20T13:00:00Z")}) // touches -> merged
		s.Event(Ev{ID: "B4", Cal: "C1", Title: "d", Start: u("2026-10-20T15:00:00Z"), End: u("2026-10-20T16:00:00Z")})
		s.Event(Ev{ID: "B5", Cal: "C1", Title: "free", NotBusy: true, Start: u("2026-10-20T08:00:00Z"), End: u("2026-10-20T09:00:00Z")})
		s.Event(Ev{ID: "B6", Cal: "C1", Title: "declined", Self: "declined", Start: u("2026-10-20T17:00:00Z"), End: u("2026-10-20T18:00:00Z")})
		s.Event(Ev{ID: "B7", Cal: "CH", Title: "hidden", Start: u("2026-10-20T19:00:00Z"), End: u("2026-10-20T20:00:00Z")})
		s.Event(Ev{ID: "B8", Cal: "C1", Title: "cancelled", Status: "cancelled", Start: u("2026-10-20T21:00:00Z"), End: u("2026-10-20T22:00:00Z")})
		s.Event(Ev{ID: "B9", Cal: "C1", Title: "straddles from", Start: u("2026-10-19T23:00:00Z"), End: u("2026-10-20T00:30:00Z")})
	})
	fb, err := st.FreeBusy(ctx, ts("2026-10-20T00:00:00Z"), ts("2026-10-20T15:30:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, b := range fb.Busy {
		got = append(got, b.Start.Format("15:04")+"-"+b.End.Format("15:04"))
	}
	if fmt.Sprint(got) != "[00:00-00:30 09:30-09:45 10:00-13:00 15:00-15:30]" {
		t.Fatalf("%v", got)
	}
	if fb, _ = st.FreeBusy(ctx, ts("2031-01-01T00:00:00Z"), ts("2031-01-02T00:00:00Z")); fb.Busy == nil || len(fb.Busy) != 0 {
		t.Fatal("want empty non-nil")
	}
}

func TestSummarise(t *testing.T) {
	mon := ts("2026-10-12T09:00:00Z") // Monday
	cases := map[string]string{
		"FREQ=DAILY":                                    "Daily",
		"FREQ=DAILY;INTERVAL=3":                         "Every 3 days",
		"FREQ=WEEKLY;BYDAY=TU,TH":                       "Weekly on Tue, Thu",
		"FREQ=WEEKLY":                                   "Weekly on Mon",
		"FREQ=WEEKLY;INTERVAL=2;BYDAY=FR":               "Every 2 weeks on Fri",
		"FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR":              "Every weekday",
		"FREQ=MONTHLY;BYMONTHDAY=15":                    "Monthly on day 15",
		"FREQ=MONTHLY;BYDAY=-1FR":                       "Monthly on the last Fri",
		"FREQ=MONTHLY;BYDAY=2TU":                        "Monthly on the 2nd Tue",
		"FREQ=MONTHLY;BYDAY=FR;BYSETPOS=-1":             "Monthly on the last Fri",
		"FREQ=MONTHLY":                                  "Monthly on day 12",
		"FREQ=YEARLY":                                   "Yearly",
		"FREQ=WEEKLY;BYDAY=MO;COUNT=10":                 "Weekly on Mon, 10 times",
		"FREQ=DAILY;UNTIL=20261231T235959Z":             "Daily, until 31 Dec 2026",
		"FREQ=WEEKLY;BYDAY=SA,SU,TH":                    "Weekly on Thu, Sat, Sun",
		"FREQ=SECONDLY":                                 "Repeats",
		"FREQ=MONTHLY;BYDAY=3SU;INTERVAL=2":             "Every 2 months on the 3rd Sun",
		"FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1": "Monthly on the last Mon",
	}
	for in, want := range cases {
		if got := agenda.Summarise(in, mon); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestAgendaPerformance20k(t *testing.T) {
	d := Open(t)
	base := u("2026-09-01T00:00:00Z")
	n := 20000
	if raceEnabled {
		n = 3000 // seeding is ~30x slower under the race detector
	}
	Do(t, d, func(s *Seed) {
		for a := 0; a < 3; a++ {
			s.Account(fmt.Sprintf("A%d", a), "cobalt", fmt.Sprintf("a%d@x", a))
			s.Calendar(fmt.Sprintf("C%d", a), fmt.Sprintf("A%d", a), "cal", a != 2, "owner", true)
		}
		for i := 0; i < n; i++ {
			st := base + int64(i%(120*24))*3600 + int64(i/(120*24))*900 // spread over 120 days
			ev := Ev{ID: fmt.Sprintf("E%06d", i), Cal: fmt.Sprintf("C%d", i%3), Title: fmt.Sprintf("Event %d", i), Start: st, End: st + 1800}
			if i%25 == 0 {
				day := time.Unix(st, 0).UTC().Format(time.DateOnly)
				next := time.Unix(st, 0).UTC().AddDate(0, 0, 1).Format(time.DateOnly)
				ev.Start, ev.End, ev.SD, ev.ED = 0, 0, day, next
			}
			s.Event(ev)
		}
	})
	st := &agenda.Store{DB: d}
	from, to := ts("2026-10-05T00:00:00Z"), ts("2026-10-19T00:00:00Z")
	var res agenda.AgendaResult
	var best time.Duration
	for i := 0; i < 5; i++ {
		t0 := time.Now()
		var err error
		if res, err = st.Agenda(ctx, from, to, nil); err != nil {
			t.Fatal(err)
		}
		if el := time.Since(t0); i == 0 || el < best {
			best = el
		}
	}
	t.Logf("14-day agenda over 20k events: %d items, best %v", len(res.Items), best)
	if len(res.Items) < n/200 {
		t.Fatalf("suspiciously few items: %d", len(res.Items))
	}
	limit := 50 * time.Millisecond
	if raceEnabled {
		limit = 500 * time.Millisecond // the detector slows queries ~10x; the 50ms budget applies to normal builds
	}
	if best > limit {
		t.Fatalf("agenda too slow: %v", best)
	}
}

func TestBriefNamesCancelledAndDeletedEventsForTwoDays(t *testing.T) {
	st, d := world(t) // now = 2026-10-15T08:00Z
	Do(t, d, func(s *Seed) {
		s.Event(Ev{ID: "B1", Cal: "C1", Title: "Design review", Start: u("2026-10-16T10:00:00Z"), End: u("2026-10-16T11:00:00Z")})
		s.Event(Ev{ID: "B2", Cal: "C1", Title: "Gone yesterday", Start: u("2026-10-16T12:00:00Z"), End: u("2026-10-16T13:00:00Z")})
		s.Event(Ev{ID: "B3", Cal: "C1", Title: "Gone last week", Start: u("2026-10-16T14:00:00Z"), End: u("2026-10-16T15:00:00Z")})
	})
	now := u("2026-10-15T08:00:00Z")
	for id, at := range map[string]int64{"B2": now - 20*3600, "B3": now - 7*24*3600} {
		if _, err := d.W.Exec(`UPDATE events SET status='cancelled', deleted_at=? WHERE id=?`, at, id); err != nil {
			t.Fatal(err)
		}
	}
	b, err := st.Brief(ctx, "B1:0")
	if err != nil || b.Title != "Design review" || b.Cancelled || b.Start == nil || b.Place != "Work" {
		t.Fatalf("live: %+v %v", b, err)
	}
	b, err = st.Brief(ctx, "B2")
	if err != nil || b.Title != "Gone yesterday" || !b.Cancelled {
		t.Fatalf("cancelled yesterday: %+v %v", b, err)
	}
	if _, err := st.Brief(ctx, "B3"); !errors.Is(err, agenda.ErrNotFound) {
		t.Fatalf("cancelled last week: %v", err)
	}
	if _, err := st.Brief(ctx, "nope"); !errors.Is(err, agenda.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}
