package gtest

import (
	"fmt"
	"strings"
	"time"
)

// Profiles of the stand-alone fake (cmd/fakegoogle --profile). The default, e2e, is the small
// dataset that scripts/e2e.sh asserts on and must not change; rich is for living with the app on
// a phone (scripts/run-local.sh demo): every state the UI has, see docs/ops/demo-data.md.
const (
	ProfileE2E  = "e2e"
	ProfileRich = "rich"
)

// RichTZ is the time zone whose midnight starts "today" in the rich profile.
var RichTZ = "Europe/London"

// RichAccounts are the accounts of the rich profile, in order.
func RichAccounts() []string {
	return []string{"work@example.com", "personal@example.com", "side@example.com", "stale@example.com"}
}

// Dates the rich profile pins to the calendar (DST changes, UK holidays). The empty and the
// "only all-day" days are chosen so they never coincide with these.
var richPinned = []string{"2026-10-25", "2026-11-01", "2026-12-25", "2026-12-26", "2027-01-01", "2027-03-14", "2027-03-28"}

// RichLayout returns the offsets (in days from today in tz) of the day that has no events at all
// (a Saturday) and of the day after it that has all-day events only (a Sunday).
func RichLayout(tz string, now time.Time) (empty, quiet int) {
	loc := loadLoc(tz)
	ln := now.In(loc)
	base := time.Date(ln.Year(), ln.Month(), ln.Day(), 0, 0, 0, 0, time.UTC)
	pinned := map[int]bool{}
	pin := func(day int) {
		for d := day - 1; d <= day+1; d++ { // a day either side: zones shift civil dates
			pinned[d] = true
		}
	}
	for _, d := range richPinned {
		t, _ := time.Parse(time.DateOnly, d)
		pin(int(t.Sub(base).Hours() / 24))
	}
	if tr, ok := nextTransition(loc, now); ok { // the profile zone's own next clock change
		t := tr.In(loc)
		pin(int(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Sub(base).Hours() / 24))
	}
	for i := 0; i <= 3; i++ { // the 1st of the next months (a monthly all-day series)
		pin(int(time.Date(ln.Year(), ln.Month()+time.Month(i), 1, 0, 0, 0, 0, time.UTC).Sub(base).Hours() / 24))
	}
	off := 5
	for base.AddDate(0, 0, off).Weekday() != time.Saturday {
		off++
	}
	for pinned[off] || pinned[off+1] {
		off += 7
	}
	return off, off + 1
}

func loadLoc(tz string) *time.Location {
	if l, err := time.LoadLocation(tz); err == nil {
		return l
	}
	l, _ := time.LoadLocation("Europe/London")
	return l
}

type opt func(map[string]any)

type att struct {
	email, resp string
	optional    bool
	resource    bool
}

func g(email, resp string) att { return att{email: email, resp: resp} }
func (a att) opt() att         { a.optional = true; return a }
func (a att) res() att         { a.resource = true; return a }

func niceName(email string) string {
	n, _, _ := strings.Cut(email, "@")
	n = strings.NewReplacer(".", " ", "-", " ", "_", " ").Replace(n)
	return strings.Title(n)
}

type richB struct {
	f           *Fake
	self        string
	now         time.Time
	loc         *time.Location
	tz          string
	today       time.Time
	empty, quit int
}

// SeedRich fills f (one account, identified by f.Self) with the rich profile, all relative to now
// and to the midnight of tz (default RichTZ). It is called again by /_admin/reset to re-base.
func SeedRich(f *Fake, tz string, now time.Time) {
	if tz == "" {
		tz = RichTZ
	}
	loc := loadLoc(tz)
	ln := now.In(loc)
	b := &richB{f: f, self: f.Self, now: now.Truncate(time.Minute), loc: loc, tz: loc.String(),
		today: time.Date(ln.Year(), ln.Month(), ln.Day(), 0, 0, 0, 0, loc)}
	b.empty, b.quit = RichLayout(tz, now)
	switch f.Self {
	case "work@example.com":
		b.work()
	case "personal@example.com":
		b.personal()
	case "side@example.com":
		b.side()
	default:
		b.stale()
	}
}

// ---- builders ----

func (b *richB) at(day, h, m int) time.Time {
	return time.Date(b.today.Year(), b.today.Month(), b.today.Day()+day, h, m, 0, 0, b.loc)
}
func (b *richB) wd(day int) time.Weekday { return b.at(day, 12, 0).Weekday() }
func (b *richB) date(day int) string     { return b.at(day, 12, 0).Format(time.DateOnly) }

// ok moves a day offset off the empty day and the all-day-only day.
func (b *richB) ok(day int) int {
	for day == b.empty || day == b.quit {
		day++
	}
	return day
}

// ok2 is ok for events that run into the next day too.
func (b *richB) ok2(day int) int {
	for day == b.empty || day == b.quit || day+1 == b.empty || day+1 == b.quit {
		day++
	}
	return day
}

// days lists the first n day offsets >= from whose weekday is in wds.
func (b *richB) days(from, n int, wds ...time.Weekday) []int {
	var out []int
	for d := from; len(out) < n; d++ {
		for _, w := range wds {
			if b.wd(d) == w {
				out = append(out, d)
			}
		}
	}
	return out
}

func (b *richB) prevWeekday(from int, w time.Weekday) int {
	for b.wd(from) != w {
		from--
	}
	return from
}

func (b *richB) evt(title string, s time.Time, mins int, tz string) map[string]any {
	if tz == "" {
		tz = b.tz
	}
	l := loadLoc(tz)
	e := s.Add(time.Duration(mins) * time.Minute)
	m := map[string]any{
		"start": map[string]any{"dateTime": s.In(l).Format(time.RFC3339), "timeZone": tz},
		"end":   map[string]any{"dateTime": e.In(l).Format(time.RFC3339), "timeZone": tz},
	}
	if title != "" {
		m["summary"] = title
	}
	return m
}

func (b *richB) allday(title string, from, toExcl int) map[string]any {
	return AllDay(title, b.date(from), b.date(toExcl))
}

