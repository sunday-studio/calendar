package gtest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMultiRoutesByToken(t *testing.T) {
	m := NewMulti("a@x.test", "b@x.test")
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	get := func(path, tok string) (int, map[string]any) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return r.StatusCode, out
	}
	code, cl := get("/users/me/calendarList", "at-"+RefreshToken("b@x.test"))
	if code != 200 || len(cl["items"].([]any)) != 2 {
		t.Fatalf("b calendars: %d %v", code, cl)
	}
	if c, _ := get("/users/me/calendarList", ""); c != 401 {
		t.Fatalf("no token: %d", c)
	}
	// token endpoint: refresh works, then revoke makes it invalid_grant
	post := func(path, body string) int {
		r, err := http.Post(srv.URL+path, "application/x-www-form-urlencoded", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {RefreshToken("a@x.test")}}.Encode()
	if c := post("/token", form); c != 200 {
		t.Fatalf("refresh: %d", c)
	}
	post("/_admin/revoke", "") // no json body: ignored content type; body empty = all accounts
	if c := post("/token", form); c != 400 {
		t.Fatalf("revoked refresh: %d", c)
	}
	if c, _ := get("/users/me/calendarList", "at-"+RefreshToken("a@x.test")); c != 401 {
		t.Fatalf("revoked api: %d", c)
	}
	post("/_admin/reset", "")
	if c := post("/token", form); c != 200 {
		t.Fatalf("after reset: %d", c)
	}
}
