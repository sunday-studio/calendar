package sync

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"calendar/server/internal/provider"
	"calendar/server/internal/provider/ics"
)

const icsToken = "feedtoken-31337-xyz"

type icsFeed struct {
	*httptest.Server
	mu       gosync.Mutex
	body     string
	etag     string
	status   int
	gets, nm int
}

func newICSFeed(t *testing.T, body string) *icsFeed {
	f := &icsFeed{body: body, etag: `"e1"`, status: 200}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.gets++
		if f.status != 200 {
			w.WriteHeader(f.status)
			return
		}
		if f.etag != "" && r.Header.Get("If-None-Match") == f.etag {
			f.nm++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if f.etag != "" {
			w.Header().Set("ETag", f.etag)
		}
		fmt.Fprint(w, f.body)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *icsFeed) set(body, etag string) { f.mu.Lock(); f.body, f.etag = body, etag; f.mu.Unlock() }
func (f *icsFeed) url() string           { return f.URL + "/" + icsToken + "/team.ics" }

func fixture(t *testing.T, name string) string {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ics", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func icsEnv(t *testing.T, logs *bytes.Buffer) *tenv {
	e := newTEnv(t)
	e.mgr.Log = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.mgr.Now = func() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) }
	p := ics.New(ics.Config{URL: e.acc.ICSURL, AllowPrivate: true})
	e.acc.ICS = p
	e.mgr.Providers["ics"] = p
	e.mgr.Interval = map[string]time.Duration{"ics": 15 * time.Minute}
	return e
}

