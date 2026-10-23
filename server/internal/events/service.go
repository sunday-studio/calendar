package events

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"log/slog"
	"strings"
	gosync "sync"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"calendar/server/internal/accounts"
	"calendar/server/internal/agenda"
	"calendar/server/internal/db"
	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
	syncer "calendar/server/internal/sync"
	"calendar/server/internal/telemetry"
	"calendar/server/internal/tz"
)

// Accounts is what the service needs from the accounts service.
type Accounts interface {
	Get(ctx context.Context, id string) (*domain.Account, error)
	MarkNeedsReauth(ctx context.Context, id, detail string) error
}

// Syncer is the sync manager: the shared per-account lock, the after-write nudge and the
// materialiser used for the read model.
type Syncer interface {
	Lock(accountID string) *gosync.Mutex
	Nudge(accountID string)
	Materialiser() syncer.Materialiser
}

type Service struct {
	DB        *db.DB
	Accounts  Accounts
	Providers map[string]provider.Provider
	Sync      Syncer
	Store     *agenda.Store
	Log       *slog.Logger
	Now       func() time.Time
}

// Opts carries per-request details that are not part of the payload.
type Opts struct {
	// Seed makes a create idempotent at the provider: the same seed always yields the same
	// remote id, so a retry after an ambiguous failure adopts the event instead of duplicating it.
	Seed string
	// IfMatch is the event `version` the client last saw; nil skips the hub-side check.
	IfMatch *int64
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// internal records an unexpected failure and returns the opaque 5xx-style error. A failure to mirror a
// change the provider already accepted is the dangerous case (consistency); everything else is a db or
// internal error.
func (s *Service) internal(ctx context.Context, what string, err error) *Error {
	if strings.HasPrefix(what, "mirror") || what == "apply latest" {
		consistency(ctx, "mirror_failed", err)
	} else {
		telemetry.RecordError(ctx, "events", err, telemetry.Operation(opName(ctx)), attribute.String("calendar.step", wordOf(what)))
	}
	return &Error{Kind: KProvider, Msg: "internal error"}
}

func wordOf(s string) string { return strings.ReplaceAll(strings.ToLower(s), " ", "_") }

// remoteID derives a Google-safe client id (base32hex, lowercase) from a seed.
func remoteID(seed string) string {
	if seed == "" {
		return ""
	}
	h := sha256.Sum256([]byte(seed))
	return base32.HexEncoding.WithPadding(base32.NoPadding).EncodeToString(h[:16])[:26]
}

// ---- session: lock + account + provider ----

type sess struct {
	acct   domain.Account
	prov   provider.Provider
	unlock func()
}

func (s *Service) begin(ctx context.Context, accountID string) (*sess, *Error) {
	_, err := s.Accounts.Get(ctx, accountID)
	if err != nil {
		if errors.Is(err, accounts.ErrNotFound) {
			return nil, fail(KNotFound, "", "no such account")
		}
		return nil, s.internal(ctx, "account", err)
	}
	l := s.Sync.Lock(accountID)
	l.Lock()
	// re-read under the lock: status may have changed while we waited
	a, err := s.Accounts.Get(ctx, accountID)
	if err != nil {
		l.Unlock()
		return nil, s.internal(ctx, "account", err)
	}
	switch a.Status {
	case accounts.StatusNeedsReauth:
		l.Unlock()
		return nil, fail(KNeedsReauth, "", "this account needs to sign in again")
	case accounts.StatusDisabled:
		l.Unlock()
		return nil, fail(KForbidden, "", "this account is paused")
	}
	p := s.Providers[a.Provider]
	if p == nil {
		l.Unlock()
		return nil, fail(KUnavailable, "", "no provider for this account")
	}
	opFrom(ctx).setProvider(a.Provider)
	return &sess{acct: *a, prov: p, unlock: l.Unlock}, nil
}

// ---- targets ----

type target struct {
	row    row
	master *row // series master when row is an exception or a master
	cal    domain.Calendar
	occKey *int64 // occurrence addressed on a master (no exception row yet)
	ref    provider.Ref
	shown  string // id to show/refetch after the write
	hasKey bool
}

func (s *Service) nearestKey(ctx context.Context, eid string) (int64, bool) {
	var k int64
	err := s.DB.R.QueryRowContext(ctx, `SELECT rkey FROM occurrences WHERE event_id=? AND end_utc >= ? ORDER BY start_utc LIMIT 1`, eid, s.now().Unix()).Scan(&k)
	if err == nil {
		return k, true
	}
	err = s.DB.R.QueryRowContext(ctx, `SELECT rkey FROM occurrences WHERE event_id=? ORDER BY start_utc DESC LIMIT 1`, eid).Scan(&k)
	return k, err == nil
}

func (s *Service) occurrenceExists(ctx context.Context, eid string, rk int64) bool {
	var n int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM occurrences WHERE event_id=? AND rkey=?`, eid, rk).Scan(&n)
	return n > 0
}

// load resolves an API id to the rows a write needs. Call under the account lock.
func (s *Service) load(ctx context.Context, id string, scope provider.Scope, needExceptions bool) (*target, *Error) {
	eid, rk, hasKey := parseID(id)
	r, err := loadRow(ctx, s.DB.R, eid)
	if errors.Is(err, errGone) || err == nil && r.Deleted {
		return nil, fail(KNotFound, "", "no such event")
	}
	if err != nil {
		return nil, s.internal(ctx, "load event", err)
	}
	cal, err := loadCalendar(ctx, s.DB.R, r.CalendarID)
	if err != nil {
		return nil, s.internal(ctx, "load calendar", err)
	}
	opFrom(ctx).setAccess(cal.Access)
	t := &target{row: r, cal: cal, hasKey: hasKey && rk != 0}
	t.ref = provider.Ref{Cal: cal, Event: r.Event}
	switch r.Kind {
	case domain.Single:
		t.shown = r.ID
	case domain.Master:
		t.master = &r
		var k int64
		var ok bool
		switch {
		case hasKey && rk != 0:
			if !s.occurrenceExists(ctx, eid, rk) {
				return nil, fail(KNotFound, "", "no such occurrence")
			}
			k, ok = rk, true
		case scope == provider.ScopeAll:
			k, ok = keyOf(r.When), true
		default:
			if k, ok = s.nearestKey(ctx, eid); !ok {
				k, ok = keyOf(r.When), true
			}
		}
		if ok {
			t.occKey = &k
			t.ref.OccKey = &k
			t.shown = occID(r.ID, k)
		}
	case domain.Exception:
		if r.SeriesID != nil {
			m, err := loadRow(ctx, s.DB.R, *r.SeriesID)
			if err == nil && !m.Deleted {
				t.master = &m
				t.ref.Master = &m.Event
			}
		}
		k := int64(0)
		if r.RecurrenceKey != nil {
			k = *r.RecurrenceKey
		}
		t.shown = occID(r.ID, k)
	default:
		return nil, fail(KValidation, "", "this kind of event cannot be edited")
	}
	if needExceptions && t.master != nil {
		ex, err := loadExceptions(ctx, s.DB.R, t.master.ID)
		if err != nil {
			return nil, s.internal(ctx, "load exceptions", err)
		}
		t.ref.Exceptions = ex
	}
	return t, nil
}

func (s *Service) current(ctx context.Context, id string) *agenda.Event {
	ev, err := s.Store.Event(ctx, id)
	if err != nil {
		return nil
	}
	return &ev
}

func (s *Service) checkVersion(ctx context.Context, t *target, o Opts) *Error {
	if o.IfMatch == nil || *o.IfMatch == t.row.Rev {
		return nil
	}
	e := fail(KPrecondition, "", "this event changed since you last loaded it")
	e.Current = s.current(ctx, t.shown)
	return e
}

func scopeOf(sc provider.Scope) (provider.Scope, *Error) {
	switch sc {
	case "":
		return provider.ScopeThis, nil
	case provider.ScopeThis, provider.ScopeFollowing, provider.ScopeAll:
		return sc, nil
	}
	return "", fail(KValidation, "scope", "scope must be this, following or all")
}

// ---- mirror application ----

// applyTx mirrors what the provider confirmed inside tx (same code path as a sync page).
func applyTx(ctx context.Context, s *Service, tx *db.Tx, cal domain.Calendar, res provider.Result) error {
	var events []domain.Event
	deleted := append([]string(nil), res.Deleted...)
	for _, e := range res.Events {
		if e.Status == domain.Cancelled && e.Kind == "" { // a bare "gone" stub
			deleted = append(deleted, e.RemoteID)
			continue
		}
		events = append(events, e)
	}
	if len(events) == 0 && len(deleted) == 0 {
		return nil
	}
	return syncer.Apply(ctx, tx, s.Sync.Materialiser(), cal, events, deleted, s.now())
}

// apply writes the provider's confirmed state into the mirror and the optional extra step in one tx.
func (s *Service) apply(ctx context.Context, cal domain.Calendar, res provider.Result, extra func(tx *db.Tx) error) error {
	return s.DB.Write(ctx, func(tx *db.Tx) error {
		tx.Quiet = true
		if err := applyTx(ctx, s, tx, cal, res); err != nil {
			return err
		}
		if extra != nil {
			return extra(tx)
		}
		return nil
	})
}

func localID(ctx context.Context, q querier, calID, remoteID string) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM events WHERE calendar_id=? AND remote_id=?`, calID, remoteID).Scan(&id)
	return id, err
}

