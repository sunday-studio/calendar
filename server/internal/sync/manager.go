// Package sync supervises one worker per account and runs the provider-agnostic sync loop
// of ENGINEERING §5.3.
package sync

import (
	"calendar/server/internal/recur"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"calendar/server/internal/accounts"
	"calendar/server/internal/db"
	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
	"calendar/server/internal/provider/ics"
	"calendar/server/internal/telemetry"
)

type Trigger int

const (
	Tick Trigger = iota
	Manual
	AfterWrite
	Reauthed
)

func (t Trigger) String() string {
	return [...]string{"tick", "manual", "after_write", "reauthed"}[t]
}

const (
	errorThreshold = 3 // consecutive failures before ok -> error
	maxRateRetries = 3
	maxFullResyncs = 2 // 410 recoveries per calendar per pass
)

type Manager struct {
	DB        *db.DB
	Accounts  *accounts.Service
	Providers map[string]provider.Provider // by accounts.provider
	Mat       Materialiser
	Log       *slog.Logger

	Fast, Slow time.Duration // cadence with / without SSE clients (defaults 60s / 5m)
	SSEClients func() int    // nil = 0
	// OnSync, if set, is told when a pass starts and how it ended (stream.Sync* states).
	OnSync    func(accountID, state string)
	Poll      time.Duration // worker wake granularity (default 2s)
	Reconcile time.Duration // account-set refresh (default 5s)
	Now       func() time.Time
	Sleep     func(ctx context.Context, d time.Duration) error // rate-limit waits
	Backoff   func(fails int) time.Duration
	// Interval overrides the poll cadence per provider (accounts.provider), e.g. "ics" -> 15m.
	Interval map[string]time.Duration

	mu       gosync.Mutex
	backoffs map[string]time.Duration // current retry delay per failing account (calendar.sync.backoff_seconds)
	workers  map[string]*worker
	locks    map[string]*gosync.Mutex
	ctx      context.Context
	wg       gosync.WaitGroup
}

type worker struct {
	id      string
	trig    chan Trigger
	cancel  context.CancelFunc
	running atomic.Bool
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) log() *slog.Logger {
	if m.Log != nil {
		return m.Log
	}
	return slog.Default()
}

func dur(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func (m *Manager) cadence() time.Duration {
	if m.SSEClients != nil && m.SSEClients() > 0 {
		return dur(m.Fast, time.Minute)
	}
	return dur(m.Slow, 5*time.Minute)
}

// cadenceFor is the poll cadence for an account of the given provider.
func (m *Manager) cadenceFor(prov string) time.Duration {
	if d := m.Interval[prov]; d > 0 {
		return d
	}
	return m.cadence()
}

func (m *Manager) mat() Materialiser {
	if m.Mat != nil {
		return m.Mat
	}
	return &recur.Materialiser{Now: m.Now, Log: m.Log}
}

func (m *Manager) sleep(ctx context.Context, d time.Duration) error {
	if m.Sleep != nil {
		return m.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// backoff: 1, 2, 4 ... capped at 15 min, +-20% jitter.
func (m *Manager) backoff(fails int) time.Duration {
	if m.Backoff != nil {
		return m.Backoff(fails)
	}
	d := time.Minute
	for i := 1; i < fails && d < 15*time.Minute; i++ {
		d *= 2
	}
	if d > 15*time.Minute {
		d = 15 * time.Minute
	}
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}

// Lock returns the per-account mutex. The write service holds it across provider call +
// mirror upsert; sync holds it around each page fetch + apply.
func (m *Manager) Lock(accountID string) *gosync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks == nil {
		m.locks = map[string]*gosync.Mutex{}
	}
	l := m.locks[accountID]
	if l == nil {
		l = &gosync.Mutex{}
		m.locks[accountID] = l
	}
	return l
}

func eligible(status string) bool {
	return status == accounts.StatusOK || status == accounts.StatusError || status == accounts.StatusConnecting
}

// Run supervises workers until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	m.mu.Lock()
	m.ctx = ctx
	if m.workers == nil {
		m.workers = map[string]*worker{}
	}
	m.mu.Unlock()
	if ext, ok := m.mat().(interface {
		RunDaily(context.Context, *db.DB, time.Duration)
	}); ok {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			telemetry.Supervise(ctx, "recur.extend_window", func(ctx context.Context) { ext.RunDaily(ctx, m.DB, 24*time.Hour) })
		}()
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		telemetry.Supervise(ctx, "sync.pruner", func(ctx context.Context) { m.runPruner(ctx, 24*time.Hour) })
	}()
	t := time.NewTicker(dur(m.Reconcile, 5*time.Second))
	defer t.Stop()
	for {
		if telemetry.Safe(ctx, "sync.reconcile", func() {
			if err := m.reconcile(ctx); err != nil && ctx.Err() == nil {
				telemetry.RecordError(ctx, "sync", err, telemetry.Operation("reconcile"))
			}
		}) {
			continue
		}
		select {
		case <-ctx.Done():
			m.wg.Wait()
			return nil
		case <-t.C:
		}
	}
}

