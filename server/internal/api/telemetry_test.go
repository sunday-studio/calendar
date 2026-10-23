package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "calendar/server/internal/agenda/agendatest"
	"calendar/server/internal/telemetry"
	"calendar/server/internal/telemetry/telemetrytest"
)

// TestTelemetryNeverCarriesUserContent drives real handlers with distinctive strings seeded in the
// database and in requests, then asserts none of them appear in anything the hub would export or log:
// spans (attributes, events, status), metrics, OTLP log records and the stdout JSON log.
func TestTelemetryNeverCarriesUserContent(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newReadEnv(t)
	e.s.Log = rec.Logger
	e.h = e.s.Handler()
	const (
		title    = "ZQXTITLE-dentist-7731"
		desc     = "ZQXDESC-private-notes"
		loc      = "ZQXLOC-clinic-street"
		guest    = "zqxguest@priv-guest.test"
		owner    = "zqxowner@priv-owner.test"
		query    = "ZQXQUERY-secret-search"
		badTok   = "cal_ZQXBADTOKEN0123456789abcdef"
		panicMsg = "ZQXPANIC-leaked-title"
	)
	Do(t, e.d, func(s *Seed) {
		s.Account("A9", "cobalt", owner)
		s.Calendar("C9", "A9", "Private", true, "owner", true)
		s.Event(Ev{ID: "E9", Cal: "C9", Title: title, Description: desc, Location: loc, Start: unix("2026-10-15T14:00:00Z"), End: unix("2026-10-15T15:00:00Z"),
			Attendees: [][2]string{{guest, "Zqx Guest"}}, Org: [2]string{owner, "Zqx Owner"}})
	})
	for _, path := range []string{
		"/v1/agenda?" + q15,
		"/v1/events?" + q15 + "&q=" + query,
		"/v1/events/E9",
		"/v1/events/does-not-exist",
		"/v1/calendars",
		"/v1/hello",
		"/health",
		"/nope/ZQXPATH-scanner-" + title,
	} {
		e.get(path, e.read)
	}
	e.get("/v1/agenda?"+q15, badTok) // bad token: 401, and the token must not be exported
	e.get("/v1/events/E9?token="+badTok, "")

	// a handler that panics with content in the panic value goes through the real recoverer + telemetry
	boom := telemetry.HTTP(e.s.recoverer(e.s.logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(panicMsg + " " + guest)
	}))))
	r := httptest.NewRequest("GET", "/v1/events/E9?q="+query, nil)
	r.Header.Set("Authorization", "Bearer "+badTok)
	w := httptest.NewRecorder()
	boom.ServeHTTP(w, r)
	if w.Code != 500 {
		t.Fatal(w.Code)
	}

	dump := rec.Dump(t)
	if !strings.Contains(dump, "GET /v1/events/{id}") || !strings.Contains(dump, "calendar.errors") {
		t.Fatalf("expected telemetry missing:\n%s", dump)
	}
	secrets := []string{title, "ZQXPATH", desc, loc, guest, owner, query, badTok, panicMsg, "ZQX", "priv-guest", "priv-owner", "Zqx",
		"/v1/events/E9", "does-not-exist", e.read, e.write, "Bearer "}
	for _, s := range secrets {
		if strings.Contains(dump, s) {
			t.Errorf("%q leaked into spans/metrics/OTLP logs:\n%s", s, grep(dump, s))
		}
	}
	// the stdout log is the existing format: it must not contain content or tokens either
	// (the recoverer's existing "panic" line keeps its value on stdout only; and a raw path stays there too)
	for _, s := range []string{desc, loc, owner, query, badTok, e.read} {
		if strings.Contains(rec.Stdout.String(), s) {
			t.Errorf("%q leaked into the stdout log:\n%s", s, grep(rec.Stdout.String(), s))
		}
	}
}

func grep(s, sub string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// The four hooks the HTTP layer owns: replay, MCP rate limit, invalid-token throttle, scope denial.
func TestTelemetryHTTPHooks(t *testing.T) {
	t.Run("idempotency replay", func(t *testing.T) {
		rec := telemetrytest.Install(t)
		e := newWEnv(t)
		e.create("Once", "Idempotency-Key", "k1")
		if got := rec.Sum(t, "calendar.idempotency.replays", nil); got != 0 {
			t.Fatalf("replays before replay = %v", got)
		}
		e.create("Once", "Idempotency-Key", "k1")
		if got := rec.Sum(t, "calendar.idempotency.replays", telemetrytest.Attrs{"calendar.op": "create"}); got != 1 {
			t.Fatalf("replays = %v\n%s", got, rec.Dump(t))
		}
	})
	t.Run("mcp rate limited", func(t *testing.T) {
		rec := telemetrytest.Install(t)
		e := newWEnv(t)
		e.srv.Limits = Limits{MCPPerMinute: 2}
		e.h = e.srv.Handler()
		for i := 0; i < 3; i++ {
			mcpPost(e, e.read, `{}`)
		}
		if got := rec.Sum(t, "calendar.mcp.rate_limited", nil); got != 1 {
			t.Fatalf("rate_limited = %v", got)
		}
	})
	t.Run("invalid token throttled", func(t *testing.T) {
		rec := telemetrytest.Install(t)
		e := newWEnv(t)
		for i := 0; i < 200; i++ {
			r := httptest.NewRequest("GET", "/v1/hello", nil)
			r.RemoteAddr = "10.7.7.7:1"
			r.Header.Set("Authorization", "Bearer cal_nope")
			w := httptest.NewRecorder()
			e.h.ServeHTTP(w, r)
			if w.Code == 429 {
				break
			}
		}
		if got := rec.Sum(t, "calendar.auth.throttled", nil); got < 1 {
			t.Fatalf("throttled = %v", got)
		}
	})
	t.Run("scope denied", func(t *testing.T) {
		rec := telemetrytest.Install(t)
		e := newWEnv(t)
		if w := e.do("POST", "/v1/events", e.read, map[string]any{}); w.Code != 403 {
			t.Fatal(w.Code)
		}
		if got := rec.Sum(t, "calendar.auth.requests", telemetrytest.Attrs{"calendar.outcome": "scope_denied", "calendar.token_kind": "script"}); got != 1 {
			t.Fatalf("scope_denied = %v\n%s", got, rec.Dump(t))
		}
	})
}
