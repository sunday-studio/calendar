package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"calendar/server/internal/authn"
	"calendar/server/internal/automate"
	"calendar/server/internal/vault"
)

func checkSchema(t *testing.T, doc *openapi3.T, tmpl, method string, w *httptest.ResponseRecorder) {
	t.Helper()
	op := doc.Paths.Find(tmpl).GetOperation(method)
	if op == nil {
		t.Fatalf("no operation %s %s", method, tmpl)
	}
	ref := op.Responses.Status(w.Code)
	if ref == nil || ref.Value == nil {
		t.Fatalf("%s %s: status %d not documented (%s)", method, tmpl, w.Code, w.Body.String())
	}
	if w.Code == 204 {
		return
	}
	var v any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if err := ref.Value.Content.Get("application/json").Schema.Value.VisitJSON(v, openapi3.MultiErrors()); err != nil {
		t.Errorf("%s %s (%d) violates schema: %v\n%s", method, tmpl, w.Code, err, w.Body.String())
	}
}

func TestTokensAdminAPI(t *testing.T) {
	e := newReadEnv(t)
	doc, err := openapi3.NewLoader().LoadFromFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, admin, _ := e.s.Auth.Create(context.Background(), "admin", "device", authn.Admin)
	chk := func(tmpl, method string, w *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		checkSchema(t, doc, tmpl, method, w)
		return w
	}
	chk("/v1/tokens", "GET", e.req("GET", "/v1/tokens", e.write, nil)) // write scope is not enough
	if w := e.req("GET", "/v1/tokens", e.write, nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
	w := chk("/v1/tokens", "POST", e.req("POST", "/v1/tokens", admin, map[string]any{"name": " grafana ", "kind": "script", "scope": "read"}))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	secret, id := created["secret"].(string), created["id"].(string)
	if !strings.HasPrefix(secret, "cal_") || created["name"] != "grafana" || created["scope"] != "read" || created["revokedAt"] != nil {
		t.Fatalf("%v", created)
	}
	// the secret is shown once: listing never contains it
	w = chk("/v1/tokens", "GET", e.req("GET", "/v1/tokens", admin, nil))
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("secret leaked in list")
	}
	// a read-only script token reads but cannot write
	if w := e.get("/v1/agenda?"+q15, secret); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := e.req("PATCH", "/v1/calendars/C1", secret, map[string]any{"visible": true}); w.Code != 403 {
		t.Fatalf("read token wrote: %d", w.Code)
	}
	for _, bad := range []map[string]any{{"name": "", "kind": "script", "scope": "read"}, {"name": "x", "kind": "robot", "scope": "read"}, {"name": "x", "kind": "script", "scope": "root"}} {
		chk("/v1/tokens", "POST", e.req("POST", "/v1/tokens", admin, bad))
		if w := e.req("POST", "/v1/tokens", admin, bad); w.Code != 400 {
			t.Fatalf("%v: %d", bad, w.Code)
		}
	}
	// revoke
	if w := chk("/v1/tokens/{id}", "DELETE", e.req("DELETE", "/v1/tokens/"+id, admin, nil)); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w := e.get("/v1/agenda?"+q15, secret); w.Code != 401 {
		t.Fatal("revoked token still works:", w.Code)
	}
	chk("/v1/tokens/{id}", "DELETE", e.req("DELETE", "/v1/tokens/"+id, admin, nil))
	if w := e.req("DELETE", "/v1/tokens/"+id, admin, nil); w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = chk("/v1/tokens", "GET", e.req("GET", "/v1/tokens", admin, nil))
	if !strings.Contains(w.Body.String(), `"revokedAt":"`) {
		t.Fatal("revoked token missing from list")
	}
	// cannot revoke the token in use
	var me map[string]any
	json.Unmarshal(e.req("GET", "/v1/hello", admin, nil).Body.Bytes(), &me)
	w = chk("/v1/tokens/{id}", "DELETE", e.req("DELETE", "/v1/tokens/"+me["tokenId"].(string), admin, nil))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestFreeTimeHTTP(t *testing.T) {
	e := newReadEnv(t)
	doc, _ := openapi3.NewLoader().LoadFromFile("../../../api/openapi.yaml")
	post := func(body map[string]any) *httptest.ResponseRecorder {
		w := e.req("POST", "/v1/freetime", e.read, body)
		checkSchema(t, doc, "/v1/freetime", "POST", w)
		return w
	}
	// now = Thu 15 Oct 08:00Z = 09:00 BST; working hours 09:00-17:30 local
	w := post(map[string]any{"durationMin": 120, "from": "2026-10-15T08:00:00Z", "to": "2026-10-16T00:00:00Z"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var ft struct {
		Slots []struct {
			Start, End, Reason string
			Best               bool
		}
		Closest []map[string]any
	}
	json.Unmarshal(w.Body.Bytes(), &ft)
	if len(ft.Slots) != 2 || ft.Slots[0].Start != "2026-10-15T09:45:00Z" || ft.Slots[0].End != "2026-10-15T11:45:00Z" || !ft.Slots[0].Best ||
		ft.Slots[0].Reason != "Between two meetings" || ft.Slots[1].Start != "2026-10-15T13:00:00Z" || ft.Slots[1].Best || ft.Slots[1].Reason != "After Lunch" {
		t.Fatalf("%s", w.Body.String())
	}
	// a hidden calendar's event (E4 10:00-11:00Z) must not block, and declined/non-busy ones neither
	// 4 h does not fit: closest = first fit next week-day + the longest shorter gap
	w = post(map[string]any{"durationMin": 240, "from": "2026-10-15T08:00:00Z", "to": "2026-10-16T00:00:00Z"})
	json.Unmarshal(w.Body.Bytes(), &ft)
	if len(ft.Slots) != 0 || len(ft.Closest) != 2 || ft.Closest[0]["kind"] != "later" || ft.Closest[0]["start"] != "2026-10-16T08:00:00Z" ||
		ft.Closest[1]["kind"] != "shorter" || ft.Closest[1]["durationMin"] != float64(210) {
		t.Fatalf("%s", w.Body.String())
	}
	// ignoring working hours opens the evening
	w = post(map[string]any{"durationMin": 240, "from": "2026-10-15T08:00:00Z", "to": "2026-10-16T00:00:00Z", "ignoreWorkingHours": true})
	json.Unmarshal(w.Body.Bytes(), &ft)
	if len(ft.Slots) == 0 {
		t.Fatalf("%s", w.Body.String())
	}
	// day part
	w = post(map[string]any{"durationMin": 60, "from": "2026-10-15T08:00:00Z", "to": "2026-10-16T00:00:00Z", "dayPart": "afternoon", "limit": 1})
	json.Unmarshal(w.Body.Bytes(), &ft)
	if len(ft.Slots) != 1 || ft.Slots[0].Start < "2026-10-15T11:00:00Z" {
		t.Fatalf("%s", w.Body.String())
	}
	for _, bad := range []map[string]any{
		{"durationMin": 1, "from": "2026-10-15T08:00:00Z", "to": "2026-10-16T00:00:00Z"},
		{"durationMin": 60, "from": "2026-10-16T08:00:00Z", "to": "2026-10-15T00:00:00Z"},
		{"durationMin": 60, "from": "2026-10-15T08:00:00Z", "to": "2027-10-16T00:00:00Z"},
		{"durationMin": 60, "from": "x", "to": "y"},
		{"durationMin": 60, "from": "2026-10-15T08:00:00Z", "to": "2026-10-16T00:00:00Z", "dayPart": "dawn"},
	} {
		if w := post(bad); w.Code != 400 {
			t.Fatalf("%v: %d", bad, w.Code)
		}
	}
	if w := e.req("POST", "/v1/freetime", "", map[string]any{}); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestQuickAddAndPeopleHTTP(t *testing.T) {
	e := newReadEnv(t)
	doc, _ := openapi3.NewLoader().LoadFromFile("../../../api/openapi.yaml")
	w := e.req("POST", "/v1/quick-add", e.read, map[string]any{"text": "lunch with Boss tomorrow 1pm at Kōya"})
	checkSchema(t, doc, "/v1/quick-add", "POST", w)
	var d struct {
		Source string
		Event  struct {
			CalendarID, Title, Location string
			Attendees                   []string
			When                        struct{ Start, TZID string }
		}
		Ambiguities []any
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	if w.Code != 200 || d.Source != "rules" || d.Event.Title != "Lunch with Boss" || d.Event.When.Start != "2026-10-16T13:00:00" || d.Event.When.TZID != "Europe/London" ||
		d.Event.Location != "Kōya" || len(d.Event.Attendees) != 1 || d.Event.Attendees[0] != "boss@x.test" || len(d.Ambiguities) != 0 || d.Event.CalendarID == "" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, bad := range []map[string]any{{"text": ""}, {"text": "x", "now": "yesterday"}, {"text": "x", "tzid": "Mars/Base"}} {
		w := e.req("POST", "/v1/quick-add", e.read, bad)
		checkSchema(t, doc, "/v1/quick-add", "POST", w)
		if w.Code != 400 {
			t.Fatalf("%v: %d %s", bad, w.Code, w.Body.String())
		}
	}
	// nothing is ever created
	var n int
	e.d.R.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n)
	if n != 4 {
		t.Fatalf("quick-add wrote something: %d events", n)
	}
	w = e.get("/v1/people?q=bos", e.read)
	checkSchema(t, doc, "/v1/people", "GET", w)
	if !strings.Contains(w.Body.String(), "boss@x.test") {
		t.Fatal(w.Body.String())
	}
	w = e.get("/v1/people?q=", e.read)
	checkSchema(t, doc, "/v1/people", "GET", w)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestAutomationsHTTP(t *testing.T) {
	e := newReadEnv(t)
	doc, _ := openapi3.NewLoader().LoadFromFile("../../../api/openapi.yaml")
	_, admin, _ := e.s.Auth.Create(context.Background(), "admin", "device", authn.Admin)
	v, _ := vault.New([]byte("0123456789abcdef0123456789abcdef"))
	e.s.Automations = &automate.Service{DB: e.d, Vault: v, Store: e.s.Store, Sleep: func(context.Context, time.Duration) error { return nil }}
	e.s.Store = nil
	e.h = e.s.Handler()
	var hits int
	var sig string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; sig = r.Header.Get("X-Calendar-Signature") }))
	defer target.Close()
	chk := func(tmpl, method string, w *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		checkSchema(t, doc, tmpl, method, w)
		return w
	}
	if w := e.req("GET", "/v1/automations", e.write, nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
	body := map[string]any{"name": "ping", "trigger": map[string]any{"type": "cron", "expr": "0 7 * * *"}, "action": map[string]any{"type": "webhook", "url": target.URL, "secret": "shh"}}
	w := chk("/v1/automations", "POST", e.req("POST", "/v1/automations", admin, body))
	if w.Code != 201 || strings.Contains(w.Body.String(), "shh") || !strings.Contains(w.Body.String(), `"secretSet":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	var a map[string]any
	json.Unmarshal(w.Body.Bytes(), &a)
	id := a["id"].(string)
	body["trigger"] = map[string]any{"type": "cron", "expr": "99 7 * * *"}
	w = chk("/v1/automations", "POST", e.req("POST", "/v1/automations", admin, body))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "trigger.expr") {
		t.Fatal(w.Code, w.Body.String())
	}
	chk("/v1/automations", "GET", e.req("GET", "/v1/automations", admin, nil))
	chk("/v1/automations/{id}", "GET", e.req("GET", "/v1/automations/"+id, admin, nil))
	w = chk("/v1/automations/{id}/test", "POST", e.req("POST", "/v1/automations/"+id+"/test", admin, nil))
	if w.Code != 200 || hits != 1 || !strings.HasPrefix(sig, "sha256=") || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatal(w.Code, hits, sig, w.Body.String())
	}
	w = chk("/v1/automations/{id}/runs", "GET", e.req("GET", "/v1/automations/"+id+"/runs", admin, nil))
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatal(w.Body.String())
	}
	w = chk("/v1/automations/{id}", "PATCH", e.req("PATCH", "/v1/automations/"+id, admin, map[string]any{"enabled": false}))
	if !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal(w.Body.String())
	}
	chk("/v1/automations/{id}", "PATCH", e.req("PATCH", "/v1/automations/zz", admin, map[string]any{"enabled": false}))
	if w := chk("/v1/automations/{id}", "DELETE", e.req("DELETE", "/v1/automations/"+id, admin, nil)); w.Code != 204 {
		t.Fatal(w.Code)
	}
	chk("/v1/automations/{id}", "DELETE", e.req("DELETE", "/v1/automations/"+id, admin, nil))
	chk("/v1/automations/{id}/runs", "GET", e.req("GET", "/v1/automations/"+id+"/runs", admin, nil))
}

func TestAgendaItemCarriesOrganizer(t *testing.T) {
	e := newReadEnv(t)
	doc, _ := openapi3.NewLoader().LoadFromFile("../../../api/openapi.yaml")
	e.d.W.Exec(`UPDATE events SET is_organizer=1, organizer_email='a@x.test', organizer_name='Alex' WHERE id='E2'`)
	w := e.get("/v1/agenda?"+q15, e.read)
	checkSchema(t, doc, "/v1/agenda", "GET", w)
	var ag struct {
		Items []struct {
			Title     string
			Organizer *struct {
				Email, Name string
				IsSelf      bool
			}
		}
	}
	json.Unmarshal(w.Body.Bytes(), &ag)
	seen := map[string]bool{}
	for _, it := range ag.Items {
		switch it.Title {
		case "Standup":
			seen["standup"] = it.Organizer != nil && it.Organizer.Email == "boss@x.test" && it.Organizer.Name == "Boss" && !it.Organizer.IsSelf
		case "Lunch":
			seen["lunch"] = it.Organizer != nil && it.Organizer.IsSelf && it.Organizer.Email == "a@x.test"
		case "Birthday":
			seen["birthday"] = it.Organizer == nil // unknown organizer: field absent
		}
	}
	if !seen["standup"] || !seen["lunch"] || !seen["birthday"] {
		t.Fatalf("%v\n%s", seen, w.Body.String())
	}
	// event detail agrees and still validates
	w = e.get("/v1/events/E1", e.read)
	checkSchema(t, doc, "/v1/events/{id}", "GET", w)
	if !strings.Contains(w.Body.String(), `"organizer":{"email":"boss@x.test","name":"Boss"}`) {
		t.Fatal(w.Body.String())
	}
}

func TestLunchBreakShapesFreeTime(t *testing.T) {
	e := newReadEnv(t)
	doc, _ := openapi3.NewLoader().LoadFromFile("../../../api/openapi.yaml")
	w := e.get("/v1/settings", e.read)
	checkSchema(t, doc, "/v1/settings", "GET", w)
	if !strings.Contains(w.Body.String(), `"lunchBreak":{"enabled":false,"start":"12:00","end":"13:00"}`) {
		t.Fatal("default must be off:", w.Body.String())
	}
	free := func(extra map[string]any) []string {
		body := map[string]any{"durationMin": 60, "from": "2026-10-15T08:00:00Z", "to": "2026-10-16T00:00:00Z", "limit": 10}
		for k, v := range extra {
			body[k] = v
		}
		w := e.req("POST", "/v1/freetime", e.read, body)
		var ft struct{ Slots []struct{ Start, End string } }
		json.Unmarshal(w.Body.Bytes(), &ft)
		var out []string
		for _, s := range ft.Slots {
			out = append(out, s.Start+"/"+s.End)
		}
		return out
	}
	// off: the gap 09:45Z-12:00Z (before the 13:00 local lunch event at 12:00Z) is one slot starting 09:45Z
	off := strings.Join(free(nil), " ")
	if !strings.Contains(off, "2026-10-15T09:45:00Z/2026-10-15T10:45:00Z") {
		t.Fatal(off)
	}
	put := func(lb any) *httptest.ResponseRecorder {
		w := e.req("PUT", "/v1/settings", e.write, map[string]any{"weekStart": 1, "workingHours": map[string]any{"start": "09:00", "end": "17:30", "days": []int{1, 2, 3, 4, 5}}, "lunchBreak": lb})
		checkSchema(t, doc, "/v1/settings", "PUT", w)
		return w
	}
	if w := put(map[string]any{"enabled": true, "start": "11:00", "end": "13:00"}); w.Code != 200 || !strings.Contains(w.Body.String(), `"lunchBreak":{"enabled":true,"start":"11:00","end":"13:00"}`) {
		t.Fatal(w.Code, w.Body.String())
	}
	// lunch 11:00-13:00 local = 10:00Z-12:00Z: nothing may start inside it
	on := free(nil)
	for _, s := range on {
		st := strings.SplitN(s, "/", 2)[0]
		en := strings.SplitN(s, "/", 2)[1]
		if st < "2026-10-15T12:00:00Z" && en > "2026-10-15T10:00:00Z" {
			t.Fatalf("slot %s overlaps lunch: %v", s, on)
		}
	}
	if len(on) == 0 {
		t.Fatal("no slots at all")
	}
	if got := free(map[string]any{"ignoreWorkingHours": true, "durationMin": 120, "from": "2026-10-15T10:00:00Z", "to": "2026-10-15T12:00:00Z"}); len(got) != 0 && got[0] != "" {
		// the standup sits at 09:30Z; 10:00Z-12:00Z is free, so ignoring working hours lifts the lunch window too
		if got[0] != "2026-10-15T10:00:00Z/2026-10-15T12:00:00Z" {
			t.Fatal(got)
		}
	} else {
		t.Fatal("ignoreWorkingHours should lift the lunch window:", got)
	}
	// omitted = unchanged; bad values are refused
	w = e.req("PUT", "/v1/settings", e.write, map[string]any{"weekStart": 1, "workingHours": map[string]any{"start": "09:00", "end": "17:30", "days": []int{1}}})
	if !strings.Contains(w.Body.String(), `"lunchBreak":{"enabled":true`) {
		t.Fatal("omitted lunchBreak was reset:", w.Body.String())
	}
	for _, bad := range []map[string]any{{"enabled": true, "start": "13:00", "end": "12:00"}, {"enabled": true, "start": "noon", "end": "13:00"}} {
		if w := put(bad); w.Code != 400 {
			t.Fatalf("%v: %d", bad, w.Code)
		}
	}
	if w := put(map[string]any{"enabled": false, "start": "11:00", "end": "13:00"}); !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal(w.Body.String())
	}
	if !strings.Contains(strings.Join(free(nil), " "), "2026-10-15T09:45:00Z/2026-10-15T10:45:00Z") {
		t.Fatal("disabled lunch still applied")
	}
}

func TestAllDayTimeSetting(t *testing.T) {
	e := newReadEnv(t)
	doc, _ := openapi3.NewLoader().LoadFromFile("../../../api/openapi.yaml")
	w := e.get("/v1/settings", e.read)
	checkSchema(t, doc, "/v1/settings", "GET", w)
	if !strings.Contains(w.Body.String(), `"allDayTime":"09:00"`) {
		t.Fatal(w.Body.String())
	}
	put := func(ad map[string]any) *httptest.ResponseRecorder {
		w := e.req("PUT", "/v1/settings", e.write, map[string]any{"weekStart": 1, "workingHours": map[string]any{"start": "09:00", "end": "17:30", "days": []int{1}}, "alertDefaults": ad})
		checkSchema(t, doc, "/v1/settings", "PUT", w)
		return w
	}
	if w := put(map[string]any{"timed": 10, "allDay": 1440, "allDayTime": "08:15"}); w.Code != 200 || !strings.Contains(w.Body.String(), `"allDay":1440,"allDayTime":"08:15"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	// an older client that does not know the field keeps the stored time
	if w := put(map[string]any{"timed": 15, "allDay": 2880}); !strings.Contains(w.Body.String(), `"allDayTime":"08:15"`) {
		t.Fatal(w.Body.String())
	}
	if w := put(map[string]any{"timed": 10, "allDay": nil, "allDayTime": "9am"}); w.Code != 400 || !strings.Contains(w.Body.String(), "alertDefaults.allDayTime") {
		t.Fatal(w.Code, w.Body.String())
	}
}
