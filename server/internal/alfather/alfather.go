// Package alfather is the optional natural-language parser behind POST /v1/quick-add. alfather
// is reached as an MCP server over streamable HTTP; the tool name and the payload shapes are a
// configurable contract (see docs/engineering/alfather.md) because alfather's real tool names are not known
// yet. Any failure (unreachable, timeout, tool error, output that is not a valid Draft) falls
// back to the rule parser and marks the Draft degraded so the app can show "alfather isn't
// answering" while still offering the rule-based guess.
package alfather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"calendar/server/internal/quickadd"
	"calendar/server/internal/telemetry"
	"calendar/server/internal/tz"
)

const (
	DefaultTool    = "parse_event"
	DefaultTimeout = 3 * time.Second
)

// Config is read from CALENDAR_ALFATHER_URL / _PARSE_TOOL / _TOKEN. An empty URL = feature off.
type Config struct {
	URL       string
	ParseTool string
	Token     string        // optional bearer token for alfather
	Timeout   time.Duration // default 3 s; covers connect + call
	HTTP      *http.Client  // test seam
}

// Calendar is the calendar summary sent to alfather.
type Calendar struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

// Defaults tells alfather what to assume when the text is silent.
type Defaults struct {
	CalendarID  string `json:"calendarId,omitempty"`
	DurationMin int    `json:"durationMin"`
}

// Input is the arguments of the parse tool.
type Input struct {
	Text      string     `json:"text"`
	Now       string     `json:"now"` // RFC 3339, in TZ
	TZ        string     `json:"tz"`
	Calendars []Calendar `json:"calendars"`
	Defaults  Defaults   `json:"defaults"`
}

// Parser asks alfather first and falls back to Fallback.
type Parser struct {
	Cfg       Config
	Fallback  quickadd.Parser
	Env       quickadd.Env
	DefaultTZ string
	Now       func() time.Time
	Log       *slog.Logger
}

func (c Config) tool() string {
	if c.ParseTool != "" {
		return c.ParseTool
	}
	return DefaultTool
}

func (c Config) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// Parse implements quickadd.Parser.
func (p *Parser) Parse(ctx context.Context, req quickadd.Request) (out quickadd.Draft, rerr error) {
	ctx, span := telemetry.StartSpan(ctx, "quickadd.parse", telemetry.Attr("source", "alfather"))
	defer func() { quickadd.RecordParse(ctx, out, rerr); span.End() }()
	began := time.Now()
	d, err := p.askObserved(ctx, req)
	telemetry.Record(ctx, "alfather.latency", time.Since(began).Seconds())
	if err == nil {
		return d, nil
	}
	// a failed or slow assistant is not a failed request: the rule parser answers, flagged degraded
	if errors.Is(err, context.DeadlineExceeded) {
		telemetry.Inc(ctx, "alfather.timeouts")
		telemetry.RecordErrorKind(ctx, "alfather", telemetry.KindTimeout, err, telemetry.Operation("parse"))
	} else if ctx.Err() == nil {
		telemetry.RecordError(ctx, "alfather", err, telemetry.Operation("parse"))
	}
	span.SetAttributes(telemetry.Attr("degraded", "true"))
	lg := p.Log
	if lg == nil {
		lg = slog.Default()
	}
	lg.WarnContext(ctx, "alfather parse failed; using rules", "err", telemetry.Sanitize(err.Error()))
	fd, ferr := p.Fallback.Parse(quickadd.WithoutMetrics(ctx), req)
	if ferr != nil {
		return fd, ferr
	}
	fd.Source, fd.Degraded = "rules", true
	return fd, nil
}

func (p *Parser) askObserved(ctx context.Context, req quickadd.Request) (quickadd.Draft, error) {
	ctx, span := telemetry.StartSpan(ctx, "alfather.ask")
	defer span.End()
	return p.ask(ctx, req)
}

