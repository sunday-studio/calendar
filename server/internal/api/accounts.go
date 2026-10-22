package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"calendar/server/internal/accounts"
	"calendar/server/internal/authn"
	"calendar/server/internal/domain"
)

func (s *Server) accountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/google/callback", s.googleCallback)
	mux.Handle("GET /v1/accounts", s.require(authn.Read, http.HandlerFunc(s.listAccounts)))
	mux.Handle("POST /v1/accounts/ics", s.require(authn.Admin, http.HandlerFunc(s.addICS)))
	mux.Handle("POST /v1/accounts/connect", s.require(authn.Admin, http.HandlerFunc(s.connectAccount)))
	mux.Handle("GET /v1/accounts/{id}", s.require(authn.Read, http.HandlerFunc(s.getAccount)))
	mux.Handle("PATCH /v1/accounts/{id}", s.require(authn.Write, http.HandlerFunc(s.patchAccount)))
	mux.Handle("DELETE /v1/accounts/{id}", s.require(authn.Admin, http.HandlerFunc(s.deleteAccount)))
	mux.Handle("POST /v1/accounts/{id}/reconnect", s.require(authn.Admin, http.HandlerFunc(s.reconnectAccount)))
}

// WireStatus maps stored status to the wire enum (active|syncing|needs_reauth|error|paused).
func WireStatus(s string) string { return apiStatus(s) }

func apiStatus(s string) string {
	switch s {
	case accounts.StatusOK:
		return "active"
	case accounts.StatusConnecting:
		return "syncing"
	case accounts.StatusDisabled:
		return "paused"
	}
	return s
}

func toAccount(a domain.Account) Account {
	return Account{
		ID: a.ID, Provider: a.Provider, Email: a.Email, DisplayName: a.DisplayName,
		Color: a.Color, Status: apiStatus(a.Status), StatusDetail: a.StatusDetail,
		LastSyncAt: a.LastSyncAt, LastOKAt: a.LastOKAt, CreatedAt: a.CreatedAt,
	}
}

func (s *Server) accountErr(w http.ResponseWriter, err error) {
	var icsErr *accounts.ICSInvalidError
	switch {
	case errors.Is(err, accounts.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such account", "")
	case errors.As(err, &icsErr):
		WriteError(w, http.StatusUnprocessableEntity, CodeValidation, icsErr.Msg, "url")
	case errors.Is(err, accounts.ErrInvalidColor):
		WriteError(w, http.StatusUnprocessableEntity, CodeValidation, "unknown palette colour", "color")
	case errors.Is(err, accounts.ErrUnsupported):
		WriteError(w, http.StatusUnprocessableEntity, CodeValidation, "unsupported provider", "provider")
	case errors.Is(err, accounts.ErrNotConfigured):
		WriteError(w, http.StatusServiceUnavailable, "unavailable", "Google sign-in is not configured on the hub", "")
	default:
		s.Log.Error("accounts", "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "internal error", "")
	}
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	l, err := s.Accounts.List(r.Context())
	if err != nil {
		s.accountErr(w, err)
		return
	}
	out := AccountList{Items: make([]Account, 0, len(l))}
	for _, a := range l {
		out.Items = append(out.Items, toAccount(a))
	}
	WriteJSON(w, http.StatusOK, out)
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	a, err := s.Accounts.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.accountErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, toAccount(*a))
}

func (s *Server) patchAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DisplayName *string `json:"displayName"`
		Color       *string `json:"color"`
		URL         *string `json:"url"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		WriteError(w, http.StatusUnprocessableEntity, CodeValidation, "invalid JSON body", "")
		return
	}
	if body.URL != nil {
		// A feed URL can embed a secret token: changing it is an admin action, and it is validated
		// (fetched + parsed) before anything is saved.
		if !authn.FromContext(r.Context()).Scope.Allows(authn.Admin) {
			WriteError(w, http.StatusForbidden, CodeForbidden, "changing a feed URL needs an admin token", "url")
			return
		}
		if body.Color != nil && !accounts.ValidColor(*body.Color) {
			s.accountErr(w, accounts.ErrInvalidColor)
			return
		}
		if _, err := s.Accounts.SetICSURL(r.Context(), r.PathValue("id"), *body.URL); err != nil {
			if errors.Is(err, accounts.ErrUnsupported) {
				WriteError(w, http.StatusUnprocessableEntity, CodeValidation, "only calendar feeds have a URL", "url")
				return
			}
			s.accountErr(w, err)
			return
		}
	}
	a, err := s.Accounts.Patch(r.Context(), r.PathValue("id"), accounts.Patch{DisplayName: body.DisplayName, Color: body.Color})
	if err != nil {
		s.accountErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, toAccount(*a))
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.Accounts.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.accountErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) connectAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider        string `json:"provider"`
		ReauthAccountID string `json:"reauthAccountId"`
		RedirectURI     string `json:"redirectUri"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		WriteError(w, http.StatusUnprocessableEntity, CodeValidation, "invalid JSON body", "")
		return
	}
	s.startConnect(w, r, body.Provider, body.ReauthAccountID)
}

func (s *Server) reconnectAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.Accounts.Get(r.Context(), id)
	if err != nil {
		s.accountErr(w, err)
		return
	}
	if a.Provider == "ics" {
		WriteError(w, http.StatusUnprocessableEntity, CodeValidation, "calendar feeds reconnect by changing their URL (PATCH url)", "url")
		return
	}
	s.startConnect(w, r, a.Provider, id)
}

func (s *Server) addICS(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL   string `json:"url"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		WriteError(w, http.StatusUnprocessableEntity, CodeValidation, "invalid JSON body", "")
		return
	}
	if strings.TrimSpace(body.URL) == "" {
		WriteError(w, http.StatusUnprocessableEntity, CodeValidation, "url is required", "url")
		return
	}
	a, created, err := s.Accounts.CreateICS(r.Context(), body.URL, body.Name, body.Color)
	if err != nil {
		s.accountErr(w, err)
		return
	}
	code := http.StatusCreated
	if !created {
		code = http.StatusOK
	}
	WriteJSON(w, code, toAccount(*a))
}

func (s *Server) startConnect(w http.ResponseWriter, r *http.Request, provider, reauth string) {
	p := authn.FromContext(r.Context())
	u, st, err := s.Accounts.Connect(r.Context(), provider, p.TokenID, reauth)
	if err != nil {
		s.accountErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"authUrl": u, "state": st})
}

// googleCallback is the only unauthenticated route; the single-use state is the guard.
func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	loc := s.Accounts.Callback(r.Context(), q.Get("state"), q.Get("code"), q.Get("error"))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, loc, http.StatusFound)
}
