package automate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"calendar/server/internal/agenda"
	. "calendar/server/internal/agenda/agendatest"
	"calendar/server/internal/db"
	"calendar/server/internal/vault"
)

var london, _ = time.LoadLocation("Europe/London")

func TestCronNext(t *testing.T) {
	at := func(s string) time.Time { x, _ := time.ParseInLocation("2006-01-02 15:04", s, london); return x }
	cases := []struct{ expr, after, want string }{
		{"0 7 * * *", "2026-10-15 06:59", "2026-10-15 07:00"},
		{"0 7 * * *", "2026-10-15 07:00", "2026-10-16 07:00"},
		{"*/15 9-10 * * 1-5", "2026-10-15 09:20", "2026-10-15 09:30"},
		{"*/15 9-10 * * 1-5", "2026-10-15 10:45", "2026-10-16 09:00"},
		{"*/15 9-10 * * 1-5", "2026-10-16 10:45", "2026-10-19 09:00"}, // Fri -> Mon
		{"30 8 1 * *", "2026-10-15 00:00", "2026-11-01 08:30"},
		{"0 9 * * mon,wed", "2026-10-15 00:00", "2026-10-19 09:00"},
		{"0 0 29 2 *", "2026-10-15 00:00", "2028-02-29 00:00"},
		{"0 12 15 * 5", "2026-10-13 00:00", "2026-10-15 12:00"}, // day-of-month OR day-of-week
		{"0 7 * * 7", "2026-10-15 00:00", "2026-10-18 07:00"},   // 7 = Sunday
	}
	for _, c := range cases {
		cr, err := ParseCron(c.expr)
		if err != nil {
			t.Fatal(c.expr, err)
		}
		if got := cr.Next(at(c.after), london); !got.Equal(at(c.want)) {
			t.Errorf("%q after %s: got %v want %s", c.expr, c.after, got, c.want)
		}
	}
	for _, bad := range []string{"", "* * * *", "61 * * * *", "* 24 * * *", "*/0 * * * *", "a b c d e", "5-1 * * * *", "* * 0 * *"} {
		if _, err := ParseCron(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}

// ---- engine ----

type hook struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []hreq
	code []int
}

type hreq struct {
	path string
	hdr  http.Header
	body []byte
}

func newHook(t *testing.T, codes ...int) *hook {
	h := &hook{code: codes}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		defer h.mu.Unlock()
		h.reqs = append(h.reqs, hreq{r.URL.Path, r.Header.Clone(), b})
		c := 200
		if len(h.code) > 0 {
			c, h.code = h.code[0], h.code[1:]
		}
		w.WriteHeader(c)
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *hook) take() []hreq { h.mu.Lock(); defer h.mu.Unlock(); r := h.reqs; h.reqs = nil; return r }

type aenv struct {
	t   *testing.T
	d   *db.DB
	svc *Service
	now time.Time
}

func unix(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.Unix()
}

func newAEnv(t *testing.T) *aenv {
	d := Open(t)
	v, _ := vault.New([]byte("0123456789abcdef0123456789abcdef"))
	e := &aenv{t: t, d: d, now: time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)}
	clock := func() time.Time { return e.now }
	e.svc = &Service{DB: d, Vault: v, Store: &agenda.Store{DB: d, Now: clock}, Now: clock,
		Sleep: func(context.Context, time.Duration) error { return nil }}
	Do(t, d, func(s *Seed) {
		s.Account("A1", "cobalt", "a@x.test")
		s.Calendar("C1", "A1", "Work", true, "owner", true)
		s.Calendar("C2", "A1", "Home", true, "owner", false)
		s.Event(Ev{ID: "E1", Cal: "C1", Title: "Design review", Location: "Room 4", Start: unix("2026-10-15T09:00:00Z"), End: unix("2026-10-15T09:45:00Z"), Self: "accepted"})
		s.Event(Ev{ID: "E2", Cal: "C2", Title: "Dentist", Start: unix("2026-10-15T14:00:00Z"), End: unix("2026-10-15T14:30:00Z")})
	})
	if err := e.svc.Tick(context.Background()); err != nil { // baseline
		t.Fatal(err)
	}
	return e
}

