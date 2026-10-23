package sync

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"calendar/server/internal/accounts"
	"calendar/server/internal/telemetry"
)

// statusLabel maps the stored account status (plus "a pass is running right now") to the label used on the
// calendar.account.status gauge: active | syncing | needs_reauth | error | paused.
func statusLabel(status string, running bool) string {
	switch status {
	case accounts.StatusOK:
		if running {
			return "syncing"
		}
		return "active"
	case accounts.StatusConnecting:
		return "syncing"
	case accounts.StatusNeedsReauth:
		return "needs_reauth"
	case accounts.StatusDisabled:
		return "paused"
	}
	return "error"
}

type acctRow struct {
	id, provider, status string
	lastOK, created      int64
}

func (m *Manager) snapshot(ctx context.Context) []acctRow {
	rows, err := m.DB.R.QueryContext(context.WithoutCancel(ctx), `SELECT id, provider, status, COALESCE(last_ok_at,0), created_at FROM accounts`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []acctRow
	for rows.Next() {
		var r acctRow
		if rows.Scan(&r.id, &r.provider, &r.status, &r.lastOK, &r.created) == nil {
			out = append(out, r)
		}
	}
	return out
}

func (m *Manager) isRunning(id string) bool {
	m.mu.Lock()
	w := m.workers[id]
	m.mu.Unlock()
	return w != nil && w.running.Load()
}

// RegisterGauges registers the account gauges (bounded per-account series: a handful of accounts):
//
//	calendar.account.last_sync_age_seconds{calendar.account.id, calendar.provider}  seconds since the last successful sync
//	calendar.account.status{calendar.account.id, calendar.provider, calendar.status} 1 for the current status
//	calendar.accounts{calendar.provider, calendar.status}                           accounts per status
//	calendar.sync.backoff_seconds                                                   longest current retry delay
//
// Call once at start-up, after telemetry.Setup. The returned function unregisters them.
func (m *Manager) RegisterGauges() func() {
	var offs []func()
	offs = append(offs, telemetry.GaugeSet("account.last_sync_age_seconds", "Seconds since the account last synced successfully (since it was created if it never has)", "s",
		func(ctx context.Context) []telemetry.Observation {
			var out []telemetry.Observation
			now := m.now().Unix()
			for _, r := range m.snapshot(ctx) {
				at := r.lastOK
				if at == 0 {
					at = r.created
				}
				age := float64(now - at)
				if age < 0 {
					age = 0
				}
				out = append(out, telemetry.Observation{Value: age, Attrs: []attribute.KeyValue{telemetry.AccountID(r.id), telemetry.Provider(r.provider)}})
			}
			return out
		}))
	offs = append(offs, telemetry.GaugeSet("account.status", "1 for each account's current status (active|syncing|needs_reauth|error|paused)", "1",
		func(ctx context.Context) []telemetry.Observation {
			var out []telemetry.Observation
			for _, r := range m.snapshot(ctx) {
				out = append(out, telemetry.Observation{Value: 1, Attrs: []attribute.KeyValue{telemetry.AccountID(r.id), telemetry.Provider(r.provider),
					telemetry.Attr("status", statusLabel(r.status, m.isRunning(r.id)))}})
			}
			return out
		}))
	offs = append(offs, telemetry.GaugeSet("accounts", "Connected accounts by provider and status", "{account}",
		func(ctx context.Context) []telemetry.Observation {
			type key struct{ p, s string }
			n := map[key]float64{}
			for _, r := range m.snapshot(ctx) {
				n[key{r.provider, statusLabel(r.status, m.isRunning(r.id))}]++
			}
			var out []telemetry.Observation
			for k, v := range n {
				out = append(out, telemetry.Observation{Value: v, Attrs: []attribute.KeyValue{telemetry.Provider(k.p), telemetry.Attr("status", k.s)}})
			}
			return out
		}))
	offs = append(offs, telemetry.GaugeSet("sync.backoff_seconds", "Longest retry delay currently scheduled for a failing account (0 when none)", "s",
		func(context.Context) []telemetry.Observation {
			m.mu.Lock()
			defer m.mu.Unlock()
			var max time.Duration
			for _, d := range m.backoffs {
				if d > max {
					max = d
				}
			}
			return []telemetry.Observation{{Value: max.Seconds()}}
		}))
	return func() {
		for _, f := range offs {
			f()
		}
	}
}
