package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"calendar/server/internal/authn"
)

func TestSecurityHeadersAndRequestID(t *testing.T) {
	s, logs := setup(t)
	h := s.Handler()
	w := do(h, "GET", "/health", "")
	for k, v := range map[string]string{"X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "Cache-Control": "no-store"} {
		if w.Header().Get(k) != v {
			t.Errorf("%s = %q", k, w.Header().Get(k))
		}
	}
	id := w.Header().Get("X-Request-Id")
	if len(id) != 26 || !strings.Contains(logs.String(), "id="+id) {
		t.Fatalf("request id %q not in logs:\n%s", id, logs.String())
	}
	if id2 := do(h, "GET", "/health", "").Header().Get("X-Request-Id"); id2 == id {
		t.Fatal("request ids repeat")
	}
}

func TestSearchTermsAndTokensStayOutOfLogs(t *testing.T) {
	s, logs := setup(t)
	_, secret, _ := s.Auth.Create(context.Background(), "t", "script", authn.Read)
	do(s.Handler(), "GET", "/v1/events?q=dentist+appointment&from=2026-10-01T00:00:00Z&to=2026-11-01T00:00:00Z&token="+secret, secret)
	out := logs.String()
	if strings.Contains(out, "dentist") || strings.Contains(out, secret) || !strings.Contains(out, "/v1/events") {
		t.Fatalf("log line leaks or is missing:\n%s", out)
	}
}

func TestInvalidTokenFloodIsSlowedButOwnerIsNot(t *testing.T) {
	s, _ := setup(t)
	_, good, _ := s.Auth.Create(context.Background(), "t", "script", authn.Read)
	h := s.Handler()
	attempt := func(tok, ip string) int {
		r := httptest.NewRequest("GET", "/v1/hello", nil)
		r.RemoteAddr = ip + ":1234"
		r.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code == 429 && w.Header().Get("Retry-After") == "" {
			t.Error("429 without Retry-After")
		}
		return w.Code
	}
	for i := 0; i < failLimit; i++ {
		if c := attempt("cal_wrong", "10.0.0.9"); c != 401 {
			t.Fatalf("attempt %d: %d", i, c)
		}
	}
	if c := attempt("cal_wrong", "10.0.0.9"); c != 429 {
		t.Fatalf("flood not slowed: %d", c)
	}
	if c := attempt(good, "10.0.0.9"); c != 200 {
		t.Fatalf("owner locked out from the same address: %d", c)
	}
	if c := attempt("cal_wrong", "10.0.0.10"); c != 401 {
		t.Fatalf("another client affected: %d", c)
	}
	// behind a local proxy the forwarded client is what counts
	r := httptest.NewRequest("GET", "/v1/hello", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "100.64.0.7, 127.0.0.1")
	if k := clientKey(r); k != "100.64.0.7" {
		t.Fatal(k)
	}
	r.RemoteAddr = "10.1.1.1:5555" // not loopback: header is untrusted
	if k := clientKey(r); k != "10.1.1.1" {
		t.Fatal(k)
	}
	// the block expires
	l := &failLimiter{}
	now := time.Now()
	for i := 0; i < failLimit; i++ {
		l.fail("k", now)
	}
	if _, b := l.blocked("k", now); !b {
		t.Fatal("not blocked")
	}
	if _, b := l.blocked("k", now.Add(failWindow+time.Second)); b {
		t.Fatal("still blocked after the window")
	}
}

func TestMetricsAndBackupWarning(t *testing.T) {
	s, _ := setup(t)
	_, rd, _ := s.Auth.Create(context.Background(), "r", "script", authn.Read)
	h := s.Handler()
	do(h, "GET", "/v1/hello", rd)
	do(h, "GET", "/nope", rd)
	w := do(h, "GET", "/metrics", rd)
	body := w.Body.String()
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") ||
		!strings.Contains(body, "calendard_rev ") || !strings.Contains(body, "calendard_db_bytes ") ||
		!strings.Contains(body, `calendard_http_requests_total{class="2xx"}`) || !strings.Contains(body, `calendard_http_requests_total{class="4xx"} 1`) {
		t.Fatal(w.Code, body)
	}
	if do(h, "GET", "/metrics", "").Code != 401 {
		t.Fatal("metrics must need a token")
	}
	// backup warnings only when backups are configured
	warns := func() string { return do(h, "GET", "/v1/status", rd).Body.String() }
	if strings.Contains(warns(), `"kind":"backup"`) {
		t.Fatal("warned without backups configured")
	}
	s.BackupDir = "/data/backups"
	s.Auth.DB.W.Exec(`UPDATE tokens SET created_at = created_at - 200000`)
	if !strings.Contains(warns(), "No backup has been made yet") {
		t.Fatal(warns())
	}
	s.Auth.DB.W.Exec(`INSERT INTO meta(key,value) VALUES ('last_backup_at',?)`, time.Now().Add(-50*time.Hour).Unix())
	if !strings.Contains(warns(), "50 hours old") {
		t.Fatal(warns())
	}
	s.Auth.DB.W.Exec(`UPDATE meta SET value=? WHERE key='last_backup_at'`, time.Now().Add(-2*time.Hour).Unix())
	if strings.Contains(warns(), `"kind":"backup"`) {
		t.Fatal("fresh backup still warns")
	}
}

func TestOversizedBodyRefused(t *testing.T) {
	e := newWEnv(t)
	big := bytes.Repeat([]byte("a"), 2<<20)
	r := httptest.NewRequest("POST", "/v1/events", bytes.NewReader(append([]byte(`{"title":"`), big...)))
	r.Header.Set("Authorization", "Bearer "+e.write)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized body: %d", w.Code)
	}
	_ = slog.Default
}
