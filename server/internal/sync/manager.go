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
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"

	"calendar/server/internal/accounts"
	"calendar/server/internal/db"
	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
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

	mu      gosync.Mutex
	workers map[string]*worker
	locks   map[string]*gosync.Mutex
	ctx     context.Context
	wg      gosync.WaitGroup
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
		go func() { defer m.wg.Done(); ext.RunDaily(ctx, m.DB, 24*time.Hour) }()
	}
	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.runPruner(ctx, 24*time.Hour) }()
	t := time.NewTicker(dur(m.Reconcile, 5*time.Second))
	defer t.Stop()
	for {
		if err := m.reconcile(ctx); err != nil && ctx.Err() == nil {
			m.log().Error("sync reconcile", "err", err)
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
		m.runWorker(ctx, w)
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
	for {
		var trig Trigger
		select {
		case <-ctx.Done():
			return
		case trig = <-w.trig:
		case <-tk.C:
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
		retry, err := m.syncAccount(ctx, w.id, trig)
		w.running.Store(false)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if errors.Is(err, provider.ErrNeedsReauth) {
				return
			}
			due = m.now().Add(retry)
		} else {
			due = m.now().Add(m.cadenceFor(a.Provider))
		}
	}
}

// SyncAccount runs one full pass for an account synchronously (used by workers and tests).
func (m *Manager) SyncAccount(ctx context.Context, id string, trig Trigger) error {
	_, err := m.syncAccount(ctx, id, trig)
	return err
}

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
		m.log().Error("sync_runs insert", "err", werr)
	}
}

func (m *Manager) syncAccount(ctx context.Context, id string, trig Trigger) (retry time.Duration, err error) {
	acct, err := m.Accounts.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	if !eligible(acct.Status) {
		return 0, nil
	}
	prov := m.Providers[acct.Provider]
	if prov == nil {
		return m.fail(ctx, acct, fmt.Errorf("no provider for %q", acct.Provider))
	}
	start := m.now()
	var total stats
	m.emit(id, "started")
	defer func() {
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
		return 0, err
	}
	m.setNext(ctx, id, m.now().Add(m.cadenceFor(acct.Provider)))
	return 0, nil
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
		if n, err := m.PruneRuns(ctx); err != nil && ctx.Err() == nil {
			m.log().Error("prune sync_runs", "err", err)
		} else if n > 0 {
			m.log().Info("pruned sync_runs", "rows", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

func (m *Manager) setNext(ctx context.Context, id string, t time.Time) {
	_ = m.DB.Write(context.WithoutCancel(ctx), func(tx *db.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE accounts SET next_sync_at=? WHERE id=?`, t.Unix(), id)
		return err
	})
}

// fail applies the §5.4 state machine for a failed pass and returns the retry delay.
func (m *Manager) fail(ctx context.Context, a *domain.Account, cause error) (time.Duration, error) {
	wctx := context.WithoutCancel(ctx)
	detail := shorten(cause.Error())
	if errors.Is(cause, provider.ErrNeedsReauth) {
		m.log().Warn("account needs reauth", "account", a.ID)
		if err := m.Accounts.MarkNeedsReauth(wctx, a.ID, "Google sign-in expired or was revoked"); err != nil {
			m.log().Error("mark needs_reauth", "err", err)
		}
		return 0, cause
	}
	var fails int
	_ = m.DB.R.QueryRowContext(wctx, `SELECT fail_count FROM accounts WHERE id=?`, a.ID).Scan(&fails)
	fails++
	var rl provider.ErrRateLimited
	retry := m.backoff(fails)
	if errors.As(cause, &rl) && rl.RetryAfter > retry {
		retry = rl.RetryAfter
	}
	var err error
	if a.Status == accounts.StatusConnecting || fails >= errorThreshold {
		err = m.Accounts.MarkError(wctx, a.ID, detail) // increments fail_count, status=error
	} else {
		err = m.DB.Write(wctx, func(tx *db.Tx) error {
			_, e := tx.ExecContext(wctx, `UPDATE accounts SET fail_count=fail_count+1, last_sync_at=?, status_detail=? WHERE id=?`, m.now().Unix(), detail, a.ID)
			return e
		})
	}
	if err != nil {
		m.log().Error("record sync failure", "err", err)
	}
	m.setNext(wctx, a.ID, m.now().Add(retry))
	m.log().Warn("sync failed", "account", a.ID, "fails", fails, "retry_in", retry, "err", cause)
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

func (m *Manager) cursor(ctx context.Context, calID string) (string, error) {
	var c sql.NullString
	err := m.DB.R.QueryRowContext(ctx, `SELECT sync_cursor FROM calendars WHERE id=?`, calID).Scan(&c)
	return c.String, err
}

// syncCalendar pulls pages until done. The cursor is committed with the last page's data.
func (m *Manager) syncCalendar(ctx context.Context, prov provider.Provider, acct domain.Account, cal domain.Calendar) (total stats, err error) {
	cursor, err := m.cursor(ctx, cal.ID)
	if err != nil {
		return total, err
	}
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
			return total, serr
		}
		lock.Unlock()
		var rl provider.ErrRateLimited
		switch {
		case errors.Is(serr, provider.ErrCursorExpired) && cursor != "" && resyncs < maxFullResyncs:
			resyncs++
			if _, e := m.DB.W.ExecContext(ctx, `UPDATE calendars SET sync_cursor=NULL WHERE id=?`, cal.ID); e != nil {
				return total, e
			}
			cursor, page, seen = "", "", map[string]struct{}{}
		case errors.As(serr, &rl) && rateTries < maxRateRetries:
			rateTries++
			wait := rl.RetryAfter
			if wait <= 0 {
				wait = 30 * time.Second
			}
			if e := m.sleep(ctx, wait); e != nil {
				return total, e
			}
		default:
			return total, serr
		}
	}
}