func setReminder(ctx context.Context, tx *db.Tx, eid string, rkey int64, minutes *int) error {
	if minutes == nil {
		_, err := tx.ExecContext(ctx, `DELETE FROM reminder_overrides WHERE event_id=? AND rkey=?`, eid, rkey)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO reminder_overrides(event_id,rkey,minutes_before) VALUES (?,?,?)
		ON CONFLICT(event_id,rkey) DO UPDATE SET minutes_before=excluded.minutes_before`, eid, rkey, *minutes)
	return err
}

// touch bumps an event's version without changing the provider copy (hub-only data changed).
func touch(ctx context.Context, tx *db.Tx, eid string) error {
	if err := tx.Change(ctx, "event", eid, "upsert"); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE events SET rev=? WHERE id=?`, tx.Rev(), eid)
	return err
}

// afterConflict mirrors the provider's latest state and builds the 409.
func (s *Service) afterConflict(ctx context.Context, ss *sess, t *target, ce *provider.ConflictError) *Error {
	if err := s.apply(ctx, t.cal, ce.Latest, nil); err != nil {
		return s.internal(ctx, "apply latest", err)
	}
	s.Sync.Nudge(ss.acct.ID)
	if cur := s.current(ctx, t.shown); cur != nil {
		e := fail(KConflict, "", "this event changed elsewhere")
		e.Current = cur
		return e
	}
	// the occurrence ids may have moved with the series: fall back to the event itself
	if cur := s.current(ctx, t.row.ID); cur != nil {
		e := fail(KConflict, "", "this event changed elsewhere")
		e.Current = cur
		return e
	}
	return fail(KNotFound, "", "this event was deleted elsewhere")
}

