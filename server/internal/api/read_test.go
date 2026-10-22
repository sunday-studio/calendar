package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"calendar/server/internal/agenda"
	. "calendar/server/internal/agenda/agendatest"
	"calendar/server/internal/authn"
)

func unix(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.Unix()
}

type readEnv struct {
	*env
	read, write string
}

func newReadEnv(t *testing.T) *readEnv {
	e := newEnv(t)
	e.s.Store = &agenda.Store{DB: e.d, Now: func() time.Time { return time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC) }}
	_, rd, _ := e.s.Auth.Create(context.Background(), "r", "script", authn.Read)
	_, wr, _ := e.s.Auth.Create(context.Background(), "w", "script", authn.Write)
	Do(t, e.d, func(s *Seed) {
		s.Account("A1", "cobalt", "a@x.test")
		s.Account("A2", "rose", "b@x.test")
		s.Calendar("C1", "A1", "Work", true, "owner", true)
		s.Calendar("C2", "A2", "Home", true, "writer", true)
		s.Calendar("CH", "A2", "Hidden", false, "reader", false)
		s.Event(Ev{ID: "E1", Cal: "C1", Title: "Standup", Kind: "master", RRule: "FREQ=WEEKLY;BYDAY=TU,TH", Start: unix("2026-10-15T09:30:00Z"), End: unix("2026-10-15T09:45:00Z"),
			Conf: "https://meet.google.com/abc", ConfKind: "meet", Self: "accepted", Org: [2]string{"boss@x.test", "Boss"}, Attendees: [][2]string{{"a@x.test", "Alex"}, {"boss@x.test", "Boss"}},
			Occ: []Occ{{RKey: unix("2026-10-15T09:30:00Z"), Start: unix("2026-10-15T09:30:00Z"), End: unix("2026-10-15T09:45:00Z")}, {RKey: unix("2026-10-20T09:30:00Z"), Start: unix("2026-10-20T09:30:00Z"), End: unix("2026-10-20T09:45:00Z")}}})
		s.Event(Ev{ID: "E2", Cal: "C2", Title: "Lunch", Location: "Kōya", Description: "with Sam", Start: unix("2026-10-15T12:00:00Z"), End: unix("2026-10-15T13:00:00Z")})
		s.Event(Ev{ID: "E3", Cal: "C2", Title: "Birthday", SD: "2026-10-15", ED: "2026-10-16", NotBusy: true})
		s.Event(Ev{ID: "E4", Cal: "CH", Title: "Hidden lunch", Start: unix("2026-10-15T10:00:00Z"), End: unix("2026-10-15T11:00:00Z")})
	})
	return &readEnv{env: e, read: rd, write: wr}
}

