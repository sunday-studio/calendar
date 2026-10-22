// Package fixtures serves canned, "now"-relative v1 responses so the iOS app can be
// built without real sync (`calendard serve --fixtures`). It uses the real api DTOs.
package fixtures

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // zone data for Europe/London in distroless images

	"calendar/server/internal/agenda"
	"calendar/server/internal/api"
)

const (
	Rev = 1

	AccCobalt = "01FIXACC00000000000000COB1"
	AccRose   = "01FIXACC00000000000000ROS1"
	AccOchre  = "01FIXACC00000000000000OCH1"

	CalWork     = "01FIXCAL00000000000000WRK1"
	CalTeam     = "01FIXCAL00000000000000TEA1"
	CalPersonal = "01FIXCAL00000000000000PER1"
	CalSide     = "01FIXCAL00000000000000SID1"
)

const tzid = "Europe/London"

// Data is the fixture world, computed relative to Now.
type Data struct {
	Now       time.Time
	Accounts  []api.Account
	Calendars []api.Calendar
	Settings  api.Settings
	items     []api.AgendaItem // every occurrence in [today-2d, today+14d)
	details   map[string]api.Event
	st        *shared // revision and lock; copies of Data (state variants) share it
}

func ptr[T any](v T) *T { return &v }

// New builds fixtures anchored at now (typically time.Now()).
func New(now time.Time) *Data {
	loc, _ := time.LoadLocation(tzid)
	d := &Data{Now: now.UTC(), details: map[string]api.Event{}, st: &shared{rev: Rev}}
	created := now.UTC().AddDate(0, -3, 0).Truncate(time.Second)
	syncAt := now.UTC().Add(-2 * time.Minute).Truncate(time.Second)
	d.Accounts = []api.Account{
		{ID: AccCobalt, Provider: "google", Email: "alex@northwind.example", DisplayName: "Work", Color: "cobalt", Status: "active", LastSyncAt: &syncAt, LastOKAt: &syncAt, CreatedAt: created},
		{ID: AccRose, Provider: "google", Email: "alex.p@gmail.example", DisplayName: "Personal", Color: "rose", Status: "active", LastSyncAt: &syncAt, LastOKAt: &syncAt, CreatedAt: created},
		{ID: AccOchre, Provider: "google", Email: "alex@studio.example", DisplayName: "Studio", Color: "ochre", Status: "active", LastSyncAt: &syncAt, LastOKAt: &syncAt, CreatedAt: created},
	}
	d.Calendars = []api.Calendar{
		{ID: CalWork, AccountID: AccCobalt, Name: "Alex (Work)", Color: "cobalt", TZID: tzid, Access: "owner", IsPrimary: true, Visible: true},
		{ID: CalTeam, AccountID: AccCobalt, Name: "Team", Color: "cobalt", TZID: tzid, Access: "reader", Visible: true},
		{ID: CalPersonal, AccountID: AccRose, Name: "Alex", Color: "rose", TZID: tzid, Access: "owner", IsPrimary: true, Visible: true},
		{ID: CalSide, AccountID: AccOchre, Name: "Studio", Color: "ochre", TZID: tzid, Access: "owner", IsPrimary: true, Visible: true},
	}
	d.Settings = api.Settings{
		DefaultCalendarID: ptr(CalWork), WeekStart: 1, SecondaryTZID: ptr("America/New_York"),
		WorkingHours: api.WorkingHours{Start: "09:00", End: "17:30", Days: []int{1, 2, 3, 4, 5}},
		UserTZID:     ptr(tzid), AlertDefaults: &api.AlertDefaults{Timed: ptr(10)}, MorningSummary: &agenda.MorningSummary{Time: "07:30"},
	}

	ln := now.In(loc)
	today := time.Date(ln.Year(), ln.Month(), ln.Day(), 0, 0, 0, 0, loc)
	at := func(day, h, m int) time.Time {
		return today.AddDate(0, 0, day).Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	}

	type spec struct {
		key, title, cal, loc string
		day, h, m, dur       int
		join                 *api.Join
		resp                 string // "" = none (own event / no attendees)
		recurring            bool
		allDay               bool
		days                 int // all-day length
		status               string
		busy                 *bool
		readOnly             bool
		guests               int    // extra guests with mixed responses (full detail only)
		desc                 string // description (a parsed join link lives here)
		rrule                string // recurrence rule for detail
		exception            bool   // a single changed occurrence of a series
	}
	meet := func(code string) *api.Join { return &api.Join{URL: "https://meet.google.com/" + code, Kind: "meet"} }
	zoom := &api.Join{URL: "https://zoom.us/j/84012345678", Kind: "zoom"}
	free := false
	var specs []spec
	// Daily recurring standup on weekdays across the window.
	for day := -2; day < 14; day++ {
		if wd := at(day, 0, 0).Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		specs = append(specs, spec{key: "standup", title: "Team standup", cal: CalWork, day: day, h: 9, m: 30, dur: 15, join: meet("abc-defg-hij"), resp: "accepted", recurring: true})
	}
	// Design crit: Tuesdays and Thursdays, with one occurrence changed (an exception with its own id).
	excDone := false
	for day := -2; day < 14; day++ {
		wd := at(day, 0, 0).Weekday()
		if wd != time.Tuesday && wd != time.Thursday {
			continue
		}
		if wd == time.Thursday && day >= 2 && !excDone {
			excDone = true
			specs = append(specs, spec{key: "critx", title: "Design crit: moved, new room", cal: CalWork, loc: "Studio 2", day: day, h: 15, m: 30, dur: 45, resp: "accepted", recurring: true, exception: true, rrule: "FREQ=WEEKLY;BYDAY=TU,TH"})
			continue
		}
		specs = append(specs, spec{key: "crit", title: "Design crit", cal: CalWork, loc: "Studio 1", day: day, h: 14, m: 0, dur: 45, resp: "accepted", recurring: true, rrule: "FREQ=WEEKLY;BYDAY=TU,TH"})
	}
	specs = append(specs,
		spec{key: "interview", title: "Interview: Staff designer", cal: CalWork, day: 0, h: 10, m: 15, dur: 45, join: meet("int-rvwx-yyz"), resp: "accepted"}, // third overlap with design + roadmap
		spec{key: "allhands", title: "All hands: quarterly results, hiring plan and the offsite logistics review", cal: CalWork, day: 3, h: 16, m: 0, dur: 60, join: meet("all-hand-xxx"), resp: "accepted", guests: 7, desc: "Agenda: results, hiring, offsite logistics. Questions in the doc."},
		spec{key: "quick", title: "Quick sync with finance", cal: CalWork, day: 2, h: 12, m: 0, dur: 15, resp: "accepted"},
		spec{key: "custcall", title: "Customer call: Northwind renewal", cal: CalWork, day: 4, h: 11, m: 0, dur: 30, resp: "accepted", desc: "Join Zoom Meeting\nhttps://zoom.us/j/99887766554?pwd=abc\nMeeting ID: 998 8776 6554", join: &api.Join{URL: "https://zoom.us/j/99887766554?pwd=abc", Kind: "zoom"}},
		spec{key: "design", title: "Design review", cal: CalWork, loc: "Room 4", day: 0, h: 10, m: 0, dur: 45, resp: "accepted"},
		spec{key: "sync1", title: "Roadmap sync", cal: CalWork, day: 0, h: 10, m: 30, dur: 60, join: zoom, resp: "tentative"}, // overlaps design review
		spec{key: "lunch", title: "Lunch with Sam", cal: CalPersonal, loc: "Kōya, 49 Frith St", day: 0, h: 13, m: 0, dur: 60},
		spec{key: "dentist", title: "Dentist", cal: CalPersonal, loc: "Harley Street Dental", day: 0, h: 16, m: 30, dur: 45},
		spec{key: "offsite", title: "Company offsite", cal: CalTeam, day: 1, allDay: true, days: 2, readOnly: true, busy: &free},
		spec{key: "oneonone", title: "1:1 with Priya", cal: CalWork, day: 1, h: 11, m: 0, dur: 30, join: meet("pri-yaxx-one"), resp: "accepted", recurring: true},
		spec{key: "vendor", title: "Vendor call", cal: CalWork, day: 1, h: 15, m: 0, dur: 30, join: zoom, resp: "declined"},
		spec{key: "portfolio", title: "Portfolio work block", cal: CalSide, day: 2, h: 14, m: 0, dur: 180},
		spec{key: "client", title: "Client kickoff", cal: CalSide, loc: "Shoreditch Studio", day: 2, h: 15, m: 0, dur: 60, resp: "accepted"}, // overlaps work block
		spec{key: "yoga", title: "Yoga", cal: CalPersonal, loc: "Triyoga Soho", day: 3, h: 18, m: 30, dur: 60, recurring: true},
		spec{key: "bday", title: "Mum's birthday", cal: CalPersonal, day: 4, allDay: true, days: 1, busy: &free, recurring: true},
		spec{key: "planning", title: "Quarterly planning", cal: CalWork, loc: "Boardroom", day: 5, h: 9, m: 0, dur: 240, resp: "needsAction"},
		spec{key: "dinner", title: "Dinner at Padella", cal: CalPersonal, loc: "Padella, Borough Market", day: 5, h: 19, m: 30, dur: 120},
		spec{key: "trip", title: "Lisbon trip", cal: CalPersonal, day: 8, allDay: true, days: 4},
		spec{key: "late", title: "Late call with NYC", cal: CalWork, day: 7, h: 21, m: 0, dur: 45, join: meet("nyc-late-call"), resp: "accepted"},
		spec{key: "past", title: "Retro", cal: CalWork, day: -1, h: 15, m: 0, dur: 60, join: meet("ret-roxx-xxx"), resp: "accepted"},
		spec{key: "cancelled", title: "Cancelled: Brown bag", cal: CalTeam, day: 3, h: 12, m: 0, dur: 60, status: "cancelled", readOnly: true, resp: "accepted"},
	)

	calAcc := map[string]string{}
	calColor := map[string]string{}
	for _, c := range d.Calendars {
		calAcc[c.ID], calColor[c.ID] = c.AccountID, c.Color
	}
	for _, s := range specs {
		evID := "01FIXEVT" + strings.ToUpper(s.key)
		evID = (evID + "0000000000000000000000000")[:26]
		st := s.status
		if st == "" {
			st = "confirmed"
		}
		busy := true
		if s.busy != nil {
			busy = *s.busy
		}
		it := api.AgendaItem{
			EventID: evID, Version: 100 + int64(len(d.items)), CalendarID: s.cal, AccountID: calAcc[s.cal], Color: calColor[s.cal],
			Title: s.title, AllDay: s.allDay, Location: s.loc, Join: s.join, Status: st, Busy: busy, Recurring: s.recurring,
			CanEdit: !s.readOnly, CanRSVP: s.resp != "" && st != "cancelled", CanAddConference: !s.readOnly && s.join == nil && !s.allDay,
		}
		if s.resp != "" {
			it.MyResponse = ptr(s.resp)
			it.Organizer = &api.Person{Email: "priya@northwind.example", Name: "Priya Nair"}
		} else {
			it.Organizer = &api.Person{Email: d.Accounts[0].Email, Name: "Alex", IsSelf: true}
		}
		var rkey int64
		if s.allDay {
			start := today.AddDate(0, 0, s.day)
			it.Date = start.Format(time.DateOnly)
			it.EndDate = start.AddDate(0, 0, s.days).Format(time.DateOnly)
			rkey = start.Unix()
		} else {
			st0 := at(s.day, s.h, s.m).UTC()
			it.Start, it.End, it.TZID = ptr(st0), ptr(st0.Add(time.Duration(s.dur)*time.Minute)), tzid
			rkey = st0.Unix()
		}
		it.ID = fmt.Sprintf("%s:%d", evID, rkey)
		d.items = append(d.items, it)

		ev := api.Event{AgendaItem: it, UpdatedAt: ptr(syncAt)}
		if s.resp != "" {
			ev.Attendees = []api.Attendee{
				{Email: "priya@northwind.example", Name: "Priya Nair", Response: "accepted", IsOrganizer: true},
				{Email: d.Accounts[0].Email, Name: "Alex", Response: s.resp, IsSelf: true},
				{Email: "sam@northwind.example", Name: "Sam Okafor", Response: "needsAction", Optional: true},
			}
		}
		if s.recurring {
			rule, sum := "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", "Every weekday"
			if s.rrule == "FREQ=WEEKLY;BYDAY=TU,TH" {
				rule, sum = s.rrule, "Weekly on Tue, Thu"
			}
			ev.Recurrence = &api.Recurrence{RRule: rule, Summary: sum}
		}
		if s.desc != "" {
			ev.Description = strings.ReplaceAll(s.desc, "\\n", "\n")
		}
		if s.guests > 0 {
			resps := []string{"accepted", "declined", "tentative", "needsAction", "accepted", "needsAction", "accepted"}
			names := []string{"Jo Lindqvist", "Maya Chen", "Tom Reyes", "Aisha Bello", "Luca Romano", "Hana Sato", "Ed Walsh"}
			for i := 0; i < s.guests; i++ {
				first := strings.ToLower(strings.Fields(names[i%len(names)])[0])
				ev.Attendees = append(ev.Attendees, api.Attendee{Email: first + "@northwind.example", Name: names[i%len(names)], Response: resps[i%len(resps)], Optional: i == 5})
			}
		}
		if s.status == "cancelled" {
			ev.Description = "Cancelled by the organizer."
		}
		if s.key == "design" {
			ev.Description = "Walk through the new month view and the agenda density options."
			ev.ReminderMin = ptr(10)
		}
		d.details[it.ID] = ev
	}
	sort.SliceStable(d.items, func(i, j int) bool { return sortKey(d.items[i]).Before(sortKey(d.items[j])) })
	return d
}

