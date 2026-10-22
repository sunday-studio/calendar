package agenda

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var dayNames = map[string]string{"MO": "Mon", "TU": "Tue", "WE": "Wed", "TH": "Thu", "FR": "Fri", "SA": "Sat", "SU": "Sun"}
var dayOrder = []string{"MO", "TU", "WE", "TH", "FR", "SA", "SU"}
var wdCode = [7]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

func ordinal(n int) string {
	switch {
	case n == -1:
		return "last"
	case n < 0:
		return fmt.Sprintf("%d from last", -n)
	}
	suf := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suf = "st"
		case 2:
			suf = "nd"
		case 3:
			suf = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suf)
}

// Summarise renders an RRULE (without prefix) as short English. start is the first
// occurrence in its own zone; it supplies the weekday/day-of-month when the rule omits them.
func Summarise(rrule string, start time.Time) string {
	p := map[string]string{}
	for _, part := range strings.Split(rrule, ";") {
		k, v, ok := strings.Cut(part, "=")
		if ok {
			p[strings.ToUpper(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	n := 1
	if v, err := strconv.Atoi(p["INTERVAL"]); err == nil && v > 1 {
		n = v
	}
	var out string
	switch p["FREQ"] {
	case "DAILY":
		out = "Daily"
		if n > 1 {
			out = fmt.Sprintf("Every %d days", n)
		}
	case "WEEKLY":
		days := parseDays(p["BYDAY"])
		if len(days) == 0 && !start.IsZero() {
			days = []string{wdCode[start.Weekday()]}
		}
		set := map[string]bool{}
		for _, d := range days {
			set[d] = true
		}
		weekdays := len(set) == 5 && set["MO"] && set["TU"] && set["WE"] && set["TH"] && set["FR"]
		switch {
		case weekdays && n == 1:
			out = "Every weekday"
		default:
			names := []string{}
			for _, c := range dayOrder {
				if set[c] {
					names = append(names, dayNames[c])
				}
			}
			base := "Weekly"
			if n > 1 {
				base = fmt.Sprintf("Every %d weeks", n)
			}
			out = base
			if len(names) > 0 {
				out += " on " + strings.Join(names, ", ")
			}
		}
	case "MONTHLY":
		base := "Monthly"
		if n > 1 {
			base = fmt.Sprintf("Every %d months", n)
		}
		switch {
		case p["BYMONTHDAY"] != "":
			days := strings.Split(p["BYMONTHDAY"], ",")
			for i, d := range days {
				if d == "-1" {
					days[i] = "last"
				}
			}
			out = base + " on day " + strings.Join(days, ", ")
		case p["BYDAY"] != "":
			out = base + " on the " + nthDay(p["BYDAY"], p["BYSETPOS"])
		case !start.IsZero():
			out = fmt.Sprintf("%s on day %d", base, start.Day())
		default:
			out = base
		}
	case "YEARLY":
		out = "Yearly"
		if n > 1 {
			out = fmt.Sprintf("Every %d years", n)
		}
	default:
		return "Repeats"
	}
	if c, err := strconv.Atoi(p["COUNT"]); err == nil && c > 0 {
		if c == 1 {
			out += ", once"
		} else {
			out += fmt.Sprintf(", %d times", c)
		}
	} else if u := p["UNTIL"]; u != "" {
		if len(u) >= 8 {
			if t, err := time.Parse("20060102", u[:8]); err == nil {
				out += ", until " + t.Format("2 Jan 2006")
			}
		}
	}
	return out
}

// parseDays returns two-letter codes, stripping any ordinal prefix.
func parseDays(s string) []string {
	var out []string
	for _, d := range strings.Split(s, ",") {
		d = strings.ToUpper(strings.TrimLeft(strings.TrimSpace(d), "+-0123456789"))
		if _, ok := dayNames[d]; ok {
			out = append(out, d)
		}
	}
	return out
}

func nthDay(byday, setpos string) string {
	first := strings.Split(byday, ",")[0]
	code := strings.ToUpper(strings.TrimLeft(first, "+-0123456789"))
	prefix := strings.TrimSuffix(strings.ToUpper(first), code)
	pos := prefix
	if pos == "" {
		pos = setpos
	}
	name := dayNames[code]
	if name == "" {
		name = "day"
	}
	if v, err := strconv.Atoi(pos); err == nil && v != 0 {
		return ordinal(v) + " " + name
	}
	return name
}
