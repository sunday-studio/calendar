package fixtures

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func stateServer(t *testing.T, state string) *httptest.Server {
	t.Helper()
	s := &Server{Data: New(time.Now()), Name: "fx", Version: "t", State: state, Delay: 150 * time.Millisecond}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func sget(t *testing.T, ts *httptest.Server, path string) (int, map[string]any, error) {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+path, nil)
	req.Header.Set("Authorization", "Bearer cal_x")
	resp, err := (&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m, nil
}

const swin = "/v1/agenda?from=2020-01-01T00:00:00Z&to=2030-01-01T00:00:00Z"

func sitems(m map[string]any) int { return len(m["items"].([]any)) }

func TestFixtureStates(t *testing.T) {
	for _, st := range States {
		if !ValidState(st) {
			t.Fatal(st)
		}
	}
	if ValidState("nope") {
		t.Fatal("accepted junk")
	}

	code, m, _ := sget(t, stateServer(t, "normal"), swin)
	if code != 200 || sitems(m) < 10 {
		t.Fatal("normal baseline", code)
	}

	ts := stateServer(t, "empty")
	if code, m, _ = sget(t, ts, swin); code != 200 || sitems(m) != 0 {
		t.Fatal("empty agenda", code)
	}
	if _, m, _ = sget(t, ts, "/v1/accounts"); sitems(m) != 3 {
		t.Fatal("empty keeps accounts")
	}

	ts = stateServer(t, "noaccounts")
	for _, p := range []string{"/v1/accounts", "/v1/calendars", swin} {
		if _, m, _ = sget(t, ts, p); sitems(m) != 0 {
			t.Fatal("noaccounts", p)
		}
	}
	if _, m, _ = sget(t, ts, "/v1/status"); len(m["accounts"].([]any)) != 0 {
		t.Fatal("status accounts")
	}

	ts = stateServer(t, "banner")
	_, m, _ = sget(t, ts, "/v1/accounts")
	a := m["items"].([]any)[0].(map[string]any)
	if a["status"] != "needs_reauth" || a["statusDetail"] == "" || sitems(func() map[string]any { _, x, _ := sget(t, ts, swin); return x }()) < 10 {
		t.Fatalf("banner %v", a)
	}

	ts = stateServer(t, "syncing")
	_, m, _ = sget(t, ts, "/v1/accounts")
	as := m["items"].([]any)
	if as[0].(map[string]any)["status"] != "active" || as[1].(map[string]any)["status"] != "active" || as[2].(map[string]any)["status"] != "syncing" {
		t.Fatalf("%v", as)
	}

	ts = stateServer(t, "down")
	if code, _, _ = sget(t, ts, "/v1/hello"); code != 200 {
		t.Fatal("hello must answer")
	}
	code, m, _ = sget(t, ts, swin)
	if code != 503 || m["error"].(map[string]any)["code"] != "unavailable" {
		t.Fatal("down", code, m)
	}

	ts = stateServer(t, "offline")
	if code, _, _ = sget(t, ts, "/v1/hello"); code != 200 {
		t.Fatal("hello must answer")
	}
	if _, _, err := sget(t, ts, swin); err == nil {
		t.Fatal("offline must drop the connection")
	}

	ts = stateServer(t, "loading")
	t0 := time.Now()
	sget(t, ts, "/v1/hello")
	if time.Since(t0) > 100*time.Millisecond {
		t.Fatal("hello must not be slow")
	}
	code, _, _ = sget(t, ts, swin)
	if code != 200 || time.Since(t0) < 150*time.Millisecond {
		t.Fatal("loading must delay")
	}
}

func TestDefaultFixtureCoverage(t *testing.T) {
	ts := stateServer(t, "normal")
	_, ag, _ := sget(t, ts, swin)
	var many, weekly, readonly, meet, zoomParsed, cancelled, declined, long, short, exc int
	byStart := map[string]int{}
	for _, raw := range ag["items"].([]any) {
		it := raw.(map[string]any)
		code, ev, _ := sget(t, ts, "/v1/events/"+it["id"].(string))
		if code != 200 || ev["id"] != it["id"] {
			t.Fatalf("detail for %v: %d", it["title"], code)
		}
		title := it["title"].(string)
		if at, ok := ev["attendees"].([]any); ok && len(at) >= 6 {
			many++
		}
		if r, ok := ev["recurrence"].(map[string]any); ok && r["summary"] == "Weekly on Tue, Thu" {
			weekly++
			if title != "Design crit" {
				exc++
			}
		}
		if it["canEdit"] == false {
			readonly++
		}
		if j, ok := it["join"].(map[string]any); ok {
			if j["kind"] == "meet" {
				meet++
			}
			if j["kind"] == "zoom" && strings.Contains(fmt.Sprint(ev["description"]), j["url"].(string)) {
				zoomParsed++
			}
		}
		switch {
		case it["status"] == "cancelled":
			cancelled++
		case it["myResponse"] == "declined":
			declined++
		}
		if len(title) >= 60 {
			long++
		}
		if s, e := it["start"], it["end"]; s != nil {
			st, _ := time.Parse(time.RFC3339, s.(string))
			en, _ := time.Parse(time.RFC3339, e.(string))
			if en.Sub(st) == 15*time.Minute {
				short++
			}
			if it["status"] == "confirmed" {
				byStart[st.Format("2006-01-02")]++
			}
		}
	}
	for name, n := range map[string]int{"6+ guests": many, "weekly tue/thu": weekly, "exception": exc, "read-only": readonly, "meet": meet,
		"zoom in description": zoomParsed, "cancelled": cancelled, "declined": declined, "long title": long, "15 min": short} {
		if n == 0 {
			t.Errorf("fixtures lack: %s", name)
		}
	}
	// three events overlap on day 0 (10:00-10:45, 10:15-11:00, 10:30-11:30 local)
	titles := map[string]bool{}
	for _, raw := range ag["items"].([]any) {
		titles[raw.(map[string]any)["title"].(string)] = true
	}
	for _, want := range []string{"Design review", "Interview: Staff designer", "Roadmap sync"} {
		if !titles[want] {
			t.Errorf("missing overlap member %q", want)
		}
	}
}
