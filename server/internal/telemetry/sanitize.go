package telemetry

import (
	"net/url"
	"regexp"
	"strings"
)

const maxMessage = 200

var (
	emailRE  = regexp.MustCompile(`[A-Za-z0-9._%+\-']+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	calTokRE = regexp.MustCompile(`cal_[A-Za-z0-9_\-]+`)
	// "Bearer abc", "Authorization: Basic abc", "token=abc", "refresh_token":"abc", access_token=abc, client_secret=...
	authRE   = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=\-]+`)
	kvSecret = regexp.MustCompile(`(?i)((?:access_|refresh_|id_)?token|secret|client_secret|password|passwd|api[_-]?key|apikey|authorization|signature|sig|code)(["']?\s*[:=]\s*["']?)[^\s"'&,;}]+`)
	// well-known credential shapes: Google access (ya29.) and refresh (1//) tokens, JWTs, long opaque blobs
	googleTokRE = regexp.MustCompile(`\b(?:ya29\.[A-Za-z0-9._\-]+|1//[A-Za-z0-9._\-]{20,})`)
	jwtRE       = regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]*`)
	urlRE       = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.\-]*://[^\s"'<>]+`)
	blobRE      = regexp.MustCompile(`\b[A-Za-z0-9_\-]{32,}\b`)
	ulidRE      = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
)

// Scrub removes e-mail addresses, bearer/refresh/API tokens, URL query strings and credentials and long
// opaque blobs from free text. It is applied to every error message and log string that leaves the process.
func Scrub(s string) string {
	if s == "" {
		return s
	}
	s = urlRE.ReplaceAllStringFunc(s, scrubURL)
	s = emailRE.ReplaceAllString(s, "[email]")
	s = calTokRE.ReplaceAllString(s, "[token]")
	s = authRE.ReplaceAllString(s, "$1 [token]")
	s = googleTokRE.ReplaceAllString(s, "[token]")
	s = jwtRE.ReplaceAllString(s, "[token]")
	s = kvSecret.ReplaceAllString(s, "${1}${2}[redacted]")
	s = blobRE.ReplaceAllStringFunc(s, func(m string) string {
		if ulidRE.MatchString(m) { // our own account/event ULIDs are allowed
			return m
		}
		return "[blob]"
	})
	return s
}

// scrubURL keeps scheme://host and the templated path of a URL found inside text; query, fragment and
// credentials are dropped.
func scrubURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[url]"
	}
	return u.Scheme + "://" + u.Hostname() + TemplatePath(u.Path)
}

// Sanitize is Scrub plus truncation to 200 characters. Use it for anything error-shaped that is
// recorded on a span, a metric attribute or a log.
func Sanitize(msg string) string {
	msg = Scrub(msg)
	if len(msg) > maxMessage {
		cut := maxMessage
		for cut > 0 && !isRuneStart(msg[cut]) {
			cut--
		}
		msg = msg[:cut] + "..."
	}
	return msg
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// literalSegments are path words that are API structure, not data. Anything else becomes ":id".
var literalSegments = map[string]bool{
	// Google Calendar API / OAuth
	"calendar": true, "v3": true, "v2": true, "v1": true, "calendars": true, "events": true, "instances": true,
	"calendarlist": true, "colors": true, "settings": true, "acl": true, "freebusy": true, "channels": true,
	"stop": true, "watch": true, "move": true, "import": true, "quickadd": true, "clear": true, "primary": true,
	"users": true, "me": true, "oauth2": true, "token": true, "revoke": true, "userinfo": true, "auth": true,
	"o": true, "tokeninfo": true,
	// APNs
	"3": true, "device": true, "push": true,
	// hub
	"health": true, "mcp": true, "sse": true, "stream": true,
	// fixed labels used for redacted destinations (ICS feeds, webhooks)
	"feed": true, "webhook": true,
}

// TemplatePath replaces every path segment that is not known API structure with ":id", so URLs never
// carry calendar ids, event ids, feed tokens or e-mail addresses ("/calendar/v3/calendars/:id/events/:id").
func TemplatePath(p string) string {
	if p == "" || p == "/" {
		return "/"
	}
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range segs {
		if s == "" {
			continue
		}
		// a ":verb" suffix such as "events:watch" or "calendars/x:clear" keeps the verb only if it is known
		if base, verb, ok := strings.Cut(s, ":"); ok && base != "" {
			if literalSegments[strings.ToLower(verb)] {
				if literalSegments[strings.ToLower(base)] {
					continue
				}
				segs[i] = ":id:" + verb
				continue
			}
			segs[i] = ":id"
			continue
		}
		if !literalSegments[strings.ToLower(s)] && s != ":id" {
			segs[i] = ":id"
		}
	}
	return "/" + strings.Join(segs, "/")
}

// denyKeys are attribute/log keys that may hold user content or secrets. They never leave the process
// through spans, metrics or the log bridge.
var denyKeys = map[string]bool{
	"title": true, "summary": true, "description": true, "location": true, "attendee": true, "attendees": true,
	"email": true, "organizer": true, "body": true, "query": true, "path": true, "url": true,
	"ics_url": true, "value": true, "feed": true, "feed_url": true, "token": true, "secret": true, "password": true,
	"authorization": true, "api_key": true,
	"display_name": true, "note": true, "notes": true, "client.address": true,
	"network.peer.address": true, "url.query": true, "http.request.header.authorization": true,
}

func denied(key string) bool {
	k := strings.ToLower(key)
	if denyKeys[k] {
		return true
	}
	for _, frag := range []string{"email", "token", "secret", "password", "authorization", "attendee", "cookie", "credential"} {
		if strings.Contains(k, frag) {
			return true
		}
	}
	return false
}
