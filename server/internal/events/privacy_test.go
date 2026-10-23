package events_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oklog/ulid/v2"

	"calendar/server/internal/accounts"
	"calendar/server/internal/agenda"
	"calendar/server/internal/alfather"
	"calendar/server/internal/authn"
	"calendar/server/internal/automate"
	"calendar/server/internal/backup"
	"calendar/server/internal/db"
	"calendar/server/internal/events"
	"calendar/server/internal/hubmcp"
	"calendar/server/internal/provider"
	"calendar/server/internal/provider/google"
	"calendar/server/internal/provider/google/gtest"
	"calendar/server/internal/provider/httpx"
	"calendar/server/internal/push"
	"calendar/server/internal/quickadd"
	syncer "calendar/server/internal/sync"
	"calendar/server/internal/telemetry/telemetrytest"
	"calendar/server/internal/vault"
)

// Distinctive strings seeded into every place user content or secrets can enter. None of them may appear
// in any span, span event, metric attribute, OTLP log record or stdout log line.
var secrets = []string{
	"Zebra-Title-91", "Zebra-Desc-91", "Zebra-Loc-91", "Zebra-Search-91", "Zebra-CalName-91", "Zebra-Quick-91", "Zebra-Token-Name-91",
	"zebra.guest91@example.test", "Zebra Guest91", "zebra.owner91@example.test", "zebra-refresh-91",
	"zebraevent91xyz", "zebracal91xyz", "ZebraHook91", "ZebraQuery91", "ZebraAlf91", "zebradevicetoken91", "ZebraNtfy91",
}

