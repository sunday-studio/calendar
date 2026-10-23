package hubmcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/trace"

	"calendar/server/internal/db"
	"calendar/server/internal/telemetry"
)

// ConfirmTTL is how long a draft can wait for a human.
const ConfirmTTL = 10 * time.Minute

const confirmPrefix = "cfm_"

// Machine-readable refusal codes returned by mutating tools and the approval endpoints.
const (
	CodeRequired = "confirmation_required" // no confirm_token given
	CodeInvalid  = "confirmation_invalid"  // unknown, wrong tool, or drafted by another token
	CodeExpired  = "confirmation_expired"
	CodeUsed     = "confirmation_used"
	CodePending  = "confirmation_pending" // drafted but no human has approved it yet
)

// ConfError is a confirmation refusal.
type ConfError struct{ Code, Msg string }

func (e *ConfError) Error() string { return e.Code + ": " + e.Msg }

// Confirmation is a stored approval handle (the token itself is never stored).
type Confirmation struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Summary    json.RawMessage `json:"summary"`
	Status     string          `json:"status"` // pending | approved | used | expired
	CreatedAt  time.Time       `json:"createdAt"`
	ExpiresAt  time.Time       `json:"expiresAt"`
	ApprovedAt *time.Time      `json:"approvedAt,omitempty"`
	TokenID    string          `json:"-"`
	Payload    json.RawMessage `json:"-"`
}

func hashToken(t string) []byte { h := sha256.Sum256([]byte(t)); return h[:] }

func (h *Hub) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Mint stores a pending confirmation drafted by API token tokenID and returns the one-time token.
func (h *Hub) Mint(ctx context.Context, tokenID, kind string, summary, payload any) (token string, c Confirmation, err error) {
	raw := make([]byte, 20)
	if _, err = rand.Read(raw); err != nil {
		return
	}
	token = confirmPrefix + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	sj, err := json.Marshal(summary)
	if err != nil {
		return
	}
	pj, err := json.Marshal(payload)
	if err != nil {
		return
	}
	now := h.now()
	c = Confirmation{ID: ulid.Make().String(), Kind: kind, Summary: sj, Status: "pending", CreatedAt: now.UTC(), ExpiresAt: now.Add(ConfirmTTL).UTC()}
	err = h.DB.Write(ctx, func(tx *db.Tx) error {
		// housekeeping: forget handles that expired more than a day ago
		if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_confirmations WHERE expires_at < ?`, now.Add(-24*time.Hour).Unix()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO mcp_confirmations(hash,id,kind,token_id,summary,payload,created_at,expires_at) VALUES (?,?,?,?,?,?,?,?)`,
			hashToken(token), c.ID, kind, tokenID, string(sj), string(pj), now.Unix(), c.ExpiresAt.Unix())
		return err
	})
	if err == nil {
		telemetry.Inc(ctx, "mcp.confirmations", telemetry.Attr("state", "minted"))
		trace.SpanFromContext(ctx).AddEvent("mcp.confirmation_minted", trace.WithAttributes(telemetry.Attr("kind", kind)))
	}
	return
}

func (h *Hub) load(ctx context.Context, token string) (Confirmation, *ConfError) {
	var c Confirmation
	var created, exp int64
	var appr, used sql.NullInt64
	var sum, pay string
	err := h.DB.R.QueryRowContext(ctx, `SELECT id,kind,token_id,summary,payload,created_at,expires_at,approved_at,used_at FROM mcp_confirmations WHERE hash=?`, hashToken(token)).
		Scan(&c.ID, &c.Kind, &c.TokenID, &sum, &pay, &created, &exp, &appr, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return c, &ConfError{CodeInvalid, "unknown confirm_token"}
	}
	if err != nil {
		telemetry.RecordError(ctx, "mcp", err, telemetry.Operation("confirm_load"))
		return c, &ConfError{"internal", "confirmation lookup failed"}
	}
	c.Summary, c.Payload = json.RawMessage(sum), json.RawMessage(pay)
	c.CreatedAt, c.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(exp, 0).UTC()
	c.Status = "pending"
	if appr.Valid {
		t := time.Unix(appr.Int64, 0).UTC()
		c.ApprovedAt, c.Status = &t, "approved"
	}
	switch {
	case used.Valid:
		c.Status = "used"
	case !h.now().Before(c.ExpiresAt):
		c.Status = "expired"
	}
	return c, nil
}

// Describe returns a confirmation for the approval sheet.
func (h *Hub) Describe(ctx context.Context, token string) (Confirmation, *ConfError) {
	return h.load(ctx, token)
}