// providerFailure turns a provider error into a service error, mirroring state where needed.
func (s *Service) providerFailure(ctx context.Context, ss *sess, t *target, err error) *Error {
	var ce *provider.ConflictError
	recordProviderWriteError(ctx, err)
	switch {
	case errors.As(err, &ce):
		return s.afterConflict(ctx, ss, t, ce)
	case errors.Is(err, provider.ErrNotFound) && t.row.RemoteID != "":
		if aerr := s.apply(ctx, t.cal, provider.Result{Deleted: []string{t.row.RemoteID}}, nil); aerr != nil {
			telemetry.RecordError(ctx, "events", aerr, telemetry.Operation(opName(ctx)), attribute.String("calendar.step", "mirror_gone"))
		}
		return fail(KNotFound, "", "this event no longer exists")
	case errors.Is(err, provider.ErrNeedsReauth):
		if merr := s.Accounts.MarkNeedsReauth(ctx, ss.acct.ID, "calendar provider rejected the credentials"); merr != nil {
			telemetry.RecordError(ctx, "events", merr, telemetry.Operation(opName(ctx)), attribute.String("calendar.step", "mark_needs_reauth"))
		}
	}
	return providerErr(err)
}

// respond returns the first candidate id that resolves.
func (s *Service) respond(ctx context.Context, ids ...string) (agenda.Event, error) {
	var last error = agenda.ErrNotFound
	for _, id := range ids {
		if id == "" {
			continue
		}
		ev, err := s.Store.Event(ctx, id)
		if err == nil {
			return ev, nil
		}
		last = err
		if !errors.Is(err, agenda.ErrNotFound) {
			break
		}
	}
	return agenda.Event{}, last
}