func TestTelemetryPrivacyAcrossEverySignal(t *testing.T) {
	rec := telemetrytest.Install(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	v, _ := vault.New([]byte("0123456789abcdef0123456789abcdef"))
	f := gtest.New(t)
	f.Self = "zebra.owner91@example.test"
	acc := &accounts.Service{DB: d, Vault: v}
	prov := google.New(google.Config{BaseURL: f.URL, TokenURL: f.URL + "/token", ClientID: "c", ClientSecret: "s", Refresh: acc.RefreshToken,
		Transport: &httpx.Transport{Sleep: func(context.Context, time.Duration) error { return nil }, Jitter: func() float64 { return 0 }}})
	provs := map[string]provider.Provider{"google": prov}
	mgr := &syncer.Manager{DB: d, Accounts: acc, Providers: provs, Sleep: func(context.Context, time.Duration) error { return nil }}
	store := &agenda.Store{DB: d}
	svc := &events.Service{DB: d, Accounts: acc, Providers: provs, Sync: mgr, Store: store}
	defer mgr.RegisterGauges()()
	defer d.RegisterGauges()()

	f.AddCal("zebracal91xyz", "Zebra-CalName-91", "owner")
	raw := gtest.Timed("Zebra-Title-91", now.Add(2*time.Hour).Format(time.RFC3339), now.Add(3*time.Hour).Format(time.RFC3339))
	raw["description"], raw["location"] = "Zebra-Desc-91", "Zebra-Loc-91"
	raw["attendees"] = []any{map[string]any{"email": "zebra.guest91@example.test", "displayName": "Zebra Guest91", "responseStatus": "accepted"}}
	f.Put("zebracal91xyz", "zebraevent91xyz", raw)
	accID := ulid.Make().String()
	sealed, _ := v.Seal([]byte(`{"refresh_token":"zebra-refresh-91"}`), []byte(accID))
	if _, err := d.W.Exec(`INSERT INTO accounts(id,provider,external_id,email,display_name,color,status,secret,created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		accID, "google", "sub-zebra", "zebra.owner91@example.test", "Zebra Owner", "cobalt", "connecting", sealed, now.Unix()); err != nil {
		t.Fatal(err)
	}

	// sync, then writes: ok, validation, conflict, provider failure
	if err := mgr.SyncAccount(ctx, accID, syncer.Manual); err != nil {
		t.Fatal(err)
	}
	var calID, evID string
	d.R.QueryRow(`SELECT id FROM calendars WHERE remote_id='zebracal91xyz'`).Scan(&calID)
	d.R.QueryRow(`SELECT id FROM events WHERE remote_id='zebraevent91xyz'`).Scan(&evID)
	ev, err := svc.Create(ctx, events.CreateIn{CalendarID: calID, Title: "Zebra-Title-91", Description: "Zebra-Desc-91", Location: "Zebra-Loc-91",
		Attendees: []string{"zebra.guest91@example.test"}, When: events.When{Start: now.Add(5 * time.Hour).Format("2006-01-02T15:04:05"), TZID: "Europe/London", DurationMin: 30}}, events.Opts{})
	if err != nil {
		t.Fatal(err)
	}
	svc.Create(ctx, events.CreateIn{Title: "Zebra-Title-91"}, events.Opts{})
	f.Edit("zebracal91xyz", "zebraevent91xyz", func(r map[string]any) { r["summary"] = "Zebra-Title-91 edited" })
	stale := int64(1)
	svc.Patch(ctx, evID, "", events.PatchIn{Title: ptr("Zebra-Title-91 mine")}, events.Opts{IfMatch: &stale})
	f.FailNext("PATCH", "/events/", 403, 1, "")
	svc.Patch(ctx, ev.ID, "", events.PatchIn{Title: ptr("Zebra-Title-91 x")}, events.Opts{})
	events.RecordReplay(ctx, "create")
	store.List(ctx, agenda.ListQuery{Q: "Zebra-Search-91"})
	store.List(ctx, agenda.ListQuery{Q: "Zebra"})

	// revoked refresh token: needs_reauth path
	f.Revoke("zebra-refresh-91")
	mgr.SyncAccount(ctx, accID, syncer.Manual)

	// auth
	as := authn.Store{DB: d}
	tid, secret, _ := as.Create(ctx, "Zebra-Token-Name-91", "device", authn.Write)
	as.Verify(ctx, secret)
	as.Verify(ctx, secret+"x")
	secrets = append(secrets, secret)

	// push through the real APNs client against a fake that kills the token, errors, then throttles
	_ = tid
	codes := []int{410, 500, 429}
	apnsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := 200
		if len(codes) > 0 {
			c, codes = codes[0], codes[1:]
		}
		w.WriteHeader(c)
		w.Write([]byte(`{"reason":"Unregistered"}`))
	}))
	t.Cleanup(apnsSrv.Close)
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	ap, err := push.NewAPNs(push.APNsConfig{KeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), KeyID: "K", TeamID: "T", Topic: "b",
		BaseURL: map[string]string{"sandbox": apnsSrv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	disp := &push.Dispatcher{DB: d, Sender: ap}
	disp.Process(ctx) // baseline
	for i, dev := range []string{"zebradevicetoken91aaaa", "zebradevicetoken91bbbb", "zebradevicetoken91cccc"} {
		d.W.Exec(`INSERT INTO push_endpoints(token_id,apns_token,env,updated_at) VALUES (?,?,?,?)`, tid, dev, "sandbox", i)
		disp.AccountStatus(accID, "needs_reauth")
		disp.Process(ctx)
	}

	// automations: webhook with a secret path and query, plus an unreachable ntfy
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	t.Cleanup(hook.Close)
	au := &automate.Service{DB: d, Vault: v, Store: store, Sleep: func(context.Context, time.Duration) error { return nil }}
	a1, err := au.Create(ctx, automate.Input{Name: "Zebra-Title-91 automation", Trigger: &automate.Trigger{Type: "event.created"},
		Action: &automate.Action{Type: "webhook", URL: hook.URL + "/ZebraHook91/x?k=ZebraQuery91", Secret: "ZebraNtfy91"}})
	if err != nil {
		t.Fatal(err)
	}
	au.Test(ctx, a1.ID)
	dead := httptest.NewServer(http.NotFoundHandler())
	dURL := dead.URL
	dead.Close()
	a2, _ := au.Create(ctx, automate.Input{Name: "n", Trigger: &automate.Trigger{Type: "event.created"}, Action: &automate.Action{Type: "webhook", URL: dURL + "/ZebraHook91?k=ZebraQuery91"}})
	au.Test(ctx, a2.ID)

	// MCP
	h := &hubmcp.Hub{DB: d, Store: store, Events: svc, Version: "test"}
	inner := h.Handler()
	mts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(authn.WithPrincipal(r.Context(), &authn.Principal{TokenID: tid, Kind: "script", Scope: authn.Write})))
	}))
	t.Cleanup(mts.Close)
	cl := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	sess, err := cl.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: mts.URL, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	for _, c := range []struct {
		n string
		a map[string]any
	}{
		{"search", map[string]any{"query": "Zebra-Search-91"}},
		{"get_event", map[string]any{"id": ev.ID}},
		{"draft_delete_event", map[string]any{"id": ev.ID}},
		{"delete_event", map[string]any{"confirm_token": "cfm_Zebra"}},
	} {
		sess.CallTool(ctx, &mcp.CallToolParams{Name: c.n, Arguments: c.a})
	}

	// quick add: rules, and alfather pointing at an unreachable endpoint (degraded)
	qa := &quickadd.RuleParser{Env: quickadd.HubEnv{DB: d, Store: store}}
	qa.Parse(ctx, quickadd.Request{Text: "Zebra-Quick-91 with zebra.guest91@example.test tomorrow 3pm", Now: now, TZID: "Europe/London"})
	(&alfather.Parser{Cfg: alfather.Config{URL: "http://127.0.0.1:1/ZebraAlf91", Token: "ZebraNtfy91", Timeout: 300 * time.Millisecond}, Fallback: qa,
		Env: quickadd.HubEnv{DB: d, Store: store}}).Parse(ctx, quickadd.Request{Text: "Zebra-Quick-91", Now: now, TZID: "Europe/London"})

	// backup ok + failed
	backup.Create(ctx, backup.Options{DB: d, Key: []byte("0123456789abcdef0123456789abcdef"), Dir: filepath.Join(t.TempDir(), "b")})

	// ---- the assertion ----
	dump := rec.Dump(t) + "\n" + rec.Stdout.String()
	for _, s := range secrets {
		if strings.Contains(dump, s) {
			i := strings.Index(dump, s)
			t.Errorf("%q leaked into telemetry: ...%s...", s, dump[max(0, i-200):min(len(dump), i+120)])
		}
	}
	// and the signals are really there (the test is not vacuous)
	for _, name := range []string{"calendar.sync.runs", "calendar.writes", "calendar.push.sent", "calendar.automation.fires", "calendar.mcp.calls",
		"calendar.quickadd.requests", "calendar.backup.runs", "calendar.auth.requests", "calendar.account.status", "calendar.provider.requests", "calendar.errors"} {
		found := false
		for _, n := range rec.MetricNames(t) {
			found = found || n == name
		}
		if !found {
			t.Errorf("metric %s was never recorded; got %v", name, rec.MetricNames(t))
		}
	}
	t.Logf("spans: %v", rec.SpanNames())
	t.Logf("metrics: %v", rec.MetricNames(t))
}

func ptr(s string) *string { return &s }
