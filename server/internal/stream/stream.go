// Package stream is the SSE broker behind GET /v1/stream (ENGINEERING §7.3).
//
// Publishing never blocks: every client has a bounded queue and a client whose queue
// is full is dropped (its stream ends; it reconnects with Last-Event-ID and re-syncs
// from the current rev).
package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Event is one SSE frame. ID 0 means "no id line".
type Event struct {
	ID   int64
	Name string
	Data []byte
}

// Sync states carried by the `sync` event.
const (
	SyncStarted  = "started"
	SyncFinished = "finished"
	SyncFailed   = "failed"
)

type Broker struct {
	// Rev returns the current global rev (used for Last-Event-ID catch-up).
	Rev func(ctx context.Context) (int64, error)
	// PingInterval defaults to 20s.
	PingInterval time.Duration
	// Buffer is the per-client queue length (default 32).
	Buffer int
	// WriteTimeout bounds a single frame write to a client (default 10s).
	WriteTimeout time.Duration
	Log          *slog.Logger

	mu      sync.Mutex
	subs    map[*client]struct{}
	rev     int64
	dropped atomic.Int64
}

type client struct {
	ch chan Event
}

func (b *Broker) buffer() int {
	if b.Buffer > 0 {
		return b.Buffer
	}
	return 32
}

func (b *Broker) ping() time.Duration {
	if b.PingInterval > 0 {
		return b.PingInterval
	}
	return 20 * time.Second
}

func (b *Broker) wtimeout() time.Duration {
	if b.WriteTimeout > 0 {
		return b.WriteTimeout
	}
	return 10 * time.Second
}

// Clients is the number of connected clients (wired to sync.Manager.SSEClients).
func (b *Broker) Clients() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// Dropped is the number of slow clients dropped so far.
func (b *Broker) Dropped() int64 { return b.dropped.Load() }

func (b *Broker) subscribe() *client {
	c := &client{ch: make(chan Event, b.buffer())}
	b.mu.Lock()
	if b.subs == nil {
		b.subs = map[*client]struct{}{}
	}
	b.subs[c] = struct{}{}
	b.mu.Unlock()
	return c
}

func (b *Broker) unsubscribe(c *client) {
	b.mu.Lock()
	delete(b.subs, c) // channel is closed only by the dropper, under mu
	b.mu.Unlock()
}

// fanout must be called with b.mu held.
func (b *Broker) fanout(e Event) {
	for c := range b.subs {
		select {
		case c.ch <- e:
		default:
			delete(b.subs, c)
			close(c.ch)
			b.dropped.Add(1)
			if b.Log != nil {
				b.Log.Warn("sse client dropped (slow)")
			}
		}
	}
}

func mustJSON(v any) []byte {
	d, _ := json.Marshal(v)
	return d
}

// PublishRev announces a new rev; it is a no-op unless rev is higher than the last one published.
func (b *Broker) PublishRev(rev int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if rev <= b.rev {
		return
	}
	b.rev = rev
	b.fanout(Event{ID: rev, Name: "rev", Data: mustJSON(map[string]int64{"rev": rev})})
}

// Account announces an account status change (status is the wire enum).
func (b *Broker) Account(id, status string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fanout(Event{ID: b.rev, Name: "account", Data: mustJSON(map[string]string{"id": id, "status": status})})
}

// Sync announces a sync state change for an account (started|finished|failed).
func (b *Broker) Sync(accountID, state string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fanout(Event{ID: b.rev, Name: "sync", Data: mustJSON(map[string]string{"accountId": accountID, "state": state})})
}

func (b *Broker) write(rc *http.ResponseController, w http.ResponseWriter, e Event) error {
	_ = rc.SetWriteDeadline(time.Now().Add(b.wtimeout()))
	if e.ID > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", e.ID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Name, e.Data); err != nil {
		return err
	}
	return rc.Flush()
}

// ServeHTTP serves one SSE connection until the client goes away, is dropped, or ctx ends.
// Authentication is the caller's job.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	ctx := r.Context()

	// Subscribe before reading the rev so no commit can fall between the two.
	c := b.subscribe()
	defer b.unsubscribe(c)

	cur, err := b.Rev(ctx)
	if err != nil {
		h.Del("Content-Type")
		http.Error(w, `{"error":{"code":"internal","message":"internal error"}}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil || rc.Flush() != nil {
		return
	}
	last, hasLast := int64(0), false
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			last, hasLast = n, true
		}
	}
	sent := cur // rev events at or below this are already known to the client
	if !hasLast || last != cur {
		// New or behind (or ahead of a restored DB): tell it where we are now.
		if b.write(rc, w, Event{ID: cur, Name: "rev", Data: mustJSON(map[string]int64{"rev": cur})}) != nil {
			return
		}
	}

	tk := time.NewTicker(b.ping())
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-c.ch:
			if !ok {
				return // dropped as a slow client
			}
			if e.Name == "rev" {
				if e.ID <= sent {
					continue
				}
				sent = e.ID
			}
			if b.write(rc, w, e) != nil {
				return
			}
		case <-tk.C:
			if b.write(rc, w, Event{Name: "ping", Data: []byte("{}")}) != nil {
				return
			}
		}
	}
}
