// Command calendard is the calendar hub.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"calendar/server/internal/accounts"
	"calendar/server/internal/agenda"
	"calendar/server/internal/alfather"
	"calendar/server/internal/api"
	"calendar/server/internal/authn"
	"calendar/server/internal/automate"
	"calendar/server/internal/backup"
	"calendar/server/internal/config"
	"calendar/server/internal/db"
	"calendar/server/internal/events"
	"calendar/server/internal/fixtures"
	"calendar/server/internal/hubmcp"
	"calendar/server/internal/oauth"
	"calendar/server/internal/provider"
	"calendar/server/internal/provider/google"
	"calendar/server/internal/provider/ics"
	"calendar/server/internal/push"
	"calendar/server/internal/stream"
	syncer "calendar/server/internal/sync"
	"calendar/server/internal/vault"
)

var version = "dev"

const usage = `usage: calendard <command>
  serve [--fixtures]                       run the hub (fixtures: canned data, no DB)
  token create --name N --kind device|script --scope read|write|admin
  token list
  token revoke <id>
  migrate
  health
  doctor                                   check configuration, database, keys, backups
  backup [--out DIR] [--plain]             verified (encrypted) snapshot of the database
  restore --from FILE [--force]            install a backup (stop the hub first)
  dev seed-account --provider google --email E   (CALENDAR_DEV=1 only) account with a fake refresh token, no consent
  rekey --new-key-file FILE                re-seal secrets under a new master key (stop the hub first)`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	switch args[0] {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		fx := fs.Bool("fixtures", false, "serve canned data (no DB, no master key; any cal_ bearer accepted)")
		fxWrite := fs.String("fixtures-write", "ok", "with --fixtures: outcome of write requests, "+strings.Join(fixtures.Outcomes, "|")+" (or send header X-Fixture-Outcome)")
		fxState := fs.String("fixtures-state", "normal", "with --fixtures: "+strings.Join(fixtures.States, "|"))
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *fxState != "normal" && !*fx {
			return errors.New("--fixtures-state needs --fixtures")
		}
		if *fxWrite != "ok" && !*fx {
			return errors.New("--fixtures-write needs --fixtures")
		}
		if !fixtures.ValidOutcome(*fxWrite) {
			return fmt.Errorf("unknown --fixtures-write %q (want %s)", *fxWrite, strings.Join(fixtures.Outcomes, "|"))
		}
		if !fixtures.ValidState(*fxState) {
			return fmt.Errorf("unknown --fixtures-state %q (want %s)", *fxState, strings.Join(fixtures.States, "|"))
		}
		if *fx {
			return serveFixtures(ctx, cfg, *fxState, *fxWrite)
		}
		return serve(ctx, cfg)
	case "health":
		return health(ctx, cfg)
	case "migrate":
		d, err := db.Open(ctx, cfg.DBPath)
		if err != nil {
			return err
		}
		defer d.Close()
		fmt.Fprintln(out, "migrations applied")
		return nil
	case "token":
		return tokenCmd(ctx, cfg, args[1:], out)
	case "backup":
		return backupCmd(ctx, cfg, args[1:], out)
	case "restore":
		return restoreCmd(ctx, cfg, args[1:], out)
	case "rekey":
		return rekeyCmd(ctx, cfg, args[1:], out)
	case "doctor":
		return doctorCmd(ctx, cfg, out)
	case "dev":
		return devCmd(ctx, cfg, args[1:], out)
	}
	return errors.New(usage)
}