func (m *Manager) reconcile(ctx context.Context) error {
	list, err := m.Accounts.List(ctx)
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for _, a := range list {
		live[a.ID] = true
		if !eligible(a.Status) {
			continue
		}
		w := m.ensureWorker(a.ID)
		if a.Status == accounts.StatusConnecting && w != nil && !w.running.Load() {
			m.send(w, Reauthed)
		}
	}
	m.mu.Lock()
	for id, w := range m.workers {
		if !live[id] {
			w.cancel()
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) ensureWorker(id string) *worker {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil || m.ctx.Err() != nil {
		return nil
	}
	if w := m.workers[id]; w != nil {
		return w
	}
	ctx, cancel := context.WithCancel(m.ctx)
	w := &worker{id: id, trig: make(chan Trigger, 1), cancel: cancel}
	m.workers[id] = w
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() {
			m.mu.Lock()
			if m.workers[id] == w {
				delete(m.workers, id)
			}
			m.mu.Unlock()
			cancel()
		}()
		telemetry.Safe(ctx, "sync.worker", func() { m.runWorker(ctx, w) }) // reconcile restarts a worker that died
	}()
	return w
}

func (m *Manager) send(w *worker, t Trigger) bool {
	select {
	case w.trig <- t:
	default: // one already pending: coalesced (singleflight)
	}
	return true
}

// Trigger nudges an account's worker (e.g. AfterWrite). False if it has no live worker.
func (m *Manager) Trigger(accountID string, t Trigger) bool {
	m.mu.Lock()
	w := m.workers[accountID]
	m.mu.Unlock()
	if w == nil {
		return false
	}
	return m.send(w, t)
}

// TriggerManual queues an immediate sync for one account (or all when id == "") and returns
// how many accounts were queued. Concurrent calls coalesce per account.
func (m *Manager) TriggerManual(ctx context.Context, accountID string) (int, error) {
	var list []domain.Account
	if accountID != "" {
		a, err := m.Accounts.Get(ctx, accountID)
		if err != nil {
			return 0, err
		}
		list = []domain.Account{*a}
	} else {
		var err error
		if list, err = m.Accounts.List(ctx); err != nil {
			return 0, err
		}
	}
	n := 0
	for _, a := range list {
		if !eligible(a.Status) {
			continue
		}
		if w := m.ensureWorker(a.ID); w != nil && m.send(w, Manual) {
			n++
		}
	}
	return n, nil
}

func (m *Manager) runWorker(ctx context.Context, w *worker) {
	due := m.now()
	var next sql.NullInt64
	_ = m.DB.R.QueryRowContext(ctx, `SELECT next_sync_at FROM accounts WHERE id=?`, w.id).Scan(&next)
	if next.Valid && time.Unix(next.Int64, 0).After(due) {
		due = time.Unix(next.Int64, 0)
	}
	tk := time.NewTicker(dur(m.Poll, 2*time.Second))
	defer tk.Stop()
	// After a good pass the next one is due a cadence after it ended. The cadence depends on whether a client is
	// connected, and phones reconnect often, so it is re-read on every wake instead of fixed when the pass ends.
	var okAt time.Time
	var okProv string
	for {
		var trig Trigger
		select {
		case <-ctx.Done():
			return
		case trig = <-w.trig:
		case <-tk.C:
			if !okAt.IsZero() {
				due = okAt.Add(m.cadenceFor(okProv))
			}
			if m.now().Before(due) {
				continue
			}
			trig = Tick
		}
		a, err := m.Accounts.Get(ctx, w.id)
		if err != nil || !eligible(a.Status) {
			return // deleted, paused or needs sign-in; reconcile restarts us when eligible again
		}
		w.running.Store(true)
		var retry time.Duration
		if telemetry.Safe(ctx, "sync.pass", func() { retry, err = m.syncAccount(ctx, w.id, trig) }) {
			// a panic inside one pass must not end the worker: back off and try again
			retry, err = m.backoff(1), errPassPanicked
		}
		w.running.Store(false)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if errors.Is(err, provider.ErrNeedsReauth) {
				return
			}
			okAt = time.Time{}
			due = m.now().Add(retry)
		} else {
			okAt, okProv = m.now(), a.Provider
			due = okAt.Add(m.cadenceFor(okProv))
		}
	}
}

