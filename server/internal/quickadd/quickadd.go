// Package quickadd turns a typed sentence into an event Draft. Nothing is written; the app shows
// the draft, asks about anything ambiguous and then creates the event through the normal write
// path. The deterministic RuleParser always works; an LLM-backed Parser (alfather) can sit in
// front of it and fall back to it.
package quickadd

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"calendar/server/internal/events"
	"calendar/server/internal/tz"
)

// Request is POST /v1/quick-add.
type Request struct {
	Text string    `json:"text"`
	Now  time.Time `json:"now"`
	TZID string    `json:"tzid"`
}

// Contact is someone seen on earlier events.
type Contact struct {
	Email, Name string
	Count       int
	Last        time.Time
}

// Calendar is a writable calendar a draft can land in.
type Calendar struct {
	ID, Name  string
	IsDefault bool
}

// Env supplies what the parser needs from the hub.
type Env interface {
	Contacts(ctx context.Context, name string) ([]Contact, error)
	Calendars(ctx context.Context) ([]Calendar, error)
}

// Option is one answer to an ambiguity. Value is machine-readable (an ISO local date, an
// email, a calendar id, HH:MM); Label is ready to show.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Ambiguity is a question the user must answer. Default is the index of the suggested option
// (the app preselects it only where the design says so); -1 = no suggestion. An empty Options
// list means "ask for a free-form answer" (Optional says the draft is usable without one).
type Ambiguity struct {
	Field    string   `json:"field"`
	Question string   `json:"question"`
	Options  []Option `json:"options"`
	Default  int      `json:"default"`
	Optional bool     `json:"optional"`
}

// Span marks a recognised phrase in the original text (UTF-16 offsets, like NSRange).
type Span struct {
	Kind string `json:"kind"` // who | when | where | other
	From int    `json:"from"`
	To   int    `json:"to"`
}

// Person is someone named in the text; Email is empty when they could not be resolved.
type Person struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// Draft is the answer. Event can be POSTed to /v1/events once the ambiguities are settled.
type Draft struct {
	Confidence   float64         `json:"confidence"`
	Source       string          `json:"source"` // "rules" | "alfather"
	Event        events.CreateIn `json:"event"`
	People       []Person        `json:"people"`
	Ambiguities  []Ambiguity     `json:"ambiguities"`
	Spans        []Span          `json:"spans"`
	CalendarName string          `json:"calendarName,omitempty"`
	// Degraded is true when alfather is configured but could not answer, so the rule parser did.
	Degraded bool `json:"degraded,omitempty"`
}

// Parser is implemented by the rule parser and (later) alfather.
type Parser interface {
	Parse(ctx context.Context, req Request) (Draft, error)
}

// RuleParser is the deterministic parser.
type RuleParser struct {
	Env        Env
	DefaultTZ  string // used when the request has none
	DefaultNow func() time.Time
}

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday, "mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday, "wed": time.Wednesday, "weds": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday, "sat": time.Saturday, "saturday": time.Saturday,
}

var months = map[string]time.Month{
	"jan": 1, "january": 1, "feb": 2, "february": 2, "mar": 3, "march": 3, "apr": 4, "april": 4, "may": 5,
	"jun": 6, "june": 6, "jul": 7, "july": 7, "aug": 8, "august": 8, "sep": 9, "sept": 9, "september": 9,
	"oct": 10, "october": 10, "nov": 11, "november": 11, "dec": 12, "december": 12,
}

const (
	wdAlt = `sunday|sun|monday|mon|tuesday|tues|tue|wednesday|weds|wed|thursday|thurs|thur|thu|friday|fri|saturday|sat`
	moAlt = `january|jan|february|feb|march|mar|april|apr|may|june|jun|july|jul|august|aug|september|sept|sep|october|oct|november|nov|december|dec`
)