func (p *Parser) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Parser) ask(parent context.Context, req quickadd.Request) (quickadd.Draft, error) {
	var zero quickadd.Draft
	ctx, cancel := context.WithTimeout(parent, p.Cfg.timeout())
	defer cancel()

	zone := req.TZID
	if zone == "" {
		zone = p.DefaultTZ
	}
	loc, _ := tz.Load(zone)
	if zone == "" {
		zone = loc.String()
	}
	now := req.Now
	if now.IsZero() {
		now = p.now()
	}
	cals, err := p.Env.Calendars(ctx)
	if err != nil {
		return zero, fmt.Errorf("calendars: %w", err)
	}
	in := Input{Text: req.Text, Now: now.In(loc).Format(time.RFC3339), TZ: zone, Defaults: Defaults{DurationMin: 60}, Calendars: []Calendar{}}
	valid := map[string]bool{}
	for _, c := range cals {
		in.Calendars = append(in.Calendars, Calendar{ID: c.ID, Name: c.Name, IsDefault: c.IsDefault})
		valid[c.ID] = true
		if c.IsDefault {
			in.Defaults.CalendarID = c.ID
		}
	}

	hc := p.Cfg.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	if p.Cfg.Token != "" {
		hc = &http.Client{Transport: bearer{p.Cfg.Token, hc.Transport}, Timeout: hc.Timeout}
	}
	// client spans for the assistant call (templated path, no headers, so the bearer token never shows)
	hc = &http.Client{Transport: telemetry.WrapClientRedacted(hc.Transport, "alfather", "/mcp"), Timeout: hc.Timeout}
	client := mcp.NewClient(&mcp.Implementation{Name: "calendard", Version: "1"}, nil)
	sess, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: p.Cfg.URL, HTTPClient: hc, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return zero, fmt.Errorf("connect: %w", err)
	}
	defer sess.Close()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: p.Cfg.tool(), Arguments: in})
	if err != nil {
		return zero, fmt.Errorf("call %s: %w", p.Cfg.tool(), err)
	}
	if res.IsError {
		return zero, errors.New("tool reported an error")
	}
	raw, err := resultJSON(res)
	if err != nil {
		return zero, err
	}
	d, err := Validate(raw, valid, in.Defaults.CalendarID, req.Text)
	if err != nil {
		return zero, fmt.Errorf("invalid draft: %w", err)
	}
	return d, nil
}

type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	if b.next == nil {
		return http.DefaultTransport.RoundTrip(r)
	}
	return b.next.RoundTrip(r)
}

// resultJSON prefers structuredContent and otherwise the first text block.
func resultJSON(res *mcp.CallToolResult) ([]byte, error) {
	if res.StructuredContent != nil {
		return json.Marshal(res.StructuredContent)
	}
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			return []byte(t.Text), nil
		}
	}
	return nil, errors.New("tool returned no content")
}

// Validate decodes and checks alfather's answer. Anything that would not make a usable,
// safe-to-show Draft is an error (the caller then falls back to rules).
func Validate(raw []byte, calendars map[string]bool, defaultCal, text string) (quickadd.Draft, error) {
	var d quickadd.Draft
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("not JSON: %w", err)
	}
	e := &d.Event
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" || len(e.Title) > 1024 {
		return d, errors.New("event.title missing or too long")
	}
	switch {
	case e.When.AllDay:
		if _, err := time.Parse(time.DateOnly, e.When.Date); err != nil {
			return d, errors.New("event.when.date must be YYYY-MM-DD")
		}
	default:
		ok := false
		for _, l := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
			if _, err := time.Parse(l, e.When.Start); err == nil {
				ok = true
			}
		}
		if !ok {
			return d, errors.New("event.when.start must be a local time like 2026-10-16T13:00:00")
		}
		if e.When.DurationMin < 0 || e.When.DurationMin > 24*60*30 {
			return d, errors.New("event.when.durationMin out of range")
		}
	}
	if e.CalendarID == "" {
		e.CalendarID = defaultCal
	}
	if e.CalendarID == "" || (len(calendars) > 0 && !calendars[e.CalendarID]) {
		return d, errors.New("event.calendarId is not a writable calendar")
	}
	if len(e.Attendees) > 200 {
		return d, errors.New("too many attendees")
	}
	if e.Attendees == nil {
		e.Attendees = []string{}
	}
	if d.Confidence < 0 || d.Confidence > 1 || d.Confidence != d.Confidence {
		return d, errors.New("confidence out of range")
	}
	spans := d.Spans[:0]
	for _, s := range d.Spans {
		if s.From >= 0 && s.To > s.From && s.To <= utf16Count(text) && (s.Kind == "who" || s.Kind == "when" || s.Kind == "where") {
			spans = append(spans, s)
		}
	}
	d.Spans = spans
	if d.Spans == nil {
		d.Spans = []quickadd.Span{}
	}
	if d.People == nil {
		d.People = []quickadd.Person{}
	}
	if d.Ambiguities == nil {
		d.Ambiguities = []quickadd.Ambiguity{}
	}
	for i := range d.Ambiguities {
		a := &d.Ambiguities[i]
		if a.Question == "" || a.Field == "" {
			return d, errors.New("ambiguity needs field and question")
		}
		if a.Options == nil {
			a.Options = []quickadd.Option{}
		}
		if a.Default < -1 || a.Default >= len(a.Options) {
			a.Default = -1
		}
	}
	d.Source, d.Degraded = "alfather", false
	return d, nil
}

func utf16Count(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}
