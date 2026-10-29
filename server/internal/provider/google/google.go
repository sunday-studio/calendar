// Package google is the Google Calendar v3 REST provider.
package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
	"calendar/server/internal/provider/httpx"
	"calendar/server/internal/telemetry"
)

const (
	DefaultBaseURL  = "https://www.googleapis.com/calendar/v3"
	DefaultTokenURL = "https://oauth2.googleapis.com/token"
	pageSize        = "250"
)

type Config struct {
	BaseURL, TokenURL      string
	ClientID, ClientSecret string
	// Refresh returns the unsealed refresh token of an account.
	Refresh func(ctx context.Context, accountID string) (string, error)
	// Transport is the base round tripper (default: httpx.Transport over http.DefaultTransport).
	Transport http.RoundTripper
}

type Provider struct {
	cfg Config
	rt  http.RoundTripper
	mu  sync.Mutex
	cl  map[string]*cached
}

type cached struct {
	refresh string
	client  *http.Client
}

func New(cfg Config) *Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = DefaultTokenURL
	}
	rt := cfg.Transport
	if rt == nil {
		rt = &httpx.Transport{Provider: "google"}
	}
	return &Provider{cfg: cfg, rt: rt, cl: map[string]*cached{}}
}

func (p *Provider) Kind() provider.Kind { return provider.KindGoogle }
func (p *Provider) Caps() provider.Caps {
	return provider.Caps{CanWrite: true, CanRSVP: true, CanConference: true, SyncStyle: "token"}
}

// client returns an authorised client for the account, rebuilt when its refresh token changes.
func (p *Provider) client(ctx context.Context, accountID string) (*http.Client, error) {
	rt, err := p.cfg.Refresh(ctx, accountID)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.cl[accountID]; ok && c.refresh == rt {
		return c.client, nil
	}
	conf := &oauth2.Config{ClientID: p.cfg.ClientID, ClientSecret: p.cfg.ClientSecret,
		Endpoint: oauth2.Endpoint{TokenURL: p.cfg.TokenURL, AuthStyle: oauth2.AuthStyleInParams}}
	hc := &http.Client{Transport: p.rt, Timeout: 60 * time.Second}
	// Background ctx: the token source outlives this request.
	bg := context.WithValue(telemetry.WithProvider(context.Background(), "google"), oauth2.HTTPClient, hc)
	ts := oauth2.ReuseTokenSource(nil, meteredSource{src: conf.TokenSource(bg, &oauth2.Token{RefreshToken: rt}), ctx: context.Background()})
	cl := &http.Client{Transport: &oauth2.Transport{Source: ts, Base: p.rt}, Timeout: 60 * time.Second}
	p.cl[accountID] = &cached{refresh: rt, client: cl}
	return cl, nil
}

func (p *Provider) drop(accountID string) {
	p.mu.Lock()
	delete(p.cl, accountID)
	p.mu.Unlock()
}

// HTTPError is an unclassified non-2xx response.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("google: HTTP %d: %s", e.Status, e.Body) }

func mapTransportErr(err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		if re.ErrorCode == "invalid_grant" || (re.Response != nil && (re.Response.StatusCode == 400 || re.Response.StatusCode == 401) && re.ErrorCode != "invalid_client") {
			return fmt.Errorf("%w: %s", provider.ErrNeedsReauth, re.ErrorCode)
		}
	}
	return err
}

type gErrBody struct {
	Error struct {
		Errors []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
		Status string `json:"status"`
	} `json:"error"`
}