func (s *Service) respondErr(ctx context.Context, ids ...string) (agenda.Event, error) {
	ev, err := s.respond(ctx, ids...)
	if err != nil {
		return ev, s.internal(ctx, "read back", err)
	}
	return ev, nil
}

func (s *Service) userTZ(ctx context.Context, cal domain.Calendar) string {
	if cal.TZID != "" {
		if _, w := tz.Load(cal.TZID); w == "" {
			return cal.TZID
		}
	}
	if st, err := s.Store.Settings(ctx); err == nil && st.UserTZID != nil {
		return *st.UserTZID
	}
	return "UTC"
}

// ---- create ----

func (s *Service) Create(ctx context.Context, in CreateIn, o Opts) (ev agenda.Event, err error) {
	ctx, op := startOp(ctx, "create", "")
	defer func() { op.done(ctx, err) }()

	if e := checkText("title", in.Title, maxTitle); e != nil {
		return agenda.Event{}, e
	}
	if e := checkText("description", in.Description, maxText); e != nil {
		return agenda.Event{}, e
	}
	if e := checkText("location", in.Location, maxText); e != nil {
		return agenda.Event{}, e
	}
	if e := checkReminder(in.ReminderMin); e != nil {
		return agenda.Event{}, e
	}
	if e := checkConference(in.Conference); e != nil {
		return agenda.Event{}, e
	}
	if in.CalendarID == "" {
		return agenda.Event{}, fail(KValidation, "calendarId", "calendarId is required")
	}
	cal, err := loadCalendar(ctx, s.DB.R, in.CalendarID)
	if errors.Is(err, errGone) {
		return agenda.Event{}, fail(KValidation, "calendarId", "no such calendar")
	}
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "load calendar", err)
	}
	if !writable(cal) {
		return agenda.Event{}, fail(KForbidden, "calendarId", "this calendar is read-only")
	}
	when, werr := resolveWhen(in.When, nil, s.userTZ(ctx, cal), time.Hour)
	if werr != nil {
		return agenda.Event{}, werr
	}
	guests, gerr := cleanAttendees(in.Attendees)
	if gerr != nil {
		return agenda.Event{}, gerr
	}
	pin := provider.EventInput{ID: remoteID(o.Seed), Title: in.Title, Description: in.Description, Location: in.Location,
		When: when, Attendees: guests, Conference: in.Conference == "meet"}
	if in.Recurrence != nil {
		rule, rerr := cleanRule(*in.Recurrence)
		if rerr != nil {
			return agenda.Event{}, rerr
		}
		pin.Recurrence = &domain.Recurrence{RRule: rule}
	}
	ss, serr := s.begin(ctx, cal.AccountID)
	if serr != nil {
		return agenda.Event{}, serr
	}
	defer ss.unlock()
	res, err := ss.prov.Create(ctx, ss.acct, cal, pin)
	if err != nil {
		return agenda.Event{}, s.providerFailure(ctx, ss, &target{cal: cal}, err)
	}
	if len(res.Events) == 0 {
		return agenda.Event{}, s.internal(ctx, "create", errors.New("provider returned no event"))
	}
	var newID string
	err = s.apply(ctx, cal, res, func(tx *db.Tx) error {
		id, err := localID(ctx, tx, cal.ID, res.Events[0].RemoteID)
		if err != nil {
			return err
		}
		newID = id
		if in.ReminderMin != nil {
			if err := setReminder(ctx, tx, id, 0, in.ReminderMin); err != nil {
				return err
			}
			return touch(ctx, tx, id)
		}
		return nil
	})
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "mirror create", err)
	}
	s.Sync.Nudge(ss.acct.ID)
	return s.respondErr(ctx, newID)
}

// ---- patch ----

