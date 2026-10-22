package api

import (
	"errors"
	"net/http"
	"strconv"

	"calendar/server/internal/authn"
	"calendar/server/internal/automate"
)

func (s *Server) automationRoutes(mux *http.ServeMux) {
	a := func(h http.HandlerFunc) http.Handler { return s.require(authn.Admin, h) }
	mux.Handle("GET /v1/automations", a(s.listAutomations))
	mux.Handle("POST /v1/automations", a(s.createAutomation))
	mux.Handle("GET /v1/automations/{id}", a(s.getAutomation))
	mux.Handle("PATCH /v1/automations/{id}", a(s.patchAutomation))
	mux.Handle("DELETE /v1/automations/{id}", a(s.deleteAutomation))
	mux.Handle("POST /v1/automations/{id}/test", a(s.testAutomation))
	mux.Handle("GET /v1/automations/{id}/runs", a(s.automationRuns))
}

func (s *Server) automationFail(w http.ResponseWriter, err error) {
	var ve *automate.ValidationError
	switch {
	case errors.As(err, &ve):
		WriteError(w, http.StatusBadRequest, CodeValidation, ve.Msg, ve.Field)
	case errors.Is(err, automate.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such automation", "")
	default:
		s.internalErr(w, "automation", err)
	}
}

func (s *Server) listAutomations(w http.ResponseWriter, r *http.Request) {
	l, err := s.Automations.List(r.Context())
	if err != nil {
		s.automationFail(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": l})
}

func (s *Server) getAutomation(w http.ResponseWriter, r *http.Request) {
	a, err := s.Automations.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.automationFail(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, a)
}

func (s *Server) createAutomation(w http.ResponseWriter, r *http.Request) {
	var in automate.Input
	if !decodeBody(w, r, &in, true) {
		return
	}
	a, err := s.Automations.Create(r.Context(), in)
	if err != nil {
		s.automationFail(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, a)
}

func (s *Server) patchAutomation(w http.ResponseWriter, r *http.Request) {
	var in automate.Input
	if !decodeBody(w, r, &in, true) {
		return
	}
	a, err := s.Automations.Patch(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.automationFail(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, a)
}

func (s *Server) deleteAutomation(w http.ResponseWriter, r *http.Request) {
	if err := s.Automations.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.automationFail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) testAutomation(w http.ResponseWriter, r *http.Request) {
	run, err := s.Automations.Test(r.Context(), r.PathValue("id"))
	if err != nil {
		s.automationFail(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, run)
}

func (s *Server) automationRuns(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Automations.Get(r.Context(), r.PathValue("id")); err != nil {
		s.automationFail(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.Automations.Runs(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		s.automationFail(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": runs})
}
