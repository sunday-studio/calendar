package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"calendar/server/internal/accounts"
	"calendar/server/internal/authn"
)

// Syncer is what POST /v1/sync needs from the sync manager.
type Syncer interface {
	TriggerManual(ctx context.Context, accountID string) (int, error)
}

func (s *Server) syncRoutes(mux *http.ServeMux) {
	mux.Handle("POST /v1/sync", s.require(authn.Read, http.HandlerFunc(s.triggerSync)))
}

func (s *Server) triggerSync(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AccountID string `json:"accountId"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err == nil && len(body) > 0 {
		err = json.Unmarshal(body, &in)
	}
	if err != nil {
		WriteError(w, http.StatusBadRequest, CodeValidation, "body must be JSON {accountId?}", "")
		return
	}
	n, err := s.Syncer.TriggerManual(r.Context(), in.AccountID)
	switch {
	case errors.Is(err, accounts.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such account", "accountId")
	case err != nil:
		s.Log.Error("trigger sync", "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "internal error", "")
	default:
		WriteJSON(w, http.StatusAccepted, map[string]any{"queued": n})
	}
}
