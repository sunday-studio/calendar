package agenda

import (
	"testing"
	"time"
)

func TestAllDayAlertAt(t *testing.T) {
	london, _ := time.LoadLocation("Europe/London")
	ny, _ := time.LoadLocation("America/New_York")
	at := func(date string, lead int, hhmm string, loc *time.Location) string {
		x, ok := AllDayAlertAt(date, lead, hhmm, loc)
		if !ok {
			return "invalid"
		}
		return x.Format("2006-01-02 15:04 MST")
	}
	for _, c := range []struct {
		date string
		lead int
		time string
		loc  *time.Location
		want string
	}{
		{"2026-10-16", 1440, "09:00", london, "2026-10-15 09:00 BST"}, // one day = the previous day at 09:00
		{"2026-10-16", 2880, "09:00", london, "2026-10-14 09:00 BST"},
		{"2026-10-16", 0, "09:00", london, "2026-10-16 09:00 BST"}, // the day itself
		{"2026-10-16", 1440, "18:30", london, "2026-10-15 18:30 BST"},
		{"2026-10-16", 1440, "", london, "2026-10-15 09:00 BST"}, // missing time = default
		{"2026-10-16", 1440, "junk", london, "2026-10-15 09:00 BST"},
		{"2026-10-16", 720, "09:00", london, "2026-10-15 12:00 BST"},  // not whole days: 12 h before midnight
		{"2026-10-26", 1440, "09:00", london, "2026-10-25 09:00 GMT"}, // the clocks went back that night
		{"2026-03-29", 1440, "09:00", london, "2026-03-28 09:00 GMT"},
		{"2026-10-16", 1440, "09:00", ny, "2026-10-15 09:00 EDT"},
		{"nope", 1440, "09:00", london, "invalid"},
	} {
		if got := at(c.date, c.lead, c.time, c.loc); got != c.want {
			t.Errorf("%s lead %d %s: got %s want %s", c.date, c.lead, c.time, got, c.want)
		}
	}
	if _, ok := AllDayAlertAt("2026-10-16", -5, "09:00", london); ok {
		t.Error("negative lead accepted")
	}
}
