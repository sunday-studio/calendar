package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mcpPost(e *wenv, tok, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+tok)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func TestMCPRateLimitPerToken(t *testing.T) {
	e := newWEnv(t)
	e.srv.Limits = Limits{MCPPerMinute: 5}
	e.h = e.srv.Handler()
	for i := 0; i < 5; i++ {
		if w := mcpPost(e, e.read, `{}`); w.Code == http.StatusTooManyRequests {
			t.Fatalf("call %d limited too early", i)
		}
	}
	w := mcpPost(e, e.read, `{}`)
	errOf(t, w, 429, "rate_limited")
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	// another token has its own allowance, and the REST API is not affected
	if w := mcpPost(e, e.write, `{}`); w.Code == http.StatusTooManyRequests {
		t.Fatal("limit is not per token")
	}
	if w := e.do("GET", "/v1/hello", e.read, nil); w.Code != 200 {
		t.Fatal("REST limited by MCP budget:", w.Code)
	}
}

func TestRateLimiterRefills(t *testing.T) {
	var l rateLimiter
	now := time.Now()
	for i := 0; i < 60; i++ {
		if _, ok := l.allow("k", 60, now); !ok {
			t.Fatalf("burst call %d refused", i)
		}
	}
	wait, ok := l.allow("k", 60, now)
	if ok || wait <= 0 || wait > 2*time.Second {
		t.Fatalf("%v %v", wait, ok)
	}
	if _, ok := l.allow("k", 60, now.Add(1500*time.Millisecond)); !ok {
		t.Fatal("did not refill at 1/s")
	}
}

func TestMCPBodyCap(t *testing.T) {
	e := newWEnv(t)
	e.srv.Limits = Limits{MCPMaxBody: 2048}
	e.h = e.srv.Handler()
	big := `{"jsonrpc":"2.0","id":1,"method":"x","params":{"pad":"` + strings.Repeat("a", 4096) + `"}}`
	w := mcpPost(e, e.read, big)
	errOf(t, w, http.StatusRequestEntityTooLarge, "validation")
	// a chunked body with no length is cut off while reading
	r := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(big)))
	r.ContentLength = -1
	r.Header.Set("Authorization", "Bearer "+e.read)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	if rec.Code == http.StatusOK || rec.Code < 400 {
		t.Fatalf("unbounded body accepted: %d", rec.Code)
	}
}

func TestInvalidTokenPathLimitsAreConfigurable(t *testing.T) {
	e := newWEnv(t)
	e.srv.Limits = Limits{AuthFailLimit: 3, AuthFailWindow: time.Minute}
	e.h = e.srv.Handler()
	try := func(auth string) int {
		r := httptest.NewRequest("GET", "/v1/hello", nil)
		r.RemoteAddr = "10.9.9.9:1"
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < 3; i++ {
		if c := try("Bearer cal_nope"); c != 401 {
			t.Fatalf("%d: %d", i, c)
		}
	}
	if c := try("Bearer cal_nope"); c != 429 {
		t.Fatalf("limit not applied: %d", c)
	}
	if c := try("Bearer " + e.read); c != 200 {
		t.Fatalf("valid token blocked: %d", c)
	}
	// an enormous Authorization header is refused without being hashed, and counts as a failure
	e2 := newWEnv(t)
	e2.srv.Limits = Limits{AuthFailLimit: 2}
	e2.h = e2.srv.Handler()
	huge := "Bearer cal_" + strings.Repeat("x", 100000)
	r := httptest.NewRequest("GET", "/v1/hello", nil)
	r.RemoteAddr = "10.8.8.8:1"
	r.Header.Set("Authorization", huge)
	w := httptest.NewRecorder()
	e2.h.ServeHTTP(w, r)
	if w.Code != 401 || w.Header().Get("Connection") != "close" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
}