func sortKey(it api.AgendaItem) time.Time {
	if it.AllDay {
		t, _ := time.Parse(time.DateOnly, it.Date)
		return t
	}
	return *it.Start
}

// Agenda returns items overlapping [from,to), optionally limited to calendars.
func (d *Data) Agenda(from, to time.Time, cals map[string]bool) []api.AgendaItem {
	d.st.mu.RLock()
	defer d.st.mu.RUnlock()
	out := []api.AgendaItem{}
	for _, it := range d.items {
		if len(cals) > 0 && !cals[it.CalendarID] {
			continue
		}
		var s, e time.Time
		if it.AllDay {
			s, _ = time.Parse(time.DateOnly, it.Date)
			e, _ = time.Parse(time.DateOnly, it.EndDate)
		} else {
			s, e = *it.Start, *it.End
		}
		if s.Before(to) && e.After(from) {
			out = append(out, it)
		}
	}
	return out
}

// Event looks up by occurrence id ("eventId:rkey") or bare eventId (first occurrence).
func (d *Data) Event(id string) (api.Event, bool) {
	d.st.mu.RLock()
	defer d.st.mu.RUnlock()
	if ev, ok := d.details[id]; ok {
		return ev, true
	}
	for _, it := range d.items {
		if it.EventID == id {
			return d.details[it.ID], true
		}
	}
	return api.Event{}, false
}

