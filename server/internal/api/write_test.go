package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	gosync "sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oklog/ulid/v2"

	"calendar/server/internal/accounts"
	"calendar/server/internal/agenda"
	"calendar/server/internal/authn"
	"calendar/server/internal/db"
	"calendar/server/internal/events"
	"calendar/server/internal/hubmcp"
	"calendar/server/internal/provider"
	"calendar/server/internal/provider/google"
	"calendar/server/internal/provider/google/gtest"
	"calendar/server/internal/provider/httpx"
	syncer "calendar/server/internal/sync"
	"calendar/server/internal/vault"
)

type wenv struct {
	t           *testing.T
	d           *db.DB
	f           *gtest.Fake
	h           http.Handler
	mgr         *syncer.Manager
	acct        string
	cal         string
	read, write string
	doc         *openapi3.T
	dev         string // device token, write scope
	srv         *Server
	mcpNow      time.Time // zero = real clock
}

type quietSync struct{ *syncer.Manager }

func (quietSync) Nudge(string) {}

func newWEnv(t *testing.T) *wenv {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	v, _ := vault.New([]byte("0123456789abcdef0123456789abcdef"))
	f := gtest.New(t)
	f.AddCal("primary", "Me", "owner")
	acc := &accounts.Service{DB: d, Vault: v}
	prov := google.New(google.Config{BaseURL: f.URL, TokenURL: f.URL + "/token", ClientID: "c", ClientSecret: "s", Refresh: acc.RefreshToken,
		Transport: &httpx.Transport{Sleep: func(context.Context, time.Duration) error { return nil }, Jitter: func() float64 { return 0 }}})
	provs := map[string]provider.Provider{"google": prov}
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	mgr := &syncer.Manager{DB: d, Accounts: acc, Providers: provs, Now: func() time.Time { return now }}
	store := &agenda.Store{DB: d, Now: func() time.Time { return now }}
	svc := &events.Service{DB: d, Accounts: acc, Providers: provs, Sync: quietSync{mgr}, Store: store, Now: func() time.Time { return now }}
	e := &wenv{t: t, d: d, f: f, mgr: mgr}
	hub := &hubmcp.Hub{DB: d, Store: store, Events: svc, Version: "test", Now: func() time.Time {
		if !e.mcpNow.IsZero() {
			return e.mcpNow
		}
		return time.Now()
	}}
	s := &Server{Auth: authn.Store{DB: d}, Store: store, Events: svc, MCP: hub, Log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), Name: "calendard", Version: "test"}
	e.srv, e.h = s, s.Handler()
	_, e.dev, _ = s.Auth.Create(context.Background(), "phone", "device", authn.Write)
	_, e.read, _ = s.Auth.Create(context.Background(), "r", "script", authn.Read)
	_, e.write, _ = s.Auth.Create(context.Background(), "w", "script", authn.Write)
	e.acct = ulid.Make().String()
	sealed, _ := v.Seal([]byte(`{"refresh_token":"rt"}`), []byte(e.acct))
	if _, err := d.W.Exec(`INSERT INTO accounts(id,provider,external_id,email,color,status,secret,created_at) VALUES (?,?,?,?,?,?,?,?)`,
		e.acct, "google", "sub", "me@x.test", "cobalt", "connecting", sealed, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	e.sync()
	e.doc, err = openapi3.NewLoader().LoadFromFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *wenv) sync() {
	if err := e.mgr.SyncAccount(context.Background(), e.acct, syncer.Manual); err != nil {
		e.t.Fatal(err)
	}
	if err := e.d.R.QueryRow(`SELECT id FROM calendars WHERE remote_id='primary'`).Scan(&e.cal); err != nil {
		e.t.Fatal(err)
	}
}

func (e *wenv) do(method, path, tok string, body any, hdr ...string) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Authorization", "Bearer "+tok)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

