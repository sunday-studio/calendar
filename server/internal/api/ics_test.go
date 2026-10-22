package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"calendar/server/internal/accounts"
	"calendar/server/internal/agenda"
	"calendar/server/internal/authn"
	"calendar/server/internal/db"
	"calendar/server/internal/events"
	"calendar/server/internal/provider"
	"calendar/server/internal/provider/ics"
	syncer "calendar/server/internal/sync"
	"calendar/server/internal/vault"
)

const icsSecretToken = "s3cr3t-feed-token-42"

type icsEnv struct {
	t           *testing.T
	d           *db.DB
	h           http.Handler
	mgr         *syncer.Manager
	logs        *bytes.Buffer
	admin, wr   string
	doc         *openapi3.T
	mu          sync.Mutex
	body        string
	feed        *httptest.Server
	feedURLPath string
}

func newICSEnv(t *testing.T, allowPrivate bool) *icsEnv {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	v, _ := vault.New([]byte("0123456789abcdef0123456789abcdef"))
	e := &icsEnv{t: t, d: d, logs: &bytes.Buffer{}, feedURLPath: "/" + icsSecretToken + "/cal.ics"}
	b, _ := os.ReadFile("../../testdata/ics/floating.ics")
	e.body = string(b)
	e.feed = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if !strings.HasSuffix(r.URL.Path, "/cal.ics") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, e.body)
	}))
	t.Cleanup(e.feed.Close)
	acc := &accounts.Service{DB: d, Vault: v}
	p := ics.New(ics.Config{URL: acc.ICSURL, AllowPrivate: allowPrivate})
	acc.ICS = p
	log := slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	provs := map[string]provider.Provider{"ics": p}
	e.mgr = &syncer.Manager{DB: d, Accounts: acc, Providers: provs, Log: log}
	store := &agenda.Store{DB: d}
	svc := &events.Service{DB: d, Accounts: acc, Providers: provs, Sync: quietSync{e.mgr}, Store: store, Log: log}
	s := &Server{Auth: authn.Store{DB: d}, Accounts: acc, Store: store, Events: svc, Log: log, Name: "calendard", Version: "test"}
	e.h = s.Handler()
	_, e.admin, _ = s.Auth.Create(context.Background(), "admin", "script", authn.Admin)
	_, e.wr, _ = s.Auth.Create(context.Background(), "w", "script", authn.Write)
	e.doc, err = openapi3.NewLoader().LoadFromFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *icsEnv) url() string { return e.feed.URL + e.feedURLPath }

func (e *icsEnv) do(method, path, tok string, body any) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	b, _ := json.Marshal(body)
	if body == nil {
		b = nil
	}
	rd = bytes.NewReader(b)
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *icsEnv) check(tmpl, method string, w *httptest.ResponseRecorder) {
	e.t.Helper()
	op := e.doc.Paths.Find(tmpl).GetOperation(method)
	ref := op.Responses.Status(w.Code)
	if ref == nil || ref.Value == nil {
		e.t.Fatalf("%s %s: status %d not documented: %s", method, tmpl, w.Code, w.Body)
	}
	var v any
	json.Unmarshal(w.Body.Bytes(), &v)
	if err := ref.Value.Content.Get("application/json").Schema.Value.VisitJSON(v, openapi3.MultiErrors()); err != nil {
		e.t.Errorf("%s %s (%d) violates schema: %v\n%s", method, tmpl, w.Code, err, w.Body)
	}
}

func (e *icsEnv) accountCount() int {
	var n int
	e.d.R.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&n)
	return n
}