// baseWhen is the current time of the occurrence being edited (defaults for partial `when`).
func baseWhen(t *target) domain.When {
	if t.row.Kind == domain.Master && t.occKey != nil {
		m := t.row.When
		if m.AllDay {
			s := time.Unix(*t.occKey, 0).UTC()
			days := int(dayNum(m.EndDate) - dayNum(m.StartDate))
			e := s.AddDate(0, 0, days)
			return domain.When{AllDay: true, StartDate: toDate(s), EndDate: toDate(e)}
		}
		st := time.Unix(*t.occKey, 0).UTC()
		return domain.When{Start: st, End: st.Add(m.End.Sub(m.Start)), TZID: m.TZID}
	}
	return t.row.When
}

// originalKey is the original start of the occurrence a write is addressed at.
func originalKey(t *target) int64 {
	switch {
	case t.row.Kind == domain.Exception && t.row.RecurrenceKey != nil:
		return *t.row.RecurrenceKey
	case t.occKey != nil:
		return *t.occKey
	}
	return keyOf(t.row.When)
}

// shiftSeries moves a series so that its occurrence with original start `key` takes `nw`.
func shiftSeries(m domain.When, key int64, nw domain.When) domain.When {
	if m.AllDay {
		orig := dayNum(toDate(time.Unix(key, 0).UTC()))
		delta := int(dayNum(nw.StartDate) - orig)
		sd := time.Date(m.StartDate.Y, time.Month(m.StartDate.M), m.StartDate.D+delta, 0, 0, 0, 0, time.UTC)
		days := int(dayNum(nw.EndDate) - dayNum(nw.StartDate))
		return domain.When{AllDay: true, StartDate: toDate(sd), EndDate: toDate(sd.AddDate(0, 0, days))}
	}
	oldLoc, _ := tz.Load(m.TZID)
	newLoc, _ := tz.Load(nw.TZID)
	wall := func(t time.Time, l *time.Location) time.Time {
		x := t.In(l)
		return time.Date(x.Year(), x.Month(), x.Day(), x.Hour(), x.Minute(), x.Second(), 0, time.UTC)
	}
	delta := wall(nw.Start, newLoc).Sub(wall(time.Unix(key, 0), oldLoc))
	target := wall(m.Start, oldLoc).Add(delta)
	st := time.Date(target.Year(), target.Month(), target.Day(), target.Hour(), target.Minute(), target.Second(), 0, newLoc)
	return domain.When{Start: st.UTC(), End: st.Add(nw.End.Sub(nw.Start)).UTC(), TZID: nw.TZID}
}

func (s *Service) buildPatch(ctx context.Context, in PatchIn, t *target, scope provider.Scope) (provider.Patch, *Error) {
	var p provider.Patch
	if in.Title != nil {
		if e := checkText("title", *in.Title, maxTitle); e != nil {
			return p, e
		}
		p.Title = in.Title
	}
	for _, f := range []struct {
		name string
		opt  provider.Opt[string]
		dst  *provider.Opt[string]
	}{{"description", in.Description, &p.Description}, {"location", in.Location, &p.Location}} {
		if f.opt.Set && f.opt.Val != nil {
			if e := checkText(f.name, *f.opt.Val, maxText); e != nil {
				return p, e
			}
		}
		*f.dst = f.opt
	}
	if in.Attendees.Set {
		p.Attendees.Set = true
		if in.Attendees.Val != nil {
			list, e := cleanAttendees(*in.Attendees.Val)
			if e != nil {
				return p, e
			}
			p.Attendees.Val = &list
		}
	}
	if in.Recurrence.Set {
		if scope == provider.ScopeThis && t.row.Kind != domain.Single {
			return p, fail(KValidation, "recurrence", "the repeat rule cannot change for a single occurrence; use scope following or all")
		}
		p.Recurrence.Set = true
		if in.Recurrence.Val != nil {
			rule, e := cleanRule(*in.Recurrence.Val)
			if e != nil {
				return p, e
			}
			p.Recurrence.Val = &domain.Recurrence{RRule: rule}
		}
	}
	if in.Conference.Set {
		if in.Conference.Val != nil {
			if e := checkConference(*in.Conference.Val); e != nil {
				return p, e
			}
		}
		p.Conference.Set = true
		if in.Conference.Val != nil && *in.Conference.Val == "meet" {
			p.Conference.Val = in.Conference.Val
		}
	}
	if e := checkReminder(optInt(in.ReminderMin)); e != nil {
		return p, e
	}
	if in.When != nil {
		base := baseWhen(t)
		w, e := resolveWhen(*in.When, &base, s.userTZ(ctx, t.cal), base.End.Sub(base.Start))
		if e != nil {
			return p, e
		}
		if scope == provider.ScopeAll && t.master != nil {
			w = shiftSeries(t.master.When, originalKey(t), w)
		}
		p.When = &w
	}
	return p, nil
}

