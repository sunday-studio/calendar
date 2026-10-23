package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"

	"calendar/server/internal/telemetry"
	"calendar/server/internal/telemetry/telemetrytest"
)

func TestOutboundCallsGetTemplatedClientSpans(t *testing.T) {
	rec := telemetrytest.Install(t)
	var gotURL, gotAuth, gotTrace string
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL, gotAuth, gotTrace = r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("traceparent")
		if n.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte("{}"))
	}))
	defer srv.Close()
	var slept []time.Duration
	c := &http.Client{Transport: &Transport{Provider: "google",
		Sleep:  func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil },
		Jitter: func() float64 { return 0 }}}

	ctx, parent := telemetry.StartSpan(context.Background(), "sync.account")
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/calendar/v3/calendars/alice@example.com/events/EVT-SECRET-123?syncToken=SYNC-SECRET&q=Dentist", nil)
	req.Header.Set("Authorization", "Bearer ya29.TOKEN-SECRET")
	resp, err := c.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", resp, err)
	}
	resp.Body.Close()
	parent.End()

	// the real request is untouched: same URL and Authorization, no trace header to the provider
	if gotURL != "/calendar/v3/calendars/alice@example.com/events/EVT-SECRET-123?syncToken=SYNC-SECRET&q=Dentist" || gotAuth != "Bearer ya29.TOKEN-SECRET" || gotTrace != "" {
		t.Fatalf("outbound request altered: %q %q %q", gotURL, gotAuth, gotTrace)
	}
	if len(slept) != 1 || n.Load() != 2 {
		t.Fatalf("retry behaviour changed: %v %d", slept, n.Load())
	}
	var clients []string
	for _, s := range rec.Spans() {
		if s.SpanKind() != trace.SpanKindClient {
			continue
		}
		clients = append(clients, s.Name())
		if s.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Errorf("client span not a child of the sync span")
		}
		a := map[string]string{}
		for _, kv := range s.Attributes() {
			a[string(kv.Key)] = kv.Value.Emit()
		}
		if a["calendar.provider"] != "google" {
			t.Errorf("provider attr: %v", a)
		}
		for k, v := range a {
			for _, bad := range []string{"alice", "EVT-SECRET", "SYNC-SECRET", "Dentist", "TOKEN-SECRET", "syncToken", "?"} {
				if strings.Contains(v, bad) {
					t.Errorf("%s=%s contains %q", k, v, bad)
				}
			}
		}
		if p := a["url.full"]; !strings.Contains(p, "/calendar/v3/calendars/:id/events/:id") {
			t.Errorf("url.full = %q", p)
		}
	}
	// one span per attempt: the 503 and the 200
	if len(clients) != 2 || clients[0] != "google GET /calendar/v3/calendars/:id/events/:id" {
		t.Fatalf("client spans %v", clients)
	}
	var retries int64
	for _, m := range rec.Metrics(t) {
		if m.Name == "calendar.provider.retries" {
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				v, _ := dp.Attributes.Value(attribute.Key("calendar.retry.reason"))
				p, _ := dp.Attributes.Value(attribute.Key("calendar.provider"))
				if v.AsString() == "5xx" && p.AsString() == "google" {
					retries += dp.Value
				}
			}
		}
	}
	if retries != 1 {
		t.Fatalf("retry counter = %d", retries)
	}
}
