package ics

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"calendar/server/internal/domain"
	"calendar/server/internal/provider/google"
	"calendar/server/internal/tz"
)

const maxEvents = 50000

// Feed is a parsed iCalendar file.
type Feed struct {
	Name   string
	TZID   string // X-WR-TIMEZONE, used for floating times; "" = UTC
	Events []domain.Event
}

var errNotICS = userErr("That address did not return an iCalendar (.ics) file.")

// Parse reads an iCalendar document into domain events (RemoteID/Kind/Status set; CalendarID empty).
func Parse(body []byte) (feed *Feed, err error) {
	defer func() {
		if r := recover(); r != nil {
			feed, err = nil, errNotICS
		}
	}()
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	if !bytes.Contains(bytes.ToUpper(body[:min(len(body), 4096)]), []byte("BEGIN:VCALENDAR")) {
		return nil, errNotICS
	}
	if tooDeep(body) {
		return nil, userErr("The calendar file is malformed.")
	}
	cal, derr := ical.NewDecoder(bytes.NewReader(body)).Decode()
	if derr != nil {
		return nil, userErr("The calendar file could not be read: %s", shortMsg(derr.Error()))
	}
	f := &Feed{}
	if p := cal.Props.Get("X-WR-CALNAME"); p != nil {
		f.Name = strings.TrimSpace(unescape(p.Value))
	}
	if p := cal.Props.Get("X-WR-TIMEZONE"); p != nil {
		if loc, w := tz.Load(strings.TrimSpace(p.Value)); w == "" {
			f.TZID = loc.String()
		}
	}
	floating, _ := tz.Load(f.TZID)
	if f.TZID == "" {
		floating = time.UTC
	}
	vevents := cal.Events()
	if len(vevents) > maxEvents {
		return nil, userErr("The calendar has too many events (over %d).", maxEvents)
	}
	type item struct {
		ev  domain.Event
		uid string
		rec bool
	}
	var items []item
	masters := map[string]bool{}
	for i := range vevents {
		ev, uid, rec, ok := mapEvent(&vevents[i], f.TZID, floating)
		if !ok {
			continue
		}
		if !rec && ev.Kind == domain.Master {
			masters[uid] = true
		}
		items = append(items, item{ev, uid, rec})
	}
	// Exceptions whose master is not in the feed (an invitation to one occurrence) stand alone.
	byID := map[string]int{}
	for _, it := range items {
		ev := it.ev
		if it.rec && !masters[it.uid] {
			ev.Kind, ev.RemoteSeriesID, ev.RecurrenceKey = domain.Single, "", nil
		}
		ev.ETag = etag(ev)
		if j, dup := byID[ev.RemoteID]; dup { // duplicate UID: the later one wins
			f.Events[j] = ev
			continue
		}
		byID[ev.RemoteID] = len(f.Events)
		f.Events = append(f.Events, ev)
	}
	return f, nil
}

func tooDeep(b []byte) bool {
	depth := 0
	for len(b) > 0 {
		line := b
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			line, b = b[:i], b[i+1:]
		} else {
			b = nil
		}
		if len(line) < 4 || (line[0] != 'B' && line[0] != 'b' && line[0] != 'E' && line[0] != 'e') {
			continue
		}
		u := bytes.ToUpper(bytes.TrimRight(line, "\r"))
		switch {
		case bytes.HasPrefix(u, []byte("BEGIN:")):
			if depth++; depth > 8 {
				return true
			}
		case bytes.HasPrefix(u, []byte("END:")):
			depth--
		}
	}
	return false
}

