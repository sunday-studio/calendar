package api

import (
	"net/http"

	"calendar/server/internal/authn"
	"calendar/server/internal/hubmcp"
)

// mcpRoutes mounts the hub's MCP server and the human-approval endpoints for its drafts.
func (s *Server) mcpRoutes(mux *http.ServeMux) {
	h := s.require(authn.Read, s.limitMCP(s.MCP.Handler()))
	mux.Handle("POST /mcp", h)
	mux.Handle("GET /mcp", h)
	mux.Handle("DELETE /mcp", h)
	mux.Handle("GET /v1/mcp/confirmations/{token}", s.require(authn.Write, http.HandlerFunc(s.getConfirmation)))
	mux.Handle("POST /v1/mcp/confirmations/{token}/approve", s.require(authn.Write, http.HandlerFunc(s.approveConfirmation)))
}

// humanOnly allows device tokens only: a script/agent token must not approve its own drafts.
func humanOnly(w http.ResponseWriter, r *http.Request) (*authn.Principal, bool) {
	p := authn.FromContext(r.Context())
	if p.Kind != "device" {
		WriteError(w, http.StatusForbidden, CodeForbidden, "only a paired device (token kind device) can approve agent drafts", "")
		return nil, false
	}
	return p, true
}

func (s *Server) confirmationFail(w http.ResponseWriter, ce *hubmcp.ConfError) {
	switch ce.Code {
	case hubmcp.CodeInvalid:
		WriteError(w, http.StatusNotFound, CodeNotFound, ce.Msg, "")
	case hubmcp.CodeExpired:
		WriteError(w, http.StatusNotFound, CodeNotFound, ce.Msg, "")
	case hubmcp.CodeUsed:
		WriteError(w, http.StatusConflict, CodeConflict, ce.Msg, "")
	default:
		WriteError(w, http.StatusInternalServerError, "internal", "internal error", "")
	}
}

func (s *Server) getConfirmation(w http.ResponseWriter, r *http.Request) {
	if _, ok := humanOnly(w, r); !ok {
		return
	}
	c, ce := s.MCP.Describe(r.Context(), r.PathValue("token"))
	if ce != nil {
		s.confirmationFail(w, ce)
		return
	}
	WriteJSON(w, http.StatusOK, c)
}

func (s *Server) approveConfirmation(w http.ResponseWriter, r *http.Request) {
	p, ok := humanOnly(w, r)
	if !ok {
		return
	}
	c, ce := s.MCP.Approve(r.Context(), r.PathValue("token"), p.TokenID)
	if ce != nil {
		if ce.Code == hubmcp.CodeInvalid && c.ID != "" { // approving your own draft
			WriteError(w, http.StatusForbidden, CodeForbidden, ce.Msg, "")
			return
		}
		s.confirmationFail(w, ce)
		return
	}
	WriteJSON(w, http.StatusOK, c)
}
