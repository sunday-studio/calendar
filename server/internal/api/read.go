package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"calendar/server/internal/agenda"
	"calendar/server/internal/authn"
)

const maxWindow = 400 * 24 * time.Hour

func (s *Server) readRoutes(mux *http.ServeMux) {
	mux.Handle("GET /v1/agenda", s.require(authn.Read, http.HandlerFunc(s.agenda)))
	mux.Handle("GET /v1/events", s.require(authn.Read, http.HandlerFunc(s.listEvents)))
	mux.Handle("GET /v1/events/{id}", s.require(authn.Read, http.HandlerFunc(s.getEvent)))
	mux.Handle("GET /v1/calendars", s.require(authn.Read, http.HandlerFunc(s.listCalendars)))
	mux.Handle("PATCH /v1/calendars/{id}", s.require(authn.Write, http.HandlerFunc(s.patchCalendar)))
	mux.Handle("GET /v1/settings", s.require(authn.Read, http.HandlerFunc(s.getSettings)))
	mux.Handle("PUT /v1/settings", s.require(authn.Write, http.HandlerFunc(s.putSettings)))
	mux.Handle("GET /v1/freebusy", s.require(authn.Read, http.HandlerFunc(s.freeBusy)))
}

func (s *Server) store() *agenda.Store {
	if s.Store != nil {
		return s.Store
	}
	return &agenda.Store{DB: s.Auth.DB}
}

// fail maps store errors to the documented error envelope.
func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	var ve *agenda.ValidationError
	switch {
	case errors.As(err, &ve):
		WriteError(w, http.StatusBadRequest, CodeValidation, ve.Msg, ve.Field)
	case errors.Is(err, agenda.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "not found", "")
	default:
		s.internalErr(w, what, err)
	}
}

func parseInstant(w http.ResponseWriter, q, field string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, q)
	if err != nil {
		WriteError(w, http.StatusBadRequest, CodeValidation, field+" must be an RFC 3339 instant", field)
		return t, false
	}
	return t.UTC(), true
}

// parseWindow reads required from/to.
func parseWindow(w http.ResponseWriter, r *http.Request) (from, to time.Time, ok bool) {
	q := r.URL.Query()
	if from, ok = parseInstant(w, q.Get("from"), "from"); !ok {
		return
	}
	if to, ok = parseInstant(w, q.Get("to"), "to"); !ok {
		return
	}
	if !to.After(from) {
		WriteError(w, http.StatusBadRequest, CodeValidation, "to must be after from", "to")
		return from, to, false
	}
	if to.Sub(from) > maxWindow {
		WriteError(w, http.StatusBadRequest, CodeValidation, "window may span at most 400 days", "to")
		return from, to, false
	}
	return from, to, true
}

