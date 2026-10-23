package ics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"calendar/server/internal/provider"
	"calendar/server/internal/provider/httpx"
)

const (
	DefaultMaxBytes     = 20 << 20
	DefaultTimeout      = 20 * time.Second
	DefaultMaxRedirects = 3
)

// UserError carries a plain-language message that is safe to show to the user and to log:
// it never contains the feed URL (URLs can embed secret tokens).
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userErr(format string, a ...any) *UserError { return &UserError{Msg: fmt.Sprintf(format, a...)} }

var errBlocked = errors.New("ics: address not allowed")

// Normalize validates a user-supplied feed URL and returns its canonical form
// (webcal:// and webcals:// become https://).
func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	bad := userErr("That does not look like a calendar link. Use an https:// or webcal:// address.")
	if raw == "" || len(raw) > 2048 {
		return "", bad
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", bad
	}
	switch strings.ToLower(u.Scheme) {
	case "webcal", "webcals":
		u.Scheme = "https"
	case "https", "http":
		u.Scheme = strings.ToLower(u.Scheme)
	default:
		return "", bad
	}
	if u.Hostname() == "" {
		return "", bad
	}
	u.Fragment = ""
	return u.String(), nil
}

// ExternalID is the stable account identity of a feed: sha256 of its normalised URL.
func ExternalID(normalised string) string {
	sum := sha256.Sum256([]byte(normalised))
	return hex.EncodeToString(sum[:])
}

// Host returns the host of a feed URL (the only part ever shown).
func Host(normalised string) string {
	u, err := url.Parse(normalised)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"::/128", "64:ff9b::/96", "100::/64", "2001:db8::/32",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// IsPublic reports whether ip is an address a feed may be fetched from.
func IsPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.Is6() { // NAT64 embeds an IPv4 address; judge that one
		if b := ip.As16(); netip.MustParsePrefix("64:ff9b::/96").Contains(ip) {
			return IsPublic(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
		}
	}
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// cursor is stored in calendars.sync_cursor.
type cursor struct {
	ETag         string `json:"e,omitempty"`
	LastModified string `json:"l,omitempty"`
	Hash         string `json:"h,omitempty"`
}

func decodeCursor(s string) cursor {
	var c cursor
	if s != "" {
		_ = json.Unmarshal([]byte(s), &c)
	}
	return c
}

func (c cursor) String() string {
	b, _ := json.Marshal(c)
	return string(b)
}

type fetched struct {
	notModified bool
	body        []byte
	cur         cursor
}

// safeClient builds an http.Client whose every connection (first hop and redirects) is checked
// against policy at dial time, on the address actually being connected to. That defeats DNS
// rebinding and redirects to internal hosts. Proxies are ignored on purpose.
func safeClient(policy func(netip.AddrPort) bool, timeout time.Duration, maxRedirects int, base http.RoundTripper) *http.Client {
	rt := base
	if rt == nil {
		d := &net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				ap, err := netip.ParseAddrPort(address)
				if err != nil {
					return errBlocked
				}
				if !policy(netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())) {
					return errBlocked
				}
				return nil
			},
		}
		rt = &http.Transport{
			Proxy:                 nil,
			DialContext:           d.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: timeout,
			MaxIdleConns:          2,
			IdleConnTimeout:       30 * time.Second,
			ForceAttemptHTTP2:     true,
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: traced(rt),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return userErr("The calendar link redirects too many times.")
			}
			if s := req.URL.Scheme; s != "https" && s != "http" {
				return userErr("The calendar link redirects somewhere that is not a web address.")
			}
			return nil
		},
	}
}

// cleanErr turns a transport error into a message that never includes the URL.
func cleanErr(err error) error {
	if err == nil {
		return nil
	}
	var ue *UserError
	if errors.As(err, &ue) {
		return ue
	}
	switch {
	case errors.Is(err, errBlocked):
		return userErr("That address points to a private or local network, which is not allowed.")
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return userErr("The server took too long to respond.")
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return userErr("Could not find that server. Check the address.")
	}
	var uerr *url.Error
	if errors.As(err, &uerr) && strings.Contains(uerr.Err.Error(), "certificate") {
		return userErr("The server's security certificate was not accepted.")
	}
	return userErr("Could not reach that address.")
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func (p *Provider) fetch(ctx context.Context, raw string, prev cursor) (*fetched, error) {
	u, err := Normalize(raw)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, userErr("That does not look like a calendar link.")
	}
	req.Header.Set("Accept", "text/calendar, text/plain;q=0.5, */*;q=0.1")
	req.Header.Set("User-Agent", "calendard-ics/1")
	if prev.ETag != "" {
		req.Header.Set("If-None-Match", prev.ETag)
	}
	if prev.LastModified != "" {
		req.Header.Set("If-Modified-Since", prev.LastModified)
	}
	began := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		observeFetch(ctx, began, 0, err)
		return nil, cleanErr(err)
	}
	defer resp.Body.Close()
	observeFetch(ctx, began, resp.StatusCode, nil)
	switch {
	case resp.StatusCode == http.StatusNotModified && (prev.ETag != "" || prev.LastModified != ""):
		return &fetched{notModified: true, cur: prev}, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		ra, _ := httpx.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		return nil, provider.ErrRateLimited{RetryAfter: ra}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return nil, userErr("The server refused access (HTTP %d). The link may need a secret token or may have expired.", resp.StatusCode)
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		return nil, userErr("The calendar was not found at that address (HTTP %d).", resp.StatusCode)
	case resp.StatusCode/100 != 2:
		return nil, userErr("The server answered HTTP %d.", resp.StatusCode)
	}
	if resp.ContentLength > p.maxBytes {
		return nil, userErr("The calendar file is larger than %d MB.", p.maxBytes>>20)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, p.maxBytes+1))
	if err != nil {
		return nil, cleanErr(err)
	}
	if int64(len(body)) > p.maxBytes {
		return nil, userErr("The calendar file is larger than %d MB.", p.maxBytes>>20)
	}
	sum := sha256.Sum256(body)
	return &fetched{body: body, cur: cursor{
		ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified"), Hash: hex.EncodeToString(sum[:]),
	}}, nil
}