// SyncAccount runs one full pass for an account synchronously (used by workers and tests).
func (m *Manager) SyncAccount(ctx context.Context, id string, trig Trigger) error {
	_, err := m.syncAccount(ctx, id, trig)
	return err
}

var errPassPanicked = errors.New("sync pass panicked")

func errKind(err error) string {
	var rl provider.ErrRateLimited
	switch {
	case err == nil:
		return ""
	case errors.Is(err, provider.ErrNeedsReauth):
		return "needs_reauth"
	case errors.As(err, &rl):
		return "rate_limited"
	case errors.Is(err, provider.ErrForbiddenAdmin):
		return "forbidden_admin"
	case errors.Is(err, provider.ErrCursorExpired):
		return "cursor_expired"
	}
	return "error"
}

func shorten(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func (m *Manager) logRun(ctx context.Context, accountID, calID string, start time.Time, trig Trigger, st stats, err error) {
	var cal, ek, es any
	if calID != "" {
		cal = calID
	}
	if err != nil {
		ek, es = errKind(err), shorten(err.Error())
	}
	werr := m.DB.Write(context.WithoutCancel(ctx), func(tx *db.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO sync_runs(account_id,calendar_id,started_at,duration_ms,trigger,added,changed,removed,error_kind,error) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			accountID, cal, start.Unix(), m.now().Sub(start).Milliseconds(), trig.String(), st.added, st.changed, st.removed, ek, es)
		return e
	})
	if werr != nil {
		telemetry.RecordError(ctx, "sync", werr, telemetry.Operation("log_run"))
	}
}

func runOutcome(err error) string {
	var rl provider.ErrRateLimited
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, provider.ErrNeedsReauth):
		return "needs_reauth"
	case errors.As(err, &rl):
		return "rate_limited"
	}
	return "error"
}