func splitIDs(v string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func etagMatches(header, etag string) bool {
	for _, p := range strings.Split(header, ",") {
		p = strings.TrimPrefix(strings.TrimSpace(p), "W/")
		if p == "*" || p == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

func agendaETag(rev int64, from, to time.Time, cals []string) string {
	h := sha256.Sum256([]byte(from.Format(time.RFC3339Nano) + "|" + to.Format(time.RFC3339Nano) + "|" + strings.Join(cals, ",")))
	return fmt.Sprintf(`W/"rev-%d-%s"`, rev, hex.EncodeToString(h[:4]))
}

func (s *Server) agenda(w http.ResponseWriter, r *http.Request) {
	from, to, ok := parseWindow(w, r)
	if !ok {
		return
	}
	cals := splitIDs(r.URL.Query().Get("calendars"))
	st := s.store()
	rev, err := st.Rev(r.Context())
	if err != nil {
		s.internalErr(w, "agenda rev", err)
		return
	}
	if et := agendaETag(rev, from, to, cals); etagMatches(r.Header.Get("If-None-Match"), et) {
		w.Header().Set("ETag", et)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	res, err := st.Agenda(r.Context(), from, to, cals)
	if err != nil {
		s.fail(w, "agenda", err)
		return
	}
	w.Header().Set("ETag", agendaETag(res.Rev, from, to, cals)) // rev of the snapshot actually read
	WriteJSON(w, http.StatusOK, AgendaResponse{Rev: res.Rev, From: res.From, To: res.To, Items: res.Items})
}

func (s *Server) getEvent(w http.ResponseWriter, r *http.Request) {
	ev, err := s.store().Event(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, "event", err)
		return
	}
	WriteJSON(w, http.StatusOK, ev)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lq := agenda.ListQuery{Calendar: q.Get("calendar"), Q: strings.TrimSpace(q.Get("q")), Cursor: q.Get("cursor")}
	if v := q.Get("from"); v != "" {
		t, ok := parseInstant(w, v, "from")
		if !ok {
			return
		}
		lq.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, ok := parseInstant(w, v, "to")
		if !ok {
			return
		}
		lq.To = &t
	}
	if lq.From != nil && lq.To != nil && !lq.To.After(*lq.From) {
		WriteError(w, http.StatusBadRequest, CodeValidation, "to must be after from", "to")
		return
	}
	if lq.Q == "" && (lq.From == nil || lq.To == nil) {
		WriteError(w, http.StatusBadRequest, CodeValidation, "from and to are required unless q is set", "from")
		return
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			WriteError(w, http.StatusBadRequest, CodeValidation, "limit must be 1..200", "limit")
			return
		}
		lq.Limit = n
	}
	page, err := s.store().List(r.Context(), lq)
	if err != nil {
		s.fail(w, "events list", err)
		return
	}
	WriteJSON(w, http.StatusOK, page)
}

func (s *Server) listCalendars(w http.ResponseWriter, r *http.Request) {
	items, err := s.store().Calendars(r.Context())
	if err != nil {
		s.fail(w, "calendars", err)
		return
	}
	WriteJSON(w, http.StatusOK, CalendarList{Items: items})
}

func (s *Server) patchCalendar(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	var raw map[string]json.RawMessage
	if err == nil {
		err = json.Unmarshal(body, &raw)
	}
	if err != nil || raw == nil {
		WriteError(w, http.StatusBadRequest, CodeValidation, "body must be a JSON object", "")
		return
	}
	var p agenda.CalendarPatch
	if v, ok := raw["visible"]; ok {
		var b bool
		if json.Unmarshal(v, &b) != nil {
			WriteError(w, http.StatusBadRequest, CodeValidation, "visible must be a boolean", "visible")
			return
		}
		p.Visible = &b
	}
	if v, ok := raw["colorOverride"]; ok {
		var c *string
		if json.Unmarshal(v, &c) != nil {
			WriteError(w, http.StatusBadRequest, CodeValidation, "colorOverride must be a string or null", "colorOverride")
			return
		}
		p.ColorOverride = &c
	}
	cal, vis, err := s.store().PatchCalendar(r.Context(), r.PathValue("id"), p)
	if err != nil {
		s.fail(w, "patch calendar", err)
		return
	}
	if vis && s.Syncer != nil { // newly visible calendars were not being synced; catch up now
		if acc, err := s.store().AccountOf(r.Context(), cal.ID); err == nil {
			_, _ = s.Syncer.TriggerManual(context.WithoutCancel(r.Context()), acc)
		}
	}
	WriteJSON(w, http.StatusOK, cal)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	v, err := s.store().Settings(r.Context())
	if err != nil {
		s.fail(w, "settings", err)
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in agenda.SettingsInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		WriteError(w, http.StatusBadRequest, CodeValidation, "body must be a settings JSON object", "")
		return
	}
	v, err := s.store().PutSettings(r.Context(), in)
	if err != nil {
		s.fail(w, "put settings", err)
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

func (s *Server) freeBusy(w http.ResponseWriter, r *http.Request) {
	from, to, ok := parseWindow(w, r)
	if !ok {
		return
	}
	fb, err := s.store().FreeBusy(r.Context(), from, to)
	if err != nil {
		s.fail(w, "freebusy", err)
		return
	}
	WriteJSON(w, http.StatusOK, fb)
}
