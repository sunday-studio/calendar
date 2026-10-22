package fixtures

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	l := openapi3.NewLoader()
	doc, err := l.LoadFromFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml invalid: %v", err)
	}
	return doc
}

func get(h http.Handler, path string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Authorization", "Bearer cal_test")
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// check validates the recorded body against the documented response schema.
func check(t *testing.T, doc *openapi3.T, tmpl, method string, w *httptest.ResponseRecorder) {
	t.Helper()
	op := doc.Paths.Find(tmpl).GetOperation(method)
	if op == nil {
		t.Fatalf("no operation %s %s", method, tmpl)
	}
	ref := op.Responses.Status(w.Code)
	if ref == nil || ref.Value == nil {
		t.Fatalf("%s %s: status %d not documented", method, tmpl, w.Code)
	}
	mt := ref.Value.Content.Get("application/json")
	if mt == nil {
		t.Fatalf("%s %s: no json content for %d", method, tmpl, w.Code)
	}
	var v any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if err := mt.Schema.Value.VisitJSON(v, openapi3.MultiErrors()); err != nil {
		t.Errorf("%s %s (%d) violates schema: %v\n%s", method, tmpl, w.Code, err, w.Body.String())
	}
}

func TestFixturesMatchOpenAPI(t *testing.T) {
	doc := loadSpec(t)
	now := time.Date(2026, 10, 14, 8, 15, 0, 0, time.UTC) // a Wednesday
	h := (&Server{Data: New(now), Name: "calendard-fixtures", Version: "test"}).Handler()

	from := now.Add(-48 * time.Hour)
	to := now.AddDate(0, 0, 14)
	q := url.Values{"from": {from.Format(time.RFC3339)}, "to": {to.Format(time.RFC3339)}}.Encode()

	check(t, doc, "/v1/hello", "GET", get(h, "/v1/hello"))
	check(t, doc, "/v1/status", "GET", get(h, "/v1/status"))
	check(t, doc, "/v1/accounts", "GET", get(h, "/v1/accounts"))
	check(t, doc, "/v1/calendars", "GET", get(h, "/v1/calendars"))
	check(t, doc, "/v1/settings", "GET", get(h, "/v1/settings"))
	check(t, doc, "/health", "GET", get(h, "/health"))

	w := get(h, "/v1/agenda?"+q)
	check(t, doc, "/v1/agenda", "GET", w)
	var ag struct {
		Items []struct{ ID, EventID string }
	}
	json.Unmarshal(w.Body.Bytes(), &ag)
	if len(ag.Items) < 15 {
		t.Fatalf("expected a rich agenda, got %d items", len(ag.Items))
	}

	// Event detail by occurrence id and by event id.
	check(t, doc, "/v1/events/{id}", "GET", get(h, "/v1/events/"+ag.Items[0].ID))
	check(t, doc, "/v1/events/{id}", "GET", get(h, "/v1/events/"+ag.Items[0].EventID))

	// Errors validate against Error.
	check(t, doc, "/v1/events/{id}", "GET", get(h, "/v1/events/nope"))
	r := httptest.NewRequest("GET", "/v1/hello", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	check(t, doc, "/v1/hello", "GET", rec)
	check(t, doc, "/v1/agenda", "GET", get(h, "/v1/agenda?from=x&to=y"))
}

func TestAgendaShapeAndETag(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 15, 0, 0, time.UTC)
	h := (&Server{Data: New(now)}).Handler()
	q := url.Values{"from": {now.Add(-24 * time.Hour).Format(time.RFC3339)}, "to": {now.AddDate(0, 0, 10).Format(time.RFC3339)}}.Encode()

	w := get(h, "/v1/agenda?"+q)
	etag := w.Header().Get("ETag")
	if w.Code != 200 || etag == "" {
		t.Fatal(w.Code, etag)
	}
	if w2 := get(h, "/v1/agenda?"+q, "If-None-Match", etag); w2.Code != 304 || w2.Body.Len() != 0 {
		t.Fatalf("want 304, got %d", w2.Code)
	}
	if w3 := get(h, "/v1/agenda?"+q, "If-None-Match", `"other"`); w3.Code != 200 {
		t.Fatalf("want 200, got %d", w3.Code)
	}

	var ag struct {
		Items []map[string]any
	}
	json.Unmarshal(w.Body.Bytes(), &ag)
	var allDay, recurring, join, declined, tentative, inPerson, overlap bool
	var prevEnd time.Time
	for _, it := range ag.Items {
		allDay = allDay || it["allDay"] == true
		recurring = recurring || it["recurring"] == true
		join = join || it["join"] != nil
		declined = declined || it["myResponse"] == "declined"
		tentative = tentative || it["myResponse"] == "tentative"
		inPerson = inPerson || (it["location"] != nil && it["join"] == nil)
		if it["allDay"] == false {
			s, _ := time.Parse(time.RFC3339, it["start"].(string))
			if s.Before(prevEnd) {
				overlap = true
			}
			e, _ := time.Parse(time.RFC3339, it["end"].(string))
			if e.After(prevEnd) {
				prevEnd = e
			}
		}
		if v, ok := it["version"].(float64); !ok || v < 1 {
			t.Errorf("version missing: %v", it["version"])
		}
	}
	for name, ok := range map[string]bool{"allDay": allDay, "recurring": recurring, "join": join, "declined": declined, "tentative": tentative, "inPerson": inPerson, "overlap": overlap} {
		if !ok {
			t.Errorf("fixtures lack %s", name)
		}
	}

	// calendars filter + window filter.
	w = get(h, "/v1/agenda?"+q+"&calendars="+CalSide)
	json.Unmarshal(w.Body.Bytes(), &ag)
	for _, it := range ag.Items {
		if it["calendarId"] != CalSide {
			t.Errorf("calendar filter leaked %v", it["calendarId"])
		}
	}
	if len(ag.Items) == 0 {
		t.Error("no side items")
	}
}

func call(h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Authorization", "Bearer cal_test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// checkAny validates JSON bodies; 204 and non-JSON responses only need a documented status.
func checkAny(t *testing.T, doc *openapi3.T, tmpl, method string, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code == 204 {
		if ref := doc.Paths.Find(tmpl).GetOperation(method).Responses.Status(204); ref == nil {
			t.Errorf("%s %s: 204 not documented", method, tmpl)
		}
		return
	}
	check(t, doc, tmpl, method, w)
}

func TestEveryStateMatchesOpenAPI(t *testing.T) {
	doc := loadSpec(t)
	now := time.Date(2026, 10, 14, 8, 15, 0, 0, time.UTC) // a Wednesday
	from, to := now.Add(-48*time.Hour).Format(time.RFC3339), now.AddDate(0, 0, 14).Format(time.RFC3339)
	win := "from=" + url.QueryEscape(from) + "&to=" + url.QueryEscape(to)
	for _, state := range States {
		if state == "offline" || state == "down" {
			continue // transport-level failures; covered by TestFixtureStates
		}
		t.Run(state, func(t *testing.T) {
			h := (&Server{Data: New(now), Name: "fx", Version: "t", State: state, Delay: time.Millisecond}).Handler()
			for _, ep := range [][2]string{
				{"/v1/hello", "/v1/hello"}, {"/v1/status", "/v1/status"}, {"/v1/accounts", "/v1/accounts"}, {"/v1/calendars", "/v1/calendars"},
				{"/v1/settings", "/v1/settings"}, {"/v1/agenda?" + win, "/v1/agenda"},
				{"/v1/sync/runs", "/v1/sync/runs"}, {"/v1/sync/runs?limit=3", "/v1/sync/runs"}, {"/v1/sync/runs?limit=0", "/v1/sync/runs"},
				{"/v1/tokens", "/v1/tokens"}, {"/v1/summary", "/v1/summary"}, {"/v1/summary?date=2026-10-15", "/v1/summary"}, {"/v1/summary?date=bad", "/v1/summary"},
				{"/v1/people?q=sam", "/v1/people"}, {"/v1/people?q=", "/v1/people"},
				{"/v1/events?q=design", "/v1/events"}, {"/v1/events?" + win, "/v1/events"}, {"/v1/events", "/v1/events"},
			} {
				w := call(h, "GET", ep[0], nil)
				if w.Code == 404 && strings.HasPrefix(ep[0], "/v1/events/") {
					continue
				}
				checkAny(t, doc, ep[1], "GET", w)
			}
			// every state serves a decodable status with the accounts it claims
			var st struct {
				Accounts []struct{ AccountID, Status string }
				Warnings []map[string]any
			}
			json.Unmarshal(call(h, "GET", "/v1/status", nil).Body.Bytes(), &st)
			wantAccts := 3
			if state == "noaccounts" {
				wantAccts = 0
			}
			if len(st.Accounts) != wantAccts {
				t.Errorf("status accounts: %d want %d", len(st.Accounts), wantAccts)
			}
			if (state == "banner") != (len(st.Warnings) == 1) {
				t.Errorf("warnings %v in state %s", st.Warnings, state)
			}
			// POSTs and DELETEs
			ft := map[string]any{"durationMin": 90, "from": from, "to": to}
			checkAny(t, doc, "/v1/freetime", "POST", call(h, "POST", "/v1/freetime", ft))
			ft["durationMin"] = 1
			checkAny(t, doc, "/v1/freetime", "POST", call(h, "POST", "/v1/freetime", ft))
			ft["durationMin"] = 600
			checkAny(t, doc, "/v1/freetime", "POST", call(h, "POST", "/v1/freetime", ft))
			checkAny(t, doc, "/v1/quick-add", "POST", call(h, "POST", "/v1/quick-add", map[string]any{"text": "lunch with Sam fri 1pm at Kōya", "now": now.Format(time.RFC3339)}))
			checkAny(t, doc, "/v1/quick-add", "POST", call(h, "POST", "/v1/quick-add", map[string]any{"text": ""}))
			checkAny(t, doc, "/v1/tokens", "POST", call(h, "POST", "/v1/tokens", map[string]any{"name": "ci", "kind": "script", "scope": "read"}))
			checkAny(t, doc, "/v1/tokens", "POST", call(h, "POST", "/v1/tokens", map[string]any{"name": "", "kind": "script", "scope": "read"}))
			for _, id := range []string{"01FIXTOKEN0000000000000002", "01FIXTOKEN0000000000000001", "nope"} {
				checkAny(t, doc, "/v1/tokens/{id}", "DELETE", call(h, "DELETE", "/v1/tokens/"+id, nil))
			}
			checkAny(t, doc, "/v1/sync", "POST", call(h, "POST", "/v1/sync", nil))
			checkAny(t, doc, "/v1/devices/self/push", "PUT", call(h, "PUT", "/v1/devices/self/push", map[string]any{"apnsToken": "abcd", "environment": "sandbox"}))
		})
	}
}

func TestFixtureQuickAddAndFreeTimeContent(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 15, 0, 0, time.UTC)
	h := (&Server{Data: New(now)}).Handler()
	var d struct {
		Source string
		Event  struct {
			Title string
			When  struct{ Start string }
		}
		Ambiguities []struct{ Field string }
	}
	json.Unmarshal(call(h, "POST", "/v1/quick-add", map[string]any{"text": "lunch with Sam fri 1pm", "now": now.Format(time.RFC3339)}).Body.Bytes(), &d)
	if d.Source != "rules" || d.Event.Title != "Lunch with Sam" || d.Event.When.Start != "2026-10-16T13:00:00" || len(d.Ambiguities) != 1 || d.Ambiguities[0].Field != "when.date" {
		t.Fatalf("%+v", d)
	}
	var ft struct{ Slots []struct{ Best bool } }
	json.Unmarshal(call(h, "POST", "/v1/freetime", map[string]any{"durationMin": 60, "from": now.Format(time.RFC3339), "to": now.AddDate(0, 0, 3).Format(time.RFC3339)}).Body.Bytes(), &ft)
	if len(ft.Slots) == 0 {
		t.Fatal("no free slots in the fixture week")
	}
	var sum struct {
		Count int
		Title string
	}
	json.Unmarshal(call(h, "GET", "/v1/summary?date=2026-10-14", nil).Body.Bytes(), &sum)
	if sum.Count == 0 || !strings.HasPrefix(sum.Title, "Today: ") {
		t.Fatalf("%+v", sum)
	}
}

func TestFixtureStreamFrames(t *testing.T) {
	read := func(state string) string {
		s := &Server{Data: New(time.Now()), State: state, PingEvery: 30 * time.Millisecond}
		ts := httptest.NewServer(s.Handler())
		defer ts.Close()
		req, _ := http.NewRequest("GET", ts.URL+"/v1/stream", nil)
		req.Header.Set("Authorization", "Bearer cal_x")
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatal(resp.Header)
		}
		b := make([]byte, 0, 2048)
		buf := make([]byte, 512)
		for {
			n, err := resp.Body.Read(buf)
			b = append(b, buf[:n]...)
			if err != nil {
				break
			}
		}
		return string(b)
	}
	out := read("normal")
	if !strings.HasPrefix(out, "retry: 3000\n\n") || !strings.Contains(out, "id: 1\nevent: rev\ndata: {\"rev\":1}\n\n") || strings.Count(out, "event: ping\ndata: {}\n\n") < 2 {
		t.Fatalf("%q", out)
	}
	if out := read("syncing"); !strings.Contains(out, `event: sync`) || !strings.Contains(out, `"state":"started"`) {
		t.Fatalf("%q", out)
	}
	if out := read("banner"); !strings.Contains(out, `event: account`) || !strings.Contains(out, `needs_reauth`) {
		t.Fatalf("%q", out)
	}
}
