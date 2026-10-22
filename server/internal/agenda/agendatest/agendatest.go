// Package agendatest seeds a database directly (bypassing sync) for read-path tests.
package agendatest

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"calendar/server/internal/db"
)

func Open(t testing.TB) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// Ev describes one seeded event; zero values are sensible (single, confirmed, busy, timed).
type Ev struct {
	ID, Cal, Title, Location, Description string
	Kind, Status                          string // default single / confirmed
	Start, End                            int64  // timed (unix)
	TZID                                  string
	SD, ED                                string // all-day dates
	NotBusy                               bool
	Deleted                               bool
	Self                                  string // self_response
	RRule                                 string
	Series                                string
	Conf, ConfKind                        string
	Rev                                   int64
	Attendees                             [][2]string // email, name
	Org                                   [2]string
	Occ                                   []Occ // extra/override occurrences; default one at own times
	NoOcc                                 bool
}

type Occ struct {
	RKey       int64
	Start, End int64
	SD, ED     string
}

type Seed struct {
	D  *db.DB
	tx *db.Tx
}

// Do runs fn with a seeder inside one write transaction.
func Do(t testing.TB, d *db.DB, fn func(s *Seed)) {
	t.Helper()
	err := d.Write(context.Background(), func(tx *db.Tx) error {
		fn(&Seed{D: d, tx: tx})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (s *Seed) exec(q string, a ...any) {
	if _, err := s.tx.Exec(q, a...); err != nil {
		panic(fmt.Sprintf("%v: %s", err, q))
	}
}

func (s *Seed) Account(id, color, email string) {
	s.exec(`INSERT INTO accounts(id,provider,external_id,email,display_name,color,status,secret,created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		id, "google", id, email, "Acct "+id, color, "ok", []byte("x"), time.Now().Unix())
}

func (s *Seed) Calendar(id, acc, name string, visible bool, access string, primary bool) {
	s.exec(`INSERT INTO calendars(id,account_id,remote_id,name,tzid,access,is_primary,visible) VALUES (?,?,?,?,?,?,?,?)`,
		id, acc, id, name, "Europe/London", access, primary, visible)
}

func nz(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Seed) Event(e Ev) {
	if e.Kind == "" {
		e.Kind = "single"
	}
	if e.Status == "" {
		e.Status = "confirmed"
	}
	if e.Rev == 0 {
		e.Rev = 7
	}
	allDay := e.SD != ""
	var su, eu, tzid any
	if !allDay {
		su, eu = e.Start, e.End
		tzid = nz(e.TZID)
		if e.TZID == "" {
			tzid = "Europe/London"
		}
	}
	var del any
	if e.Deleted {
		del = 1
	}
	s.exec(`INSERT INTO events(id,calendar_id,remote_id,kind,series_id,title,description,location,all_day,start_utc,end_utc,tzid,start_date,end_date,rrule,status,busy,
	  organizer_email,organizer_name,self_response,conf_url,conf_kind,can_edit,can_rsvp,can_add_conf,rev,deleted_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,1,0,?,?)`,
		e.ID, e.Cal, e.ID, e.Kind, nz(e.Series), e.Title, nz(e.Description), nz(e.Location), allDay, su, eu, tzid, nz(e.SD), nz(e.ED), nz(e.RRule), e.Status, !e.NotBusy,
		nz(e.Org[0]), nz(e.Org[1]), nz(e.Self), nz(e.Conf), nz(e.ConfKind), e.Rev, del)
	var rowid int64
	if err := s.tx.QueryRow(`SELECT rowid FROM events WHERE id=?`, e.ID).Scan(&rowid); err != nil {
		panic(err)
	}
	att := ""
	for _, a := range e.Attendees {
		s.exec(`INSERT INTO attendees(event_id,email,name,response) VALUES (?,?,?,?)`, e.ID, a[0], nz(a[1]), "needsAction")
		att += a[1] + " " + a[0] + " "
	}
	s.exec(`INSERT INTO events_fts(rowid,title,location,description,attendees) VALUES (?,?,?,?,?)`, rowid, e.Title, e.Location, e.Description, att)
	if e.NoOcc || e.Deleted {
		return
	}
	occs := e.Occ
	if len(occs) == 0 {
		occs = []Occ{{Start: e.Start, End: e.End, SD: e.SD, ED: e.ED}}
		if allDay {
			occs[0].Start, occs[0].End = utcMidnight(e.SD), utcMidnight(e.ED)
		}
	}
	for _, o := range occs {
		sd, ed := o.SD, o.ED
		st, en := o.Start, o.End
		if allDay && sd == "" {
			sd, ed = e.SD, e.ED
		}
		s.exec(`INSERT INTO occurrences(event_id,rkey,calendar_id,all_day,start_utc,end_utc,start_date,end_date) VALUES (?,?,?,?,?,?,?,?)`,
			e.ID, o.RKey, e.Cal, allDay, st, en, nz(sd), nz(ed))
	}
}

func utcMidnight(d string) int64 {
	t, _ := time.Parse(time.DateOnly, d)
	return t.Unix()
}

func (s *Seed) Setting(k, v string) {
	s.exec(`INSERT OR REPLACE INTO settings(key,value) VALUES (?,?)`, k, v)
}

var _ = sql.ErrNoRows
