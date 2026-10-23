package events

import (
	"context"
	"errors"
	"sort"

	"calendar/server/internal/agenda"
	"calendar/server/internal/db"
	"calendar/server/internal/domain"
	"calendar/server/internal/provider"
)

// Move changes the calendar of an event. Within one account the provider moves it and the
// event keeps its id. Across accounts it is create-then-delete (the delete is only attempted
// after the create succeeded) and the event gets a new id; repeating series cannot cross accounts.
func (s *Service) Move(ctx context.Context, id, calendarID string, o Opts) (ev agenda.Event, err error) {
	ctx, op := startOp(ctx, "move", provider.ScopeAll)
	defer func() { op.done(ctx, err) }()
	if calendarID == "" {
		return agenda.Event{}, fail(KValidation, "calendarId", "calendarId is required")
	}
	dest, err := loadCalendar(ctx, s.DB.R, calendarID)
	if errors.Is(err, errGone) {
		return agenda.Event{}, fail(KValidation, "calendarId", "no such calendar")
	}
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "load calendar", err)
	}
	if !writable(dest) {
		return agenda.Event{}, fail(KForbidden, "calendarId", "this calendar is read-only")
	}
	eid, _, _ := parseID(id)
	srcAcct, err := accountOfEvent(ctx, s.DB.R, eid)
	if errors.Is(err, errGone) {
		return agenda.Event{}, fail(KNotFound, "", "no such event")
	}
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "account of event", err)
	}
	if srcAcct == dest.AccountID {
		return s.moveSame(ctx, id, dest, o)
	}
	return s.moveAcross(ctx, id, srcAcct, dest, o)
}

func (s *Service) moveSame(ctx context.Context, id string, dest domain.Calendar, o Opts) (agenda.Event, error) {
	ss, e := s.begin(ctx, dest.AccountID)
	if e != nil {
		return agenda.Event{}, e
	}
	defer ss.unlock()
	t, e := s.load(ctx, id, provider.ScopeAll, true)
	if e != nil {
		return agenda.Event{}, e
	}
	if e := s.checkVersion(ctx, t, o); e != nil {
		return agenda.Event{}, e
	}
	if !t.row.Caps.CanEdit {
		return agenda.Event{}, fail(KForbidden, "", "you cannot move this event")
	}
	if t.cal.ID == dest.ID {
		return s.respondErr(ctx, t.shown, t.row.ID)
	}
	if t.row.Kind == domain.Exception && t.master == nil {
		return agenda.Event{}, fail(KValidation, "", "the repeating series is not available")
	}
	t.ref.Acct = ss.acct
	res, err := ss.prov.Move(ctx, t.ref, dest)
	if err != nil {
		return agenda.Event{}, s.providerFailure(ctx, ss, t, err)
	}
	seriesID := t.row.ID
	if t.master != nil {
		seriesID = t.master.ID
	}
	err = s.DB.Write(ctx, func(tx *db.Tx) error {
		tx.Quiet = true
		// the rows (and their ids) follow the event to the new calendar before the provider state is applied
		if err := tx.Change(ctx, "event", seriesID, "upsert"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE events SET calendar_id=?, rev=? WHERE id=? OR series_id=?`, dest.ID, tx.Rev(), seriesID, seriesID); err != nil {
			return err
		}
		if err := applyTx(ctx, s, tx, dest, res); err != nil {
			return err
		}
		return s.Sync.Materialiser().Rebuild(ctx, tx, []string{seriesID})
	})
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "mirror move", err)
	}
	s.Sync.Nudge(ss.acct.ID)
	shown := t.shown
	if t.master != nil && t.row.Kind == domain.Master {
		shown = t.row.ID
	}
	return s.respondErr(ctx, shown, seriesID)
}

func (s *Service) moveAcross(ctx context.Context, id, srcAcct string, dest domain.Calendar, o Opts) (agenda.Event, error) {
	// lock both accounts in a fixed order so two opposite moves cannot deadlock
	ids := []string{srcAcct, dest.AccountID}
	sort.Strings(ids)
	var sessions = map[string]*sess{}
	for _, a := range ids {
		ss, e := s.begin(ctx, a)
		if e != nil {
			for _, x := range sessions {
				x.unlock()
			}
			return agenda.Event{}, e
		}
		sessions[a] = ss
	}
	defer func() {
		for _, x := range sessions {
			x.unlock()
		}
	}()
	src, dst := sessions[srcAcct], sessions[dest.AccountID]
	t, e := s.load(ctx, id, provider.ScopeAll, false)
	if e != nil {
		return agenda.Event{}, e
	}
	if e := s.checkVersion(ctx, t, o); e != nil {
		return agenda.Event{}, e
	}
	if !t.row.Caps.CanEdit {
		return agenda.Event{}, fail(KForbidden, "", "you cannot move this event")
	}
	if t.row.Kind != domain.Single {
		return agenda.Event{}, fail(KValidation, "calendarId", "a repeating event cannot be moved to another account")
	}
	r := t.row
	in := provider.EventInput{ID: remoteID(o.Seed), Title: r.Title, Description: r.Description, Location: r.Location, When: r.When, Silent: true}
	for _, a := range r.Attendees {
		if !a.IsSelf && !a.IsOrganizer && a.Role != "resource" {
			in.Attendees = append(in.Attendees, a.Email)
		}
	}
	created, err := dst.prov.Create(ctx, dst.acct, dest, in)
	if err != nil {
		return agenda.Event{}, s.providerFailure(ctx, dst, &target{cal: dest}, err)
	}
	t.ref.Acct = src.acct
	if _, err := src.prov.Delete(ctx, t.ref, provider.ScopeAll); err != nil {
		// undo the copy so nothing is duplicated, then report the original failure
		for _, c := range created.Events {
			if _, derr := dst.prov.Delete(ctx, provider.Ref{Acct: dst.acct, Cal: dest, Event: c}, provider.ScopeAll); derr != nil {
				// the copy could not be removed: the event now exists in both accounts
				consistency(ctx, "move_copy_not_removed", derr)
			}
		}
		return agenda.Event{}, s.providerFailure(ctx, src, t, err)
	}
	var newID string
	err = s.DB.Write(ctx, func(tx *db.Tx) error {
		tx.Quiet = true
		if err := applyTx(ctx, s, tx, dest, created); err != nil {
			return err
		}
		if err := applyTx(ctx, s, tx, t.cal, provider.Result{Deleted: []string{r.RemoteID}}); err != nil {
			return err
		}
		var err error
		newID, err = localID(ctx, tx, dest.ID, created.Events[0].RemoteID)
		return err
	})
	if err != nil {
		return agenda.Event{}, s.internal(ctx, "mirror move", err)
	}
	s.Sync.Nudge(src.acct.ID)
	s.Sync.Nudge(dst.acct.ID)
	return s.respondErr(ctx, newID)
}
