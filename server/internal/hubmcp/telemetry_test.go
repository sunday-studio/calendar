package hubmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"calendar/server/internal/agenda"
	. "calendar/server/internal/agenda/agendatest"
	"calendar/server/internal/authn"
	"calendar/server/internal/events"
	"calendar/server/internal/telemetry/telemetrytest"
)

type A = telemetrytest.Attrs

func session(t *testing.T, h *Hub, scope authn.Scope) *mcp.ClientSession {
	t.Helper()
	inner := h.Handler()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(authn.WithPrincipal(r.Context(), &authn.Principal{TokenID: "01TESTTOKEN0000000000000A", Name: "agent", Kind: "script", Scope: scope})))
	}))
	t.Cleanup(ts.Close)
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	s, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTelemetryMCPCallsAndConfirmations(t *testing.T) {
	rec := telemetrytest.Install(t)
	d := Open(t)
	Do(t, d, func(s *Seed) {
		s.Account("A1", "cobalt", "a@x.test")
		s.Calendar("C1", "A1", "Work", true, "owner", true)
		s.Event(Ev{ID: "E1", Cal: "C1", Title: "Quarterly secret", Start: time.Now().Add(time.Hour).Unix(), End: time.Now().Add(2 * time.Hour).Unix()})
	})
	h := &Hub{DB: d, Store: &agenda.Store{DB: d}, Events: &events.Service{}, Version: "test"}
	s := session(t, h, authn.Write)

	call(t, s, "calendars", nil)                                                // ok
	call(t, s, "get_event", map[string]any{"id": "01NOSUCHEVENT0000000000000"}) // denied (not_found)
	call(t, s, "create_event", map[string]any{"confirm_token": ""})             // confirm_required
	m := call(t, s, "draft_delete_event", map[string]any{"id": "E1"})
	tok, _ := m["confirm_token"].(string)
	if tok == "" {
		t.Fatalf("no confirm token: %v", m)
	}
	call(t, s, "delete_event", map[string]any{"confirm_token": tok})           // confirm_required (pending approval)
	call(t, s, "delete_event", map[string]any{"confirm_token": "cfm_UNKNOWN"}) // denied (invalid)

	for outcome, want := range map[string]float64{"ok": 2, "denied": 2, "confirm_required": 2} {
		if got := rec.Sum(t, "calendar.mcp.calls", A{"calendar.outcome": outcome}); got < want-1 {
			t.Errorf("mcp.calls %s = %v, want about %v\n%s", outcome, got, want, rec.Dump(t))
		}
	}
	if rec.Sum(t, "calendar.mcp.calls", A{"calendar.outcome": "confirm_required", "calendar.tool": "delete_event"}) != 1 {
		t.Fatal("pending approval should be confirm_required")
	}
	if rec.Sum(t, "calendar.mcp.calls", A{"calendar.outcome": "denied", "calendar.tool": "get_event"}) != 1 {
		t.Fatal("unknown event should be denied")
	}
	if rec.HistCount(t, "calendar.mcp.call.duration", A{"calendar.tool": "calendars"}) != 1 {
		t.Fatal("duration missing")
	}
	if rec.Sum(t, "calendar.mcp.confirmations", A{"calendar.state": "minted"}) != 1 {
		t.Fatal("mint not counted")
	}
	// approving needs another token; expiry: move the clock past the TTL
	if _, ce := h.Approve(context.Background(), tok, "01OTHERTOKEN00000000000000"); ce != nil {
		t.Fatal(ce)
	}
	if rec.Sum(t, "calendar.mcp.confirmations", A{"calendar.state": "approved"}) != 1 {
		t.Fatal("approval not counted")
	}
	h.Now = func() time.Time { return time.Now().Add(ConfirmTTL + time.Minute) }
	call(t, s, "delete_event", map[string]any{"confirm_token": tok})
	if rec.Sum(t, "calendar.mcp.calls", A{"calendar.outcome": "expired"}) != 1 || rec.Sum(t, "calendar.mcp.confirmations", A{"calendar.state": "expired"}) != 1 {
		t.Fatalf("expiry not counted:\n%s", rec.Dump(t))
	}
	if len(rec.SpansNamed("mcp.tool")) < 6 {
		t.Fatal(rec.SpanNames())
	}
	if rec.Sum(t, "calendar.errors", nil) != 0 {
		t.Fatalf("refusals are outcomes, not errors:\n%s", rec.Dump(t))
	}
	if strings.Contains(rec.Dump(t), "Quarterly secret") || strings.Contains(rec.Dump(t), tok) {
		t.Fatal("event title or confirm token in telemetry")
	}
	RecordRateLimited(context.Background())
	if rec.Sum(t, "calendar.mcp.rate_limited", nil) != 1 {
		t.Fatal("rate limit counter")
	}
}
