package fixtures

import (
	"encoding/json"
	"testing"
)

func TestFixtureICSAccount(t *testing.T) {
	e := newW(t, "ok")
	rr := e.do("POST", "/v1/accounts/ics", map[string]string{"url": "https://cal.example.com/f.ics"})
	if rr.Code != 201 {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
	var a struct{ Provider, Email, ID string }
	_ = json.Unmarshal(rr.Body.Bytes(), &a)
	if a.Provider != "ics" || a.Email != "cal.example.com" {
		t.Fatalf("%+v", a)
	}
	rr = e.do("GET", "/v1/accounts", nil)
	var l struct{ Items []struct{ ID string } }
	_ = json.Unmarshal(rr.Body.Bytes(), &l)
	if len(l.Items) != 4 {
		t.Fatalf("accounts %d", len(l.Items))
	}
	for _, u := range []string{"https://bad.example.com/x.ics", "nonsense"} {
		rr = e.do("POST", "/v1/accounts/ics", map[string]string{"url": u})
		var er struct{ Error struct{ Field string } }
		_ = json.Unmarshal(rr.Body.Bytes(), &er)
		if rr.Code != 422 || er.Error.Field != "url" {
			t.Fatalf("%s: %d %s", u, rr.Code, rr.Body)
		}
	}
}