// ---- HTTP ----

// States are the --fixtures-state values; they let UI work verify every screen state against a running hub.
var States = []string{"normal", "loading", "empty", "noaccounts", "banner", "offline", "down", "syncing"}

type Server struct {
	Data    *Data
	Name    string
	Version string
	// State selects a canned condition (see States); "" = normal.
	//   loading    every /v1 response except hello is delayed by Delay (default 3s)
	//   empty      accounts and calendars exist, the agenda has no events
	//   noaccounts no accounts, calendars or events
	//   banner     the first account needs sign-in; its rows are still served (stale)
	//   syncing    the last account is syncing (the first accounts are done: "2 of 3")
	//   offline    hello works, then every connection is dropped without a reply
	//   down       503 unavailable on everything except hello and /health
	State string
	Delay time.Duration
	warn  bool // set by the banner state: status carries a warning
	// PingEvery is the SSE ping interval (default 20s).
	PingEvery time.Duration
	// WriteOutcome forces the result of write endpoints: ok (default) | fail | conflict | slow | ratelimited.
	// A request can override it with the X-Fixture-Outcome header.
	WriteOutcome string
	// WriteLatency delays every write (default 600 ms; negative = none); SlowDelay is the "slow" outcome (default 5 s).
	WriteLatency, SlowDelay time.Duration
	ws                      *writeState
}

