// Package backup makes consistent, verified, optionally encrypted copies of the SQLite database
// and restores them. The distroless image has no cron or shell, so the nightly job runs in-process.
package backup

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"

	"calendar/server/internal/db"
)

const (
	magic     = "CALBAK01"
	chunkSize = 64 * 1024
	// RevBump is added to the hub revision after a restore so that clients that saw a newer
	// revision before the restore still notice that something changed and refetch.
	RevBump = 100000
)

var (
	ErrWrongKey    = errors.New("backup: cannot decrypt (wrong master key, or the file is damaged)")
	ErrNotBackup   = errors.New("backup: not a calendard backup")
	ErrNewerSchema = errors.New("backup: made by a newer calendard (unknown migrations); upgrade the binary first")
)

func deriveKey(master []byte) ([]byte, error) {
	r := hkdf.New(sha256.New, master, nil, []byte("calendard backup v1"))
	k := make([]byte, chacha20poly1305.KeySize)
	_, err := io.ReadFull(r, k)
	return k, err
}

func nonceFor(prefix []byte, n uint64) []byte {
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	copy(nonce, prefix)
	binary.BigEndian.PutUint64(nonce[16:], n)
	return nonce
}

// Encrypt streams src to dst as length-prefixed XChaCha20-Poly1305 chunks. The last chunk is
// marked final in its AAD so a truncated file does not decrypt.
func Encrypt(dst io.Writer, src io.Reader, master []byte) error {
	k, err := deriveKey(master)
	if err != nil {
		return err
	}
	aead, err := chacha20poly1305.NewX(k)
	if err != nil {
		return err
	}
	prefix := make([]byte, 16)
	if _, err := rand.Read(prefix); err != nil {
		return err
	}
	if _, err := dst.Write(append([]byte(magic), prefix...)); err != nil {
		return err
	}
	br := bufio.NewReaderSize(src, chunkSize+1)
	buf := make([]byte, chunkSize)
	var n uint64
	for {
		m, rerr := io.ReadFull(br, buf)
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return rerr
		}
		final := byte(0)
		if _, perr := br.Peek(1); perr == io.EOF {
			final = 1
		}
		ct := aead.Seal(nil, nonceFor(prefix, n), buf[:m], []byte{final})
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(ct)))
		if _, err := dst.Write(append(l[:], ct...)); err != nil {
			return err
		}
		n++
		if final == 1 {
			return nil
		}
	}
}

// Decrypt reverses Encrypt.
func Decrypt(dst io.Writer, src io.Reader, master []byte) error {
	br := bufio.NewReader(src)
	head := make([]byte, len(magic)+16)
	if _, err := io.ReadFull(br, head); err != nil || string(head[:len(magic)]) != magic {
		return ErrNotBackup
	}
	prefix := head[len(magic):]
	k, err := deriveKey(master)
	if err != nil {
		return err
	}
	aead, err := chacha20poly1305.NewX(k)
	if err != nil {
		return err
	}
	var n uint64
	for {
		var l [4]byte
		if _, err := io.ReadFull(br, l[:]); err != nil {
			return ErrWrongKey // ended without a final chunk: truncated
		}
		sz := binary.BigEndian.Uint32(l[:])
		if sz > chunkSize+chacha20poly1305.Overhead {
			return ErrNotBackup
		}
		ct := make([]byte, sz)
		if _, err := io.ReadFull(br, ct); err != nil {
			return ErrWrongKey
		}
		final := byte(0)
		if _, perr := br.Peek(1); perr == io.EOF {
			final = 1
		}
		pt, err := aead.Open(nil, nonceFor(prefix, n), ct, []byte{final})
		if err != nil {
			return ErrWrongKey
		}
		if _, err := dst.Write(pt); err != nil {
			return err
		}
		n++
		if final == 1 {
			return nil
		}
	}
}

// IsEncrypted reports whether the file starts with the backup magic.
func IsEncrypted(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, len(magic))
	_, err = io.ReadFull(f, b)
	return err == nil && string(b) == magic
}

