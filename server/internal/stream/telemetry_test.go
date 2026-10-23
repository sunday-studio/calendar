package stream

import (
	"sync/atomic"
	"testing"

	"calendar/server/internal/telemetry/telemetrytest"
)

type A = telemetrytest.Attrs

func TestTelemetrySSEConnectionsDropsAndGauge(t *testing.T) {
	rec := telemetrytest.Install(t)
	var rev atomic.Int64
	b := newBroker(&rev)
	off := b.RegisterGauges()
	defer off()
	srv := newServer(t, b)
	c1 := dial(t, srv.URL, "")
	c1.expect(t, "rev", `{"rev":0}`)
	waitClients(t, b, 1)
	if g := rec.Gauge(t, "calendar.sse.clients", nil); len(g) != 1 || g[0] != 1 {
		t.Fatalf("sse.clients gauge %v", g)
	}
	b.PublishRev(1)
	b.Account("acc", "active")
	c1.expect(t, "rev", `{"rev":1}`)
	if rec.Sum(t, "calendar.sse.events_published", A{"calendar.type": "rev"}) != 1 || rec.Sum(t, "calendar.sse.events_published", A{"calendar.type": "account"}) != 1 {
		t.Fatal("published events not counted by type")
	}
	if rec.Sum(t, "calendar.sse.connections", A{"calendar.event": "connect"}) != 1 {
		t.Fatal("connect not counted")
	}
	c1.cancel()
	waitClients(t, b, 0)
	for i := 0; i < 100 && rec.Sum(t, "calendar.sse.connections", A{"calendar.event": "disconnect"}) == 0; i++ {
		b.PublishRev(int64(10 + i))
	}
	// a stalled subscriber is dropped and counted
	b.Buffer = 1
	slow := b.subscribe()
	_ = slow
	for i := int64(100); i < 105; i++ {
		b.PublishRev(i)
	}
	if rec.Sum(t, "calendar.sse.drops", nil) != 1 {
		t.Fatal("drop not counted")
	}
}
