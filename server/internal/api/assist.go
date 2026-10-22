package api

import (
	"net/http"
	"strings"
	"time"

	"calendar/server/internal/agenda"
	"calendar/server/internal/alfather"
	"calendar/server/internal/authn"
	"calendar/server/internal/quickadd"
)

func (s *Server) assistRoutes(mux *http.ServeMux) {
	mux.Handle("POST /v1/freetime", s.require(authn.Read, http.HandlerFunc(s.freeTime)))
	mux.Handle("POST /v1/quick-add", s.require(authn.Read, http.HandlerFunc(s.quickAdd)))
	mux.Handle("GET /v1/people", s.require(authn.Read, http.HandlerFunc(s.people)))
}

func (s *Server) freeTime(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DurationMin        int    `json:"durationMin"`
		From               string `json:"from"`
		To                 string `json:"to"`
		IgnoreWorkingHours bool   `json:"ignoreWorkingHours"`
		DayPart            string `json:"dayPart"`
		Limit              int    `json:"limit"`
	}
	if !decodeBody(w, r, &in, true) {
		return
	}
	from, ok := parseInstant(w, in.From, "from")
	if !ok {
		return
	}
	to, ok := parseInstant(w, in.To, "to")
	if !ok {
		return
	}
	ft, err := s.store().FreeTime(r.Context(), agenda.FreeTimeQuery{DurationMin: in.DurationMin, From: from, To: to,
		IgnoreWorkingHours: in.IgnoreWorkingHours, DayPart: in.DayPart, Limit: in.Limit})
	if err != nil {
		s.fail(w, "freetime", err)
		return
	}
	WriteJSON(w, http.StatusOK, ft)
}

func (s *Server) parser() quickadd.Parser {
	if s.QuickAdd != nil {
		return s.QuickAdd
	}
	st := s.store()
	env := quickadd.HubEnv{DB: s.Auth.DB, Store: st}
	rules := &quickadd.RuleParser{Env: env, DefaultNow: st.Now}
	if s.Alfather != nil && s.Alfather.URL != "" {
		return &alfather.Parser{Cfg: *s.Alfather, Fallback: rules, Env: env, Now: st.Now, Log: s.Log}
	}
	return rules
}

// quickAdd turns text into a Draft. It never writes anything.
func (s *Server) quickAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
		Now  string `json:"now"`
		TZID string `json:"tzid"`
	}
	if !decodeBody(w, r, &in, true) {
		return
	}
	req := quickadd.Request{Text: in.Text, TZID: in.TZID}
	if in.Now != "" {
		t, err := time.Parse(time.RFC3339, in.Now)
		if err != nil {
			badRequest(w, "now", "now must be an RFC 3339 instant")
			return
		}
		req.Now = t
	}
	if req.TZID == "" {
		if st, err := s.store().Settings(r.Context()); err == nil && st.UserTZID != nil {
			req.TZID = *st.UserTZID
		}
	}
	d, err := s.parser().Parse(r.Context(), req)
	if err != nil {
		s.writeFail(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, d)
}

func (s *Server) people(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		badRequest(w, "q", "q is required")
		return
	}
	cs, err := quickadd.HubEnv{DB: s.Auth.DB, Store: s.store()}.Contacts(r.Context(), q)
	if err != nil {
		s.internalErr(w, "people", err)
		return
	}
	type person struct {
		Email    string     `json:"email"`
		Name     string     `json:"name"`
		Events   int        `json:"events"`
		LastSeen *time.Time `json:"lastSeen"`
	}
	out := make([]person, 0, len(cs))
	for _, c := range cs {
		p := person{Email: c.Email, Name: c.Name, Events: c.Count}
		if !c.Last.IsZero() && c.Last.Unix() > 0 {
			t := c.Last
			p.LastSeen = &t
		}
		out = append(out, p)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}
