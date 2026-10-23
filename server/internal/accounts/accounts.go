// Package accounts owns the accounts table: connect/reconnect via OAuth, CRUD, and the
// status transitions of ENGINEERING §5.4 (used by the sync worker too).
package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/trace"

	"calendar/server/internal/db"
	"calendar/server/internal/domain"
	"calendar/server/internal/oauth"
	"calendar/server/internal/provider/ics"
	"calendar/server/internal/telemetry"
	"calendar/server/internal/vault"
)

// lifecycle records an account lifecycle event (connected, reconnected, removed, needs_reauth, error) as
// a span event on the active span, an info log with trace ids and calendar.account.events. It never
// carries the e-mail address or display name.
func lifecycle(ctx context.Context, event, id, prov string, kv ...any) {
	trace.SpanFromContext(ctx).AddEvent("account."+event, trace.WithAttributes(telemetry.AccountID(id), telemetry.Provider(prov)))
	telemetry.Inc(ctx, "account.events", telemetry.Provider(prov), telemetry.Attr("event", event))
	telemetry.Log().InfoContext(ctx, "account "+event, append([]any{"account", id, "provider", prov}, kv...)...)
}

// Palette is the account colour order; new accounts get the first unused key.
var Palette = []string{"cobalt", "rose", "ochre", "mauve", "verdigris", "slate", "moss", "clay"}

// Status values stored in accounts.status.
const (
	StatusConnecting  = "connecting"
	StatusOK          = "ok"
	StatusNeedsReauth = "needs_reauth"
	StatusError       = "error"
	StatusDisabled    = "disabled"
)

// Callback error codes (calendar://connected?error=CODE).
const (
	ErrCodeInvalidState     = "invalid_state"
	ErrCodeExchange         = "exchange_failed"
	ErrCodeIdentityMismatch = "identity_mismatch"
	ErrCodeNoRefreshToken   = "no_refresh_token"
	ErrCodeInternal         = "internal"
)

var (
	ErrNotFound      = errors.New("accounts: not found")
	ErrNotConfigured = errors.New("accounts: google sign-in is not configured")
	ErrInvalidColor  = errors.New("accounts: unknown palette colour")
	ErrUnsupported   = errors.New("accounts: unsupported provider")
)

