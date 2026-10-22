package ics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
)

const tiny = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:a@x\r\nDTSTART:20261020T090000Z\r\nDTEND:20261020T100000Z\r\nSUMMARY:A\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

const token = "tok-9f8e7d6c5b4a"

func open() *Provider { // strict: loopback is blocked unless a test says otherwise
	return New(Config{URL: func(context.Context, string) (string, error) { return "", errors.New("unused") }})
}

func allowPorts(ports ...string) func(netip.AddrPort) bool {
	return func(ap netip.AddrPort) bool {
		for _, p := range ports {
			if fmt.Sprint(ap.Port()) == p {
				return true
			}
		}
		return IsPublic(ap.Addr())
	}
}

func portOf(s *httptest.Server) string { u, _ := url.Parse(s.URL); return u.Port() }

func providerFor(raw string, cfg Config) *Provider {
	cfg.URL = func(context.Context, string) (string, error) { return raw, nil }
	return New(cfg)
}

func syncOnce(p *Provider, cur string) (provider.SyncBatch, error) {
	return p.Sync(context.Background(), domain.Account{ID: "acct"}, domain.Calendar{ID: "cal"}, provider.SyncRequest{Cursor: cur})
}

func TestConditionalGET(t *testing.T) {
	var gets, conditional int32
	body := tiny
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&gets, 1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			atomic.AddInt32(&conditional, 1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 05 Oct 2026 10:00:00 GMT")
		w.Header().Set("Content-Type", "text/calendar")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	p := providerFor(srv.URL+"/"+token+"/cal.ics", Config{Policy: allowPorts(portOf(srv))})
	b1, err := syncOnce(p, "")
	if err != nil || len(b1.Events) != 1 || !b1.Replace || b1.NextCursor == "" || b1.Events[0].CalendarID != "cal" {
		t.Fatalf("%v %+v", err, b1)
	}
	if c := decodeCursor(b1.NextCursor); c.ETag != `"v1"` || c.LastModified == "" || c.Hash == "" {
		t.Fatalf("%+v", c)
	}
	b2, err := syncOnce(p, b1.NextCursor)
	if err != nil || len(b2.Events) != 0 || b2.Replace || b2.NextCursor != b1.NextCursor || conditional != 1 {
		t.Fatalf("304: %v %+v cond=%d", err, b2, conditional)
	}
	// a feed that changed bytes but kept no validators is detected by hash
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer srv2.Close()
	p2 := providerFor(srv2.URL+"/f.ics", Config{Policy: allowPorts(portOf(srv2))})
	c1, _ := syncOnce(p2, "")
	c2, err := syncOnce(p2, c1.NextCursor)
	if err != nil || len(c2.Events) != 0 || c2.Replace || c2.NextCursor == "" {
		t.Fatalf("same bytes: %v %+v", err, c2)
	}
	body = strings.Replace(tiny, "SUMMARY:A", "SUMMARY:B", 1)
	c3, err := syncOnce(p2, c2.NextCursor)
	if err != nil || len(c3.Events) != 1 || c3.Events[0].Title != "B" || !c3.Replace {
		t.Fatalf("changed: %v %+v", err, c3)
	}
}

func TestSSRFRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tiny) }))
	defer srv.Close()
	p := open()
	for _, raw := range []string{
		srv.URL + "/cal.ics",           // 127.0.0.1
		"http://10.1.2.3/cal.ics",      // private
		"http://192.168.1.1/cal.ics",   // private
		"http://169.254.169.254/",      // link-local metadata
		"http://[::1]:8080/cal.ics",    // v6 loopback
		"http://[::ffff:10.0.0.1]/x",   // v4-mapped private
		"http://localhost:9/cal.ics",   // name resolving to loopback
		"http://0.0.0.0/cal.ics",       // unspecified
		"http://100.64.0.1/cal.ics",    // CGNAT
		"webcal://127.0.0.1:1/cal.ics", // webcal maps to https, still blocked
	} {
		_, err := p.Check(context.Background(), raw)
		var ue *UserError
		if !errors.As(err, &ue) || !strings.Contains(ue.Msg, "private or local") {
			t.Errorf("%s: want blocked, got %v", raw, err)
		}
	}
	for _, raw := range []string{"file:///etc/passwd", "ftp://example.test/x.ics", "gopher://x/", "javascript:alert(1)", "", "not a url", "https:///x"} {
		_, err := p.Check(context.Background(), raw)
		var ue *UserError
		if !errors.As(err, &ue) || !strings.Contains(ue.Msg, "does not look like a calendar link") {
			t.Errorf("%q: want bad link, got %v", raw, err)
		}
	}
}

func TestAllowPrivateOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tiny) }))
	defer srv.Close()
	p := New(Config{AllowPrivate: true})
	info, err := p.Check(context.Background(), srv.URL+"/cal.ics")
	if err != nil || info.Events != 1 {
		t.Fatalf("%v %+v", err, info)
	}
}

