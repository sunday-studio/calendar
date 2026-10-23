// Package httpx is a RoundTripper that retries 5xx/429 with Retry-After and jittered
// exponential backoff. Sleep/Now/Jitter are injectable for tests.
package httpx

import (
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"calendar/server/internal/telemetry"
)

type Transport struct {
	Base       http.RoundTripper // default http.DefaultTransport
	MaxRetries int               // retries after the first attempt; default 3
	BaseDelay  time.Duration     // default 500ms; doubles each attempt
	MaxDelay   time.Duration     // longest wait we do in-transport; default 30s. A longer Retry-After is returned to the caller untouched.
	Now        func() time.Time
	Sleep      func(ctx context.Context, d time.Duration) error
	Jitter     func() float64 // [0,1); default rand.Float64
	// Provider labels telemetry (calendar.provider). Empty = inferred from the request host or context.
	Provider string

	telOnce sync.Once
	traced  http.RoundTripper
}

// retried counts one retry. reason is low cardinality: 429, 5xx or network.
func (t *Transport) retried(req *http.Request, reason string) {
	telemetry.Counter("provider.retries", "Outbound provider calls retried by the transport, by provider and reason", "{retry}").Add(req.Context(), 1, metric.WithAttributes(
		attribute.String("calendar.provider", telemetry.ProviderFor(req, t.Provider)), attribute.String("calendar.retry.reason", reason)))
}

func (t *Transport) defaults() (base http.RoundTripper, max int, bd, md time.Duration) {
	base, max, bd, md = t.Base, t.MaxRetries, t.BaseDelay, t.MaxDelay
	if base == nil {
		base = http.DefaultTransport
	}
	if max == 0 {
		max = 3
	}
	if bd == 0 {
		bd = 500 * time.Millisecond
	}
	if md == 0 {
		md = 30 * time.Second
	}
	return
}

func sleep(ctx context.Context, d time.Duration) error {
	tm := time.NewTimer(d)
	defer tm.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tm.C:
		return nil
	}
}

// ParseRetryAfter reads delta-seconds or an HTTP date.
func ParseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return time.Duration(n) * time.Second, true
	}
	if tm, err := http.ParseTime(v); err == nil {
		if d := tm.Sub(now); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == 500 || status == 502 || status == 503 || status == 504
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base, maxRetries, baseDelay, maxDelay := t.defaults()
	// one client span per attempt, templated URL, no ids/query/tokens; a no-op until telemetry is enabled
	t.telOnce.Do(func() { t.traced = telemetry.WrapClient(base, t.Provider) })
	base = t.traced
	now, slp, jit := t.Now, t.Sleep, t.Jitter
	if now == nil {
		now = time.Now
	}
	if slp == nil {
		slp = sleep
	}
	if jit == nil {
		jit = rand.Float64
	}
	replayable := req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
	for attempt := 0; ; attempt++ {
		r := req
		if attempt > 0 && req.GetBody != nil {
			r = req.Clone(req.Context())
			b, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			r.Body = b
		}
		resp, err := base.RoundTrip(r)
		var wait time.Duration
		switch {
		case err != nil:
			if !replayable || req.Context().Err() != nil || attempt >= maxRetries {
				return nil, err
			}
			t.retried(req, "network")
			wait = backoff(attempt, baseDelay, maxDelay, jit())
		case retryable(resp.StatusCode):
			if !replayable || attempt >= maxRetries {
				return resp, nil
			}
			if resp.StatusCode == http.StatusTooManyRequests {
				t.retried(req, "429")
			} else {
				t.retried(req, "5xx")
			}
			if ra, ok := ParseRetryAfter(resp.Header.Get("Retry-After"), now()); ok {
				if ra > maxDelay {
					return resp, nil // let the sync engine schedule it
				}
				wait = ra
			} else {
				wait = backoff(attempt, baseDelay, maxDelay, jit())
			}
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
		default:
			return resp, nil
		}
		if err := slp(req.Context(), wait); err != nil {
			return nil, err
		}
	}
}

// backoff = exp/2 + jitter*exp/2 where exp = base*2^attempt capped at max.
func backoff(attempt int, base, max time.Duration, j float64) time.Duration {
	exp := base
	for i := 0; i < attempt && exp < max; i++ {
		exp *= 2
	}
	if exp > max {
		exp = max
	}
	return exp/2 + time.Duration(j*float64(exp/2))
}