// Approve records that a human accepted the draft. approver must not be the token that drafted
// it. Approving twice is a no-op.
func (h *Hub) Approve(ctx context.Context, token, approverTokenID string) (Confirmation, *ConfError) {
	c, ce := h.load(ctx, token)
	if ce != nil {
		return c, ce
	}
	switch c.Status {
	case "expired":
		telemetry.Inc(ctx, "mcp.confirmations", telemetry.Attr("state", "expired"))
		return c, &ConfError{CodeExpired, "this confirmation has expired; ask the agent to draft again"}
	case "used":
		return c, &ConfError{CodeUsed, "this confirmation was already used"}
	case "approved":
		return c, nil
	}
	if c.TokenID == approverTokenID {
		telemetry.Inc(ctx, "mcp.confirmations", telemetry.Attr("state", "denied"))
		return c, &ConfError{CodeInvalid, "a draft cannot be approved by the token that made it"}
	}
	now := h.now()
	err := h.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE mcp_confirmations SET approved_at=?, approved_by=? WHERE hash=? AND approved_at IS NULL AND used_at IS NULL`, now.Unix(), approverTokenID, hashToken(token))
		return err
	})
	if err != nil {
		telemetry.RecordError(ctx, "mcp", err, telemetry.Operation("approve"))
		return c, &ConfError{"internal", "could not record approval"}
	}
	telemetry.Inc(ctx, "mcp.confirmations", telemetry.Attr("state", "approved"))
	telemetry.Log().InfoContext(ctx, "mcp confirmation approved", "kind", c.Kind)
	return h.load(ctx, token)
}

// consume validates the token for kind and marks it used (single use, burned even when the
// action later fails). Only the API token that drafted it may spend it.
func (h *Hub) consume(ctx context.Context, token, kind, tokenID string) (Confirmation, *ConfError) {
	if token == "" {
		return Confirmation{}, &ConfError{CodeRequired, "this tool needs a confirm_token from a draft_* tool, approved by the user in the Calendar app"}
	}
	c, ce := h.load(ctx, token)
	if ce != nil {
		return c, ce
	}
	if c.Kind != kind || c.TokenID != tokenID {
		telemetry.Inc(ctx, "mcp.confirmations", telemetry.Attr("state", "denied"))
		return c, &ConfError{CodeInvalid, "confirm_token does not belong to this tool or caller"}
	}
	switch c.Status {
	case "expired":
		telemetry.Inc(ctx, "mcp.confirmations", telemetry.Attr("state", "expired"))
		return c, &ConfError{CodeExpired, "confirm_token expired; draft again and ask the user to approve"}
	case "used":
		return c, &ConfError{CodeUsed, "confirm_token was already used"}
	case "pending":
		return c, &ConfError{CodePending, "the user has not approved this draft yet (they approve it in the Calendar app)"}
	}
	var n int64
	err := h.DB.Write(ctx, func(tx *db.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE mcp_confirmations SET used_at=? WHERE hash=? AND used_at IS NULL AND approved_at IS NOT NULL AND expires_at>?`,
			h.now().Unix(), hashToken(token), h.now().Unix())
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	if err != nil {
		telemetry.RecordError(ctx, "mcp", err, telemetry.Operation("spend"))
		return c, &ConfError{"internal", "could not spend confirmation"}
	}
	if n == 0 { // lost a race with another call
		return c, &ConfError{CodeUsed, "confirm_token was already used"}
	}
	return c, nil
}

func (h *Hub) setOutcome(ctx context.Context, id, outcome string) {
	if err := h.DB.Write(context.WithoutCancel(ctx), func(tx *db.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE mcp_confirmations SET outcome=? WHERE id=?`, outcome, id)
		return err
	}); err != nil {
		telemetry.RecordError(ctx, "mcp", err, telemetry.Operation("set_outcome"))
	}
}

// Stats feeds /v1/status.
type Stats struct {
	Calls24h    int        `json:"calls24h"`
	Refused24h  int        `json:"refused24h"`
	Pending     int        `json:"pendingConfirmations"`
	LastCallAt  *time.Time `json:"lastCallAt"`
	Executed24h int        `json:"executed24h"`
}

// Stats counts audit rows.
func (h *Hub) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	now := h.now().Unix()
	since := now - 86400
	var last sql.NullInt64
	err := h.DB.R.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(ok=0),0), MAX(ts) FROM mcp_calls WHERE ts>=?`, since).Scan(&s.Calls24h, &s.Refused24h, &last)
	if err != nil {
		return s, err
	}
	if last.Valid {
		t := time.Unix(last.Int64, 0).UTC()
		s.LastCallAt = &t
	}
	if err = h.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_calls WHERE ts>=? AND ok=1 AND tool IN ('create_event','update_event','delete_event','rsvp_event')`, since).Scan(&s.Executed24h); err != nil {
		return s, err
	}
	err = h.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_confirmations WHERE used_at IS NULL AND expires_at>?`, now).Scan(&s.Pending)
	return s, err
}

func (h *Hub) audit(ctx context.Context, tokenID, tool string, ok bool, code string, took time.Duration) {
	okn := 0
	if ok {
		okn = 1
	}
	err := h.DB.Write(context.WithoutCancel(ctx), func(tx *db.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_calls(ts,token_id,tool,ok,code,ms) VALUES (?,?,?,?,?,?)`, h.now().Unix(), tokenID, tool, okn, nullStr(code), took.Milliseconds()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM mcp_calls WHERE ts < ?`, h.now().Add(-90*24*time.Hour).Unix())
		return err
	})
	if err != nil {
		telemetry.RecordError(ctx, "mcp", err, telemetry.Operation("audit"))
	}
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