func TestRedirects(t *testing.T) {
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tiny) }))
	defer private.Close()
	var hops int32
	mux := http.NewServeMux()
	mux.HandleFunc("/to-private", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, private.URL+"/secret.ics", http.StatusFound)
	})
	mux.HandleFunc("/to-file", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	})
	mux.HandleFunc("/to-meta", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	})
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hops, 1)
		http.Redirect(w, r, "/loop", http.StatusFound)
	})
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final.ics", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/final.ics", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tiny) })
	public := httptest.NewServer(mux)
	defer public.Close()
	// Only the "public" test server's port is allowed; every other loopback port is private.
	p := New(Config{Policy: allowPorts(portOf(public))})
	for _, path := range []string{"/to-private", "/to-meta"} {
		_, err := p.Check(context.Background(), public.URL+path)
		var ue *UserError
		if !errors.As(err, &ue) || !strings.Contains(ue.Msg, "private or local") {
			t.Errorf("%s: want blocked, got %v", path, err)
		}
	}
	if _, err := p.Check(context.Background(), public.URL+"/to-file"); err == nil || !strings.Contains(err.Error(), "not a web address") {
		t.Errorf("file redirect: %v", err)
	}
	if _, err := p.Check(context.Background(), public.URL+"/loop"); err == nil || !strings.Contains(err.Error(), "redirects too many times") || hops != 4 {
		t.Errorf("loop: %v hops=%d", err, hops)
	}
	if info, err := p.Check(context.Background(), public.URL+"/ok"); err != nil || info.Events != 1 {
		t.Errorf("good redirect: %v %+v", err, info)
	}
}

func TestSizeCapAndTimeout(t *testing.T) {
	big := tiny + strings.Repeat("X-PAD:"+strings.Repeat("a", 100)+"\r\n", 50)
	mk := func(chunked bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if chunked {
				w.(http.Flusher).Flush() // no Content-Length
			}
			fmt.Fprint(w, big)
		}))
	}
	for _, chunked := range []bool{false, true} {
		srv := mk(chunked)
		p := New(Config{MaxBytes: 1024, Policy: allowPorts(portOf(srv))})
		_, err := p.Check(context.Background(), srv.URL+"/c.ics")
		if err == nil || !strings.Contains(err.Error(), "larger than") {
			t.Errorf("chunked=%v: %v", chunked, err)
		}
		srv.Close()
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	p := New(Config{Timeout: 150 * time.Millisecond, Policy: allowPorts(portOf(slow))})
	start := time.Now()
	_, err := p.Check(context.Background(), slow.URL+"/"+token)
	if err == nil || !strings.Contains(err.Error(), "too long") || time.Since(start) > time.Second {
		t.Fatalf("%v after %s", err, time.Since(start))
	}
}

func TestHTTPErrorsNeverLeakURL(t *testing.T) {
	status := 404
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == 429 {
			w.Header().Set("Retry-After", "120")
		}
		if status == 200 {
			fmt.Fprint(w, "<html>login required</html>")
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	p := providerFor(srv.URL+"/"+token+"/cal.ics?key="+token, Config{Policy: allowPorts(portOf(srv))})
	for _, st := range []int{404, 403, 500, 200} {
		status = st
		_, err := syncOnce(p, "")
		if err == nil || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("%d: %v", st, err)
		}
	}
	status = 429
	_, err := syncOnce(p, "")
	var rl provider.ErrRateLimited
	if !errors.As(err, &rl) || rl.RetryAfter != 2*time.Minute {
		t.Fatalf("%v", err)
	}
	// connection refused, DNS failure, bad certificate: still no URL
	srv.Close()
	for _, raw := range []string{srv.URL + "/" + token, "https://no-such-host.invalid/" + token, "https://localhost:1/" + token} {
		_, err := New(Config{Policy: func(netip.AddrPort) bool { return true }}).Check(context.Background(), raw)
		if err == nil || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "invalid/") {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"webcal://example.test/a.ics":     "https://example.test/a.ics",
		"WEBCALS://example.test/a.ics#x":  "https://example.test/a.ics",
		" https://example.test/a.ics?k=v": "https://example.test/a.ics?k=v",
		"http://example.test/a.ics":       "http://example.test/a.ics",
	} {
		if got, err := Normalize(in); err != nil || got != want {
			t.Errorf("%q -> %q %v", in, got, err)
		}
	}
	if ExternalID("https://example.test/a.ics") == ExternalID("https://example.test/b.ics") || len(ExternalID("x")) != 64 {
		t.Fatal("external id")
	}
}

func TestIsPublic(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8": true, "1.1.1.1": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.0.0.1": false, "172.16.5.5": false, "192.168.0.1": false, "169.254.1.1": false,
		"0.0.0.0": false, "100.64.0.1": false, "224.0.0.1": false, "::1": false, "fe80::1": false, "fc00::1": false,
		"::ffff:127.0.0.1": false, "64:ff9b::a00:1": false, "64:ff9b::808:808": true,
	} {
		if got := IsPublic(netip.MustParseAddr(ip)); got != want {
			t.Errorf("%s: %v", ip, got)
		}
	}
}
