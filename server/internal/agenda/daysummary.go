package agenda

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"calendar/server/internal/tz"
)

// DaySummary is the morning summary for one local day. Title and Body are ready to show;
// the notification service extension fetches them so no event title ever passes through Apple.
type DaySummary struct {
	Date       string    `json:"date"`
	Count      int       `json:"count"`
	FirstStart *string   `json:"firstStart"` // HH:MM of the first timed event
	Titles     []string  `json:"titles"`     // up to 5, timed first
	NeedsReply int       `json:"needsReply"`
	Free       *FreeSlot `json:"free"` // longest free stretch (>= 60 min) inside working hours
	Title      string    `json:"title"`
	Body       string    `json:"body"`
}

type FreeSlot struct {
	Start string `json:"start"` // HH:MM
	End   string `json:"end"`
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// DaySummary summarises the local day `date` (YYYY-MM-DD, user zone; "" = today).
func (s *Store) DaySummary(ctx context.Context, date string) (DaySummary, error) {
	st, err := s.Settings(ctx)
	if err != nil {
		return DaySummary{}, err
	}
	loc, _ := tz.Load(*st.UserTZID)
	if date == "" {
		date = s.now().In(loc).Format(time.DateOnly)
	}
	day, err := time.ParseInLocation(time.DateOnly, date, loc)
	if err != nil {
		return DaySummary{}, &ValidationError{"date", "must be YYYY-MM-DD"}
	}
	from, to := day.UTC(), day.AddDate(0, 0, 1).UTC()
	ag, err := s.Agenda(ctx, from, to, nil)
	if err != nil {
		return DaySummary{}, err
	}
	return SummariseDay(ag.Items, st, loc, day), nil
}

// SummariseDay builds the summary for the local day starting at `day` from that day's agenda items.
func SummariseDay(items []AgendaItem, st Settings, loc *time.Location, day time.Time) DaySummary {
	out := DaySummary{Date: day.Format(time.DateOnly), Titles: []string{}}
	type iv struct{ a, b time.Time }
	var busy []iv
	var timed, allday []string
	for _, it := range items {
		if it.MyResponse != nil && *it.MyResponse == "declined" {
			continue
		}
		out.Count++
		if it.MyResponse != nil && *it.MyResponse == "needsAction" {
			out.NeedsReply++
		}
		if it.AllDay {
			allday = append(allday, it.Title)
			continue
		}
		timed = append(timed, it.Title)
		if out.FirstStart == nil {
			f := it.Start.In(loc).Format("15:04")
			out.FirstStart = &f
		}
		if it.Busy {
			busy = append(busy, iv{*it.Start, *it.End})
		}
	}
	out.Titles = append(append(out.Titles, timed...), allday...)
	if len(out.Titles) > 5 {
		out.Titles = out.Titles[:5]
	}
	// longest free stretch inside working hours
	wh := st.WorkingHours
	working := false
	for _, d := range wh.Days {
		if d == int(day.Weekday()) {
			working = true
		}
	}
	if working {
		hm := func(v string) time.Time {
			t, _ := time.Parse("15:04", v)
			return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, loc)
		}
		ws, we := hm(wh.Start), hm(wh.End)
		sort.Slice(busy, func(i, j int) bool { return busy[i].a.Before(busy[j].a) })
		cur := ws
		var bestA, bestB time.Time
		gap := func(a, b time.Time) {
			if b.Sub(a) >= time.Hour && b.Sub(a) > bestB.Sub(bestA) {
				bestA, bestB = a, b
			}
		}
		for _, b := range busy {
			if b.b.Before(cur) || !b.a.Before(we) {
				continue
			}
			if b.a.After(cur) {
				gap(cur, b.a)
			}
			if b.b.After(cur) {
				cur = b.b
			}
		}
		if cur.Before(we) {
			gap(cur, we)
		}
		if !bestA.IsZero() {
			out.Free = &FreeSlot{Start: bestA.In(loc).Format("15:04"), End: bestB.In(loc).Format("15:04")}
		}
	}
	switch {
	case out.Count == 0:
		out.Title, out.Body = "Nothing on today", "Your day is clear."
	default:
		out.Title = "Today: " + plural(out.Count, "event", "events")
		if out.FirstStart != nil {
			out.Title += ", first at " + *out.FirstStart
		}
		var parts []string
		if len(out.Titles) > 0 {
			parts = append(parts, strings.Join(out.Titles, ", ")+".")
		}
		if out.NeedsReply == 1 {
			parts = append(parts, "One reply needed.")
		} else if out.NeedsReply > 1 {
			parts = append(parts, fmt.Sprintf("%d replies needed.", out.NeedsReply))
		}
		if out.Free != nil {
			parts = append(parts, fmt.Sprintf("Free from %s to %s.", out.Free.Start, out.Free.End))
		}
		out.Body = strings.Join(parts, " ")
	}
	return out
}