// check validates a response against the documented schema for its status.
func (e *wenv) check(tmpl, method string, w *httptest.ResponseRecorder) {
	e.t.Helper()
	op := e.doc.Paths.Find(tmpl).GetOperation(method)
	if op == nil {
		e.t.Fatalf("no operation %s %s", method, tmpl)
	}
	ref := op.Responses.Status(w.Code)
	if ref == nil || ref.Value == nil {
		e.t.Fatalf("%s %s: status %d not documented (%s)", method, tmpl, w.Code, w.Body.String())
	}
	if w.Code == 204 {
		return
	}
	mt := ref.Value.Content.Get("application/json")
	var v any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		e.t.Fatal(err, w.Body.String())
	}
	if err := mt.Schema.Value.VisitJSON(v, openapi3.MultiErrors()); err != nil {
		e.t.Errorf("%s %s (%d) violates schema: %v\n%s", method, tmpl, w.Code, err, w.Body.String())
	}
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err, w.Body.String())
	}
	return m
}

func errOf(t *testing.T, w *httptest.ResponseRecorder, status int, code string) map[string]any {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d want %d: %s", w.Code, status, w.Body.String())
	}
	e := decode(t, w)["error"].(map[string]any)
	if e["code"] != code {
		t.Fatalf("code %v want %s", e["code"], code)
	}
	return e
}

func (e *wenv) count(q string, a ...any) int {
	var n int
	if err := e.d.R.QueryRow(q, a...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *wenv) create(title string, hdr ...string) *httptest.ResponseRecorder {
	return e.do("POST", "/v1/events", e.write, map[string]any{"calendarId": e.cal, "title": title,
		"when": map[string]any{"allDay": false, "start": "2026-10-16T13:00:00", "tzid": "Europe/London", "durationMin": 60}}, hdr...)
}

func TestCreateEventHTTP(t *testing.T) {
	e := newWEnv(t)
	errOf(t, e.do("POST", "/v1/events", e.read, map[string]any{}), 403, "forbidden")
	errOf(t, e.do("POST", "/v1/events", "", map[string]any{}), 401, "unauthorized")
	w := e.create("Lunch")
	e.check("/v1/events", "POST", w)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	ev := decode(t, w)
	if ev["title"] != "Lunch" || w.Header().Get("Location") != "/v1/events/"+ev["eventId"].(string) {
		t.Fatalf("%v %v", ev, w.Header())
	}
	// validation errors use the documented envelope with a field
	w = e.do("POST", "/v1/events", e.write, map[string]any{"calendarId": e.cal, "title": "x", "when": map[string]any{"allDay": false, "start": "bad"}})
	e.check("/v1/events", "POST", w)
	if x := errOf(t, w, 400, "validation"); x["field"] != "when.start" {
		t.Fatal(x)
	}
	e.check("/v1/events", "POST", e.do("POST", "/v1/events", e.write, nil))
	errOf(t, e.do("POST", "/v1/events", e.write, nil), 400, "validation")
}

func TestIdempotencyKey(t *testing.T) {
	e := newWEnv(t)
	a := e.create("Once", "Idempotency-Key", "k1")
	b := e.create("Once", "Idempotency-Key", "k1")
	if a.Code != 201 || b.Code != 201 || a.Body.String() != b.Body.String() || b.Header().Get("Idempotent-Replayed") != "true" || a.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("%d %d %q\n%s\n%s", a.Code, b.Code, b.Header(), a.Body, b.Body)
	}
	if e.count(`SELECT COUNT(*) FROM events`) != 1 || len(e.f.IDs("primary")) != 1 || len(e.f.Writes()) != 1 {
		t.Fatal("created more than once")
	}
	x := e.create("Different", "Idempotency-Key", "k1")
	e.check("/v1/events", "POST", x)
	if m := errOf(t, x, 422, "validation"); m["field"] != "Idempotency-Key" {
		t.Fatal(m)
	}
	// the key belongs to the token
	_, other, _ := (&authn.Store{DB: e.d}).Create(context.Background(), "w2", "script", authn.Write)
	c := e.do("POST", "/v1/events", other, map[string]any{"calendarId": e.cal, "title": "Once", "when": map[string]any{"start": "2026-10-16T13:00:00", "tzid": "Europe/London", "durationMin": 60}}, "Idempotency-Key", "k1")
	if c.Code != 201 || c.Header().Get("Idempotent-Replayed") != "" {
		t.Fatal(c.Code, c.Header())
	}
	// failures are not stored: the retry runs again
	bad := e.do("POST", "/v1/events", e.write, map[string]any{"calendarId": e.cal, "title": "x", "when": map[string]any{"start": "no"}}, "Idempotency-Key", "k2")
	if bad.Code != 400 || e.count(`SELECT COUNT(*) FROM idempotency WHERE key='k2'`) != 0 {
		t.Fatal(bad.Code)
	}
}

