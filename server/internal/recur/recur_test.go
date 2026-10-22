package recur

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"calendar/server/internal/db"
)

type fixture struct {
	Name   string   `json:"name"`
	Window []string `json:"window"`
	Master struct {
		AllDay      bool     `json:"allDay"`
		Start       string   `json:"start"`
		TZID        string   `json:"tzid"`
		DurationMin int      `json:"durationMin"`
		StartDate   string   `json:"startDate"`
		EndDate     string   `json:"endDate"`
		RRule       string   `json:"rrule"`
		RDates      []string `json:"rdates"`
		EXDates     []string `json:"exdates"`
	} `json:"master"`
	Exceptions []struct {
		Orig      string `json:"orig"`
		Cancelled bool   `json:"cancelled"`
		Start     string `json:"start"`
		End       string `json:"end"`
	} `json:"exceptions"`
	Expected []struct {
		Start     string `json:"start"`
		End       string `json:"end"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
	} `json:"expected"`
}

func mustT(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func openDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	_, err = d.W.Exec(`INSERT INTO accounts(id,provider,external_id,email,color,status,secret,created_at) VALUES ('a','google','x','e@x','cobalt','ok',x'00',0);
INSERT INTO calendars(id,account_id,remote_id,name,access) VALUES ('c','a','r','n','owner')`)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

type occRow struct {
	ID         string
	Rkey, S, E int64
	SD, ED     string
}

func readOccs(t *testing.T, d *db.DB) []occRow {
	t.Helper()
	rows, err := d.R.Query(`SELECT event_id, rkey, start_utc, end_utc, COALESCE(start_date,''), COALESCE(end_date,'') FROM occurrences ORDER BY start_utc, rkey`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []occRow
	for rows.Next() {
		var r occRow
		if err := rows.Scan(&r.ID, &r.Rkey, &r.S, &r.E, &r.SD, &r.ED); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestGolden(t *testing.T) {
	files, _ := filepath.Glob("../../testdata/recurrence/*.json")
	if len(files) < 20 {
		t.Fatalf("expected 20+ fixtures, got %d", len(files))
	}
	ctx := context.Background()
	for _, f := range files {
		b, _ := os.ReadFile(f)
		var fx fixture
		if err := json.Unmarshal(b, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		t.Run(fx.Name, func(t *testing.T) {
			d := openDB(t)
			win := Window{mustT(t, fx.Window[0]), mustT(t, fx.Window[1])}
			m := &Materialiser{win: &win}
			mm := fx.Master
			var su, eu any
			var sd, ed any
			tzid := mm.TZID
			if mm.AllDay {
				sd, ed = mm.StartDate, mm.EndDate
			} else {
				loc, _ := time.LoadLocation(mm.TZID)
				st, err := time.ParseInLocation("2006-01-02T15:04:05", mm.Start, loc)
				if err != nil {
					t.Fatal(err)
				}
				su, eu = st.Unix(), st.Add(time.Duration(mm.DurationMin)*time.Minute).Unix()
			}
			rd, _ := json.Marshal(mm.RDates)
			xd, _ := json.Marshal(mm.EXDates)
			if _, err := d.W.Exec(`INSERT INTO events(id,calendar_id,remote_id,kind,all_day,start_utc,end_utc,tzid,start_date,end_date,rrule,rdates,exdates,rev) VALUES ('m','c','m','master',?,?,?,?,?,?,?,?,?,1)`,
				mm.AllDay, su, eu, tzid, sd, ed, mm.RRule, string(rd), string(xd)); err != nil {
				t.Fatal(err)
			}
			for i, x := range fx.Exceptions {
				var key int64
				var cancelled = "confirmed"
				if x.Cancelled {
					cancelled = "cancelled"
				}
				var xs, xe, xsd, xed any
				if mm.AllDay {
					dt, _ := time.Parse(time.DateOnly, x.Orig)
					key = dt.Unix()
					if !x.Cancelled {
						xsd, xed = x.Start, x.End
					}
				} else {
					key = mustT(t, x.Orig).Unix()
					if !x.Cancelled {
						xs, xe = mustT(t, x.Start).Unix(), mustT(t, x.End).Unix()
					}
				}
				var del any
				if x.Cancelled {
					del = 1
				}
				if _, err := d.W.Exec(`INSERT INTO events(id,calendar_id,remote_id,kind,series_id,recurrence_key,all_day,start_utc,end_utc,start_date,end_date,status,deleted_at,rev) VALUES (?,'c',?,'exception','m',?,?,?,?,?,?,?,?,1)`,
					fmt.Sprintf("x%d", i), fmt.Sprintf("x%d", i), key, mm.AllDay, xs, xe, xsd, xed, cancelled, del); err != nil {
					t.Fatal(err)
				}
			}
			// rebuild twice: must be idempotent
			for i := 0; i < 2; i++ {
				if err := d.Write(ctx, func(tx *db.Tx) error { return m.Rebuild(ctx, tx, []string{"m"}) }); err != nil {
					t.Fatal(err)
				}
			}
			got := readOccs(t, d)
			if len(got) != len(fx.Expected) {
				t.Fatalf("got %d occurrences %+v, want %d", len(got), got, len(fx.Expected))
			}
			for i, w := range fx.Expected {
				g := got[i]
				if mm.AllDay {
					if g.SD != w.StartDate || g.ED != w.EndDate {
						t.Errorf("[%d] got %s..%s want %s..%s", i, g.SD, g.ED, w.StartDate, w.EndDate)
					}
					continue
				}
				if g.S != mustT(t, w.Start).Unix() || g.E != mustT(t, w.End).Unix() {
					t.Errorf("[%d] got %s..%s want %s..%s", i, time.Unix(g.S, 0).UTC().Format(time.RFC3339), time.Unix(g.E, 0).UTC().Format(time.RFC3339), w.Start, w.End)
				}
			}
		})
	}
}

func TestPropertySortedUniqueWithinWindow(t *testing.T) {
	win := Window{time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC), time.Date(2027, 11, 15, 0, 0, 0, 0, time.UTC)}
	rules := []string{"FREQ=DAILY", "FREQ=DAILY;INTERVAL=3", "FREQ=WEEKLY;BYDAY=MO,TU,TH", "FREQ=MONTHLY;BYMONTHDAY=-1", "FREQ=MONTHLY;BYDAY=2TU",
		"FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=29", "FREQ=HOURLY;INTERVAL=5;COUNT=500", "FREQ=WEEKLY;COUNT=7;BYDAY=SA,SU"}
	zones := []string{"UTC", "Europe/London", "America/New_York", "Pacific/Auckland", "Asia/Kolkata"}
	for _, r := range rules {
		for _, z := range zones {
			for _, allDay := range []bool{false, true} {
				if allDay && strings.HasPrefix(r, "FREQ=HOURLY") {
					continue
				}
				s := Series{AllDay: allDay, TZID: z, RRule: r, Start: time.Date(2025, 3, 30, 1, 30, 0, 0, time.UTC), End: time.Date(2025, 3, 30, 2, 30, 0, 0, time.UTC),
					StartDate: "2025-03-30", EndDate: "2025-03-31",
					EXDates: []string{"EXDATE:20261003T013000Z"}}
				out, _ := Expand(s, win, 5000)
				if !sort.SliceIsSorted(out, func(i, j int) bool { return out[i].Key < out[j].Key }) {
					t.Fatalf("%s %s: unsorted", r, z)
				}
				seen := map[int64]bool{}
				for _, o := range out {
					if seen[o.Key] {
						t.Fatalf("%s %s: duplicate key %d", r, z, o.Key)
					}
					seen[o.Key] = true
					st, en := o.Start, o.End
					if allDay {
						st, _ = time.Parse(time.DateOnly, o.StartDate)
						en, _ = time.Parse(time.DateOnly, o.EndDate)
					}
					if !st.Before(win.To) || !en.After(win.From) {
						t.Fatalf("%s %s: %v outside window", r, z, st)
					}
				}
				if len(out) > 5000 {
					t.Fatal("cap")
				}
			}
		}
	}
}

func TestCapPerSeries(t *testing.T) {
	win := Window{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC)}
	out, _ := Expand(Series{TZID: "UTC", RRule: "FREQ=HOURLY", Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)}, win, 5000)
	if len(out) != 5000 {
		t.Fatal(len(out))
	}
}

func TestRebuildLifecycle(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	m := &Materialiser{Now: func() time.Time { return now }}
	mustExec := func(q string, a ...any) {
		t.Helper()
		if _, err := d.W.Exec(q, a...); err != nil {
			t.Fatal(err)
		}
	}
	rb := func(ids ...string) {
		t.Helper()
		if err := d.Write(ctx, func(tx *db.Tx) error { return m.Rebuild(ctx, tx, ids) }); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int { return len(readOccs(t, d)) }
	// single timed + single all-day (tz Europe/London default: 2026-10-14 00:00 BST = 2026-10-13T23:00Z)
	mustExec(`INSERT INTO events(id,calendar_id,remote_id,kind,start_utc,end_utc,tzid,rev) VALUES ('s','c','s','single',1791792000,1791795600,'UTC',1)`)
	mustExec(`INSERT INTO events(id,calendar_id,remote_id,kind,all_day,start_date,end_date,rev) VALUES ('ad','c','ad','single',1,'2026-10-14','2026-10-15',1)`)
	// master: daily 09:00Z with COUNT=5 from 2026-10-12 -> 1791795600 is 2026-10-12T09:00Z? use explicit
	mustExec(`INSERT INTO events(id,calendar_id,remote_id,kind,start_utc,end_utc,tzid,rrule,rev) VALUES ('m','c','m','master',1791795600,1791799200,'UTC','FREQ=DAILY;COUNT=5',1)`)
	rb("s", "ad", "m")
	if n := count(); n != 7 {
		t.Fatalf("got %d", n)
	}
	var s int64
	if err := d.R.QueryRow(`SELECT start_utc FROM occurrences WHERE event_id='ad'`).Scan(&s); err != nil || s != time.Date(2026, 10, 13, 23, 0, 0, 0, time.UTC).Unix() {
		t.Fatal(s, err)
	}
	// user tz setting changes all-day ordering instants
	mustExec(`INSERT INTO settings(key,value) VALUES ('user_tz','"America/New_York"')`)
	rb("ad")
	if err := d.R.QueryRow(`SELECT start_utc FROM occurrences WHERE event_id='ad'`).Scan(&s); err != nil || s != time.Date(2026, 10, 14, 4, 0, 0, 0, time.UTC).Unix() {
		t.Fatal(s, err)
	}
	// tombstoned exception excludes the occurrence; rebuilding via the exception id rebuilds the series
	mustExec(`INSERT INTO events(id,calendar_id,remote_id,kind,series_id,recurrence_key,status,rev,deleted_at) VALUES ('x','c','x','exception','m',1791795600+86400,'cancelled',1,5)`)
	rb("x")
	if n := count(); n != 6 {
		t.Fatalf("after cancel %d", n)
	}
	// deleted master removes series
	mustExec(`UPDATE events SET deleted_at=9 WHERE id='m'`)
	rb("m")
	if n := count(); n != 2 {
		t.Fatalf("after delete %d", n)
	}
	// ExtendWindow: a clock move materialises more of an infinite series
	mustExec(`UPDATE events SET deleted_at=NULL, rrule='FREQ=DAILY' WHERE id='m'`)
	rb("m")
	m.Now = func() time.Time { return now.AddDate(0, 0, 60) }
	if err := m.ExtendWindow(ctx, d); err != nil {
		t.Fatal(err)
	}
	var maxS int64
	d.R.QueryRow(`SELECT MAX(start_utc) FROM occurrences WHERE event_id='m'`).Scan(&maxS)
	if maxS < now.AddDate(0, 0, 450).Unix() {
		t.Fatalf("window did not move: %v", time.Unix(maxS, 0))
	}
}
