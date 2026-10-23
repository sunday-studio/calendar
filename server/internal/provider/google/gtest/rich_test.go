package gtest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
	"calendar/server/internal/provider/google"
)

var richNow = time.Date(2026, 10, 21, 14, 7, 30, 0, time.UTC)

type synced struct {
	cal    provider.RemoteCalendar
	events []domain.Event
}

func richMulti(now time.Time, tz string) *Multi {
	m := NewMultiProfile(ProfileRich)
	m.Now = func() time.Time { return now }
	m.TZ = tz
	m.Reset()
	return m
}

// syncAll runs the real Google provider (discovery, paging, the mapper) against the fake.
func syncAll(t *testing.T, m *Multi) map[string][]synced {
	t.Helper()
	srv := httptest.NewServer(m.Handler())
	t.Cleanup(srv.Close)
	p := google.New(google.Config{BaseURL: srv.URL, TokenURL: srv.URL + "/token",
		Refresh: func(_ context.Context, id string) (string, error) { return RefreshToken(id), nil }})
	out := map[string][]synced{}
	for _, email := range m.Emails {
		acct := domain.Account{ID: email, Email: email}
		cals, err := p.Discover(context.Background(), acct)
		if err != nil {
			t.Fatalf("%s discover: %v", email, err)
		}
		for _, rc := range cals {
			c := domain.Calendar{ID: rc.RemoteID, AccountID: email, RemoteID: rc.RemoteID, Name: rc.Name, TZID: rc.TZID, Access: rc.Access, IsPrimary: rc.IsPrimary, Visible: rc.Visible}
			s := synced{cal: rc}
			tok := ""
			for {
				b, err := p.Sync(context.Background(), acct, c, provider.SyncRequest{PageToken: tok})
				if err != nil {
					t.Fatalf("%s/%s sync: %v", email, rc.Name, err)
				}
				s.events = append(s.events, b.Events...)
				if b.PageToken == "" {
					break
				}
				tok = b.PageToken
			}
			out[email] = append(out[email], s)
		}
	}
	return out
}

