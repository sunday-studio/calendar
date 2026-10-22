package alfather

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"calendar/server/internal/events"
	"calendar/server/internal/quickadd"
)

type env struct{}

func (env) Contacts(context.Context, string) ([]quickadd.Contact, error) { return nil, nil }
func (env) Calendars(context.Context) ([]quickadd.Calendar, error) {
	return []quickadd.Calendar{{ID: "cal1", Name: "Personal", IsDefault: true}, {ID: "cal2", Name: "Work"}}, nil
}

type rules struct {
	err error
	n   int
}

func (r *rules) Parse(_ context.Context, req quickadd.Request) (quickadd.Draft, error) {
	r.n++
	if r.err != nil {
		return quickadd.Draft{}, r.err
	}
	return quickadd.Draft{Source: "rules", Confidence: 0.5, Event: events.CreateIn{CalendarID: "cal1", Title: req.Text}}, nil
}

// fake is an alfather stand-in: one tool, behaviour set per test.
type fake struct {
	mu      sync.Mutex
	args    map[string]any
	tool    string
	reply   func() (*mcp.CallToolResult, error)
	delay   time.Duration
	srv     *httptest.Server
	hdrAuth string
}

func newFake(t *testing.T, tool string, reply func() (*mcp.CallToolResult, error)) *fake {
	f := &fake{tool: tool, reply: reply}
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		s := mcp.NewServer(&mcp.Implementation{Name: "fake-alfather", Version: "0"}, nil)
		s.AddTool(&mcp.Tool{Name: f.tool, InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a map[string]any
			_ = json.Unmarshal(r.Params.Arguments, &a)
			f.mu.Lock()
			f.args = a
			f.mu.Unlock()
			if f.delay > 0 {
				select {
				case <-time.After(f.delay):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return f.reply()
		})
		return s
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true})
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hdrAuth = r.Header.Get("Authorization")
		f.mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func text(s string) func() (*mcp.CallToolResult, error) {
	return func() (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}, nil
	}
}

const good = `{"confidence":0.9,"source":"alfather","event":{"calendarId":"cal2","title":"Lunch with Sam","when":{"allDay":false,"start":"2026-10-16T13:00:00","tzid":"Europe/London","durationMin":60},"attendees":["sam@x.test"],"recurrence":null,"reminderMin":null},
"people":[{"name":"Sam","email":"sam@x.test"}],"ambiguities":[],"spans":[{"kind":"when","from":15,"to":22},{"kind":"who","from":0,"to":9999}]}`

func parser(f *fake, fb *rules, timeout time.Duration) *Parser {
	return &Parser{Cfg: Config{URL: f.srv.URL, Timeout: timeout, Token: "tok"}, Fallback: fb, Env: env{}, Now: func() time.Time { return time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC) }}
}

func req() quickadd.Request {
	return quickadd.Request{Text: "Lunch with Sam fri 1pm", TZID: "Europe/London"}
}