func (b *richB) put(cal, id string, raw map[string]any, o ...opt) {
	for _, x := range o {
		x(raw)
	}
	b.f.Put(cal, id, raw)
}

// zoned is day (offset in the profile zone) at h:m wall clock in tz, moved on until neither its
// start nor its end falls on the empty or the all-day-only day as seen in the profile zone.
func (b *richB) zoned(day, h, m, mins int, tz string) time.Time {
	l := loadLoc(tz)
	for ; ; day++ {
		d := b.at(day, 12, 0)
		s := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, l)
		bad := false
		for _, t := range []time.Time{s, s.Add(time.Duration(mins)*time.Minute - time.Minute)} {
			if o := b.offOf(t); o == b.empty || o == b.quit {
				bad = true
			}
		}
		if !bad {
			return s
		}
	}
}

// offOf is the day offset (in the profile zone) of an instant.
func (b *richB) offOf(t time.Time) int {
	x := t.In(b.loc)
	return int(time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.UTC).Sub(time.Date(b.today.Year(), b.today.Month(), b.today.Day(), 0, 0, 0, 0, time.UTC)).Hours() / 24)
}

// okRange moves from so that days from..from+n-1 avoid the empty and the all-day-only day.
func (b *richB) okRange(from, n int) int {
	for {
		bad := false
		for d := from; d < from+n; d++ {
			if d == b.empty || d == b.quit {
				bad = true
			}
		}
		if !bad {
			return from
		}
		from++
	}
}

// timed puts a timed event on day (offset) at h:m local.
func (b *richB) timed(cal, id, title string, day, h, m, mins int, o ...opt) {
	b.put(cal, id, b.evt(title, b.at(b.ok(day), h, m), mins, ""), o...)
}

// allDay puts an all-day event, moved off the empty and the all-day-only day if needed;
// use put(.., b.allday(..)) to place one exactly.
func (b *richB) allDay(cal, id, title string, from, toExcl int, o ...opt) {
	n := toExcl - from
	from = b.okRange(from, n)
	b.put(cal, id, b.allday(title, from, from+n), o...)
}

func (b *richB) master(cal, id, title string, startDay, h, m, mins int, tz string, rr []string, o ...opt) map[string]any {
	raw := b.evt(title, b.at(startDay, h, m), mins, tz)
	raw["recurrence"] = rr
	for _, x := range o {
		x(raw)
	}
	b.f.Put(cal, id, raw)
	return raw
}

// exception stores one changed (or, cancelled) occurrence of a series.
func (b *richB) exception(cal, master string, mraw map[string]any, orig, newStart time.Time, mins int, title string, cancelled bool, o ...opt) {
	x := clone(mraw)
	delete(x, "recurrence")
	delete(x, "etag")
	tz, _ := mraw["start"].(map[string]any)["timeZone"].(string)
	l := loadLoc(tz)
	if title != "" {
		x["summary"] = title
	}
	x["start"] = map[string]any{"dateTime": newStart.In(l).Format(time.RFC3339), "timeZone": tz}
	x["end"] = map[string]any{"dateTime": newStart.Add(time.Duration(mins) * time.Minute).In(l).Format(time.RFC3339), "timeZone": tz}
	x["recurringEventId"] = master
	x["originalStartTime"] = map[string]any{"dateTime": orig.In(l).Format(time.RFC3339), "timeZone": tz}
	if cancelled {
		x["status"] = "cancelled"
	}
	for _, f := range o {
		f(x)
	}
	b.f.Put(cal, master+"_"+orig.UTC().Format("20060102T150405Z"), x)
}

func (b *richB) org(email string) map[string]any {
	return map[string]any{"email": email, "displayName": niceName(email), "self": email == b.self}
}

func attJSON(a att) map[string]any {
	m := map[string]any{"email": a.email, "displayName": niceName(a.email), "responseStatus": a.resp}
	if a.optional {
		m["optional"] = true
	}
	if a.resource {
		m["resource"] = true
		m["displayName"] = "Room: " + niceName(a.email)
	}
	return m
}

// mine: I organise; guests as given, me accepted.
func (b *richB) mine(others ...att) opt {
	return func(r map[string]any) {
		r["organizer"] = b.org(b.self)
		at := []any{map[string]any{"email": b.self, "self": true, "organizer": true, "responseStatus": "accepted", "displayName": "Me"}}
		for _, o := range others {
			at = append(at, attJSON(o))
		}
		r["attendees"] = at
	}
}

// invited: someone else organises; my reply is resp (optional=true marks me as an optional guest).
func (b *richB) invited(orgEmail, resp string, selfOptional bool, others ...att) opt {
	return func(r map[string]any) {
		r["organizer"] = b.org(orgEmail)
		at := []any{map[string]any{"email": orgEmail, "displayName": niceName(orgEmail), "organizer": true, "responseStatus": "accepted"}}
		for _, o := range others {
			at = append(at, attJSON(o))
		}
		me := map[string]any{"email": b.self, "self": true, "responseStatus": resp, "displayName": "Me"}
		if selfOptional {
			me["optional"] = true
		}
		at = append(at, me)
		r["attendees"] = at
	}
}

func someone(email string) opt {
	return func(r map[string]any) {
		r["organizer"] = map[string]any{"email": email, "displayName": niceName(email)}
	}
}

