package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"calendar/server/internal/accounts"
	"calendar/server/internal/authn"
	"calendar/server/internal/db"
	"calendar/server/internal/oauth"
	"calendar/server/internal/vault"
)

type fakeIdent struct{ sub, email, name, refresh string }

// fakeGoogle is an httptest stand-in for Google's token + userinfo endpoints.
type fakeGoogle struct {
	*httptest.Server
	mu         sync.Mutex
	codes      map[string]fakeIdent
	challenges map[string]string // code -> PKCE challenge, set by authorize()
	n          int
	scope      string // what the token response says was granted; empty = the calendar scope is included
}

func (f *fakeGoogle) granted() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scope != "" {
		return f.scope
	}
	return "openid email https://www.googleapis.com/auth/calendar"
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	f := &fakeGoogle{codes: map[string]fakeIdent{}, challenges: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		id, ok := f.codes[r.Form.Get("code")]
		ch := f.challenges[r.Form.Get("code")]
		f.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || r.Form.Get("client_secret") != "sekret" || base64.RawURLEncoding.EncodeToString(sum[:]) != ch {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		claims, _ := json.Marshal(map[string]string{"sub": id.sub, "email": id.email, "name": id.name})
		idt := "x." + base64.RawURLEncoding.EncodeToString(claims) + ".y"
		m := map[string]any{"access_token": "at-" + id.sub, "token_type": "Bearer", "expires_in": 3600,
			"scope": f.granted(), "id_token": idt}
		if id.refresh != "" {
			m["refresh_token"] = id.refresh
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m)
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"sub": "u", "email": "u@example.com"})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

// authorize plays the user: reads the PKCE challenge from authURL and returns a code for id.
func (f *fakeGoogle) authorize(t *testing.T, authURL string, id fakeIdent) string {
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("access_type") != "offline" || q.Get("prompt") != "select_account consent" ||
		q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "cid" {
		t.Fatalf("bad auth url: %s", authURL)
	}
	for _, sc := range []string{"https://www.googleapis.com/auth/calendar", "openid", "email"} {
		if !strings.Contains(q.Get("scope"), sc) {
			t.Fatalf("scope %s missing in %s", sc, q.Get("scope"))
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	code := fmt.Sprintf("code%d", f.n)
	f.codes[code] = id
	f.challenges[code] = q.Get("code_challenge")
	return code
}

type env struct {
	t      *testing.T
	s      *Server
	h      http.Handler
	fake   *fakeGoogle
	d      *db.DB
	admin  string
	clock  *time.Time
	logbuf *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	v, _ := vault.New([]byte("0123456789abcdef0123456789abcdef"))
	fake := newFakeGoogle(t)
	g := oauth.NewGoogle(oauth.ClientFile{ClientID: "cid", ClientSecret: "sekret"}, "https://hub.test/auth/google/callback")
	g.SetEndpoints(fake.URL+"/auth", fake.URL+"/token", fake.URL+"/userinfo")
	now := time.Now()
	e := &env{t: t, fake: fake, d: d, clock: &now}
	clock := func() time.Time { return *e.clock }
	svc := &accounts.Service{DB: d, Vault: v, Google: g, Sessions: oauth.Sessions{DB: d, Now: clock}, Now: clock}
	e.logbuf = &bytes.Buffer{}
	e.s = &Server{Auth: authn.Store{DB: d}, Accounts: svc, Log: slog.New(slog.NewTextHandler(e.logbuf, nil)), Name: "calendard", Version: "test"}
	e.h = e.s.Handler()
	_, e.admin, _ = e.s.Auth.Create(context.Background(), "admin", "script", authn.Admin)
	return e
}

func (e *env) req(method, path, token string, body any) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, path, rd)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *env) connect(body map[string]any) (authURL, state string) {
	e.t.Helper()
	w := e.req("POST", "/v1/accounts/connect", e.admin, body)
	if w.Code != 200 {
		e.t.Fatal(w.Code, w.Body.String())
	}
	var out map[string]string
	json.Unmarshal(w.Body.Bytes(), &out)
	return out["authUrl"], out["state"]
}

