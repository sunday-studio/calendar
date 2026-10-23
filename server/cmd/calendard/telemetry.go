package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"modernc.org/sqlite"

	"calendar/server/internal/accounts"
	"calendar/server/internal/agenda"
	"calendar/server/internal/authn"
	"calendar/server/internal/automate"
	"calendar/server/internal/backup"
	"calendar/server/internal/db"
	"calendar/server/internal/events"
	"calendar/server/internal/oauth"
	"calendar/server/internal/provider"
	"calendar/server/internal/provider/google"
	"calendar/server/internal/provider/ics"
	"calendar/server/internal/push"
	"calendar/server/internal/stream"
	syncer "calendar/server/internal/sync"
	"calendar/server/internal/telemetry"
	"calendar/server/internal/vault"
)

// startTelemetry turns on OpenTelemetry when the standard OTEL_* variables ask for it and returns the
// hub's logger (stdout JSON; plus trace ids and the OTLP log bridge when enabled) and the flush function
// (5 s budget). A setup problem is logged and the hub runs without telemetry: observability never blocks serving.
func startTelemetry(ctx context.Context, w io.Writer) (*slog.Logger, func()) {
	registerErrorKinds()
	cfg := telemetry.ConfigFromEnv(version)
	shutdown, err := telemetry.Setup(ctx, cfg)
	log := telemetry.NewLogger(w)
	switch {
	case err != nil:
		log.Warn("telemetry setup failed; continuing without telemetry", "err", telemetry.Sanitize(err.Error()))
		return log, func() {}
	case cfg.Enabled():
		log.Info("telemetry enabled", "endpoint", cfg.EndpointHost(), "protocol", cfg.Protocol(), "signals", strings.Join(cfg.Signals(), ","))
	default:
		log.Info("telemetry disabled")
	}
	return log, func() {
		if err := shutdown(ctx); err != nil {
			log.Warn("telemetry flush incomplete", "err", telemetry.Sanitize(err.Error()))
		}
	}
}

// registerErrorKinds maps the repo's typed errors to calendar.error.kind (telemetry cannot import them).
func registerErrorKinds() {
	is := func(targets ...error) func(error) bool {
		return func(err error) bool {
			for _, t := range targets {
				if errors.Is(err, t) {
					return true
				}
			}
			return false
		}
	}
	telemetry.RegisterErrorKind(telemetry.KindPush, is(push.ErrUnregistered, push.ErrTransient))
	telemetry.RegisterErrorKind(telemetry.KindAuth, is(authn.ErrUnauthorized, oauth.ErrInvalidState, vault.ErrOpen))
	telemetry.RegisterErrorKind(telemetry.KindProvider, func(err error) bool {
		var rl provider.ErrRateLimited
		var ce *provider.ConflictError
		var he *google.HTTPError
		return errors.Is(err, provider.ErrNeedsReauth) || errors.Is(err, provider.ErrCursorExpired) ||
			errors.Is(err, provider.ErrPreconditionFailed) || errors.Is(err, provider.ErrForbiddenAdmin) ||
			errors.Is(err, provider.ErrNotSupported) || errors.Is(err, provider.ErrNotFound) || errors.Is(err, oauth.ErrNoIdentity) ||
			errors.As(err, &rl) || errors.As(err, &ce) || errors.As(err, &he)
	})
	telemetry.RegisterErrorKind(telemetry.KindValidation, func(err error) bool {
		var av *agenda.ValidationError
		var uv *automate.ValidationError
		var ue *ics.UserError
		var ie *accounts.ICSInvalidError
		return errors.As(err, &av) || errors.As(err, &uv) || errors.As(err, &ue) || errors.As(err, &ie) ||
			errors.Is(err, accounts.ErrInvalidColor) || errors.Is(err, accounts.ErrUnsupported) || errors.Is(err, accounts.ErrNotFound) ||
			errors.Is(err, automate.ErrNotFound) || errors.Is(err, authn.ErrNotFound)
	})
	telemetry.RegisterErrorKind(telemetry.KindValidation, func(err error) bool {
		var ee *events.Error
		if !errors.As(err, &ee) {
			return false
		}
		return ee.Kind != events.KProvider && ee.Kind != events.KNeedsReauth && ee.Kind != events.KRateLimited &&
			ee.Kind != events.KForbidden && ee.Kind != events.KUnavailable
	})
	telemetry.RegisterErrorKind(telemetry.KindProvider, func(err error) bool {
		var ee *events.Error
		return errors.As(err, &ee) && (ee.Kind == events.KProvider || ee.Kind == events.KNeedsReauth ||
			ee.Kind == events.KRateLimited || ee.Kind == events.KForbidden || ee.Kind == events.KUnavailable)
	})
	telemetry.RegisterErrorKind(telemetry.KindDB, func(err error) bool {
		var se *sqlite.Error
		return errors.As(err, &se) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, sql.ErrConnDone) || errors.Is(err, sql.ErrTxDone)
	})
	telemetry.RegisterErrorKind(telemetry.KindInternal, is(backup.ErrWrongKey, backup.ErrNotBackup, backup.ErrNewerSchema))
}

// doctorTelemetry adds the Telemetry line to `calendard doctor`.
func doctorTelemetry(ctx context.Context) (detail string, warn bool) {
	cfg := telemetry.ConfigFromEnv(version)
	detail = cfg.Describe()
	if !cfg.Enabled() {
		return detail, false
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := cfg.Probe(pctx); err != nil {
		return detail + "; endpoint not reachable from here: " + err.Error() + " (the hub keeps running; telemetry is dropped until it is)", true
	}
	return detail + "; endpoint reachable", false
}

// registerGauges adds the observable gauges sampled at each metric export: process uptime and build info,
// database revision/size/WAL/row counts, per-account sync age and status, SSE clients and (when scheduled
// backups are on) the age of the last good backup.
func registerGauges(d *db.DB, mgr *syncer.Manager, broker *stream.Broker, backups bool) {
	telemetry.RegisterProcessGauges(version)
	d.RegisterGauges()
	mgr.RegisterGauges()
	broker.RegisterGauges()
	if backups {
		backup.RegisterGauges(d)
	}
}