func (m *Manager) syncAccount(ctx context.Context, id string, trig Trigger) (retry time.Duration, err error) {
	acct, err := m.Accounts.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	prov := m.Providers[acct.Provider]
	pname := telemetry.Provider(acct.Provider)
	if !eligible(acct.Status) {
		telemetry.Inc(ctx, "sync.runs", pname, telemetry.SyncTrigger(trig.String()), telemetry.Outcome("skipped"))
		return 0, nil
	}
	ctx, span := telemetry.StartSpan(ctx, "sync.run", telemetry.AccountID(id), pname, telemetry.SyncTrigger(trig.String()))
	defer span.End()
	wall := time.Now()
	if prov == nil {
		err = fmt.Errorf("no provider for %q", acct.Provider)
		telemetry.RecordError(ctx, "sync", err, telemetry.Operation("provider_lookup"))
		telemetry.Inc(ctx, "sync.runs", pname, telemetry.SyncTrigger(trig.String()), telemetry.Outcome("error"))
		return m.fail(ctx, acct, err)
	}
	start := m.now()
	var total stats
	m.emit(id, "started")
	defer func() {
		outcome := runOutcome(err)
		span.SetAttributes(attribute.String("calendar.sync.outcome", outcome),
			attribute.Int("calendar.sync.added", total.added), attribute.Int("calendar.sync.changed", total.changed), attribute.Int("calendar.sync.removed", total.removed))
		telemetry.Inc(ctx, "sync.runs", pname, telemetry.SyncTrigger(trig.String()), telemetry.Outcome(outcome))
		telemetry.Record(ctx, "sync.duration", time.Since(wall).Seconds(), pname)
		for c, n := range map[string]int{"added": total.added, "changed": total.changed, "removed": total.removed} {
			if n > 0 {
				telemetry.Add(ctx, "sync.events", int64(n), pname, telemetry.Attr("change", c))
			}
		}
		if err != nil {
			telemetry.Log().WarnContext(ctx, "sync run failed", "account", id, "provider", acct.Provider, "trigger", trig.String(), "outcome", outcome)
		} else {
			telemetry.Log().DebugContext(ctx, "sync run ok", "account", id, "provider", acct.Provider, "trigger", trig.String(),
				"added", total.added, "changed", total.changed, "removed", total.removed)
		}
		m.logRun(ctx, id, "", start, trig, total, err)
		if err != nil {
			m.emit(id, "failed")
		} else {
			m.emit(id, "finished")
		}
	}()

	cals, err := m.discover(ctx, prov, *acct)
	if err != nil {
		return m.fail(ctx, acct, err)
	}
	var firstErr error
	for _, cal := range cals {
		if !cal.Visible {
			continue
		}
		if staticFeed(*acct, cal) && trig == Tick && !m.staticDue(ctx, cal.ID) {
			continue
		}
		cs := m.now()
		st, cerr := m.syncCalendar(ctx, prov, *acct, cal)
		total.add(st)
		m.logRun(ctx, id, cal.ID, cs, trig, st, cerr)
		if cerr != nil {
			if firstErr == nil {
				firstErr = cerr
			}
			if errors.Is(cerr, provider.ErrNeedsReauth) || ctx.Err() != nil {
				break
			}
		}
	}
	if firstErr != nil {
		return m.fail(ctx, acct, firstErr)
	}
	if err := m.Accounts.MarkOK(ctx, id); err != nil {
		telemetry.RecordError(ctx, "sync", err, telemetry.Operation("mark_ok"))
		return 0, err
	}
	m.clearBackoff(id)
	m.setNext(ctx, id, m.now().Add(m.cadenceFor(acct.Provider)))
	return 0, nil
}

func (m *Manager) setBackoff(id string, d time.Duration) {
	m.mu.Lock()
	if m.backoffs == nil {
		m.backoffs = map[string]time.Duration{}
	}
	m.backoffs[id] = d
	m.mu.Unlock()
}

func (m *Manager) clearBackoff(id string) {
	m.mu.Lock()
	delete(m.backoffs, id)
	m.mu.Unlock()
}

func (m *Manager) emit(accountID, state string) {
	if m.OnSync != nil {
		m.OnSync(accountID, state)
	}
}

// RetainRuns is how long sync_runs rows are kept.
const RetainRuns = 30 * 24 * time.Hour

// PruneRuns deletes sync_runs older than RetainRuns and returns the number removed.
func (m *Manager) PruneRuns(ctx context.Context) (int64, error) {
	var n int64
	err := m.DB.Write(ctx, func(tx *db.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM sync_runs WHERE started_at < ?`, m.now().Add(-RetainRuns).Unix())
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}

func (m *Manager) runPruner(ctx context.Context, every time.Duration) {
	for {
		pctx, span := telemetry.StartSpan(ctx, "sync.prune_runs")
		if n, err := m.PruneRuns(pctx); err != nil && ctx.Err() == nil {
			telemetry.RecordError(pctx, "sync", err, telemetry.Operation("prune_runs"))
		} else if n > 0 {
			telemetry.Add(pctx, "sync_runs.pruned", n)
			span.SetAttributes(attribute.Int64("calendar.rows", n))
			telemetry.Log().InfoContext(pctx, "pruned sync_runs", "rows", n)
		}
		span.End()
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

func (m *Manager) setNext(ctx context.Context, id string, t time.Time) {
	err := m.DB.Write(context.WithoutCancel(ctx), func(tx *db.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE accounts SET next_sync_at=? WHERE id=?`, t.Unix(), id)
		return err
	})
	if err != nil {
		telemetry.RecordError(ctx, "sync", err, telemetry.Operation("set_next"))
	}
}

