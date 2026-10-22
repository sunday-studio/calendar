package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"calendar/server/internal/authn"
	"calendar/server/internal/db"
	"calendar/server/internal/domain"
	"calendar/server/internal/events"
	"calendar/server/internal/provider"
)

// Extra error codes of the write path (openapi ErrorCode).
const (
	CodeConflict     = "conflict"
	CodePrecondition = "precondition_failed"
	CodeProvider     = "provider_error"
	CodeNeedsReauth  = "needs_reauth"
	CodeRateLimited  = "rate_limited"
	CodeUnavailable  = "unavailable"
)

const (
	maxWriteBody = 1 << 20
	idemTTL      = 24 * time.Hour
	maxIdemKey   = 200
)

func (s *Server) writeRoutes(mux *http.ServeMux) {
	w := func(h http.HandlerFunc) http.Handler { return s.require(authn.Write, s.idempotent(h)) }
	mux.Handle("POST /v1/events", w(s.createEvent))
	mux.Handle("PATCH /v1/events/{id}", w(s.patchEvent))
	mux.Handle("DELETE /v1/events/{id}", w(s.deleteEvent))
	mux.Handle("POST /v1/events/{id}/rsvp", w(s.rsvpEvent))
	mux.Handle("POST /v1/events/{id}/meet", w(s.meetEvent))
	mux.Handle("POST /v1/events/{id}/move", w(s.moveEvent))
	mux.Handle("PUT /v1/events/{id}/reminder", w(s.setReminder))
}

// ---- errors ----

type errBodyCurrent struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Field   string `json:"field,omitempty"`
		Current any    `json:"current,omitempty"`
	} `json:"error"`
}

func (s *Server) writeFail(w http.ResponseWriter, err error) {
	e, ok := events.AsError(err)
	if !ok {
		s.Log.Error("write", "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "internal error", "")
		return
	}
	status, code := http.StatusInternalServerError, "internal"
	switch e.Kind {
	case events.KValidation:
		status, code = http.StatusBadRequest, CodeValidation
	case events.KNotFound:
		status, code = http.StatusNotFound, CodeNotFound
	case events.KForbidden:
		status, code = http.StatusForbidden, CodeForbidden
	case events.KPrecondition:
		status, code = http.StatusPreconditionFailed, CodePrecondition
	case events.KConflict:
		status, code = http.StatusConflict, CodeConflict
	case events.KProvider:
		status, code = http.StatusBadGateway, CodeProvider
	case events.KNeedsReauth:
		status, code = http.StatusFailedDependency, CodeNeedsReauth
	case events.KRateLimited:
		status, code = http.StatusTooManyRequests, CodeRateLimited
		if e.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int((e.RetryAfter+time.Second-1)/time.Second)))
		}
	case events.KUnavailable:
		status, code = http.StatusServiceUnavailable, CodeUnavailable
	}
	var b errBodyCurrent
	b.Error.Code, b.Error.Message, b.Error.Field = code, e.Msg, e.Field
	if e.Current != nil {
		b.Error.Current = e.Current
	}
	WriteJSON(w, status, b)
}

func badRequest(w http.ResponseWriter, field, msg string) {
	WriteError(w, http.StatusBadRequest, CodeValidation, msg, field)
}

// ---- request helpers ----

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWriteBody))
	if err != nil {
		badRequest(w, "", "request body too large or unreadable")
		return nil, false
	}
	return b, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any, required bool) bool {
	b, ok := readBody(w, r)
	if !ok {
		return false
	}
	if len(bytes.TrimSpace(b)) == 0 {
		if required {
			badRequest(w, "", "a JSON body is required")
			return false
		}
		return true
	}
	if err := json.Unmarshal(b, v); err != nil {
		badRequest(w, "", "invalid JSON: "+err.Error())
		return false
	}
	return true
}

