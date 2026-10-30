package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"calendar/server/internal/backup"
	"calendar/server/internal/config"
	"calendar/server/internal/db"
	"calendar/server/internal/oauth"
	"calendar/server/internal/rekey"
	"calendar/server/internal/vault"
)

func backupDir(cfg *config.Config) string {
	if cfg.BackupDir != "" {
		return cfg.BackupDir
	}
	return filepath.Join(filepath.Dir(cfg.DBPath), "backups")
}

// backupCmd: calendard backup [--out DIR] [--plain]
func backupCmd(ctx context.Context, cfg *config.Config, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	dir := fs.String("out", backupDir(cfg), "directory for the backup file")
	plain := fs.Bool("plain", cfg.BackupPlain, "do not encrypt (the file then contains your Google refresh tokens in sealed form only, but all event data in clear)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var key []byte
	if !*plain {
		if err := cfg.RequireMasterKey(); err != nil {
			return fmt.Errorf("%w (or pass --plain)", err)
		}
		key = cfg.MasterKey
	}
	d, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()
	path, size, err := backup.Create(ctx, backup.Options{DB: d, Key: key, Dir: *dir, Keep: cfg.BackupKeep})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "backup written: %s (%d bytes, verified)\n", path, size)
	return nil
}

// restoreCmd: calendard restore --from FILE [--force]
func restoreCmd(ctx context.Context, cfg *config.Config, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	from := fs.String("from", "", "backup file to restore")
	force := fs.Bool("force", false, "replace an existing database (it is kept as <db>.pre-restore-<time>)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" {
		return errors.New("usage: calendard restore --from FILE [--force]")
	}
	var key []byte
	if backup.IsEncrypted(*from) {
		if err := cfg.RequireMasterKey(); err != nil {
			return fmt.Errorf("this backup is encrypted: %w", err)
		}
		key = cfg.MasterKey
	}
	if err := backup.Restore(ctx, *from, cfg.DBPath, key, *force, time.Now()); err != nil {
		return err
	}
	fmt.Fprintf(out, "restored %s into %s\nstart calendard again; apps will refetch because the revision was advanced by %d\n", *from, cfg.DBPath, backup.RevBump)
	return nil
}

// rekeyCmd: calendard rekey --new-key-file FILE
func rekeyCmd(ctx context.Context, cfg *config.Config, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rekey", flag.ContinueOnError)
	nf := fs.String("new-key-file", "", "file holding the NEW master key (32 bytes, base64 or hex)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.RequireMasterKey(); err != nil {
		return err
	}
	if *nf == "" {
		return errors.New("usage: calendard rekey --new-key-file FILE   (stop calendard first; the current key is read from the environment)")
	}
	raw, err := os.ReadFile(*nf)
	if err != nil {
		return err
	}
	newKey, err := config.ParseKey(string(raw))
	if err != nil {
		return err
	}
	oldV, _ := vault.New(cfg.MasterKey)
	newV, err := vault.New(newKey)
	if err != nil {
		return err
	}
	d, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()
	// take a safety copy first
	if _, _, err := backup.Create(ctx, backup.Options{DB: d, Key: cfg.MasterKey, Dir: filepath.Join(backupDir(cfg), "pre-rekey"), Keep: 3}); err != nil {
		return fmt.Errorf("safety backup: %w", err)
	}
	res, err := rekey.Run(ctx, d, oldV, newV)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "re-sealed %d accounts and %d automations under the new key.\nNow replace the master_key secret with the new key and start calendard.\nOld backups can only be restored with the OLD key: keep it, and take a fresh backup after starting.\n", res.Accounts, res.Automations)
	return nil
}

type check struct {
	name, detail string
	ok, warn     bool
}

