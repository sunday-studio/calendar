package api

import (
	"net/http"
	"regexp"
	"time"

	"calendar/server/internal/authn"
	"calendar/server/internal/db"
)

var apnsTokenRE = regexp.MustCompile(`^[0-9a-fA-F]{32,200}$`)

func (s *Server) pushRoutes(mux *http.ServeMux) {
	mux.Handle("PUT /v1/devices/self/push", s.require(authn.Read, http.HandlerFunc(s.registerPush)))
	mux.Handle("DELETE /v1/devices/self/push", s.require(authn.Read, http.HandlerFunc(s.unregisterPush)))
	mux.Handle("GET /v1/summary", s.require(authn.Read, http.HandlerFunc(s.daySummary)))
}

// registerPush stores this device's APNs token. A device has one live token: registering
// replaces any earlier one for the same API token.
func (s *Server) registerPush(w http.ResponseWriter, r *http.Request) {
	var in struct {
		APNsToken   string `json:"apnsToken"`
		Environment string `json:"environment"`
		BundleID    string `json:"bundleId"`
	}
	if !decodeBody(w, r, &in, true) {
		return
	}
	switch {
	case !apnsTokenRE.MatchString(in.APNsToken):
		badRequest(w, "apnsToken", "apnsToken must be the hex device token")
		return
	case in.Environment != "sandbox" && in.Environment != "production":
		badRequest(w, "environment", "environment must be sandbox or production")
		return
	case len(in.BundleID) > 200:
		badRequest(w, "bundleId", "too long")
		return
	}
	p := authn.FromContext(r.Context())
	err := s.Auth.DB.Write(r.Context(), func(tx *db.Tx) error {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM push_endpoints WHERE token_id=?`, p.TokenID); err != nil {
			return err
		}
		var b any
		if in.BundleID != "" {
			b = in.BundleID
		}
		_, err := tx.ExecContext(r.Context(), `INSERT INTO push_endpoints(token_id,apns_token,env,bundle_id,updated_at) VALUES (?,?,?,?,?)`,
			p.TokenID, in.APNsToken, in.Environment, b, time.Now().Unix())
		return err
	})
	if err != nil {
		s.internalErr(w, "register push", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unregisterPush(w http.ResponseWriter, r *http.Request) {
	p := authn.FromContext(r.Context())
	if _, err := s.Auth.DB.W.ExecContext(r.Context(), `DELETE FROM push_endpoints WHERE token_id=?`, p.TokenID); err != nil {
		s.internalErr(w, "unregister push", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) daySummary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.store().DaySummary(r.Context(), r.URL.Query().Get("date"))
	if err != nil {
		s.fail(w, "summary", err)
		return
	}
	WriteJSON(w, http.StatusOK, sum)
}
