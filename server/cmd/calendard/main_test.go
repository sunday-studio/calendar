package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeRefusesWithoutMasterKey(t *testing.T) {
	t.Setenv("CALENDAR_DB", filepath.Join(t.TempDir(), "c.db"))
	t.Setenv("CALENDAR_MASTER_KEY", "")
	t.Setenv("CALENDAR_MASTER_KEY_FILE", "")
	err := run([]string{"serve"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "master key") {
		t.Fatalf("got %v", err)
	}
	t.Setenv("CALENDAR_MASTER_KEY", "tooshort")
	if err := run([]string{"serve"}, &bytes.Buffer{}); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestTokenCLI(t *testing.T) {
	t.Setenv("CALENDAR_DB", filepath.Join(t.TempDir(), "c.db"))
	var out bytes.Buffer
	if err := run([]string{"token", "create", "--name", "me", "--kind", "device", "--scope", "write"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cal_") {
		t.Fatal(out.String())
	}
	out.Reset()
	run([]string{"token", "list"}, &out)
	if !strings.Contains(out.String(), "active") {
		t.Fatal(out.String())
	}
}

const testKeyB64 = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // 0123456789abcdef0123456789abcdef

func TestBackupRestoreRekeyDoctorCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CALENDAR_DB", filepath.Join(dir, "hub", "c.db"))
	t.Setenv("CALENDAR_MASTER_KEY", testKeyB64)
	t.Setenv("CALENDAR_BACKUP_DIR", filepath.Join(dir, "bk"))
	var out bytes.Buffer
	if err := run([]string{"token", "create", "--name", "me", "--kind", "device", "--scope", "admin"}, &out); err != nil {
		t.Fatal(err)
	}
	// doctor: a young install is healthy with warnings, never failures
	out.Reset()
	if err := run([]string{"doctor"}, &out); err != nil || !strings.Contains(out.String(), "[ok  ] master key") || !strings.Contains(out.String(), "[warn] last backup") {
		t.Fatalf("%v\n%s", err, out.String())
	}
	out.Reset()
	if err := run([]string{"backup"}, &out); err != nil || !strings.Contains(out.String(), "verified") {
		t.Fatalf("%v %s", err, out.String())
	}
	files, _ := filepath.Glob(filepath.Join(dir, "bk", "calendar-*.db.enc"))
	if len(files) != 1 {
		t.Fatalf("%v", files)
	}
	// lose the database, restore it
	os.RemoveAll(filepath.Join(dir, "hub"))
	out.Reset()
	if err := run([]string{"restore", "--from", files[0]}, &out); err != nil {
		t.Fatalf("%v %s", err, out.String())
	}
	out.Reset()
	run([]string{"token", "list"}, &out)
	if !strings.Contains(out.String(), "active") {
		t.Fatalf("token not restored: %s", out.String())
	}
	if err := run([]string{"restore", "--from", files[0]}, &out); err == nil {
		t.Fatal("restore over an existing database without --force")
	}
	// a different master key cannot read the backup
	t.Setenv("CALENDAR_MASTER_KEY", "ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA=")
	if err := run([]string{"restore", "--from", files[0], "--force"}, &out); err == nil {
		t.Fatal("restored with the wrong key")
	}
	t.Setenv("CALENDAR_MASTER_KEY", testKeyB64)
	// rekey writes a safety backup and reseals
	kf := filepath.Join(dir, "new.key")
	os.WriteFile(kf, []byte("ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA="), 0o600)
	out.Reset()
	if err := run([]string{"rekey", "--new-key-file", kf}, &out); err != nil || !strings.Contains(out.String(), "re-sealed 0 accounts") {
		t.Fatalf("%v %s", err, out.String())
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "bk", "pre-rekey", "calendar-*.enc")); len(m) != 1 {
		t.Fatal("no safety backup before rekey")
	}
	// --plain needs no key
	t.Setenv("CALENDAR_MASTER_KEY", "")
	out.Reset()
	if err := run([]string{"backup", "--plain", "--out", filepath.Join(dir, "plain")}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"backup", "--out", filepath.Join(dir, "x")}, &out); err == nil || !strings.Contains(err.Error(), "--plain") {
		t.Fatalf("%v", err)
	}
}
