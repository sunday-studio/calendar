package sync

import (
	"context"
	"strings"
	"testing"
	"time"

	"calendar/server/internal/agenda"
)

// Google edge cases end to end: fake Google -> sync -> read model, in the user zone Europe/London.

func agendaTitles(t *testing.T, e *tenv, from, to string) []string {
	t.Helper()
	f, _ := time.Parse(time.RFC3339, from)
	tt, _ := time.Parse(time.RFC3339, to)
	st := &agenda.Store{DB: e.d, Now: func() time.Time { return time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC) }}
	r, err := st.Agenda(context.Background(), f, tt, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range r.Items {
		title := it.Title
		if title == "" {
			title = "<untitled>"
		}
		if it.AllDay {
			out = append(out, title+"@"+it.Date)
		} else {
			out = append(out, title+"@"+it.Start.Format("01-02T15:04"))
		}
	}
	return out
}

func same(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestGoogleEdgeCasesEndToEnd(t *testing.T) {
	e := newTEnv(t)
	e.fake.addCal("primary@x.com", "Me", "owner")
	e.fake.addCal("bday", "Birthdays", "reader")
	// untitled timed event
	e.fake.put("primary@x.com", "notitle", map[string]any{"start": map[string]any{"dateTime": "2026-10-14T09:00:00+01:00"}, "end": map[string]any{"dateTime": "2026-10-14T09:30:00+01:00"}})
	// timed event across local midnight (23:00-02:00 BST) shows on both days
	e.fake.put("primary@x.com", "night", timed("Night shift", "2026-10-14T23:00:00+01:00", "2026-10-15T02:00:00+01:00"))
	// across the end of BST: 00:30 BST -> 02:30 GMT is three hours of wall time
	e.fake.put("primary@x.com", "clocks", timed("Clocks back", "2026-10-25T00:30:00+01:00", "2026-10-25T02:30:00+00:00"))
	// special types
	wl := allDay("", "2026-10-14", "2026-10-15")
	wl["eventType"], wl["transparency"] = "workingLocation", "transparent"
	wl["workingLocationProperties"] = map[string]any{"type": "homeOffice", "homeOffice": map[string]any{}}
	e.fake.put("primary@x.com", "wl", wl)
	ooo := timed("Out of office", "2026-10-16T09:00:00+01:00", "2026-10-16T17:00:00+01:00")
	ooo["eventType"] = "outOfOffice"
	e.fake.put("primary@x.com", "ooo", ooo)
	focus := timed("", "2026-10-16T13:00:00+01:00", "2026-10-16T15:00:00+01:00")
	focus["eventType"] = "focusTime"
	e.fake.put("primary@x.com", "focus", focus)
	bd := allDay("Sam's birthday", "2026-10-14", "2026-10-15")
	bd["eventType"], bd["transparency"] = "birthday", "transparent"
	e.fake.put("bday", "bd", bd)
	// a recurring series with one cancelled instance that arrives without times
	series := timed("Standup", "2026-10-12T09:00:00+01:00", "2026-10-12T09:15:00+01:00")
	series["recurrence"] = []any{"RRULE:FREQ=DAILY;COUNT=4"}
	e.fake.put("primary@x.com", "ser", series)
	e.fake.put("primary@x.com", "ser_20261013T080000Z", map[string]any{"status": "cancelled", "recurringEventId": "ser",
		"originalStartTime": map[string]any{"dateTime": "2026-10-13T09:00:00+01:00"}})
	a := e.addAccount("a@x.com", "rt-a")
	if err := e.sync(a); err != nil {
		t.Fatal(err)
	}

	// 14 Oct (local): untitled, the working-location bar, the birthday, the start of the night shift, standup
	same(t, agendaTitles(t, e, "2026-10-13T23:00:00Z", "2026-10-14T23:00:00Z"),
		[]string{"Sam's birthday@2026-10-14", "Working from home@2026-10-14", "<untitled>@10-14T08:00", "Standup@10-14T08:00", "Night shift@10-14T22:00"})
	// 15 Oct: the tail of the night shift is still there (sorted under the day it started)
	same(t, agendaTitles(t, e, "2026-10-14T23:00:00Z", "2026-10-15T23:00:00Z"), []string{"Night shift@10-14T22:00", "Standup@10-15T08:00"})
	same(t, agendaTitles(t, e, "2026-10-12T23:00:00Z", "2026-10-13T23:00:00Z"), []string{}) // 13 Oct: the instance was cancelled
	// nothing leaks onto 16 Oct from the night shift; out of office and focus time are plain busy events
	same(t, agendaTitles(t, e, "2026-10-15T23:00:00Z", "2026-10-16T23:00:00Z"), []string{"Out of office@10-16T08:00", "Focus time@10-16T12:00"})
	// clocks back: one event, three hours of elapsed time, on 25 Oct only
	var su, eu int64
	if err := e.d.R.QueryRow(`SELECT o.start_utc, o.end_utc FROM occurrences o JOIN events ev ON ev.id=o.event_id WHERE ev.remote_id='clocks'`).Scan(&su, &eu); err != nil {
		t.Fatal(err)
	}
	if eu-su != 3*3600 || time.Unix(su, 0).UTC().Format(time.RFC3339) != "2026-10-24T23:30:00Z" {
		t.Fatalf("dst span: %s +%ds", time.Unix(su, 0).UTC().Format(time.RFC3339), eu-su)
	}
	same(t, agendaTitles(t, e, "2026-10-24T23:00:00Z", "2026-10-25T23:00:00Z"), []string{"Clocks back@10-24T23:30"})
	// capabilities and busy flags
	check := func(remote string, edit, busy bool) {
		t.Helper()
		var ce, b bool
		if err := e.d.R.QueryRow(`SELECT can_edit, busy FROM events WHERE remote_id=?`, remote).Scan(&ce, &b); err != nil {
			t.Fatal(remote, err)
		}
		if ce != edit || b != busy {
			t.Errorf("%s: canEdit=%v busy=%v, want %v %v", remote, ce, b, edit, busy)
		}
	}
	check("notitle", true, true)
	check("wl", false, false)
	check("ooo", false, true)
	check("focus", false, true)
	check("bd", false, false)
	// a second pass changes nothing and does not crash on any of them
	rev, _ := e.d.Rev(context.Background())
	if err := e.sync(a); err != nil {
		t.Fatal(err)
	}
	if r2, _ := e.d.Rev(context.Background()); r2 != rev {
		t.Fatalf("resync bumped rev %d -> %d", rev, r2)
	}
}
