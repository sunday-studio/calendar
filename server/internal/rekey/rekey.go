// Package rekey re-seals every vault-sealed value under a new master key in one transaction.
package rekey

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"calendar/server/internal/db"
	"calendar/server/internal/vault"
)

// Result counts what was re-sealed.
type Result struct{ Accounts, Automations int }

// Run opens every sealed value with oldV and seals it again with newV. Nothing changes if any
// value cannot be opened with the old key (wrong key, or damaged data).
func Run(ctx context.Context, d *db.DB, oldV, newV *vault.Vault) (Result, error) {
	var res Result
	err := d.Write(ctx, func(tx *db.Tx) error {
		rs, err := tx.QueryContext(ctx, `SELECT id, secret FROM accounts`)
		if err != nil {
			return err
		}
		type acc struct {
			id     string
			sealed []byte
		}
		var accs []acc
		for rs.Next() {
			var a acc
			if err := rs.Scan(&a.id, &a.sealed); err != nil {
				rs.Close()
				return err
			}
			accs = append(accs, a)
		}
		rs.Close()
		for _, a := range accs {
			pt, err := oldV.Open(a.sealed, []byte(a.id))
			if err != nil {
				return fmt.Errorf("account %s: cannot open with the current key", a.id)
			}
			n, err := newV.Seal(pt, []byte(a.id))
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE accounts SET secret=? WHERE id=?`, n, a.id); err != nil {
				return err
			}
			res.Accounts++
		}
		ar, err := tx.QueryContext(ctx, `SELECT id, action FROM automations`)
		if err != nil {
			return err
		}
		type auto struct{ id, action string }
		var autos []auto
		for ar.Next() {
			var a auto
			if err := ar.Scan(&a.id, &a.action); err != nil {
				ar.Close()
				return err
			}
			autos = append(autos, a)
		}
		ar.Close()
		for _, a := range autos {
			var m map[string]any
			if json.Unmarshal([]byte(a.action), &m) != nil {
				continue
			}
			changed := false
			for _, f := range []string{"secretSealed", "tokenSealed"} {
				s, _ := m[f].(string)
				if s == "" {
					continue
				}
				raw, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return fmt.Errorf("automation %s: damaged %s", a.id, f)
				}
				pt, err := oldV.Open(raw, []byte(a.id))
				if err != nil {
					return fmt.Errorf("automation %s: cannot open %s with the current key", a.id, f)
				}
				n, err := newV.Seal(pt, []byte(a.id))
				if err != nil {
					return err
				}
				m[f] = base64.StdEncoding.EncodeToString(n)
				changed = true
			}
			if changed {
				b, _ := json.Marshal(m)
				if _, err := tx.ExecContext(ctx, `UPDATE automations SET action=? WHERE id=?`, string(b), a.id); err != nil {
					return err
				}
				res.Automations++
			}
		}
		return nil
	})
	return res, err
}
