package stream

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type frame struct{ id, event, data string }

// sseConn reads frames from an open SSE response.
type sseConn struct {
	resp   *http.Response
	frames chan frame
	cancel context.CancelFunc
}

func dial(t *testing.T, url, lastID string) *sseConn {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	c := &sseConn{resp: resp, frames: make(chan frame, 256), cancel: cancel}
	go func() {
		defer close(c.frames)
		sc := bufio.NewScanner(resp.Body)
		var f frame
		for sc.Scan() {
			l := sc.Text()
			switch {
			case l == "":
				if f.event != "" {
					c.frames <- f
				}
				f = frame{}
			case strings.HasPrefix(l, "id: "):
				f.id = l[4:]
			case strings.HasPrefix(l, "event: "):
				f.event = l[7:]
			case strings.HasPrefix(l, "data: "):
				f.data = l[6:]
			}
		}
	}()
	t.Cleanup(c.close)
	return c
}

func (c *sseConn) close() { c.cancel(); c.resp.Body.Close() }

func (c *sseConn) next(t *testing.T) frame {
	t.Helper()
	select {
	case f, ok := <-c.frames:
		if !ok {
			t.Fatal("stream closed")
		}
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for frame")
	}
	return frame{}
}

func (c *sseConn) expect(t *testing.T, event, data string) frame {
	t.Helper()
	f := c.next(t)
	if f.event != event || f.data != data {
		t.Fatalf("got %+v, want %s %s", f, event, data)
	}
	return f
}

func newBroker(rev *atomic.Int64) *Broker {
	return &Broker{Rev: func(context.Context) (int64, error) { return rev.Load(), nil }, PingInterval: time.Hour}
}

// newServer closes after the clients (t.Cleanup is LIFO and dial registers later).
func newServer(t *testing.T, b *Broker) *httptest.Server {
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	return srv
}

func waitClients(t *testing.T, b *Broker, n int) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if b.Clients() == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("clients = %d, want %d", b.Clients(), n)
}

func TestHeadersAndInitialRev(t *testing.T) {
	var rev atomic.Int64
	rev.Store(7)
	b := newBroker(&rev)
	srv := newServer(t, b)
	c := dial(t, srv.URL, "")
	if ct := c.resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatal(ct)
	}
	if c.resp.Header.Get("Cache-Control") != "no-cache" || c.resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatal(c.resp.Header)
	}
	f := c.expect(t, "rev", `{"rev":7}`)
	if f.id != "7" {
		t.Fatal(f.id)
	}
	waitClients(t, b, 1)
}

func TestFanOutAndRevDedup(t *testing.T) {
	var rev atomic.Int64
	b := newBroker(&rev)
	srv := newServer(t, b)
	c1, c2 := dial(t, srv.URL, ""), dial(t, srv.URL, "")
	c1.expect(t, "rev", `{"rev":0}`)
	c2.expect(t, "rev", `{"rev":0}`)
	waitClients(t, b, 2)

	b.PublishRev(1)
	b.PublishRev(1) // duplicate: no event
	b.PublishRev(0) // stale: no event
	b.Account("acc1", "needs_reauth")
	b.Sync("acc1", SyncStarted)
	b.PublishRev(2)
	for _, c := range []*sseConn{c1, c2} {
		if f := c.expect(t, "rev", `{"rev":1}`); f.id != "1" {
			t.Fatal(f.id)
		}
		c.expect(t, "account", `{"id":"acc1","status":"needs_reauth"}`)
		c.expect(t, "sync", `{"accountId":"acc1","state":"started"}`)
		c.expect(t, "rev", `{"rev":2}`)
	}
}

func TestLastEventIDCatchUp(t *testing.T) {
	var rev atomic.Int64
	rev.Store(10)
	b := newBroker(&rev)
	srv := newServer(t, b)

	behind := dial(t, srv.URL, "4")
	behind.expect(t, "rev", `{"rev":10}`) // immediately told the current rev

	current := dial(t, srv.URL, "10")
	waitClients(t, b, 2)
	b.PublishRev(11)
	// up-to-date client gets no initial rev: first frame is the live one
	current.expect(t, "rev", `{"rev":11}`)
	behind.expect(t, "rev", `{"rev":11}`)

	garbage := dial(t, srv.URL, "abc")
	garbage.expect(t, "rev", `{"rev":10}`)
}

func TestSlowClientDropped(t *testing.T) {
	var rev atomic.Int64
	b := newBroker(&rev)
	b.Buffer = 2
	// Subscribe directly and never read, simulating a stalled connection.
	slow := b.subscribe()
	b.Buffer = 64 // the healthy client must never be starved by scheduling jitter (race detector)
	fast := b.subscribe()
	go func() {
		for range fast.ch {
		}
	}()
	for i := int64(1); i <= 10; i++ {
		b.PublishRev(i) // must never block
	}
	if b.Dropped() != 1 {
		t.Fatalf("dropped = %d", b.Dropped())
	}
	if b.Clients() != 1 {
		t.Fatalf("clients = %d", b.Clients())
	}
	n := 0
	for range slow.ch { // channel was closed by the dropper
		n++
	}
	if n != 2 {
		t.Fatalf("slow client buffered %d", n)
	}
	b.unsubscribe(slow) // double removal is harmless
	b.unsubscribe(fast)
}

func TestPing(t *testing.T) {
	var rev atomic.Int64
	b := newBroker(&rev)
	b.PingInterval = 20 * time.Millisecond
	srv := newServer(t, b)
	c := dial(t, srv.URL, "")
	c.expect(t, "rev", `{"rev":0}`)
	f := c.expect(t, "ping", `{}`)
	if f.id != "" {
		t.Fatal("ping must not carry an id")
	}
}

func TestNoGoroutineLeak(t *testing.T) {
	var rev atomic.Int64
	b := newBroker(&rev)
	srv := httptest.NewServer(b)
	http.DefaultClient.CloseIdleConnections()
	base := runtime.NumGoroutine()
	var conns []*sseConn
	for i := 0; i < 20; i++ {
		c := dial(t, srv.URL, "")
		c.expect(t, "rev", `{"rev":0}`)
		conns = append(conns, c)
	}
	waitClients(t, b, 20)
	for _, c := range conns {
		c.close()
	}
	waitClients(t, b, 0)
	srv.Close()
	http.DefaultClient.CloseIdleConnections()
	for i := 0; i < 200; i++ {
		if runtime.NumGoroutine() <= base+1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines: base %d, now %d", base, runtime.NumGoroutine())
}