func optInt(o provider.Opt[int]) *int {
	if o.Set {
		return o.Val
	}
	return nil
}

func (s *Service) Patch(ctx context.Context, id string, scope provider.Scope, in PatchIn, o Opts) (ev agenda.Event, err error) {
	ctx, op := startOp(ctx, "patch", scope)
	defer func() { op.done(ctx, err) }()

	scope, serr := scopeOf(scope)
	if serr != nil {
		return agenda.Event{}, serr
	}
	if in.Empty() {
		return agenda.Event{}, fail(KValidation, "", "nothing to change")
	}
	if in.ReminderMin.Set && in.Title == nil && in.When == nil && !in.Location.Set && !in.Description.Set &&
		!in.Attendees.Set && !in.Recurrence.Set && !in.Conference.Set {
		return s.reminderOnly(ctx, id, in.ReminderMin.Val) // hub-only: no provider, no lock
	}
	eid, _, _ := parseID(id)
	acctID, err := accountOfEvent(ctx, s.DB.R, eid)
	if errors.Is(err, errGone) {
		return agenda.Event{}, fail(KNotFound, "", "no such event")
	}
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "account of event", err)
	}
	ss, e := s.begin(ctx, acctID)
	if e != nil {
		return agenda.Event{}, e
	}
	defer ss.unlock()
	t, e := s.load(ctx, id, scope, scope != provider.ScopeThis)
	if e != nil {
		return agenda.Event{}, e
	}
	if e := s.checkVersion(ctx, t, o); e != nil {
		return agenda.Event{}, e
	}
	if !t.row.Caps.CanEdit || t.master != nil && scope != provider.ScopeThis && !t.master.Caps.CanEdit {
		return agenda.Event{}, fail(KForbidden, "", "you cannot edit this event")
	}
	if scope != provider.ScopeThis && t.row.Kind == domain.Exception && t.master == nil {
		return agenda.Event{}, fail(KValidation, "scope", "the repeating series is not available; edit only this event")
	}
	p, e := s.buildPatch(ctx, in, t, scope)
	if e != nil {
		return agenda.Event{}, e
	}
	t.ref.Acct = ss.acct
	hasProviderChange := p.Title != nil || p.When != nil || p.Description.Set || p.Location.Set || p.Attendees.Set || p.Recurrence.Set || p.Conference.Set
	var res provider.Result
	if hasProviderChange {
		var err error
		if res, err = ss.prov.Update(ctx, t.ref, p, scope); err != nil {
			return agenda.Event{}, s.providerFailure(ctx, ss, t, err)
		}
	}
	err = s.apply(ctx, t.cal, res, func(tx *db.Tx) error {
		if !in.ReminderMin.Set {
			return nil
		}
		reid, rkey, err := s.reminderTarget(ctx, tx, t, scope, res)
		if err != nil {
			return err
		}
		if err := setReminder(ctx, tx, reid, rkey, in.ReminderMin.Val); err != nil {
			return err
		}
		return touch(ctx, tx, reid)
	})
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "mirror patch", err)
	}
	s.Sync.Nudge(ss.acct.ID)
	return s.respondErr(ctx, s.shownAfter(ctx, t, scope, res)...)
}

