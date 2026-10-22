package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oklog/ulid/v2"

	"calendar/server/internal/authn"
	"calendar/server/internal/stream"
)

// wireStream attaches a broker to the env exactly like cmd/calendard does.
func (e *env) wireStream() *stream.Broker {
	b := &stream.Broker{Rev: e.d.Rev, PingInterval: time.Hour}
	e.d.OnCommit(b.PublishRev)
	e.s.Accounts.OnStatus = func(id, st string) { b.Account(id, WireStatus(st)) }
	e.s.Stream = b
	e.h = e.s.Handler()
	return b
}

type sseFrame struct{ id, event, data string }

func openStream(t *testing.T, url, token, lastID string) (*http.Response, <-chan sseFrame) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", url+"/v1/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); resp.Body.Close() })
	ch := make(chan sseFrame, 64)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(resp.Body)
		var f sseFrame
		for sc.Scan() {
			switch l := sc.Text(); {
			case l == "":
				if f.event != "" {
					ch <- f
				}
				f = sseFrame{}
			case strings.HasPrefix(l, "id: "):
				f.id = l[4:]
			case strings.HasPrefix(l, "event: "):
				f.event = l[7:]
			case strings.HasPrefix(l, "data: "):
				f.data = l[6:]
			}
		}
	}()
	return resp, ch
}

