package push

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"sync"
	"time"

	"calendar/server/internal/db"
	"calendar/server/internal/telemetry"
)

const (
	window     = 48 * time.Hour
	maxAlerts  = 5 // more alerts than this in one batch collapse into one generic one
	cursorKey  = "push_cursor"
	summaryKey = "summary_sent"
)

// Dispatcher turns committed changes into pushes. Process is one batch; Run drives it from
// commit notifications with a small debounce so a sync page does not become 50 pushes.
type Dispatcher struct {
	DB       *db.DB
	Sender   Sender // nil = push disabled: Process only advances the cursor
	Log      *slog.Logger
	Now      func() time.Time
	Debounce time.Duration // default 2s

	wake chan struct{}
	mu   sync.Mutex
	// reauth holds accounts whose status flipped to needs_reauth since the last batch.
	reauth []string
}

func (d *Dispatcher) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Dispatcher) log() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}

func (d *Dispatcher) init() {
	d.mu.Lock()
	if d.wake == nil {
		d.wake = make(chan struct{}, 1)
	}
	d.mu.Unlock()
}

// Notify is the db.OnCommit hook: it must not block.
func (d *Dispatcher) Notify(int64) {
	d.init()
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// AccountStatus is the accounts.Service.OnStatus hook (stored status values).
func (d *Dispatcher) AccountStatus(id, status string) {
	if status != "needs_reauth" {
		return
	}
	d.mu.Lock()
	d.reauth = append(d.reauth, id)
	d.mu.Unlock()
	d.Notify(0)
}

// Run processes batches until ctx ends.
func (d *Dispatcher) Run(ctx context.Context) {
	d.init()
	d.safeProcess(ctx) // sets the cursor and baseline on first start
	deb := d.Debounce
	if deb <= 0 {
		deb = 2 * time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.wake:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(deb):
		}
		d.safeProcess(ctx)
	}
}

// safeProcess runs one batch; a failure is recorded and the loop carries on (the cursor was not advanced,
// so the next batch retries). A panic is recovered, recorded and counted instead of ending the dispatcher.
func (d *Dispatcher) safeProcess(ctx context.Context) {
	telemetry.Safe(ctx, "push.dispatch", func() {
		ctx, span := telemetry.StartSpan(ctx, "push.process")
		defer span.End()
		if err := d.Process(ctx); err != nil && ctx.Err() == nil {
			telemetry.RecordError(ctx, "push", err, telemetry.Operation("process"))
		}
	})
}

// ---- payloads ----

func silentMsg(rev int64) Message {
	b, _ := json.Marshal(map[string]any{"aps": map[string]any{"content-available": 1}, "k": "sync", "rev": rev})
	return Message{Kind: "sync", Type: "background", Priority: 5, CollapseID: "sync", Body: b}
}

// alertMsg is a generic alert; the notification service extension fetches the real text from the hub.
func alertMsg(kind string, extra map[string]any, thread, collapse string) Message {
	body := "Your calendar changed"
	if kind == "invited" {
		body = "You have a new invitation"
	}
	p := map[string]any{
		"aps": map[string]any{
			"alert":           map[string]any{"title": "Calendar", "body": body},
			"mutable-content": 1,
			"thread-id":       thread,
		},
		"k": kind,
	}
	for k, v := range extra {
		p[k] = v
	}
	b, _ := json.Marshal(p)
	return Message{Kind: kind, Type: "alert", Priority: 10, CollapseID: collapse, Body: b}
}

// ---- classification ----

type snap struct {
	exists  bool // a live, non-cancelled row
	start   int64
	has     bool // has an upcoming occurrence
	invited bool // an invitation still waiting for an answer, from someone else, that reached us recently
}

// inviteFresh is how recent an invitation must be to alert: a reconnect or full resync re-reads old pending
// invitations, and those are not news.
const inviteFresh = 30 * time.Minute

