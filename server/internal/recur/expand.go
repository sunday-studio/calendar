// Package recur expands recurring series into the occurrences read model (ENGINEERING §4.5).
package recur

import (
	"sort"
	"strings"
	"time"

	"github.com/teambition/rrule-go"

	"calendar/server/internal/tz"
)

// Series describes one master (all-day via dates, timed via instants + tzid).
type Series struct {
	AllDay     bool
	Start, End time.Time // timed
	TZID       string
	StartDate  string // all-day, inclusive YYYY-MM-DD
	EndDate    string // all-day, exclusive
	RRule      string // value after "RRULE:" (prefix tolerated)
	RDates     []string
	EXDates    []string // raw lines
}

// Occ is one generated occurrence. Key is the original start (unix s; all-day: UTC midnight of the start date).
type Occ struct {
	Key        int64
	AllDay     bool
	Start, End time.Time // timed (UTC instants)
	StartDate  string
	EndDate    string
}

// Window is a half-open range [From, To).
type Window struct{ From, To time.Time }

const iterGuard = 200000

// Expand generates master occurrences overlapping win, honouring COUNT/UNTIL, RDATE and EXDATE.
// Output is sorted by start and unique per key, capped at max (<=0 means no cap).
// warnings carry non-fatal problems (unknown tz, unparsable lines).
func Expand(s Series, win Window, max int) (out []Occ, warnings []string) {
	loc := time.UTC
	if !s.AllDay {
		var w string
		loc, w = tz.Load(s.TZID)
		if w != "" {
			warnings = append(warnings, w)
		}
	}
	// base wall-clock start and duration
	var dtstart time.Time
	var dur time.Duration
	var nDays int
	if s.AllDay {
		sd, err := time.Parse(time.DateOnly, s.StartDate)
		if err != nil {
			return nil, append(warnings, "bad start date "+s.StartDate)
		}
		ed, err := time.Parse(time.DateOnly, s.EndDate)
		if err != nil || !ed.After(sd) {
			ed = sd.AddDate(0, 0, 1)
		}
		dtstart = sd
		nDays = int(ed.Sub(sd).Hours() / 24)
	} else {
		dtstart = s.Start.In(loc)
		dur = s.End.Sub(s.Start)
		if dur < 0 {
			dur = 0
		}
	}

	ex, rd := map[int64]bool{}, []time.Time{}
	exDates := map[string]bool{}
	for _, l := range s.EXDates {
		ps, err := parseDateLine(l, loc)
		if err != nil {
			warnings = append(warnings, "exdate: "+err.Error())
		}
		for _, p := range ps {
			if p.isDate {
				exDates[p.date] = true
			}
			if s.AllDay {
				exDates[p.t.Format(time.DateOnly)] = true
			} else {
				ex[p.t.Unix()] = true
			}
		}
	}
	for _, l := range s.RDates {
		ps, err := parseDateLine(l, loc)
		if err != nil {
			warnings = append(warnings, "rdate: "+err.Error())
		}
		for _, p := range ps {
			rd = append(rd, p.t)
		}
	}

	seen := map[int64]bool{}
	add := func(wall time.Time) {
		var o Occ
		if s.AllDay {
			d := time.Date(wall.Year(), wall.Month(), wall.Day(), 0, 0, 0, 0, time.UTC)
			ds := d.Format(time.DateOnly)
			if exDates[ds] {
				return
			}
			o = Occ{Key: d.Unix(), AllDay: true, StartDate: ds, EndDate: d.AddDate(0, 0, nDays).Format(time.DateOnly)}
			if !d.Before(win.To) || !d.AddDate(0, 0, nDays).After(win.From) {
				return
			}
		} else {
			st := wall.UTC()
			if ex[st.Unix()] || exDates[wall.In(loc).Format(time.DateOnly)] {
				return
			}
			o = Occ{Key: st.Unix(), Start: st, End: st.Add(dur)}
			if !(st.Before(win.To) && (o.End.After(win.From) || (dur == 0 && !st.Before(win.From)))) {
				return
			}
		}
		if seen[o.Key] {
			return
		}
		seen[o.Key] = true
		out = append(out, o)
	}

	rule := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s.RRule), "RRULE:"))
	if rule == "" {
		add(dtstart)
	} else if opt, err := rrule.StrToROptionInLocation(rule, loc); err != nil {
		warnings = append(warnings, "rrule: "+err.Error())
		add(dtstart)
	} else {
		opt.Dtstart = dtstart
		if r, err := rrule.NewRRule(*opt); err != nil {
			warnings = append(warnings, "rrule: "+err.Error())
			add(dtstart)
		} else {
			next := r.Iterator()
			for i := 0; i < iterGuard; i++ {
				t, ok := next()
				if !ok || !t.Before(win.To.Add(24*time.Hour)) {
					break
				}
				if s.AllDay { // rrule-go yields in loc (UTC) already
					add(t)
				} else {
					add(t.In(loc))
				}
			}
		}
	}
	for _, t := range rd {
		if s.AllDay {
			add(t)
		} else {
			add(t.In(loc))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out, warnings
}

type dline struct {
	t      time.Time
	date   string
	isDate bool
}

// parseDateLine parses EXDATE/RDATE lines: NAME[;TZID=..][;VALUE=DATE|DATE-TIME]:v1,v2.
// Floating values use def. PERIOD values are ignored.
func parseDateLine(line string, def *time.Location) ([]dline, error) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return nil, errBad(line)
	}
	head, vals := line[:i], line[i+1:]
	loc := def
	for _, p := range strings.Split(head, ";")[1:] {
		if k, v, ok := strings.Cut(p, "="); ok && strings.EqualFold(k, "TZID") {
			loc, _ = tz.Load(v)
		}
	}
	var out []dline
	for _, v := range strings.Split(vals, ",") {
		v = strings.TrimSpace(v)
		switch {
		case v == "" || strings.Contains(v, "/"):
			continue
		case len(v) == 8:
			d, err := time.Parse("20060102", v)
			if err != nil {
				return out, errBad(line)
			}
			out = append(out, dline{t: d, date: d.Format(time.DateOnly), isDate: true})
		case strings.HasSuffix(v, "Z"):
			d, err := time.Parse("20060102T150405Z", v)
			if err != nil {
				return out, errBad(line)
			}
			out = append(out, dline{t: d})
		default:
			d, err := time.ParseInLocation("20060102T150405", v, loc)
			if err != nil {
				return out, errBad(line)
			}
			out = append(out, dline{t: d})
		}
	}
	return out, nil
}

type errBad string

func (e errBad) Error() string { return "unparsable line " + string(e) }

// FilterDateLine keeps only the values of an EXDATE/RDATE line for which keep(instant) is true and
// returns the rebuilt line, or "" when none remain or the line cannot be parsed. Date-only
// values are tested at UTC midnight of their date.
func FilterDateLine(line string, def *time.Location, keep func(time.Time) bool) string {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return ""
	}
	head, vals := line[:i], line[i+1:]
	var kept []string
	for _, v := range strings.Split(vals, ",") {
		v = strings.TrimSpace(v)
		ds, err := parseDateLine(head+":"+v, def)
		if err != nil || len(ds) != 1 {
			continue
		}
		t := ds[0].t
		if ds[0].isDate {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		}
		if keep(t) {
			kept = append(kept, v)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return head + ":" + strings.Join(kept, ",")
}
