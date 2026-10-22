package authn

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"calendar/server/internal/db"
)

func store(t *testing.T) Store {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return Store{DB: d}
}

func TestCreateVerifyHashed(t *testing.T) {
	ctx := context.Background()
	s := store(t)
	id, secret, err := s.Create(ctx, "iphone", "device", Write)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "cal_") {
		t.Fatal(secret)
	}
	var h []byte
	s.DB.R.QueryRow(`SELECT hash FROM tokens WHERE id=?`, id).Scan(&h)
	if !bytes.Equal(h, Hash(secret)) || bytes.Contains(h, []byte(secret)) {
		t.Fatal("hash not stored correctly")
	}
	p, err := s.Verify(ctx, secret)
	if err != nil || p.TokenID != id || p.Scope != Write {
		t.Fatalf("%+v %v", p, err)
	}
	var lu *int64
	s.DB.R.QueryRow(`SELECT last_used_at FROM tokens WHERE id=?`, id).Scan(&lu)
	if lu == nil {
		t.Fatal("last_used_at not set")
	}
	if _, err := s.Verify(ctx, "cal_nope"); err != ErrUnauthorized {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, "garbage"); err != ErrUnauthorized {
		t.Fatal(err)
	}
}

func TestRevoke(t *testing.T) {
	ctx := context.Background()
	s := store(t)
	id, secret, _ := s.Create(ctx, "x", "script", Read)
	if err := s.Revoke(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, secret); err != ErrUnauthorized {
		t.Fatal("revoked token verified")
	}
	if err := s.Revoke(ctx, id); err != ErrNotFound {
		t.Fatal(err)
	}
	l, _ := s.List(ctx)
	if len(l) != 1 || l[0].RevokedAt == nil {
		t.Fatalf("%+v", l)
	}
}

func TestScopes(t *testing.T) {
	cases := []struct {
		have, need Scope
		ok         bool
	}{
		{Read, Read, true}, {Read, Write, false}, {Write, Read, true},
		{Write, Admin, false}, {Admin, Write, true}, {"", Read, false},
	}
	for _, c := range cases {
		if c.have.Allows(c.need) != c.ok {
			t.Errorf("%q allows %q = %v", c.have, c.need, !c.ok)
		}
	}
	if _, _, err := (store(t)).Create(context.Background(), "x", "bogus", Read); err == nil {
		t.Fatal("bad kind accepted")
	}
}
