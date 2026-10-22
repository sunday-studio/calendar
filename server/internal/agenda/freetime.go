package agenda

import (
	"context"
	"sort"
	"time"

	"calendar/server/internal/tz"
)

// FreeTimeQuery asks for gaps of DurationMin inside [From, To).
type FreeTimeQuery struct {
	DurationMin        int       `json:"durationMin"`
	From               time.Time `json:"from"`
	To                 time.Time `json:"to"`
	IgnoreWorkingHours bool      `json:"ignoreWorkingHours"`
	DayPart            string    `json:"dayPart"` // "", morning, afternoon, evening
	Limit              int       `json:"limit"`
}

type Slot struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Reason string    `json:"reason"`
	Best   bool      `json:"best"`
}

// Closest is a near miss: a longer slot later ("later") or the longest shorter gap in range ("shorter").
type Closest struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	DurationMin int       `json:"durationMin"`
	Kind        string    `json:"kind"`
	Reason      string    `json:"reason"`
}

type FreeTime struct {
	DurationMin  int           `json:"durationMin"`
	From         time.Time     `json:"from"`
	To           time.Time     `json:"to"`
	WorkingHours *WorkingHours `json:"workingHours"` // nil when working hours were ignored
	Slots        []Slot        `json:"slots"`
	Closest      []Closest     `json:"closest"`
}

const (
	maxFreeWindow = 62 * 24 * time.Hour
	gridMinutes   = 15
)

type block struct {
	a, b  time.Time
	title string
}

type gap struct {
	a, b       time.Time
	prev, next *block
	edgeA      bool // starts at the edge of the searched window (not at a meeting)
	edgeB      bool
}

func (s *Store) busyBlocks(ctx context.Context, from, to time.Time, loc *time.Location) ([]block, error) {
	ag, err := s.Agenda(ctx, from.AddDate(0, 0, -1), to.AddDate(0, 0, 1), nil)
	if err != nil {
		return nil, err
	}
	return blocksOf(ag.Items, loc), nil
}

// blocksOf turns agenda items into sorted busy blocks (declined and non-busy skipped).
func blocksOf(items []AgendaItem, loc *time.Location) []block {
	var out []block
	for _, it := range items {
		if !it.Busy || (it.MyResponse != nil && *it.MyResponse == "declined") {
			continue
		}
		if it.AllDay {
			a, e1 := time.ParseInLocation(time.DateOnly, it.Date, loc)
			b, e2 := time.ParseInLocation(time.DateOnly, it.EndDate, loc)
			if e1 != nil || e2 != nil {
				continue
			}
			out = append(out, block{a.UTC(), b.UTC(), it.Title})
			continue
		}
		out = append(out, block{*it.Start, *it.End, it.Title})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].a.Before(out[j].a) })
	return out
}

func ceilGrid(t time.Time) time.Time {
	t = t.Truncate(time.Minute)
	m := t.Minute() % gridMinutes
	if m != 0 || t.Second() != 0 {
		t = t.Add(time.Duration(gridMinutes-m) * time.Minute)
	}
	return t
}

// dayWindows lists the allowed [a,b) windows per local day in [from,to).
func dayWindows(from, to time.Time, loc *time.Location, wh WorkingHours, lunch *LunchBreak, ignore bool, part string) [][2]time.Time {
	var out [][2]time.Time
	day := time.Date(from.In(loc).Year(), from.In(loc).Month(), from.In(loc).Day(), 0, 0, 0, 0, loc)
	workDay := map[int]bool{}
	for _, d := range wh.Days {
		workDay[d] = true
	}
	hm := func(d time.Time, v string) time.Time {
		t, _ := time.Parse("15:04", v)
		return time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	}
	for ; day.Before(to); day = day.AddDate(0, 0, 1) {
		next := day.AddDate(0, 0, 1)
		a, b := day, next
		if !ignore {
			if !workDay[int(day.Weekday())] {
				continue
			}
			a, b = hm(day, wh.Start), hm(day, wh.End)
		}
		switch part {
		case "morning":
			if n := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, loc); b.After(n) {
				b = n
			}
		case "afternoon":
			if n := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, loc); a.Before(n) {
				a = n
			}
			if n := time.Date(day.Year(), day.Month(), day.Day(), 18, 0, 0, 0, loc); b.After(n) {
				b = n
			}
		case "evening":
			if n := time.Date(day.Year(), day.Month(), day.Day(), 17, 0, 0, 0, loc); a.Before(n) {
				a = n
			}
		}
		pieces := [][2]time.Time{{a, b}}
		if lunch != nil && lunch.Enabled && !ignore { // lunch is a preference inside working time, so "ignore working hours" lifts it too
			ls, le := hm(day, lunch.Start), hm(day, lunch.End)
			pieces = nil
			if ls.After(a) {
				pieces = append(pieces, [2]time.Time{a, minT(b, ls)})
			}
			if le.Before(b) {
				pieces = append(pieces, [2]time.Time{maxT(a, le), b})
			}
		}
		for _, pc := range pieces {
			x, y := pc[0], pc[1]
			if x.Before(from) {
				x = from
			}
			if y.After(to) {
				y = to
			}
			if y.After(x) {
				out = append(out, [2]time.Time{x.UTC(), y.UTC()})
			}
		}
	}
	return out
}

