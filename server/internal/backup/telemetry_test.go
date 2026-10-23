package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"calendar/server/internal/telemetry/telemetrytest"
)

type A = telemetrytest.Attrs

func TestTelemetryBackupSuccessAndFailure(t *testing.T) {
	rec := telemetrytest.Install(t)
	d, _ := openDB(t)
	seed(t, d)
	dir := filepath.Join(t.TempDir(), "backups")
	off := RegisterGauges(d)
	defer off()
	if _, _, err := Create(context.Background(), Options{DB: d, Key: key, Dir: dir, Now: func() time.Time { return time.Now() }}); err != nil {
		t.Fatal(err)
	}
	if rec.Sum(t, "calendar.backup.runs", A{"calendar.outcome": "ok"}) != 1 || rec.HistCount(t, "calendar.backup.duration", nil) != 1 {
		t.Fatalf("success accounting:\n%s", rec.Dump(t))
	}
	sp := rec.SpansNamed("backup.run")
	if len(sp) != 1 || telemetrytest.SpanAttr(sp[0], "calendar.outcome") != "ok" {
		t.Fatal(rec.SpanNames())
	}
	if g := rec.Gauge(t, "calendar.backup.last_success_age_seconds", nil); len(g) != 1 || g[0] < 0 || g[0] > 60 {
		t.Fatalf("last_success_age %v", g)
	}
	if g := rec.Gauge(t, "calendar.backup.last_size_bytes", nil); len(g) != 1 || g[0] <= 0 {
		t.Fatalf("size gauge %v", g)
	}
	// failure: the target directory cannot be created (a file is in the way)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := writeFile(blocker); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Create(context.Background(), Options{DB: d, Key: key, Dir: filepath.Join(blocker, "sub")}); err == nil {
		t.Fatal("expected a failure")
	}
	if rec.Sum(t, "calendar.backup.runs", A{"calendar.outcome": "failed"}) != 1 {
		t.Fatal("failed outcome missing")
	}
	if rec.Sum(t, "calendar.errors", A{"calendar.component": "backup"}) != 1 {
		t.Fatalf("backup failure not recorded as an error:\n%s", rec.Dump(t))
	}
}

func writeFile(p string) error { return os.WriteFile(p, []byte("x"), 0o600) }