type Service struct {
	DB       *db.DB
	Vault    *vault.Vault
	Google   *oauth.Google // nil = not configured
	Sessions oauth.Sessions
	ICS      *ics.Provider // nil = ICS feeds unavailable
	Now      func() time.Time
	// OnStatus, if set, is called after a commit that changed an account's stored status.
	OnStatus func(id, status string)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// secretPayload is the vault-sealed JSON in accounts.secret for Google.
type secretPayload struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshToken unseals an account's Google refresh token (for the sync worker).
func (s *Service) RefreshToken(ctx context.Context, id string) (string, error) {
	var sealed []byte
	if err := s.DB.R.QueryRowContext(ctx, `SELECT secret FROM accounts WHERE id=?`, id).Scan(&sealed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	pt, err := s.Vault.Open(sealed, []byte(id))
	if err != nil {
		return "", err
	}
	var p secretPayload
	if err := json.Unmarshal(pt, &p); err != nil {
		return "", err
	}
	return p.RefreshToken, nil
}

const cols = `id,provider,external_id,email,COALESCE(display_name,''),color,status,COALESCE(status_detail,''),last_sync_at,last_ok_at,created_at`

type scanner interface{ Scan(...any) error }

func scan(r scanner) (domain.Account, error) {
	var a domain.Account
	var ls, lo sql.NullInt64
	var created int64
	if err := r.Scan(&a.ID, &a.Provider, &a.ExternalID, &a.Email, &a.DisplayName, &a.Color, &a.Status, &a.StatusDetail, &ls, &lo, &created); err != nil {
		return a, err
	}
	if ls.Valid {
		t := time.Unix(ls.Int64, 0).UTC()
		a.LastSyncAt = &t
	}
	if lo.Valid {
		t := time.Unix(lo.Int64, 0).UTC()
		a.LastOKAt = &t
	}
	a.CreatedAt = time.Unix(created, 0).UTC()
	return a, nil
}

func (s *Service) List(ctx context.Context) ([]domain.Account, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+cols+` FROM accounts ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Account{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, id string) (*domain.Account, error) {
	a, err := scan(s.DB.R.QueryRowContext(ctx, `SELECT `+cols+` FROM accounts WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

type Patch struct {
	DisplayName *string
	Color       *string
}

func ValidColor(c string) bool {
	for _, p := range Palette {
		if p == c {
			return true
		}
	}
	return false
}

func (s *Service) Patch(ctx context.Context, id string, p Patch) (*domain.Account, error) {
	if p.Color != nil && !ValidColor(*p.Color) {
		return nil, ErrInvalidColor
	}
	err := s.DB.Write(ctx, func(tx *db.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE id=?`, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		if p.DisplayName != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE accounts SET display_name=? WHERE id=?`, *p.DisplayName, id); err != nil {
				return err
			}
		}
		if p.Color != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE accounts SET color=? WHERE id=?`, *p.Color, id); err != nil {
				return err
			}
		}
		return tx.Change(ctx, "account", id, "upsert")
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete disconnects an account and drops everything cached for it.
func (s *Service) Delete(ctx context.Context, id string) error {
	prov := ""
	_ = s.DB.R.QueryRowContext(ctx, `SELECT provider FROM accounts WHERE id=?`, id).Scan(&prov)
	err := s.DB.Write(ctx, func(tx *db.Tx) error {
		// FK cascades remove calendars -> events -> attendees/occurrences/overrides.
		res, err := tx.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		for _, q := range []string{
			`DELETE FROM sync_runs WHERE account_id=?`,
			`DELETE FROM oauth_sessions WHERE reauth_account_id=?`,
		} {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
		return tx.Change(ctx, "account", id, "delete")
	})
	switch {
	case err == nil:
		lifecycle(ctx, "removed", id, prov)
	case !errors.Is(err, ErrNotFound):
		telemetry.RecordError(ctx, "accounts", err, telemetry.Operation("delete"))
	}
	return err
}

// ---- connect flow ----

// Connect starts sign-in. reauthAccountID (optional) ties the session to an existing account.
func (s *Service) Connect(ctx context.Context, provider, tokenID, reauthAccountID string) (authURL, state string, err error) {
	if provider != "google" {
		return "", "", ErrUnsupported
	}
	if s.Google == nil {
		return "", "", ErrNotConfigured
	}
	if reauthAccountID != "" {
		if _, err := s.Get(ctx, reauthAccountID); err != nil {
			return "", "", err
		}
	}
	verifier := oauth.NewVerifier()
	state, err = s.Sessions.Create(ctx, provider, verifier, tokenID, reauthAccountID)
	if err != nil {
		telemetry.RecordError(ctx, "accounts", err, telemetry.Operation("connect"))
		return "", "", err
	}
	return s.Google.AuthURL(state, verifier), state, nil
}

func redirect(q url.Values) string { return "calendar://connected?" + q.Encode() }

func errRedirect(code string) string { return redirect(url.Values{"error": {code}}) }

// Callback completes sign-in and returns the calendar:// URL to 302 to. It never returns
// an error: every failure becomes calendar://connected?error=CODE.
func (s *Service) Callback(ctx context.Context, state, code, providerErr string) string {
	ctx, span := telemetry.StartSpan(ctx, "accounts.oauth_callback", telemetry.Provider("google"))
	defer span.End()
	sess, err := s.Sessions.Consume(ctx, state)
	if err != nil {
		if errors.Is(err, oauth.ErrInvalidState) {
			telemetry.Log().InfoContext(ctx, "oauth callback with an unknown or expired state")
			telemetry.Inc(ctx, "account.events", telemetry.Provider("google"), telemetry.Attr("event", "invalid_state"))
			return errRedirect(ErrCodeInvalidState)
		}
		telemetry.RecordError(ctx, "accounts", err, telemetry.Operation("oauth_session"))
		return errRedirect(ErrCodeInternal)
	}
	if providerErr != "" {
		// e.g. access_denied; pass Google's code through if it is a short token.
		if len(providerErr) > 40 {
			providerErr = "denied"
		}
		// the user said no (or Google did): an expected outcome, not a hub error
		telemetry.Log().InfoContext(ctx, "oauth sign-in declined", "reason", providerErr)
		telemetry.Inc(ctx, "account.events", telemetry.Provider("google"), telemetry.Attr("event", "declined"))
		return errRedirect(providerErr)
	}
	if s.Google == nil || code == "" {
		telemetry.RecordError(ctx, "accounts", errors.New("oauth callback without a code or google configuration"), telemetry.Operation("oauth_exchange"))
		return errRedirect(ErrCodeExchange)
	}
	ident, err := s.Google.Exchange(ctx, code, sess.Verifier)
	if err != nil {
		telemetry.RecordError(ctx, "accounts", err, telemetry.Operation("oauth_exchange"))
		return errRedirect(ErrCodeExchange)
	}
	acc, code2 := s.upsert(ctx, sess, ident)
	if code2 != "" {
		return errRedirect(code2)
	}
	return redirect(url.Values{"account": {acc}, "status": {StatusConnecting}})
}

// upsert returns (accountID, "") or ("", errorCode).
func (s *Service) upsert(ctx context.Context, sess *oauth.Session, ident *oauth.Identity) (string, string) {
	var resultID, errCode string
	created := false
	now := s.now().Unix()
	err := s.DB.Write(ctx, func(tx *db.Tx) error {
		var existingID, existingExt string
		err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE provider='google' AND external_id=?`, ident.Subject).Scan(&existingID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if sess.ReauthAccountID != "" {
			err := tx.QueryRowContext(ctx, `SELECT external_id FROM accounts WHERE id=?`, sess.ReauthAccountID).Scan(&existingExt)
			if errors.Is(err, sql.ErrNoRows) {
				errCode = ErrCodeInternal
				return nil
			}
			if err != nil {
				return err
			}
			if existingExt != ident.Subject {
				errCode = ErrCodeIdentityMismatch
				return nil
			}
		}
		if existingID == "" && ident.RefreshToken == "" {
			errCode = ErrCodeNoRefreshToken
			return nil
		}
		id := existingID
		isNew := id == ""
		created = isNew
		if isNew {
			id = ulid.Make().String()
		}
		var sealed []byte
		if ident.RefreshToken != "" {
			pt, _ := json.Marshal(secretPayload{RefreshToken: ident.RefreshToken})
			sealed, err = s.Vault.Seal(pt, []byte(id))
			if err != nil {
				return err
			}
		}
		if isNew {
			color, err := nextColor(ctx, tx)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,provider,external_id,email,display_name,color,status,secret,granted_scopes,next_sync_at,created_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
				id, "google", ident.Subject, ident.Email, ident.Name, color, StatusConnecting, sealed, ident.Scopes, now, now)
			if err != nil {
				return err
			}
		} else {
			// Existing account: refresh identity fields and secret; keep colour and display name.
			if sealed != nil {
				_, err = tx.ExecContext(ctx, `UPDATE accounts SET email=?, secret=?, granted_scopes=?, status=?, status_detail=NULL, fail_count=0, next_sync_at=? WHERE id=?`,
					ident.Email, sealed, ident.Scopes, StatusConnecting, now, id)
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE accounts SET email=?, status=?, status_detail=NULL, fail_count=0, next_sync_at=? WHERE id=?`,
					ident.Email, StatusConnecting, now, id)
			}
			if err != nil {
				return err
			}
		}
		resultID = id
		return tx.Change(ctx, "account", id, "upsert")
	})
	if err != nil {
		telemetry.RecordError(ctx, "accounts", err, telemetry.Operation("upsert"))
		return "", ErrCodeInternal
	}
	if errCode != "" {
		// identity mismatch, missing refresh token ...: the user can fix these, but a hub that keeps
		// producing them is worth seeing
		telemetry.Log().WarnContext(ctx, "account sign-in rejected", "code", errCode, "reauth", sess.ReauthAccountID != "")
		telemetry.Inc(ctx, "account.events", telemetry.Provider("google"), telemetry.Attr("event", errCode))
		return "", errCode
	}
	if created {
		lifecycle(ctx, "connected", resultID, "google")
	} else {
		lifecycle(ctx, "reconnected", resultID, "google")
	}
	if s.OnStatus != nil {
		s.OnStatus(resultID, StatusConnecting)
	}
	return resultID, ""
}

// SeedDev creates (or refreshes) a Google account with a sealed refresh token and no browser
// consent, for end-to-end runs against the fake Google. The caller must have checked CALENDAR_DEV.
// A second call with the same subject refreshes the token and puts the account back to connecting.
func (s *Service) SeedDev(ctx context.Context, subject, email, name, refreshToken string) (string, error) {
	id, code := s.upsert(ctx, &oauth.Session{Provider: "google"}, &oauth.Identity{
		Subject: subject, Email: email, Name: name, RefreshToken: refreshToken, Scopes: "https://www.googleapis.com/auth/calendar openid email profile"})
	if code != "" {
		return "", errors.New("accounts: seed failed: " + code)
	}
	return id, nil
}

func nextColor(ctx context.Context, tx *db.Tx) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT color FROM accounts`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	used := map[string]int{}
	total := 0
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return "", err
		}
		used[c]++
		total++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	for _, p := range Palette {
		if used[p] == 0 {
			return p, nil
		}
	}
	return Palette[total%len(Palette)], nil
}

// ---- status helpers (sync worker) ----

func (s *Service) setStatus(ctx context.Context, id, status, detail string, extra string, args ...any) error {
	changed := false
	prov := ""
	err := s.DB.Write(ctx, func(tx *db.Tx) error {
		var cur string
		if err := tx.QueryRowContext(ctx, `SELECT status, provider FROM accounts WHERE id=?`, id).Scan(&cur, &prov); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var d any
		if detail != "" {
			d = detail
		}
		q := `UPDATE accounts SET status=?, status_detail=?` + extra + ` WHERE id=?`
		a := append([]any{status, d}, args...)
		a = append(a, id)
		if _, err := tx.ExecContext(ctx, q, a...); err != nil {
			return err
		}
		if cur != status {
			changed = true
			return tx.Change(ctx, "account", id, "status")
		}
		return nil
	})
	if err == nil && changed {
		switch status {
		case StatusNeedsReauth:
			lifecycle(ctx, "needs_reauth", id, prov, "reason", "provider_rejected_credentials")
		case StatusError:
			lifecycle(ctx, "error", id, prov, "reason", "repeated_sync_failures")
		}
		if s.OnStatus != nil {
			s.OnStatus(id, status)
		}
	}
	return err
}

// MarkOK: first/next successful sync. Resets failures.
func (s *Service) MarkOK(ctx context.Context, id string) error {
	n := s.now().Unix()
	return s.setStatus(ctx, id, StatusOK, "", `, fail_count=0, last_sync_at=?, last_ok_at=?`, n, n)
}

// MarkNeedsReauth: invalid_grant / revoked consent / 401.
func (s *Service) MarkNeedsReauth(ctx context.Context, id, detail string) error {
	return s.setStatus(ctx, id, StatusNeedsReauth, detail, `, last_sync_at=?`, s.now().Unix())
}

// MarkError: sync or discovery failed. Increments fail_count.
func (s *Service) MarkError(ctx context.Context, id, detail string) error {
	return s.setStatus(ctx, id, StatusError, detail, `, fail_count=fail_count+1, last_sync_at=?`, s.now().Unix())
}

// MarkConnecting: back to the initial-sync state (e.g. after reconnect).
func (s *Service) MarkConnecting(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, StatusConnecting, "", `, fail_count=0, next_sync_at=?`, s.now().Unix())
}

// SetDisabled pauses (true) or resumes (false) an account.
func (s *Service) SetDisabled(ctx context.Context, id string, disabled bool) error {
	if disabled {
		return s.setStatus(ctx, id, StatusDisabled, "", ``)
	}
	return s.setStatus(ctx, id, StatusOK, "", ``)
}
