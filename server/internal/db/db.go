// Package db wraps SQLite: one writer connection, a reader pool, embedded migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type DB struct {
	W *sql.DB // single writer
	R *sql.DB // reader pool

	path string

	hookMu sync.RWMutex
	hooks  []func(rev int64)
}

// OnCommit registers fn to run after every committed transaction that called Change.
// fn receives the new rev; it must not block (it runs on the writer's goroutine).
func (d *DB) OnCommit(fn func(rev int64)) {
	d.hookMu.Lock()
	d.hooks = append(d.hooks, fn)
	d.hookMu.Unlock()
}

// SizeBytes returns the database size in bytes (page_count * page_size).
func (d *DB) SizeBytes(ctx context.Context) (int64, error) {
	var n, sz int64
	if err := d.R.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&n); err != nil {
		return 0, err
	}
	if err := d.R.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&sz); err != nil {
		return 0, err
	}
	return n * sz, nil
}

func dsn(path string, extra ...string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	for _, e := range extra {
		k, v, _ := strings.Cut(e, "=")
		q.Add(k, v)
	}
	return "file:" + path + "?" + q.Encode()
}

// MigrationNames lists the migrations this binary knows, in order.
func MigrationNames() []string {
	ents, _ := migrationFS.ReadDir("migrations")
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "." && !strings.HasPrefix(path, ":") {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	w, err := sql.Open("sqlite", dsn(path, "_txlock=immediate"))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open("sqlite", dsn(path, "_pragma=query_only(1)"))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	d := &DB{W: w, R: r, path: path}
	if err := d.Migrate(ctx); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error {
	e1 := d.R.Close()
	e2 := d.W.Close()
	if e1 != nil {
		return e1
	}
	return e2
}

// Migrate applies embedded migrations in order; already-applied ones are skipped.
func (d *DB) Migrate(ctx context.Context) error {
	if _, err := d.W.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	ents, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if err := d.backupBeforeUpgrade(ctx, names); err != nil {
		return err
	}
	for _, n := range names {
		var done int
		if err := d.W.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name=?`, n).Scan(&done); err != nil {
			return err
		}
		if done > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + n)
		if err != nil {
			return err
		}
		err = d.Write(ctx, func(tx *Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(name, applied_at) VALUES (?,?)`, n, time.Now().Unix())
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", n, err)
		}
	}
	return nil
}

// Tx is a write transaction. Call Change to record a user-visible mutation.
type Tx struct {
	*sql.Tx
	changed bool
	rev     int64
	// Quiet marks changes made on behalf of a client's own request: other devices still
	// refresh, but nobody is alerted about what they just did themselves.
	Quiet bool
}

// Rev returns the revision assigned to this transaction (valid after the first Change).
func (t *Tx) Rev() int64 { return t.rev }

// Change bumps meta.rev (once per transaction) and appends to the changes log.
func (t *Tx) Change(ctx context.Context, entity, id, op string) error {
	if !t.changed {
		if _, err := t.ExecContext(ctx, `UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key='rev'`); err != nil {
			return err
		}
		if err := t.QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM meta WHERE key='rev'`).Scan(&t.rev); err != nil {
			return err
		}
		t.changed = true
	}
	src := ""
	if t.Quiet {
		src = "user"
	}
	_, err := t.ExecContext(ctx, `INSERT INTO changes(entity, entity_id, op, ts, src) VALUES (?,?,?,?,?)`, entity, id, op, time.Now().Unix(), src)
	return err
}

// Write runs fn in the single-writer transaction; commits on nil, rolls back on error.
func (d *DB) Write(ctx context.Context, fn func(tx *Tx) error) error {
	sqlTx, err := d.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	tx := &Tx{Tx: sqlTx}
	if err := fn(tx); err != nil {
		sqlTx.Rollback()
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return err
	}
	if tx.changed {
		d.hookMu.RLock()
		hs := d.hooks
		d.hookMu.RUnlock()
		for _, h := range hs {
			h(tx.rev)
		}
	}
	return nil
}

// Rev returns the current global revision.
func (d *DB) Rev(ctx context.Context) (int64, error) {
	var r int64
	err := d.R.QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM meta WHERE key='rev'`).Scan(&r)
	return r, err
}

// backupBeforeUpgrade snapshots an existing database before pending migrations are applied
// (ENGINEERING 10: "the DB is backed up first when the schema version changes"). Fresh databases
// are skipped; the three newest snapshots are kept next to the database file.
func (d *DB) backupBeforeUpgrade(ctx context.Context, all []string) error {
	if d.path == "" || strings.HasPrefix(d.path, ":") {
		return nil
	}
	rows, err := d.W.QueryContext(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return err
	}
	applied := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		applied[n] = true
	}
	rows.Close()
	pending := 0
	for _, n := range all {
		if !applied[n] {
			pending++
		}
	}
	if pending == 0 || len(applied) == 0 {
		return nil
	}
	dst := d.path + ".pre-migrate-" + time.Now().UTC().Format("20060102-150405")
	if _, err := d.W.ExecContext(ctx, `VACUUM INTO '`+strings.ReplaceAll(dst, `'`, `''`)+`'`); err != nil {
		return fmt.Errorf("backup before upgrade: %w", err)
	}
	old, _ := filepath.Glob(d.path + ".pre-migrate-*")
	sort.Strings(old)
	for len(old) > 3 {
		os.Remove(old[0])
		old = old[1:]
	}
	return nil
}