// doctorCmd: calendard doctor. Prints a checklist; exit status is non-zero if a required check fails.
func doctorCmd(ctx context.Context, cfg *config.Config, out io.Writer) error {
	var cs []check
	add := func(name string, err error, okDetail string) {
		if err != nil {
			cs = append(cs, check{name: name, detail: err.Error()})
			return
		}
		cs = append(cs, check{name: name, ok: true, detail: okDetail})
	}
	warn := func(name, detail string) { cs = append(cs, check{name: name, ok: true, warn: true, detail: detail}) }

	add("master key", cfg.RequireMasterKey(), "32 bytes")
	d, derr := db.Open(ctx, cfg.DBPath)
	add("database", derr, cfg.DBPath)
	if derr == nil {
		defer d.Close()
		var n int
		_ = d.R.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n)
		var integ string
		ierr := d.R.QueryRow(`PRAGMA integrity_check`).Scan(&integ)
		if ierr == nil && integ != "ok" {
			ierr = errors.New(integ)
		}
		add("database integrity", ierr, fmt.Sprintf("ok, %d migrations", n))
		var accts, bad int
		_ = d.R.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status IN ('needs_reauth','error')),0) FROM accounts`).Scan(&accts, &bad)
		if accts == 0 {
			warn("accounts", "none connected yet")
		} else if bad > 0 {
			warn("accounts", fmt.Sprintf("%d of %d need attention (see the app's account list)", bad, accts))
		} else {
			add("accounts", nil, fmt.Sprintf("%d connected", accts))
		}
		var tokens int
		_ = d.R.QueryRow(`SELECT COUNT(*) FROM tokens WHERE revoked_at IS NULL`).Scan(&tokens)
		if tokens == 0 {
			warn("tokens", "no API token yet: run `calendard token create --name iphone --kind device --scope admin`")
		} else {
			add("tokens", nil, fmt.Sprintf("%d active", tokens))
		}
		var last string
		if err := d.R.QueryRow(`SELECT value FROM meta WHERE key='last_backup_at'`).Scan(&last); err == nil {
			var n int64
			fmt.Sscan(last, &n)
			age := time.Since(time.Unix(n, 0))
			if age > 36*time.Hour {
				warn("last backup", fmt.Sprintf("%d hours ago", int(age.Hours())))
			} else {
				add("last backup", nil, fmt.Sprintf("%d hours ago", int(age.Hours())))
			}
		} else {
			warn("last backup", "never; set CALENDAR_BACKUP_DIR or run `calendard backup`")
		}
	}
	hasValues := cfg.GoogleClientID != "" && cfg.GoogleClientSecret != ""
	if cfg.GoogleClientFile == "" && !hasValues {
		warn("google sign-in", "CALENDAR_GOOGLE_CLIENT_FILE (or _ID and _SECRET) not set: accounts cannot be connected")
	} else {
		var err error
		if cfg.GoogleClientFile != "" {
			_, err = oauth.LoadClientFile(cfg.GoogleClientFile)
			add("google client file", err, cfg.GoogleClientFile)
		} else {
			add("google client", nil, "from CALENDAR_GOOGLE_CLIENT_ID and _SECRET")
		}
		if err == nil && cfg.PublicURL == "" {
			add("public url", errors.New("CALENDAR_PUBLIC_URL is required when Google is enabled"), "")
		} else if err == nil {
			add("public url", nil, strings.TrimRight(cfg.PublicURL, "/")+"/auth/google/callback (must match the Google redirect URI)")
		}
	}
	if ap, err := loadAPNs(cfg); ap == nil && err == nil {
		warn("push", "CALENDAR_APNS_KEY_FILE / CALENDAR_APNS_KEY not set: no notifications")
	} else {
		add("apns key", err, "topic "+cfg.APNsTopic)
	}
	if cfg.BackupDir != "" {
		err := os.MkdirAll(cfg.BackupDir, 0o700)
		if err == nil {
			f, e2 := os.CreateTemp(cfg.BackupDir, ".doctor-*")
			if e2 == nil {
				f.Close()
				os.Remove(f.Name())
			}
			err = e2
		}
		add("backup directory", err, cfg.BackupDir+" is writable")
	}
	if detail, w := doctorTelemetry(ctx); w {
		warn("telemetry", detail)
	} else {
		add("telemetry", nil, detail)
	}
	failed := 0
	for _, c := range cs {
		mark := "ok  "
		switch {
		case !c.ok:
			mark, failed = "FAIL", failed+1
		case c.warn:
			mark = "warn"
		}
		fmt.Fprintf(out, "[%s] %-20s %s\n", mark, c.name, c.detail)
	}
	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	return nil
}
