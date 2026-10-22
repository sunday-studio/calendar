package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"calendar/server/internal/db"
)

var key = bytes.Repeat([]byte{7}, 32)

func TestEncryptRoundTripAndTamper(t *testing.T) {
	for _, n := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3*chunkSize + 17} {
		data := make([]byte, n)
		rand.Read(data)
		var enc, dec bytes.Buffer
		if err := Encrypt(&enc, bytes.NewReader(data), key); err != nil {
			t.Fatal(err)
		}
		if err := Decrypt(&dec, bytes.NewReader(enc.Bytes()), key); err != nil || !bytes.Equal(dec.Bytes(), data) {
			t.Fatalf("size %d: %v", n, err)
		}
		if n >= 16 && bytes.Contains(enc.Bytes(), data[:min(n, 32)]) {
			t.Fatalf("size %d: plaintext visible", n)
		}
	}
	data := bytes.Repeat([]byte("calendar"), 30000) // several chunks
	var enc bytes.Buffer
	Encrypt(&enc, bytes.NewReader(data), key)
	b := enc.Bytes()
	if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(b), bytes.Repeat([]byte{8}, 32)); !errors.Is(err, ErrWrongKey) {
		t.Fatal("wrong key:", err)
	}
	if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(b[:len(b)-100]), key); err == nil {
		t.Fatal("truncated file accepted")
	}
	cut := append([]byte(nil), b[:len(magic)+16+4+chunkSize+16]...) // exactly the first chunk, no final one
	if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(cut), key); err == nil {
		t.Fatal("file cut at a chunk boundary accepted")
	}
	flipped := append([]byte(nil), b...)
	flipped[len(flipped)/2] ^= 1
	if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(flipped), key); err == nil {
		t.Fatal("bit flip accepted")
	}
	if err := Decrypt(&bytes.Buffer{}, bytes.NewReader([]byte("not a backup at all")), key); !errors.Is(err, ErrNotBackup) {
		t.Fatal(err)
	}
}

func openDB(t *testing.T) (*db.DB, string) {
	p := filepath.Join(t.TempDir(), "hub", "calendar.db")
	os.MkdirAll(filepath.Dir(p), 0o700)
	d, err := db.Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, p
}

func seed(t *testing.T, d *db.DB) {
	t.Helper()
	if _, err := d.W.Exec(`INSERT INTO settings(key,value) VALUES ('marker','"hello"')`); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(context.Background(), func(tx *db.Tx) error { return tx.Change(context.Background(), "event", "x", "upsert") }); err != nil {
		t.Fatal(err)
	}
}

func TestCreateVerifyRetentionAndRestore(t *testing.T) {
	d, dbPath := openDB(t)
	seed(t, d)
	dir := filepath.Join(t.TempDir(), "backups")
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	var last string
	for i := 0; i < 5; i++ {
		p, size, err := Create(context.Background(), Options{DB: d, Key: key, Dir: dir, Keep: 3, Now: func() time.Time { return now.Add(time.Duration(i) * time.Hour) }})
		if err != nil || size == 0 {
			t.Fatal(err)
		}
		last = p
	}
	names, _ := List(dir)
	if len(names) != 3 || names[0] != "calendar-20261015-070000.db.enc" {
		t.Fatalf("retention: %v", names)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
	if st, _ := os.Stat(last); st.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %v", st.Mode())
	}
	if !IsEncrypted(last) {
		t.Fatal("not encrypted")
	}
	var at string
	if err := d.R.QueryRow(`SELECT value FROM meta WHERE key='last_backup_at'`).Scan(&at); err != nil || at == "" {
		t.Fatal("last_backup_at not recorded")
	}

	// restore onto a new path
	target := filepath.Join(t.TempDir(), "restored.db")
	if err := Restore(context.Background(), last, target, nil, false, now); err == nil {
		t.Fatal("encrypted restore without key must fail")
	}
	if err := Restore(context.Background(), last, target, bytes.Repeat([]byte{9}, 32), false, now); !errors.Is(err, ErrWrongKey) {
		t.Fatal(err)
	}
	if err := Restore(context.Background(), last, target, key, false, now); err != nil {
		t.Fatal(err)
	}
	r, err := db.Open(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	var v string
	r.R.QueryRow(`SELECT value FROM settings WHERE key='marker'`).Scan(&v)
	rev, _ := r.Rev(context.Background())
	origRev, _ := d.Rev(context.Background())
	r.Close()
	if v != `"hello"` || rev != origRev+RevBump {
		t.Fatalf("marker=%q rev=%d want %d", v, rev, origRev+RevBump)
	}

	// replacing an existing database needs --force and keeps the old one
	if err := Restore(context.Background(), last, target, key, false, now); err == nil {
		t.Fatal("overwrite without force")
	}
	if err := Restore(context.Background(), last, target, key, true, now); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(target + ".pre-restore-*"); len(m) != 1 {
		t.Fatalf("old database not kept: %v", m)
	}
	// a hub that is running holds the database: refuse
	live, err := db.Open(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	tx, _ := live.W.Begin() // single writer connection held with a write lock
	tx.Exec(`UPDATE meta SET value=value WHERE key='rev'`)
	if err := Restore(context.Background(), last, target, key, true, now); err == nil {
		t.Fatal("restored over a database in use")
	}
	tx.Rollback()
	_ = dbPath
}

func TestPlainBackupAndBadFiles(t *testing.T) {
	d, _ := openDB(t)
	seed(t, d)
	dir := t.TempDir()
	p, _, err := Create(context.Background(), Options{DB: d, Dir: dir})
	if err != nil || IsEncrypted(p) || filepath.Ext(p) != ".db" {
		t.Fatal(err, p)
	}
	if _, err := Verify(p); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "r.db")
	if err := Restore(context.Background(), p, target, nil, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	// garbage is refused and nothing is installed
	bad := filepath.Join(t.TempDir(), "bad.db")
	os.WriteFile(bad, []byte("this is not sqlite"), 0o600)
	t2 := filepath.Join(t.TempDir(), "r2.db")
	if err := Restore(context.Background(), bad, t2, nil, false, time.Now()); err == nil {
		t.Fatal("garbage restored")
	}
	if _, err := os.Stat(t2); err == nil {
		t.Fatal("garbage installed")
	}
	// a database from a newer calendard is refused
	nd, _ := openDB(t)
	nd.W.Exec(`INSERT INTO schema_migrations(name, applied_at) VALUES ('9999_future.sql', 1)`)
	np, _, _ := Create(context.Background(), Options{DB: nd, Dir: t.TempDir()})
	if err := Restore(context.Background(), np, filepath.Join(t.TempDir(), "r3.db"), nil, false, time.Now()); !errors.Is(err, ErrNewerSchema) {
		t.Fatal(err)
	}
}

func TestBackupWhileWriting(t *testing.T) {
	d, _ := openDB(t)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				d.Write(context.Background(), func(tx *db.Tx) error { return tx.Change(context.Background(), "event", "x", "upsert") })
			}
		}
	}()
	for i := 0; i < 5; i++ {
		p, _, err := Create(context.Background(), Options{DB: d, Key: key, Dir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		tmp := filepath.Join(t.TempDir(), "x.db")
		if err := Restore(context.Background(), p, tmp, key, false, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	<-done
}