func TestRichProfileMapsEverything(t *testing.T) {
	m := richMulti(richNow, "")
	all := syncAll(t, m)

	wantCals := map[string]int{"work@example.com": 7, "personal@example.com": 4, "side@example.com": 3, "stale@example.com": 2}
	for e, n := range wantCals {
		if len(all[e]) != n {
			t.Errorf("%s: %d calendars, want %d", e, len(all[e]), n)
		}
	}
	var events []domain.Event
	access := map[string]int{}
	hidden, longName := 0, 0
	for _, cs := range all {
		for _, s := range cs {
			access[s.cal.Access]++
			if !s.cal.Visible {
				hidden++
			}
			if utf8.RuneCountInString(s.cal.Name) > 80 {
				longName++
			}
			events = append(events, s.events...)
		}
	}
	for _, a := range []string{"owner", "writer", "reader", "freebusy"} {
		if access[a] == 0 {
			t.Errorf("no calendar with access %s", a)
		}
	}
	if hidden != 2 || longName != 1 {
		t.Errorf("hidden/unselected calendars %d (want 2), long names %d (want 1)", hidden, longName)
	}
	if len(events) < 160 {
		t.Errorf("only %d events", len(events))
	}

	c := map[string]int{}
	conf := map[string]int{}
	maxTitle, maxGuests := 0, 0
	for _, e := range events {
		inc := func(k string, cond bool) {
			if cond {
				c[k]++
			}
		}
		inc("allday", e.When.AllDay)
		inc("master", e.Kind == domain.Master)
		inc("exception", e.Kind == domain.Exception)
		inc("cancelled", e.Status == domain.Cancelled)
		inc("tentativeStatus", e.Status == domain.Tentative)
		inc("free", !e.Busy)
		inc("emptyTitle", e.Title == "")
		inc("nonEditable", !e.Caps.CanEdit)
		inc("rsvpable", e.Caps.CanRSVP)
		inc("organizerMe", e.IsOrganizer)
		inc("organizerOther", e.Organizer != nil && !e.IsOrganizer)
		inc("noGuests", len(e.Attendees) == 0)
		inc("oneGuest", len(e.Attendees) == 2)
		inc("rsvpOnly", e.SelfResponse != nil && !e.Caps.CanRSVP && !e.IsOrganizer)
		if e.SelfResponse != nil && e.Status != domain.Cancelled {
			inc("reply:"+string(*e.SelfResponse), true)
		}
		for _, a := range e.Attendees {
			inc("role:"+a.Role, true)
		}
		if e.Conference != nil {
			conf[e.Conference.Kind+"/"+e.Conference.Source]++
		} else {
			inc("noConference", e.Status != domain.Cancelled && !e.When.AllDay)
		}
		if n := utf8.RuneCountInString(e.Title); n > maxTitle {
			maxTitle = n
		}
		if len(e.Attendees) > maxGuests {
			maxGuests = len(e.Attendees)
		}
		inc("longDescription", len(e.Description) > 1500)
		inc("location", e.Location != "")
		inc("locationURL", strings.HasPrefix(e.Location, "https://") && !strings.Contains(e.Location, " "))
	}
	min := map[string]int{"allday": 25, "master": 20, "exception": 8, "cancelled": 6, "tentativeStatus": 2, "free": 12, "emptyTitle": 1,
		"nonEditable": 15, "rsvpable": 8, "organizerMe": 30, "organizerOther": 12, "noGuests": 30, "oneGuest": 2, "rsvpOnly": 1,
		"reply:needsAction": 3, "reply:accepted": 8, "reply:tentative": 2, "reply:declined": 3,
		"role:optional": 2, "role:resource": 1, "role:required": 20, "noConference": 10, "longDescription": 1, "location": 10, "locationURL": 2}
	for k, n := range min {
		if c[k] < n {
			t.Errorf("category %s: %d, want >= %d", k, c[k], n)
		}
	}
	for _, k := range []string{"meet/structured", "zoom/parsed", "teams/parsed", "webex/parsed"} {
		if conf[k] == 0 {
			t.Errorf("no %s conference (have %v)", k, conf)
		}
	}
	if maxTitle != 120 || maxGuests < 12 {
		t.Errorf("longest title %d (want 120), most attendees %d (want >= 12)", maxTitle, maxGuests)
	}
	t.Logf("accounts %d, calendars %d, events %d; categories %v; conferences %v", len(all), 16+2, len(events), c, conf)
}

func TestRichIsRelativeToNow(t *testing.T) {
	for _, now := range []time.Time{richNow, richNow.AddDate(0, 0, 3).Add(7 * time.Hour), richNow.AddDate(0, 4, 0)} {
		m := richMulti(now, "")
		f := m.fake("work@example.com")
		start := func(id string) time.Time {
			e := f.Get("work@example.com", id)
			if e == nil {
				t.Fatalf("%s missing", id)
			}
			x, _ := time.Parse(time.RFC3339, e["start"].(map[string]any)["dateTime"].(string))
			return x
		}
		mnow := now.Truncate(time.Minute)
		for id, d := range map[string]time.Duration{"w-live": -20 * time.Minute, "w-soon8": 8 * time.Minute, "w-soon25": 25 * time.Minute} {
			if got := start(id).Sub(mnow); got != d {
				t.Errorf("%s starts %v from now, want %v (now %v)", id, got, d, now)
			}
		}
		loc := loadLoc("Europe/London")
		tomorrow := now.In(loc).AddDate(0, 0, 1).Format(time.DateOnly)
		n := 0
		for _, e := range f.Dump("work@example.com") {
			if s, ok := e["start"].(map[string]any)["dateTime"].(string); ok && e["recurrence"] == nil {
				if x, _ := time.Parse(time.RFC3339, s); x.In(loc).Format(time.DateOnly) == tomorrow {
					n++
				}
			}
		}
		if n < 12 {
			t.Errorf("now %v: %d single events tomorrow, want a busy day (>= 12)", now, n)
		}
		// pinned DST events and the 300 days past / 200 days ahead ones
		for _, id := range []string{"w-dst-uk-1", "w-dst-uk-2", "w-dst-uk-span", "w-dst-uk-spring", "w-dst-us-1", "w-dst-us-2", "w-dst-us-spring", "w-dst-next", "w-p300", "w-f200"} {
			if f.Get("work@example.com", id) == nil {
				t.Errorf("%s missing", id)
			}
		}
		if d := mnow.Sub(start("w-p300")); d < 295*24*time.Hour || d > 305*24*time.Hour {
			t.Errorf("w-p300 is %v ago", d)
		}
		if d := start("w-f200").Sub(mnow); d < 195*24*time.Hour || d > 205*24*time.Hour {
			t.Errorf("w-f200 is %v ahead", d)
		}
	}
}