func TestIdempotencyDoubleTapRunsOnce(t *testing.T) {
	e := newWEnv(t)
	var wg gosync.WaitGroup
	codes := make([]int, 8)
	bodies := make([]string, 8)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := e.create("Tap", "Idempotency-Key", "tap")
			codes[i], bodies[i] = w.Code, w.Body.String()
		}()
	}
	wg.Wait()
	for i := range codes {
		if codes[i] != 201 || bodies[i] != bodies[0] {
			t.Fatalf("%d: %d", i, codes[i])
		}
	}
	if e.count(`SELECT COUNT(*) FROM events`) != 1 || len(e.f.IDs("primary")) != 1 {
		t.Fatal("double tap created duplicates")
	}
}

func TestIdempotencyExpires(t *testing.T) {
	e := newWEnv(t)
	e.create("Once", "Idempotency-Key", "old")
	e.d.W.Exec(`UPDATE idempotency SET created_at = created_at - 90000`)
	w := e.create("Once", "Idempotency-Key", "old")
	if w.Header().Get("Idempotent-Replayed") != "" || w.Code != 201 {
		t.Fatal("expired key must not replay")
	}
	if len(e.f.IDs("primary")) != 1 {
		// same key -> same provider id -> adopted, not duplicated
		t.Fatalf("remote ids: %v", e.f.IDs("primary"))
	}
}

func TestPatchIfMatchAndConflict(t *testing.T) {
	e := newWEnv(t)
	ev := decode(t, e.create("Mine"))
	id, ver := ev["id"].(string), int64(ev["version"].(float64))
	ifm := func(v int64) string { return `"` + i64s(v) + `"` }

	errOf(t, e.do("PATCH", "/v1/events/"+id, e.write, map[string]any{"title": "x"}, "If-Match", "abc"), 400, "validation")
	w := e.do("PATCH", "/v1/events/"+id, e.write, map[string]any{"title": "x"}, "If-Match", ifm(ver-1))
	e.check("/v1/events/{id}", "PATCH", w)
	x := errOf(t, w, 412, "precondition_failed")
	if cur, _ := x["current"].(map[string]any); cur == nil || cur["title"] != "Mine" {
		t.Fatalf("%v", x)
	}
	w = e.do("PATCH", "/v1/events/"+id, e.write, map[string]any{"title": "Renamed", "location": nil}, "If-Match", ifm(ver))
	e.check("/v1/events/{id}", "PATCH", w)
	if w.Code != 200 || decode(t, w)["title"] != "Renamed" {
		t.Fatal(w.Code, w.Body.String())
	}
	ev = decode(t, w)
	// changed at the provider behind our back
	e.f.Edit("primary", e.remoteOf(ev["eventId"].(string)), func(r map[string]any) { r["summary"] = "Elsewhere" })
	w = e.do("PATCH", "/v1/events/"+id, e.write, map[string]any{"title": "Mine again"}, "If-Match", ifm(int64(ev["version"].(float64))))
	e.check("/v1/events/{id}", "PATCH", w)
	x = errOf(t, w, 409, "conflict")
	if cur, _ := x["current"].(map[string]any); cur == nil || cur["title"] != "Elsewhere" {
		t.Fatalf("%v", x)
	}
	errOf(t, e.do("PATCH", "/v1/events/nope", e.write, map[string]any{"title": "x"}), 404, "not_found")
	e.check("/v1/events/{id}", "PATCH", e.do("PATCH", "/v1/events/nope", e.write, map[string]any{"title": "x"}))
}

func i64s(n int64) string { b, _ := json.Marshal(n); return string(b) }

func (e *wenv) remoteOf(eid string) string {
	var r string
	if err := e.d.R.QueryRow(`SELECT remote_id FROM events WHERE id=?`, eid).Scan(&r); err != nil {
		e.t.Fatal(err)
	}
	return r
}