// ifMatch parses If-Match: "4812", 4812 or W/"4812". Absent = nil.
func ifMatch(w http.ResponseWriter, r *http.Request) (*int64, bool) {
	h := strings.TrimSpace(r.Header.Get("If-Match"))
	if h == "" || h == "*" {
		return nil, true
	}
	h = strings.Trim(strings.TrimPrefix(h, "W/"), `"`)
	n, err := strconv.ParseInt(h, 10, 64)
	if err != nil || n < 0 {
		badRequest(w, "If-Match", `If-Match must be the event version, e.g. "4812"`)
		return nil, false
	}
	return &n, true
}

func scopeParam(r *http.Request) provider.Scope { return provider.Scope(r.URL.Query().Get("scope")) }

type ctxKey int

const seedKey ctxKey = 1

func opts(w http.ResponseWriter, r *http.Request) (events.Opts, bool) {
	im, ok := ifMatch(w, r)
	if !ok {
		return events.Opts{}, false
	}
	seed, _ := r.Context().Value(seedKey).(string)
	return events.Opts{IfMatch: im, Seed: seed}, true
}

// ---- handlers ----

func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) {
	var in events.CreateIn
	if !decodeBody(w, r, &in, true) {
		return
	}
	o, ok := opts(w, r)
	if !ok {
		return
	}
	ev, err := s.Events.Create(r.Context(), in, o)
	if err != nil {
		s.writeFail(w, err)
		return
	}
	w.Header().Set("Location", "/v1/events/"+ev.EventID)
	WriteJSON(w, http.StatusCreated, ev)
}

