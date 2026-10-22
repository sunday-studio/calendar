// Package ics is a read-only provider for iCalendar (.ics) feeds fetched over HTTP(S).
package ics

import (
	"context"
	"net/http"
	"net/netip"
	"time"

	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
)

const KindICS provider.Kind = "ics"

// CalendarRemoteID is the id of the one synthetic calendar every feed account has.
const CalendarRemoteID = "feed"

type Config struct {
	// URL returns the unsealed feed URL of an account.
	URL func(ctx context.Context, accountID string) (string, error)
	// AllowPrivate disables the SSRF guard (CALENDAR_ICS_ALLOW_PRIVATE=1): loopback, private
	// and link-local addresses become reachable. For feeds on the user's own network.
	AllowPrivate bool
	MaxBytes     int64         // default 20 MB
	Timeout      time.Duration // default 20 s
	MaxRedirects int           // default 3
	// Policy overrides the address policy (tests). Default: public addresses only, or anything when AllowPrivate.
	Policy func(netip.AddrPort) bool
	// Transport replaces the network layer entirely (tests; skips the SSRF dial guard).
	Transport http.RoundTripper
}

type Provider struct {
	cfg      Config
	client   *http.Client
	maxBytes int64
}

func New(cfg Config) *Provider {
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxRedirects <= 0 {
		cfg.MaxRedirects = DefaultMaxRedirects
	}
	policy := cfg.Policy
	if policy == nil {
		policy = func(ap netip.AddrPort) bool { return cfg.AllowPrivate || IsPublic(ap.Addr()) }
	}
	return &Provider{cfg: cfg, maxBytes: cfg.MaxBytes, client: safeClient(policy, cfg.Timeout, cfg.MaxRedirects, cfg.Transport)}
}

func (p *Provider) Kind() provider.Kind { return KindICS }
func (p *Provider) Caps() provider.Caps { return provider.Caps{SyncStyle: "poll"} }

// Info describes a feed that fetched and parsed.
type Info struct {
	Name   string // X-WR-CALNAME, may be empty
	Events int
}

// Check fetches and parses a feed (no conditional GET). Errors that are *UserError are safe to show.
func (p *Provider) Check(ctx context.Context, normalised string) (Info, error) {
	f, err := p.fetch(ctx, normalised, cursor{})
	if err != nil {
		return Info{}, err
	}
	feed, err := Parse(f.body)
	if err != nil {
		return Info{}, err
	}
	return Info{Name: feed.Name, Events: len(feed.Events)}, nil
}

// Discover returns the single synthetic calendar, always read-only.
func (p *Provider) Discover(_ context.Context, acct domain.Account) ([]provider.RemoteCalendar, error) {
	name := acct.DisplayName
	if name == "" {
		name = acct.Email
	}
	return []provider.RemoteCalendar{{RemoteID: CalendarRemoteID, Name: name, Access: "reader", Visible: true}}, nil
}

// Sync downloads the feed with a conditional GET. An unchanged feed (304, or identical bytes)
// returns an empty batch; a changed one returns every event with Replace set, so the engine
// tombstones whatever the feed no longer contains.
func (p *Provider) Sync(ctx context.Context, acct domain.Account, cal domain.Calendar, req provider.SyncRequest) (provider.SyncBatch, error) {
	raw, err := p.cfg.URL(ctx, acct.ID)
	if err != nil {
		return provider.SyncBatch{}, err
	}
	prev := decodeCursor(req.Cursor)
	f, err := p.fetch(ctx, raw, prev)
	if err != nil {
		return provider.SyncBatch{}, err
	}
	if f.notModified {
		return provider.SyncBatch{NextCursor: req.Cursor}, nil
	}
	if prev.Hash != "" && prev.Hash == f.cur.Hash {
		return provider.SyncBatch{NextCursor: f.cur.String()}, nil
	}
	feed, err := Parse(f.body)
	if err != nil {
		return provider.SyncBatch{}, err
	}
	for i := range feed.Events {
		feed.Events[i].CalendarID = cal.ID
	}
	return provider.SyncBatch{Events: feed.Events, Replace: true, NextCursor: f.cur.String()}, nil
}

func (p *Provider) Create(context.Context, domain.Account, domain.Calendar, provider.EventInput) (provider.Result, error) {
	return provider.Result{}, provider.ErrNotSupported
}
func (p *Provider) Update(context.Context, provider.Ref, provider.Patch, provider.Scope) (provider.Result, error) {
	return provider.Result{}, provider.ErrNotSupported
}
func (p *Provider) Delete(context.Context, provider.Ref, provider.Scope) (provider.Result, error) {
	return provider.Result{}, provider.ErrNotSupported
}
func (p *Provider) RSVP(context.Context, provider.Ref, domain.Response, string, provider.Scope) (provider.Result, error) {
	return provider.Result{}, provider.ErrNotSupported
}
func (p *Provider) AddConference(context.Context, provider.Ref) (provider.Result, error) {
	return provider.Result{}, provider.ErrNotSupported
}
func (p *Provider) Move(context.Context, provider.Ref, domain.Calendar) (provider.Result, error) {
	return provider.Result{}, provider.ErrNotSupported
}

var _ provider.Provider = (*Provider)(nil)