func TestRichDSTEventsAreRepeatedHour(t *testing.T) {
	f := richMulti(richNow, "").fake("work@example.com")
	a, b := f.Get("work@example.com", "w-dst-uk-1"), f.Get("work@example.com", "w-dst-uk-2")
	sa := a["start"].(map[string]any)["dateTime"].(string)
	sb := b["start"].(map[string]any)["dateTime"].(string)
	if sa != "2026-10-25T01:30:00+01:00" || sb != "2026-10-25T01:30:00Z" {
		t.Errorf("repeated hour: %s and %s", sa, sb)
	}
}

// The day with nothing on it and the day with only all-day events hold for every start date and zone.
func TestRichEmptyAndAllDayOnlyDays(t *testing.T) {
	for _, tz := range []string{"Europe/London", "America/Los_Angeles", "Pacific/Kiritimati"} {
		loc := loadLoc(tz)
		for i := 0; i < 70; i++ {
			now := richNow.AddDate(0, 0, i*5).Add(time.Duration(i) * 37 * time.Minute)
			empty, quiet := RichLayout(tz, now)
			if quiet != empty+1 || empty < 5 || empty > 25 {
				t.Fatalf("%s %v: layout %d %d", tz, now, empty, quiet)
			}
			ln := now.In(loc)
			mid := time.Date(ln.Year(), ln.Month(), ln.Day(), 0, 0, 0, 0, loc)
			m := NewMultiProfile(ProfileRich)
			m.Now, m.TZ = func() time.Time { return now }, tz
			m.Reset()
			for _, email := range m.Emails {
				f := m.fake(email)
				for _, cal := range f.CalIDs() {
					for off, wantTimed := range map[int]bool{empty: false, quiet: false} {
						d0 := time.Date(mid.Year(), mid.Month(), mid.Day()+off, 0, 0, 0, 0, loc)
						timed, allday := onDay(f, cal, d0, d0.AddDate(0, 0, 1))
						if timed > 0 || (off == empty && allday > 0) {
							t.Errorf("%s %v %s/%s day +%d: %d timed, %d all-day (timed allowed: %v)", tz, now.Format(time.DateOnly), email, cal, off, timed, allday, wantTimed)
						}
					}
				}
			}
		}
	}
}

// onDay counts timed and all-day events (series expanded, exceptions applied, cancelled skipped) on [from,to).
func onDay(f *Fake, cal string, from, to time.Time) (timed, allday int) {
	dump := f.Dump(cal)
	byID := map[string]map[string]any{}
	for _, e := range dump {
		byID[e["id"].(string)] = e
	}
	check := func(e map[string]any) {
		if e["status"] == "cancelled" {
			return
		}
		st, en := e["start"].(map[string]any), e["end"].(map[string]any)
		if d, ok := st["date"].(string); ok {
			ed := en["date"].(string)
			if d < to.Format(time.DateOnly) && ed > from.Format(time.DateOnly) {
				allday++
			}
			return
		}
		s, _ := time.Parse(time.RFC3339, st["dateTime"].(string))
		x, _ := time.Parse(time.RFC3339, en["dateTime"].(string))
		if s.Before(to) && x.After(from) {
			timed++
		}
	}
	for _, e := range dump {
		if e["recurrence"] != nil {
			for _, in := range expand(e, from.AddDate(0, 0, -1), to.AddDate(0, 0, 1)) {
				if ov := byID[in.id]; ov != nil {
					continue // an exception row decides
				}
				check(in.raw)
			}
			continue
		}
		check(e)
	}
	return
}