func (d *Dispatcher) snapshot(ctx context.Context, eventID string, now time.Time) (snap, error) {
	var status string
	var del, upd sql.NullInt64
	var resp sql.NullString
	var org int
	err := d.DB.R.QueryRowContext(ctx, `SELECT status, deleted_at, self_response, is_organizer, remote_updated_at FROM events WHERE id=?`, eventID).
		Scan(&status, &del, &resp, &org, &upd)
	if errors.Is(err, sql.ErrNoRows) {
		return snap{}, nil
	}
	if err != nil {
		return snap{}, err
	}
	s := snap{exists: !del.Valid && status != "cancelled"}
	if !s.exists {
		return s, nil
	}
	var st sql.NullInt64
	if err := d.DB.R.QueryRowContext(ctx, `SELECT MIN(start_utc) FROM occurrences WHERE event_id=? AND end_utc>=?`, eventID, now.Unix()).Scan(&st); err != nil {
		return s, err
	}
	s.start, s.has = st.Int64, st.Valid
	s.invited = resp.String == "needsAction" && org == 0 && upd.Valid && now.Sub(time.Unix(upd.Int64, 0)) <= inviteFresh
	return s, nil
}

type evChange struct {
	id    string
	quiet bool // every row for it was a user's own write
}

type decision struct {
	silent bool
	alert  string // "", "moved", "cancelled", "invited"
}

func within(start int64, now time.Time) bool {
	return start >= now.Unix() && start <= now.Add(window).Unix()
}

