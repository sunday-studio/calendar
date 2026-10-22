// Package authn issues, verifies and scopes bearer tokens. Only sha256(secret) is stored.
package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"errors"
	"strings"
	"time"

	"calendar/server/internal/db"
	"github.com/oklog/ulid/v2"
)

const Prefix = "cal_"

type Scope string

const (
	Read  Scope = "read"
	Write Scope = "write"
	Admin Scope = "admin"
)

func (s Scope) rank() int {
	switch s {
	case Read:
		return 1
	case Write:
		return 2
	case Admin:
		return 3
	}
	return 0
}

// Allows reports whether s satisfies the required scope (admin > write > read).
func (s Scope) Allows(required Scope) bool { return s.rank() > 0 && s.rank() >= required.rank() }

func ParseScope(s string) (Scope, error) {
	if sc := Scope(s); sc.rank() > 0 {
		return sc, nil
	}
	return "", errors.New("scope must be read, write or admin")
}

var ErrUnauthorized = errors.New("invalid or revoked token")

type Principal struct {
	TokenID string
	Name    string
	Kind    string // device | script
	Scope   Scope
}

type TokenInfo struct {
	ID, Name, Kind, Prefix string
	Scope                  Scope
	CreatedAt              time.Time
	LastUsedAt, RevokedAt  *time.Time
}

// Hash returns sha256 of the secret.
func Hash(secret string) []byte { h := sha256.Sum256([]byte(secret)); return h[:] }

type Store struct{ DB *db.DB }

// Create mints a token. The returned secret is shown once and never stored.
func (s Store) Create(ctx context.Context, name, kind string, scope Scope) (id, secret string, err error) {
	if kind != "device" && kind != "script" {
		return "", "", errors.New("kind must be device or script")
	}
	if scope.rank() == 0 {
		return "", "", errors.New("invalid scope")
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return
	}
	secret = Prefix + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	id = ulid.Make().String()
	err = s.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO tokens(id,name,kind,scope,hash,prefix,created_at) VALUES (?,?,?,?,?,?,?)`,
			id, name, kind, string(scope), Hash(secret), secret[:len(Prefix)+4], time.Now().Unix())
		return err
	})
	return id, secret, err
}

// Verify resolves a secret to a Principal and records last_used_at (at most once a minute).
func (s Store) Verify(ctx context.Context, secret string) (*Principal, error) {
	if !strings.HasPrefix(secret, Prefix) {
		return nil, ErrUnauthorized
	}
	var p Principal
	var sc string
	var last sql.NullInt64
	err := s.DB.R.QueryRowContext(ctx, `SELECT id,name,kind,scope,last_used_at FROM tokens WHERE hash=? AND revoked_at IS NULL`, Hash(secret)).Scan(&p.TokenID, &p.Name, &p.Kind, &sc, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	p.Scope = Scope(sc)
	now := time.Now().Unix()
	if !last.Valid || now-last.Int64 >= 60 {
		_ = s.DB.Write(ctx, func(tx *db.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE tokens SET last_used_at=? WHERE id=?`, now, p.TokenID)
			return err
		})
	}
	return &p, nil
}

func (s Store) List(ctx context.Context) ([]TokenInfo, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id,name,kind,scope,prefix,created_at,last_used_at,revoked_at FROM tokens ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TokenInfo
	for rows.Next() {
		var t TokenInfo
		var sc string
		var created int64
		var lu, rv sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Name, &t.Kind, &sc, &t.Prefix, &created, &lu, &rv); err != nil {
			return nil, err
		}
		t.Scope, t.CreatedAt = Scope(sc), time.Unix(created, 0)
		if lu.Valid {
			x := time.Unix(lu.Int64, 0)
			t.LastUsedAt = &x
		}
		if rv.Valid {
			x := time.Unix(rv.Int64, 0)
			t.RevokedAt = &x
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

var ErrNotFound = errors.New("token not found or already revoked")

func (s Store) Revoke(ctx context.Context, id string) error {
	return s.DB.Write(ctx, func(tx *db.Tx) error {
		r, err := tx.ExecContext(ctx, `UPDATE tokens SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, time.Now().Unix(), id)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		// a revoked device must stop receiving pushes
		_, err = tx.ExecContext(ctx, `DELETE FROM push_endpoints WHERE token_id=?`, id)
		return err
	})
}

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// BearerToken extracts the secret from an Authorization header value.
func BearerToken(h string) string {
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
