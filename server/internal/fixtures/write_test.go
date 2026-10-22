package fixtures

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

var wnow = time.Date(2026, 10, 14, 8, 15, 0, 0, time.UTC) // a Wednesday

type wenv struct {
	t   *testing.T
	doc *openapi3.T
	s   *Server
	h   http.Handler
}

func newW(t *testing.T, outcome string) *wenv {
	s := &Server{Data: New(wnow), WriteOutcome: outcome, WriteLatency: -1, SlowDelay: 120 * time.Millisecond}
	return &wenv{t: t, doc: loadSpec(t), s: s, h: s.Handler()}
}

func (e *wenv) do(method, path string, body any, hdr ...string) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Authorization", "Bearer cal_t")
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *wenv) chk(tmpl, method string, w *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	e.t.Helper()
	checkAny(e.t, e.doc, tmpl, method, w)
	return w
}

func dec(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err, w.Body.String())
	}
	return m
}

func errCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) map[string]any {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d want %d: %s", w.Code, status, w.Body.String())
	}
	er := dec(t, w)["error"].(map[string]any)
	if er["code"] != code {
		t.Fatalf("code %v want %s", er["code"], code)
	}
	return er
}

// agenda lists "title@start" for the whole fixture window.
func (e *wenv) items() []map[string]any {
	q := url.Values{"from": {wnow.AddDate(0, 0, -3).Format(time.RFC3339)}, "to": {wnow.AddDate(0, 0, 20).Format(time.RFC3339)}}.Encode()
	var ag struct{ Items []map[string]any }
	json.Unmarshal(e.do("GET", "/v1/agenda?"+q, nil).Body.Bytes(), &ag)
	return ag.Items
}

func (e *wenv) byTitle(title string) []map[string]any {
	var out []map[string]any
	for _, it := range e.items() {
		if it["title"] == title {
			out = append(out, it)
		}
	}
	return out
}

func ver(m map[string]any) string { return `"` + jsonNum(m["version"]) + `"` }
func jsonNum(v any) string        { b, _ := json.Marshal(v); return string(b) }

func TestWriteCreatePatchDeleteSingle(t *testing.T) {
	e := newW(t, "")
	rev0 := e.s.Data.Rev()
	w := e.chk("/v1/events", "POST", e.do("POST", "/v1/events", map[string]any{"calendarId": CalWork, "title": "Fixture lunch", "attendees": []string{"sam@x.test"},
		"when": map[string]any{"start": "2026-10-16T13:00:00", "tzid": "Europe/London", "durationMin": 45}, "location": "Kōya", "reminderMin": 15}))
	if w.Code != 201 || e.s.Data.Rev() != rev0+1 {
		t.Fatalf("%d %s rev %d->%d", w.Code, w.Body.String(), rev0, e.s.Data.Rev())
	}
	ev := dec(t, w)
	if ev["title"] != "Fixture lunch" || ev["start"] != "2026-10-16T12:00:00Z" || ev["end"] != "2026-10-16T12:45:00Z" || ev["reminderMin"] != float64(15) || w.Header().Get("Location") != "/v1/events/"+ev["eventId"].(string) {
		t.Fatalf("%v", ev)
	}
	if len(e.byTitle("Fixture lunch")) != 1 {
		t.Fatal("not in the agenda")
	}
	// read back
	e.chk("/v1/events/{id}", "GET", e.do("GET", "/v1/events/"+ev["id"].(string), nil))
	// stale If-Match
	id := ev["id"].(string)
	w = e.chk("/v1/events/{id}", "PATCH", e.do("PATCH", "/v1/events/"+id, map[string]any{"title": "Nope"}, "If-Match", `"1"`))
	er := errCode(t, w, 412, "precondition_failed")
	if er["current"].(map[string]any)["title"] != "Fixture lunch" {
		t.Fatal(er)
	}
	w = e.chk("/v1/events/{id}", "PATCH", e.do("PATCH", "/v1/events/"+id, map[string]any{"title": "Renamed", "location": nil, "when": map[string]any{"start": "2026-10-16T15:00:00"}}, "If-Match", ver(ev)))
	got := dec(t, w)
	if w.Code != 200 || got["title"] != "Renamed" || got["location"] != nil || got["start"] != "2026-10-16T14:00:00Z" || got["end"] != "2026-10-16T14:45:00Z" || got["version"].(float64) <= ev["version"].(float64) {
		t.Fatalf("%d %v", w.Code, got)
	}
	id = got["id"].(string) // a changed start gives the occurrence a new id
	e.chk("/v1/events/{id}", "PATCH", e.do("PATCH", "/v1/events/"+id, map[string]any{}))
	errCode(t, e.do("PATCH", "/v1/events/"+id, map[string]any{}), 400, "validation")
	errCode(t, e.chk("/v1/events/{id}", "PATCH", e.do("PATCH", "/v1/events/nope", map[string]any{"title": "x"})), 404, "not_found")
	errCode(t, e.chk("/v1/events/{id}", "PATCH", e.do("PATCH", "/v1/events/"+id+"?scope=sideways", map[string]any{"title": "x"})), 400, "validation")
	w = e.chk("/v1/events/{id}", "DELETE", e.do("DELETE", "/v1/events/"+id, nil, "If-Match", ver(got)))
	if w.Code != 204 || len(e.byTitle("Renamed")) != 0 {
		t.Fatal(w.Code)
	}
	errCode(t, e.chk("/v1/events/{id}", "DELETE", e.do("DELETE", "/v1/events/"+id, nil)), 404, "not_found")
	// validation + read-only calendar
	errCode(t, e.chk("/v1/events", "POST", e.do("POST", "/v1/events", map[string]any{"calendarId": CalTeam, "title": "x", "when": map[string]any{"start": "2026-10-16T13:00:00"}})), 403, "forbidden")
	errCode(t, e.chk("/v1/events", "POST", e.do("POST", "/v1/events", map[string]any{"calendarId": CalWork, "title": "x", "when": map[string]any{"start": "bad"}})), 400, "validation")
	errCode(t, e.chk("/v1/events", "POST", e.do("POST", "/v1/events", nil)), 400, "validation")
}