var (
	reEmail    = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	reCalHint  = regexp.MustCompile(`(?i)\b(?:on|in|to|into)\s+(?:(?:my|the|our)\s+)?([\p{L}][\p{L}\d_-]*)\s+calendar\b|#([\p{L}][\p{L}\d_-]*)`)
	reEvery    = regexp.MustCompile(`(?i)\bevery\s+(day|weekday|week|month|` + wdAlt + `)\b|\b(daily|weekly|monthly)\b`)
	reInRel    = regexp.MustCompile(`(?i)\bin\s+(\d+|an?|half an)\s*(minutes?|mins?|hours?|hrs?|h|days?|weeks?)\b`)
	reFor      = regexp.MustCompile(`(?i)\bfor\s+(\d+(?:\.\d+)?|an?|half an)\s*(minutes?|mins?|hours?|hrs?|h|m)\b`)
	reDurBare  = regexp.MustCompile(`(?i)\b(\d+(?:\.\d+)?)\s*(minutes?|mins?|hours?|hrs?|h)\b`)
	reRange    = regexp.MustCompile(`(?i)\b(?:from\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*(?:-|–|—|to|until)\s*(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b`)
	reTimeAP   = regexp.MustCompile(`(?i)\b(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)\b`)
	reTime24   = regexp.MustCompile(`\b(?:at\s+)?([01]?\d|2[0-3]):([0-5]\d)\b`)
	reAtBare   = regexp.MustCompile(`(?i)\bat\s+(\d{1,2})\b`)
	reNoon     = regexp.MustCompile(`(?i)\b(noon|midday|midnight)\b`)
	reISO      = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	reDMon     = regexp.MustCompile(`(?i)\b(?:the\s+)?(\d{1,2})(?:st|nd|rd|th)?\s+(?:of\s+)?(` + moAlt + `)\b(?:\s+(\d{4}))?`)
	reMonD     = regexp.MustCompile(`(?i)\b(` + moAlt + `)\s+(\d{1,2})(?:st|nd|rd|th)?\b(?:,?\s+(\d{4}))?`)
	reSlash    = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})(?:/(\d{2,4}))?\b`)
	reWeekday  = regexp.MustCompile(`(?i)\b(?:(this|next|on)\s+)?(` + wdAlt + `)\b`)
	reRelDay   = regexp.MustCompile(`(?i)\b(today|tonight|tomorrow|tmrw|day after tomorrow)\b`)
	reAllDay   = regexp.MustCompile(`(?i)\ball[- ]day\b`)
	reWith     = regexp.MustCompile(`(?i)\b(?:with|w/)\s+`)
	reAt       = regexp.MustCompile(`(?i)(?:\bat\s+|@\s*)`)
	reTimeOnly = regexp.MustCompile(`(?i)^\s*\d{1,2}(?::\d{2})?\s*(?:am|pm)?\s*(?:$|[,.;]|\s(?:on|for|with|tomorrow|today|next|this|every|from)\b)`)
)

// stop words end a person or place phrase.
var stop = map[string]bool{
	"at": true, "on": true, "for": true, "with": true, "tomorrow": true, "today": true, "tonight": true, "next": true, "this": true,
	"every": true, "in": true, "from": true, "to": true, "until": true, "by": true, "tmrw": true, "w/": true, "and": false,
}

type span struct {
	a, b int // rune offsets
	kind string
}

type parse struct {
	text     []rune
	req      Request
	loc      *time.Location
	now      time.Time
	spans    []span
	amb      []Ambiguity
	conf     float64
	event    events.CreateIn
	people   []Person
	rrule    string
	date     *time.Time // local midnight of the chosen date
	hour     int
	minute   int
	hasTime  bool
	endH     int
	endM     int
	hasEnd   bool
	durMin   int
	allDay   bool
	where    string
	calHint  string
	calName  string
	dateDone bool
}

func (p *parse) taken(a, b int) bool {
	for _, s := range p.spans {
		if a < s.b && b > s.a {
			return true
		}
	}
	return false
}

func (p *parse) mark(a, b int, kind string) { p.spans = append(p.spans, span{a, b, kind}) }

// find returns the first match of re whose range is free, as rune offsets and submatches.
func (p *parse) find(re *regexp.Regexp) (a, b int, sub []string, ok bool) {
	s := string(p.text)
	for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
		ra, rb := len([]rune(s[:m[0]])), len([]rune(s[:m[1]]))
		if p.taken(ra, rb) {
			continue
		}
		sub = make([]string, len(m)/2)
		for i := range sub {
			if m[2*i] >= 0 {
				sub[i] = s[m[2*i]:m[2*i+1]]
			}
		}
		return ra, rb, sub, true
	}
	return 0, 0, nil, false
}

func hour24(h int, ap string) (int, bool) {
	ap = strings.ToLower(ap)
	switch {
	case h < 0 || h > 23 && ap == "" || h > 12 && ap != "":
		return 0, false
	case ap == "am":
		if h == 12 {
			return 0, true
		}
		return h, true
	case ap == "pm":
		if h == 12 {
			return 12, true
		}
		return h + 12, true
	}
	return h, true
}

func nearest(from time.Time, wd time.Weekday) time.Time {
	d := (int(wd) - int(from.Weekday()) + 7) % 7
	return from.AddDate(0, 0, d)
}

func dayLabel(t time.Time) string { return t.Format("Mon 2 Jan") }

// Parse implements Parser.
func (rp *RuleParser) Parse(ctx context.Context, req Request) (Draft, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Draft{}, &events.Error{Kind: events.KValidation, Field: "text", Msg: "text is required"}
	}
	if len([]rune(text)) > 500 {
		return Draft{}, &events.Error{Kind: events.KValidation, Field: "text", Msg: "text is too long"}
	}
	zone := req.TZID
	if zone == "" {
		zone = rp.DefaultTZ
	}
	if zone == "" {
		zone = "Europe/London"
	}
	loc, warn := tz.Load(zone)
	if warn != "" {
		return Draft{}, &events.Error{Kind: events.KValidation, Field: "tzid", Msg: "unknown time zone"}
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now()
		if rp.DefaultNow != nil {
			now = rp.DefaultNow()
		}
	}
	p := &parse{text: []rune(text), req: req, loc: loc, now: now.In(loc), conf: 0.3}
	p.durMin = 0
	p.emails()
	p.everyThing()
	p.calendarHint()
	p.place()
	p.durationAndTimes()
	p.dates()
	if err := p.people_(ctx, rp.Env); err != nil {
		return Draft{}, err
	}
	if err := p.calendar(ctx, rp.Env); err != nil {
		return Draft{}, err
	}
	p.finish()
	return p.draft(), nil
}

// ---- recurrence ----

func (p *parse) everyThing() {
	a, b, sub, ok := p.find(reEvery)
	if !ok {
		return
	}
	p.mark(a, b, "when")
	w := strings.ToLower(sub[1] + sub[2])
	switch w {
	case "day", "daily":
		p.rrule = "FREQ=DAILY"
	case "weekday":
		p.rrule = "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"
	case "week", "weekly":
		p.rrule = "FREQ=WEEKLY"
	case "month", "monthly":
		p.rrule = "FREQ=MONTHLY"
	default:
		if wd, ok := weekdays[w]; ok {
			p.rrule = "FREQ=WEEKLY;BYDAY=" + [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}[wd]
			t := nearest(p.today(), wd)
			p.date = &t
		}
	}
}

func (p *parse) today() time.Time {
	return time.Date(p.now.Year(), p.now.Month(), p.now.Day(), 0, 0, 0, 0, p.loc)
}

// ---- calendar hint (#work, "on my work calendar") ----

func (p *parse) calendarHint() {
	a, b, sub, ok := p.find(reCalHint)
	if !ok {
		return
	}
	p.mark(a, b, "other")
	p.calHint = strings.ToLower(sub[1] + sub[2])
}

// ---- place ----

func (p *parse) place() {
	s := string(p.text)
	for _, m := range reAt.FindAllStringIndex(s, -1) {
		a := len([]rune(s[:m[0]]))
		start := len([]rune(s[:m[1]]))
		if p.taken(a, start) || start >= len(p.text) {
			continue
		}
		rest := string(p.text[start:])
		lr := strings.ToLower(rest)
		if reTimeOnly.MatchString(rest) || strings.HasPrefix(lr, "noon") || strings.HasPrefix(lr, "midnight") || strings.HasPrefix(lr, "midday") {
			// "at 1pm", "at noon": a time, not a place — unless the number is not a clock hour ("at 49 Frith St")
			if n, _ := leadingInt(rest); n >= 0 && n <= 24 || !startsDigit(rest) {
				continue
			}
		}
		// the place runs to a stop word, an already-recognised phrase, or the end
		end := len(p.text)
		i := start
		for i < len(p.text) {
			j := i
			for j < len(p.text) && p.text[j] != ' ' {
				j++
			}
			word := strings.ToLower(strings.Trim(string(p.text[i:j]), ",.;"))
			if i > start && (stop[word] || p.taken(i, j)) {
				end = i
				break
			}
			i = j
			for i < len(p.text) && p.text[i] == ' ' {
				i++
			}
		}
		loc := strings.TrimSpace(strings.Trim(strings.TrimSpace(string(p.text[start:end])), ",.;"))
		if loc == "" {
			continue
		}
		// a trailing time or date phrase belongs to those parsers, not the place name
		loc = trimTail(loc)
		if loc == "" {
			continue
		}
		p.where = loc
		endOfLoc := start + len([]rune(loc))
		p.mark(a, endOfLoc, "where")
		return
	}
}

var reTail = regexp.MustCompile(`(?i)\s+(?:(?:on|this|next)\s+)?(?:` + wdAlt + `|today|tomorrow|tonight)\b.*$|\s+(?:at\s+)?\d{1,2}(?::\d{2})?\s*(?:am|pm)\b.*$|\s+\d{1,2}:\d{2}\b.*$|\s+for\s+\d.*$`)

func trimTail(s string) string { return strings.TrimSpace(reTail.ReplaceAllString(s, "")) }

func startsDigit(s string) bool { return len(s) > 0 && s[0] >= '0' && s[0] <= '9' }

func leadingInt(s string) (int, int) {
	n, i := 0, 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		i++
	}
	if i == 0 {
		return -1, 0
	}
	return n, i
}

// ---- duration & times ----

func (p *parse) durationAndTimes() {
	// relative start: "in 2 hours", "in 30 min"
	if a, b, sub, ok := p.find(reInRel); ok {
		n := relNum(sub[1])
		switch u := strings.ToLower(sub[2]); {
		case strings.HasPrefix(u, "min"), strings.HasPrefix(u, "h"):
			d := time.Duration(n * float64(time.Minute))
			if !strings.HasPrefix(u, "min") {
				d = time.Duration(n * float64(time.Hour))
			}
			t := p.now.Add(d).Truncate(time.Minute)
			day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, p.loc)
			p.date, p.hour, p.minute, p.hasTime, p.dateDone = &day, t.Hour(), t.Minute(), true, true
			p.mark(a, b, "when")
		case strings.HasPrefix(u, "day"), strings.HasPrefix(u, "week"):
			days := int(n)
			if strings.HasPrefix(u, "week") {
				days *= 7
			}
			t := p.today().AddDate(0, 0, days)
			p.date, p.dateDone = &t, true
			p.mark(a, b, "when")
		}
	}
	if a, b, sub, ok := p.find(reFor); ok {
		n := relNum(sub[1])
		u := strings.ToLower(sub[2])
		if strings.HasPrefix(u, "h") {
			n *= 60
		}
		p.durMin = int(n + 0.5)
		p.mark(a, b, "when")
	} else if a, b, sub, ok := p.find(reDurBare); ok {
		n, _ := parseF(sub[1])
		if strings.HasPrefix(strings.ToLower(sub[2]), "h") {
			n *= 60
		}
		p.durMin = int(n + 0.5)
		p.mark(a, b, "when")
	}
	// range 1-2pm, 13:00-14:30
	if a, b, sub, ok := p.find(reRange); ok && (sub[3] != "" || sub[6] != "" || sub[2] != "" || sub[5] != "") {
		h1, _ := atoi(sub[1])
		h2, _ := atoi(sub[4])
		ap1, ap2 := sub[3], sub[6]
		if ap1 == "" {
			ap1 = ap2
			// 11-1pm: the start is before noon
			if strings.EqualFold(ap2, "pm") && h1 > h2 && h1 < 12 {
				ap1 = "am"
			}
		}
		if ap2 == "" {
			ap2 = ap1
		}
		sh, ok1 := hour24(h1, ap1)
		eh, ok2 := hour24(h2, ap2)
		if ok1 && ok2 {
			m1, _ := atoi(sub[2])
			m2, _ := atoi(sub[5])
			p.hour, p.minute, p.hasTime = sh, m1, true
			p.endH, p.endM, p.hasEnd = eh, m2, true
			p.mark(a, b, "when")
			return
		}
	}
	if a, b, sub, ok := p.find(reTimeAP); ok {
		h, _ := atoi(sub[1])
		if hh, ok := hour24(h, sub[3]); ok {
			m, _ := atoi(sub[2])
			p.hour, p.minute, p.hasTime = hh, m, true
			p.mark(a, b, "when")
			return
		}
	}
	if a, b, sub, ok := p.find(reTime24); ok {
		h, _ := atoi(sub[1])
		m, _ := atoi(sub[2])
		p.hour, p.minute, p.hasTime = h, m, true
		p.mark(a, b, "when")
		return
	}
	if a, b, sub, ok := p.find(reNoon); ok {
		p.hour, p.minute, p.hasTime = 12, 0, true
		if strings.EqualFold(sub[1], "midnight") {
			p.hour = 0
		}
		p.mark(a, b, "when")
		return
	}
	if a, b, sub, ok := p.find(reAtBare); ok {
		h, _ := atoi(sub[1])
		if h >= 0 && h <= 24 {
			if h >= 13 || h == 0 {
				p.hour, p.hasTime = h%24, true
			} else {
				// 1..12 with no am/pm: offer both, suggest the likelier one
				am, pm := h%12, h%12+12
				def := 1 // pm
				if h >= 8 && h <= 11 {
					def = 0
				}
				p.hour, p.hasTime = []int{am, pm}[def], true
				p.amb = append(p.amb, Ambiguity{Field: "when.time", Question: "Morning or afternoon?", Default: def,
					Options: []Option{{fmt.Sprintf("%02d:00", am), fmt.Sprintf("%d am", h)}, {fmt.Sprintf("%02d:00", pm), fmt.Sprintf("%d pm", h)}}})
			}
			p.mark(a, b, "when")
		}
	}
}

func relNum(s string) float64 {
	switch strings.ToLower(s) {
	case "a", "an":
		return 1
	case "half an":
		return 0.5
	}
	f, _ := parseF(s)
	return f
}

func parseF(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%g", &f)
	return f, err
}

func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, i := leadingInt(s)
	return n, i == len(s)
}

// ---- dates ----

func (p *parse) setDate(t time.Time) {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, p.loc)
	p.date, p.dateDone = &d, true
}

func (p *parse) futureOf(m time.Month, d int, y int) (time.Time, bool) {
	if d < 1 || d > 31 {
		return time.Time{}, false
	}
	today := p.today()
	if y == 0 {
		y = today.Year()
	}
	t := time.Date(y, m, d, 0, 0, 0, 0, p.loc)
	if t.Month() != m {
		return time.Time{}, false
	}
	if t.Before(today) && y == today.Year() {
		t = time.Date(y+1, m, d, 0, 0, 0, 0, p.loc)
	}
	return t, true
}

func (p *parse) dates() {
	if p.dateDone {
		return
	}
	if a, b, sub, ok := p.find(reRelDay); ok {
		t := p.today()
		switch strings.ToLower(sub[1]) {
		case "tomorrow", "tmrw":
			t = t.AddDate(0, 0, 1)
		case "day after tomorrow":
			t = t.AddDate(0, 0, 2)
		case "tonight":
			if !p.hasTime {
				p.amb = append(p.amb, Ambiguity{Field: "when.time", Question: "What time tonight?", Default: -1, Optional: true})
			}
		}
		p.setDate(t)
		p.mark(a, b, "when")
		return
	}
	if a, b, sub, ok := p.find(reISO); ok {
		y, _ := atoi(sub[1])
		m, _ := atoi(sub[2])
		d, _ := atoi(sub[3])
		if t, ok := p.futureOf(time.Month(m), d, y); ok && m >= 1 && m <= 12 {
			p.setDate(t)
			p.mark(a, b, "when")
			return
		}
	}
	if a, b, sub, ok := p.find(reDMon); ok {
		d, _ := atoi(sub[1])
		y, _ := atoi(sub[3])
		if t, ok := p.futureOf(months[strings.ToLower(sub[2])], d, y); ok {
			p.setDate(t)
			p.mark(a, b, "when")
			return
		}
	}
	if a, b, sub, ok := p.find(reMonD); ok {
		d, _ := atoi(sub[2])
		y, _ := atoi(sub[3])
		if t, ok := p.futureOf(months[strings.ToLower(sub[1])], d, y); ok {
			p.setDate(t)
			p.mark(a, b, "when")
			return
		}
	}
	if a, b, sub, ok := p.find(reSlash); ok { // day/month, as in the UK
		d, _ := atoi(sub[1])
		m, _ := atoi(sub[2])
		y, _ := atoi(sub[3])
		if y > 0 && y < 100 {
			y += 2000
		}
		if m >= 1 && m <= 12 {
			if t, ok := p.futureOf(time.Month(m), d, y); ok {
				p.setDate(t)
				p.mark(a, b, "when")
				return
			}
		}
	}
	if p.date != nil { // set by "every monday"
		return
	}
	if a, b, sub, ok := p.find(reWeekday); ok {
		wd := weekdays[strings.ToLower(sub[2])]
		near := nearest(p.today(), wd)
		p.mark(a, b, "when")
		switch strings.ToLower(sub[1]) {
		case "this":
			p.setDate(near)
		case "next":
			p.setDate(near.AddDate(0, 0, 7))
		default:
			// bare weekday: this one or the one after — ask, with the nearest suggested
			name := wd.String()
			p.setDate(near)
			p.amb = append(p.amb, Ambiguity{Field: "when.date", Question: "Which " + name + "?", Default: 0, Options: []Option{
				{near.Format(time.DateOnly), "This " + name + ", " + near.Format("2 Jan")},
				{near.AddDate(0, 0, 7).Format(time.DateOnly), "Next " + name + ", " + near.AddDate(0, 0, 7).Format("2 Jan")},
			}})
		}
	}
}

// ---- people ----

// emails claims explicit addresses first so their "@" is never read as a place.
func (p *parse) emails() {
	for {
		a, b, sub, ok := p.find(reEmail)
		if !ok {
			return
		}
		p.mark(a, b, "who")
		p.people = append(p.people, Person{Name: sub[0], Email: sub[0]})
	}
}

func (p *parse) people_(ctx context.Context, env Env) error {
	for {
		a, b, _, ok := p.find(reWith)
		if !ok {
			break
		}
		// names follow "with" until a stop word or an already-recognised phrase
		var names []string
		i, nameStart, nameEnd := b, b, b
		for i < len(p.text) {
			j := i
			for j < len(p.text) && p.text[j] != ' ' {
				j++
			}
			raw := string(p.text[i:j])
			word := strings.ToLower(strings.Trim(raw, ",.;"))
			if p.taken(i, j) || stop[word] || word == "" {
				break
			}
			if word == "and" || word == "&" {
				i = j
				for i < len(p.text) && p.text[i] == ' ' {
					i++
				}
				continue
			}
			// a number or a month/weekday word is part of a date, not a name
			if _, isDay := weekdays[word]; isDay || startsDigit(word) || months[word] != 0 {
				break
			}
			names = append(names, strings.Trim(raw, ",.;"))
			nameEnd = j
			i = j
			for i < len(p.text) && p.text[i] == ' ' {
				i++
			}
		}
		if len(names) == 0 {
			p.mark(a, b, "keep") // a stray "with": claim it so it is not retried, but keep it in the title
			continue
		}
		p.mark(a, b, "keep")
		p.mark(nameStart, nameEnd, "who")
		for _, n := range names {
			p.people = append(p.people, Person{Name: n})
		}
	}
	// resolve names against people from earlier events
	for i := range p.people {
		pe := &p.people[i]
		if pe.Email != "" || env == nil {
			continue
		}
		cs, err := env.Contacts(ctx, pe.Name)
		if err != nil {
			return err
		}
		switch {
		case len(cs) == 1:
			pe.Email, pe.Name = cs[0].Email, firstNonEmpty(cs[0].Name, pe.Name)
		case len(cs) > 1:
			q := Ambiguity{Field: "attendees", Question: "Which " + pe.Name + "?", Default: 0}
			for _, c := range cs {
				label := firstNonEmpty(c.Name, c.Email)
				if c.Name != "" {
					label += " (" + c.Email + ")"
				}
				q.Options = append(q.Options, Option{c.Email, label})
			}
			p.amb = append(p.amb, q)
		}
	}
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- calendar ----

func (p *parse) calendar(ctx context.Context, env Env) error {
	if env == nil {
		return nil
	}
	cals, err := env.Calendars(ctx)
	if err != nil || len(cals) == 0 {
		return err
	}
	var def *Calendar
	for i := range cals {
		if cals[i].IsDefault {
			def = &cals[i]
		}
	}
	if p.calHint != "" {
		var hits []Calendar
		for _, c := range cals {
			if strings.Contains(strings.ToLower(c.Name), p.calHint) {
				hits = append(hits, c)
			}
		}
		switch {
		case len(hits) == 1:
			p.event.CalendarID, p.calName = hits[0].ID, hits[0].Name
			return nil
		case len(hits) > 1:
			q := Ambiguity{Field: "calendarId", Question: "Which calendar?", Default: -1}
			for _, c := range hits {
				q.Options = append(q.Options, Option{c.ID, c.Name})
			}
			p.amb = append(p.amb, q)
		default:
			// the hint matched nothing: say so instead of guessing
			q := Ambiguity{Field: "calendarId", Question: "No calendar matches \"" + p.calHint + "\". Which one?", Default: -1}
			for _, c := range cals {
				q.Options = append(q.Options, Option{c.ID, c.Name})
			}
			p.amb = append(p.amb, q)
		}
	}
	if def == nil {
		def = &cals[0]
	}
	if p.event.CalendarID == "" {
		p.event.CalendarID, p.calName = def.ID, def.Name
	}
	return nil
}

// ---- assemble ----

func (p *parse) finish() {
	title := p.title()
	p.event.Title = title
	if title == "" {
		p.amb = append(p.amb, Ambiguity{Field: "title", Question: "What is it called?", Default: -1})
	}
	if p.where != "" {
		p.event.Location = p.where
	}
	for _, pe := range p.people {
		if pe.Email != "" {
			p.event.Attendees = append(p.event.Attendees, pe.Email)
		}
	}
	if p.rrule != "" {
		p.event.Recurrence = &events.RecurrenceIn{RRule: p.rrule}
	}
	// when
	date := p.today()
	if p.date != nil {
		date = *p.date
	} else {
		p.amb = append(p.amb, Ambiguity{Field: "when.date", Question: "Which day?", Default: -1, Optional: true})
		p.conf -= 0.1
		// without a day, a time today that has passed means tomorrow
		if p.hasTime && date.Add(time.Duration(p.hour)*time.Hour+time.Duration(p.minute)*time.Minute).Before(p.now) {
			date = date.AddDate(0, 0, 1)
		}
	}
	w := events.When{TZID: p.loc.String()}
	switch {
	case reAllDay.MatchString(string(p.text)):
		w.AllDay, w.Date = true, date.Format(time.DateOnly)
		if a, b, _, ok := p.find(reAllDay); ok {
			p.mark(a, b, "when")
		}
		w.TZID = ""
	default:
		h, m := 9, 0
		if p.hasTime {
			h, m = p.hour, p.minute
		} else {
			p.amb = append(p.amb, Ambiguity{Field: "when.time", Question: "What time?", Default: -1, Optional: true})
		}
		w.Start = date.Format(time.DateOnly) + fmt.Sprintf("T%02d:%02d:00", h, m)
		switch {
		case p.hasEnd:
			st := time.Date(date.Year(), date.Month(), date.Day(), h, m, 0, 0, p.loc)
			en := time.Date(date.Year(), date.Month(), date.Day(), p.endH, p.endM, 0, 0, p.loc)
			if !en.After(st) {
				en = en.AddDate(0, 0, 1)
			}
			w.DurationMin = int(en.Sub(st) / time.Minute)
		case p.durMin > 0:
			w.DurationMin = p.durMin
		default:
			w.DurationMin = 60
		}
	}
	p.event.When = w
	p.conf += 0.1 // a title
	if title == "" {
		p.conf -= 0.1
	}
	if p.date != nil {
		p.conf += 0.25
	}
	if p.hasTime {
		p.conf += 0.25
	}
	if p.where != "" {
		p.conf += 0.05
	}
	for _, a := range p.amb {
		if !a.Optional {
			p.conf -= 0.1
		}
	}
	if p.conf < 0.05 {
		p.conf = 0.05
	}
	if p.conf > 0.95 {
		p.conf = 0.95
	}
}

var reDangling = regexp.MustCompile(`(?i)^(?:(?:at|on|for|from|to|in|by|every|with|and|the)\s+)+|(?:\s+(?:at|on|for|from|to|in|by|every|with|and|the))+$`)

// title is the text with everything recognised as when/where/calendar/recurrence removed;
// "with Sam" stays, as the design shows ("Lunch with Sam").
func (p *parse) title() string {
	var out []rune
	for i, r := range p.text {
		cut := false
		for _, s := range p.spans {
			if i >= s.a && i < s.b && s.kind != "who" && s.kind != "keep" {
				cut = true
			}
		}
		if !cut {
			out = append(out, r)
		}
	}
	t := strings.Join(strings.Fields(string(out)), " ")
	t = strings.Trim(t, " ,.;-–")
	for {
		n := reDangling.ReplaceAllString(t, "")
		n = strings.Trim(n, " ,.;-–")
		if n == t {
			break
		}
		t = n
	}
	if t == "" {
		return ""
	}
	r := []rune(t)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

func (p *parse) draft() Draft {
	d := Draft{Confidence: round2(p.conf), Source: "rules", Event: p.event, People: p.people, Ambiguities: p.amb, CalendarName: p.calName}
	if d.People == nil {
		d.People = []Person{}
	}
	if d.Ambiguities == nil {
		d.Ambiguities = []Ambiguity{}
	}
	if d.Event.Attendees == nil {
		d.Event.Attendees = []string{}
	}
	sort.SliceStable(p.spans, func(i, j int) bool { return p.spans[i].a < p.spans[j].a })
	d.Spans = []Span{}
	for _, s := range p.spans {
		if s.kind == "other" || s.kind == "keep" {
			continue
		}
		d.Spans = append(d.Spans, Span{Kind: s.kind, From: utf16Len(p.text[:s.a]), To: utf16Len(p.text[:s.b])})
	}
	return d
}

func utf16Len(r []rune) int { return len(utf16.Encode(r)) }

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