func serve(ctx context.Context, cfg *config.Config) error {
	if err := cfg.RequireMasterKey(); err != nil {
		return err
	}
	v, err := vault.New(cfg.MasterKey)
	if err != nil {
		return err
	}
	d, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	acc := &accounts.Service{DB: d, Vault: v, Sessions: oauth.Sessions{DB: d}}
	var cf *oauth.ClientFile
	switch {
	case cfg.GoogleClientFile != "":
		if cf, err = oauth.LoadClientFile(cfg.GoogleClientFile); err != nil {
			return err
		}
		if cfg.PublicURL == "" {
			return errors.New("CALENDAR_PUBLIC_URL is required when CALENDAR_GOOGLE_CLIENT_FILE is set")
		}
	case cfg.Dev && cfg.GoogleTokenURL != "":
		// end-to-end against a fake Google: no real client needed
		cf = &oauth.ClientFile{ClientID: "fake-client", ClientSecret: "fake-secret"}
		log.Warn("CALENDAR_DEV: using a placeholder Google client against " + cfg.GoogleTokenURL)
	default:
		log.Warn("CALENDAR_GOOGLE_CLIENT_FILE not set: Google sign-in disabled")
	}
	providers := map[string]provider.Provider{}
	if cf != nil {
		pub := cfg.PublicURL
		if pub == "" {
			pub = "http://" + cfg.Addr
		}
		acc.Google = oauth.NewGoogle(*cf, strings.TrimRight(pub, "/")+"/auth/google/callback")
		acc.Google.SetEndpoints(cfg.GoogleAuthURL, cfg.GoogleTokenURL, cfg.GoogleUserinfoURL)
		providers["google"] = google.New(google.Config{ClientID: cf.ClientID, ClientSecret: cf.ClientSecret, Refresh: acc.RefreshToken,
			BaseURL: cfg.GoogleAPIBase, TokenURL: cfg.GoogleTokenURL})
	}
	icsProv := ics.New(ics.Config{URL: acc.ICSURL, AllowPrivate: cfg.ICSAllowPrivate})
	providers["ics"] = icsProv
	acc.ICS = icsProv
	broker := &stream.Broker{Rev: d.Rev, Log: log}
	d.OnCommit(broker.PublishRev)
	disp := &push.Dispatcher{DB: d, Log: log}
	if cfg.APNsKeyFile != "" {
		ap, err := push.LoadAPNsFiles(cfg.APNsKeyFile, cfg.APNsKeyID, cfg.APNsTeamID, cfg.APNsTopic)
		if err != nil {
			return fmt.Errorf("apns: %w", err)
		}
		disp.Sender = ap
	} else {
		log.Warn("CALENDAR_APNS_KEY_FILE not set: push notifications disabled")
	}
	d.OnCommit(disp.Notify)
	acc.OnStatus = func(id, status string) {
		broker.Account(id, api.WireStatus(status))
		disp.AccountStatus(id, status)
	}
	mgr := &syncer.Manager{DB: d, Accounts: acc, Providers: providers, Log: log, Fast: cfg.SyncFast, Slow: cfg.SyncSlow,
		SSEClients: broker.Clients, OnSync: broker.Sync,
		Interval: map[string]time.Duration{"ics": cfg.ICSPoll}}
	mctx, stopSync := context.WithCancel(ctx)
	mgrDone := make(chan struct{})
	go func() { mgr.Run(mctx); close(mgrDone) }()
	defer func() { stopSync(); <-mgrDone }() // workers persist cursors per page, so stopping is safe
	store := &agenda.Store{DB: d}
	evSvc := &events.Service{DB: d, Accounts: acc, Providers: providers, Sync: mgr, Store: store, Log: log}
	pctx, stopPush := context.WithCancel(ctx)
	defer stopPush()
	auto := &automate.Service{DB: d, Vault: v, Store: store, Log: log}
	go auto.Run(pctx, 0)
	if cfg.BackupDir != "" {
		var bkey []byte
		if !cfg.BackupPlain {
			bkey = cfg.MasterKey
		}
		loc, _ := time.LoadLocation("Local")
		go backup.Schedule(pctx, backup.Options{DB: d, Key: bkey, Dir: cfg.BackupDir, Keep: cfg.BackupKeep, Log: log}, cfg.BackupHour, loc)
	} else {
		log.Warn("CALENDAR_BACKUP_DIR not set: no scheduled backups")
	}
	go disp.Run(pctx)
	go disp.RunSummary(pctx, store)
	s := &api.Server{Auth: authn.Store{DB: d}, Accounts: acc, Syncer: mgr, Stream: broker, Store: store, Events: evSvc, Automations: auto, BackupDir: cfg.BackupDir, Log: log,
		Limits: api.Limits{AuthFailLimit: cfg.AuthFailLimit, AuthFailWindow: cfg.AuthFailWindow, MCPPerMinute: cfg.MCPRate, MCPMaxBody: int64(cfg.MCPMaxBody)}, Name: "calendard", Version: version}
	if cfg.AlfatherURL != "" {
		s.Alfather = &alfather.Config{URL: cfg.AlfatherURL, ParseTool: cfg.AlfatherParseTool, Token: cfg.AlfatherToken}
		log.Info("alfather quick-add enabled", "tool", cfg.AlfatherParseTool)
	}
	if !cfg.MCPDisabled {
		s.MCP = &hubmcp.Hub{DB: d, Store: store, Events: evSvc, Log: log, Version: version}
	}
	srv := &http.Server{Addr: cfg.Addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.Addr, "version", version)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}

func serveFixtures(ctx context.Context, cfg *config.Config, state, write string) error {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	fs := &fixtures.Server{Data: fixtures.New(time.Now()), Name: "calendard-fixtures", Version: version, State: state, WriteOutcome: write}
	srv := &http.Server{Addr: cfg.Addr, Handler: fs.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening (FIXTURES, any 'cal_' bearer accepted)", "addr", cfg.Addr, "state", state, "write", write)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}

// health probes the local server (used by the container HEALTHCHECK).
func health(ctx context.Context, cfg *config.Config) error {
	addr := cfg.Addr
	if len(addr) > 0 && addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("unhealthy: HTTP %d", resp.StatusCode)
	}
	return nil
}

func tokenCmd(ctx context.Context, cfg *config.Config, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	d, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()
	st := authn.Store{DB: d}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("token create", flag.ContinueOnError)
		name := fs.String("name", "", "token name")
		kind := fs.String("kind", "device", "device|script")
		scope := fs.String("scope", "read", "read|write|admin")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" {
			return errors.New("--name is required")
		}
		sc, err := authn.ParseScope(*scope)
		if err != nil {
			return err
		}
		id, secret, err := st.Create(ctx, *name, *kind, sc)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "id:     %s\nsecret: %s\n(shown once; store it now)\n", id, secret)
	case "list":
		l, err := st.List(ctx)
		if err != nil {
			return err
		}
		for _, t := range l {
			state := "active"
			if t.RevokedAt != nil {
				state = "revoked"
			}
			last := "never"
			if t.LastUsedAt != nil {
				last = t.LastUsedAt.Format(time.RFC3339)
			}
			fmt.Fprintf(out, "%s  %-8s %-6s %-6s %s…  %s  last used %s\n", t.ID, t.Name, t.Kind, t.Scope, t.Prefix, state, last)
		}
	case "revoke":
		if len(args) != 2 {
			return errors.New("usage: token revoke <id>")
		}
		if err := st.Revoke(ctx, args[1]); err != nil {
			return err
		}
		fmt.Fprintln(out, "revoked")
	default:
		return errors.New(usage)
	}
	return nil
}
