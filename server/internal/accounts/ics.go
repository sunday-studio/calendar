package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"calendar/server/internal/db"
	"calendar/server/internal/domain"
	"calendar/server/internal/provider/ics"
)

// ICSInvalidError is a feed URL problem to show the user (HTTP 422, field "url"). The message
// never contains the URL.
type ICSInvalidError struct{ Msg string }

func (e *ICSInvalidError) Error() string { return e.Msg }

type icsSecret struct {
	URL string `json:"url"`
}

// ICSURL unseals an ICS account's feed URL (for the provider only; it must never be logged or returned).
func (s *Service) ICSURL(ctx context.Context, id string) (string, error) {
	var sealed []byte
	var prov string
	if err := s.DB.R.QueryRowContext(ctx, `SELECT provider, secret FROM accounts WHERE id=?`, id).Scan(&prov, &sealed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	pt, err := s.Vault.Open(sealed, []byte(id))
	if err != nil {
		return "", err
	}
	var p icsSecret
	if err := json.Unmarshal(pt, &p); err != nil {
		return "", err
	}
	return p.URL, nil
}

// checkICS normalises and validates a feed by fetching and parsing it.
func (s *Service) checkICS(ctx context.Context, raw string) (norm string, info ics.Info, err error) {
	if s.ICS == nil {
		return "", info, ErrUnsupported
	}
	norm, err = ics.Normalize(raw)
	if err == nil {
		info, err = s.ICS.Check(ctx, norm)
	}
	if err != nil {
		var ue *ics.UserError
		if errors.As(err, &ue) {
			return "", info, &ICSInvalidError{Msg: ue.Msg}
		}
		return "", info, err
	}
	return norm, info, nil
}

// CreateICS validates the feed URL (fetch + parse) and, only then, saves the account. Adding a
// feed that is already connected returns the existing account (created == false).
func (s *Service) CreateICS(ctx context.Context, rawURL, name, color string) (acc *domain.Account, created bool, err error) {
	if color != "" && !ValidColor(color) {
		return nil, false, ErrInvalidColor
	}
	norm, info, err := s.checkICS(ctx, rawURL)
	if err != nil {
		return nil, false, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = info.Name
	}
	host := ics.Host(norm)
	if name == "" {
		name = host
	}
	if utf8.RuneCountInString(name) > 80 {
		name = string([]rune(name)[:80])
	}
	ext := ics.ExternalID(norm)
	now := s.now().Unix()
	var id string
	err = s.DB.Write(ctx, func(tx *db.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE provider='ics' AND external_id=?`, ext).Scan(&id)
		if err == nil {
			created = false
			if color != "" {
				if _, err := tx.ExecContext(ctx, `UPDATE accounts SET color=? WHERE id=?`, color, id); err != nil {
					return err
				}
			}
			return tx.Change(ctx, "account", id, "upsert")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		created = true
		id = ulid.Make().String()
		if color == "" {
			if color, err = nextColor(ctx, tx); err != nil {
				return err
			}
		}
		pt, _ := json.Marshal(icsSecret{URL: norm})
		sealed, err := s.Vault.Seal(pt, []byte(id))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO accounts(id,provider,external_id,email,display_name,color,status,secret,next_sync_at,created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, id, "ics", ext, host, name, color, StatusConnecting, sealed, now, now); err != nil {
			return err
		}
		return tx.Change(ctx, "account", id, "upsert")
	})
	if err != nil {
		return nil, false, err
	}
	if created && s.OnStatus != nil {
		s.OnStatus(id, StatusConnecting)
	}
	acc, err = s.Get(ctx, id)
	return acc, created, err
}

// SetICSURL points an existing feed account at a new URL (the "reconnect" of ICS). The URL is
// validated first; on any failure nothing changes. Cached events stay until the next sync replaces them.
func (s *Service) SetICSURL(ctx context.Context, id, rawURL string) (*domain.Account, error) {
	a, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.Provider != "ics" {
		return nil, ErrUnsupported
	}
	norm, _, err := s.checkICS(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	ext := ics.ExternalID(norm)
	pt, _ := json.Marshal(icsSecret{URL: norm})
	sealed, err := s.Vault.Seal(pt, []byte(id))
	if err != nil {
		return nil, err
	}
	err = s.DB.Write(ctx, func(tx *db.Tx) error {
		var other string
		e := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE provider='ics' AND external_id=? AND id<>?`, ext, id).Scan(&other)
		if e == nil {
			return &ICSInvalidError{Msg: "That calendar is already connected."}
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET external_id=?, email=?, secret=?, status=?, status_detail=NULL, fail_count=0, next_sync_at=? WHERE id=?`,
			ext, ics.Host(norm), sealed, StatusConnecting, s.now().Unix(), id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE calendars SET sync_cursor=NULL WHERE account_id=?`, id); err != nil {
			return err
		}
		return tx.Change(ctx, "account", id, "upsert")
	})
	if err != nil {
		return nil, err
	}
	if s.OnStatus != nil {
		s.OnStatus(id, StatusConnecting)
	}
	return s.Get(ctx, id)
}