// callback drives the full flow and returns the redirect Location.
func (e *env) signIn(id fakeIdent, body map[string]any) *url.URL {
	e.t.Helper()
	au, st := e.connect(body)
	code := e.fake.authorize(e.t, au, id)
	return e.callback(st, code)
}

func (e *env) callback(state, code string) *url.URL {
	e.t.Helper()
	w := e.req("GET", "/auth/google/callback?state="+url.QueryEscape(state)+"&code="+url.QueryEscape(code), "", nil)
	if w.Code != 302 {
		e.t.Fatal(w.Code, w.Body.String())
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		e.t.Fatal(err)
	}
	if u.Scheme != "calendar" || u.Host != "connected" {
		e.t.Fatalf("bad redirect %s", u)
	}
	return u
}

func (e *env) count(q string, args ...any) int {
	var n int
	if err := e.d.R.QueryRow(q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

var alice = fakeIdent{"sub-alice", "alice@gmail.com", "Alice A", "rt-alice-SECRET"}

func TestConnectFlowAndAccountAPI(t *testing.T) {
	e := newEnv(t)
	rev0, _ := e.d.Rev(context.Background())
	u := e.signIn(alice, map[string]any{"provider": "google"})
	if u.Query().Get("status") != "connecting" || u.Query().Get("account") == "" || u.Query().Get("error") != "" {
		t.Fatal(u)
	}
	id := u.Query().Get("account")

	w := e.req("GET", "/v1/accounts", e.admin, nil)
	var list struct{ Items []map[string]any }
	json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != 200 || len(list.Items) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	a := list.Items[0]
	if a["id"] != id || a["provider"] != "google" || a["email"] != "alice@gmail.com" || a["displayName"] != "Alice A" ||
		a["color"] != "cobalt" || a["status"] != "syncing" {
		t.Fatalf("%v", a)
	}
	if s := w.Body.String(); strings.Contains(s, "SECRET") || strings.Contains(s, "secret") || strings.Contains(s, "sub-alice") {
		t.Fatal("secret material in response: " + s)
	}
	if rev, _ := e.d.Rev(context.Background()); rev <= rev0 {
		t.Fatal("rev not bumped")
	}
	if e.count(`SELECT COUNT(*) FROM changes WHERE entity='account' AND entity_id=? AND op='upsert'`, id) != 1 {
		t.Fatal("no change row")
	}
	// state is single-use: gone from the table.
	if e.count(`SELECT COUNT(*) FROM oauth_sessions`) != 0 {
		t.Fatal("session not consumed")
	}

	// secret sealed, not plaintext; opens with vault via account id as AAD.
	var blob []byte
	e.d.R.QueryRow(`SELECT secret FROM accounts WHERE id=?`, id).Scan(&blob)
	if bytes.Contains(blob, []byte("rt-alice")) || len(blob) == 0 {
		t.Fatal("refresh token not sealed")
	}
	rt, err := e.s.Accounts.RefreshToken(context.Background(), id)
	if err != nil || rt != "rt-alice-SECRET" {
		t.Fatal(rt, err)
	}

	// GET one, PATCH, validation.
	if w := e.req("GET", "/v1/accounts/"+id, e.admin, nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = e.req("PATCH", "/v1/accounts/"+id, e.admin, map[string]any{"color": "rose", "displayName": "Home"})
	var patched map[string]any
	json.Unmarshal(w.Body.Bytes(), &patched)
	if w.Code != 200 || patched["color"] != "rose" || patched["displayName"] != "Home" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := e.req("PATCH", "/v1/accounts/"+id, e.admin, map[string]any{"color": "neon"}); w.Code != 422 {
		t.Fatal(w.Code)
	}
	if w := e.req("GET", "/v1/accounts/nope", e.admin, nil); w.Code != 404 {
		t.Fatal(w.Code)
	}

	// Scopes: read token may list but not mutate/connect; write may patch only.
	_, rd, _ := e.s.Auth.Create(context.Background(), "r", "device", authn.Read)
	_, wr, _ := e.s.Auth.Create(context.Background(), "w", "device", authn.Write)
	if e.req("GET", "/v1/accounts", rd, nil).Code != 200 ||
		e.req("PATCH", "/v1/accounts/"+id, rd, map[string]any{"color": "ochre"}).Code != 403 ||
		e.req("PATCH", "/v1/accounts/"+id, wr, map[string]any{"color": "ochre"}).Code != 200 ||
		e.req("POST", "/v1/accounts/connect", wr, map[string]any{"provider": "google"}).Code != 403 ||
		e.req("DELETE", "/v1/accounts/"+id, wr, nil).Code != 403 ||
		e.req("GET", "/v1/accounts", "", nil).Code != 401 {
		t.Fatal("scope enforcement")
	}
	if w := e.req("POST", "/v1/accounts/connect", e.admin, map[string]any{"provider": "microsoft"}); w.Code != 422 {
		t.Fatal(w.Code)
	}
}

func TestPaletteAssignment(t *testing.T) {
	e := newEnv(t)
	var colors []string
	for i := 0; i < 3; i++ {
		id := fakeIdent{fmt.Sprint("s", i), fmt.Sprint("u", i, "@x.com"), "", "rt"}
		u := e.signIn(id, map[string]any{"provider": "google"})
		a, _ := e.s.Accounts.Get(context.Background(), u.Query().Get("account"))
		colors = append(colors, a.Color)
	}
	if strings.Join(colors, ",") != "cobalt,rose,ochre" {
		t.Fatal(colors)
	}
}

func TestDuplicateConnectUpdates(t *testing.T) {
	e := newEnv(t)
	u1 := e.signIn(alice, map[string]any{"provider": "google"})
	e.req("PATCH", "/v1/accounts/"+u1.Query().Get("account"), e.admin, map[string]any{"color": "slate"})
	again := alice
	again.refresh, again.email = "rt-new", "alice.new@gmail.com"
	u2 := e.signIn(again, map[string]any{"provider": "google"})
	if u1.Query().Get("account") != u2.Query().Get("account") {
		t.Fatal("different ids")
	}
	if e.count(`SELECT COUNT(*) FROM accounts`) != 1 {
		t.Fatal("duplicated")
	}
	a, _ := e.s.Accounts.Get(context.Background(), u2.Query().Get("account"))
	if a.Email != "alice.new@gmail.com" || a.Color != "slate" {
		t.Fatalf("%+v", a)
	}
	if rt, _ := e.s.Accounts.RefreshToken(context.Background(), a.ID); rt != "rt-new" {
		t.Fatal(rt)
	}
	// Second sign-in without a refresh token keeps the old one.
	noRT := again
	noRT.refresh = ""
	e.signIn(noRT, map[string]any{"provider": "google"})
	if rt, _ := e.s.Accounts.RefreshToken(context.Background(), a.ID); rt != "rt-new" {
		t.Fatal(rt)
	}
	// A brand-new identity without a refresh token is rejected.
	u := e.signIn(fakeIdent{"sub-b", "b@x.com", "", ""}, map[string]any{"provider": "google"})
	if u.Query().Get("error") != "no_refresh_token" || e.count(`SELECT COUNT(*) FROM accounts`) != 1 {
		t.Fatal(u)
	}
}

// Google lets people untick permissions on the consent screen, and Workspace admins can limit what an app may ask for.
// An account that comes back without calendar access can only fail every sync with a 403, so it must not be connected.
func TestAccountWithoutCalendarAccessIsRefused(t *testing.T) {
	e := newEnv(t)
	e.fake.mu.Lock()
	e.fake.scope = "openid email profile"
	e.fake.mu.Unlock()
	u := e.signIn(fakeIdent{"sub-w", "w@work.com", "", "rt-w"}, map[string]any{"provider": "google"})
	if u.Query().Get("error") != "scope_missing" || e.count(`SELECT COUNT(*) FROM accounts`) != 0 {
		t.Fatal(u)
	}
}

func TestReauth(t *testing.T) {
	e := newEnv(t)
	id := e.signIn(alice, map[string]any{"provider": "google"}).Query().Get("account")
	ctx := context.Background()
	if err := e.s.Accounts.MarkNeedsReauth(ctx, id, "invalid_grant"); err != nil {
		t.Fatal(err)
	}
	if a, _ := e.s.Accounts.Get(ctx, id); a.Status != "needs_reauth" || a.StatusDetail != "invalid_grant" {
		t.Fatalf("%+v", a)
	}
	// Wrong identity is rejected and changes nothing.
	w := e.req("POST", "/v1/accounts/"+id+"/reconnect", e.admin, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out map[string]string
	json.Unmarshal(w.Body.Bytes(), &out)
	code := e.fake.authorize(t, out["authUrl"], fakeIdent{"sub-mallory", "m@x.com", "", "rt-m"})
	u := e.callback(out["state"], code)
	if u.Query().Get("error") != "identity_mismatch" {
		t.Fatal(u)
	}
	if a, _ := e.s.Accounts.Get(ctx, id); a.Status != "needs_reauth" {
		t.Fatal("status changed on mismatch")
	}
	if rt, _ := e.s.Accounts.RefreshToken(ctx, id); rt != "rt-alice-SECRET" {
		t.Fatal("secret overwritten on mismatch")
	}
	if e.count(`SELECT COUNT(*) FROM accounts`) != 1 {
		t.Fatal("account created on mismatch")
	}
	// Same identity succeeds -> connecting.
	ok := alice
	ok.refresh = "rt-fresh"
	au, st := e.connect(map[string]any{"provider": "google", "reauthAccountId": id})
	u = e.callback(st, e.fake.authorize(t, au, ok))
	if u.Query().Get("account") != id || u.Query().Get("status") != "connecting" {
		t.Fatal(u)
	}
	a, _ := e.s.Accounts.Get(ctx, id)
	if a.Status != "connecting" || a.StatusDetail != "" {
		t.Fatalf("%+v", a)
	}
	if rt, _ := e.s.Accounts.RefreshToken(ctx, id); rt != "rt-fresh" {
		t.Fatal(rt)
	}
	// Unknown reauth account / account id.
	if w := e.req("POST", "/v1/accounts/connect", e.admin, map[string]any{"provider": "google", "reauthAccountId": "zzz"}); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := e.req("POST", "/v1/accounts/zzz/reconnect", e.admin, nil); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestStateRejected(t *testing.T) {
	e := newEnv(t)
	// unknown state
	if u := e.callback("bogus", "c"); u.Query().Get("error") != "invalid_state" {
		t.Fatal(u)
	}
	// reused state
	au, st := e.connect(map[string]any{"provider": "google"})
	code := e.fake.authorize(t, au, alice)
	if u := e.callback(st, code); u.Query().Get("account") == "" {
		t.Fatal(u)
	}
	if u := e.callback(st, code); u.Query().Get("error") != "invalid_state" {
		t.Fatal(u)
	}
	// expired state
	au, st = e.connect(map[string]any{"provider": "google"})
	code = e.fake.authorize(t, au, fakeIdent{"s2", "b@x.com", "", "rt"})
	*e.clock = e.clock.Add(oauth.SessionTTL + time.Minute)
	if u := e.callback(st, code); u.Query().Get("error") != "invalid_state" {
		t.Fatal(u)
	}
	if e.count(`SELECT COUNT(*) FROM accounts`) != 1 {
		t.Fatal("account created from expired state")
	}
	// provider-side denial passes through and still burns the state
	*e.clock = time.Now()
	_, st = e.connect(map[string]any{"provider": "google"})
	w := e.req("GET", "/auth/google/callback?state="+url.QueryEscape(st)+"&error=access_denied", "", nil)
	if w.Code != 302 || !strings.Contains(w.Header().Get("Location"), "error=access_denied") {
		t.Fatal(w.Code, w.Header())
	}
	if e.count(`SELECT COUNT(*) FROM oauth_sessions WHERE state=?`, st) != 0 {
		t.Fatal("state survived denial")
	}
	// bad code (PKCE/verifier or unknown code) -> exchange_failed
	_, st = e.connect(map[string]any{"provider": "google"})
	if u := e.callback(st, "nocode"); u.Query().Get("error") != "exchange_failed" {
		t.Fatal(u)
	}
}

func TestDeleteCascades(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.signIn(alice, map[string]any{"provider": "google"}).Query().Get("account")
	other := e.signIn(fakeIdent{"s2", "b@x.com", "", "rt"}, map[string]any{"provider": "google"}).Query().Get("account")
	for _, aid := range []string{id, other} {
		cal := "cal-" + aid
		evt := "evt-" + aid
		e.d.W.Exec(`INSERT INTO calendars(id,account_id,remote_id,name,access) VALUES (?,?,?,?,?)`, cal, aid, "r", "Cal", "owner")
		e.d.W.Exec(`INSERT INTO events(id,calendar_id,remote_id,kind,rev) VALUES (?,?,?,?,?)`, evt, cal, "r", "single", 1)
		e.d.W.Exec(`INSERT INTO attendees(event_id,email) VALUES (?,?)`, evt, "a@x.com")
		e.d.W.Exec(`INSERT INTO occurrences(event_id,rkey,calendar_id,all_day,start_utc,end_utc) VALUES (?,?,?,?,?,?)`, evt, 0, cal, 0, 1, 2)
		e.d.W.Exec(`INSERT INTO sync_runs(account_id,started_at) VALUES (?,?)`, aid, 1)
	}
	if w := e.req("DELETE", "/v1/accounts/"+id, e.admin, nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, q := range []string{"accounts", "calendars", "events", "attendees", "occurrences", "sync_runs"} {
		if n := e.count(`SELECT COUNT(*) FROM ` + q); n != 1 {
			t.Fatalf("%s has %d rows, want 1 (only the other account's)", q, n)
		}
	}
	if e.count(`SELECT COUNT(*) FROM changes WHERE entity='account' AND entity_id=? AND op='delete'`, id) != 1 {
		t.Fatal("no delete change row")
	}
	if w := e.req("DELETE", "/v1/accounts/"+id, e.admin, nil); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if _, err := e.s.Accounts.Get(ctx, other); err != nil {
		t.Fatal(err)
	}
}

func TestStatusHelpers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	svc := e.s.Accounts
	id := e.signIn(alice, map[string]any{"provider": "google"}).Query().Get("account")
	rev := func() int64 { r, _ := e.d.Rev(ctx); return r }
	check := func(want string) {
		t.Helper()
		if a, _ := svc.Get(ctx, id); a.Status != want {
			t.Fatalf("status %s want %s", a.Status, want)
		}
	}
	r0 := rev()
	svc.MarkOK(ctx, id)
	check("ok")
	if a, _ := svc.Get(ctx, id); a.LastOKAt == nil || a.LastSyncAt == nil {
		t.Fatal("timestamps")
	}
	if rev() == r0 {
		t.Fatal("status change should bump rev")
	}
	r1 := rev()
	svc.MarkOK(ctx, id) // no transition: no rev bump
	if rev() != r1 {
		t.Fatal("rev bumped without transition")
	}
	svc.MarkError(ctx, id, "boom")
	check("error")
	svc.MarkNeedsReauth(ctx, id, "revoked")
	check("needs_reauth")
	svc.MarkConnecting(ctx, id)
	check("connecting")
	svc.SetDisabled(ctx, id, true)
	check("disabled")
	svc.SetDisabled(ctx, id, false)
	check("ok")
	if err := svc.MarkOK(ctx, "nope"); err != accounts.ErrNotFound {
		t.Fatal(err)
	}
}

func TestNotConfigured(t *testing.T) {
	e := newEnv(t)
	e.s.Accounts.Google = nil
	if w := e.req("POST", "/v1/accounts/connect", e.admin, map[string]any{"provider": "google"}); w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestClientFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "g.json")
	if _, err := oauth.LoadClientFile(p); err == nil {
		t.Fatal("missing file")
	}
	writeFile(t, p, `{"client_id":"a"}`)
	if _, err := oauth.LoadClientFile(p); err == nil {
		t.Fatal("incomplete")
	}
	writeFile(t, p, `{"client_id":"a","client_secret":"b"}`)
	c, err := oauth.LoadClientFile(p)
	if err != nil || c.ClientID != "a" || c.ClientSecret != "b" {
		t.Fatal(c, err)
	}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}