// ValidState reports whether name is a known --fixtures-state.
func ValidState(name string) bool {
	for _, x := range States {
		if x == name {
			return true
		}
	}
	return name == ""
}

// stateData returns the data a state serves (never mutates s.Data).
func (s *Server) stateData() *Data {
	d := *s.Data
	switch s.State {
	case "empty":
		d.items, d.details = nil, map[string]api.Event{}
	case "noaccounts":
		d.items, d.details = nil, map[string]api.Event{}
		d.Accounts, d.Calendars = []api.Account{}, []api.Calendar{}
		d.Settings.DefaultCalendarID = nil
	case "banner":
		d.Accounts = append([]api.Account(nil), d.Accounts...)
		d.Accounts[0].Status = "needs_reauth"
		d.Accounts[0].StatusDetail = "Google no longer accepts the saved sign-in."
		stale := d.Now.Add(-26 * time.Hour).Truncate(time.Second)
		d.Accounts[0].LastSyncAt, d.Accounts[0].LastOKAt = &stale, &stale
	case "syncing":
		d.Accounts = append([]api.Account(nil), d.Accounts...)
		d.Accounts[len(d.Accounts)-1].Status = "syncing"
	}
	return &d
}

func (s *Server) Handler() http.Handler {
	if s.State != "" && s.State != "normal" {
		s2 := *s
		s2.Data = s.stateData()
		s2.warn = s.State == "banner"
		s2.State = ""
		return s.withState(s2.Handler())
	}
	return s.baseHandler()
}

