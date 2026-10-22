// Package automate runs user-defined automations: a trigger (an event about to start, a new
// event, a daily cron time) delivered to a signed webhook or an ntfy topic.
package automate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"github.com/oklog/ulid/v2"

	"calendar/server/internal/agenda"
	"calendar/server/internal/db"
	"calendar/server/internal/tz"
	"calendar/server/internal/vault"
)

// Trigger is the stored trigger. Type is event.start | event.created | cron.
type Trigger struct {
	Type       string `json:"type"`
	OffsetMin  int    `json:"offsetMin,omitempty"`  // event.start: minutes relative to the start (-5 = five minutes before)
	CalendarID string `json:"calendarId,omitempty"` // event.*: only this calendar
	Expr       string `json:"expr,omitempty"`       // cron: "m h dom mon dow" in the user's zone
}

// Action is the stored action. Secrets are sealed with the vault and never returned.
type Action struct {
	Type   string `json:"type"` // webhook | ntfy
	URL    string `json:"url,omitempty"`
	Server string `json:"server,omitempty"` // ntfy base URL, default https://ntfy.sh
	Topic  string `json:"topic,omitempty"`
	// Secret (webhook signing key) and Token (ntfy bearer) are accepted as plaintext on input only.
	Secret       string `json:"secret,omitempty"`
	Token        string `json:"token,omitempty"`
	SecretSealed string `json:"secretSealed,omitempty"`
	TokenSealed  string `json:"tokenSealed,omitempty"`
}

// Automation is the API shape.
type Automation struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Enabled bool      `json:"enabled"`
	Trigger Trigger   `json:"trigger"`
	Action  PubAction `json:"action"`
	LastRun *Run      `json:"lastRun"`
}

// PubAction is an Action as shown back to the client: no secret material.
type PubAction struct {
	Type      string `json:"type"`
	URL       string `json:"url,omitempty"`
	Server    string `json:"server,omitempty"`
	Topic     string `json:"topic,omitempty"`
	SecretSet bool   `json:"secretSet"`
	TokenSet  bool   `json:"tokenSet"`
}

type Run struct {
	ID           int64     `json:"id"`
	AutomationID string    `json:"automationId"`
	At           time.Time `json:"at"`
	Trigger      string    `json:"trigger"`
	OK           bool      `json:"ok"`
	Status       *int      `json:"status"`
	Detail       string    `json:"detail,omitempty"`
	Ms           int64     `json:"ms"`
}

// ValidationError maps to HTTP 400.
type ValidationError struct{ Field, Msg string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Msg }

var ErrNotFound = errors.New("automations: not found")

type Service struct {
	DB    *db.DB
	Vault *vault.Vault // seals webhook secrets and ntfy tokens; nil = secrets are refused
	Store *agenda.Store
	HTTP  *http.Client
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	Log   *slog.Logger
	// Retry waits between delivery attempts (default 2s, 10s).
	Retry []time.Duration
	// AllowPrivate is accepted for completeness: targets on the home network are the point of this feature.
	wg gosync.WaitGroup
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

var topicRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validURL(field, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, &ValidationError{field, "must be an http(s) URL"}
	}
	return u, nil
}

func validateTrigger(t Trigger) error {
	switch t.Type {
	case "event.start":
		if t.OffsetMin < -1440 || t.OffsetMin > 60 {
			return &ValidationError{"trigger.offsetMin", "must be between -1440 and 60 minutes"}
		}
	case "event.created":
	case "cron":
		if _, err := ParseCron(t.Expr); err != nil {
			return &ValidationError{"trigger.expr", err.Error()}
		}
		return nil
	default:
		return &ValidationError{"trigger.type", "must be event.start, event.created or cron"}
	}
	return nil
}

// seal turns plaintext secrets into sealed ones and checks the action.
func (s *Service) prepareAction(a Action, id string, old *Action) (Action, error) {
	out := Action{Type: a.Type, URL: a.URL, Server: a.Server, Topic: a.Topic}
	if old != nil { // keep what is not being replaced
		out.SecretSealed, out.TokenSealed = old.SecretSealed, old.TokenSealed
	}
	switch a.Type {
	case "webhook":
		if _, err := validURL("action.url", a.URL); err != nil {
			return out, err
		}
		out.Server, out.Topic, out.TokenSealed = "", "", ""
	case "ntfy":
		if !topicRE.MatchString(a.Topic) {
			return out, &ValidationError{"action.topic", "topic must be 1-64 letters, digits, - or _"}
		}
		if out.Server == "" {
			out.Server = "https://ntfy.sh"
		}
		if _, err := validURL("action.server", out.Server); err != nil {
			return out, err
		}
		out.URL, out.SecretSealed = "", ""
	default:
		return out, &ValidationError{"action.type", "must be webhook or ntfy"}
	}
	sealField := func(plain string, field string, dst *string) error {
		if plain == "" {
			return nil
		}
		if s.Vault == nil {
			return &ValidationError{field, "secrets cannot be stored: no master key available"}
		}
		b, err := s.Vault.Seal([]byte(plain), []byte(id))
		if err != nil {
			return err
		}
		*dst = base64.StdEncoding.EncodeToString(b)
		return nil
	}
	if err := sealField(a.Secret, "action.secret", &out.SecretSealed); err != nil {
		return out, err
	}
	if err := sealField(a.Token, "action.token", &out.TokenSealed); err != nil {
		return out, err
	}
	return out, nil
}