func (d *Dispatcher) classify(ctx context.Context, c evChange, now time.Time) (decision, error) {
	cur, err := d.snapshot(ctx, c.id, now)
	if err != nil {
		return decision{}, err
	}
	var prevStart int64
	var havePrev bool
	err = d.DB.R.QueryRowContext(ctx, `SELECT start_utc FROM push_state WHERE event_id=?`, c.id).Scan(&prevStart)
	switch {
	case err == nil:
		havePrev = true
	case !errors.Is(err, sql.ErrNoRows):
		return decision{}, err
	}
	var dec decision
	prevUpcoming := havePrev && prevStart > now.Unix()
	curIn := cur.exists && cur.has && within(cur.start, now)
	prevIn := prevUpcoming && within(prevStart, now)
	dec.silent = curIn || prevIn
	switch {
	case c.quiet:
	case prevIn && !cur.exists:
		dec.alert = "cancelled"
	case !havePrev && cur.exists && cur.has && cur.invited && curIn:
		dec.alert = "invited"
	case prevUpcoming && cur.exists && cur.has && cur.start != prevStart && (curIn || prevIn):
		dec.alert = "moved"
	}
	if dec.alert != "" {
		dec.silent = true
	}
	// remember what we now know
	err = d.DB.Write(ctx, func(tx *db.Tx) error {
		if cur.exists && cur.has {
			_, err := tx.ExecContext(ctx, `INSERT INTO push_state(event_id,start_utc,status,seen_at) VALUES (?,?,?,?)
				ON CONFLICT(event_id) DO UPDATE SET start_utc=excluded.start_utc, status=excluded.status, seen_at=excluded.seen_at`,
				c.id, cur.start, "confirmed", now.Unix())
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM push_state WHERE event_id=?`, c.id)
		return err
	})
	return dec, err
}

// ---- delivery ----

type endpoint struct {
	tokenID string
	Target
}

func (d *Dispatcher) endpoints(ctx context.Context) ([]endpoint, error) {
	rs, err := d.DB.R.QueryContext(ctx, `SELECT p.token_id, p.apns_token, p.env, COALESCE(p.bundle_id,'') FROM push_endpoints p
		JOIN tokens t ON t.id=p.token_id WHERE t.revoked_at IS NULL ORDER BY p.token_id, p.apns_token`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []endpoint
	for rs.Next() {
		var e endpoint
		if err := rs.Scan(&e.tokenID, &e.Token, &e.Env, &e.BundleID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rs.Err()
}

// deliver sends m to every endpoint; permanently invalid tokens are removed.
func (d *Dispatcher) deliver(ctx context.Context, m Message) (sent int, err error) {
	if d.Sender == nil {
		return 0, nil
	}
	eps, err := d.endpoints(ctx)
	if err != nil {
		return 0, err
	}
	for _, e := range eps {
		sent += d.sendOne(ctx, e, m)
	}
	return sent, nil
}

func (d *Dispatcher) cursor(ctx context.Context) (int64, bool, error) {
	var v string
	err := d.DB.R.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, cursorKey).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return n, true, nil
}

func (d *Dispatcher) setCursor(ctx context.Context, n int64) error {
	_, err := d.DB.W.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, cursorKey, strconv.FormatInt(n, 10))
	return err
}

// Process handles every change committed since the last batch.
func (d *Dispatcher) Process(ctx context.Context) error {
	d.mu.Lock()
	reauth := d.reauth
	d.reauth = nil
	d.mu.Unlock()

	cur, ok, err := d.cursor(ctx)
	if err != nil {
		return err
	}
	var maxRev int64
	if err := d.DB.R.QueryRowContext(ctx, `SELECT COALESCE(MAX(rev),0) FROM changes`).Scan(&maxRev); err != nil {
		return err
	}
	if !ok { // first start: never replay history; remember where upcoming events are so a later move is recognised
		_, err := d.DB.W.ExecContext(ctx, `INSERT OR IGNORE INTO push_state(event_id,start_utc,status,seen_at)
			SELECT event_id, MIN(start_utc), 'confirmed', ? FROM occurrences WHERE end_utc>=? GROUP BY event_id`, d.now().Unix(), d.now().Unix())
		if err != nil {
			return err
		}
		return d.setCursor(ctx, maxRev)
	}
	rs, err := d.DB.R.QueryContext(ctx, `SELECT rev, entity, entity_id, src FROM changes WHERE rev>? AND rev<=? ORDER BY rev`, cur, maxRev)
	if err != nil {
		return err
	}
	byEvent := map[string]*evChange{}
	var order []string
	other := false
	for rs.Next() {
		var rev int64
		var entity, id, src string
		if err := rs.Scan(&rev, &entity, &id, &src); err != nil {
			rs.Close()
			return err
		}
		if entity != "event" {
			other = true // calendars, settings, accounts: the agenda may look different
			continue
		}
		c := byEvent[id]
		if c == nil {
			c = &evChange{id: id, quiet: true}
			byEvent[id] = c
			order = append(order, id)
		}
		if src != "user" {
			c.quiet = false
		}
	}
	rs.Close()
	if err := rs.Err(); err != nil {
		return err
	}
	now := d.now()
	silent := other
	var alerts, invites []Message
	sort.Strings(order)
	for _, id := range order {
		dec, err := d.classify(ctx, *byEvent[id], now)
		if err != nil {
			return err
		}
		silent = silent || dec.silent
		switch dec.alert {
		case "invited":
			invites = append(invites, alertMsg("invited", map[string]any{"eventId": id, "rev": maxRev}, "invitations", "inv-"+id))
		case "":
		default:
			alerts = append(alerts, alertMsg(dec.alert, map[string]any{"eventId": id, "rev": maxRev}, "events", "ev-"+id))
		}
	}
	// A pile of invitations at once is a first sync or a reconnect, not news: say nothing rather than one vague alert.
	if len(invites) <= maxAlerts {
		alerts = append(alerts, invites...)
	}
	if len(alerts) > maxAlerts {
		alerts = []Message{alertMsg("changed", map[string]any{"rev": maxRev}, "events", "changed")}
	}
	for _, id := range reauth {
		alerts = append(alerts, alertMsg("reauth", map[string]any{"accountId": id}, "accounts", "reauth-"+id))
	}
	if silent && maxRev > cur {
		if _, err := d.deliver(ctx, silentMsg(maxRev)); err != nil {
			return err
		}
	}
	for _, a := range alerts {
		if _, err := d.deliver(ctx, a); err != nil {
			return err
		}
	}
	if _, err := d.DB.W.ExecContext(ctx, `DELETE FROM push_state WHERE start_utc < ?`, now.Add(-24*time.Hour).Unix()); err != nil {
		return err
	}
	return d.setCursor(ctx, maxRev)
}