// reminderTarget picks the (event, rkey) a reminder override attaches to after a write: the
// exception row for one occurrence, the master (rkey 0 = every occurrence) for a series.
func (s *Service) reminderTarget(ctx context.Context, q querier, t *target, scope provider.Scope, res provider.Result) (string, int64, error) {
	if t.row.Kind == domain.Single {
		return t.row.ID, 0, nil
	}
	switch scope {
	case provider.ScopeThis:
		for _, e := range res.Events {
			if e.Kind == domain.Exception && e.RecurrenceKey != nil {
				id, err := localID(ctx, q, t.cal.ID, e.RemoteID)
				return id, *e.RecurrenceKey, err
			}
		}
		if t.row.Kind == domain.Exception && t.row.RecurrenceKey != nil {
			return t.row.ID, *t.row.RecurrenceKey, nil
		}
	case provider.ScopeFollowing:
		for _, e := range res.Events {
			if e.Kind == domain.Master && (t.master == nil || e.RemoteID != t.master.RemoteID) {
				id, err := localID(ctx, q, t.cal.ID, e.RemoteID)
				return id, 0, err
			}
		}
	}
	if t.master != nil {
		return t.master.ID, 0, nil
	}
	return t.row.ID, 0, nil
}

// shownAfter lists the ids to try, best first, for the response to a successful write.
func (s *Service) shownAfter(ctx context.Context, t *target, scope provider.Scope, res provider.Result) []string {
	var ids []string
	byRemote := func(remote string) string {
		id, _ := localID(ctx, s.DB.R, t.cal.ID, remote)
		return id
	}
	switch scope {
	case provider.ScopeThis:
		for _, e := range res.Events {
			if e.Kind == domain.Exception && e.RecurrenceKey != nil {
				if id := byRemote(e.RemoteID); id != "" {
					ids = append(ids, occID(id, *e.RecurrenceKey))
				}
			}
		}
	case provider.ScopeFollowing:
		for _, e := range res.Events {
			if e.Kind == domain.Master && (t.master == nil || e.RemoteID != t.master.RemoteID) {
				if id := byRemote(e.RemoteID); id != "" {
					ids = append(ids, occID(id, keyOf(e.When)), id)
				}
			}
		}
	}
	ids = append(ids, t.shown)
	if t.master != nil {
		ids = append(ids, t.master.ID)
	}
	return append(ids, t.row.ID)
}

// ---- delete ----

func (s *Service) Delete(ctx context.Context, id string, scope provider.Scope, o Opts) (err error) {
	ctx, op := startOp(ctx, "delete", scope)
	defer func() { op.done(ctx, err) }()

	scope, serr := scopeOf(scope)
	if serr != nil {
		return serr
	}
	eid, _, _ := parseID(id)
	acctID, err := accountOfEvent(ctx, s.DB.R, eid)
	if errors.Is(err, errGone) {
		return fail(KNotFound, "", "no such event")
	}
	if err != nil {
		return s.internal(ctx, "account of event", err)
	}
	ss, e := s.begin(ctx, acctID)
	if e != nil {
		return e
	}
	defer ss.unlock()
	t, e := s.load(ctx, id, scope, true)
	if e != nil {
		return e
	}
	if e := s.checkVersion(ctx, t, o); e != nil {
		return e
	}
	if !t.row.Caps.CanEdit {
		return fail(KForbidden, "", "you cannot delete this event")
	}
	if scope != provider.ScopeThis && t.row.Kind == domain.Exception && t.master == nil {
		return fail(KValidation, "scope", "the repeating series is not available; delete only this event")
	}
	t.ref.Acct = ss.acct
	res, err := ss.prov.Delete(ctx, t.ref, scope)
	if err != nil {
		return s.providerFailure(ctx, ss, t, err)
	}
	if err := s.apply(ctx, t.cal, res, nil); err != nil {
		return s.internal(ctx, "mirror delete", err)
	}
	s.Sync.Nudge(ss.acct.ID)
	return nil
}

// ---- RSVP ----