func classify(resp *http.Response, body []byte) error {
	switch resp.StatusCode {
	case 401:
		return provider.ErrNeedsReauth
	case 410:
		var eb gErrBody
		_ = json.Unmarshal(body, &eb)
		reason := "unknown"
		if len(eb.Error.Errors) > 0 && eb.Error.Errors[0].Reason != "" {
			reason = eb.Error.Errors[0].Reason
		}
		return fmt.Errorf("%w: google reason %s", provider.ErrCursorExpired, reason)
	case 412:
		return provider.ErrPreconditionFailed
	case 429:
		ra, _ := httpx.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		return provider.ErrRateLimited{RetryAfter: ra}
	case 403:
		var b gErrBody
		_ = json.Unmarshal(body, &b)
		for _, e := range b.Error.Errors {
			switch e.Reason {
			case "rateLimitExceeded", "userRateLimitExceeded", "quotaExceeded", "dailyLimitExceeded":
				ra, _ := httpx.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
				return provider.ErrRateLimited{RetryAfter: ra}
			case "domainPolicy", "accessNotConfigured", "adminPolicyEnforced":
				return provider.ErrForbiddenAdmin
			}
		}
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 300 {
		s = s[:300]
	}
	return &HTTPError{Status: resp.StatusCode, Body: s}
}

func (p *Provider) get(ctx context.Context, accountID, path string, q url.Values, out any) error {
	ctx = telemetry.WithProvider(ctx, "google")
	for attempt := 0; ; attempt++ {
		cl, err := p.client(ctx, accountID)
		if err != nil {
			return err
		}
		u := p.cfg.BaseURL + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		began := time.Now()
		resp, err := cl.Do(req)
		if err != nil {
			observe(ctx, listOp(ctx, path), began, nil, nil, err)
			return mapTransportErr(err)
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		observe(ctx, listOp(ctx, path), began, resp, body, rerr)
		if rerr != nil {
			return rerr
		}
		if resp.StatusCode == 401 && attempt == 0 {
			p.drop(accountID) // maybe a stale access token; mint a fresh one once
			continue
		}
		if resp.StatusCode/100 != 2 {
			return classify(resp, body)
		}
		return json.Unmarshal(body, out)
	}
}

type gCalendar struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	SummaryOverride string `json:"summaryOverride"`
	BackgroundColor string `json:"backgroundColor"`
	TimeZone        string `json:"timeZone"`
	AccessRole      string `json:"accessRole"`
	Primary         bool   `json:"primary"`
	Selected        *bool  `json:"selected"`
	Hidden          bool   `json:"hidden"`
	Deleted         bool   `json:"deleted"`
}

// Discover lists all calendars (paged). calendarList is small, so it is re-read in full each
// pass instead of using its own syncToken; that also detects removed calendars.
func (p *Provider) Discover(ctx context.Context, acct domain.Account) ([]provider.RemoteCalendar, error) {
	var out []provider.RemoteCalendar
	page := ""
	for {
		q := url.Values{"maxResults": {"250"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var r struct {
			Items         []gCalendar `json:"items"`
			NextPageToken string      `json:"nextPageToken"`
		}
		if err := p.get(ctx, acct.ID, "/users/me/calendarList", q, &r); err != nil {
			return nil, err
		}
		for _, c := range r.Items {
			if c.Deleted {
				continue
			}
			access := "reader"
			switch c.AccessRole {
			case "owner":
				access = "owner"
			case "writer":
				access = "writer"
			case "freeBusyReader":
				access = "freebusy"
			}
			name := c.SummaryOverride
			if name == "" {
				name = c.Summary
			}
			out = append(out, provider.RemoteCalendar{
				RemoteID: c.ID, Name: name, Color: c.BackgroundColor, TZID: c.TimeZone, Access: access,
				IsPrimary: c.Primary, Visible: !c.Hidden && (c.Selected == nil || *c.Selected),
			})
		}
		if r.NextPageToken == "" {
			return out, nil
		}
		page = r.NextPageToken
	}
}

// Sync fetches one page of events. No cursor = full sync; with a cursor, deletions are included.
func (p *Provider) Sync(ctx context.Context, acct domain.Account, cal domain.Calendar, req provider.SyncRequest) (provider.SyncBatch, error) {
	q := url.Values{"singleEvents": {"false"}, "maxResults": {pageSize}}
	if req.Cursor != "" {
		q.Set("syncToken", req.Cursor)
		q.Set("showDeleted", "true")
	}
	if req.PageToken != "" {
		q.Set("pageToken", req.PageToken)
	}
	var r struct {
		Items         []gEvent `json:"items"`
		NextPageToken string   `json:"nextPageToken"`
		NextSyncToken string   `json:"nextSyncToken"`
		TimeZone      string   `json:"timeZone"`
	}
	if err := p.get(ctx, acct.ID, "/calendars/"+url.PathEscape(cal.RemoteID)+"/events", q, &r); err != nil {
		return provider.SyncBatch{}, err
	}
	if cal.TZID == "" {
		cal.TZID = r.TimeZone
	}
	b := provider.SyncBatch{PageToken: r.NextPageToken}
	if r.NextPageToken == "" {
		b.NextCursor = r.NextSyncToken
	}
	for _, g := range r.Items {
		b.Events = append(b.Events, mapEvent(g, cal))
	}
	return b, nil
}

var _ provider.Provider = (*Provider)(nil)
