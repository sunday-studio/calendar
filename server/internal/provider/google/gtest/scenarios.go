package gtest

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type postFn func(path string, h func(a adminReq, fs []*Fake) (any, int))

// scenarioRoutes are the sync-state hooks of the demo (scripts/run-local.sh scenario ...):
// outages, rate limits, slow answers, outside changes, bursts, calendar list changes.
// Each answers {"ok":true,"did":"...","look":"..."} so the script can tell the user what to expect.
func (m *Multi) scenarioRoutes(mux *http.ServeMux, post postFn) {
	say := func(did, look string, extra map[string]any) map[string]any {
		out := map[string]any{"ok": true, "did": did, "look": look}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	scope := func(a adminReq) string {
		if a.Account == "" {
			return "all accounts"
		}
		return a.Account
	}
	// down: 5xx for every call (API and token) for Seconds, or for the next N requests.
	post("down", func(a adminReq, fs []*Fake) (any, int) {
		code := a.Code
		if code == 0 {
			code = 503
		}
		g := &gate{account: a.Account, code: code, n: a.N}
		what := fmt.Sprintf("the next %d requests", a.N)
		if a.N == 0 {
			if a.Seconds == 0 {
				a.Seconds = 60
			}
			g.until = time.Now().Add(time.Duration(a.Seconds) * time.Second)
			what = fmt.Sprintf("%d s", a.Seconds)
		}
		m.addGate(g)
		return say(fmt.Sprintf("Google answers %d for %s (%s)", code, scope(a), what),
			"Settings > Server and sync > Sync log shows failing runs (HTTP 503) with a trace id; the hub backs off about a minute between retries and the account stays active with its saved events; edits and replies made now fail. Pull to refresh retries at once; after the outage the next sync succeeds.", nil), 200
	})
	post("rate-limit", func(a adminReq, fs []*Fake) (any, int) {
		if a.Seconds == 0 {
			a.Seconds = 60
		}
		ra := a.RetryAfter
		if ra == "" {
			ra = "20"
		}
		m.addGate(&gate{account: a.Account, code: 429, retryAfter: ra, until: time.Now().Add(time.Duration(a.Seconds) * time.Second)})
		return say(fmt.Sprintf("Google answers 429 with Retry-After %s for %s for %d s", ra, scope(a), a.Seconds),
			"The hub backs off quietly (next run waits at least Retry-After; no account error, no lost data); edits made now are refused by Google and fail. Syncing resumes by itself after the window.", nil), 200
	})
	post("slow", func(a adminReq, fs []*Fake) (any, int) {
		m.gmu.Lock()
		m.delay = time.Duration(a.Ms) * time.Millisecond
		m.gmu.Unlock()
		return say(fmt.Sprintf("every Google answer now takes %d ms", a.Ms),
			"Pull-to-refresh and sync runs take longer (spinner / syncing state); long delays make syncs time out. `recover` removes the delay.", nil), 200
	})
	// recover: clear outages, delay, per-path failures and revocations (not the data).
	post("recover", func(a adminReq, fs []*Fake) (any, int) {
		m.gmu.Lock()
		m.gates, m.delay = nil, 0
		m.gmu.Unlock()
		for _, f := range fs {
			f.ClearFailures()
			f.mu.Lock()
			delete(f.revoked, RefreshToken(f.Self))
			f.mu.Unlock()
		}
		return say("cleared outages, rate limits, slowness, failure rules and revocations for "+scope(a),
			"The next sync succeeds. An account already marked needs_reauth in the hub stays so until you reconnect it (Settings > Accounts > Reconnect; the fake approves at once).", nil), 200
	})
	post("reauth", func(a adminReq, fs []*Fake) (any, int) {
		if a.Account == "" {
			if f := m.fake("stale@example.com"); f != nil {
				fs = []*Fake{f}
			}
		}
		var names []string
		for _, f := range fs {
			f.Revoke(RefreshToken(f.Self))
			names = append(names, f.Self)
		}
		return say("revoked "+strings.Join(names, ", "),
			"Within one sync cycle (15 s in the demo) the account turns needs_reauth: Home banner / Settings > Accounts show \"Reconnect\"; its events stay but are marked stale; other accounts keep syncing.", nil), 200
	})
	post("outside", func(a adminReq, fs []*Fake) (any, int) {
		f := fs[0]
		if a.Account == "" {
			f = m.fake(m.Emails[0])
		}
		out, code := m.outside(f, a)
		return out, code
	})
	post("burst", func(a adminReq, fs []*Fake) (any, int) {
		f := fs[0]
		if a.Account == "" {
			f = m.fake(m.Emails[0])
		}
		n := a.N
		if n == 0 {
			n = 200
		}
		cal := f.CalIDs()[0]
		now := m.now()
		batch := now.Unix()
		for i := 0; i < n; i++ {
			day, slot := 1+i%30, i/30
			s := time.Date(now.Year(), now.Month(), now.Day()+day, 7+slot%12, (i%2)*30, 0, 0, time.UTC)
			ev := Timed(fmt.Sprintf("Burst %03d", i+1), s.Format(time.RFC3339), s.Add(25*time.Minute).Format(time.RFC3339))
			ev["start"].(map[string]any)["timeZone"], ev["end"].(map[string]any)["timeZone"] = "UTC", "UTC"
			f.Put(cal, fmt.Sprintf("burst-%d-%03d", batch, i+1), ev)
		}
		return say(fmt.Sprintf("added %d events titled \"Burst NNN\" to %s over the next 30 days (%s)", n, f.Self, cal),
			"The next sync pulls them in pages; month dots and the agenda for the next 30 days fill up; search \"Burst\" finds them. Check scrolling and sync time with many events.", nil), 200
	})
	post("calendar-add", func(a adminReq, fs []*Fake) (any, int) {
		f := fs[0]
		if a.Account == "" {
			f = m.fake(m.Emails[0])
		}
		id := fmt.Sprintf("popup-%d@group.calendar.google.com", time.Now().Unix())
		f.AddCalWith(id, "Pop-up: Hack night", "writer", "#00bfa5", "Europe/London", false, false)
		s := m.now().Add(48 * time.Hour).Truncate(time.Hour)
		f.Put(id, "hack-1", Timed("Hack night", s.Format(time.RFC3339), s.Add(2*time.Hour).Format(time.RFC3339)))
		f.Put(id, "hack-2", AllDay("Hack night weekend", s.AddDate(0, 0, 5).Format(time.DateOnly), s.AddDate(0, 0, 6).Format(time.DateOnly)))
		return say("added calendar \"Pop-up: Hack night\" with 2 events to "+f.Self,
			"After the next sync: a new calendar in Settings > Calendars (visible, teal) and its two events appear without relaunching.", map[string]any{"cal": id}), 200
	})
	post("calendar-remove", func(a adminReq, fs []*Fake) (any, int) {
		f := fs[0]
		if a.Account == "" {
			f = m.fake(m.Emails[0])
		}
		var victim string
		for _, c := range f.Cals() {
			if strings.HasPrefix(c.ID, "popup-") {
				victim = c.ID
			}
		}
		if victim == "" {
			for _, c := range f.Cals() {
				if c.Role == "reader" && !c.Hidden {
					victim = c.ID
					break
				}
			}
		}
		if victim == "" || !f.RemoveCal(victim) {
			return map[string]any{"error": "no removable calendar on " + f.Self}, 404
		}
		return say("removed calendar "+victim+" from "+f.Self,
			"After the next sync the calendar disappears from Settings > Calendars and its events leave Home, month and agenda (it is detected as removed, not as an error).", map[string]any{"cal": victim}), 200
	})
	mux.HandleFunc("GET /_admin/summary", func(w http.ResponseWriter, r *http.Request) {
		accts := map[string]any{}
		for _, f := range m.targets(r.URL.Query().Get("account")) {
			var cals []map[string]any
			total := 0
			for _, c := range f.Cals() {
				cals = append(cals, map[string]any{"id": c.ID, "name": c.Summary, "role": c.Role, "hidden": c.Hidden, "events": c.Events})
				total += c.Events
			}
			accts[f.Self] = map[string]any{"revoked": f.Revoked(RefreshToken(f.Self)), "events": total, "calendars": cals, "pendingFailureRules": f.PendingFailures()}
		}
		m.gmu.Lock()
		var gs []map[string]any
		for _, g := range m.gates {
			gm := map[string]any{"account": g.account, "code": g.code, "hits": g.hits}
			if !g.until.IsZero() {
				gm["secondsLeft"] = int(time.Until(g.until).Seconds())
			} else {
				gm["requestsLeft"] = g.n
			}
			gs = append(gs, gm)
		}
		delay := m.delay.Milliseconds()
		m.gmu.Unlock()
		writeJSON(w, 200, map[string]any{"profile": m.Profile, "now": m.now().Format(time.RFC3339), "accounts": accts, "outages": gs, "delayMs": delay})
	})
}

func (m *Multi) addGate(g *gate) { m.gmu.Lock(); m.gates = append(m.gates, g); m.gmu.Unlock() }

// outside applies one change "made in Google web" to the next upcoming timed single event of the
// account's primary calendar (move +1h, rename, delete) or adds a new one in 10 minutes.
func (m *Multi) outside(f *Fake, a adminReq) (any, int) {
	cal := f.CalIDs()[0]
	now := m.now()
	say := func(did, look string, extra map[string]any) map[string]any {
		out := map[string]any{"ok": true, "did": did, "look": look, "account": f.Self, "cal": cal}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	if a.Action == "add" {
		id := "outside-" + strconv.FormatInt(time.Now().Unix(), 10)
		s := now.Add(10 * time.Minute).Truncate(time.Minute)
		ev := Timed("Added in Google web", s.Format(time.RFC3339), s.Add(30*time.Minute).Format(time.RFC3339))
		ev["start"].(map[string]any)["timeZone"], ev["end"].(map[string]any)["timeZone"] = "UTC", "UTC"
		ev["hangoutLink"] = "https://meet.google.com/new-out-" + id[len(id)-3:]
		f.Put(cal, id, ev)
		return say("created \"Added in Google web\" in 10 minutes on "+f.Self, "Within 15 s it appears in Home / agenda / Day (live update, no relaunch); \"Next up\" or the join window may change; a notification for a new event, if enabled.",
			map[string]any{"id": id}), 200
	}
	type cand struct {
		id, title string
		start     time.Time
	}
	var best *cand
	for _, e := range f.Dump(cal) {
		id, _ := e["id"].(string)
		if strings.HasPrefix(id, "burst-") || e["recurrence"] != nil || e["recurringEventId"] != nil || e["status"] == "cancelled" {
			continue
		}
		st, _ := e["start"].(map[string]any)
		ds, _ := st["dateTime"].(string)
		t, err := time.Parse(time.RFC3339, ds)
		if err != nil || !t.After(now.Add(5*time.Minute)) {
			continue
		}
		if best == nil || t.Before(best.start) {
			title, _ := e["summary"].(string)
			best = &cand{id, title, t}
		}
	}
	if best == nil {
		return map[string]any{"error": "no upcoming timed single event on " + cal}, 404
	}
	desc := fmt.Sprintf("\"%s\" (%s, %s)", best.title, best.id, best.start.UTC().Format("Mon 15:04 UTC"))
	switch a.Action {
	case "move":
		f.Edit(cal, best.id, func(raw map[string]any) {
			for _, k := range []string{"start", "end"} {
				x := raw[k].(map[string]any)
				t, _ := time.Parse(time.RFC3339, x["dateTime"].(string))
				x["dateTime"] = t.Add(time.Hour).Format(time.RFC3339)
			}
			raw["updated"] = time.Now().UTC().Format(time.RFC3339)
		})
		return say("moved "+desc+" one hour later", "After the next sync the block jumps +1h in Home/Day; a \"moved\" notification (\"<title> moved to HH:MM\") and the \"moved\" word on its row, if notifications for changes are on.",
			map[string]any{"id": best.id}), 200
	case "rename":
		f.Edit(cal, best.id, func(raw map[string]any) {
			raw["summary"] = fmt.Sprint(raw["summary"]) + " (renamed in Google)"
			raw["updated"] = time.Now().UTC().Format(time.RFC3339)
		})
		return say("renamed "+desc, "After the next sync the title shows \"(renamed in Google)\" in every view and in search; open detail updates in place.", map[string]any{"id": best.id}), 200
	case "delete":
		f.Delete(cal, best.id)
		return say("deleted "+desc+" in Google", "After the next sync the event disappears everywhere; a \"cancelled\" notification if enabled; an open detail screen shows it was deleted.", map[string]any{"id": best.id}), 200
	}
	keys := []string{"move", "rename", "delete", "add"}
	sort.Strings(keys)
	return map[string]any{"error": "action must be one of " + strings.Join(keys, ", ")}, 400
}