// Verify opens a SQLite file read-only and runs integrity_check; it also returns the names of its migrations.
func Verify(path string) ([]string, error) {
	h, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(3000)")
	if err != nil {
		return nil, err
	}
	defer h.Close()
	var res string
	if err := h.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	if res != "ok" {
		return nil, fmt.Errorf("backup: integrity check failed: %s", res)
	}
	rs, err := h.Query(`SELECT name FROM schema_migrations ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("backup: not a calendard database: %w", err)
	}
	defer rs.Close()
	var names []string
	for rs.Next() {
		var n string
		if err := rs.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rs.Err()
}

// Options for Create.
type Options struct {
	DB   *db.DB
	Key  []byte // master key; nil = write an unencrypted copy
	Dir  string
	Keep int // newest N backups are kept (0 = default 14)
	Now  func() time.Time
	Log  *slog.Logger
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func isBackupName(n string) bool {
	return strings.HasPrefix(n, "calendar-") && (strings.HasSuffix(n, ".db.enc") || strings.HasSuffix(n, ".db"))
}

// List returns backups in dir, newest first.
func List(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && isBackupName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// Create writes calendar-YYYYMMDD-HHMMSS.db[.enc] into Dir. The snapshot is taken with VACUUM INTO
// (consistent while the hub runs), integrity-checked, then encrypted.
func Create(ctx context.Context, o Options) (path string, size int64, err error) {
	if o.Dir == "" {
		return "", 0, errors.New("backup: no directory")
	}
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(o.Dir, ".snapshot-*.db")
	if err != nil {
		return "", 0, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	os.Remove(tmpPath) // VACUUM INTO needs a path that does not exist
	defer os.Remove(tmpPath)
	if _, err := o.DB.W.ExecContext(ctx, `VACUUM INTO '`+strings.ReplaceAll(tmpPath, `'`, `''`)+`'`); err != nil {
		return "", 0, fmt.Errorf("backup: snapshot: %w", err)
	}
	if _, err := Verify(tmpPath); err != nil {
		return "", 0, err
	}
	name := "calendar-" + o.now().UTC().Format("20060102-150405") + ".db"
	if o.Key != nil {
		name += ".enc"
	}
	final := filepath.Join(o.Dir, name)
	part := final + ".partial"
	in, err := os.Open(tmpPath)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", 0, err
	}
	if o.Key != nil {
		err = Encrypt(out, in, o.Key)
	} else {
		_, err = io.Copy(out, in)
	}
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(part)
		return "", 0, err
	}
	if err := os.Rename(part, final); err != nil {
		return "", 0, err
	}
	st, _ := os.Stat(final)
	if st != nil {
		size = st.Size()
	}
	keep := o.Keep
	if keep <= 0 {
		keep = 14
	}
	if names, err := List(o.Dir); err == nil && len(names) > keep {
		for _, n := range names[keep:] {
			os.Remove(filepath.Join(o.Dir, n))
		}
	}
	_, _ = o.DB.W.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES ('last_backup_at',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprint(o.now().Unix()))
	return final, size, nil
}

// Restore decrypts (if needed), verifies and installs a backup as the database at dbPath.
// An existing database is only replaced with force, and then kept next to it as
// <db>.pre-restore-<time>. The hub must be stopped (an in-use database is refused).
func Restore(ctx context.Context, from, dbPath string, key []byte, force bool, now time.Time) error {
	if _, err := os.Stat(dbPath); err == nil && !force {
		return fmt.Errorf("backup: %s exists; pass --force to replace it (it is kept as a .pre-restore copy)", dbPath)
	}
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".restore-*.db")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	src, err := os.Open(from)
	if err != nil {
		tmp.Close()
		return err
	}
	defer src.Close()
	if IsEncrypted(from) {
		if key == nil {
			tmp.Close()
			return errors.New("backup: this backup is encrypted; the master key is required")
		}
		err = Decrypt(tmp, src, key)
	} else {
		_, err = io.Copy(tmp, src)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	names, err := Verify(tmpPath)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, n := range db.MigrationNames() {
		known[n] = true
	}
	for _, n := range names {
		if !known[n] {
			return ErrNewerSchema
		}
	}
	if _, err := os.Stat(dbPath); err == nil {
		if err := ensureUnused(dbPath); err != nil {
			return err
		}
		stamp := now.UTC().Format("20060102-150405")
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if _, err := os.Stat(dbPath + suffix); err == nil {
				if err := os.Rename(dbPath+suffix, dbPath+".pre-restore-"+stamp+suffix); err != nil {
					return err
				}
			}
		}
	}
	if err := os.Rename(tmpPath, dbPath); err != nil {
		return err
	}
	// make clients that saw a newer revision notice the change
	h, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(3000)")
	if err != nil {
		return err
	}
	defer h.Close()
	_, err = h.ExecContext(ctx, `UPDATE meta SET value = CAST(value AS INTEGER) + ? WHERE key='rev'`, RevBump)
	return err
}

// ensureUnused refuses when another process holds the database (the hub is running).
func ensureUnused(path string) error {
	h, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(500)&_txlock=exclusive")
	if err != nil {
		return err
	}
	defer h.Close()
	tx, err := h.Begin()
	if err != nil {
		return fmt.Errorf("backup: the database is in use; stop calendard first (%v)", err)
	}
	return tx.Rollback()
}

// Schedule runs Create every day at hour:00 local time until ctx ends.
func Schedule(ctx context.Context, o Options, hour int, loc *time.Location) {
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	for {
		now := o.now().In(loc)
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, loc)
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(next.Sub(now)):
		}
		path, size, err := Create(ctx, o)
		if err != nil {
			log.Error("nightly backup failed", "err", err)
			continue
		}
		log.Info("nightly backup", "file", filepath.Base(path), "bytes", size)
	}
}