func TestICSSyncReplaceAnd304(t *testing.T) {
	logs := &bytes.Buffer{}
	e := icsEnv(t, logs)
	feed := newICSFeed(t, fixture(t, "weekly.ics"))
	ctx := context.Background()

	acc, created, err := e.acc.CreateICS(ctx, feed.url(), "", "")
	if err != nil || !created {
		t.Fatal(err, created)
	}
	if acc.Provider != "ics" || acc.DisplayName != "Team Cal" || acc.Email != "127.0.0.1" || strings.Contains(acc.Email+acc.DisplayName, icsToken) {
		t.Fatalf("%+v", acc)
	}
	// the URL is sealed, never stored in the clear
	var secret []byte
	e.d.R.QueryRow(`SELECT secret FROM accounts WHERE id=?`, acc.ID).Scan(&secret)
	if bytes.Contains(secret, []byte(icsToken)) || len(secret) == 0 {
		t.Fatal("secret not sealed")
	}
	if got, err := e.acc.ICSURL(ctx, acc.ID); err != nil || got != feed.url() {
		t.Fatalf("unseal: %q %v", got, err)
	}
	// adding the same URL again is idempotent
	if again, created, err := e.acc.CreateICS(ctx, "webcal://"+strings.TrimPrefix(feed.url(), "http://"), "", "rose"); err == nil && !created && again.ID == acc.ID {
		t.Fatal("webcal maps to https, which is a different feed")
	}
	if again, created, err := e.acc.CreateICS(ctx, feed.url(), "", "rose"); err != nil || created || again.ID != acc.ID || again.Color != "rose" {
		t.Fatalf("dup: %v %v %+v", err, created, again)
	}
	if n := e.count(`SELECT COUNT(*) FROM accounts`); n != 1 {
		t.Fatal(n)
	}

	if err := e.sync(acc.ID); err != nil {
		t.Fatal(err)
	}
	if e.status(acc.ID) != "ok" {
		t.Fatal(e.status(acc.ID))
	}
	if n := e.count(`SELECT COUNT(*) FROM calendars WHERE account_id=? AND access='reader' AND visible=1 AND remote_id='feed'`, acc.ID); n != 1 {
		t.Fatal("calendar", n)
	}
	// master + moved override live; the cancelled override is a tombstoned marker row
	if n := e.count(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`); n != 2 {
		t.Fatal("events", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE deleted_at IS NOT NULL AND kind='exception'`); n != 1 {
		t.Fatal("cancelled marker", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE can_edit=1 OR can_rsvp=1 OR can_add_conf=1`); n != 0 {
		t.Fatal("caps must be false", n)
	}
	// occurrences: Mon 5, moved Wed 7 at 13:00Z (14:00 BST), Mon 12 excluded (EXDATE), Wed 14 excluded (cancelled)
	rows, _ := e.d.R.Query(`SELECT o.start_utc FROM occurrences o JOIN events ev ON ev.id=o.event_id WHERE ev.calendar_id IN (SELECT id FROM calendars WHERE account_id=?) ORDER BY 1 LIMIT 4`, acc.ID)
	var got []string
	for rows.Next() {
		var u int64
		rows.Scan(&u)
		got = append(got, time.Unix(u, 0).UTC().Format("01-02T15"))
	}
	rows.Close()
	if strings.Join(got, ",") != "10-05T09,10-07T13,10-19T09,10-21T09" {
		t.Fatalf("occurrences %v", got)
	}
	cur := e.str(`SELECT sync_cursor FROM calendars WHERE account_id=?`, acc.ID)
	if !strings.Contains(cur, "e1") {
		t.Fatal("cursor", cur)
	}
	rev := e.count(`SELECT CAST(value AS INTEGER) FROM meta WHERE key='rev'`)

	// 304: nothing changes, no new rev
	if err := e.sync(acc.ID); err != nil {
		t.Fatal(err)
	}
	if feed.nm != 1 || e.count(`SELECT CAST(value AS INTEGER) FROM meta WHERE key='rev'`) != rev {
		t.Fatalf("nm=%d rev changed", feed.nm)
	}

	// the feed changes: a new document with different events replaces the old (missing => tombstoned)
	feed.set(fixture(t, "allday.ics"), `"e2"`)
	if err := e.sync(acc.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL AND title='Offsite'`); n != 1 {
		t.Fatal("offsite", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL AND title LIKE 'Standup%'`); n != 0 {
		t.Fatal("old events must be tombstoned", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE deleted_at IS NOT NULL AND title='Standup'`); n < 1 {
		t.Fatal("tombstone", n)
	}

	// a feed that vanished (500) backs off and keeps the data; the account never needs reauth
	feed.mu.Lock()
	feed.status = 500
	feed.mu.Unlock()
	for i := 0; i < 3; i++ {
		if err := e.sync(acc.ID); err == nil || errIs(err, provider.ErrNeedsReauth) {
			t.Fatalf("want plain error, got %v", err)
		}
	}
	if st := e.status(acc.ID); st != "error" {
		t.Fatal(st)
	}
	if n := e.count(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL AND title='Offsite'`); n != 1 {
		t.Fatal("data must survive a failed fetch", n)
	}
	detail := e.str(`SELECT status_detail FROM accounts WHERE id=?`, acc.ID)
	runErr := e.str(`SELECT group_concat(COALESCE(error,'')) FROM sync_runs`)
	if !strings.Contains(detail, "HTTP 500") {
		t.Fatal(detail)
	}
	// recovery
	feed.mu.Lock()
	feed.status = 200
	feed.mu.Unlock()
	if err := e.sync(acc.ID); err != nil || e.status(acc.ID) != "ok" {
		t.Fatalf("%v %s", err, e.status(acc.ID))
	}
	// next poll is 15 minutes out
	if next := e.count(`SELECT next_sync_at FROM accounts WHERE id=?`, acc.ID); time.Unix(int64(next), 0).Sub(e.mgr.Now()) != 15*time.Minute {
		t.Fatal("cadence", time.Unix(int64(next), 0).Sub(e.mgr.Now()))
	}
	for name, s := range map[string]string{"status_detail": detail, "sync_runs": runErr, "logs": logs.String()} {
		if strings.Contains(s, icsToken) {
			t.Errorf("%s leaks the feed URL", name)
		}
	}
}

func errIs(err, target error) bool { return strings.Contains(fmt.Sprint(err), target.Error()) }

func TestICSInvalidFeedSavesNothing(t *testing.T) {
	e := icsEnv(t, &bytes.Buffer{})
	feed := newICSFeed(t, "<html>not a calendar</html>")
	_, _, err := e.acc.CreateICS(context.Background(), feed.url(), "x", "")
	if err == nil || strings.Contains(err.Error(), icsToken) {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM accounts`); n != 0 {
		t.Fatal(n)
	}
}