func TestWriteIdempotency(t *testing.T) {
	e := newW(t, "")
	body := map[string]any{"calendarId": CalWork, "title": "Once", "when": map[string]any{"start": "2026-10-16T13:00:00", "tzid": "Europe/London"}}
	a := e.chk("/v1/events", "POST", e.do("POST", "/v1/events", body, "Idempotency-Key", "k1"))
	b := e.do("POST", "/v1/events", body, "Idempotency-Key", "k1")
	if a.Code != 201 || b.Code != 201 || a.Body.String() != b.Body.String() || b.Header().Get("Idempotent-Replayed") != "true" || len(e.byTitle("Once")) != 1 {
		t.Fatalf("%d %d", a.Code, b.Code)
	}
	body["title"] = "Different"
	errCode(t, e.chk("/v1/events", "POST", e.do("POST", "/v1/events", body, "Idempotency-Key", "k1")), 422, "validation")
}

func TestWriteRecurringScopes(t *testing.T) {
	e := newW(t, "")
	crit := e.byTitle("Design crit")
	if len(crit) < 3 {
		t.Fatalf("fixture series too short: %d", len(crit))
	}
	n := len(crit)
	pick := crit[1]
	// this: one occurrence becomes its own event
	w := e.chk("/v1/events/{id}", "PATCH", e.do("PATCH", "/v1/events/"+pick["id"].(string)+"?scope=this", map[string]any{"title": "Only this crit"}))
	one := dec(t, w)
	if w.Code != 200 || one["eventId"] == pick["eventId"] || one["id"] != one["eventId"].(string)+":"+jsonNum(int64(parseT(pick["start"].(string)).Unix())) || len(e.byTitle("Design crit")) != n-1 || len(e.byTitle("Only this crit")) != 1 {
		t.Fatalf("this: %d %v", w.Code, one)
	}
	// all: the series shifts by the same wall-clock difference; ids follow the new starts
	rest := e.byTitle("Design crit")
	target := rest[0]
	w = e.chk("/v1/events/{id}", "PATCH", e.do("PATCH", "/v1/events/"+target["id"].(string)+"?scope=all", map[string]any{"title": "Crit", "when": map[string]any{"start": localOf(target["start"].(string), 1)}}))
	if w.Code != 200 || len(e.byTitle("Crit")) != len(rest) {
		t.Fatalf("all: %d %s", w.Code, w.Body.String())
	}
	for i, it := range e.byTitle("Crit") {
		if got, want := parseT(it["start"].(string)), parseT(rest[i]["start"].(string)).Add(time.Hour); !got.Equal(want) {
			t.Fatalf("occurrence %d: %v want %v", i, got, want)
		}
		if !strings.HasSuffix(it["id"].(string), ":"+jsonNum(parseT(it["start"].(string)).Unix())) {
			t.Fatalf("id does not follow start: %v", it["id"])
		}
	}
	// following: from the 3rd occurrence on gets a new event id
	cs := e.byTitle("Crit")
	w = e.chk("/v1/events/{id}", "PATCH", e.do("PATCH", "/v1/events/"+cs[1]["id"].(string)+"?scope=following", map[string]any{"title": "Later crit"}))
	got := dec(t, w)
	if w.Code != 200 || got["eventId"] == cs[0]["eventId"] || len(e.byTitle("Crit")) != 1 || len(e.byTitle("Later crit")) != len(cs)-1 {
		t.Fatalf("following: %d %v", w.Code, w.Body.String())
	}
	// delete scopes
	later := e.byTitle("Later crit")
	if len(later) < 2 {
		t.Fatalf("need two later occurrences, have %d", len(later))
	}
	e.chk("/v1/events/{id}", "DELETE", e.do("DELETE", "/v1/events/"+later[0]["id"].(string)+"?scope=this", nil))
	if len(e.byTitle("Later crit")) != len(later)-1 {
		t.Fatal("delete this")
	}
	later = e.byTitle("Later crit")
	e.chk("/v1/events/{id}", "DELETE", e.do("DELETE", "/v1/events/"+later[0]["id"].(string)+"?scope=following", nil))
	if len(e.byTitle("Later crit")) != 0 {
		t.Fatal("delete following")
	}
	e.chk("/v1/events/{id}", "DELETE", e.do("DELETE", "/v1/events/"+e.byTitle("Crit")[0]["id"].(string)+"?scope=all", nil))
	if len(e.byTitle("Crit")) != 0 {
		t.Fatal("delete all")
	}
}

