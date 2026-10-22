package automate

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron is a parsed 5-field expression: minute hour day-of-month month day-of-week.
type Cron struct {
	min, hour, dom, mon, dow []int
	domAny, dowAny           bool
}

var monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
var dowNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

func field(s string, lo, hi int, names map[string]int) ([]int, bool, error) {
	any := s == "*" || s == "?"
	set := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		step := 1
		if i := strings.IndexByte(part, '/'); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n < 1 {
				return nil, false, fmt.Errorf("bad step in %q", s)
			}
			step, part = n, part[:i]
		}
		val := func(x string) (int, error) {
			if n, ok := names[strings.ToLower(x)]; ok {
				return n, nil
			}
			n, err := strconv.Atoi(x)
			if err != nil {
				return 0, fmt.Errorf("bad value %q", x)
			}
			return n, nil
		}
		a, b := lo, hi
		switch {
		case part == "*" || part == "?":
			any = any || step == 1
		case strings.Contains(part, "-"):
			r := strings.SplitN(part, "-", 2)
			var err error
			if a, err = val(r[0]); err != nil {
				return nil, false, err
			}
			if b, err = val(r[1]); err != nil {
				return nil, false, err
			}
		default:
			var err error
			if a, err = val(part); err != nil {
				return nil, false, err
			}
			if !strings.Contains(s, "/") {
				b = a
			}
		}
		if a < lo || b > hi || a > b {
			return nil, false, fmt.Errorf("%q out of range %d-%d", s, lo, hi)
		}
		for v := a; v <= b; v += step {
			set[v] = true
		}
	}
	var out []int
	for v := lo; v <= hi; v++ {
		if set[v] {
			out = append(out, v)
		}
	}
	return out, any, nil
}

// ParseCron parses "m h dom mon dow" (numbers, ranges, lists, steps, month/day names; dow 0 or 7 = Sunday).
func ParseCron(expr string) (*Cron, error) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, fmt.Errorf("cron needs 5 fields (minute hour day-of-month month day-of-week), got %d", len(f))
	}
	c := &Cron{}
	var err error
	if c.min, _, err = field(f[0], 0, 59, nil); err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	if c.hour, _, err = field(f[1], 0, 23, nil); err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	if c.dom, c.domAny, err = field(f[2], 1, 31, nil); err != nil {
		return nil, fmt.Errorf("day of month: %w", err)
	}
	if c.mon, _, err = field(f[3], 1, 12, monthNames); err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	dow := strings.ReplaceAll(f[4], "7", "0")
	if c.dow, c.dowAny, err = field(dow, 0, 6, dowNames); err != nil {
		return nil, fmt.Errorf("day of week: %w", err)
	}
	return c, nil
}

func has(v []int, x int) bool {
	for _, y := range v {
		if y == x {
			return true
		}
	}
	return false
}

func (c *Cron) dayOK(d time.Time) bool {
	if !has(c.mon, int(d.Month())) {
		return false
	}
	domOK, dowOK := has(c.dom, d.Day()), has(c.dow, int(d.Weekday()))
	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return dowOK
	case c.dowAny:
		return domOK
	}
	return domOK || dowOK // both restricted: either matches, as in Vixie cron
}

// Next returns the first matching minute strictly after t, in loc; zero if none within ~8 years.
func (c *Cron) Next(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc).Truncate(time.Minute).Add(time.Minute)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	for i := 0; i < 3000; i, day = i+1, day.AddDate(0, 0, 1) {
		if !c.dayOK(day) {
			continue
		}
		for _, h := range c.hour {
			for _, m := range c.min {
				cand := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, loc)
				if !cand.Before(t) {
					return cand
				}
			}
		}
	}
	return time.Time{}
}