// fail applies the §5.4 state machine for a failed pass and returns the retry delay.
func (m *Manager) fail(ctx context.Context, a *domain.Account, cause error) (time.Duration, error) {
	wctx := context.WithoutCancel(ctx)
	detail := shorten(cause.Error())
	span := trace.SpanFromContext(ctx)
	if errors.Is(cause, provider.ErrNeedsReauth) {
		telemetry.RecordErrorKind(ctx, "sync", telemetry.KindReauth, cause, telemetry.Operation("sync"))
		span.AddEvent("account.needs_reauth", trace.WithAttributes(telemetry.AccountID(a.ID), telemetry.Provider(a.Provider)))
		telemetry.Log().WarnContext(ctx, "account needs reauth", "account", a.ID, "provider", a.Provider)
		if err := m.Accounts.MarkNeedsReauth(wctx, a.ID, "Google sign-in expired or was revoked"); err != nil {
			telemetry.RecordError(ctx, "sync", err, telemetry.Operation("mark_needs_reauth"))
		}
		m.clearBackoff(a.ID)
		return 0, cause
	}
	var fails int
	if err := m.DB.R.QueryRowContext(wctx, `SELECT fail_count FROM accounts WHERE id=?`, a.ID).Scan(&fails); err != nil && !errors.Is(err, sql.ErrNoRows) {
		telemetry.RecordError(ctx, "sync", err, telemetry.Operation("read_fail_count"))
	}
	fails++
	var rl provider.ErrRateLimited
	retry := m.backoff(fails)
	if errors.As(cause, &rl) && rl.RetryAfter > retry {
		retry = rl.RetryAfter
	}
	if errors.As(cause, &rl) {
		// an expected condition: counted as a run outcome, not recorded as an error
		span.AddEvent("sync.rate_limited", trace.WithAttributes(attribute.Int64("calendar.retry_after_s", int64(rl.RetryAfter.Seconds()))))
	} else if ctx.Err() == nil {
		var ue *ics.UserError // a feed that stopped answering is a provider problem, not a validation one
		if errors.As(cause, &ue) {
			telemetry.RecordErrorKind(ctx, "sync", telemetry.KindProvider, cause, telemetry.Operation("sync"), attribute.Int("calendar.sync.fails", fails))
		} else {
			telemetry.RecordError(ctx, "sync", cause, telemetry.Operation("sync"), attribute.Int("calendar.sync.fails", fails))
		}
	}
	var err error
	if a.Status == accounts.StatusConnecting || fails >= errorThreshold {
		err = m.Accounts.MarkError(wctx, a.ID, detail) // increments fail_count, status=error
		if err == nil {
			span.AddEvent("account.error", trace.WithAttributes(telemetry.AccountID(a.ID), attribute.Int("calendar.sync.fails", fails)))
		}
	} else {
		err = m.DB.Write(wctx, func(tx *db.Tx) error {
			_, e := tx.ExecContext(wctx, `UPDATE accounts SET fail_count=fail_count+1, last_sync_at=?, status_detail=? WHERE id=?`, m.now().Unix(), detail, a.ID)
			return e
		})
	}
	if err != nil {
		telemetry.RecordError(ctx, "sync", err, telemetry.Operation("record_failure"))
	}
	m.setNext(wctx, a.ID, m.now().Add(retry))
	m.setBackoff(a.ID, retry)
	telemetry.Inc(ctx, "sync.backoff", telemetry.Provider(a.Provider))
	telemetry.Log().WarnContext(ctx, "sync failed", "account", a.ID, "provider", a.Provider, "fails", fails, "retry_in", retry.String(), "err", telemetry.Sanitize(cause.Error()))
	return retry, cause
}