func (s *Service) RSVP(ctx context.Context, id string, resp domain.Response, note string, scope provider.Scope, o Opts) (ev agenda.Event, err error) {
	ctx, op := startOp(ctx, "rsvp", scope)
	defer func() { op.done(ctx, err) }()

	switch resp {
	case domain.Accepted, domain.Declined, domain.TentativeResponse:
	default:
		return agenda.Event{}, fail(KValidation, "response", "response must be accepted, declined or tentative")
	}
	if e := checkText("note", note, maxText); e != nil {
		return agenda.Event{}, e
	}
	scope, serr := scopeOf(scope)
	if serr != nil {
		return agenda.Event{}, serr
	}
	if scope == provider.ScopeFollowing {
		return agenda.Event{}, fail(KValidation, "scope", "a reply can apply to this occurrence or the whole series")
	}
	eid, _, _ := parseID(id)
	acctID, err := accountOfEvent(ctx, s.DB.R, eid)
	if errors.Is(err, errGone) {
		return agenda.Event{}, fail(KNotFound, "", "no such event")
	}
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "account of event", err)
	}
	ss, e := s.begin(ctx, acctID)
	if e != nil {
		return agenda.Event{}, e
	}
	defer ss.unlock()
	t, e := s.load(ctx, id, scope, false)
	if e != nil {
		return agenda.Event{}, e
	}
	if e := s.checkVersion(ctx, t, o); e != nil {
		return agenda.Event{}, e
	}
	if !t.row.Caps.CanRSVP {
		return agenda.Event{}, fail(KForbidden, "", "you cannot reply to this event")
	}
	t.ref.Acct = ss.acct
	res, err := ss.prov.RSVP(ctx, t.ref, resp, note, scope)
	if err != nil {
		return agenda.Event{}, s.providerFailure(ctx, ss, t, err)
	}
	if err := s.apply(ctx, t.cal, res, nil); err != nil {
		return agenda.Event{}, s.internal(ctx, "mirror rsvp", err)
	}
	s.Sync.Nudge(ss.acct.ID)
	return s.respondErr(ctx, s.shownAfter(ctx, t, scope, res)...)
}

// ---- Meet ----

func (s *Service) AddMeet(ctx context.Context, id string, o Opts) (ev agenda.Event, err error) {
	ctx, op := startOp(ctx, "meet", provider.ScopeAll)
	defer func() { op.done(ctx, err) }()

	eid, _, _ := parseID(id)
	acctID, err := accountOfEvent(ctx, s.DB.R, eid)
	if errors.Is(err, errGone) {
		return agenda.Event{}, fail(KNotFound, "", "no such event")
	}
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "account of event", err)
	}
	ss, e := s.begin(ctx, acctID)
	if e != nil {
		return agenda.Event{}, e
	}
	defer ss.unlock()
	t, e := s.load(ctx, id, provider.ScopeAll, false)
	if e != nil {
		return agenda.Event{}, e
	}
	if e := s.checkVersion(ctx, t, o); e != nil {
		return agenda.Event{}, e
	}
	if !t.row.Caps.CanAddConference || t.row.Conference != nil {
		return agenda.Event{}, fail(KForbidden, "", "a video call cannot be added to this event")
	}
	t.ref.Acct = ss.acct
	res, err := ss.prov.AddConference(ctx, t.ref)
	if err != nil {
		return agenda.Event{}, s.providerFailure(ctx, ss, t, err)
	}
	if err := s.apply(ctx, t.cal, res, nil); err != nil {
		return agenda.Event{}, s.internal(ctx, "mirror meet", err)
	}
	s.Sync.Nudge(ss.acct.ID)
	return s.respondErr(ctx, t.shown, t.row.ID)
}

// ---- reminder override (hub-only) ----

func (s *Service) SetReminder(ctx context.Context, id string, minutes *int) (ev agenda.Event, err error) {
	ctx, op := startOp(ctx, "reminder", "")
	defer func() { op.done(ctx, err) }()
	return s.reminderOnly(ctx, id, minutes)
}

func (s *Service) reminderOnly(ctx context.Context, id string, minutes *int) (agenda.Event, error) {

	if e := checkReminder(minutes); e != nil {
		return agenda.Event{}, e
	}
	eid, rk, _ := parseID(id)
	r, err := loadRow(ctx, s.DB.R, eid)
	if errors.Is(err, errGone) || err == nil && (r.Deleted || r.Status == domain.Cancelled) {
		return agenda.Event{}, fail(KNotFound, "", "no such event")
	}
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "load event", err)
	}
	err = s.DB.Write(ctx, func(tx *db.Tx) error {
		tx.Quiet = true
		if err := setReminder(ctx, tx, eid, rk, minutes); err != nil {
			return err
		}
		return touch(ctx, tx, eid)
	})
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "set reminder", err)
	}
	return s.respondErr(ctx, id, eid)
}
