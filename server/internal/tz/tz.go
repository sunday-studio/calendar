// Package tz loads IANA zones (tzdata embedded) and maps Windows zone names to IANA.
package tz

import (
	"log/slog"
	"strings"
	"time"
	_ "time/tzdata" // embed the IANA database so containers without /usr/share/zoneinfo work
)

// Load returns the IANA location for name. An empty or unknown name falls back to UTC and
// the returned warning is non-empty (callers log it). Windows names are mapped first.
func Load(name string) (loc *time.Location, warning string) {
	if name == "" {
		return time.UTC, "empty timezone, using UTC"
	}
	if name == "UTC" || name == "Z" {
		return time.UTC, ""
	}
	if l, err := time.LoadLocation(name); err == nil {
		return l, ""
	}
	if iana, ok := WindowsToIANA(name); ok {
		if l, err := time.LoadLocation(iana); err == nil {
			return l, ""
		}
	}
	return time.UTC, "unknown timezone " + name + ", using UTC"
}

// MustLoad is Load that logs the warning.
func MustLoad(name string, log *slog.Logger) *time.Location {
	l, w := Load(name)
	if w != "" {
		if log == nil {
			log = slog.Default()
		}
		log.Warn("tz", "msg", w)
	}
	return l
}

// WindowsToIANA maps a Windows time zone name (Microsoft Graph) to its primary IANA zone.
func WindowsToIANA(name string) (string, bool) {
	v, ok := windowsZones[strings.TrimSpace(name)]
	return v, ok
}

// Common CLDR windowsZones (territory 001 = primary zone). Not exhaustive; unknown names fall back to UTC.
var windowsZones = map[string]string{
	"UTC":                             "UTC",
	"Dateline Standard Time":          "Etc/GMT+12",
	"Hawaiian Standard Time":          "Pacific/Honolulu",
	"Alaskan Standard Time":           "America/Anchorage",
	"Pacific Standard Time":           "America/Los_Angeles",
	"Pacific Standard Time (Mexico)":  "America/Tijuana",
	"US Mountain Standard Time":       "America/Phoenix",
	"Mountain Standard Time":          "America/Denver",
	"Central Standard Time":           "America/Chicago",
	"Central America Standard Time":   "America/Guatemala",
	"Canada Central Standard Time":    "America/Regina",
	"Central Standard Time (Mexico)":  "America/Mexico_City",
	"Eastern Standard Time":           "America/New_York",
	"US Eastern Standard Time":        "America/Indianapolis",
	"SA Pacific Standard Time":        "America/Bogota",
	"Atlantic Standard Time":          "America/Halifax",
	"Newfoundland Standard Time":      "America/St_Johns",
	"SA Eastern Standard Time":        "America/Cayenne",
	"E. South America Standard Time":  "America/Sao_Paulo",
	"Argentina Standard Time":         "America/Buenos_Aires",
	"Greenland Standard Time":         "America/Godthab",
	"Azores Standard Time":            "Atlantic/Azores",
	"Cape Verde Standard Time":        "Atlantic/Cape_Verde",
	"GMT Standard Time":               "Europe/London",
	"Greenwich Standard Time":         "Atlantic/Reykjavik",
	"W. Europe Standard Time":         "Europe/Berlin",
	"Central Europe Standard Time":    "Europe/Budapest",
	"Romance Standard Time":           "Europe/Paris",
	"Central European Standard Time":  "Europe/Warsaw",
	"W. Central Africa Standard Time": "Africa/Lagos",
	"GTB Standard Time":               "Europe/Bucharest",
	"E. Europe Standard Time":         "Europe/Chisinau",
	"FLE Standard Time":               "Europe/Kiev",
	"Egypt Standard Time":             "Africa/Cairo",
	"South Africa Standard Time":      "Africa/Johannesburg",
	"Israel Standard Time":            "Asia/Jerusalem",
	"Turkey Standard Time":            "Europe/Istanbul",
	"Arab Standard Time":              "Asia/Riyadh",
	"Arabic Standard Time":            "Asia/Baghdad",
	"Russian Standard Time":           "Europe/Moscow",
	"E. Africa Standard Time":         "Africa/Nairobi",
	"Iran Standard Time":              "Asia/Tehran",
	"Arabian Standard Time":           "Asia/Dubai",
	"Pakistan Standard Time":          "Asia/Karachi",
	"India Standard Time":             "Asia/Calcutta",
	"Sri Lanka Standard Time":         "Asia/Colombo",
	"Nepal Standard Time":             "Asia/Katmandu",
	"Bangladesh Standard Time":        "Asia/Dhaka",
	"SE Asia Standard Time":           "Asia/Bangkok",
	"China Standard Time":             "Asia/Shanghai",
	"Singapore Standard Time":         "Asia/Singapore",
	"W. Australia Standard Time":      "Australia/Perth",
	"Taipei Standard Time":            "Asia/Taipei",
	"Tokyo Standard Time":             "Asia/Tokyo",
	"Korea Standard Time":             "Asia/Seoul",
	"Cen. Australia Standard Time":    "Australia/Adelaide",
	"AUS Central Standard Time":       "Australia/Darwin",
	"E. Australia Standard Time":      "Australia/Brisbane",
	"AUS Eastern Standard Time":       "Australia/Sydney",
	"West Pacific Standard Time":      "Pacific/Port_Moresby",
	"Tasmania Standard Time":          "Australia/Hobart",
	"New Zealand Standard Time":       "Pacific/Auckland",
	"Fiji Standard Time":              "Pacific/Fiji",
	"Tonga Standard Time":             "Pacific/Tongatapu",
	"Morocco Standard Time":           "Africa/Casablanca",
}