func pub(a Action) PubAction {
	return PubAction{Type: a.Type, URL: a.URL, Server: a.Server, Topic: a.Topic, SecretSet: a.SecretSealed != "", TokenSet: a.TokenSealed != ""}
}

func (s *Service) open(sealed, id string) string {
	if sealed == "" || s.Vault == nil {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return ""
	}
	pt, err := s.Vault.Open(raw, []byte(id))
	if err != nil {
		s.log().Error("automation secret cannot be opened", "id", id)
		return ""
	}
	return string(pt)
}

type row struct {
	id, name string
	enabled  bool
	trig     Trigger
	act      Action
}

func (s *Service) rows(ctx context.Context, where string, args ...any) ([]row, error) {
	rs, err := s.DB.R.QueryContext(ctx, `SELECT id,name,enabled,trigger,action FROM automations `+where+` ORDER BY rowid`, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		var t, a string
		if err := rs.Scan(&r.id, &r.name, &r.enabled, &t, &a); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(t), &r.trig) != nil || json.Unmarshal([]byte(a), &r.act) != nil {
			s.log().Warn("automation with unreadable definition skipped", "id", r.id)
			continue
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

func (s *Service) lastRun(ctx context.Context, id string) *Run {
	runs, _ := s.Runs(ctx, id, 1)
	if len(runs) == 0 {
		return nil
	}
	return &runs[0]
}

func (s *Service) toAutomation(ctx context.Context, r row) Automation {
	return Automation{ID: r.id, Name: r.name, Enabled: r.enabled, Trigger: r.trig, Action: pub(r.act), LastRun: s.lastRun(ctx, r.id)}
}

func (s *Service) List(ctx context.Context) ([]Automation, error) {
	rs, err := s.rows(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]Automation, 0, len(rs))
	for _, r := range rs {
		out = append(out, s.toAutomation(ctx, r))
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, id string) (Automation, error) {
	rs, err := s.rows(ctx, "WHERE id=?", id)
	if err != nil {
		return Automation{}, err
	}
	if len(rs) == 0 {
		return Automation{}, ErrNotFound
	}
	return s.toAutomation(ctx, rs[0]), nil
}

const maxAutomations = 50

type Input struct {
	Name    string   `json:"name"`
	Enabled *bool    `json:"enabled"`
	Trigger *Trigger `json:"trigger"`
	Action  *Action  `json:"action"`
}

func (s *Service) Create(ctx context.Context, in Input) (Automation, error) {
	name := strings.TrimSpace(in.Name)
	switch {
	case name == "" || len(name) > 100:
		return Automation{}, &ValidationError{"name", "name is required (at most 100 characters)"}
	case in.Trigger == nil:
		return Automation{}, &ValidationError{"trigger", "trigger is required"}
	case in.Action == nil:
		return Automation{}, &ValidationError{"action", "action is required"}
	}
	if err := validateTrigger(*in.Trigger); err != nil {
		return Automation{}, err
	}
	id := ulid.Make().String()
	act, err := s.prepareAction(*in.Action, id, nil)
	if err != nil {
		return Automation{}, err
	}
	var n int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM automations`).Scan(&n); err != nil {
		return Automation{}, err
	}
	if n >= maxAutomations {
		return Automation{}, &ValidationError{"", "too many automations"}
	}
	enabled := in.Enabled == nil || *in.Enabled
	tb, _ := json.Marshal(in.Trigger)
	ab, _ := json.Marshal(act)
	err = s.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO automations(id,name,enabled,trigger,action) VALUES (?,?,?,?,?)`, id, name, enabled, string(tb), string(ab))
		if err != nil {
			return err
		}
		if in.Trigger.Type == "cron" { // a new cron rule starts counting from now, it does not "catch up"
			return s.setMeta(ctx, tx, cronKey(id), strconv.FormatInt(s.now().Unix(), 10))
		}
		return nil
	})
	if err != nil {
		return Automation{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Patch(ctx context.Context, id string, in Input) (Automation, error) {
	rs, err := s.rows(ctx, "WHERE id=?", id)
	if err != nil {
		return Automation{}, err
	}
	if len(rs) == 0 {
		return Automation{}, ErrNotFound
	}
	r := rs[0]
	if in.Name != "" {
		r.name = strings.TrimSpace(in.Name)
		if len(r.name) > 100 {
			return Automation{}, &ValidationError{"name", "at most 100 characters"}
		}
	}
	if in.Enabled != nil {
		r.enabled = *in.Enabled
	}
	cronChanged := false
	if in.Trigger != nil {
		if err := validateTrigger(*in.Trigger); err != nil {
			return Automation{}, err
		}
		cronChanged = in.Trigger.Type == "cron" && (r.trig.Type != "cron" || r.trig.Expr != in.Trigger.Expr)
		r.trig = *in.Trigger
	}
	if in.Action != nil {
		old := r.act
		if r.act, err = s.prepareAction(*in.Action, id, &old); err != nil {
			return Automation{}, err
		}
	}
	tb, _ := json.Marshal(r.trig)
	ab, _ := json.Marshal(r.act)
	err = s.DB.Write(ctx, func(tx *db.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE automations SET name=?, enabled=?, trigger=?, action=? WHERE id=?`, r.name, r.enabled, string(tb), string(ab), id); err != nil {
			return err
		}
		if cronChanged {
			return s.setMeta(ctx, tx, cronKey(id), strconv.FormatInt(s.now().Unix(), 10))
		}
		return nil
	})
	if err != nil {
		return Automation{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.DB.Write(ctx, func(tx *db.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM automations WHERE id=?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		for _, q := range []string{`DELETE FROM automation_fired WHERE automation_id=?`, `DELETE FROM automation_runs WHERE automation_id=?`, `DELETE FROM meta WHERE key='auto_cron_'||?`} {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) Runs(ctx context.Context, id string, limit int) ([]Run, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rs, err := s.DB.R.QueryContext(ctx, `SELECT id,automation_id,ts,trigger,ok,status,COALESCE(detail,''),COALESCE(ms,0) FROM automation_runs WHERE automation_id=? ORDER BY id DESC LIMIT ?`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	out := []Run{}
	for rs.Next() {
		var r Run
		var ts int64
		var st sql.NullInt64
		if err := rs.Scan(&r.ID, &r.AutomationID, &ts, &r.Trigger, &r.OK, &st, &r.Detail, &r.Ms); err != nil {
			return nil, err
		}
		r.At = time.Unix(ts, 0).UTC()
		if st.Valid {
			v := int(st.Int64)
			r.Status = &v
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

func cronKey(id string) string { return "auto_cron_" + id }

func (s *Service) setMeta(ctx context.Context, tx *db.Tx, k, v string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return err
}

func (s *Service) getMeta(ctx context.Context, k string) (string, bool) {
	var v string
	if err := s.DB.R.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, k).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

// ---- delivery ----

// Payload is the JSON body of a webhook.
type Payload struct {
	ID         string             `json:"id"`
	Automation map[string]any     `json:"automation"`
	Trigger    string             `json:"trigger"`
	FiredAt    time.Time          `json:"firedAt"`
	Event      *agenda.Event      `json:"event,omitempty"`
	Summary    *agenda.DaySummary `json:"summary,omitempty"`
	Test       bool               `json:"test,omitempty"`
}

func sign(secret string, ts int64, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// Sign is exported so receivers (and tests) can verify X-Calendar-Signature.
func Sign(secret string, ts int64, body []byte) string { return sign(secret, ts, body) }

func (s *Service) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (s *Service) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func ntfyText(p Payload, loc *time.Location) (title, body string) {
	switch {
	case p.Summary != nil:
		return p.Summary.Title, p.Summary.Body
	case p.Event != nil:
		e := p.Event
		when := e.Date
		if e.Start != nil {
			when = e.Start.In(loc).Format("Mon 2 Jan 15:04")
		}
		parts := []string{when}
		if e.Location != "" {
			parts = append(parts, e.Location)
		}
		if e.Join != nil {
			parts = append(parts, e.Join.URL)
		}
		switch p.Trigger {
		case "event.created":
			return "New: " + e.Title, strings.Join(parts, " · ")
		}
		return e.Title, strings.Join(parts, " · ")
	}
	return "Calendar", "Test notification from your calendar hub"
}

func (s *Service) send(ctx context.Context, r row, p Payload, loc *time.Location) (status int, err error) {
	var req *http.Request
	switch r.act.Type {
	case "webhook":
		body, _ := json.Marshal(p)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, r.act.URL, bytes.NewReader(body))
		if err != nil {
			return 0, err
		}
		ts := s.now().Unix()
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Calendar-Event", p.Trigger)
		req.Header.Set("X-Calendar-Delivery", p.ID)
		req.Header.Set("X-Calendar-Timestamp", strconv.FormatInt(ts, 10))
		if secret := s.open(r.act.SecretSealed, r.id); secret != "" {
			req.Header.Set("X-Calendar-Signature", sign(secret, ts, body))
		}
	case "ntfy":
		title, text := ntfyText(p, loc)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.act.Server, "/")+"/"+r.act.Topic, strings.NewReader(text))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Title", title)
		req.Header.Set("Tags", "calendar")
		if tok := s.open(r.act.TokenSealed, r.id); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	default:
		return 0, fmt.Errorf("unknown action %q", r.act.Type)
	}
	req.Header.Set("User-Agent", "calendard")
	resp, err := s.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	if resp.StatusCode/100 != 2 {
		return resp.StatusCode, fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp.StatusCode, nil
}

// deliver sends with up to three attempts and records the outcome.
func (s *Service) deliver(ctx context.Context, r row, p Payload, loc *time.Location) Run {
	waits := s.Retry
	if waits == nil {
		waits = []time.Duration{2 * time.Second, 10 * time.Second}
	}
	start := time.Now()
	var status int
	var err error
	for attempt := 0; ; attempt++ {
		status, err = s.send(ctx, r, p, loc)
		if err == nil || attempt >= len(waits) || ctx.Err() != nil || (status >= 400 && status < 500 && status != 429) {
			break
		}
		if s.sleep(ctx, waits[attempt]) != nil {
			break
		}
	}
	run := Run{AutomationID: r.id, At: s.now().UTC(), Trigger: p.Trigger, OK: err == nil, Ms: time.Since(start).Milliseconds()}
	if status != 0 {
		st := status
		run.Status = &st
	}
	if err != nil {
		run.Detail = err.Error()
		if len(run.Detail) > 300 {
			run.Detail = run.Detail[:300]
		}
		s.log().Warn("automation delivery failed", "id", r.id, "name", r.name, "err", err)
	}
	var stv any
	if run.Status != nil {
		stv = *run.Status
	}
	_ = s.DB.Write(context.WithoutCancel(ctx), func(tx *db.Tx) error {
		if _, err := tx.Exec(`INSERT INTO automation_runs(automation_id,ts,trigger,ok,status,detail,ms) VALUES (?,?,?,?,?,?,?)`,
			r.id, run.At.Unix(), run.Trigger, run.OK, stv, run.Detail, run.Ms); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM automation_runs WHERE automation_id=? AND id NOT IN (SELECT id FROM automation_runs WHERE automation_id=? ORDER BY id DESC LIMIT 100)`, r.id, r.id)
		return err
	})
	return run
}

func (s *Service) userLoc(ctx context.Context) *time.Location {
	st, err := s.Store.Settings(ctx)
	if err != nil || st.UserTZID == nil {
		return time.UTC
	}
	l, _ := tz.Load(*st.UserTZID)
	return l
}

func (s *Service) payload(r row, trig string) Payload {
	return Payload{ID: ulid.Make().String(), Automation: map[string]any{"id": r.id, "name": r.name}, Trigger: trig, FiredAt: s.now().UTC()}
}

// Test fires an automation once with sample data and returns the delivery result.
func (s *Service) Test(ctx context.Context, id string) (Run, error) {
	rs, err := s.rows(ctx, "WHERE id=?", id)
	if err != nil {
		return Run{}, err
	}
	if len(rs) == 0 {
		return Run{}, ErrNotFound
	}
	r := rs[0]
	p := s.payload(r, r.trig.Type)
	p.Test = true
	switch r.trig.Type {
	case "cron":
		if sum, err := s.Store.DaySummary(ctx, ""); err == nil {
			p.Summary = &sum
		}
	default:
		now := s.now().UTC()
		ev := agenda.Event{AgendaItem: agenda.AgendaItem{ID: "TEST:0", EventID: "TEST", Title: "Test event", Start: &now, End: ptrT(now.Add(30 * time.Minute)), TZID: "UTC", Status: "confirmed", Busy: true}}
		p.Event = &ev
	}
	return s.deliver(ctx, r, p, s.userLoc(ctx)), nil
}

func ptrT(t time.Time) *time.Time { return &t }

// Wait blocks until background deliveries started by Run have finished (tests).
func (s *Service) Wait() { s.wg.Wait() }
