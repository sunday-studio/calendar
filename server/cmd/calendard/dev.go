package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"calendar/server/internal/accounts"
	"calendar/server/internal/config"
	"calendar/server/internal/db"
	"calendar/server/internal/oauth"
	"calendar/server/internal/vault"
)

// devCmd holds development-only helpers; every one refuses unless CALENDAR_DEV=1.
func devCmd(ctx context.Context, cfg *config.Config, args []string, out io.Writer) error {
	if !cfg.Dev {
		return errors.New("dev commands are for development only: set CALENDAR_DEV=1")
	}
	if len(args) == 0 || args[0] != "seed-account" {
		return errors.New("usage: calendard dev seed-account --provider google --email a@example.com [--name N] [--refresh-token T]")
	}
	fs := flag.NewFlagSet("dev seed-account", flag.ContinueOnError)
	prov := fs.String("provider", "google", "google")
	email := fs.String("email", "", "account email (must exist in the fake Google, see cmd/fakegoogle)")
	name := fs.String("name", "", "display name")
	rt := fs.String("refresh-token", "", "default: fake-refresh-<email> (cmd/fakegoogle's contract)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *prov != "google" {
		return errors.New("--provider: only google")
	}
	if !strings.Contains(*email, "@") {
		return errors.New("--email is required")
	}
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
	if *rt == "" {
		*rt = "fake-refresh-" + *email
	}
	if *name == "" {
		*name, _, _ = strings.Cut(*email, "@")
	}
	acc := &accounts.Service{DB: d, Vault: v, Sessions: oauth.Sessions{DB: d}}
	// subject matches gtest.Subject, so a later real "connect" against the fake maps to the same account
	id, err := acc.SeedDev(ctx, "fake-sub-"+*email, *email, *name, *rt)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, id)
	return nil
}