// discover lists remote calendars and upserts them, preserving visible and color_override.
func (m *Manager) discover(ctx context.Context, prov provider.Provider, acct domain.Account) ([]domain.Calendar, error) {
	remote, err := prov.Discover(ctx, acct)
	if err != nil {
		return nil, err
	}
	var out []domain.Calendar
	err = m.DB.Write(ctx, func(tx *db.Tx) error {
		out = out[:0]
		keep := map[string]bool{}
		for _, rc := range remote {
			var id string
			var name, color, tz, access sql.NullString
			var primary, visible int
			e := tx.QueryRowContext(ctx, `SELECT id, name, remote_color, tzid, access, is_primary, visible FROM calendars WHERE account_id=? AND remote_id=?`, acct.ID, rc.RemoteID).
				Scan(&id, &name, &color, &tz, &access, &primary, &visible)
			switch {
			case errors.Is(e, sql.ErrNoRows):
				id = ulid.Make().String()
				visible = b2i(rc.Visible)
				if _, e := tx.ExecContext(ctx, `INSERT INTO calendars(id,account_id,remote_id,name,remote_color,tzid,access,is_primary,visible) VALUES (?,?,?,?,?,?,?,?,?)`,
					id, acct.ID, rc.RemoteID, rc.Name, nstr(rc.Color), nstr(rc.TZID), rc.Access, b2i(rc.IsPrimary), visible); e != nil {
					return e
				}
				if e := tx.Change(ctx, "calendar", id, "upsert"); e != nil {
					return e
				}
			case e != nil:
				return e
			default:
				if name.String != rc.Name || color.String != rc.Color || tz.String != rc.TZID || access.String != rc.Access || (primary == 1) != rc.IsPrimary {
					if _, e := tx.ExecContext(ctx, `UPDATE calendars SET name=?, remote_color=?, tzid=?, access=?, is_primary=? WHERE id=?`,
						rc.Name, nstr(rc.Color), nstr(rc.TZID), rc.Access, b2i(rc.IsPrimary), id); e != nil {
						return e
					}
					if e := tx.Change(ctx, "calendar", id, "upsert"); e != nil {
						return e
					}
				}
			}
			keep[id] = true
			out = append(out, domain.Calendar{ID: id, AccountID: acct.ID, RemoteID: rc.RemoteID, Name: rc.Name, RemoteColor: rc.Color,
				TZID: rc.TZID, Access: rc.Access, IsPrimary: rc.IsPrimary, Visible: visible == 1})
		}
		if len(remote) == 0 {
			return nil // never wipe the mirror on an empty listing
		}
		rows, e := tx.QueryContext(ctx, `SELECT id FROM calendars WHERE account_id=?`, acct.ID)
		if e != nil {
			return e
		}
		var gone []string
		for rows.Next() {
			var id string
			if e := rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			if !keep[id] {
				gone = append(gone, id)
			}
		}
		rows.Close()
		for _, id := range gone {
			if _, e := tx.ExecContext(ctx, `DELETE FROM calendars WHERE id=?`, id); e != nil {
				return e
			}
			if e := tx.Change(ctx, "calendar", id, "delete"); e != nil {
				return e
			}
		}
		return nil
	})
	return out, err
}

// staticRefresh is how often a static feed is re-read in full.
const staticRefresh = 6 * time.Hour

// staticFeed reports whether a calendar is one of Google's public holiday calendars. Google answers 410 fullSyncRequired
// to the sync token of every one of them, on every request, so incremental sync cannot work. They change a few times a
// year, so they are read in full every staticRefresh instead, which also keeps them out of the "syncing" state on every poll.
func staticFeed(acct domain.Account, cal domain.Calendar) bool {
	return acct.Provider == "google" && strings.HasSuffix(cal.RemoteID, "#holiday@group.v.calendar.google.com")
}

// staticDue is true when a static feed has never synced or was last read more than staticRefresh ago.
func (m *Manager) staticDue(ctx context.Context, calID string) bool {
	var last sql.NullInt64
	if err := m.DB.R.QueryRowContext(ctx, `SELECT last_sync_at FROM calendars WHERE id=?`, calID).Scan(&last); err != nil || !last.Valid {
		return true
	}
	return m.now().Sub(time.Unix(last.Int64, 0)) >= staticRefresh
}