func desc(s string) opt     { return func(r map[string]any) { r["description"] = s } }
func loc(s string) opt      { return func(r map[string]any) { r["location"] = s } }
func free() opt             { return func(r map[string]any) { r["transparency"] = "transparent" } }
func vis(v string) opt      { return func(r map[string]any) { r["visibility"] = v } }
func tentative() opt        { return func(r map[string]any) { r["status"] = "tentative" } }
func typ(t string) opt      { return func(r map[string]any) { r["eventType"] = t } }
func canModify() opt        { return func(r map[string]any) { r["guestsCanModify"] = true } }
func locked() opt           { return func(r map[string]any) { r["locked"] = true } }
func cancelled() opt        { return func(r map[string]any) { r["status"] = "cancelled" } }
func recur(l ...string) opt { return func(r map[string]any) { r["recurrence"] = l } }
func meet(code string) opt {
	return func(r map[string]any) {
		l := "https://meet.google.com/" + code
		r["hangoutLink"] = l
		r["conferenceData"] = map[string]any{"conferenceId": code, "conferenceSolution": map[string]any{"name": "Google Meet"},
			"entryPoints": []any{map[string]any{"entryPointType": "video", "uri": l, "label": "meet.google.com/" + code}}}
	}
}
func phoneOnly(num, pin string) opt {
	return func(r map[string]any) {
		r["conferenceData"] = map[string]any{"conferenceSolution": map[string]any{"name": "Dial-in"},
			"entryPoints": []any{map[string]any{"entryPointType": "phone", "uri": "tel:" + num, "pin": pin, "label": num}}}
	}
}
func attachments() opt {
	return func(r map[string]any) {
		r["attachments"] = []any{
			map[string]any{"fileUrl": "https://docs.google.com/document/d/1AbCdEfGhIjK/edit", "title": "Agenda and notes", "mimeType": "application/vnd.google-apps.document"},
			map[string]any{"fileUrl": "https://drive.google.com/file/d/1XyZ/view", "title": "Q4 numbers.pdf", "mimeType": "application/pdf"},
		}
	}
}
func workLoc(kind, label string) opt {
	return func(r map[string]any) {
		r["eventType"] = "workingLocation"
		p := map[string]any{"type": kind}
		switch kind {
		case "officeLocation":
			p["officeLocation"] = map[string]any{"label": label}
		case "customLocation":
			p["customLocation"] = map[string]any{"label": label}
		default:
			p["homeOffice"] = map[string]any{}
		}
		r["workingLocationProperties"] = p
		r["transparency"] = "transparent"
	}
}

// longTitle is exactly 120 characters.
func longTitle() string {
	s := "Extraordinarily long event title for checking wrapping, truncation and the detail screen with a hundred and twenty characters "
	return string([]rune(s)[:120])
}

func longDescription() string {
	var sb strings.Builder
	sb.WriteString("Planning notes for the whole programme.\n\n")
	for i := 1; i <= 8; i++ {
		fmt.Fprintf(&sb, "Section %d: this paragraph is deliberately wordy so the description needs to wrap over many lines and scroll inside the detail screen. It mentions deliverables, owners, risks and dependencies, and then repeats itself a little to take up room.\n", i)
	}
	sb.WriteString("\nLinks:\n")
	sb.WriteString("- Roadmap: https://example.com/roadmap/q4?view=board&filter=mine\n")
	sb.WriteString("- Budget sheet: https://docs.google.com/spreadsheets/d/1AbCdEfGhIjKlMnOpQrStUvWxYz/edit#gid=0\n")
	sb.WriteString("- Design files: https://www.figma.com/file/AbCdEf123/Calendar-app?node-id=12%3A34\n")
	sb.WriteString("- Ticket: https://github.com/example/calendar/issues/1234\n")
	sb.WriteString("- A very long link: https://example.com/this/is/a/very/long/path/that/keeps/going/and/going/and/going/until/it/must/be/truncated/or/wrapped/somewhere?token=abcdefghijklmnopqrstuvwxyz0123456789&other=value\n")
	sb.WriteString("\nLast line after a blank line. Thanks!\n")
	return sb.String()
}

// nextTransition finds the next UTC-offset change of l after t (within 400 days).
func nextTransition(l *time.Location, t time.Time) (time.Time, bool) {
	_, off := t.In(l).Zone()
	for h := 1; h < 400*24; h++ {
		x := t.Add(time.Duration(h) * time.Hour)
		if _, o := x.In(l).Zone(); o != off {
			for m := -59; m <= 0; m++ { // refine to the minute
				y := x.Add(time.Duration(m) * time.Minute)
				if _, o2 := y.In(l).Zone(); o2 != off {
					return y, true
				}
			}
			return x, true
		}
	}
	return time.Time{}, false
}

func ymd(y int, m time.Month, d int) string {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
}

func utc(y int, mo time.Month, d, h, m int) time.Time {
	return time.Date(y, mo, d, h, m, 0, 0, time.UTC)
}

// ---- work@example.com ----