func (e *readEnv) get(path, tok string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

const q15 = "from=2026-10-15T00:00:00Z&to=2026-10-16T00:00:00Z"

func TestAgendaHTTP(t *testing.T) {
	e := newReadEnv(t)
	if w := e.get("/v1/agenda?"+q15, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := e.get("/v1/agenda?"+q15, e.read)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var ag struct {
		Rev   int64
		Items []map[string]any
	}
	json.Unmarshal(w.Body.Bytes(), &ag)
	var got []string
	for _, i := range ag.Items {
		got = append(got, i["title"].(string))
	}
	if strings.Join(got, ",") != "Birthday,Standup,Lunch" {
		t.Fatalf("%v", got)
	}
	if id := ag.Items[1]["id"].(string); id != "E1:"+itoa(unix("2026-10-15T09:30:00Z")) {
		t.Fatal(id)
	}
	if _, has := ag.Items[0]["start"]; has {
		t.Fatal("all-day must omit start")
	}

	// ETag / 304
	et := w.Header().Get("ETag")
	if !strings.HasPrefix(et, `W/"rev-`) {
		t.Fatal(et)
	}
	if w2 := e.get("/v1/agenda?"+q15, e.read, "If-None-Match", et); w2.Code != 304 || w2.Body.Len() != 0 || w2.Header().Get("ETag") != et {
		t.Fatalf("want 304 got %d", w2.Code)
	}
	if w2 := e.get("/v1/agenda?"+q15, e.read, "If-None-Match", strings.TrimPrefix(et, "W/")); w2.Code != 304 {
		t.Fatal("strong form of same tag must match weakly")
	}
	if w2 := e.get("/v1/agenda?"+q15+"&calendars=C1", e.read, "If-None-Match", et); w2.Code != 200 || w2.Header().Get("ETag") == et {
		t.Fatal("calendars param must change etag")
	}
	// calendars order / dup insensitive
	a := e.get("/v1/agenda?"+q15+"&calendars=C2,C1", e.read).Header().Get("ETag")
	b := e.get("/v1/agenda?"+q15+"&calendars=C1,C2,C1", e.read).Header().Get("ETag")
	if a != b {
		t.Fatal("etag must ignore calendar order/dups")
	}
	// any data change invalidates it
	if w3 := e.req("PATCH", "/v1/calendars/C1", e.write, map[string]any{"visible": false}); w3.Code != 200 {
		t.Fatal(w3.Code, w3.Body.String())
	}
	w4 := e.get("/v1/agenda?"+q15, e.read, "If-None-Match", et)
	if w4.Code != 200 || w4.Header().Get("ETag") == et {
		t.Fatal("rev bump must invalidate")
	}
	json.Unmarshal(w4.Body.Bytes(), &ag)
	for _, i := range ag.Items {
		if i["title"] == "Standup" {
			t.Fatal("hidden calendar still in agenda")
		}
	}

	// validation
	for _, p := range []string{"", "from=x&to=y", "from=2026-10-15T00:00:00Z", "from=2026-10-16T00:00:00Z&to=2026-10-15T00:00:00Z", "from=2020-01-01T00:00:00Z&to=2026-01-01T00:00:00Z"} {
		if w := e.get("/v1/agenda?"+p, e.read); w.Code != 400 || !strings.Contains(w.Body.String(), `"validation"`) {
			t.Errorf("%q: %d", p, w.Code)
		}
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestScopesAndPatchSyncKick(t *testing.T) {
	e := newReadEnv(t)
	if w := e.req("PATCH", "/v1/calendars/C1", e.read, map[string]any{"visible": false}); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := e.req("PUT", "/v1/settings", e.read, map[string]any{}); w.Code != 403 {
		t.Fatal(w.Code)
	}
	st := &stubSyncer{e: e.env}
	e.s.Syncer = st
	e.h = e.s.Handler()
	e.req("PATCH", "/v1/calendars/CH", e.write, map[string]any{"visible": true})
	if len(st.calls) != 1 || st.calls[0] != "A2" {
		t.Fatalf("becoming visible should sync its account: %v", st.calls)
	}
	e.req("PATCH", "/v1/calendars/CH", e.write, map[string]any{"visible": true})
	e.req("PATCH", "/v1/calendars/CH", e.write, map[string]any{"colorOverride": "moss"})
	if len(st.calls) != 1 {
		t.Fatalf("no kick without a hidden->visible change: %v", st.calls)
	}
	for _, body := range []any{map[string]any{}, map[string]any{"visible": "yes"}, map[string]any{"colorOverride": "neon"}, map[string]any{"colorOverride": 4}} {
		if w := e.req("PATCH", "/v1/calendars/C1", e.write, body); w.Code != 400 {
			t.Errorf("%v: %d", body, w.Code)
		}
	}
	if w := e.req("PATCH", "/v1/calendars/nope", e.write, map[string]any{"visible": true}); w.Code != 404 {
		t.Fatal(w.Code)
	}
	// colourOverride null clears
	e.req("PATCH", "/v1/calendars/C1", e.write, map[string]any{"colorOverride": "moss"})
	w := e.req("PATCH", "/v1/calendars/C1", e.write, map[string]any{"colorOverride": nil})
	var c map[string]any
	json.Unmarshal(w.Body.Bytes(), &c)
	if c["color"] != "cobalt" || c["colorOverride"] != nil {
		t.Fatalf("%v", c)
	}
}

func TestEventsHTTP(t *testing.T) {
	e := newReadEnv(t)
	w := e.get("/v1/events/E1", e.read)
	var ev agenda.Event
	json.Unmarshal(w.Body.Bytes(), &ev)
	if w.Code != 200 || ev.Recurrence == nil || ev.Recurrence.Summary != "Weekly on Tue, Thu" || len(ev.Attendees) != 2 || ev.Organizer.Email != "boss@x.test" || ev.CalendarName != "Work" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := e.get("/v1/events/nope", e.read); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := e.get("/v1/events?q=lunch", e.read); w.Code != 200 || strings.Contains(w.Body.String(), "Hidden lunch") || !strings.Contains(w.Body.String(), `"Lunch"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := e.get("/v1/events?q=lunch&calendar=CH", e.read); !strings.Contains(w.Body.String(), "Hidden lunch") {
		t.Fatal(w.Body.String())
	}
	for _, p := range []string{"", "from=2026-10-15T00:00:00Z", "q=%21%21", "q=a&limit=0", "q=a&limit=500", "q=a&cursor=%25", "q=a&from=bad"} {
		if w := e.get("/v1/events?"+p, e.read); w.Code != 400 {
			t.Errorf("%q: %d", p, w.Code)
		}
	}
	// pagination over HTTP
	var seen []string
	cur := ""
	for i := 0; i < 5; i++ {
		u := "/v1/events?" + q15 + "&limit=2"
		if cur != "" {
			u += "&cursor=" + url.QueryEscape(cur)
		}
		var pg struct {
			Items      []struct{ ID string }
			NextCursor *string
		}
		json.Unmarshal(e.get(u, e.read).Body.Bytes(), &pg)
		for _, it := range pg.Items {
			seen = append(seen, it.ID)
		}
		if pg.NextCursor == nil {
			break
		}
		cur = *pg.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("%v", seen)
	}
}

func TestSettingsHTTP(t *testing.T) {
	e := newReadEnv(t)
	w := e.get("/v1/settings", e.read)
	var s map[string]any
	json.Unmarshal(w.Body.Bytes(), &s)
	if w.Code != 200 || s["weekStart"].(float64) != 1 || s["userTzid"] != "Europe/London" {
		t.Fatal(w.Body.String())
	}
	body := map[string]any{"defaultCalendarId": "C1", "weekStart": 0, "secondaryTzid": "Asia/Tokyo", "userTzid": "America/New_York",
		"workingHours": map[string]any{"start": "08:30", "end": "16:00", "days": []int{1, 2, 3, 4}}, "alertDefaults": map[string]any{"timed": 15, "allDay": nil}}
	w = e.req("PUT", "/v1/settings", e.write, body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var back map[string]any
	json.Unmarshal(e.get("/v1/settings", e.read).Body.Bytes(), &back)
	if back["defaultCalendarId"] != "C1" || back["secondaryTzid"] != "Asia/Tokyo" || back["userTzid"] != "America/New_York" || back["workingHours"].(map[string]any)["start"] != "08:30" {
		t.Fatal(back)
	}
	// birthday (all-day) was re-timed to New York midnight
	var su int64
	e.d.R.QueryRow(`SELECT start_utc FROM occurrences WHERE event_id='E3'`).Scan(&su)
	if su != unix("2026-10-15T04:00:00Z") {
		t.Fatal(su)
	}
	body["weekStart"] = 9
	w = e.req("PUT", "/v1/settings", e.write, body)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"field":"weekStart"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := e.req("PUT", "/v1/settings", e.write, "not an object"); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestFreeBusyHTTP(t *testing.T) {
	e := newReadEnv(t)
	w := e.get("/v1/freebusy?"+q15, e.read)
	var fb agenda.FreeBusy
	json.Unmarshal(w.Body.Bytes(), &fb)
	if w.Code != 200 || len(fb.Busy) != 2 { // standup + lunch; birthday is not busy; hidden excluded
		t.Fatal(w.Code, w.Body.String())
	}
	if w := e.get("/v1/freebusy?from=x", e.read); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

// ---- contract: every real response validates against api/openapi.yaml ----

func TestReadResponsesMatchOpenAPI(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml invalid: %v", err)
	}
	check := func(tmpl, method string, w *httptest.ResponseRecorder) {
		t.Helper()
		op := doc.Paths.Find(tmpl).GetOperation(method)
		if op == nil {
			t.Fatalf("no operation %s %s", method, tmpl)
		}
		ref := op.Responses.Status(w.Code)
		if ref == nil || ref.Value == nil {
			t.Fatalf("%s %s: status %d not documented (%s)", method, tmpl, w.Code, w.Body.String())
		}
		mt := ref.Value.Content.Get("application/json")
		if mt == nil {
			t.Fatalf("%s %s: no json content for %d", method, tmpl, w.Code)
		}
		var v any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if err := mt.Schema.Value.VisitJSON(v, openapi3.MultiErrors()); err != nil {
			t.Errorf("%s %s (%d) violates schema: %v\n%s", method, tmpl, w.Code, err, w.Body.String())
		}
	}
	e := newReadEnv(t)
	check("/v1/agenda", "GET", e.get("/v1/agenda?"+q15, e.read))
	check("/v1/agenda", "GET", e.get("/v1/agenda?from=x&to=y", e.read))
	check("/v1/agenda", "GET", e.get("/v1/agenda?"+q15, ""))
	check("/v1/events", "GET", e.get("/v1/events?"+q15, e.read))
	check("/v1/events", "GET", e.get("/v1/events?q=stand", e.read))
	check("/v1/events", "GET", e.get("/v1/events?q=%21", e.read))
	for _, id := range []string{"E1", "E2", "E3"} {
		check("/v1/events/{id}", "GET", e.get("/v1/events/"+id, e.read))
	}
	check("/v1/events/{id}", "GET", e.get("/v1/events/nope", e.read))
	check("/v1/calendars", "GET", e.get("/v1/calendars", e.read))
	check("/v1/calendars/{id}", "PATCH", e.req("PATCH", "/v1/calendars/C1", e.write, map[string]any{"colorOverride": "moss"}))
	check("/v1/calendars/{id}", "PATCH", e.req("PATCH", "/v1/calendars/C1", e.write, map[string]any{"colorOverride": nil, "visible": false}))
	check("/v1/calendars/{id}", "PATCH", e.req("PATCH", "/v1/calendars/C1", e.write, map[string]any{}))
	check("/v1/calendars/{id}", "PATCH", e.req("PATCH", "/v1/calendars/C1", e.read, map[string]any{"visible": true}))
	check("/v1/calendars/{id}", "PATCH", e.req("PATCH", "/v1/calendars/zz", e.write, map[string]any{"visible": true}))
	check("/v1/settings", "GET", e.get("/v1/settings", e.read))
	good := map[string]any{"defaultCalendarId": "C1", "weekStart": 1, "workingHours": map[string]any{"start": "09:00", "end": "17:00", "days": []int{1, 2, 3}}}
	check("/v1/settings", "PUT", e.req("PUT", "/v1/settings", e.write, good))
	good["weekStart"] = 8
	check("/v1/settings", "PUT", e.req("PUT", "/v1/settings", e.write, good))
	check("/v1/settings", "PUT", e.req("PUT", "/v1/settings", e.read, good))
	check("/v1/freebusy", "GET", e.get("/v1/freebusy?"+q15, e.read))
	check("/v1/freebusy", "GET", e.get("/v1/freebusy", e.read))
	_ = http.StatusOK
}