func nextFrame(t *testing.T, ch <-chan sseFrame) sseFrame {
	t.Helper()
	select {
	case f, ok := <-ch:
		if !ok {
			t.Fatal("stream closed")
		}
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
	return sseFrame{}
}

func TestStreamIntegration(t *testing.T) {
	e := newEnv(t)
	b := e.wireStream()
	srv := httptest.NewServer(e.h)
	t.Cleanup(srv.Close) // registered first, so it runs after the client cleanups

	// auth is enforced
	r, _ := http.Get(srv.URL + "/v1/stream")
	if r.StatusCode != 401 {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()

	resp, ch := openStream(t, srv.URL, e.admin, "")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal(resp.StatusCode, resp.Header)
	}
	if f := nextFrame(t, ch); f.event != "rev" || f.data != `{"rev":0}` {
		t.Fatalf("%+v", f)
	}
	deadline := time.Now().Add(2 * time.Second)
	for b.Clients() != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if b.Clients() != 1 {
		t.Fatal("client not registered")
	}

	// A fake sync: an account appears, then the worker marks it ok and reports a pass.
	id := ulid.Make().String()
	if _, err := e.d.W.Exec(`INSERT INTO accounts(id,provider,external_id,email,color,status,secret,created_at) VALUES (?,?,?,?,?,?,?,?)`,
		id, "google", "sub", "a@b.c", "cobalt", "connecting", []byte("x"), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	b.Sync(id, stream.SyncStarted)
	if err := e.s.Accounts.MarkOK(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	b.Sync(id, stream.SyncFinished)

	if f := nextFrame(t, ch); f.event != "sync" || !strings.Contains(f.data, `"started"`) {
		t.Fatalf("%+v", f)
	}
	// MarkOK is a status transition: one rev (commit hook) and one account event.
	got := map[string]sseFrame{}
	for i := 0; i < 2; i++ {
		f := nextFrame(t, ch)
		got[f.event] = f
	}
	if got["rev"].data != `{"rev":1}` || got["rev"].id != "1" {
		t.Fatalf("%+v", got)
	}
	if got["account"].data != `{"id":"`+id+`","status":"active"}` {
		t.Fatalf("%+v", got)
	}
	if f := nextFrame(t, ch); f.event != "sync" || !strings.Contains(f.data, `"finished"`) {
		t.Fatalf("%+v", f)
	}

	// A no-op MarkOK (already ok) changes nothing: no rev, no account event.
	if err := e.s.Accounts.MarkOK(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	b.Sync(id, stream.SyncFailed)
	if f := nextFrame(t, ch); f.event != "sync" || !strings.Contains(f.data, `"failed"`) {
		t.Fatalf("expected next frame to be the sync event, got %+v", f)
	}

	// Reconnect behind: told the current rev at once.
	_, ch2 := openStream(t, srv.URL, e.admin, "0")
	if f := nextFrame(t, ch2); f.event != "rev" || f.data != `{"rev":1}` {
		t.Fatalf("%+v", f)
	}
}

func specDoc(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func validate(t *testing.T, doc *openapi3.T, path string, body []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	sch := doc.Paths.Find(path).GetOperation("GET").Responses.Status(200).Value.Content.Get("application/json").Schema.Value
	if err := sch.VisitJSON(v, openapi3.MultiErrors()); err != nil {
		t.Fatalf("%s violates schema: %v\n%s", path, err, body)
	}
}

func TestStatusAndSyncRuns(t *testing.T) {
	e := newEnv(t)
	b := e.wireStream()
	doc := specDoc(t)
	_, read, _ := e.s.Auth.Create(context.Background(), "r", "device", authn.Read)

	for _, p := range []string{"/v1/status", "/v1/sync/runs"} {
		if w := e.req("GET", p, "", nil); w.Code != 401 {
			t.Fatal(p, w.Code)
		}
	}

	// empty hub
	w := e.req("GET", "/v1/status", read, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	validate(t, doc, "/v1/status", w.Body.Bytes())

	// one failing account, one healthy, one stream client
	now := time.Now().Unix()
	ins := func(email, status, detail string, fails int) string {
		id := ulid.Make().String()
		var d any
		if detail != "" {
			d = detail
		}
		if _, err := e.d.W.Exec(`INSERT INTO accounts(id,provider,external_id,email,color,status,status_detail,secret,last_sync_at,last_ok_at,next_sync_at,fail_count,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, "google", "sub-"+email, email, "cobalt", status, d, []byte("x"), now-60, now-600, now+300, fails, now); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ok := ins("ok@x.com", "ok", "stale detail", 0)
	bad := ins("bad@x.com", "error", "boom", 3)
	srv := httptest.NewServer(e.h)
	t.Cleanup(srv.Close)
	openStream(t, srv.URL, e.admin, "")
	for b.Clients() != 1 {
		time.Sleep(5 * time.Millisecond)
	}
	if err := e.s.Accounts.MarkOK(context.Background(), ok); err != nil { // no transition; just bumps nothing
		t.Fatal(err)
	}

	w = e.req("GET", "/v1/status", read, nil)
	validate(t, doc, "/v1/status", w.Body.Bytes())
	var st Status
	json.Unmarshal(w.Body.Bytes(), &st)
	if st.HubVersion != "test" || st.SSEClients != 1 || st.DBSize <= 0 || len(st.Accounts) != 2 || len(st.Warnings) != 0 {
		t.Fatalf("%+v", st)
	}
	byID := map[string]AccountSyncStatus{}
	for _, a := range st.Accounts {
		byID[a.AccountID] = a
	}
	if a := byID[ok]; a.Status != "active" || a.Email != "ok@x.com" || a.LastError != nil || a.NextSyncAt == nil || a.FailCount != 0 {
		t.Fatalf("%+v", a)
	}
	if a := byID[bad]; a.Status != "error" || a.LastError == nil || *a.LastError != "boom" || a.FailCount != 3 || a.LastOKAt == nil || a.LastSyncAt == nil {
		t.Fatalf("%+v", a)
	}

	// sync runs: newest first, limit, account filter, validation
	for i := 0; i < 25; i++ {
		var ek any
		if i == 24 {
			ek = "network"
		}
		if _, err := e.d.W.Exec(`INSERT INTO sync_runs(account_id,calendar_id,started_at,duration_ms,trigger,added,changed,removed,error_kind,error) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			ok, nil, now-int64(100-i), 12, "tick", 1, 2, 3, ek, ek); err != nil {
			t.Fatal(err)
		}
	}
	e.d.W.Exec(`INSERT INTO sync_runs(account_id,started_at) VALUES (?,?)`, bad, now-1000)
	w = e.req("GET", "/v1/sync/runs", read, nil)
	validate(t, doc, "/v1/sync/runs", w.Body.Bytes())
	var runs struct{ Items []SyncRun }
	json.Unmarshal(w.Body.Bytes(), &runs)
	if len(runs.Items) != 20 || runs.Items[0].ErrorKind == nil || *runs.Items[0].ErrorKind != "network" ||
		runs.Items[0].Added != 1 || runs.Items[0].Trigger != "tick" || !runs.Items[0].StartedAt.After(runs.Items[1].StartedAt) {
		t.Fatalf("%d %+v", len(runs.Items), runs.Items[0])
	}
	w = e.req("GET", "/v1/sync/runs?limit=3&accountId="+bad, read, nil)
	json.Unmarshal(w.Body.Bytes(), &runs)
	if len(runs.Items) != 1 || runs.Items[0].AccountID != bad {
		t.Fatalf("%+v", runs.Items)
	}
	validate(t, doc, "/v1/sync/runs", w.Body.Bytes())
	for _, q := range []string{"limit=0", "limit=201", "limit=x"} {
		if w := e.req("GET", "/v1/sync/runs?"+q, read, nil); w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
}