func (b *richB) work() {
	f := b.f
	pri := f.AddCalWith(b.self, "Work", "owner", "#4285f4", b.tz, false, false).ID
	team := f.AddCalWith("team-platform@group.calendar.google.com", "Team Platform", "reader", "#0b8043", b.tz, false, false).ID
	rel := f.AddCalWith("release-train@import.calendar.google.com", "Release train (subscribed)", "reader", "#8e24aa", "UTC", false, false).ID
	oncall := f.AddCalWith("oncall-rota@group.calendar.google.com", "On-call rota", "writer", "#f4511e", b.tz, false, false).ID
	long := f.AddCalWith("q4-launch-programme@group.calendar.google.com", "Cross-functional product, design, research and operations planning calendar for the Q4 launch programme", "owner", "#795548", b.tz, false, false).ID
	arch := f.AddCalWith("archive-2024@group.calendar.google.com", "Archive 2024", "owner", "#9e9e9e", b.tz, true, true).ID
	rooms := f.AddCalWith("rooms-floor3@resource.calendar.google.com", "Meeting rooms, floor 3", "reader", "#616161", b.tz, false, true).ID

	ana, sam, kofi, maya := "ana@example.com", "sam@example.com", "kofi@example.com", "maya@example.com"

	// --- now-relative (join window, in progress) ---
	b.put(pri, "w-live", b.evt("Incident review: checkout latency", b.now.Add(-20*time.Minute), 60, ""),
		b.invited("ops-lead@example.com", "accepted", false, g(sam, "accepted"), g(kofi, "accepted"), g(ana, "tentative")),
		meet("inc-live-001"), desc("Live incident bridge. Dashboard: https://example.com/dash/checkout"))
	b.put(pri, "w-soon8", b.evt("Customer call: Acme renewal", b.now.Add(8*time.Minute), 30, ""),
		b.mine(g("jo@acme.example", "accepted"), g(sam, "accepted"), g(maya, "needsAction")), meet("acm-renw-008"))
	b.put(pri, "w-soon25", b.evt("Pricing sync", b.now.Add(25*time.Minute), 25, ""),
		loc("https://us02web.zoom.us/j/84012345678?pwd=abcDEF123"), b.mine(g(sam, "accepted")))

	// --- today ---
	b.timed(pri, "w-focus", "Deep work: Q4 roadmap", 0, 10, 0, 120, typ("focusTime"))
	b.timed(pri, "w-lunch", "Lunch with Ama", 0, 12, 30, 60, loc("The Gilbert Scott, Euston Rd, London NW1 2AR, United Kingdom"), free(),
		b.mine(g("ama@example.com", "accepted")))
	b.timed(pri, "w-coffee", "Coffee catch-up", 0, 15, 0, 15, loc("Kiosk by the lifts"))
	b.timed(pri, "w-timesheet", "Submit timesheet", 0, 16, 55, 5)
	b.timed(pri, "w-vendor-demo", "Vendor demo", 0, 14, 0, 45, b.invited("sales@vendor.example", "declined", false, g(kofi, "accepted")), meet("ven-demo-014"))
	b.timed(pri, "w-board-prep", "Board prep (maybe)", 0, 16, 0, 45, tentative(), b.mine())
	b.allDay(pri, "w-loc-office", "Office", 0, 1, workLoc("officeLocation", "Shoreditch HQ"))
	b.allDay(pri, "w-freeze", "Release freeze", -2, 2) // runs through tomorrow
	b.allDay(pri, "w-loc-home1", "Home", 1, 2, workLoc("homeOffice", ""))
	b.allDay(pri, "w-loc-cafe", "Working from a cafe", 3, 4, workLoc("customLocation", "Cafe Nero, Old Street"))

	// --- tomorrow: busy day (13 timed events, a 4-way overlap, back-to-back chain) ---
	b.timed(pri, "w-t1-breakfast", "Breakfast briefing", 1, 8, 0, 30, loc("Canteen"))
	b.timed(pri, "w-t1-poker", "Planning poker", 1, 9, 0, 30, b.mine(g(sam, "accepted"), g(kofi, "accepted")))
	b.timed(pri, "w-t1-int-a", "Interview: Candidate A", 1, 10, 0, 45, b.mine(g("recruiter@example.com", "accepted")), meet("int-aaa-001"))
	b.timed(pri, "w-t1-int-b", "Interview: Candidate B", 1, 10, 45, 45, b.mine(g("recruiter@example.com", "accepted")), meet("int-bbb-002"))
	b.timed(pri, "w-t1-debrief", "Interview debrief", 1, 11, 30, 30, b.mine(g("recruiter@example.com", "accepted")))
	b.timed(pri, "w-t1-design", "Design review", 1, 13, 0, 60, b.mine(g(sam, "accepted"), g(kofi, "accepted")), meet("dsg-revw-101"))
	b.timed(pri, "w-t1-eng", "Eng sync", 1, 13, 0, 45, b.invited(kofi, "accepted", false), meet("eng-sync-102"))
	b.timed(pri, "w-t1-121", "1:1 with Sam", 1, 13, 15, 45, b.mine(g(sam, "accepted")))
	b.timed(pri, "w-t1-budget", "Budget review", 1, 13, 30, 60, b.invited("finance@example.com", "tentative", false, g(maya, "accepted")))
	b.timed(pri, "w-t1-planning", "Planning", 1, 14, 30, 90)
	b.timed(pri, "w-t1-retro", "Retro", 1, 16, 0, 30)
	b.timed(pri, "w-t1-signoff", "Release sign-off", 1, 16, 30, 30, b.mine(g(sam, "accepted")))
	b.timed(pri, "w-t1-drinks", "Team drinks", 1, 17, 30, 120, loc("The Cat & Mutton, 76 Broadway Market, London E8 4QJ"), free())
	b.allDay(pri, "w-offsite", "Team offsite", 1, 4) // multi-day all-day

	// --- invitations that need a reply ---
	b.timed(pri, "w-inv-qsync", "Quarterly sync", 2, 16, 0, 60,
		b.invited("boss@example.com", "needsAction", false, g(ana, "accepted"), g(maya, "needsAction")), meet("qtr-sync-202"),
		desc("Note from Boss: please reply by Friday and bring last quarter's numbers. If you cannot make it, tell me who will cover.\n\nAgenda: https://example.com/agenda/q-sync"))
	b.timed(pri, "w-inv-vendor", "Intro with new vendor", 3, 11, 30, 30, b.invited("rep@newvendor.example", "needsAction", false), loc("Visitor lounge, reception"))
	wk := b.days(-14, 1, time.Thursday)[0] // repeating invitation
	b.master(pri, "w-inv-weekly", "Weekly product review", wk, 10, 0, 45, "", []string{"RRULE:FREQ=WEEKLY;BYDAY=TH"},
		b.invited(maya, "needsAction", false, g(sam, "accepted"), g(kofi, "needsAction")), meet("prd-revw-303"))
	b.timed(pri, "w-inv-accepted", "Roadmap workshop", 3, 14, 0, 90, b.invited(maya, "accepted", false, g(sam, "accepted"), g(ana, "declined"), g(kofi, "tentative")), meet("road-wksp-04"))
	b.timed(pri, "w-inv-tentative", "Hack session", 4, 15, 0, 60, b.invited(sam, "tentative", false, g(kofi, "accepted")))
	b.timed(pri, "w-inv-declined", "All-hands prep", 2, 10, 0, 45, b.invited(maya, "declined", false, g(sam, "accepted")))
	b.timed(pri, "w-inv-optional", "Demo day (optional)", 3, 16, 30, 60, b.invited(maya, "needsAction", true, g(sam, "accepted")), meet("demo-day-505"))
	b.timed(pri, "w-inv-edit", "Shared planning doc review", 5, 11, 0, 45, b.invited(sam, "accepted", false, g(kofi, "accepted")), canModify(), meet("shr-plan-606"))
	b.timed(pri, "w-inv-noedit", "Org-wide Q&A", 5, 12, 0, 45, b.invited("ceo@example.com", "accepted", false, g(kofi, "accepted")), meet("org-qanda-07"))

	// --- guests ---
	b.timed(pri, "w-kickoff", "Q4 kickoff", 3, 10, 0, 60, b.invited(maya, "accepted", false,
		g(sam, "accepted"), g(kofi, "declined"), g(ana, "tentative"), g("zainab@example.com", "needsAction"), g("li.wei@example.com", "accepted"),
		g("tomas@example.com", "accepted").opt(), g("priya@example.com", "needsAction").opt(), g("eng-all@example.com", "accepted"),
		g("room-3a@resource.calendar.google.com", "accepted").res(), g("noor@example.com", "declined"), g("omar@example.com", "tentative")),
		meet("q4-kick-0001"), attachments())
	b.timed(pri, "w-one-guest", "Quick chat", 2, 12, 0, 30, b.mine(g(sam, "accepted")))
	b.timed(pri, "w-no-guests", "Write quarterly summary", 2, 14, 0, 60)
	b.timed(pri, "w-org-other", "Architecture board", 4, 10, 0, 60, b.invited("cto@example.com", "accepted", false, g(sam, "accepted")), meet("arch-brd-088"))

	// --- different time zones ---
	b.put(pri, "w-ny", b.evt("Customer workshop (New York)", b.zoned(2, 9, 0, 90, "America/New_York"), 90, "America/New_York"),
		b.mine(g("pat@nyc.example", "accepted")), meet("nyc-wksp-099"))
	b.put(pri, "w-india", b.evt("India standup", b.zoned(3, 10, 0, 30, "Asia/Kolkata"), 30, "Asia/Kolkata"), meet("ind-stnd-011"))
	b.put(pri, "w-tokyo", b.evt("Tokyo early call", b.zoned(4, 7, 30, 45, "Asia/Tokyo"), 45, "Asia/Tokyo"), meet("tok-earl-012"))
	b.put(pri, "w-lordhowe", b.evt("Lord Howe sync (+30 min zone)", b.zoned(5, 10, 0, 60, "Australia/Lord_Howe"), 60, "Australia/Lord_Howe"))
	b.put(pri, "w-kiritimati", b.evt("Date line sync (UTC+14)", b.zoned(6, 9, 0, 45, "Pacific/Kiritimati"), 45, "Pacific/Kiritimati"))
	b.put(pri, "w-kathmandu", b.evt("Kathmandu check-in (+5:45)", b.zoned(7, 11, 0, 30, "Asia/Kathmandu"), 30, "Asia/Kathmandu"))

	// --- length and layout extremes ---
	b.timed(pri, "w-10h", "Customer onsite: Manchester", 4, 8, 0, 600, loc("Spinningfields, Manchester M3 3AX"))
	b.put(pri, "w-overnight", b.evt("Overnight deploy window", b.at(b.ok2(5), 22, 0), 240, ""), b.mine(g(sam, "accepted")))
	b.put(pri, "w-5min", b.evt("Stand up and stretch", b.at(b.ok(4), 12, 0), 5, ""))
	b.put(pri, "w-15min", b.evt("Hallway sync", b.at(b.ok(4), 12, 30), 15, ""))
	b.put(pri, "w-midnight-exact", b.evt("Ends at midnight", b.at(b.ok2(8), 22, 0), 120, ""))
	b.put(pri, "w-conf-week", b.allday("Conference week", b.quit+1, b.quit+6))
	b.put(pri, "w-quiet-ad", b.allday("Company day of rest", b.quit, b.quit+1), free())

	// --- content states ---
	b.timed(pri, "w-longtitle", longTitle(), 6, 10, 0, 60)
	b.timed(pri, "w-notitle", "", 6, 11, 30, 45)
	b.timed(pri, "w-emoji", "Kōya brunch 🍜 with the team 🎉", 7, 10, 0, 90, loc("Kōya, 49 Frith St, London W1D 4SG"))
	b.timed(pri, "w-jp", "会議 (Kaigi) — weekly sync 🇯🇵", 7, 14, 0, 45, meet("kai-gi-0001"))
	b.timed(pri, "w-rtl", "مرحبا — درس اللغة العربية", 8, 9, 0, 60, desc("مرحبا بكم في الدرس الأول\nשלום עולם — mixed direction text"))
	b.timed(pri, "w-accents", "Café Renée: naïve résumé review (Zoë, Søren, Łukasz)", 8, 11, 0, 60)
	b.timed(pri, "w-longdesc", "Programme planning (long notes)", 9, 10, 0, 120, desc(longDescription()), loc("Conference room 4, 2nd floor, 20 Old Street, London EC1V 9AB"))
	b.timed(pri, "w-html", "HTML-ish description", 9, 14, 0, 45, desc(`<b>Agenda</b><br>1. Review &amp; decide<br><ul><li>Item &lt;one&gt;</li></ul><script>alert('x')</script><a href="https://example.com/a">a link</a> and a literal <i>tag</i>`))
	b.timed(pri, "w-loc-url", "Walkthrough (location is a link)", 10, 10, 0, 30, loc("https://example.com/rooms/42"))
	b.timed(pri, "w-zoom-desc", "Vendor review (Zoom in notes)", 6, 14, 0, 45, desc("Join Zoom Meeting\nhttps://acme.zoom.us/j/93810234567?pwd=Zk1xcGJ4\n\nMeeting ID: 938 1023 4567"))
	b.timed(pri, "w-teams-loc", "Partner call (Teams in location)", 6, 16, 0, 45, loc("Microsoft Teams Meeting https://teams.microsoft.com/l/meetup-join/19%3ameeting_ZmE2YTk%40thread.v2/0?context=%7b%22Tid%22%7d"))
	b.timed(pri, "w-webex", "Training (Webex)", 7, 16, 0, 60, desc("Join: https://acme.webex.com/meet/jdoe"))
	b.timed(pri, "w-phone", "Dial-in only call", 7, 12, 0, 30, phoneOnly("+44 20 7946 0000", "123456"), desc("Dial +44 20 7946 0000, PIN 123456#"))
	b.timed(pri, "w-nolink", "Whiteboard session (no link)", 10, 14, 0, 60, loc("Room 5"))
	b.timed(pri, "w-private", "Performance review (private)", 2, 11, 0, 45, vis("private"), b.mine())
	b.timed(pri, "w-conf-vis", "Salary calibration (confidential)", 3, 15, 0, 45, vis("confidential"))
	b.timed(pri, "w-free", "Gym (free time)", 5, 13, 0, 60, free())
	b.timed(pri, "w-attach", "Review with attachments", 10, 11, 30, 45, attachments(), b.mine(g(sam, "accepted")))
	b.timed(pri, "w-locked", "Locked system event", 10, 16, 0, 30, locked())
	b.timed(pri, "w-cancelled", "Strategy offsite (cancelled by organizer)", 4, 16, 0, 60, b.invited(maya, "needsAction", false), cancelled())
	b.timed(pri, "w-ooo-timed", "Doctor (out of office)", 5, 14, 30, 120, typ("outOfOffice"))
	b.allDay(pri, "w-ooo", "Out of office: conference travel", 8, 10, typ("outOfOffice"))
	b.allDay(pri, "w-ooo-untitled", "", 10, 11, typ("outOfOffice"))
	b.allDay(pri, "w-allday-a", "Payroll cut-off", 2, 3)
	b.allDay(pri, "w-allday-b", "Hack week starts", 2, 3)
	b.allDay(pri, "w-allday-c", "Security training due", 2, 3)

	// --- past ---
	b.timed(pri, "w-p1-retro", "Yesterday's retro", -1, 15, 0, 45, b.mine(g(sam, "accepted")))
	b.timed(pri, "w-p1-missed", "Missed call", -1, 11, 0, 30, b.invited(maya, "declined", false))
	b.timed(pri, "w-p3-wrap", "Sprint review", -3, 14, 0, 60, b.mine(g(sam, "accepted"), g(kofi, "accepted")), meet("spr-revw-91"))
	b.allDay(pri, "w-p5-trip", "Customer trip", -9, -5)
	b.timed(pri, "w-p7-ws", "Workshop last week", -7, 10, 0, 180)
	b.timed(pri, "w-p14", "Two weeks ago", -14, 13, 0, 60)
	b.timed(pri, "w-p300", "Old project kickoff (300 days ago)", -300, 10, 0, 60)

	// --- far future ---
	b.timed(pri, "w-f45", "Quarterly planning", 45, 9, 0, 240)
	b.put(pri, "w-f200", b.evt("Lord Howe retreat (200 days out)", b.zoned(200, 10, 0, 480, "Australia/Lord_Howe"), 480, "Australia/Lord_Howe"))

	// --- daylight saving ---
	lon := loadLoc("Europe/London")
	ny := loadLoc("America/New_York")
	dst := func(id, title string, start time.Time, mins int, tz string) {
		b.put(pri, id, b.evt(title, start, mins, tz))
	}
	dst("w-dst-uk-1", "UK clocks go back: first 01:30 (BST)", utc(2026, 10, 25, 0, 30).In(lon), 30, "Europe/London")
	dst("w-dst-uk-2", "UK clocks go back: second 01:30 (GMT)", utc(2026, 10, 25, 1, 30).In(lon), 30, "Europe/London")
	dst("w-dst-uk-span", "Night ops across the repeated hour", utc(2026, 10, 24, 23, 30).In(lon), 180, "Europe/London")
	dst("w-dst-uk-spring", "UK clocks go forward: cutover across the gap", utc(2027, 3, 28, 0, 30).In(lon), 60, "Europe/London")
	dst("w-dst-us-1", "US clocks go back: 01:30 EDT", utc(2026, 11, 1, 5, 30).In(ny), 30, "America/New_York")
	dst("w-dst-us-2", "US clocks go back: 01:30 EST", utc(2026, 11, 1, 6, 30).In(ny), 30, "America/New_York")
	dst("w-dst-us-spring", "US clocks go forward: maintenance across the gap", utc(2027, 3, 14, 6, 30).In(ny), 60, "America/New_York")
	if tr, ok := nextTransition(b.loc, b.now); ok { // always something near the next real change in the profile zone
		dst("w-dst-next", "Next clock change in "+b.tz+" (window around it)", tr.Add(-30*time.Minute), 90, b.tz)
	}

	// --- recurrence ---
	su := b.prevWeekday(-56, time.Monday)
	st := b.master(pri, "w-standup", "Standup", su, 9, 30, 15, "", []string{"RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
		b.mine(g(sam, "accepted"), g(kofi, "accepted"), g(ana, "accepted")), meet("dly-stnd-001"))
	if wds := b.days(3, 1, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday); len(wds) == 1 {
		b.exception(pri, "w-standup", st, b.at(wds[0], 9, 30), b.at(wds[0], 9, 30), 15, "", true)
	}
	if wds := b.days(3, 2, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday); len(wds) == 2 {
		b.exception(pri, "w-standup", st, b.at(wds[1], 9, 30), b.at(wds[1], 11, 0), 15, "Standup (moved: offsite breakfast)", false)
	}
	tu := b.prevWeekday(-35, time.Tuesday)
	occ := b.days(1, 5, time.Tuesday, time.Thursday)
	exd := fmt.Sprintf("EXDATE;TZID=%s:%s", b.tz, b.at(occ[3], 14, 0).Format("20060102T150405"))
	crit := b.master(pri, "w-crit", "Design crit", tu, 14, 0, 60, "", []string{"RRULE:FREQ=WEEKLY;BYDAY=TU,TH", exd},
		b.mine(g(sam, "accepted"), g(ana, "accepted")), meet("dsg-crit-777"))
	b.exception(pri, "w-crit", crit, b.at(occ[0], 14, 0), b.at(occ[0], 16, 0), 60, "", false)
	b.exception(pri, "w-crit", crit, b.at(occ[1], 14, 0), b.at(occ[1], 14, 0), 60, "", true)
	b.exception(pri, "w-crit", crit, b.at(occ[2], 14, 0), b.at(occ[2], 14, 0), 60, "Design crit: Q4 mocks", false)
	bc := b.days(-2, 1, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday)[0]
	b.master(pri, "w-bootcamp", "Onboarding bootcamp (7 days)", bc, 11, 0, 60, "", []string{"RRULE:FREQ=DAILY;COUNT=7;BYDAY=MO,TU,WE,TH,FR"}, b.mine())
	b.master(pri, "w-demoday", "Monthly demo day (last Friday)", b.lastFriday(-80), 16, 0, 45, "", []string{"RRULE:FREQ=MONTHLY;BYDAY=-1FR"}, b.mine(g(sam, "accepted")), meet("mth-demo-444"))
	b.master(pri, "w-sprint", "Sprint planning (every 2 weeks)", b.prevWeekday(-42, time.Monday), 10, 0, 90, "", []string{"RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=MO"}, b.mine(g(sam, "accepted")))
	b.master(pri, "w-warroom", "Q3 launch war room (ended)", b.prevWeekday(-70, time.Monday), 15, 0, 30, "",
		[]string{"RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR;UNTIL=" + b.at(-35, 23, 59).UTC().Format("20060102T150405Z")}, b.mine())
	nm := b.days(4, 1, time.Monday)[0]
	b.master(pri, "w-hackweek", "Hackweek", nm, 10, 0, 420, "", []string{"RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR;COUNT=5"}, b.mine())
	b.master(pri, "w-mentoring", "Mentoring (starts next week)", nm, 17, 0, 30, "", []string{"RRULE:FREQ=WEEKLY;COUNT=8"}, b.mine(g(ana, "accepted")))
	ann := b.ok(14)
	d := b.at(ann, 12, 0)
	b.put(pri, "w-anniv", AllDay("Company anniversary", ymd(d.Year()-3, d.Month(), d.Day()), ymd(d.Year()-3, d.Month(), d.Day()+1)),
		recur("RRULE:FREQ=YEARLY"))

	// --- other work calendars ---
	b.timed(team, "t-retro", "Team retro", 3, 10, 0, 60)
	b.timed(team, "t-inv-ro", "Platform roadmap review (invite on a read-only calendar)", 3, 15, 0, 45,
		b.invited("platform-lead@example.com", "needsAction", false, g(sam, "accepted")))
	b.timed(team, "t-share", "Platform show and tell", 5, 15, 0, 45, b.mine())
	b.master(team, "t-weekly", "Platform weekly", b.prevWeekday(-28, time.Wednesday), 11, 0, 30, "", []string{"RRULE:FREQ=WEEKLY;BYDAY=WE"}, b.invited("platform-lead@example.com", "accepted", false))
	b.timed(rel, "r-1", "Release 2.14 cut", 2, 9, 0, 15)
	b.timed(rel, "r-2", "Release 2.14 ship", 6, 9, 0, 15)
	b.allDay(rel, "r-3", "Release 2.15 code freeze", 12, 13)
	b.master(oncall, "o-handover", "On-call handover", b.prevWeekday(-21, time.Wednesday), 9, 0, 30, "", []string{"RRULE:FREQ=WEEKLY;BYDAY=WE"}, b.mine(g(sam, "accepted")))
	b.allDay(oncall, "o-week", "On-call: primary", 0, 5, typ("outOfOffice"))
	b.timed(long, "l-1", "Launch readiness review", 2, 13, 0, 60, b.mine(g(sam, "accepted")))
	b.timed(long, "l-2", "Launch comms rehearsal", 5, 16, 0, 60)
	b.timed(arch, "a-1", "Archived retro 2024", -300, 10, 0, 60)
	b.timed(arch, "a-2", "Hidden calendar event (not shown)", 1, 12, 0, 30)
	b.timed(rooms, "rm-1", "Room 3A booked", 1, 13, 0, 60)
}

// lastFriday: the last Friday of the month that contains day offset d.
func (b *richB) lastFriday(d int) int {
	t := b.at(d, 12, 0)
	last := time.Date(t.Year(), t.Month()+1, 0, 12, 0, 0, 0, b.loc)
	for last.Weekday() != time.Friday {
		last = last.AddDate(0, 0, -1)
	}
	return int(time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, time.UTC).Sub(time.Date(b.today.Year(), b.today.Month(), b.today.Day(), 0, 0, 0, 0, time.UTC)).Hours() / 24)
}