// withState applies the transport-level behaviour of a state around the normal handler.
func (s *Server) withState(next http.Handler) http.Handler {
	delay := s.Delay
	if delay <= 0 {
		delay = 3 * time.Second
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isHello := r.URL.Path == "/v1/hello" || r.URL.Path == "/health"
		switch s.State {
		case "loading":
			if !isHello {
				select {
				case <-time.After(delay):
				case <-r.Context().Done():
					return
				}
			}
		case "down":
			if !isHello {
				api.WriteError(w, 503, api.CodeUnavailable, "the hub is not available", "")
				return
			}
		case "offline":
			if !isHello {
				if hj, ok := w.(http.Hijacker); ok {
					if c, _, err := hj.Hijack(); err == nil {
						c.Close() // the client sees a reset connection, as if the hub vanished
						return
					}
				}
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) baseHandler() http.Handler {
	if s.ws == nil {
		s.ws = &writeState{}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		api.WriteJSON(w, 200, map[string]any{"status": "ok"})
	})
	auth := func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer cal_") {
				w.Header().Set("WWW-Authenticate", "Bearer")
				api.WriteError(w, 401, api.CodeUnauthorized, "missing or malformed bearer token", "")
				return
			}
			h(w, r)
		})
	}
	mux.Handle("GET /v1/hello", auth(s.hello))
	mux.Handle("GET /v1/status", auth(s.status))
	mux.Handle("GET /v1/agenda", auth(s.agenda))
	mux.Handle("GET /v1/accounts", auth(func(w http.ResponseWriter, r *http.Request) {
		api.WriteJSON(w, 200, api.AccountList{Items: s.Data.Accounts})
	}))
	mux.Handle("GET /v1/calendars", auth(func(w http.ResponseWriter, r *http.Request) {
		api.WriteJSON(w, 200, api.CalendarList{Items: s.Data.Calendars})
	}))
	mux.Handle("GET /v1/settings", auth(func(w http.ResponseWriter, r *http.Request) {
		api.WriteJSON(w, 200, s.Data.Settings)
	}))
	mux.Handle("GET /v1/events/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		ev, ok := s.Data.Event(r.PathValue("id"))
		if !ok {
			api.WriteError(w, 404, api.CodeNotFound, "no such event", "")
			return
		}
		api.WriteJSON(w, 200, ev)
	}))
	mux.Handle("POST /v1/accounts/ics", auth(s.icsAccount))
	s.extraRoutes(mux, auth)
	s.writeRoutes(mux, auth)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		api.WriteError(w, 404, api.CodeNotFound, "not available in fixtures mode", "")
	})
	return mux
}

