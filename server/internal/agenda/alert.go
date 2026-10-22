package agenda

import (
	"time"
)

// AllDayAlertAt is when a reminder for an all-day event starting on startDate (YYYY-MM-DD, user
// zone) fires, given its lead in minutes and the all-day alert time. See AlertDefaults.
func AllDayAlertAt(startDate string, leadMin int, allDayTime string, loc *time.Location) (time.Time, bool) {
	d, err := time.ParseInLocation(time.DateOnly, startDate, loc)
	if err != nil || leadMin < 0 {
		return time.Time{}, false
	}
	if leadMin%1440 != 0 {
		return d.Add(-time.Duration(leadMin) * time.Minute), true
	}
	hhmm, ok := validHHMM(allDayTime)
	if !ok {
		hhmm, _ = validHHMM(DefaultAllDayTime)
	}
	day := d.AddDate(0, 0, -leadMin/1440)
	return time.Date(day.Year(), day.Month(), day.Day(), hhmm/60, hhmm%60, 0, 0, loc), true
}
