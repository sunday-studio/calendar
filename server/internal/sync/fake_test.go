package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"calendar/server/internal/accounts"
	"calendar/server/internal/db"
	"calendar/server/internal/provider"
	"calendar/server/internal/provider/google"
	"calendar/server/internal/provider/httpx"
	"calendar/server/internal/vault"
)

type fev struct {
	raw     map[string]any
	seq     int
	deleted bool
}

type fcal struct {
	id, summary, role string
	events            map[string]*fev
}

// fakeGoogle is an in-memory Google Calendar speaking just enough REST for the provider.
type fakeGoogle struct {
	*httptest.Server
	mu         gosync.Mutex
	seq        int
	cals       []*fcal
	revoked    map[string]bool
	expireAll  bool // every syncToken request answers 410
	pageSize   int
	failN      int // next N events.list calls fail with failCode
	failCode   int
	retryAfter string
	hits       map[string]int
}

func newFake(t *testing.T) *fakeGoogle {
	f := &fakeGoogle{revoked: map[string]bool{}, pageSize: 2, hits: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", f.token)
	mux.HandleFunc("/users/me/calendarList", f.calendarList)
	mux.HandleFunc("/calendars/", f.events)
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeGoogle) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits["token"]++
	rt := r.Form.Get("refresh_token")
	if f.revoked[rt] || rt == "" {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"access_token": "at-" + rt, "token_type": "Bearer", "expires_in": 3600})
}