func (s *Server) patchEvent(w http.ResponseWriter, r *http.Request) {
	var in events.PatchIn
	if !decodeBody(w, r, &in, true) {
		return
	}
	o, ok := opts(w, r)
	if !ok {
		return
	}
	ev, err := s.Events.Patch(r.Context(), r.PathValue("id"), scopeParam(r), in, o)
	if err != nil {
		s.writeFail(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, ev)
}

func (s *Server) deleteEvent(w http.ResponseWriter, r *http.Request) {
	o, ok := opts(w, r)
	if !ok {
		return
	}
	if err := s.Events.Delete(r.Context(), r.PathValue("id"), scopeParam(r), o); err != nil {
		s.writeFail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rsvpEvent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Response string `json:"response"`
		Note     string `json:"note"`
		Scope    string `json:"scope"`
	}
	if !decodeBody(w, r, &in, true) {
		return
	}
	o, ok := opts(w, r)
	if !ok {
		return
	}
	sc := scopeParam(r)
	if in.Scope != "" {
		sc = provider.Scope(in.Scope)
	}
	ev, err := s.Events.RSVP(r.Context(), r.PathValue("id"), domain.Response(in.Response), in.Note, sc, o)
	if err != nil {
		s.writeFail(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, ev)
}

func (s *Server) meetEvent(w http.ResponseWriter, r *http.Request) {
	o, ok := opts(w, r)
	if !ok {
		return
	}
	ev, err := s.Events.AddMeet(r.Context(), r.PathValue("id"), o)
	if err != nil {
		s.writeFail(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, ev)
}

func (s *Server) moveEvent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CalendarID string `json:"calendarId"`
	}
	if !decodeBody(w, r, &in, true) {
		return
	}
	o, ok := opts(w, r)
	if !ok {
		return
	}
	ev, err := s.Events.Move(r.Context(), r.PathValue("id"), in.CalendarID, o)
	if err != nil {
		s.writeFail(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, ev)
}

func (s *Server) setReminder(w http.ResponseWriter, r *http.Request) {
	var raw map[string]json.RawMessage
	if !decodeBody(w, r, &raw, true) {
		return
	}
	v, present := raw["minutesBefore"]
	if !present {
		badRequest(w, "minutesBefore", "minutesBefore is required (null clears the override)")
		return
	}
	var minutes *int
	if string(v) != "null" {
		var n int
		if err := json.Unmarshal(v, &n); err != nil {
			badRequest(w, "minutesBefore", "must be an integer or null")
			return
		}
		minutes = &n
	}
	ev, err := s.Events.SetReminder(r.Context(), r.PathValue("id"), minutes)
	if err != nil {
		s.writeFail(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, ev)
}

// ---- idempotency ----

// capture records a response so it can be stored and replayed.
type capture struct {
	h      http.Header
	status int
	body   bytes.Buffer
}

func (c *capture) Header() http.Header         { return c.h }
func (c *capture) WriteHeader(code int)        { c.status = code }
func (c *capture) Write(b []byte) (int, error) { return c.body.Write(b) }

type idemLock struct {
	mu   sync.Mutex
	refs int
}

// lockKey serialises requests that share an Idempotency-Key, so a double tap waits for the
// first request and then replays its stored response instead of running twice.
func (s *Server) lockKey(k string) func() {
	s.idemMu.Lock()
	if s.idemLocks == nil {
		s.idemLocks = map[string]*idemLock{}
	}
	l := s.idemLocks[k]
	if l == nil {
		l = &idemLock{}
		s.idemLocks[k] = l
	}
	l.refs++
	s.idemMu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		s.idemMu.Lock()
		if l.refs--; l.refs == 0 {
			delete(s.idemLocks, k)
		}
		s.idemMu.Unlock()
	}
}

func (s *Server) now() time.Time { return time.Now() }

// idempotent wraps a write handler with Idempotency-Key semantics: the first successful (2xx)
// response is stored for 24 h per (token, key); the same key with the same request replays it,
// the same key with a different request is rejected. Failures are not stored, so a retry re-runs.
func (s *Server) idempotent(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			h(w, r)
			return
		}
		if len(key) > maxIdemKey {
			badRequest(w, "Idempotency-Key", "Idempotency-Key is too long")
			return
		}
		body, ok := readBody(w, r)
		if !ok {
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		p := authn.FromContext(r.Context())
		sum := sha256.New()
		sum.Write([]byte(r.Method + "\x00" + r.URL.RequestURI() + "\x00" + r.Header.Get("If-Match") + "\x00"))
		sum.Write(body)
		hash := sum.Sum(nil)

		unlock := s.lockKey(p.TokenID + "\x00" + key)
		defer unlock()

		var (
			rh     []byte
			status int
			resp   []byte
			at     int64
		)
		err := s.Auth.DB.R.QueryRowContext(r.Context(), `SELECT req_hash, status, response, created_at FROM idempotency WHERE token_id=? AND key=?`, p.TokenID, key).
			Scan(&rh, &status, &resp, &at)
		switch {
		case err == nil && s.now().Sub(time.Unix(at, 0)) < idemTTL:
			if !bytes.Equal(rh, hash) {
				WriteError(w, http.StatusUnprocessableEntity, CodeValidation, "this Idempotency-Key was already used with a different request", "Idempotency-Key")
				return
			}
			w.Header().Set("Idempotent-Replayed", "true")
			if status == http.StatusNoContent {
				w.WriteHeader(status)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(resp)
			return
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			s.internalErr(w, "idempotency lookup", err)
			return
		}

		c := &capture{h: http.Header{}, status: http.StatusOK}
		h(c, r.WithContext(context.WithValue(r.Context(), seedKey, p.TokenID+"\x00"+key)))
		for k, v := range c.h {
			w.Header()[k] = v
		}
		w.WriteHeader(c.status)
		_, _ = w.Write(c.body.Bytes())
		if c.status/100 != 2 {
			return
		}
		cutoff := s.now().Add(-idemTTL).Unix()
		if err := s.Auth.DB.Write(r.Context(), func(tx *db.Tx) error {
			if _, err := tx.ExecContext(r.Context(), `DELETE FROM idempotency WHERE created_at < ?`, cutoff); err != nil {
				return err
			}
			_, err := tx.ExecContext(r.Context(), `INSERT OR REPLACE INTO idempotency(key, token_id, req_hash, status, response, created_at) VALUES (?,?,?,?,?,?)`,
				key, p.TokenID, hash, c.status, append([]byte{}, c.body.Bytes()...), s.now().Unix())
			return err
		}); err != nil {
			s.Log.Error("idempotency store", "err", err)
		}
	}
}