func admin(t *testing.T, srv *httptest.Server, path string, body any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	r, err := http.Post(srv.URL+"/_admin/"+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{}
	}
	out["_status"] = float64(r.StatusCode)
	return out
}

func TestRichScenarios(t *testing.T) {
	m := richMulti(time.Now(), "")
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	tok := func(email string) string { return "at-" + RefreshToken(email) }
	status := func(path, email string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+tok(email))
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode, r.Header.Get("Retry-After")
	}
	refresh := func(email string) int {
		r, err := http.PostForm(srv.URL+"/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {RefreshToken(email)}})
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	expect := func(what string, got, want int) {
		t.Helper()
		if got != want {
			t.Errorf("%s: %d, want %d", what, got, want)
		}
	}
	list := "/users/me/calendarList"

	admin(t, srv, "down", map[string]any{"seconds": 30})
	c, _ := status(list, "work@example.com")
	expect("down api", c, 503)
	expect("down token", refresh("work@example.com"), 503)
	admin(t, srv, "recover", map[string]any{})
	c, _ = status(list, "work@example.com")
	expect("after recover", c, 200)

	admin(t, srv, "down", map[string]any{"n": 2, "code": 500, "account": "side@example.com"})
	c, _ = status(list, "work@example.com")
	expect("down side only: work", c, 200)
	c, _ = status(list, "side@example.com")
	expect("down side 1", c, 500)
	c, _ = status(list, "side@example.com")
	expect("down side 2", c, 500)
	c, _ = status(list, "side@example.com")
	expect("down side 3 (n used up)", c, 200)

	admin(t, srv, "rate-limit", map[string]any{"seconds": 30, "retryAfter": "17"})
	c, ra := status(list, "personal@example.com")
	expect("rate limit", c, 429)
	if ra != "17" {
		t.Errorf("Retry-After %q", ra)
	}
	admin(t, srv, "recover", map[string]any{})

	admin(t, srv, "slow", map[string]any{"ms": 150})
	t0 := time.Now()
	status(list, "work@example.com")
	if time.Since(t0) < 140*time.Millisecond {
		t.Errorf("slow: answered in %v", time.Since(t0))
	}
	admin(t, srv, "recover", map[string]any{})
	t0 = time.Now()
	status(list, "work@example.com")
	if time.Since(t0) > 100*time.Millisecond {
		t.Errorf("recover left a delay: %v", time.Since(t0))
	}

	admin(t, srv, "reauth", map[string]any{})
	expect("stale refresh revoked", refresh("stale@example.com"), 400)
	expect("work still fine", refresh("work@example.com"), 200)
	c, _ = status(list, "stale@example.com")
	expect("stale api", c, 401)
	admin(t, srv, "recover", map[string]any{})
	expect("stale after recover", refresh("stale@example.com"), 200)

	// outside changes on the work primary
	f := m.fake("work@example.com")
	pri := "work@example.com"
	before := len(f.Dump(pri))
	r := admin(t, srv, "outside", map[string]any{"action": "move"})
	id, _ := r["id"].(string)
	if r["_status"] != 200.0 || id == "" || r["did"] == "" {
		t.Fatalf("outside move: %v", r)
	}
	r = admin(t, srv, "outside", map[string]any{"action": "rename"})
	if r["_status"] != 200.0 || !strings.Contains(f.Get(pri, r["id"].(string))["summary"].(string), "(renamed in Google)") {
		t.Errorf("rename: %v", r)
	}
	r = admin(t, srv, "outside", map[string]any{"action": "delete"})
	if r["_status"] != 200.0 || f.Get(pri, r["id"].(string)) != nil {
		t.Errorf("delete: %v", r)
	}
	r = admin(t, srv, "outside", map[string]any{"action": "add"})
	if r["_status"] != 200.0 || f.Get(pri, r["id"].(string)) == nil {
		t.Errorf("add: %v", r)
	}
	if r := admin(t, srv, "outside", map[string]any{"action": "bogus"}); r["_status"] != 400.0 {
		t.Errorf("bogus action: %v", r)
	}
	before2 := len(f.Dump(pri))
	if before2 != before { // one deleted, one added
		t.Errorf("events %d -> %d", before, before2)
	}
	admin(t, srv, "burst", map[string]any{})
	if n := len(f.Dump(pri)); n != before2+200 {
		t.Errorf("burst: %d -> %d", before2, n)
	}
	calsBefore := len(f.CalIDs())
	admin(t, srv, "calendar-add", map[string]any{})
	expect("calendar-add", len(f.CalIDs()), calsBefore+1)
	admin(t, srv, "calendar-remove", map[string]any{})
	expect("calendar-remove (pop-up first)", len(f.CalIDs()), calsBefore)
	admin(t, srv, "calendar-remove", map[string]any{})
	expect("calendar-remove (then a read-only one)", len(f.CalIDs()), calsBefore-1)

	admin(t, srv, "expire-sync-tokens", map[string]any{})
	rr, _ := http.NewRequest("GET", srv.URL+"/calendars/"+url.PathEscape(pri)+"/events?singleEvents=false&syncToken=st-1&showDeleted=true", nil)
	rr.Header.Set("Authorization", "Bearer "+tok(pri))
	resp, _ := http.DefaultClient.Do(rr)
	resp.Body.Close()
	expect("expired sync token", resp.StatusCode, 410)

	// summary, then reset puts everything back
	resp, err := http.Get(srv.URL + "/_admin/summary")
	if err != nil {
		t.Fatal(err)
	}
	var sum map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&sum)
	resp.Body.Close()
	if sum["profile"] != "rich" || len(sum["accounts"].(map[string]any)) != 4 {
		t.Errorf("summary: %v", sum)
	}
	// a sync token from before the reset answers 410 afterwards (the hub then re-lists)
	oldTok, page := "", ""
	for oldTok == "" {
		lr, _ := http.NewRequest("GET", srv.URL+"/calendars/"+url.PathEscape(pri)+"/events?singleEvents=false&pageToken="+page, nil)
		lr.Header.Set("Authorization", "Bearer "+tok(pri))
		lresp, _ := http.DefaultClient.Do(lr)
		var body map[string]any
		_ = json.NewDecoder(lresp.Body).Decode(&body)
		lresp.Body.Close()
		oldTok, _ = body["nextSyncToken"].(string)
		page, _ = body["nextPageToken"].(string)
	}
	admin(t, srv, "reset", map[string]any{})
	tr, _ := http.NewRequest("GET", srv.URL+"/calendars/"+url.PathEscape(pri)+"/events?singleEvents=false&showDeleted=true&syncToken="+oldTok, nil)
	tr.Header.Set("Authorization", "Bearer "+tok(pri))
	tresp, _ := http.DefaultClient.Do(tr)
	tresp.Body.Close()
	expect("sync token from before reset", tresp.StatusCode, 410)
	g2 := m.fake(pri)
	expect("reset events", len(g2.Dump(pri)), before)
	expect("reset calendars", len(g2.CalIDs()), calsBefore)
	c, _ = status(list, "work@example.com")
	expect("reset api", c, 200)
}

// The e2e profile must stay exactly as scripts/e2e.sh expects.
func TestE2EProfileUnchanged(t *testing.T) {
	m := NewMulti("a@example.com", "b@example.com")
	if m.Profile != ProfileE2E {
		t.Fatalf("default profile %q", m.Profile)
	}
	if n := len(m.fake("a@example.com").CalIDs()); n != 3 {
		t.Errorf("a: %d calendars", n)
	}
	if n := len(m.fake("b@example.com").CalIDs()); n != 2 {
		t.Errorf("b: %d calendars", n)
	}
}