func TestProviderFailureIs502AndAtomic(t *testing.T) {
	e := newWEnv(t)
	ev := decode(t, e.create("Mine"))
	e.f.FailNext("PATCH", "/events/", 403, 1, "")
	w := e.do("PATCH", "/v1/events/"+ev["id"].(string), e.write, map[string]any{"title": "x"})
	e.check("/v1/events/{id}", "PATCH", w)
	errOf(t, w, 502, "provider_error")
	e.f.FailNext("PATCH", "/events/", 429, 9, "60")
	w = e.do("PATCH", "/v1/events/"+ev["id"].(string), e.write, map[string]any{"title": "x"})
	errOf(t, w, 429, "rate_limited")
	if w.Header().Get("Retry-After") != "60" {
		t.Fatal(w.Header())
	}
	got := decode(t, e.do("GET", "/v1/events/"+ev["id"].(string), e.read, nil))
	if got["title"] != "Mine" {
		t.Fatal("changed after failure")
	}
}

func TestDeleteRSVPMeetMoveReminderHTTP(t *testing.T) {
	e := newWEnv(t)
	e.f.AddCal("work", "Work", "owner")
	e.sync()
	ev := decode(t, e.create("Mine"))
	id := ev["id"].(string)

	w := e.do("PUT", "/v1/events/"+id+"/reminder", e.write, map[string]any{"minutesBefore": 20})
	e.check("/v1/events/{id}/reminder", "PUT", w)
	if w.Code != 200 || decode(t, w)["reminderMin"] != float64(20) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = e.do("PUT", "/v1/events/"+id+"/reminder", e.write, map[string]any{"minutesBefore": nil})
	if decode(t, w)["reminderMin"] != nil {
		t.Fatal(w.Body.String())
	}
	e.check("/v1/events/{id}/reminder", "PUT", e.do("PUT", "/v1/events/"+id+"/reminder", e.write, map[string]any{}))
	errOf(t, e.do("PUT", "/v1/events/"+id+"/reminder", e.write, map[string]any{}), 400, "validation")

	w = e.do("POST", "/v1/events/"+id+"/meet", e.write, nil)
	e.check("/v1/events/{id}/meet", "POST", w)
	if w.Code != 200 || decode(t, w)["join"] == nil {
		t.Fatal(w.Code, w.Body.String())
	}
	w = e.do("POST", "/v1/events/"+id+"/meet", e.write, nil)
	e.check("/v1/events/{id}/meet", "POST", w)
	errOf(t, w, 403, "forbidden")

	w = e.do("POST", "/v1/events/"+id+"/rsvp", e.write, map[string]any{"response": "accepted"})
	e.check("/v1/events/{id}/rsvp", "POST", w)
	errOf(t, w, 403, "forbidden") // own event, nothing to reply to

	var work string
	e.d.R.QueryRow(`SELECT id FROM calendars WHERE remote_id='work'`).Scan(&work)
	w = e.do("POST", "/v1/events/"+id+"/move", e.write, map[string]any{"calendarId": work})
	e.check("/v1/events/{id}/move", "POST", w)
	if w.Code != 200 || decode(t, w)["calendarId"] != work || decode(t, w)["eventId"] != ev["eventId"] {
		t.Fatal(w.Code, w.Body.String())
	}

	w = e.do("DELETE", "/v1/events/"+id, e.write, nil, "Idempotency-Key", "d1")
	e.check("/v1/events/{id}", "DELETE", w)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	w2 := e.do("DELETE", "/v1/events/"+id, e.write, nil, "Idempotency-Key", "d1")
	if w2.Code != 204 || w2.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatal("delete replay", w2.Code)
	}
	w = e.do("DELETE", "/v1/events/"+id, e.write, nil)
	e.check("/v1/events/{id}", "DELETE", w)
	errOf(t, w, 404, "not_found")
	errOf(t, e.do("GET", "/v1/events/"+id, e.read, nil), 404, "not_found")
}

