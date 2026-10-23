package api

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"

	"calendar/server/internal/authn"
	"calendar/server/internal/hubmcp"
)

const (
	failLimit  = 20 // invalid-token attempts per client per window before it is slowed down
	failWindow = time.Minute
	writeLimit = 30 * time.Second // per-response write deadline (the SSE stream manages its own)
)

// clientKey identifies a caller for abuse limiting. Behind `tailscale serve` the peer is the
// local proxy, so the first X-Forwarded-For entry is used when the peer is loopback.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if x := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); x != "" {
			return x
		}
	}
	return host
}

type failEntry struct {
	n     int
	reset time.Time
}

// failLimiter counts invalid-token attempts. Valid tokens are never limited, so a brute force
// cannot lock the owner out; it only gets 429s itself.
type failLimiter struct {
	mu     gosync.Mutex
	m      map[string]*failEntry
	limit  int           // 0 = failLimit
	window time.Duration // 0 = failWindow
}

func (l *failLimiter) lim() int {
	if l.limit > 0 {
		return l.limit
	}
	return failLimit
}

func (l *failLimiter) win() time.Duration {
	if l.window > 0 {
		return l.window
	}
	return failWindow
}

func (l *failLimiter) blocked(key string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[key]
	if e == nil || now.After(e.reset) {
		return 0, false
	}
	return e.reset.Sub(now), e.n >= l.lim()
}

func (l *failLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]*failEntry{}
	}
	if len(l.m) > 10000 { // bounded memory
		for k, e := range l.m {
			if now.After(e.reset) {
				delete(l.m, k)
			}
		}
	}
	e := l.m[key]
	if e == nil || now.After(e.reset) {
		e = &failEntry{reset: now.Add(l.win())}
		l.m[key] = e
	}
	e.n++
}

// counters for /metrics
type counters struct{ c2, c3, c4, c5 atomic.Int64 }

func (c *counters) add(status int) {
	switch {
	case status < 300:
		c.c2.Add(1)
	case status < 400:
		c.c3.Add(1)
	case status < 500:
		c.c4.Add(1)
	default:
		c.c5.Add(1)
	}
}

// redactQuery hides search terms and similar free text from logs ("no event titles at info").
func redactQuery(raw string) string {
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.RawQuery == "" {
		return Redact(raw)
	}
	q := u.Query()
	for _, k := range []string{"q", "text"} {
		if q.Has(k) {
			q.Set(k, "…")
		}
	}
	u.RawQuery = q.Encode()
	return Redact(u.String())
}

// harden adds response headers, a request id and a write deadline.
func (s *Server) harden(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ulid.Make().String()
		h := w.Header()
		h.Set("X-Request-Id", id)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		if r.URL.Path != "/v1/stream" {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(writeLimit))
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
		next.ServeHTTP(w, r)
	})
}