// ---- personal@example.com ----

func (b *richB) personal() {
	f := b.f
	pri := f.AddCalWith(b.self, "Personal", "owner", "#d50000", b.tz, false, false).ID
	fam := f.AddCalWith("family-8812@group.calendar.google.com", "Family", "writer", "#f6bf26", b.tz, false, false).ID
	bday := f.AddCalWith("addressbook#contacts@group.v.calendar.google.com", "Birthdays", "reader", "#7986cb", b.tz, false, false).ID
	hol := f.AddCalWith("en.uk#holiday@group.v.calendar.google.com", "Holidays in United Kingdom", "reader", "#33b679", b.tz, false, false).ID

	b.timed(pri, "p-run", "Morning run", 0, 6, 45, 45, free())
	b.timed(pri, "p-dentist", "Dentist check-up", 0, 17, 15, 45, loc("Smile Dental, 14 Harley St, London W1G 9PH"))
	b.timed(pri, "p-mum", "Call with Mum", 1, 15, 0, 30, desc("Remember to ask about the weekend."))
	b.timed(pri, "p-concert", "Concert: The Cinematic Orchestra", 4, 19, 30, 150, loc("Royal Albert Hall, Kensington Gore, London SW7 2AP"), free())
	b.timed(pri, "p-padel-cx", "Padel (cancelled by organizer)", 2, 18, 0, 60, b.invited("friend@example.com", "needsAction", false), cancelled())
	b.put(pri, "p-flight", b.evt("Flight LHR to JFK (from Gmail)", b.at(b.ok2(6), 23, 30), 450, ""), typ("fromGmail"), loc("Heathrow Terminal 5"))
	b.timed(pri, "p-maybe", "Maybe: pottery pop-up (status tentative)", 3, 12, 0, 60, tentative(), b.mine())
	b.timed(pri, "p-hotel", "Hotel check-in: The Standard (from Gmail)", 7, 15, 0, 30, typ("fromGmail"))
	b.timed(pri, "p-brunch", "Brunch with the Okafors", 8, 11, 0, 90, b.invited("kemi@example.com", "accepted", false, g("tunde@example.com", "tentative")))
	b.timed(pri, "p-yest", "Dinner with Kwesi", -1, 19, 30, 120, loc("Dishoom, Shoreditch"))
	b.timed(pri, "p-p4", "Pottery open studio", -4, 10, 0, 120)
	b.timed(pri, "p-padel-future", "Padel", 9, 18, 0, 60, b.invited("friend@example.com", "needsAction", false, g("sam@example.com", "accepted")))
	cl := b.master(pri, "p-climb", "Climbing", b.prevWeekday(-28, time.Tuesday), 18, 30, 90, "", []string{"RRULE:FREQ=WEEKLY;BYDAY=TU,TH"})
	co := b.days(1, 3, time.Tuesday, time.Thursday)
	b.exception(pri, "p-climb", cl, b.at(co[1], 18, 30), b.at(co[1], 18, 30), 90, "", true)
	b.exception(pri, "p-climb", cl, b.at(co[2], 18, 30), b.at(co[2], 19, 30), 90, "Climbing (late slot)", false)

	b.allDay(fam, "f-mum", "Mum visiting", 0, 3)
	b.allDay(fam, "f-inset", "School INSET day", 0, 1)
	b.timed(fam, "f-dinner", "Family dinner", 2, 19, 0, 120, loc("Grandma's, 22 Acacia Avenue"))
	b.timed(fam, "f-concert", "School concert", 5, 18, 0, 90, loc("St Mary's Primary School Hall"))
	b.timed(fam, "f-inv", "Parents evening", 3, 17, 0, 20, b.invited("school@example.com", "needsAction", false))
	b.put(fam, "f-hol", b.allday("Half term", b.quit+8, b.quit+13))

	names := []struct {
		id, n string
		off   int
		years int
	}{{"b-kwame", "Kwame's birthday", 0, 31}, {"b-ama", "Ama's birthday", 4, 28}, {"b-sam", "Sam's birthday", 9, 35}, {"b-yaa", "Yaa's birthday", -3, 8}, {"b-kofi", "Kofi's birthday", 25, 40}, {"b-esi", "Esi's birthday", 60, 29}}
	for _, n := range names {
		d := b.at(b.ok(n.off), 12, 0)
		raw := AllDay(n.n, ymd(d.Year()-n.years, d.Month(), d.Day()), ymd(d.Year()-n.years, d.Month(), d.Day()+1))
		raw["recurrence"] = []any{"RRULE:FREQ=YEARLY"}
		raw["transparency"] = "transparent"
		raw["eventType"] = "birthday"
		raw["birthdayProperties"] = map[string]any{"type": "birthday"}
		b.f.Put(bday, n.id, raw)
	}
	b.put(hol, "h-quiet", b.allday("Bank holiday (demo)", b.quit, b.quit+1))
	b.put(hol, "h-xmas", AllDay("Christmas Day", "2026-12-25", "2026-12-26"))
	b.put(hol, "h-boxing", AllDay("Boxing Day", "2026-12-26", "2026-12-27"))
	b.put(hol, "h-ny", AllDay("New Year's Day", "2027-01-01", "2027-01-02"))
}