// authorised returns false (after writing 401) when the bearer is missing or revoked.
func (f *fakeGoogle) authorised(w http.ResponseWriter, r *http.Request) bool {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer at-")
	if tok == r.Header.Get("Authorization") || f.revoked[tok] {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"code":401,"status":"UNAUTHENTICATED"}}`))
		return false
	}
	return true
}

func (f *fakeGoogle) calendarList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits["calendarList"]++
	if !f.authorised(w, r) {
		return
	}
	items := []any{}
	for i, c := range f.cals {
		items = append(items, map[string]any{"id": c.id, "summary": c.summary, "accessRole": c.role, "primary": i == 0,
			"backgroundColor": "#112233", "timeZone": "Europe/London"})
	}
	json.NewEncoder(w).Encode(map[string]any{"items": items})
}

func (f *fakeGoogle) events(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits["events"]++
	if !f.authorised(w, r) {
		return
	}
	if f.failN > 0 {
		f.failN--
		if f.retryAfter != "" {
			w.Header().Set("Retry-After", f.retryAfter)
		}
		w.WriteHeader(f.failCode)
		w.Write([]byte(`{"error":{"code":` + strconv.Itoa(f.failCode) + `}}`))
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/calendars/"), "/events")
	var cal *fcal
	for _, c := range f.cals {
		if c.id == id {
			cal = c
		}
	}
	if cal == nil {
		w.WriteHeader(404)
		return
	}
	q := r.URL.Query()
	if q.Get("singleEvents") != "false" {
		w.WriteHeader(400)
		return
	}
	st := q.Get("syncToken")
	since := -1
	if st != "" {
		if f.expireAll || !strings.HasPrefix(st, "st-") {
			w.WriteHeader(410)
			w.Write([]byte(`{"error":{"code":410,"message":"Sync token is no longer valid, a full sync is required."}}`))
			return
		}
		since, _ = strconv.Atoi(strings.TrimPrefix(st, "st-"))
		if q.Get("showDeleted") != "true" {
			w.WriteHeader(400)
			return
		}
	}
	var ids []string
	for eid, e := range cal.events {
		if st == "" && !e.deleted || st != "" && e.seq > since {
			ids = append(ids, eid)
		}
	}
	sort.Strings(ids)
	off, _ := strconv.Atoi(q.Get("pageToken"))
	end := off + f.pageSize
	resp := map[string]any{"timeZone": "Europe/London"}
	if end < len(ids) {
		resp["nextPageToken"] = strconv.Itoa(end)
	} else {
		end = len(ids)
		resp["nextSyncToken"] = fmt.Sprintf("st-%d", f.seq)
	}
	items := []any{}
	for _, eid := range ids[off:end] {
		e := cal.events[eid]
		if e.deleted {
			items = append(items, map[string]any{"id": eid, "status": "cancelled"})
		} else {
			items = append(items, e.raw)
		}
	}
	resp["items"] = items
	json.NewEncoder(w).Encode(resp)
}

func (f *fakeGoogle) addCal(id, summary, role string) *fcal {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := &fcal{id: id, summary: summary, role: role, events: map[string]*fev{}}
	f.cals = append(f.cals, c)
	return c
}

// put creates or replaces an event; etag changes on every put.
func (f *fakeGoogle) put(cal, id string, raw map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	raw["id"] = id
	raw["etag"] = fmt.Sprintf(`"e%d"`, f.seq)
	if _, ok := raw["status"]; !ok {
		raw["status"] = "confirmed"
	}
	raw["iCalUID"] = id + "@google.com"
	for _, c := range f.cals {
		if c.id == cal {
			c.events[id] = &fev{raw: raw, seq: f.seq}
		}
	}
}

func (f *fakeGoogle) del(cal, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	for _, c := range f.cals {
		if c.id == cal {
			c.events[id].deleted, c.events[id].seq = true, f.seq
		}
	}
}

// del without a trace: simulates deletions missed while the sync token expired.
func (f *fakeGoogle) forget(cal, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cals {
		if c.id == cal {
			delete(c.events, id)
		}
	}
}

func (f *fakeGoogle) hit(k string) int { f.mu.Lock(); defer f.mu.Unlock(); return f.hits[k] }

func timed(summary, start, end string) map[string]any {
	return map[string]any{"summary": summary,
		"start": map[string]any{"dateTime": start, "timeZone": "Europe/London"},
		"end":   map[string]any{"dateTime": end, "timeZone": "Europe/London"}}
}

func allDay(summary, d1, d2 string) map[string]any {
	return map[string]any{"summary": summary, "start": map[string]any{"date": d1}, "end": map[string]any{"date": d2}}
}

// ---- environment ----

type tenv struct {
	t      *testing.T
	d      *db.DB
	v      *vault.Vault
	acc    *accounts.Service
	fake   *fakeGoogle
	mgr    *Manager
	slept  []time.Duration
	sleptM gosync.Mutex
}

func newTEnv(t *testing.T) *tenv {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	v, _ := vault.New([]byte("0123456789abcdef0123456789abcdef"))
	e := &tenv{t: t, d: d, v: v, fake: newFake(t)}
	e.acc = &accounts.Service{DB: d, Vault: v}
	prov := google.New(google.Config{
		BaseURL: e.fake.URL, TokenURL: e.fake.URL + "/token", ClientID: "cid", ClientSecret: "sec",
		Refresh: e.acc.RefreshToken,
		Transport: &httpx.Transport{
			Sleep:  func(context.Context, time.Duration) error { return nil },
			Jitter: func() float64 { return 0 },
		},
	})
	e.mgr = &Manager{DB: d, Accounts: e.acc, Providers: map[string]provider.Provider{"google": prov},
		Poll: 5 * time.Millisecond, Reconcile: 10 * time.Millisecond,
		Sleep: func(_ context.Context, dd time.Duration) error {
			e.sleptM.Lock()
			e.slept = append(e.slept, dd)
			e.sleptM.Unlock()
			return nil
		},
		Backoff: func(n int) time.Duration { return time.Duration(n) * time.Minute },
	}
	return e
}

func (e *tenv) addAccount(email, refresh string) string {
	e.t.Helper()
	id := ulid.Make().String()
	sealed, err := e.v.Seal([]byte(`{"refresh_token":"`+refresh+`"}`), []byte(id))
	if err != nil {
		e.t.Fatal(err)
	}
	_, err = e.d.W.Exec(`INSERT INTO accounts(id,provider,external_id,email,color,status,secret,created_at) VALUES (?,?,?,?,?,?,?,?)`,
		id, "google", "sub-"+email, email, "cobalt", "connecting", sealed, time.Now().Unix())
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *tenv) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.d.R.QueryRow(q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *tenv) str(q string, args ...any) string {
	e.t.Helper()
	var s *string
	if err := e.d.R.QueryRow(q, args...).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	if s == nil {
		return "<null>"
	}
	return *s
}

func (e *tenv) status(id string) string {
	return e.str(`SELECT status FROM accounts WHERE id=?`, id)
}

func (e *tenv) sync(id string) error {
	return e.mgr.SyncAccount(context.Background(), id, Manual)
}
