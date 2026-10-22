package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func open(t *testing.T) (*DB, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.db")
	d, err := Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, p
}

func TestMigrateIdempotentAndSchema(t *testing.T) {
	ctx := context.Background()
	d, p := open(t)
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	d.Close()
	d2, err := Open(ctx, p) // reopen applies again
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	var n int
	d2.R.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n)
	ents, _ := migrationFS.ReadDir("migrations")
	if n != len(ents) {
		t.Fatalf("migrations applied %d times, want once each (%d)", n, len(ents))
	}
	for _, tbl := range []string{"tokens", "accounts", "calendars", "events", "occurrences", "changes", "events_fts"} {
		if err := d2.R.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
			t.Fatalf("%s: %v", tbl, err)
		}
	}
	var mode string
	d2.R.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	if mode != "wal" {
		t.Fatalf("journal_mode=%s", mode)
	}
}

func TestWriteRevAndRollback(t *testing.T) {
	ctx := context.Background()
	d, _ := open(t)
	err := d.Write(ctx, func(tx *Tx) error {
		if err := tx.Change(ctx, "event", "a", "upsert"); err != nil {
			return err
		}
		return tx.Change(ctx, "event", "b", "upsert")
	})
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := d.Rev(ctx); r != 1 {
		t.Fatalf("rev=%d want 1 (one bump per tx)", r)
	}
	boom := errors.New("boom")
	if err := d.Write(ctx, func(tx *Tx) error { tx.Change(ctx, "event", "c", "upsert"); return boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if r, _ := d.Rev(ctx); r != 1 {
		t.Fatalf("rev=%d after rollback", r)
	}
	var n int
	d.R.QueryRow(`SELECT COUNT(*) FROM changes`).Scan(&n)
	if n != 2 {
		t.Fatalf("changes=%d", n)
	}
	// a write without Change leaves rev alone
	d.Write(ctx, func(tx *Tx) error { return nil })
	if r, _ := d.Rev(ctx); r != 1 {
		t.Fatalf("rev=%d", r)
	}
}

func TestUpgradeMakesABackupFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	// an existing database one migration behind: apply all but the newest by hand
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	names := MigrationNames()
	for _, n := range names[:len(names)-1] {
		body, _ := migrationFS.ReadFile("migrations/" + n)
		if _, err := raw.Exec(string(body)); err != nil {
			t.Fatal(n, err)
		}
		raw.Exec(`INSERT INTO schema_migrations(name, applied_at) VALUES (?,1)`, n)
	}
	raw.Close()
	d, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m, _ := filepath.Glob(path + ".pre-migrate-*")
	if len(m) != 1 {
		t.Fatalf("expected one pre-migrate snapshot, got %v", m)
	}
	// a fresh database does not make one
	p2 := filepath.Join(t.TempDir(), "fresh.db")
	d2, _ := Open(context.Background(), p2)
	d2.Close()
	if m, _ := filepath.Glob(p2 + ".pre-migrate-*"); len(m) != 0 {
		t.Fatal("fresh database snapshotted")
	}
}