// ---- side@example.com ----

func (b *richB) side() {
	f := b.f
	pri := f.AddCalWith(b.self, "Side project", "owner", "#039be5", "Europe/Paris", false, false).ID
	pot := f.AddCalWith("pottery-class@group.calendar.google.com", "Pottery class", "writer", "#e67c73", "Europe/Paris", false, false).ID
	fb := f.AddCalWith("studio-partner@example.com", "Studio partner (busy only)", "freeBusyReader", "#b39ddb", "Europe/Paris", false, false).ID

	b.timed(pri, "s-blog", "Write blog post", 1, 20, 0, 90)
	b.allDay(pri, "s-ship", "Ship v0.3", 3, 4)
	b.timed(pri, "s-interview", "Customer interview", 4, 11, 0, 45, desc("Zoom: https://us06web.zoom.us/j/81234567890"))
	b.timed(pri, "s-past", "Hack evening", -2, 20, 0, 120)
	first := b.at(b.firstOfMonth(), 12, 0)
	b.put(pri, "s-invoice", AllDay("Invoice day", ymd(first.Year(), first.Month(), 1), ymd(first.Year(), first.Month(), 2)), recur("RRULE:FREQ=MONTHLY;BYMONTHDAY=1"))
	pt := b.master(pot, "s-pottery", "Pottery class", b.prevWeekday(-21, time.Wednesday), 19, 0, 120, "", []string{"RRULE:FREQ=WEEKLY;BYDAY=WE"}, loc("Clay Studio, 3 Rue de la Paix"))
	po := b.days(1, 3, time.Wednesday)
	b.exception(pot, "s-pottery", pt, b.at(po[1], 19, 0), b.at(po[1], 19, 0), 120, "", true)
	b.allDay(pot, "s-expo", "Pottery exhibition", 20, 22)
	for i, d := range []int{1, 2, 4, 6} {
		b.timed(fb, fmt.Sprintf("fb-%d", i), "Busy", d, 10+i, 0, 90)
	}
}

func (b *richB) firstOfMonth() int {
	n := time.Date(b.today.Year(), b.today.Month(), 1, 12, 0, 0, 0, b.loc)
	return int(time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.UTC).Sub(time.Date(b.today.Year(), b.today.Month(), b.today.Day(), 0, 0, 0, 0, time.UTC)).Hours() / 24)
}

// ---- stale@example.com ----

func (b *richB) stale() {
	f := b.f
	pri := f.AddCalWith(b.self, "Stale account", "owner", "#ad1457", "America/New_York", false, false).ID
	old := f.AddCalWith("old-client@group.calendar.google.com", "Old client", "reader", "#3f51b5", "America/New_York", false, false).ID
	b.timed(pri, "x-today", "Stale: wrap-up call", 0, 17, 0, 30)
	b.timed(pri, "x-tomorrow", "Stale: client check-in", 1, 10, 0, 45, meet("stl-chck-001"))
	b.allDay(pri, "x-audit", "Stale: audit week", 2, 5)
	b.timed(old, "x-old", "Old client: kickoff", 3, 14, 0, 60)
}