func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, 200, api.Hello{Server: s.Name, Version: s.Version, APIVersion: api.APIVersion, Scope: "admin", TokenID: "01FIXTOKEN0000000000000001"})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	st := api.Status{HubVersion: s.Version, DBSize: 1 << 20, Rev: s.Data.Rev(), Accounts: []api.AccountSyncStatus{}, Warnings: []api.Warning{}}
	for _, a := range s.Data.Accounts {
		as := api.AccountSyncStatus{AccountID: a.ID, Email: a.Email, Status: a.Status, LastSyncAt: a.LastSyncAt, LastOKAt: a.LastOKAt}
		if a.Status != "active" && a.Status != "syncing" && a.StatusDetail != "" {
			d := a.StatusDetail
			as.LastError, as.FailCount = &d, 3
		}
		if a.Status == "active" {
			next := s.Data.Now.Add(3 * time.Minute).Truncate(time.Second)
			as.NextSyncAt = &next
		}
		st.Accounts = append(st.Accounts, as)
	}
	if s.warn {
		st.Warnings = append(st.Warnings, api.Warning{Kind: "backup", Message: "No backup has been made yet."})
	}
	api.WriteJSON(w, 200, st)
}

func (s *Server) agenda(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err1 := time.Parse(time.RFC3339, q.Get("from"))
	to, err2 := time.Parse(time.RFC3339, q.Get("to"))
	switch {
	case err1 != nil:
		api.WriteError(w, 400, api.CodeValidation, "from must be an RFC 3339 instant", "from")
		return
	case err2 != nil || !to.After(from):
		api.WriteError(w, 400, api.CodeValidation, "to must be an RFC 3339 instant after from", "to")
		return
	}
	var cals map[string]bool
	if c := q.Get("calendars"); c != "" {
		cals = map[string]bool{}
		for _, id := range strings.Split(c, ",") {
			cals[id] = true
		}
	}
	h := sha256.Sum256([]byte(from.UTC().Format(time.RFC3339) + "|" + to.UTC().Format(time.RFC3339) + "|" + q.Get("calendars")))
	etag := fmt.Sprintf(`"%d-%s"`, s.Data.Rev(), hex.EncodeToString(h[:4]))
	w.Header().Set("ETag", etag)
	if matches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	api.WriteJSON(w, 200, api.AgendaResponse{Rev: s.Data.Rev(), From: from.UTC(), To: to.UTC(), Items: s.Data.Agenda(from, to, cals)})
}

// matches implements If-None-Match weak comparison over a comma list or "*".
func matches(header, etag string) bool {
	for _, p := range strings.Split(header, ",") {
		p = strings.TrimPrefix(strings.TrimSpace(p), "W/")
		if p == "*" || p == etag {
			return true
		}
	}
	return false
}
