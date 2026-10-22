package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"calendar/server/internal/authn"
	"calendar/server/internal/db"
)

func setup(t *testing.T) (*Server, *bytes.Buffer) {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	buf := &bytes.Buffer{}
	return &Server{Auth: authn.Store{DB: d}, Log: slog.New(slog.NewTextHandler(buf, nil)), Name: "calendard", Version: "test"}, buf
}

func do(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHealth(t *testing.T) {
	s, _ := setup(t)
	w := do(s.Handler(), "GET", "/health", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestHello(t *testing.T) {
	s, logs := setup(t)
	id, secret, _ := s.Auth.Create(context.Background(), "t", "device", authn.Read)
	h := s.Handler()

	w := do(h, "GET", "/v1/hello", secret)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	if got["server"] != "calendard" || got["version"] != "test" || got["apiVersion"] != float64(1) || got["scope"] != "read" || got["tokenId"] != id {
		t.Fatalf("%v", got)
	}

	for _, tc := range []struct {
		tok  string
		code int
	}{{"", 401}, {"cal_bad", 401}} {
		w := do(h, "GET", "/v1/hello", tc.tok)
		var e errBody
		json.Unmarshal(w.Body.Bytes(), &e)
		if w.Code != tc.code || e.Error.Code != "unauthorized" {
			t.Fatalf("%q: %d %s", tc.tok, w.Code, w.Body.String())
		}
	}
	if w := do(h, "GET", "/nope", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	// secrets must not reach the log, even if passed in the URL
	do(h, "GET", "/v1/hello?token="+secret, secret)
	if strings.Contains(logs.String(), secret) {
		t.Fatal("token leaked into logs")
	}
}

func TestScopeForbiddenAndPanic(t *testing.T) {
	s, _ := setup(t)
	_, secret, _ := s.Auth.Create(context.Background(), "t", "device", authn.Read)
	h := s.require(authn.Write, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	p := s.recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	w = httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"internal"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