// metrics serves a small Prometheus text page (read scope).
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := s.Auth.DB
	var b strings.Builder
	g := func(name, help string, v any, labels string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s%s %v\n", name, help, name, name, labels, v)
	}
	if rev, err := d.Rev(ctx); err == nil {
		g("calendard_rev", "Current data revision.", rev, "")
	}
	if n, err := d.SizeBytes(ctx); err == nil {
		g("calendard_db_bytes", "Database size.", n, "")
	}
	if s.Stream != nil {
		g("calendard_sse_clients", "Connected live-update clients.", s.Stream.Clients(), "")
	}
	var events, fails int
	_ = d.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`).Scan(&events)
	g("calendard_events", "Live events in the mirror.", events, "")
	_ = d.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_runs WHERE error_kind IS NOT NULL AND started_at > ?`, time.Now().Add(-24*time.Hour).Unix()).Scan(&fails)
	g("calendard_sync_failures_24h", "Failed sync runs in the last 24 hours.", fails, "")
	if rs, err := d.R.QueryContext(ctx, `SELECT status, COUNT(*) FROM accounts GROUP BY status ORDER BY status`); err == nil {
		fmt.Fprintf(&b, "# HELP calendard_accounts Accounts by status.\n# TYPE calendard_accounts gauge\n")
		for rs.Next() {
			var st string
			var n int
			if rs.Scan(&st, &n) == nil {
				fmt.Fprintf(&b, "calendard_accounts{status=%q} %d\n", st, n)
			}
		}
		rs.Close()
	}
	var last string
	if err := d.R.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='last_backup_at'`).Scan(&last); err == nil {
		var n int64
		fmt.Sscan(last, &n)
		g("calendard_last_backup_age_seconds", "Seconds since the last backup.", int64(time.Since(time.Unix(n, 0)).Seconds()), "")
	}
	fmt.Fprintf(&b, "# HELP calendard_http_requests_total Requests by status class.\n# TYPE calendard_http_requests_total counter\n")
	for _, c := range []struct {
		class string
		v     *atomic.Int64
	}{{"2xx", &s.reqs.c2}, {"3xx", &s.reqs.c3}, {"4xx", &s.reqs.c4}, {"5xx", &s.reqs.c5}} {
		fmt.Fprintf(&b, "calendard_http_requests_total{class=%q} %d\n", c.class, c.v.Load())
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) metricsRoute(mux *http.ServeMux) {
	mux.Handle("GET /metrics", s.require(authn.Read, http.HandlerFunc(s.metrics)))
}

// Limits are the abuse limits; zero values mean the defaults. Environment: CALENDAR_AUTH_FAIL_LIMIT,
// CALENDAR_AUTH_FAIL_WINDOW, CALENDAR_MCP_RATE, CALENDAR_MCP_MAX_BODY.
type Limits struct {
	AuthFailLimit  int           // invalid-token attempts per client per window before 429 (default 20)
	AuthFailWindow time.Duration // default 1 minute
	MCPPerMinute   int           // POST /mcp calls per token per minute (default 120)
	MCPMaxBody     int64         // bytes accepted on /mcp (default 256 KiB)
}

const (
	defMCPRate    = 120
	defMCPMaxBody = 256 << 10
	maxAuthHeader = 512 // a real "Bearer cal_..." header is about 60 bytes
)

func (l Limits) mcpRate() int {
	if l.MCPPerMinute > 0 {
		return l.MCPPerMinute
	}
	return defMCPRate
}

func (l Limits) mcpBody() int64 {
	if l.MCPMaxBody > 0 {
		return l.MCPMaxBody
	}
	return defMCPMaxBody
}

type bucket struct {
	tokens float64
	at     time.Time
}

// rateLimiter is a per-key token bucket (capacity = one minute's allowance).
type rateLimiter struct {
	mu gosync.Mutex
	m  map[string]*bucket
}

// allow takes one token; when empty it returns how long until one is available.
func (l *rateLimiter) allow(key string, perMin int, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]*bucket{}
	}
	if len(l.m) > 10000 {
		for k, b := range l.m {
			if now.Sub(b.at) > 2*time.Minute {
				delete(l.m, k)
			}
		}
	}
	capacity := float64(perMin)
	b := l.m[key]
	if b == nil {
		b = &bucket{tokens: capacity, at: now}
		l.m[key] = b
	}
	b.tokens += now.Sub(b.at).Seconds() * capacity / 60
	if b.tokens > capacity {
		b.tokens = capacity
	}
	b.at = now
	if b.tokens < 1 {
		return time.Duration((1 - b.tokens) / capacity * 60 * float64(time.Second)), false
	}
	b.tokens--
	return 0, true
}

// limitMCP wraps the MCP handler (already authenticated) with a per-token rate limit and a body cap.
func (s *Server) limitMCP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := authn.FromContext(r.Context())
		if wait, ok := s.mcpRate.allow(p.TokenID, s.Limits.mcpRate(), time.Now()); !ok {
			hubmcp.RecordRateLimited(r.Context())
			w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
			WriteError(w, http.StatusTooManyRequests, CodeRateLimited, "too many MCP calls; slow down", "")
			return
		}
		max := s.Limits.mcpBody()
		if r.ContentLength > max {
			w.Header().Set("Connection", "close")
			WriteError(w, http.StatusRequestEntityTooLarge, CodeValidation, fmt.Sprintf("request body is over the %d byte limit", max), "")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(w, r)
	})
}