func TestRecurringScopeQuery(t *testing.T) {
	e := newWEnv(t)
	w := e.do("POST", "/v1/events", e.write, map[string]any{"calendarId": e.cal, "title": "Standup",
		"when": map[string]any{"start": "2026-10-20T10:00:00", "tzid": "Europe/London", "durationMin": 15}, "recurrence": map[string]any{"rrule": "FREQ=WEEKLY;COUNT=4"}})
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	eid := decode(t, w)["eventId"].(string)
	occ := eid + ":" + i64s(time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC).Unix())
	w = e.do("PATCH", "/v1/events/"+occ+"?scope=this", e.write, map[string]any{"title": "Special"})
	e.check("/v1/events/{id}", "PATCH", w)
	if w.Code != 200 || decode(t, w)["title"] != "Special" {
		t.Fatal(w.Code, w.Body.String())
	}
	errOf(t, e.do("PATCH", "/v1/events/"+occ+"?scope=sideways", e.write, map[string]any{"title": "x"}), 400, "validation")
	w = e.do("DELETE", "/v1/events/"+eid+"?scope=all", e.write, nil)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPushRegistrationAndSummaryHTTP(t *testing.T) {
	e := newWEnv(t)
	tok := "ab" + "cdef0123456789abcdef0123456789abcdef0123456789abcdef01234567"
	w := e.do("PUT", "/v1/devices/self/push", e.read, map[string]any{"apnsToken": tok, "environment": "sandbox", "bundleId": "dev.casprine.calendar"})
	e.check("/v1/devices/self/push", "PUT", w)
	if w.Code != 204 || e.count(`SELECT COUNT(*) FROM push_endpoints`) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	// registering again replaces
	e.do("PUT", "/v1/devices/self/push", e.read, map[string]any{"apnsToken": "ff" + tok[2:], "environment": "production"})
	if e.count(`SELECT COUNT(*) FROM push_endpoints`) != 1 || e.count(`SELECT COUNT(*) FROM push_endpoints WHERE env='production'`) != 1 {
		t.Fatal("not replaced")
	}
	for _, bad := range []map[string]any{{"apnsToken": "zz", "environment": "sandbox"}, {"apnsToken": tok, "environment": "moon"}} {
		w = e.do("PUT", "/v1/devices/self/push", e.read, bad)
		e.check("/v1/devices/self/push", "PUT", w)
		errOf(t, w, 400, "validation")
	}
	errOf(t, e.do("PUT", "/v1/devices/self/push", "", nil), 401, "unauthorized")
	w = e.do("DELETE", "/v1/devices/self/push", e.read, nil)
	e.check("/v1/devices/self/push", "DELETE", w)
	if w.Code != 204 || e.count(`SELECT COUNT(*) FROM push_endpoints`) != 0 {
		t.Fatal(w.Code)
	}

	e.f.Put("primary", "a", gtestTimed("Standup", "2026-10-15T10:30:00+01:00", "2026-10-15T10:45:00+01:00"))
	e.f.Put("primary", "b", gtestTimed("Lunch", "2026-10-15T13:00:00+01:00", "2026-10-15T14:00:00+01:00"))
	e.sync()
	w = e.do("GET", "/v1/summary?date=2026-10-15", e.read, nil)
	e.check("/v1/summary", "GET", w)
	m := decode(t, w)
	if m["title"] != "Today: 2 events, first at 10:30" || m["body"] != "Standup, Lunch. Free from 14:00 to 17:30." || m["count"] != float64(2) {
		t.Fatalf("%v", m)
	}
	w = e.do("GET", "/v1/summary?date=nope", e.read, nil)
	e.check("/v1/summary", "GET", w)
	errOf(t, w, 400, "validation")
	// settings round trip
	w = e.do("GET", "/v1/settings", e.read, nil)
	e.check("/v1/settings", "GET", w)
	if ms := decode(t, w)["morningSummary"].(map[string]any); ms["enabled"] != false || ms["time"] != "07:30" {
		t.Fatalf("%v", ms)
	}
	w = e.do("PUT", "/v1/settings", e.write, map[string]any{"weekStart": 1, "defaultCalendarId": nil,
		"workingHours": map[string]any{"start": "09:00", "end": "17:30", "days": []int{1, 2, 3, 4, 5}}, "morningSummary": map[string]any{"enabled": true, "time": "06:45"}})
	e.check("/v1/settings", "PUT", w)
	if ms := decode(t, w)["morningSummary"].(map[string]any); ms["enabled"] != true || ms["time"] != "06:45" {
		t.Fatalf("%s", w.Body.String())
	}
	w = e.do("PUT", "/v1/settings", e.write, map[string]any{"weekStart": 1, "workingHours": map[string]any{"start": "09:00", "end": "17:30", "days": []int{1}}, "morningSummary": map[string]any{"enabled": true, "time": "25:00"}})
	errOf(t, w, 400, "validation")
}

func gtestTimed(s, a, b string) map[string]any { return gtest.Timed(s, a, b) }