func parseT(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }

// localOf renders an RFC 3339 instant as London wall time, plus hours.
func localOf(s string, plusHours int) string {
	l, _ := time.LoadLocation("Europe/London")
	return parseT(s).In(l).Add(time.Duration(plusHours) * time.Hour).Format("2006-01-02T15:04:05")
}

func TestWriteRSVPMeetMoveReminder(t *testing.T) {
	e := newW(t, "")
	plan := e.byTitle("Quarterly planning")[0] // needsAction invite
	w := e.chk("/v1/events/{id}/rsvp", "POST", e.do("POST", "/v1/events/"+plan["id"].(string)+"/rsvp", map[string]any{"response": "declined", "note": "away"}))
	if w.Code != 200 || dec(t, w)["myResponse"] != "declined" {
		t.Fatal(w.Code, w.Body.String())
	}
	errCode(t, e.chk("/v1/events/{id}/rsvp", "POST", e.do("POST", "/v1/events/"+plan["id"].(string)+"/rsvp", map[string]any{"response": "maybe"})), 400, "validation")
	errCode(t, e.chk("/v1/events/{id}/rsvp", "POST", e.do("POST", "/v1/events/"+plan["id"].(string)+"/rsvp", map[string]any{"response": "accepted", "scope": "following"})), 400, "validation")
	lunch := e.byTitle("Lunch with Sam")[0]
	errCode(t, e.chk("/v1/events/{id}/rsvp", "POST", e.do("POST", "/v1/events/"+lunch["id"].(string)+"/rsvp", map[string]any{"response": "accepted"})), 403, "forbidden")

	w = e.chk("/v1/events/{id}/meet", "POST", e.do("POST", "/v1/events/"+lunch["id"].(string)+"/meet", nil))
	if m := dec(t, w); w.Code != 200 || m["join"].(map[string]any)["kind"] != "meet" || m["canAddConference"] != false {
		t.Fatal(w.Body.String())
	}
	errCode(t, e.chk("/v1/events/{id}/meet", "POST", e.do("POST", "/v1/events/"+lunch["id"].(string)+"/meet", nil)), 403, "forbidden")

	w = e.chk("/v1/events/{id}/move", "POST", e.do("POST", "/v1/events/"+lunch["id"].(string)+"/move", map[string]any{"calendarId": CalSide}))
	mv := dec(t, w)
	if w.Code != 200 || mv["calendarId"] != CalSide || mv["accountId"] != AccOchre || mv["eventId"] == lunch["eventId"] {
		t.Fatalf("cross-account move: %v", w.Body.String())
	}
	errCode(t, e.chk("/v1/events/{id}/move", "POST", e.do("POST", "/v1/events/"+mv["id"].(string)+"/move", map[string]any{"calendarId": CalTeam})), 403, "forbidden")
	standup := e.byTitle("Team standup")[0]
	errCode(t, e.chk("/v1/events/{id}/move", "POST", e.do("POST", "/v1/events/"+standup["id"].(string)+"/move", map[string]any{"calendarId": CalSide})), 400, "validation")

	w = e.chk("/v1/events/{id}/reminder", "PUT", e.do("PUT", "/v1/events/"+mv["id"].(string)+"/reminder", map[string]any{"minutesBefore": 30}))
	if dec(t, w)["reminderMin"] != float64(30) {
		t.Fatal(w.Body.String())
	}
	w = e.chk("/v1/events/{id}/reminder", "PUT", e.do("PUT", "/v1/events/"+mv["id"].(string)+"/reminder", map[string]any{"minutesBefore": nil}))
	if dec(t, w)["reminderMin"] != nil {
		t.Fatal(w.Body.String())
	}
	errCode(t, e.chk("/v1/events/{id}/reminder", "PUT", e.do("PUT", "/v1/events/"+mv["id"].(string)+"/reminder", map[string]any{})), 400, "validation")
	// read-only
	off := e.byTitle("Company offsite")[0]
	errCode(t, e.chk("/v1/events/{id}", "PATCH", e.do("PATCH", "/v1/events/"+off["id"].(string), map[string]any{"title": "x"})), 403, "forbidden")
	errCode(t, e.chk("/v1/events/{id}", "DELETE", e.do("DELETE", "/v1/events/"+off["id"].(string), nil)), 403, "forbidden")
}