func shortMsg(s string) string {
	s = strings.TrimPrefix(s, "ical: ")
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

func etag(ev domain.Event) string {
	ev.ETag, ev.ID, ev.CalendarID, ev.Rev = "", "", "", 0
	b, _ := json.Marshal(ev)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// unescape decodes iCalendar TEXT (the whole value; commas are not separators here).
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n', 'N':
				sb.WriteByte('\n')
			default:
				sb.WriteByte(s[i])
			}
			continue
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

func text(c *ical.Component, name string) string {
	if p := c.Props.Get(name); p != nil {
		return strings.TrimSpace(unescape(p.Value))
	}
	return ""
}

func mailto(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 7 && strings.EqualFold(v[:7], "mailto:") {
		return strings.ToLower(strings.TrimSpace(v[7:]))
	}
	return ""
}

// parsed is one DTSTART-like value.
type parsed struct {
	t      time.Time // timed: the instant; all-day: UTC midnight of the date
	allDay bool
	tzid   string
}

func cleanTZID(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"`)
	if strings.HasPrefix(s, "/") { // /mozilla.org/20050126_1/Europe/London
		parts := strings.Split(s, "/")
		for n := 2; n > 0; n-- {
			if len(parts) >= n+1 {
				if _, w := tz.Load(strings.Join(parts[len(parts)-n:], "/")); w == "" {
					return strings.Join(parts[len(parts)-n:], "/")
				}
			}
		}
	}
	return s
}

func parseTime(p *ical.Prop, floatName string, floating *time.Location) (parsed, bool) {
	v := strings.TrimSpace(p.Value)
	vt := strings.ToUpper(p.Params.Get("VALUE"))
	if vt == "DATE" || (vt == "" && len(v) == 8) {
		d, err := time.Parse("20060102", v)
		if err != nil {
			return parsed{}, false
		}
		return parsed{t: d, allDay: true}, true
	}
	if strings.HasSuffix(v, "Z") || strings.HasSuffix(v, "z") {
		t, err := time.Parse("20060102T150405Z", strings.ToUpper(v))
		if err != nil {
			return parsed{}, false
		}
		return parsed{t: t.UTC(), tzid: "UTC"}, true
	}
	loc, name := floating, floatName
	if name == "" {
		name = "UTC"
	}
	if z := cleanTZID(p.Params.Get("TZID")); z != "" {
		l, w := tz.Load(z)
		loc, name = l, z
		if w != "" {
			name = "UTC"
		}
	}
	t, err := time.ParseInLocation("20060102T150405", v, loc)
	if err != nil {
		return parsed{}, false
	}
	return parsed{t: t.UTC(), tzid: name}, true
}

func dateOf(t time.Time) domain.Date { return domain.Date{Y: t.Year(), M: int(t.Month()), D: t.Day()} }

// dateLine rebuilds an EXDATE/RDATE property as the "NAME;PARAMS:value" line internal/recur reads.
func dateLine(p ical.Prop) string {
	var sb strings.Builder
	sb.WriteString(p.Name)
	if z := p.Params.Get("TZID"); z != "" {
		sb.WriteString(";TZID=" + cleanTZID(z))
	}
	if v := p.Params.Get("VALUE"); v != "" {
		sb.WriteString(";VALUE=" + strings.ToUpper(v))
	}
	sb.WriteString(":" + strings.TrimSpace(p.Value))
	return sb.String()
}

func mapEvent(c *ical.Event, floatName string, floating *time.Location) (ev domain.Event, uid string, isRec, ok bool) {
	sp := c.Props.Get("DTSTART")
	if sp == nil {
		return ev, "", false, false
	}
	start, good := parseTime(sp, floatName, floating)
	if !good {
		return ev, "", false, false
	}
	uid = text(c.Component, "UID")
	if uid == "" { // not valid iCalendar, but cheap to tolerate
		sum := sha256.Sum256([]byte(text(c.Component, "SUMMARY") + "|" + sp.Value))
		uid = "noid-" + hex.EncodeToString(sum[:8])
	}
	ev = domain.Event{
		RemoteID: uid, ICalUID: uid, Status: domain.Confirmed, Busy: true,
		Title: text(c.Component, "SUMMARY"), Description: text(c.Component, "DESCRIPTION"), Location: text(c.Component, "LOCATION"),
	}
	// when
	var end *parsed
	if ep := c.Props.Get("DTEND"); ep != nil {
		if e, ok := parseTime(ep, floatName, floating); ok {
			end = &e
		}
	} else if dp := c.Props.Get("DURATION"); dp != nil {
		if d, err := dp.Duration(); err == nil && d >= 0 {
			e := start
			e.t = start.t.Add(d)
			if start.allDay {
				e.t = start.t.AddDate(0, 0, int((d+12*time.Hour)/(24*time.Hour)))
			}
			end = &e
		}
	}
	if start.allDay {
		sd, ed := dateOf(start.t), dateOf(start.t.AddDate(0, 0, 1))
		if end != nil && end.allDay {
			if e := dateOf(end.t); e.String() > sd.String() {
				ed = e
			}
		}
		ev.When = domain.When{AllDay: true, StartDate: sd, EndDate: ed}
	} else {
		et := start.t
		if end != nil && !end.allDay && end.t.After(start.t) {
			et = end.t
		}
		ev.When = domain.When{Start: start.t, End: et, TZID: start.tzid}
	}
	// recurrence
	var rrule string
	if p := c.Props.Get("RRULE"); p != nil {
		rrule = strings.TrimSpace(p.Value)
	}
	rec := &domain.Recurrence{RRule: rrule}
	for _, p := range c.Props.Values("EXDATE") {
		rec.EXDates = append(rec.EXDates, dateLine(p))
	}
	for _, p := range c.Props.Values("RDATE") {
		rec.RDates = append(rec.RDates, dateLine(p))
	}
	ev.Kind = domain.Single
	if rid := c.Props.Get("RECURRENCE-ID"); rid != nil {
		if r, ok := parseTime(rid, floatName, floating); ok {
			isRec = true
			k := r.t.Unix()
			ev.Kind, ev.RemoteSeriesID, ev.RecurrenceKey = domain.Exception, uid, &k
			ev.RemoteID = uid + "#" + strconv.FormatInt(k, 10)
		}
	} else if rrule != "" || len(rec.RDates) > 0 {
		ev.Kind, ev.Recurrence = domain.Master, rec
	}
	// status, availability
	switch strings.ToUpper(text(c.Component, "STATUS")) {
	case "CANCELLED":
		ev.Status = domain.Cancelled
	case "TENTATIVE":
		ev.Status = domain.Tentative
	}
	if strings.EqualFold(text(c.Component, "TRANSP"), "TRANSPARENT") {
		ev.Busy = false
	}
	if p := c.Props.Get("LAST-MODIFIED"); p != nil {
		if t, ok := parseTime(p, "", time.UTC); ok && !t.allDay {
			tt := t.t
			ev.RemoteUpdatedAt = &tt
		}
	}
	// people
	if p := c.Props.Get("ORGANIZER"); p != nil {
		if e := mailto(p.Value); e != "" {
			ev.Organizer = &domain.Person{Email: e, Name: strings.Trim(p.Params.Get("CN"), `"`)}
		}
	}
	seen := map[string]bool{}
	for _, p := range c.Props.Values("ATTENDEE") {
		e := mailto(p.Value)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		role := "required"
		switch {
		case strings.EqualFold(p.Params.Get("CUTYPE"), "RESOURCE"), strings.EqualFold(p.Params.Get("CUTYPE"), "ROOM"):
			role = "resource"
		case strings.EqualFold(p.Params.Get("ROLE"), "OPT-PARTICIPANT"):
			role = "optional"
		}
		resp := domain.NeedsAction
		switch strings.ToUpper(p.Params.Get("PARTSTAT")) {
		case "ACCEPTED":
			resp = domain.Accepted
		case "DECLINED":
			resp = domain.Declined
		case "TENTATIVE":
			resp = domain.TentativeResponse
		}
		ev.Attendees = append(ev.Attendees, domain.Attendee{Email: e, Name: strings.Trim(p.Params.Get("CN"), `"`), Response: resp, Role: role,
			IsOrganizer: ev.Organizer != nil && ev.Organizer.Email == e})
	}
	sort.Slice(ev.Attendees, func(i, j int) bool { return ev.Attendees[i].Email < ev.Attendees[j].Email })
	// links
	var link string
	if p := c.Props.Get("URL"); p != nil {
		link = strings.TrimSpace(p.Value)
		if u, err := url.Parse(link); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
			ev.HTMLLink = link
		}
	}
	for _, name := range []string{"X-GOOGLE-CONFERENCE", "CONFERENCE", "X-MICROSOFT-SKYPETEAMSMEETINGURL", "X-MICROSOFT-ONLINEMEETINGEXTERNALLINK"} {
		for _, p := range c.Props.Values(name) {
			if v := strings.TrimSpace(p.Value); strings.HasPrefix(strings.ToLower(v), "http") {
				k := google.KindForURL(v)
				if k == "" {
					k = "other"
				}
				ev.Conference = &domain.Conference{URL: v, Kind: k, Source: "structured"}
				break
			}
		}
		if ev.Conference != nil {
			break
		}
	}
	if ev.Conference == nil {
		ev.Conference = google.ParseJoinLink(ev.Location, ev.Description, link)
	}
	// ev.Caps stays all-false: ICS feeds are read-only.
	return ev, uid, isRec, true
}
