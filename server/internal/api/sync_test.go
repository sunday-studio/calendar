package api

import (
	"context"
	"encoding/json"
	"testing"
)

type stubSyncer struct {
	e     *env
	calls []string
}

func (s *stubSyncer) TriggerManual(ctx context.Context, id string) (int, error) {
	s.calls = append(s.calls, id)
	if id != "" {
		if _, err := s.e.s.Accounts.Get(ctx, id); err != nil {
			return 0, err
		}
		return 1, nil
	}
	return 2, nil
}

func TestTriggerSync(t *testing.T) {
	e := newEnv(t)
	st := &stubSyncer{e: e}
	e.s.Syncer = st
	e.h = e.s.Handler()
	if w := e.req("POST", "/v1/sync", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := e.req("POST", "/v1/sync", e.admin, nil) // empty body = all accounts
	var out map[string]int
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 202 || out["queued"] != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := e.req("POST", "/v1/sync", e.admin, map[string]string{"accountId": "nope"}); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(st.calls) != 2 || st.calls[1] != "nope" {
		t.Fatal(st.calls)
	}
}