func TestWriteForcedOutcomes(t *testing.T) {
	for _, tc := range []struct {
		outcome, code string
		status        int
	}{{"fail", "provider_error", 502}, {"ratelimited", "rate_limited", 429}, {"conflict", "conflict", 409}} {
		e := newW(t, tc.outcome)
		it := e.byTitle("Lunch with Sam")[0]
		before := it["version"].(float64)
		w := e.chk("/v1/events/{id}", "PATCH", e.do("PATCH", "/v1/events/"+it["id"].(string), map[string]any{"title": "Mine"}))
		er := errCode(t, w, tc.status, tc.code)
		switch tc.outcome {
		case "fail":
			if er["message"] == "" || len(e.byTitle("Mine")) != 0 {
				t.Fatal("fail must change nothing")
			}
		case "ratelimited":
			if w.Header().Get("Retry-After") != "30" {
				t.Fatal(w.Header())
			}
		case "conflict":
			cur := er["current"].(map[string]any)
			if cur["title"] != "Lunch with Sam (edited elsewhere)" || cur["version"].(float64) <= before {
				t.Fatalf("%v", cur)
			}
			// the fixture really did change, so refetching shows the same thing
			if got := dec(t, e.do("GET", "/v1/events/"+it["id"].(string), nil)); got["version"] != cur["version"] {
				t.Fatal("conflict state not applied")
			}
		}
		// creates fail too, but a conflict makes no sense for them
		w = e.do("POST", "/v1/events", map[string]any{"calendarId": CalWork, "title": "x", "when": map[string]any{"start": "2026-10-16T13:00:00"}})
		if tc.outcome == "conflict" && w.Code != 201 {
			t.Fatalf("create under conflict outcome: %d", w.Code)
		}
		if tc.outcome != "conflict" && w.Code != tc.status {
			t.Fatalf("create under %s: %d", tc.outcome, w.Code)
		}
	}
	// the header overrides the server default, and reminders are hub-only so they always succeed
	e := newW(t, "ok")
	it := e.byTitle("Lunch with Sam")[0]
	if w := e.do("PATCH", "/v1/events/"+it["id"].(string), map[string]any{"title": "x"}, "X-Fixture-Outcome", "fail"); w.Code != 502 {
		t.Fatal(w.Code)
	}
	e2 := newW(t, "fail")
	it = e2.byTitle("Lunch with Sam")[0]
	if w := e2.do("PUT", "/v1/events/"+it["id"].(string)+"/reminder", map[string]any{"minutesBefore": 5}); w.Code != 200 {
		t.Fatal("reminder should not fail", w.Code)
	}
	if w := e2.do("PATCH", "/v1/events/"+it["id"].(string), map[string]any{"title": "x"}, "X-Fixture-Outcome", "ok"); w.Code != 200 {
		t.Fatal("header ok did not override", w.Code)
	}
}

