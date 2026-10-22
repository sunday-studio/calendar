package gtest

import (
	"strings"
	"time"
)

const seedTZ = "Europe/London"

// Seed fills f with a spread of events relative to now, for the e2e stack (cmd/fakegoogle).
// idx 0 is the "work" account (primary + a read-only team calendar + holidays), idx 1 the
// "personal" one (primary + a family calendar). Every scenario the e2e script asserts on is here.
func Seed(f *Fake, idx int, now time.Time) {
	loc, _ := time.LoadLocation(seedTZ)
	ln := now.In(loc)
	today := time.Date(ln.Year(), ln.Month(), ln.Day(), 0, 0, 0, 0, loc)
	at := func(day, h, m int) time.Time {
		return today.AddDate(0, 0, day).Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	}
	ts := func(day, h, m int) string { return at(day, h, m).Format(time.RFC3339) }
	timed := func(sum string, day, h, m, mins int) map[string]any {
		return Timed(sum, ts(day, h, m), at(day, h, m).Add(time.Duration(mins)*time.Minute).Format(time.RFC3339))
	}
	guests := func(raw map[string]any, org string, self string, resp string, others ...string) {
		raw["organizer"] = map[string]any{"email": org, "self": org == self}
		at := []any{}
		for _, o := range others {
			at = append(at, map[string]any{"email": o, "displayName": strings.Title(strings.Split(o, "@")[0]), "responseStatus": "accepted"})
		}
		at = append(at, map[string]any{"email": self, "self": true, "responseStatus": resp})
		raw["attendees"] = at
	}
	meet := func(raw map[string]any, code string) {
		l := "https://meet.google.com/" + code
		raw["hangoutLink"] = l
		raw["conferenceData"] = map[string]any{"entryPoints": []any{map[string]any{"entryPointType": "video", "uri": l}}}
	}
	weekdayFrom := func(d int) int {
		for {
			if wd := at(d, 12, 0).Weekday(); wd != time.Saturday && wd != time.Sunday {
				return d
			}
			d++
		}
	}
	// recurring master with one moved and one cancelled occurrence
	series := func(cal, id, title string, h, m, mins int, rrule string) {
		master := timed(title, -7, h, m, mins)
		master["recurrence"] = []any{rrule}
		f.Put(cal, id, master)
		d1 := weekdayFrom(1)
		d2 := weekdayFrom(d1 + 1)
		basic := func(t time.Time) string { return t.UTC().Format("20060102T150405Z") }
		orig := func(t time.Time) map[string]any {
			return map[string]any{"dateTime": t.Format(time.RFC3339), "timeZone": seedTZ}
		}
		ex := timed(title+" (moved)", d1, h+2, m, mins)
		ex["recurringEventId"], ex["originalStartTime"] = id, orig(at(d1, h, m))
		f.Put(cal, id+"_"+basic(at(d1, h, m)), ex)
		cx := timed(title, d2, h, m, mins)
		cx["recurringEventId"], cx["originalStartTime"], cx["status"] = id, orig(at(d2, h, m)), "cancelled"
		f.Put(cal, id+"_"+basic(at(d2, h, m)), cx)
	}

	if idx == 0 {
		self := f.Self
		pri := f.AddCal(self, "Alex (Work)", "owner").ID
		team := f.AddCal("team-"+self+"@group.calendar.google.com", "Team", "reader").ID
		hol := f.AddCal("en.uk#holiday@group.v.calendar.google.com", "UK Holidays", "reader").ID
		series(pri, "standup", "Standup", 9, 30, 15, "RRULE:FREQ=DAILY;BYDAY=MO,TU,WE,TH,FR")
		r := timed("Design review", 0, 11, 0, 45)
		guests(r, self, self, "accepted", "sam@example.com", "kofi@example.com")
		meet(r, "dsg-revw-001")
		f.Put(pri, "design-review", r)
		f.Put(pri, "offsite", AllDay("Team offsite", at(1, 0, 0).Format(time.DateOnly), at(3, 0, 0).Format(time.DateOnly)))
		pl := timed("Planning", 1, 14, 0, 90)
		f.Put(pri, "planning", pl)
		one := timed("1:1 with Sam", 1, 15, 0, 30)
		guests(one, self, self, "accepted", "sam@example.com")
		f.Put(pri, "one-on-one", one)
		inv := timed("Quarterly sync", 2, 16, 0, 60)
		guests(inv, "boss@example.com", self, "needsAction", "boss@example.com", "ama@example.com")
		meet(inv, "qtr-sync-002")
		f.Put(pri, "quarterly-sync", inv)
		f.Put(pri, "lunch", func() map[string]any { x := timed("Lunch", 0, 13, 0, 60); x["location"] = "Kōya, Soho"; return x }())
		f.Put(team, "team-retro", timed("Team retro", 3, 10, 0, 60))
		f.Put(hol, "holiday", AllDay("Bank holiday", at(10, 0, 0).Format(time.DateOnly), at(11, 0, 0).Format(time.DateOnly)))
		return
	}
	self := f.Self
	pri := f.AddCal(self, "Personal", "owner").ID
	fam := f.AddCal("family-"+self+"@group.calendar.google.com", "Family", "writer").ID
	series(pri, "climbing", "Climbing", 18, 30, 90, "RRULE:FREQ=WEEKLY;BYDAY=TU,TH")
	f.Put(pri, "dentist", timed("Dentist", 0, 8, 30, 45))
	f.Put(pri, "clash", timed("Call with Mum", 1, 14, 30, 30)) // overlaps work "Planning" across accounts
	f.Put(pri, "birthday", AllDay("Kwame's birthday", at(4, 0, 0).Format(time.DateOnly), at(5, 0, 0).Format(time.DateOnly)))
	f.Put(fam, "dinner", timed("Family dinner", 2, 19, 0, 120))
}