func TestAlfatherSuccess(t *testing.T) {
	f := newFake(t, "parse_event", text(good))
	rp := &rules{}
	d, err := parser(f, rp, time.Second).Parse(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if d.Source != "alfather" || d.Degraded || d.Event.Title != "Lunch with Sam" || d.Event.CalendarID != "cal2" || rp.n != 0 {
		t.Fatalf("%+v rules=%d", d, rp.n)
	}
	if len(d.Spans) != 1 || d.Spans[0].Kind != "when" { // the out-of-range span is dropped
		t.Fatalf("%+v", d.Spans)
	}
	// the documented input contract
	a := f.args
	if a["text"] != "Lunch with Sam fri 1pm" || a["tz"] != "Europe/London" || a["now"] != "2026-10-12T10:00:00+01:00" {
		t.Fatalf("%v", a)
	}
	if len(a["calendars"].([]any)) != 2 || a["defaults"].(map[string]any)["calendarId"] != "cal1" {
		t.Fatalf("%v", a)
	}
	if f.hdrAuth != "Bearer tok" {
		t.Fatal(f.hdrAuth)
	}
}

func TestAlfatherStructuredContentAndCustomTool(t *testing.T) {
	var sc map[string]any
	_ = json.Unmarshal([]byte(good), &sc)
	f := newFake(t, "calendar.parse", func() (*mcp.CallToolResult, error) { return &mcp.CallToolResult{StructuredContent: sc}, nil })
	p := parser(f, &rules{}, time.Second)
	p.Cfg.ParseTool = "calendar.parse"
	d, err := p.Parse(context.Background(), req())
	if err != nil || d.Source != "alfather" {
		t.Fatalf("%v %+v", err, d)
	}
}

func degraded(t *testing.T, d quickadd.Draft, err error, rp *rules) {
	t.Helper()
	if err != nil || d.Source != "rules" || !d.Degraded || rp.n != 1 || d.Event.Title != req().Text {
		t.Fatalf("err=%v draft=%+v rules=%d", err, d, rp.n)
	}
}

func TestAlfatherFallbacks(t *testing.T) {
	cases := map[string]string{
		"garbage":          `I'm sorry, I can't do that`,
		"empty title":      `{"confidence":0.5,"event":{"calendarId":"cal1","title":" ","when":{"allDay":false,"start":"2026-10-16T13:00:00"}}}`,
		"bad start":        `{"confidence":0.5,"event":{"calendarId":"cal1","title":"x","when":{"allDay":false,"start":"tomorrow"}}}`,
		"unknown calendar": `{"confidence":0.5,"event":{"calendarId":"other","title":"x","when":{"allDay":false,"start":"2026-10-16T13:00:00"}}}`,
		"bad confidence":   `{"confidence":7,"event":{"calendarId":"cal1","title":"x","when":{"allDay":false,"start":"2026-10-16T13:00:00"}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFake(t, "parse_event", text(body))
			rp := &rules{}
			d, err := parser(f, rp, time.Second).Parse(context.Background(), req())
			degraded(t, d, err, rp)
		})
	}
	t.Run("tool error", func(t *testing.T) {
		f := newFake(t, "parse_event", func() (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "boom"}}}, nil
		})
		rp := &rules{}
		d, err := parser(f, rp, time.Second).Parse(context.Background(), req())
		degraded(t, d, err, rp)
	})
	t.Run("unknown tool", func(t *testing.T) {
		f := newFake(t, "something_else", text(good))
		rp := &rules{}
		d, err := parser(f, rp, time.Second).Parse(context.Background(), req())
		degraded(t, d, err, rp)
	})
	t.Run("unreachable", func(t *testing.T) {
		f := newFake(t, "parse_event", text(good))
		f.srv.Close()
		rp := &rules{}
		d, err := parser(f, rp, time.Second).Parse(context.Background(), req())
		degraded(t, d, err, rp)
	})
	t.Run("http error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 500) }))
		defer srv.Close()
		rp := &rules{}
		p := &Parser{Cfg: Config{URL: srv.URL, Timeout: time.Second}, Fallback: rp, Env: env{}}
		d, err := p.Parse(context.Background(), req())
		degraded(t, d, err, rp)
	})
}

func TestAlfatherTimeout(t *testing.T) {
	f := newFake(t, "parse_event", text(good))
	f.delay = 1500 * time.Millisecond
	rp := &rules{}
	start := time.Now()
	d, err := parser(f, rp, 300*time.Millisecond).Parse(context.Background(), req())
	degraded(t, d, err, rp)
	if time.Since(start) > 2*time.Second {
		t.Fatal("did not time out promptly:", time.Since(start))
	}
}

func TestDefaultTimeoutIsThreeSeconds(t *testing.T) {
	if (Config{}).timeout() != 3*time.Second || (Config{}).tool() != "parse_event" {
		t.Fatal("defaults changed")
	}
}

func TestFallbackFailureIsReturned(t *testing.T) {
	f := newFake(t, "parse_event", text("nope"))
	rp := &rules{err: errors.New("rules down")}
	if _, err := parser(f, rp, time.Second).Parse(context.Background(), req()); err == nil {
		t.Fatal("expected the fallback error")
	}
}
