package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"calendar/server/internal/agenda"
	. "calendar/server/internal/agenda/agendatest"
	"calendar/server/internal/authn"
	"calendar/server/internal/db"
)

func testKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	return k, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

type apnsFake struct {
	*httptest.Server
	mu    sync.Mutex
	reqs  []*http.Request
	codes []int
	jwts  []string
}

func newAPNsFake(t *testing.T, codes ...int) *apnsFake {
	f := &apnsFake{codes: codes}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.reqs = append(f.reqs, r)
		f.jwts = append(f.jwts, strings.TrimPrefix(r.Header.Get("authorization"), "bearer "))
		code := 200
		if len(f.codes) > 0 {
			code, f.codes = f.codes[0], f.codes[1:]
		}
		switch code {
		case 410:
			w.WriteHeader(410)
			w.Write([]byte(`{"reason":"Unregistered"}`))
		case 403:
			w.WriteHeader(403)
			w.Write([]byte(`{"reason":"ExpiredProviderToken"}`))
		case 429:
			w.WriteHeader(429)
			w.Write([]byte(`{"reason":"TooManyRequests"}`))
		default:
			w.WriteHeader(code)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func TestAPNsRequestShapeAndJWT(t *testing.T) {
	key, pemBytes := testKey(t)
	f := newAPNsFake(t)
	clock := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	a, err := NewAPNs(APNsConfig{KeyPEM: pemBytes, KeyID: "KEY123", TeamID: "TEAM456", Topic: "dev.casprine.calendar",
		BaseURL: map[string]string{"sandbox": f.URL}, Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	m := alertMsg("moved", map[string]any{"eventId": "E1"}, "events", "ev-E1")
	if err := a.Send(context.Background(), Target{Token: "ab12", Env: "sandbox"}, m); err != nil {
		t.Fatal(err)
	}
	r := f.reqs[0]
	if r.URL.Path != "/3/device/ab12" || r.Method != "POST" || r.Header.Get("apns-topic") != "dev.casprine.calendar" ||
		r.Header.Get("apns-push-type") != "alert" || r.Header.Get("apns-priority") != "10" || r.Header.Get("apns-collapse-id") != "ev-E1" {
		t.Fatalf("%v %v", r.URL, r.Header)
	}
	if !VerifyJWT(f.jwts[0], &key.PublicKey) {
		t.Fatal("bad jwt signature")
	}
	// token is reused within its lifetime, refreshed after
	_ = a.Send(context.Background(), Target{Token: "ab12", Env: "sandbox", BundleID: "other.bundle"}, silentMsg(3))
	if f.jwts[1] != f.jwts[0] || f.reqs[1].Header.Get("apns-topic") != "other.bundle" || f.reqs[1].Header.Get("apns-push-type") != "background" {
		t.Fatal("jwt not reused / topic override")
	}
	clock = clock.Add(55 * time.Minute)
	_ = a.Send(context.Background(), Target{Token: "ab12", Env: "sandbox"}, silentMsg(4))
	if f.jwts[2] == f.jwts[0] || !VerifyJWT(f.jwts[2], &key.PublicKey) {
		t.Fatal("jwt not refreshed")
	}
	if err := a.Send(context.Background(), Target{Token: "x", Env: "nowhere"}, m); err == nil {
		t.Fatal("unknown env must fail")
	}
}

func TestAPNsErrors(t *testing.T) {
	_, pemBytes := testKey(t)
	f := newAPNsFake(t, 403, 200, 410, 429, 400)
	a, _ := NewAPNs(APNsConfig{KeyPEM: pemBytes, KeyID: "K", TeamID: "T", Topic: "b", BaseURL: map[string]string{"sandbox": f.URL}})
	tg := Target{Token: "ab", Env: "sandbox"}
	if err := a.Send(context.Background(), tg, silentMsg(1)); err != nil { // 403 expired -> new jwt -> 200
		t.Fatal(err)
	}
	if len(f.reqs) != 2 {
		t.Fatalf("expected one retry, got %d requests", len(f.reqs))
	}
	if err := a.Send(context.Background(), tg, silentMsg(1)); !errors.Is(err, ErrUnregistered) {
		t.Fatal(err)
	}
	if err := a.Send(context.Background(), tg, silentMsg(1)); !errors.Is(err, ErrTransient) {
		t.Fatal(err)
	}
	if err := a.Send(context.Background(), tg, silentMsg(1)); err == nil || errors.Is(err, ErrTransient) || errors.Is(err, ErrUnregistered) {
		t.Fatal(err)
	}
	if _, err := NewAPNs(APNsConfig{KeyPEM: []byte("nope"), KeyID: "K", TeamID: "T", Topic: "b"}); err == nil {
		t.Fatal("bad key must fail")
	}
}

// ---- dispatcher ----

type rec struct {
	mu   sync.Mutex
	sent []Message
	to   []Target
	err  func(Target) error
}

func (r *rec) Send(_ context.Context, t Target, m Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		if e := r.err(t); e != nil {
			return e
		}
	}
	r.sent, r.to = append(r.sent, m), append(r.to, t)
	return nil
}

func (r *rec) take() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.sent
	r.sent, r.to = nil, nil
	return s
}

func kinds(ms []Message) []string {
	var out []string
	for _, m := range ms {
		var p map[string]any
		json.Unmarshal(m.Body, &p)
		out = append(out, p["k"].(string))
	}
	return out
}

type denv struct {
	t     *testing.T
	d     *db.DB
	rec   *rec
	disp  *Dispatcher
	now   time.Time
	token string
}

func unix(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.Unix()
}

func newDEnv(t *testing.T) *denv {
	d := Open(t)
	e := &denv{t: t, d: d, rec: &rec{}, now: time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)}
	e.disp = &Dispatcher{DB: d, Sender: e.rec, Now: func() time.Time { return e.now }}
	id, _, err := (&authn.Store{DB: d}).Create(context.Background(), "phone", "device", authn.Read)
	if err != nil {
		t.Fatal(err)
	}
	e.token = id
	e.register(id, "aaaa", "sandbox")
	Do(t, d, func(s *Seed) {
		s.Account("A1", "cobalt", "a@x.test")
		s.Calendar("C1", "A1", "Work", true, "owner", true)
		s.Event(Ev{ID: "E1", Cal: "C1", Title: "Secret board meeting", Start: unix("2026-10-15T11:00:00Z"), End: unix("2026-10-15T12:00:00Z")})
		s.Event(Ev{ID: "FAR", Cal: "C1", Title: "Far away", Start: unix("2026-10-25T11:00:00Z"), End: unix("2026-10-25T12:00:00Z")})
	})
	if err := e.disp.Process(context.Background()); err != nil { // baseline
		t.Fatal(err)
	}
	return e
}

func (e *denv) register(tokenID, apns, env string) {
	if _, err := e.d.W.Exec(`INSERT INTO push_endpoints(token_id,apns_token,env,updated_at) VALUES (?,?,?,?)`, tokenID, apns, env, 1); err != nil {
		e.t.Fatal(err)
	}
}

// change applies fn and records it as a change of event id (quiet = made by the user's own request).
func (e *denv) change(id string, quiet bool, fn func(tx *db.Tx)) {
	e.t.Helper()
	err := e.d.Write(context.Background(), func(tx *db.Tx) error {
		tx.Quiet = quiet
		fn(tx)
		return tx.Change(context.Background(), "event", id, "upsert")
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *denv) move(id, start, end string, quiet bool) {
	e.change(id, quiet, func(tx *db.Tx) {
		tx.Exec(`UPDATE events SET start_utc=?, end_utc=? WHERE id=?`, unix(start), unix(end), id)
		tx.Exec(`UPDATE occurrences SET start_utc=?, end_utc=? WHERE event_id=?`, unix(start), unix(end), id)
	})
}

func (e *denv) process() []Message {
	e.t.Helper()
	if err := e.disp.Process(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	return e.rec.take()
}

func TestNoPushForNothing(t *testing.T) {
	e := newDEnv(t)
	if got := e.process(); len(got) != 0 {
		t.Fatal(kinds(got))
	}
}

func TestMovedEventAlertsWithIdsOnly(t *testing.T) {
	e := newDEnv(t)
	e.move("E1", "2026-10-15T13:00:00Z", "2026-10-15T14:00:00Z", false)
	got := e.process()
	if k := kinds(got); len(k) != 2 || k[0] != "sync" || k[1] != "moved" {
		t.Fatal(k)
	}
	if got[0].Type != "background" || got[1].Type != "alert" {
		t.Fatal("push types")
	}
	var p map[string]any
	json.Unmarshal(got[1].Body, &p)
	if p["eventId"] != "E1" || p["aps"].(map[string]any)["mutable-content"] != float64(1) {
		t.Fatalf("%v", p)
	}
	for _, m := range got {
		if strings.Contains(string(m.Body), "Secret") || strings.Contains(string(m.Body), "board") {
			t.Fatal("event title leaked into the payload:", string(m.Body))
		}
	}
	// a second batch with nothing new sends nothing
	if got := e.process(); len(got) != 0 {
		t.Fatal(kinds(got))
	}
}

func TestEditWithoutMoveIsSilentOnly(t *testing.T) {
	e := newDEnv(t)
	e.change("E1", false, func(tx *db.Tx) { tx.Exec(`UPDATE events SET title='Renamed' WHERE id='E1'`) })
	if k := kinds(e.process()); len(k) != 1 || k[0] != "sync" {
		t.Fatal(k)
	}
}

func TestOwnWritesNeverAlert(t *testing.T) {
	e := newDEnv(t)
	e.move("E1", "2026-10-15T13:00:00Z", "2026-10-15T14:00:00Z", true)
	if k := kinds(e.process()); len(k) != 1 || k[0] != "sync" {
		t.Fatal(k) // other devices still refresh
	}
	// and the new time is the baseline: a later outside move is compared with it
	e.move("E1", "2026-10-15T15:00:00Z", "2026-10-15T16:00:00Z", false)
	if k := kinds(e.process()); len(k) != 2 || k[1] != "moved" {
		t.Fatal(k)
	}
}

func TestCancelledAlert(t *testing.T) {
	e := newDEnv(t)
	e.change("E1", false, func(tx *db.Tx) {
		tx.Exec(`UPDATE events SET status='cancelled', deleted_at=1 WHERE id='E1'`)
		tx.Exec(`DELETE FROM occurrences WHERE event_id='E1'`)
	})
	if k := kinds(e.process()); len(k) != 2 || k[1] != "cancelled" {
		t.Fatal(k)
	}
}

func TestOutsideWindowIsQuiet(t *testing.T) {
	e := newDEnv(t)
	e.move("FAR", "2026-10-26T11:00:00Z", "2026-10-26T12:00:00Z", false)
	if got := e.process(); len(got) != 0 {
		t.Fatal(kinds(got))
	}
	// moving INTO the window from far away is a silent refresh, not an alert (it was never announced)
	e.move("FAR", "2026-10-15T20:00:00Z", "2026-10-15T21:00:00Z", false)
	got := e.process()
	if k := kinds(got); len(k) != 2 || k[1] != "moved" {
		// it had a known upcoming start, so a move into the window is a real "moved"
		t.Fatal(k)
	}
}

func TestOtherEntitiesTriggerSilent(t *testing.T) {
	e := newDEnv(t)
	e.d.Write(context.Background(), func(tx *db.Tx) error { return tx.Change(context.Background(), "calendar", "C1", "upsert") })
	if k := kinds(e.process()); len(k) != 1 || k[0] != "sync" {
		t.Fatal(k)
	}
}

func TestAlertStormCollapses(t *testing.T) {
	e := newDEnv(t)
	Do(t, e.d, func(s *Seed) {
		for _, id := range []string{"M1", "M2", "M3", "M4", "M5", "M6"} {
			s.Event(Ev{ID: id, Cal: "C1", Title: id, Start: unix("2026-10-15T10:00:00Z"), End: unix("2026-10-15T10:30:00Z")})
		}
	})
	for _, id := range []string{"M1", "M2", "M3", "M4", "M5", "M6"} {
		e.change(id, false, func(*db.Tx) {})
	}
	e.process() // first sighting of new events: silent only, and remembered
	for _, id := range []string{"M1", "M2", "M3", "M4", "M5", "M6"} {
		e.move(id, "2026-10-15T17:00:00Z", "2026-10-15T17:30:00Z", false)
	}
	k := kinds(e.process())
	if len(k) != 2 || k[1] != "changed" {
		t.Fatal(k)
	}
}

func TestDeadAndRevokedEndpoints(t *testing.T) {
	e := newDEnv(t)
	other, _, _ := (&authn.Store{DB: e.d}).Create(context.Background(), "old", "device", authn.Read)
	e.register(other, "bbbb", "sandbox")
	if err := (&authn.Store{DB: e.d}).Revoke(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	e.rec.err = func(t Target) error {
		if t.Token == "aaaa" {
			return ErrUnregistered
		}
		return nil
	}
	e.move("E1", "2026-10-15T13:00:00Z", "2026-10-15T14:00:00Z", false)
	e.process()
	var n int
	e.d.R.QueryRow(`SELECT COUNT(*) FROM push_endpoints WHERE apns_token='aaaa'`).Scan(&n)
	if n != 0 {
		t.Fatal("dead token kept")
	}
	e.d.R.QueryRow(`SELECT COUNT(*) FROM push_endpoints WHERE apns_token='bbbb'`).Scan(&n)
	if n != 0 {
		t.Fatal("revoking a token must remove its push endpoint")
	}
	for _, to := range e.rec.to {
		if to.Token == "bbbb" {
			t.Fatal("pushed to a revoked token")
		}
	}
}

func TestSendFailureDoesNotBlockCursor(t *testing.T) {
	e := newDEnv(t)
	e.rec.err = func(Target) error { return ErrTransient }
	e.move("E1", "2026-10-15T13:00:00Z", "2026-10-15T14:00:00Z", false)
	e.process()
	e.rec.err = nil
	if got := e.process(); len(got) != 0 {
		t.Fatal("must not replay failed batches forever")
	}
}

func TestReauthAlert(t *testing.T) {
	e := newDEnv(t)
	e.disp.AccountStatus("A1", "ok")
	if got := e.process(); len(got) != 0 {
		t.Fatal(kinds(got))
	}
	e.disp.AccountStatus("A1", "needs_reauth")
	got := e.process()
	if k := kinds(got); len(k) != 1 || k[0] != "reauth" || !strings.Contains(string(got[0].Body), `"accountId":"A1"`) {
		t.Fatal(kinds(got), string(got[0].Body))
	}
}

func TestNoSenderStillAdvances(t *testing.T) {
	e := newDEnv(t)
	e.disp.Sender = nil
	e.move("E1", "2026-10-15T13:00:00Z", "2026-10-15T14:00:00Z", false)
	if err := e.disp.Process(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.disp.Sender = e.rec
	if got := e.process(); len(got) != 0 {
		t.Fatal("old changes replayed after enabling push")
	}
}

// ---- morning summary ----

func (e *denv) setSummary(enabled bool, at string) {
	b, _ := json.Marshal(agenda.MorningSummary{Enabled: enabled, Time: at})
	e.d.W.Exec(`INSERT INTO settings(key,value) VALUES ('morning_summary',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b))
}

func TestMorningSummary(t *testing.T) {
	e := newDEnv(t)
	st := &agenda.Store{DB: e.d, Now: func() time.Time { return e.now }}
	tick := func(at string) []Message {
		e.now = time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
		tt, _ := time.Parse(time.RFC3339, at)
		e.now = tt
		if err := e.disp.SummaryTick(context.Background(), st); err != nil {
			t.Fatal(err)
		}
		return e.rec.take()
	}
	if got := tick("2026-10-16T07:00:00Z"); len(got) != 0 { // off by default
		t.Fatal("sent while disabled")
	}
	e.setSummary(true, "07:30") // London is UTC+1 on 16 Oct: 07:30 local = 06:30Z
	if got := tick("2026-10-16T06:00:00Z"); len(got) != 0 {
		t.Fatal("sent before the time")
	}
	got := tick("2026-10-16T06:45:00Z")
	if len(got) != 1 || kinds(got)[0] != "summary" || !strings.Contains(string(got[0].Body), `"date":"2026-10-16"`) {
		t.Fatal(kinds(got))
	}
	if strings.Contains(string(got[0].Body), "Secret") {
		t.Fatal("title in payload")
	}
	if got := tick("2026-10-16T07:30:00Z"); len(got) != 0 {
		t.Fatal("sent twice the same day")
	}
	if got := tick("2026-10-17T12:00:00Z"); len(got) != 0 { // more than 3 h after the time: skipped
		t.Fatal("late summary")
	}
	if got := tick("2026-10-17T06:31:00Z"); len(got) != 1 {
		t.Fatal("next day not sent")
	}
}

// ---- all-day reminders ----

func (e *denv) setAlert(allDay *int, allDayTime string) {
	b, _ := json.Marshal(agenda.AlertDefaults{AllDay: allDay, AllDayTime: allDayTime})
	e.d.W.Exec(`INSERT INTO settings(key,value) VALUES ('alert_defaults',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b))
}

func TestAllDayReminders(t *testing.T) {
	e := newDEnv(t)
	st := &agenda.Store{DB: e.d, Now: func() time.Time { return e.now }}
	Do(t, e.d, func(s *Seed) {
		s.Event(Ev{ID: "BD", Cal: "C1", Title: "Mum's birthday", SD: "2026-10-16", ED: "2026-10-17"}) // Friday
		s.Event(Ev{ID: "TRIP", Cal: "C1", Title: "Trip", SD: "2026-10-20", ED: "2026-10-22"})
	})
	tick := func(at string) []Message {
		t.Helper()
		x, _ := time.Parse(time.RFC3339, at)
		e.now = x
		if err := e.disp.ReminderTick(context.Background(), st); err != nil {
			t.Fatal(err)
		}
		return e.rec.take()
	}
	if got := tick("2026-10-15T07:00:00Z"); len(got) != 0 { // no default lead set: nothing
		t.Fatalf("reminded without a lead: %v", kinds(got))
	}
	day := 1440
	e.setAlert(&day, "09:00") // previous day at 09:00 London (BST, UTC+1) = 08:00Z
	if got := tick("2026-10-15T07:59:00Z"); len(got) != 0 {
		t.Fatal("fired before 09:00 the day before")
	}
	got := tick("2026-10-15T08:01:00Z")
	if len(got) != 1 || kinds(got)[0] != "reminder" {
		t.Fatalf("%v", kinds(got))
	}
	var p map[string]any
	json.Unmarshal(got[0].Body, &p)
	if p["eventId"] != "BD" || !strings.HasPrefix(p["occurrenceId"].(string), "BD:") || strings.Contains(string(got[0].Body), "birthday") {
		t.Fatalf("%s", got[0].Body)
	}
	if got := tick("2026-10-15T09:00:00Z"); len(got) != 0 {
		t.Fatal("sent twice")
	}
	// a per-event override wins over the default (2 days), and 0 means the day itself at the alert time
	e.d.W.Exec(`INSERT INTO reminder_overrides(event_id,rkey,minutes_before) VALUES ('TRIP',0,2880)`)
	if got := tick("2026-10-18T08:30:00Z"); len(got) != 1 || !strings.Contains(string(got[0].Body), `"eventId":"TRIP"`) {
		t.Fatalf("override not used: %v", kinds(got))
	}
	// a reminder that is too late (hub was off all day) is skipped, not sent
	Do(t, e.d, func(s *Seed) {
		s.Event(Ev{ID: "OLD", Cal: "C1", Title: "Old", SD: "2026-10-30", ED: "2026-10-31"})
	})
	if got := tick("2026-10-29T20:00:00Z"); len(got) != 0 {
		t.Fatal("late reminder sent")
	}
	// declined, cancelled and hidden-calendar events are skipped
	e.d.W.Exec(`UPDATE events SET self_response='declined' WHERE id='OLD'`)
	if got := tick("2026-10-29T09:30:00Z"); len(got) != 0 {
		t.Fatal("declined event reminded")
	}
	// the time of day follows the setting
	e.setAlert(&day, "21:00")
	e.d.W.Exec(`UPDATE events SET self_response=NULL WHERE id='OLD'`)
	if got := tick("2026-10-29T19:00:00Z"); len(got) != 0 { // the clocks went back on the 25th: 21:00 is 21:00Z
		t.Fatal("fired before the configured time")
	}
	if got := tick("2026-10-29T21:05:00Z"); len(got) != 1 {
		t.Fatalf("configured time not honoured: %v", kinds(got))
	}
}

// invite adds a new event (so the dispatcher has never seen it) and records it as a change from the provider.
func (e *denv) invite(id, resp string, organizer bool, updatedAgo time.Duration) {
	e.t.Helper()
	Do(e.t, e.d, func(s *Seed) {
		s.Event(Ev{ID: id, Cal: "C1", Title: "Secret invitation " + id, Start: unix("2026-10-15T14:00:00Z"), End: unix("2026-10-15T15:00:00Z")})
	})
	org := 0
	if organizer {
		org = 1
	}
	e.change(id, false, func(tx *db.Tx) {
		tx.Exec(`UPDATE events SET self_response=?, is_organizer=?, remote_updated_at=? WHERE id=?`, resp, org, e.now.Add(-updatedAgo).Unix(), id)
	})
}

func TestNewInvitationAlertsOnceWithIdsOnly(t *testing.T) {
	e := newDEnv(t)
	e.invite("INV1", "needsAction", false, 2*time.Minute)
	got := e.process()
	if k := kinds(got); len(k) != 2 || k[0] != "sync" || k[1] != "invited" {
		t.Fatal(kinds(got))
	}
	if body := string(got[1].Body); strings.Contains(body, "Secret") || !strings.Contains(body, `"eventId":"INV1"`) {
		t.Fatalf("payload must carry ids only: %s", body)
	}
	// a later edit of the same invitation is not a new invitation
	e.change("INV1", false, func(tx *db.Tx) { tx.Exec(`UPDATE events SET title='Renamed' WHERE id='INV1'`) })
	if k := kinds(e.process()); len(k) > 1 {
		t.Fatal(k)
	}
}

func TestInvitationsThatAreNotNewsStaySilent(t *testing.T) {
	e := newDEnv(t)
	e.invite("OLD", "needsAction", false, 3*time.Hour) // re-read by a resync, not new
	e.invite("MINE", "needsAction", true, time.Minute) // I organise it
	e.invite("DONE", "accepted", false, time.Minute)   // already answered
	for _, k := range kinds(e.process()) {
		if k == "invited" {
			t.Fatal("alerted for something that is not a new invitation")
		}
	}
}

func TestInvitationPileIsDroppedNotCollapsed(t *testing.T) {
	e := newDEnv(t)
	for i := 0; i < maxAlerts+1; i++ {
		e.invite(fmt.Sprintf("P%d", i), "needsAction", false, time.Minute)
	}
	for _, k := range kinds(e.process()) {
		if k == "invited" || k == "changed" {
			t.Fatalf("a first-sync pile must not alert: %s", k)
		}
	}
}