func (e *aenv) create(in Input) Automation {
	e.t.Helper()
	a, err := e.svc.Create(context.Background(), in)
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

func (e *aenv) tick() {
	e.t.Helper()
	if err := e.svc.Tick(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func hookAct(h *hook, secret string) *Action {
	return &Action{Type: "webhook", URL: h.URL + "/in", Secret: secret}
}

func TestEventStartFiresOnceWithSignedPayload(t *testing.T) {
	e := newAEnv(t)
	h := newHook(t)
	a := e.create(Input{Name: "heads up", Trigger: &Trigger{Type: "event.start", OffsetMin: -5}, Action: hookAct(h, "s3cret")})
	e.now = time.Date(2026, 10, 15, 8, 54, 0, 0, time.UTC) // 09:00 - 5 min = 08:55 not yet
	e.tick()
	if len(h.take()) != 0 {
		t.Fatal("fired early")
	}
	e.now = time.Date(2026, 10, 15, 8, 56, 0, 0, time.UTC)
	e.tick()
	got := h.take()
	if len(got) != 1 {
		t.Fatalf("%d deliveries", len(got))
	}
	r := got[0]
	var p Payload
	if err := json.Unmarshal(r.body, &p); err != nil {
		t.Fatal(err)
	}
	if p.Trigger != "event.start" || p.Event == nil || p.Event.Title != "Design review" || p.Event.Location != "Room 4" || p.Automation["id"] != a.ID {
		t.Fatalf("%s", r.body)
	}
	ts := r.hdr.Get("X-Calendar-Timestamp")
	var tsn int64
	json.Unmarshal([]byte(ts), &tsn)
	if r.hdr.Get("X-Calendar-Signature") != Sign("s3cret", tsn, r.body) || r.hdr.Get("X-Calendar-Event") != "event.start" || r.hdr.Get("X-Calendar-Delivery") != p.ID {
		t.Fatal("bad headers", r.hdr)
	}
	// once per occurrence
	e.now = e.now.Add(time.Minute)
	e.tick()
	if len(h.take()) != 0 {
		t.Fatal("fired twice")
	}
	// the dentist (14:00) fires later, and not for events that started long ago
	e.now = time.Date(2026, 10, 15, 13, 56, 0, 0, time.UTC)
	e.tick()
	if got := h.take(); len(got) != 1 || !strings.Contains(string(got[0].body), "Dentist") {
		t.Fatalf("%v", got)
	}
	runs, _ := e.svc.Runs(context.Background(), a.ID, 10)
	if len(runs) != 2 || !runs[0].OK || runs[0].Status == nil || *runs[0].Status != 200 {
		t.Fatalf("%+v", runs)
	}
}

func TestStartTriggerFiltersAndCatchUp(t *testing.T) {
	e := newAEnv(t)
	h := newHook(t)
	e.create(Input{Name: "home only", Trigger: &Trigger{Type: "event.start", CalendarID: "C2"}, Action: hookAct(h, "")})
	dis := false
	e.create(Input{Name: "off", Enabled: &dis, Trigger: &Trigger{Type: "event.start"}, Action: hookAct(h, "")})
	e.now = time.Date(2026, 10, 15, 9, 1, 0, 0, time.UTC) // E1 (work) started 1 min ago: not Home, off
	e.tick()
	if len(h.take()) != 0 {
		t.Fatal("wrong calendar / disabled automation fired")
	}
	// the dentist started 8 minutes ago: still inside the catch-up window; 12 minutes ago: too late
	e.now = time.Date(2026, 10, 15, 14, 8, 0, 0, time.UTC)
	e.tick()
	if len(h.take()) != 1 {
		t.Fatal("catch-up within window failed")
	}
	e.d.W.Exec(`DELETE FROM automation_fired`)
	e.now = time.Date(2026, 10, 15, 14, 12, 0, 0, time.UTC)
	e.tick()
	if len(h.take()) != 0 {
		t.Fatal("fired for a start long past")
	}
}

func TestSkipsDeclinedCancelledAndHidden(t *testing.T) {
	e := newAEnv(t)
	h := newHook(t)
	e.create(Input{Name: "all", Trigger: &Trigger{Type: "event.start"}, Action: hookAct(h, "")})
	e.d.W.Exec(`UPDATE events SET self_response='declined' WHERE id='E1'`)
	e.d.W.Exec(`UPDATE events SET status='cancelled' WHERE id='E2'`)
	e.now = time.Date(2026, 10, 15, 9, 0, 30, 0, time.UTC)
	e.tick()
	e.now = time.Date(2026, 10, 15, 14, 0, 30, 0, time.UTC)
	e.tick()
	if got := h.take(); len(got) != 0 {
		t.Fatalf("%d deliveries", len(got))
	}
}

func TestEventCreatedFiresForNewEventsOnly(t *testing.T) {
	e := newAEnv(t)
	h := newHook(t)
	e.create(Input{Name: "new", Trigger: &Trigger{Type: "event.created"}, Action: hookAct(h, "")})
	// edit of an existing event: not a creation
	e.d.Write(context.Background(), func(tx *db.Tx) error {
		tx.Exec(`UPDATE events SET title='Design review v2' WHERE id='E1'`)
		return tx.Change(context.Background(), "event", "E1", "upsert")
	})
	e.tick()
	if len(h.take()) != 0 {
		t.Fatal("edit announced as creation")
	}
	Do(t, e.d, func(s *Seed) {
		s.Event(Ev{ID: "E3", Cal: "C1", Title: "Lunch", Start: unix("2026-10-16T12:00:00Z"), End: unix("2026-10-16T13:00:00Z")})
	})
	e.d.Write(context.Background(), func(tx *db.Tx) error { return tx.Change(context.Background(), "event", "E3", "upsert") })
	e.tick()
	got := h.take()
	if len(got) != 1 || !strings.Contains(string(got[0].body), `"Lunch"`) || !strings.Contains(string(got[0].body), "event.created") {
		t.Fatalf("%v", got)
	}
	e.d.Write(context.Background(), func(tx *db.Tx) error { return tx.Change(context.Background(), "event", "E3", "upsert") })
	e.tick()
	if len(h.take()) != 0 {
		t.Fatal("announced twice")
	}
}

func TestCronDailySummaryToNtfy(t *testing.T) {
	e := newAEnv(t)
	h := newHook(t)
	e.now = time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC) // 06:00 local
	e.create(Input{Name: "morning", Trigger: &Trigger{Type: "cron", Expr: "0 7 * * *"}, Action: &Action{Type: "ntfy", Server: h.URL, Topic: "my-cal", Token: "tk_123"}})
	e.tick()
	if len(h.take()) != 0 {
		t.Fatal("fired before 07:00")
	}
	e.now = time.Date(2026, 10, 15, 6, 0, 30, 0, time.UTC) // 07:00:30 local
	e.tick()
	got := h.take()
	if len(got) != 1 || got[0].path != "/my-cal" || got[0].hdr.Get("Authorization") != "Bearer tk_123" {
		t.Fatalf("%v", got)
	}
	if !strings.HasPrefix(got[0].hdr.Get("Title"), "Today: 2 events, first at 10:00") || !strings.Contains(string(got[0].body), "Design review, Dentist") {
		t.Fatalf("title=%q body=%q", got[0].hdr.Get("Title"), got[0].body)
	}
	e.now = e.now.Add(10 * time.Minute)
	e.tick()
	if len(h.take()) != 0 {
		t.Fatal("fired twice")
	}
	// next day
	e.now = time.Date(2026, 10, 16, 6, 0, 30, 0, time.UTC)
	e.tick()
	if len(h.take()) != 1 {
		t.Fatal("did not fire the next day")
	}
	// the server was off for a day: no replay of the missed time
	e.now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	e.tick()
	if len(h.take()) != 0 {
		t.Fatal("replayed a missed cron time")
	}
}

func TestRetriesAndFailureRecorded(t *testing.T) {
	e := newAEnv(t)
	h := newHook(t, 500, 502, 200)
	a := e.create(Input{Name: "x", Trigger: &Trigger{Type: "event.created"}, Action: hookAct(h, "")})
	run, err := e.svc.Test(context.Background(), a.ID)
	if err != nil || !run.OK || len(h.take()) != 3 {
		t.Fatal(err, run)
	}
	// 4xx is final (no retry), the failure is logged with the reason
	h2 := newHook(t, 404)
	b := e.create(Input{Name: "y", Trigger: &Trigger{Type: "event.created"}, Action: hookAct(h2, "")})
	run, _ = e.svc.Test(context.Background(), b.ID)
	if run.OK || run.Status == nil || *run.Status != 404 || len(h2.take()) != 1 || !strings.Contains(run.Detail, "404") {
		t.Fatalf("%+v", run)
	}
	got, _ := e.svc.Get(context.Background(), b.ID)
	if got.LastRun == nil || got.LastRun.OK {
		t.Fatal("last run not shown")
	}
	// unreachable target
	dead := newHook(t)
	url := dead.URL
	dead.Close()
	c := e.create(Input{Name: "z", Trigger: &Trigger{Type: "event.created"}, Action: &Action{Type: "webhook", URL: url}})
	run, _ = e.svc.Test(context.Background(), c.ID)
	if run.OK || run.Status != nil || run.Detail == "" {
		t.Fatalf("%+v", run)
	}
}

func TestValidationAndSecrets(t *testing.T) {
	e := newAEnv(t)
	h := newHook(t)
	bad := []Input{
		{Name: "", Trigger: &Trigger{Type: "cron", Expr: "0 7 * * *"}, Action: hookAct(h, "")},
		{Name: "a", Action: hookAct(h, "")},
		{Name: "a", Trigger: &Trigger{Type: "cron", Expr: "0 7 * * *"}},
		{Name: "a", Trigger: &Trigger{Type: "sometimes"}, Action: hookAct(h, "")},
		{Name: "a", Trigger: &Trigger{Type: "cron", Expr: "nope"}, Action: hookAct(h, "")},
		{Name: "a", Trigger: &Trigger{Type: "event.start", OffsetMin: -5000}, Action: hookAct(h, "")},
		{Name: "a", Trigger: &Trigger{Type: "cron", Expr: "0 7 * * *"}, Action: &Action{Type: "webhook", URL: "ftp://x"}},
		{Name: "a", Trigger: &Trigger{Type: "cron", Expr: "0 7 * * *"}, Action: &Action{Type: "ntfy", Topic: "bad topic!"}},
		{Name: "a", Trigger: &Trigger{Type: "cron", Expr: "0 7 * * *"}, Action: &Action{Type: "carrier-pigeon"}},
	}
	for i, in := range bad {
		if _, err := e.svc.Create(context.Background(), in); err == nil {
			t.Errorf("case %d accepted", i)
		} else if _, ok := err.(*ValidationError); !ok {
			t.Errorf("case %d: %T", i, err)
		}
	}
	a := e.create(Input{Name: "ok", Trigger: &Trigger{Type: "event.created"}, Action: hookAct(h, "topsecret")})
	if !a.Action.SecretSet {
		t.Fatal("secretSet missing")
	}
	b, _ := json.Marshal(a)
	var stored string
	e.d.R.QueryRow(`SELECT action FROM automations WHERE id=?`, a.ID).Scan(&stored)
	if strings.Contains(string(b), "topsecret") || strings.Contains(stored, "topsecret") {
		t.Fatal("plaintext secret leaked")
	}
	// patch keeps the secret unless replaced; can change URL
	p, err := e.svc.Patch(context.Background(), a.ID, Input{Action: &Action{Type: "webhook", URL: h.URL + "/other"}})
	if err != nil || !p.Action.SecretSet || p.Action.URL != h.URL+"/other" {
		t.Fatal(err, p)
	}
	off := false
	p, _ = e.svc.Patch(context.Background(), a.ID, Input{Enabled: &off})
	if p.Enabled {
		t.Fatal("not disabled")
	}
	if _, err := e.svc.Patch(context.Background(), "nope", Input{Name: "x"}); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := e.svc.Delete(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Delete(context.Background(), a.ID); err != ErrNotFound {
		t.Fatal(err)
	}
	// no vault: secrets are refused instead of stored in clear
	e.svc.Vault = nil
	if _, err := e.svc.Create(context.Background(), Input{Name: "n", Trigger: &Trigger{Type: "event.created"}, Action: hookAct(h, "x")}); err == nil {
		t.Fatal("stored a secret without a vault")
	}
}
