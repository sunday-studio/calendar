// Package api is the HTTP surface: routing, auth, errors, logging.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	gosync "sync"
	"time"

	"calendar/server/internal/accounts"
	"calendar/server/internal/agenda"
	"calendar/server/internal/alfather"
	"calendar/server/internal/authn"
	"calendar/server/internal/automate"
	"calendar/server/internal/events"
	"calendar/server/internal/hubmcp"
	"calendar/server/internal/quickadd"
	"calendar/server/internal/stream"
	"calendar/server/internal/telemetry"
)

const APIVersion = 1

type Server struct {
	Auth        authn.Store
	Accounts    *accounts.Service // nil = account routes not mounted
	Syncer      Syncer            // nil = POST /v1/sync not mounted
	Stream      *stream.Broker    // nil = GET /v1/stream not mounted
	Store       *agenda.Store     // nil = built from Auth.DB with the real clock
	Events      *events.Service   // nil = write routes not mounted
	QuickAdd    quickadd.Parser   // nil = the rule parser over the hub database
	Automations *automate.Service // nil = automation routes not mounted
	Alfather    *alfather.Config  // nil or empty URL = quick-add uses only the rule parser
	MCP         *hubmcp.Hub       // nil = /mcp and /v1/mcp/* not mounted
	BackupDir   string            // set when scheduled backups are configured; /v1/status warns when they stop
	Log         *slog.Logger
	Name        string
	Version     string

	idemMu    gosync.Mutex
	idemLocks map[string]*idemLock
	limiter   failLimiter
	mcpRate   rateLimiter
	// Limits tunes the abuse limits (zero values = defaults).
	Limits Limits
	reqs   counters
}

func (s *Server) Handler() http.Handler {
	s.limiter.limit, s.limiter.window = s.Limits.AuthFailLimit, s.Limits.AuthFailWindow
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.Handle("GET /v1/hello", s.require(authn.Read, http.HandlerFunc(s.hello)))
	mux.Handle("GET /v1/status", s.require(authn.Read, http.HandlerFunc(s.status)))
	mux.Handle("GET /v1/sync/runs", s.require(authn.Read, http.HandlerFunc(s.syncRuns)))
	s.readRoutes(mux)
	s.metricsRoute(mux)
	s.pushRoutes(mux)
	s.tokenRoutes(mux)
	s.assistRoutes(mux)
	if s.Stream != nil {
		mux.Handle("GET /v1/stream", s.require(authn.Read, s.Stream))
	}
	if s.Accounts != nil {
		s.accountRoutes(mux)
	}
	if s.Events != nil {
		s.writeRoutes(mux)
	}
	if s.Automations != nil {
		s.automationRoutes(mux)
	}
	if s.MCP != nil {
		s.mcpRoutes(mux)
	}
	if s.Syncer != nil {
		s.syncRoutes(mux)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found", "no such route", "")
	})
	return telemetry.HTTP(s.recoverer(s.logging(s.harden(telemetry.Route(mux)))))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.Auth.DB.R.PingContext(r.Context()); err != nil {
		WriteError(w, http.StatusServiceUnavailable, "unhealthy", "database unavailable", "")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	p := authn.FromContext(r.Context())
	WriteJSON(w, http.StatusOK, map[string]any{
		"server": s.Name, "version": s.Version, "apiVersion": APIVersion,
		"scope": p.Scope, "tokenId": p.TokenID,
	})
}

// ---- errors & JSON ----

type errBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Field   string `json:"field,omitempty"`
	} `json:"error"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, code, msg, field string) {
	var b errBody
	b.Error.Code, b.Error.Message, b.Error.Field = code, msg, field
	WriteJSON(w, status, b)
}

// ---- middleware ----

// require authenticates the bearer token and enforces a minimum scope.
func (s *Server) require(need authn.Scope, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Get("Authorization")) > maxAuthHeader { // never hash megabytes of "token"
			key, now := clientKey(r), time.Now()
			s.limiter.fail(key, now)
			w.Header().Set("Connection", "close")
			WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid or revoked token", "")
			return
		}
		secret := authn.BearerToken(r.Header.Get("Authorization"))
		if secret == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token", "")
			return
		}
		p, err := s.Auth.Verify(r.Context(), secret)
		switch {
		case errors.Is(err, authn.ErrUnauthorized):
			key, now := clientKey(r), time.Now()
			if wait, blocked := s.limiter.blocked(key, now); blocked {
				authn.RecordThrottled(r.Context())
				w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
				WriteError(w, http.StatusTooManyRequests, CodeRateLimited, "too many invalid tokens; slow down", "")
				return
			}
			s.limiter.fail(key, now)
			if r.ContentLength > 64<<10 { // do not keep a connection open for a rejected upload
				w.Header().Set("Connection", "close")
			}
			w.Header().Set("WWW-Authenticate", "Bearer")
			WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid or revoked token", "")
			return
		case err != nil:
			s.Log.ErrorContext(r.Context(), "auth lookup failed", "err", err)
			telemetry.RecordSpanError(r.Context(), "auth", err)
			WriteError(w, http.StatusInternalServerError, "internal", "internal error", "")
			return
		}
		if !p.Scope.Allows(need) {
			authn.RecordScopeDenied(r.Context(), p)
			WriteError(w, http.StatusForbidden, "forbidden", "token scope "+string(need)+" required", "")
			return
		}
		next.ServeHTTP(w, r.WithContext(authn.WithPrincipal(r.Context(), p)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(c int)           { w.status = c; w.ResponseWriter.WriteHeader(c) }
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

var tokenRE = regexp.MustCompile(`cal_[A-Za-z0-9]+`)

// Redact masks anything that looks like a token secret.
func Redact(s string) string { return tokenRE.ReplaceAllString(s, "cal_REDACTED") }

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		// Authorization header is never logged; URL is redacted in case a secret is passed in it.
		s.reqs.add(sw.status)
		args := []any{"id", w.Header().Get("X-Request-Id"), "method", r.Method, "path", redactQuery(r.URL.RequestURI()),
			"status", sw.status, "ms", time.Since(start).Milliseconds()}
		if telemetry.Enabled() { // route pattern for log/trace correlation; the raw path stays out of OTel (see telemetry.scrubHandler)
			args = append(args, "route", telemetry.RouteFromContext(r.Context()))
		}
		s.Log.InfoContext(r.Context(), "request", args...)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				stack := debug.Stack()
				telemetry.RecordPanic(r.Context(), "http", v, stack)
				s.Log.ErrorContext(r.Context(), "panic", "value", Redact(toString(v)), "stack", string(stack))
				WriteError(w, http.StatusInternalServerError, "internal", "internal error", "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func toString(v any) string {
	if e, ok := v.(error); ok {
		return e.Error()
	}
	if s, ok := v.(string); ok {
		return s
	}
	return "panic"
}

// Error codes shared by every handler (openapi ErrorCode).
const (
	CodeUnauthorized = "unauthorized"
	CodeForbidden    = "forbidden"
	CodeNotFound     = "not_found"
	CodeValidation   = "validation"
)