func TestWriteLatencyAndSlow(t *testing.T) {
	s := &Server{Data: New(wnow), WriteLatency: 80 * time.Millisecond, SlowDelay: 150 * time.Millisecond}
	e := &wenv{t: t, doc: loadSpec(t), s: s, h: s.Handler()}
	it := e.byTitle("Lunch with Sam")[0]
	t0 := time.Now()
	e.do("PATCH", "/v1/events/"+it["id"].(string), map[string]any{"title": "a"})
	if d := time.Since(t0); d < 80*time.Millisecond {
		t.Fatalf("no latency: %v", d)
	}
	t0 = time.Now()
	w := e.do("PATCH", "/v1/events/"+it["id"].(string), map[string]any{"title": "b"}, "X-Fixture-Outcome", "slow")
	if d := time.Since(t0); d < 230*time.Millisecond || w.Code != 200 {
		t.Fatalf("slow: %v %d", d, w.Code)
	}
	// the documented default is 600 ms
	if (&Server{}).latency() != 600*time.Millisecond || (&Server{}).slow() != 5*time.Second {
		t.Fatal("defaults")
	}
}

func TestWritesPublishRevOnStream(t *testing.T) {
	s := &Server{Data: New(wnow), WriteLatency: -1, PingEvery: time.Hour}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL+"/v1/stream", nil)
	req.Header.Set("Authorization", "Bearer cal_x")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	waitFor := func(want string) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			select {
			case l := <-lines:
				if l == want {
					return
				}
			case <-deadline:
				t.Fatalf("never saw %q", want)
			}
		}
	}
	waitFor(`data: {"rev":1}`)
	post := func() {
		r, _ := http.NewRequest("POST", ts.URL+"/v1/events", strings.NewReader(`{"calendarId":"`+CalWork+`","title":"live","when":{"start":"2026-10-16T13:00:00"}}`))
		r.Header.Set("Authorization", "Bearer cal_x")
		if rr, err := http.DefaultClient.Do(r); err != nil || rr.StatusCode != 201 {
			t.Fatal(err, rr)
		} else {
			rr.Body.Close()
		}
	}
	post()
	waitFor(`data: {"rev":2}`)
	post()
	waitFor(`data: {"rev":3}`)
}

func TestEveryStateStillServesWrites(t *testing.T) {
	for _, st := range []string{"empty", "banner", "syncing", "loading"} {
		s := &Server{Data: New(wnow), State: st, Delay: time.Millisecond, WriteLatency: -1}
		h := s.Handler()
		r := httptest.NewRequest("POST", "/v1/events", strings.NewReader(`{"calendarId":"`+CalWork+`","title":"x","when":{"start":"2026-10-16T13:00:00"}}`))
		r.Header.Set("Authorization", "Bearer cal_x")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 201 {
			t.Errorf("%s: %d %s", st, w.Code, w.Body.String())
		}
	}
}
