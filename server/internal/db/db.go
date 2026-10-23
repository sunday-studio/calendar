// Package db wraps SQLite: one writer connection, a reader pool, embedded migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"modernc.org/sqlite"

	"calendar/server/internal/telemetry"
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
	ctx, span := telemetry.StartSpan(ctx, "db.migrate", attribute.Int("calendar.migrations.known", len(names)))
	defer span.End()
	applied := 0
	defer func() { span.SetAttributes(attribute.Int("calendar.migrations.applied", applied)) }()
	if err := d.backupBeforeUpgrade(ctx, names); err != nil {
		telemetry.RecordError(ctx, "db", err, telemetry.Operation("migrate_backup"))
		return err
	}
	for _, n := range names {
		var done int
		if err := d.W.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name=?`, n).Scan(&done); err != nil {
			telemetry.RecordError(ctx, "db", err, telemetry.Operation("migrate"))
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
			err = fmt.Errorf("migration %s: %w", n, err)
			telemetry.RecordError(ctx, "db", err, telemetry.Operation("migrate"))
			return err
		}
		applied++
		telemetry.Inc(ctx, "db.migrations.applied")
		span.AddEvent("migration applied")
		telemetry.Log().InfoContext(ctx, "migration applied", "name", n)
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

// slowTx is the write-transaction duration beyond which a span event and calendar.db.slow_queries fire.
const slowTx = 50 * time.Millisecond

func isBusy(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		c := se.Code() & 0xff
		return c == 5 || c == 6 // SQLITE_BUSY, SQLITE_LOCKED
	}
	return false
}

// Write runs fn in the single-writer transaction; commits on nil, rolls back on error. Each call is a
// "db.write" span (child of the caller's span) with calendar.db.write.duration, lock-wait and slow-
// transaction accounting. An error returned by fn is the caller's to classify (it may be an expected
// condition such as a conflict); a failure of the database itself (begin, commit, busy) is recorded here.
func (d *DB) Write(ctx context.Context, fn func(tx *Tx) error) (err error) {
	ctx, span := telemetry.StartSpan(ctx, "db.write")
	start := time.Now()
	outcome := "committed"
	defer func() {
		dur := time.Since(start)
		telemetry.Record(ctx, "db.write.duration", dur.Seconds())
		telemetry.Inc(ctx, "db.write.transactions", telemetry.Outcome(outcome))
		if dur > slowTx {
			telemetry.Inc(ctx, "db.slow_queries")
			span.AddEvent("slow write transaction", trace.WithAttributes(attribute.Int64("calendar.duration_ms", dur.Milliseconds())))
		}
		span.SetAttributes(attribute.String("calendar.db.outcome", outcome))
		span.End()
	}()
	sqlTx, err := d.W.BeginTx(ctx, nil)
	if wait := time.Since(start); wait > slowTx || err != nil {
		if isBusy(err) || wait > slowTx {
			telemetry.Inc(ctx, "db.busy", telemetry.Attr("reason", map[bool]string{true: "timeout", false: "wait"}[err != nil]))
			span.AddEvent("waited for the writer lock", trace.WithAttributes(attribute.Int64("calendar.wait_ms", wait.Milliseconds())))
		}
	}
	if err != nil {
		outcome = "failed"
		if ctx.Err() == nil {
			telemetry.RecordError(ctx, "db", err, telemetry.Operation("begin"))
		}
		return err
	}
	tx := &Tx{Tx: sqlTx}
	if err := fn(tx); err != nil {
		sqlTx.Rollback()
		outcome = "rolled_back"
		if isBusy(err) {
			telemetry.Inc(ctx, "db.busy", telemetry.Attr("reason", "timeout"))
		}
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		outcome = "failed"
		telemetry.RecordError(ctx, "db", err, telemetry.Operation("commit"))
		return err
	}
	if tx.changed {
		span.SetAttributes(attribute.Int64("calendar.db.rev", tx.rev))
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

// RegisterGauges registers the observable gauges calendar.db.rev, db.size_bytes, db.wal_bytes,
// calendar.events and calendar.occurrences. Counts are cached for 30 s so a scrape never scans tables
// repeatedly. Call once at start-up after telemetry.Setup. The returned function unregisters them.
func (d *DB) RegisterGauges() func() {
	var mu sync.Mutex
	var at time.Time
	var ev, oc int64
	counts := func(ctx context.Context) (int64, int64) {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) > 30*time.Second {
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_ = d.R.QueryRowContext(cctx, `SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`).Scan(&ev)
			_ = d.R.QueryRowContext(cctx, `SELECT COUNT(*) FROM occurrences`).Scan(&oc)
			at = time.Now()
		}
		return ev, oc
	}
	var offs []func()
	one := func(name, desc, unit string, f func(ctx context.Context) (float64, bool)) {
		offs = append(offs, telemetry.GaugeSet(name, desc, unit, func(ctx context.Context) []telemetry.Observation {
			if v, ok := f(ctx); ok {
				return []telemetry.Observation{{Value: v}}
			}
			return nil
		}))
	}
	one("db.rev", "Current database revision (meta.rev); it moves with every user-visible change", "1", func(ctx context.Context) (float64, bool) {
		r, err := d.Rev(ctx)
		return float64(r), err == nil
	})
	one("db.size_bytes", "SQLite database size (page_count * page_size)", "By", func(ctx context.Context) (float64, bool) {
		n, err := d.SizeBytes(ctx)
		return float64(n), err == nil
	})
	one("db.wal_bytes", "Size of the SQLite write-ahead log file", "By", func(context.Context) (float64, bool) {
		if d.path == "" || strings.HasPrefix(d.path, ":") {
			return 0, false
		}
		fi, err := os.Stat(d.path + "-wal")
		if err != nil {
			return 0, err != nil && errors.Is(err, os.ErrNotExist)
		}
		return float64(fi.Size()), true
	})
	one("events", "Live events in the mirror", "{event}", func(ctx context.Context) (float64, bool) { e, _ := counts(ctx); return float64(e), true })
	one("occurrences", "Rows in the occurrence read model", "{occurrence}", func(ctx context.Context) (float64, bool) { _, o := counts(ctx); return float64(o), true })
	return func() {
		for _, f := range offs {
			f()
		}
	}
}