func (m *Manager) cursor(ctx context.Context, calID string) (string, error) {
	var c sql.NullString
	err := m.DB.R.QueryRowContext(ctx, `SELECT sync_cursor FROM calendars WHERE id=?`, calID).Scan(&c)
	return c.String, err
}

// syncCalendar pulls pages until done. The cursor is committed with the last page's data.
func (m *Manager) syncCalendar(ctx context.Context, prov provider.Provider, acct domain.Account, cal domain.Calendar) (total stats, err error) {
	ctx, span := telemetry.StartSpan(ctx, "sync.calendar", telemetry.AccountID(acct.ID), telemetry.Provider(acct.Provider))
	pname := telemetry.Provider(acct.Provider)
	pages := 0
	defer func() {
		span.SetAttributes(attribute.Int("calendar.sync.pages", pages))
		if err != nil {
			span.SetStatus(codes.Error, telemetry.Kind(err))
		}
		span.End()
	}()
	cursor, err := m.cursor(ctx, cal.ID)
	if err != nil {
		telemetry.RecordError(ctx, "sync", err, telemetry.Operation("cursor"))
		return total, err
	}
	if staticFeed(acct, cal) {
		cursor = "" // see staticFeed: Google rejects the sync token of these calendars, so never send one
	}
	span.SetAttributes(attribute.Bool("calendar.sync.full", cursor == ""))
	var seen map[string]struct{}
	if cursor == "" {
		seen = map[string]struct{}{}
	}
	page := ""
	resyncs, rateTries := 0, 0
	lock := m.Lock(acct.ID)
	for {
		lock.Lock()
		batch, serr := prov.Sync(ctx, acct, cal, provider.SyncRequest{Cursor: cursor, PageToken: page})
		if serr == nil {
			pages++
			telemetry.Inc(ctx, "sync.pages", pname)
			if batch.Replace && seen == nil {
				seen = map[string]struct{}{}
			}
			final := batch.PageToken == ""
			var st stats
			serr = m.DB.Write(ctx, func(tx *db.Tx) error {
				var e error
				st, e = applyBatch(ctx, tx, m.mat(), cal, batch.Events, batch.Deleted, seen, final, batch.NextCursor, m.now())
				return e
			})
			if serr == nil {
				total.add(st)
				lock.Unlock()
				if final {
					return total, nil
				}
				page = batch.PageToken
				continue
			}
			lock.Unlock()
			telemetry.RecordError(ctx, "sync", serr, telemetry.Operation("apply"))
			return total, serr
		}
		lock.Unlock()
		var rl provider.ErrRateLimited
		switch {
		case errors.Is(serr, provider.ErrCursorExpired) && cursor != "" && resyncs < maxFullResyncs:
			resyncs++
			telemetry.Inc(ctx, "sync.cursor_resets", pname, telemetry.Attr("reason", "410"))
			span.AddEvent("sync.cursor_reset", trace.WithAttributes(attribute.String("calendar.reason", "410")))
			telemetry.Log().InfoContext(ctx, "sync cursor expired; full resync", "account", acct.ID, "calendar", cal.ID, "provider", acct.Provider, "why", serr.Error())
			if _, e := m.DB.W.ExecContext(ctx, `UPDATE calendars SET sync_cursor=NULL WHERE id=?`, cal.ID); e != nil {
				telemetry.RecordError(ctx, "sync", e, telemetry.Operation("reset_cursor"))
				return total, e
			}
			cursor, page, seen = "", "", map[string]struct{}{}
		case errors.As(serr, &rl) && rateTries < maxRateRetries:
			rateTries++
			wait := rl.RetryAfter
			if wait <= 0 {
				wait = 30 * time.Second
			}
			span.AddEvent("sync.rate_limit_wait", trace.WithAttributes(attribute.Int64("calendar.wait_s", int64(wait.Seconds()))))
			if e := m.sleep(ctx, wait); e != nil {
				return total, e
			}
		default:
			// needs_reauth and rate limits are recorded by fail(); everything else is a provider failure
			return total, serr
		}
	}
}
