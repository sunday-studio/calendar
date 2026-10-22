package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func client(slept *[]time.Duration) *http.Client {
	return &http.Client{Transport: &Transport{
		Sleep:  func(_ context.Context, d time.Duration) error { *slept = append(*slept, d); return nil },
		Jitter: func() float64 { return 0 },
	}}
}

func TestRetries5xxThenOK(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	var slept []time.Duration
	resp, err := client(&slept).Get(srv.URL)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", resp, err)
	}
	// jitter 0 => half of 500ms, then half of 1s
	if len(slept) != 2 || slept[0] != 250*time.Millisecond || slept[1] != 500*time.Millisecond {
		t.Fatalf("slept %v", slept)
	}
}

func TestRetryAfterHonouredAndCapped(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/long" {
			w.Header().Set("Retry-After", "600")
			w.WriteHeader(429)
			return
		}
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	var slept []time.Duration
	c := client(&slept)
	resp, _ := c.Get(srv.URL)
	if resp.StatusCode != 200 || len(slept) != 1 || slept[0] != 7*time.Second {
		t.Fatalf("status %d slept %v", resp.StatusCode, slept)
	}
	slept = nil
	resp, _ = c.Get(srv.URL + "/long")
	if resp.StatusCode != 429 || len(slept) != 0 || resp.Header.Get("Retry-After") != "600" {
		t.Fatalf("long retry-after must surface: %d %v", resp.StatusCode, slept)
	}
}

func TestGivesUpAndNoRetryOn4xx(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if r.URL.Path == "/404" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()
	var slept []time.Duration
	c := client(&slept)
	resp, _ := c.Get(srv.URL)
	if resp.StatusCode != 500 || n.Load() != 4 {
		t.Fatalf("attempts %d status %d", n.Load(), resp.StatusCode)
	}
	n.Store(0)
	resp, _ = c.Get(srv.URL + "/404")
	if resp.StatusCode != 404 || n.Load() != 1 {
		t.Fatalf("404 retried: %d", n.Load())
	}
}

func TestParseRetryAfterDate(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d, ok := ParseRetryAfter(now.Add(90*time.Second).UTC().Format(http.TimeFormat), now)
	if !ok || d != 90*time.Second {
		t.Fatal(d, ok)
	}
}
