package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"calendar/server/internal/authn"
)

func (s *Server) tokenRoutes(mux *http.ServeMux) {
	mux.Handle("GET /v1/tokens", s.require(authn.Admin, http.HandlerFunc(s.listTokens)))
	mux.Handle("POST /v1/tokens", s.require(authn.Admin, http.HandlerFunc(s.createToken)))
	mux.Handle("DELETE /v1/tokens/{id}", s.require(authn.Admin, http.HandlerFunc(s.revokeToken)))
}

type tokenJSON struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Scope      string     `json:"scope"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

func toTokenJSON(t authn.TokenInfo) tokenJSON {
	j := tokenJSON{ID: t.ID, Name: t.Name, Kind: t.Kind, Scope: string(t.Scope), Prefix: t.Prefix, CreatedAt: t.CreatedAt.UTC(), LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt}
	if j.LastUsedAt != nil {
		u := j.LastUsedAt.UTC()
		j.LastUsedAt = &u
	}
	if j.RevokedAt != nil {
		u := j.RevokedAt.UTC()
		j.RevokedAt = &u
	}
	return j
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	l, err := s.Auth.List(r.Context())
	if err != nil {
		s.internalErr(w, "list tokens", err)
		return
	}
	out := make([]tokenJSON, 0, len(l))
	for _, t := range l {
		out = append(out, toTokenJSON(t))
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Kind  string `json:"kind"`
		Scope string `json:"scope"`
	}
	if !decodeBody(w, r, &in, true) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	switch {
	case in.Name == "" || len(in.Name) > 100:
		badRequest(w, "name", "name is required (at most 100 characters)")
		return
	case in.Kind != "device" && in.Kind != "script":
		badRequest(w, "kind", "kind must be device or script")
		return
	}
	sc, err := authn.ParseScope(in.Scope)
	if err != nil {
		badRequest(w, "scope", err.Error())
		return
	}
	id, secret, err := s.Auth.Create(r.Context(), in.Name, in.Kind, sc)
	if err != nil {
		s.internalErr(w, "create token", err)
		return
	}
	l, err := s.Auth.List(r.Context())
	if err != nil {
		s.internalErr(w, "list tokens", err)
		return
	}
	for _, t := range l {
		if t.ID == id {
			WriteJSON(w, http.StatusCreated, struct {
				tokenJSON
				Secret string `json:"secret"`
			}{toTokenJSON(t), secret})
			return
		}
	}
	s.internalErr(w, "create token", errors.New("created token not found"))
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == authn.FromContext(r.Context()).TokenID {
		badRequest(w, "id", "you cannot revoke the token you are using")
		return
	}
	switch err := s.Auth.Revoke(r.Context(), id); {
	case errors.Is(err, authn.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such active token", "")
	case err != nil:
		s.internalErr(w, "revoke token", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
