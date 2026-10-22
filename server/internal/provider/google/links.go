package google

import (
	"html"
	"net/url"
	"regexp"
	"strings"

	"calendar/server/internal/domain"
)

var urlRE = regexp.MustCompile(`https?://[^\s"'<>\)\]]+`)

// ParseJoinLink finds the first Meet/Zoom/Teams/Webex link in the given texts (in order).
func ParseJoinLink(texts ...string) *domain.Conference {
	for _, t := range texts {
		for _, m := range urlRE.FindAllString(html.UnescapeString(t), -1) {
			m = strings.TrimRight(m, ".,;:!?")
			if k := KindForURL(m); k != "" {
				return &domain.Conference{URL: m, Kind: k, Source: "parsed"}
			}
		}
	}
	return nil
}

// KindForURL classifies a join URL; "" when it is not a known conferencing link.
func KindForURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	switch {
	case h == "meet.google.com":
		if len(strings.Trim(u.Path, "/")) < 3 {
			return ""
		}
		return "meet"
	case h == "zoom.us" || strings.HasSuffix(h, ".zoom.us") || strings.HasSuffix(h, ".zoomgov.com") || h == "zoomgov.com":
		if strings.HasPrefix(u.Path, "/j/") || strings.HasPrefix(u.Path, "/my/") || strings.HasPrefix(u.Path, "/s/") || strings.HasPrefix(u.Path, "/w/") {
			return "zoom"
		}
	case h == "teams.microsoft.com" || h == "teams.live.com":
		if strings.HasPrefix(u.Path, "/l/meetup-join") || strings.HasPrefix(u.Path, "/meet/") || strings.HasPrefix(u.Path, "/l/meeting") {
			return "teams"
		}
	case h == "webex.com" || strings.HasSuffix(h, ".webex.com"):
		if len(strings.Trim(u.Path, "/")) > 0 {
			return "webex"
		}
	}
	return ""
}