func (e *icsEnv) eventCount() int {
	var n int
	e.d.R.QueryRow(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`).Scan(&n)
	return n
}

func TestAddICSAccountHTTP(t *testing.T) {
	e := newICSEnv(t, true)
	// scope: admin only
	errOf(t, e.do("POST", "/v1/accounts/ics", e.wr, map[string]any{"url": e.url()}), 403, "forbidden")
	errOf(t, e.do("POST", "/v1/accounts/ics", "", map[string]any{"url": e.url()}), 401, "unauthorized")

	w := e.do("POST", "/v1/accounts/ics", e.admin, map[string]any{"url": e.url(), "name": "Holidays", "color": "ochre"})
	e.check("/v1/accounts/ics", "POST", w)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	a := decode(t, w)
	if a["provider"] != "ics" || a["displayName"] != "Holidays" || a["color"] != "ochre" || a["email"] != "127.0.0.1" || a["status"] != "syncing" {
		t.Fatalf("%v", a)
	}
	if strings.Contains(w.Body.String(), icsSecretToken) {
		t.Fatal("URL leaked in response")
	}
	// a second add of the same URL returns the same account
	w2 := e.do("POST", "/v1/accounts/ics", e.admin, map[string]any{"url": e.url()})
	e.check("/v1/accounts/ics", "POST", w2)
	if w2.Code != 200 || decode(t, w2)["id"] != a["id"] || e.accountCount() != 1 {
		t.Fatal(w2.Code, w2.Body)
	}
	// listing and fetching never expose the URL either
	for _, p := range []string{"/v1/accounts", "/v1/accounts/" + a["id"].(string)} {
		if g := e.do("GET", p, e.admin, nil); g.Code != 200 || strings.Contains(g.Body.String(), icsSecretToken) {
			t.Fatal(p, g.Code, g.Body)
		}
	}
	// reconnect has no sign-in for feeds
	rc := e.do("POST", "/v1/accounts/"+a["id"].(string)+"/reconnect", e.admin, nil)
	e.check("/v1/accounts/{id}/reconnect", "POST", rc)
	if x := errOf(t, rc, 422, "validation"); x["field"] != "url" {
		t.Fatal(x)
	}

	// first sync, then read-only everywhere
	if err := e.mgr.SyncAccount(context.Background(), a["id"].(string), syncer.Manual); err != nil {
		t.Fatal(err)
	}
	var cal, ev string
	if err := e.d.R.QueryRow(`SELECT c.id, e.id FROM events e JOIN calendars c ON c.id=e.calendar_id WHERE e.remote_id='float@example.test'`).Scan(&cal, &ev); err != nil {
		t.Fatal(err)
	}
	cl := e.do("GET", "/v1/calendars", e.admin, nil)
	if cl.Code != 200 || !strings.Contains(cl.Body.String(), `"access":"reader"`) {
		t.Fatal(cl.Body)
	}
	ge := e.do("GET", "/v1/events/"+ev, e.admin, nil)
	if ge.Code != 200 {
		t.Fatal(ge.Code, ge.Body)
	}
	g := decode(t, ge)
	if g["canEdit"] != false || g["canRSVP"] != false || g["canAddConference"] != false {
		t.Fatalf("caps: %v", g)
	}
	when := map[string]any{"allDay": false, "start": "2026-10-16T13:00:00", "tzid": "Europe/London", "durationMin": 60}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/v1/events", map[string]any{"calendarId": cal, "title": "x", "when": when}},
		{"PATCH", "/v1/events/" + ev, map[string]any{"title": "renamed"}},
		{"DELETE", "/v1/events/" + ev, nil},
		{"POST", "/v1/events/" + ev + "/rsvp", map[string]any{"response": "accepted"}},
		{"POST", "/v1/events/" + ev + "/meet", nil},
		{"POST", "/v1/events/" + ev + "/move", map[string]any{"calendarId": cal}},
	} {
		w := e.do(c.method, c.path, e.wr, c.body)
		errOf(t, w, 403, "forbidden")
		if strings.Contains(w.Body.String(), icsSecretToken) {
			t.Fatal("leak")
		}
	}
	var title string
	e.d.R.QueryRow(`SELECT title FROM events WHERE id=?`, ev).Scan(&title)
	if title != "Floating" {
		t.Fatal("event modified: ", title)
	}

	// PATCH url: admin only, validated, nothing changes on failure
	id := a["id"].(string)
	errOf(t, e.do("PATCH", "/v1/accounts/"+id, e.wr, map[string]any{"url": e.url()}), 403, "forbidden")
	bad := e.do("PATCH", "/v1/accounts/"+id, e.admin, map[string]any{"url": "file:///etc/passwd"})
	e.check("/v1/accounts/{id}", "PATCH", bad)
	if x := errOf(t, bad, 422, "validation"); x["field"] != "url" {
		t.Fatal(x)
	}
	var ext0, ext1 string
	e.d.R.QueryRow(`SELECT external_id FROM accounts WHERE id=?`, id).Scan(&ext0)
	e.mu.Lock()
	e.feedURLPath = "/rotated-" + icsSecretToken + "/cal.ics"
	e.mu.Unlock()
	ok := e.do("PATCH", "/v1/accounts/"+id, e.admin, map[string]any{"url": e.url(), "displayName": "Renamed"})
	e.check("/v1/accounts/{id}", "PATCH", ok)
	if ok.Code != 200 || decode(t, ok)["displayName"] != "Renamed" || decode(t, ok)["status"] != "syncing" {
		t.Fatal(ok.Code, ok.Body)
	}
	e.d.R.QueryRow(`SELECT external_id FROM accounts WHERE id=?`, id).Scan(&ext1)
	if ext0 == ext1 {
		t.Fatal("external id must follow the url")
	}
	if err := e.mgr.SyncAccount(context.Background(), id, syncer.Manual); err != nil {
		t.Fatal(err)
	}
	if n := e.eventCount(); n != 2 {
		t.Fatal("events after url change", n)
	}
	if strings.Contains(e.logs.String(), icsSecretToken) {
		t.Fatalf("feed token reached the logs:\n%s", e.logs)
	}
}

func TestAddICSAccountValidation(t *testing.T) {
	e := newICSEnv(t, true)
	e.body = "<html>nope</html>"
	cases := []struct {
		name string
		body any
		want string
	}{
		{"not ics", map[string]any{"url": e.url()}, "iCalendar"},
		{"not found", map[string]any{"url": e.feed.URL + "/missing"}, "not found"},
		{"file scheme", map[string]any{"url": "file:///etc/passwd"}, "calendar link"},
		{"empty", map[string]any{"url": ""}, "required"},
		{"junk", map[string]any{"url": "not a url"}, "calendar link"},
	}
	for _, c := range cases {
		w := e.do("POST", "/v1/accounts/ics", e.admin, c.body)
		e.check("/v1/accounts/ics", "POST", w)
		x := errOf(t, w, 422, "validation")
		if x["field"] != "url" || !strings.Contains(fmt.Sprint(x["message"]), c.want) || strings.Contains(w.Body.String(), icsSecretToken) {
			t.Errorf("%s: %v", c.name, x)
		}
	}
	// bad colour is reported on its own field, before any network work
	e.body = "BEGIN:VCALENDAR\nEND:VCALENDAR\n"
	w := e.do("POST", "/v1/accounts/ics", e.admin, map[string]any{"url": e.url(), "color": "teal"})
	if x := errOf(t, w, 422, "validation"); x["field"] != "color" {
		t.Fatal(x)
	}
	if e.accountCount() != 0 {
		t.Fatal("a rejected feed must not be saved")
	}
	// an empty but valid calendar is fine
	if w := e.do("POST", "/v1/accounts/ics", e.admin, map[string]any{"url": e.url()}); w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	// unknown fields are rejected
	errOf(t, e.do("POST", "/v1/accounts/ics", e.admin, map[string]any{"url": e.url(), "token": "x"}), 422, "validation")
}

func TestAddICSAccountBlocksPrivateByDefault(t *testing.T) {
	e := newICSEnv(t, false) // strict SSRF guard
	w := e.do("POST", "/v1/accounts/ics", e.admin, map[string]any{"url": e.url()})
	e.check("/v1/accounts/ics", "POST", w)
	x := errOf(t, w, 422, "validation")
	if x["field"] != "url" || !strings.Contains(fmt.Sprint(x["message"]), "private or local") || e.accountCount() != 0 {
		t.Fatal(x)
	}
	if strings.Contains(w.Body.String(), icsSecretToken) || strings.Contains(e.logs.String(), icsSecretToken) {
		t.Fatal("leak")
	}
}
