// Package oauth implements the hub side of "Connect Google": client config, single-use
// state + PKCE sessions (oauth_sessions) and the code exchange.
package oauth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"calendar/server/internal/db"
	"calendar/server/internal/telemetry"
)

const SessionTTL = 10 * time.Minute

var (
	ErrInvalidState = errors.New("oauth: state unknown, expired or already used")
	ErrNoIdentity   = errors.New("oauth: provider returned no usable identity")
)

// Google endpoints; tests override them with an httptest fake.
const (
	GoogleAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	GoogleTokenURL    = "https://oauth2.googleapis.com/token"
	GoogleUserinfoURL = "https://openidconnect.googleapis.com/v1/userinfo"
)

var GoogleScopes = []string{"https://www.googleapis.com/auth/calendar", "openid", "email", "profile"}

type ClientFile struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// LoadClientFile reads {client_id, client_secret} JSON.
func LoadClientFile(path string) (*ClientFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read google client file: %w", err)
	}
	var c ClientFile
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse google client file: %w", err)
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, errors.New("google client file needs client_id and client_secret")
	}
	return &c, nil
}

// Identity is who signed in.
type Identity struct {
	Subject, Email, Name string
	RefreshToken         string
	AccessToken          string
	Expiry               time.Time
	Scopes               string
}

type Google struct {
	cfg         oauth2.Config
	UserinfoURL string
	HTTP        *http.Client // optional
}

// NewGoogle builds a Google flow. redirectURL = PUBLIC_URL + "/auth/google/callback".
func NewGoogle(c ClientFile, redirectURL string) *Google {
	return &Google{
		cfg: oauth2.Config{
			ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURL: redirectURL,
			Scopes:   GoogleScopes,
			Endpoint: oauth2.Endpoint{AuthURL: GoogleAuthURL, TokenURL: GoogleTokenURL, AuthStyle: oauth2.AuthStyleInParams},
		},
		UserinfoURL: GoogleUserinfoURL,
	}
}

// SetEndpoints overrides Google's URLs (tests, e2e fake); an empty argument keeps the current one.
func (g *Google) SetEndpoints(auth, token, userinfo string) {
	if auth != "" {
		g.cfg.Endpoint.AuthURL = auth
	}
	if token != "" {
		g.cfg.Endpoint.TokenURL = token
	}
	if userinfo != "" {
		g.UserinfoURL = userinfo
	}
}

func (g *Google) AuthURL(state, verifier string) string {
	return g.cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "select_account consent"),
		oauth2.S256ChallengeOption(verifier))
}

// Exchange trades the code (+PKCE verifier) for tokens and resolves the identity.
func (g *Google) Exchange(ctx context.Context, code, verifier string) (*Identity, error) {
	ctx, span := telemetry.StartSpan(ctx, "oauth.exchange", telemetry.Provider("google"))
	defer span.End()
	// client span per call to Google's token and userinfo endpoints (templated URL, no query: the code never shows)
	hc := http.DefaultClient
	if g.HTTP != nil {
		hc = g.HTTP
	}
	traced := *hc
	traced.Transport = telemetry.WrapClient(hc.Transport, "google")
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &traced)
	tok, err := g.cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, err
	}
	id := &Identity{RefreshToken: tok.RefreshToken, AccessToken: tok.AccessToken, Expiry: tok.Expiry}
	id.Scopes, _ = tok.Extra("scope").(string)
	if raw, _ := tok.Extra("id_token").(string); raw != "" {
		parseIDToken(raw, id)
	}
	if id.Subject == "" || id.Email == "" {
		if err := g.userinfo(ctx, tok, id); err != nil {
			return nil, err
		}
	}
	if id.Subject == "" || id.Email == "" {
		return nil, ErrNoIdentity
	}
	return id, nil
}

// parseIDToken reads claims from an id_token received directly from the token endpoint
// over TLS (OIDC core 3.1.3.7 allows skipping signature validation in that case).
func parseIDToken(raw string, id *Identity) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return
	}
	var c struct{ Sub, Email, Name string }
	if json.Unmarshal(b, &c) == nil {
		id.Subject, id.Email, id.Name = c.Sub, c.Email, c.Name
	}
}

func (g *Google) userinfo(ctx context.Context, tok *oauth2.Token, id *Identity) error {
	cl := oauth2.NewClient(ctx, oauth2.StaticTokenSource(tok))
	req, _ := http.NewRequestWithContext(ctx, "GET", g.UserinfoURL, nil)
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("userinfo: HTTP %d", resp.StatusCode)
	}
	var c struct{ Sub, Email, Name string }
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return err
	}
	id.Subject, id.Email, id.Name = c.Sub, c.Email, c.Name
	return nil
}

// ---- sessions ----

type Session struct {
	State, Provider, Verifier, TokenID, ReauthAccountID string
}

type Sessions struct {
	DB  *db.DB
	Now func() time.Time // default time.Now
}

func (s Sessions) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// NewVerifier returns a PKCE code verifier.
func NewVerifier() string { return oauth2.GenerateVerifier() }

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Create stores a new session and returns its state. Expired sessions are swept.
func (s Sessions) Create(ctx context.Context, provider, verifier, tokenID, reauthAccountID string) (string, error) {
	state, err := randomState()
	if err != nil {
		return "", err
	}
	now := s.now()
	err = s.DB.Write(ctx, func(tx *db.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_sessions WHERE expires_at < ?`, now.Unix()); err != nil {
			return err
		}
		var re any
		if reauthAccountID != "" {
			re = reauthAccountID
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO oauth_sessions(state,provider,pkce_verifier,token_id,reauth_account_id,expires_at) VALUES (?,?,?,?,?,?)`,
			state, provider, verifier, tokenID, re, now.Add(SessionTTL).Unix())
		return err
	})
	return state, err
}

// Consume atomically removes the session (single use). Expired or unknown -> ErrInvalidState.
func (s Sessions) Consume(ctx context.Context, state string) (*Session, error) {
	var out *Session
	err := s.DB.Write(ctx, func(tx *db.Tx) error {
		var ss Session
		var re sql.NullString
		var exp int64
		err := tx.QueryRowContext(ctx, `DELETE FROM oauth_sessions WHERE state=? RETURNING state,provider,pkce_verifier,token_id,reauth_account_id,expires_at`, state).
			Scan(&ss.State, &ss.Provider, &ss.Verifier, &ss.TokenID, &re, &exp)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidState
		}
		if err != nil {
			return err
		}
		ss.ReauthAccountID = re.String
		if exp < s.now().Unix() {
			return nil // commit the delete, report invalid below
		}
		out = &ss
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, ErrInvalidState
	}
	return out, nil
}