func minT(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxT(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func gapsIn(windows [][2]time.Time, busy []block, now time.Time) []gap {
	var gaps []gap
	for _, w := range windows {
		cur := w[0]
		if cur.Before(now) {
			cur = now
		}
		cur = ceilGrid(cur)
		var prev *block
		edgeA := true
		for i := range busy {
			bl := busy[i]
			if !bl.b.After(cur) {
				if bl.b.After(w[0]) || bl.b.Equal(w[0]) {
					prev, edgeA = &busy[i], false
				}
				continue
			}
			if !bl.a.Before(w[1]) {
				break
			}
			if bl.a.After(cur) {
				gaps = append(gaps, gap{a: cur, b: bl.a, prev: prev, next: &busy[i], edgeA: edgeA})
			}
			if bl.b.After(cur) {
				cur = ceilGrid(bl.b)
			}
			prev, edgeA = &busy[i], false
		}
		if cur.Before(w[1]) {
			gaps = append(gaps, gap{a: cur, b: w[1], prev: prev, edgeA: edgeA, edgeB: true})
		}
	}
	return gaps
}

func reasonFor(g gap) string {
	switch {
	case g.prev != nil && g.next != nil:
		return "Between two meetings"
	case g.prev != nil:
		return "After " + g.prev.title
	case g.next != nil:
		return "Before " + g.next.title
	}
	return "Nothing else on"
}

// FreeTime finds gaps from the busy occurrences of visible calendars, inside working hours
// unless told otherwise. It is deterministic; nothing here involves a language model.
func (s *Store) FreeTime(ctx context.Context, q FreeTimeQuery) (FreeTime, error) {
	if err := validateFreeTime(q); err != nil {
		return FreeTime{}, err
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return FreeTime{}, err
	}
	loc, _ := tz.Load(*st.UserTZID)
	// look 14 days past the window too, for the "later" near miss
	busy, err := s.busyBlocks(ctx, q.From.UTC(), q.To.UTC().AddDate(0, 0, 14), loc)
	if err != nil {
		return FreeTime{}, err
	}
	return computeFreeTime(q, busy, st, loc, s.now())
}

// FreeTimeFromItems answers a free-time query from an agenda item list (used by the fixtures hub).
func FreeTimeFromItems(q FreeTimeQuery, items []AgendaItem, st Settings, now time.Time) (FreeTime, error) {
	if err := validateFreeTime(q); err != nil {
		return FreeTime{}, err
	}
	loc, _ := tz.Load(*st.UserTZID)
	return computeFreeTime(q, blocksOf(items, loc), st, loc, now)
}

func validateFreeTime(q FreeTimeQuery) error {
	switch {
	case q.DurationMin < 5 || q.DurationMin > 24*60:
		return &ValidationError{"durationMin", "must be 5 to 1440 minutes"}
	case !q.To.After(q.From):
		return &ValidationError{"to", "to must be after from"}
	case q.To.Sub(q.From) > maxFreeWindow:
		return &ValidationError{"to", "window may span at most 62 days"}
	case q.DayPart != "" && q.DayPart != "morning" && q.DayPart != "afternoon" && q.DayPart != "evening":
		return &ValidationError{"dayPart", "must be morning, afternoon or evening"}
	case q.Limit < 0 || q.Limit > 50:
		return &ValidationError{"limit", "must be 0 to 50"}
	}
	return nil
}

func computeFreeTime(q FreeTimeQuery, busy []block, st Settings, loc *time.Location, now time.Time) (FreeTime, error) {
	limit := q.Limit
	if limit == 0 {
		limit = 5
	}
	dur := time.Duration(q.DurationMin) * time.Minute
	from, to := q.From.UTC(), q.To.UTC()
	out := FreeTime{DurationMin: q.DurationMin, From: from, To: to, Slots: []Slot{}, Closest: []Closest{}}
	if !q.IgnoreWorkingHours {
		wh := st.WorkingHours
		out.WorkingHours = &wh
	}
	extTo := to.AddDate(0, 0, 14)
	inRange := gapsIn(dayWindows(from, to, loc, st.WorkingHours, st.LunchBreak, q.IgnoreWorkingHours, q.DayPart), busy, now)

	type cand struct {
		slot  Slot
		slack time.Duration
	}
	var cands []cand
	var shorter *gap
	for i := range inRange {
		g := inRange[i]
		if g.b.Sub(g.a) >= dur {
			cands = append(cands, cand{Slot{Start: g.a, End: g.a.Add(dur), Reason: reasonFor(g)}, g.b.Sub(g.a) - dur})
		} else if g.b.Sub(g.a) >= 30*time.Minute && (shorter == nil || g.b.Sub(g.a) > shorter.b.Sub(shorter.a)) {
			shorter = &inRange[i]
		}
	}
	if len(cands) > 0 {
		// best fit: the tightest gap (least slack), earliest on ties
		best := 0
		for i, c := range cands {
			if c.slack < cands[best].slack {
				best = i
			}
		}
		cands[best].slot.Best = true
		for _, c := range cands {
			out.Slots = append(out.Slots, c.slot)
		}
		if len(out.Slots) > limit {
			// keep the best fit, then the earliest others, in time order
			keep := []Slot{}
			for _, sl := range out.Slots {
				if sl.Best || len(keep) < limit-1 {
					keep = append(keep, sl)
				}
			}
			out.Slots = keep
		}
		return out, nil
	}
	// nothing fits: show the longest shorter gap and the first fitting slot after the window
	if shorter != nil {
		d := shorter.b.Sub(shorter.a)
		out.Closest = append(out.Closest, Closest{Start: shorter.a, End: shorter.b, DurationMin: int(d / time.Minute), Kind: "shorter", Reason: reasonFor(*shorter)})
	}
	later := gapsIn(dayWindows(to, extTo, loc, st.WorkingHours, st.LunchBreak, q.IgnoreWorkingHours, q.DayPart), busy, now)
	for _, g := range later {
		if g.b.Sub(g.a) >= dur {
			out.Closest = append([]Closest{{Start: g.a, End: g.a.Add(dur), DurationMin: q.DurationMin, Kind: "later", Reason: reasonFor(g)}}, out.Closest...)
			break
		}
	}
	return out, nil
}
