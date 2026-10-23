package backup

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"calendar/server/internal/db"
	"calendar/server/internal/telemetry"
)

var (
	lastOKAt atomic.Int64 // unix seconds of the last backup this process wrote
	lastSize atomic.Int64
)

// RegisterGauges registers calendar.backup.last_success_age_seconds (the alert source: nightly backups
// should keep it under ~25 h) and calendar.backup.last_size_bytes. The age comes from the newest of this
// process's last success and meta.last_backup_at, so it survives restarts. A hub that has never backed
// up reports its age since this call, so the alert still fires once backups are overdue. Call only when
// scheduled backups are enabled.
func RegisterGauges(d *db.DB) func() {
	registered := time.Now()
	offs := []func(){
		telemetry.GaugeSet("backup.last_success_age_seconds", "Seconds since the last successful backup (since start-up if none yet)", "s",
			func(ctx context.Context) []telemetry.Observation {
				last := lastOKAt.Load()
				var v string
				if d.R.QueryRowContext(context.WithoutCancel(ctx), `SELECT value FROM meta WHERE key='last_backup_at'`).Scan(&v) == nil {
					if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > last {
						last = n
					}
				}
				if last == 0 {
					last = registered.Unix()
				}
				age := float64(time.Now().Unix() - last)
				if age < 0 {
					age = 0
				}
				return []telemetry.Observation{{Value: age}}
			}),
		telemetry.GaugeSet("backup.last_size_bytes", "Size of the most recent backup file written by this process", "By",
			func(context.Context) []telemetry.Observation {
				if n := lastSize.Load(); n > 0 {
					return []telemetry.Observation{{Value: float64(n)}}
				}
				return nil
			}),
	}
	return func() {
		for _, f := range offs {
			f()
		}
	}
}
