package fixtures

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"calendar/server/internal/agenda"
	"calendar/server/internal/api"
	"calendar/server/internal/quickadd"
	"calendar/server/internal/tz"
)

// fxEnv answers quick-add lookups from the fixture world.
type fxEnv struct{ d *Data }

func (e fxEnv) Contacts(_ context.Context, name string) ([]quickadd.Contact, error) {
	e.d.st.mu.RLock()
	defer e.d.st.mu.RUnlock()
	n := strings.ToLower(strings.TrimSpace(name))
	seen := map[string]bool{}
	var out []quickadd.Contact
	for _, ev := range e.d.details {
		for _, a := range ev.Attendees {
			if a.IsSelf || seen[a.Email] {
				continue
			}
			lname := strings.ToLower(a.Name)
			if strings.HasPrefix(lname, n) || strings.Contains(lname, " "+n) || strings.HasPrefix(strings.ToLower(a.Email), n) {
				seen[a.Email] = true
				out = append(out, quickadd.Contact{Email: a.Email, Name: a.Name, Count: 1})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

func (e fxEnv) Calendars(context.Context) ([]quickadd.Calendar, error) {
	var out []quickadd.Calendar
	for _, c := range e.d.Calendars {
		if c.Access == "owner" || c.Access == "writer" {
			out = append(out, quickadd.Calendar{ID: c.ID, Name: c.Name, IsDefault: e.d.Settings.DefaultCalendarID != nil && *e.d.Settings.DefaultCalendarID == c.ID})
		}
	}
	return out, nil
}

func (s *Server) extraRoutes(mux *http.ServeMux, auth func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /v1/sync/runs", auth(s.syncRuns))
	mux.Handle("POST /v1/sync", auth(func(w http.ResponseWriter, r *http.Request) {
		api.WriteJSON(w, http.StatusAccepted, map[string]any{"queued": len(s.Data.Accounts)})
	}))
	mux.Handle("GET /v1/tokens", auth(s.tokens))
	mux.Handle("POST /v1/tokens", auth(s.createToken))
	mux.Handle("DELETE /v1/tokens/{id}", auth(s.revokeToken))
	mux.Handle("GET /v1/summary", auth(s.summary))
	mux.Handle("POST /v1/freetime", auth(s.freeTime))
	mux.Handle("POST /v1/quick-add", auth(s.quickAdd))
	mux.Handle("GET /v1/people", auth(s.people))
	mux.Handle("GET /v1/events", auth(s.listEvents))
	mux.Handle("GET /v1/stream", auth(s.stream))
	mux.Handle("PUT /v1/devices/self/push", auth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	mux.Handle("DELETE /v1/devices/self/push", auth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
}

// ---- sync runs ----

func (s *Server) syncRuns(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			api.WriteError(w, 400, api.CodeValidation, "limit must be 1..200", "limit")
			return
		}
		limit = n
	}
	only := r.URL.Query().Get("accountId")
	items := []api.SyncRun{}
	id := int64(1000)
	now := s.Data.Now.Truncate(time.Second)
	for i, a := range s.Data.Accounts {
		for k := 0; k < 4; k++ {
			if only != "" && only != a.ID {
				continue
			}
			run := api.SyncRun{ID: id - int64(i*10+k), AccountID: a.ID, StartedAt: now.Add(-time.Duration(2+k*5+i) * time.Minute),
				DurationMs: int64(300 + 120*k + 50*i), Trigger: []string{"tick", "tick", "manual", "tick"}[k], Changed: k % 2, Added: 0}
			if a.Status == "needs_reauth" {
				kind, msg := "needs_reauth", "Google no longer accepts the saved sign-in."
				run.ErrorKind, run.Error = &kind, &msg
			}
			items = append(items, run)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].StartedAt.After(items[j].StartedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	api.WriteJSON(w, 200, map[string]any{"items": items})
}

// ---- tokens ----

type tokenJSON struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Scope      string     `json:"scope"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

func (s *Server) tokenList() []tokenJSON {
	n := s.Data.Now.Truncate(time.Second)
	used, old := n.Add(-3*24*time.Hour), n.Add(-30*24*time.Hour)
	return []tokenJSON{
		{ID: "01FIXTOKEN0000000000000001", Name: "iPhone", Kind: "device", Scope: "admin", Prefix: "cal_7QK2", CreatedAt: n.AddDate(0, -3, 0), LastUsedAt: &n},
		{ID: "01FIXTOKEN0000000000000002", Name: "Grafana", Kind: "script", Scope: "read", Prefix: "cal_M3PX", CreatedAt: n.AddDate(0, -1, 0), LastUsedAt: &used},
		{ID: "01FIXTOKEN0000000000000003", Name: "Old iPad", Kind: "device", Scope: "write", Prefix: "cal_B9ZD", CreatedAt: n.AddDate(0, -6, 0), LastUsedAt: &old, RevokedAt: &old},
	}
}

func (s *Server) tokens(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, 200, map[string]any{"items": s.tokenList()})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Kind, Scope string }
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if json.Unmarshal(b, &in) != nil || strings.TrimSpace(in.Name) == "" {
		api.WriteError(w, 400, api.CodeValidation, "name is required", "name")
		return
	}
	if in.Kind != "device" && in.Kind != "script" {
		api.WriteError(w, 400, api.CodeValidation, "kind must be device or script", "kind")
		return
	}
	if in.Scope != "read" && in.Scope != "write" && in.Scope != "admin" {
		api.WriteError(w, 400, api.CodeValidation, "scope must be read, write or admin", "scope")
		return
	}
	n := s.Data.Now.Truncate(time.Second)
	api.WriteJSON(w, 201, struct {
		tokenJSON
		Secret string `json:"secret"`
	}{tokenJSON{ID: "01FIXTOKEN0000000000000009", Name: strings.TrimSpace(in.Name), Kind: in.Kind, Scope: in.Scope, Prefix: "cal_FIXT", CreatedAt: n}, "cal_FIXTURESECRETFIXTURESECRETFIXTURESECRET0000"})
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "01FIXTOKEN0000000000000001" {
		api.WriteError(w, 400, api.CodeValidation, "you cannot revoke the token you are using", "id")
		return
	}
	for _, t := range s.tokenList() {
		if t.ID == id && t.RevokedAt == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	api.WriteError(w, 404, api.CodeNotFound, "no such active token", "")
}

// ---- summary, free time, quick add, people, search ----

func (s *Server) loc() *time.Location {
	l, _ := tz.Load(*s.Data.Settings.UserTZID)
	return l
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	loc := s.loc()
	date := r.URL.Query().Get("date")
	if date == "" {
		date = s.Data.Now.In(loc).Format(time.DateOnly)
	}
	day, err := time.ParseInLocation(time.DateOnly, date, loc)
	if err != nil {
		api.WriteError(w, 400, api.CodeValidation, "date must be YYYY-MM-DD", "date")
		return
	}
	items := s.Data.Agenda(day.UTC(), day.AddDate(0, 0, 1).UTC(), nil)
	api.WriteJSON(w, 200, agenda.SummariseDay(items, s.Data.Settings, loc, day))
}

func (s *Server) freeTime(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DurationMin        int    `json:"durationMin"`
		From, To           string `json:"-"`
		FromS              string `json:"from"`
		ToS                string `json:"to"`
		IgnoreWorkingHours bool   `json:"ignoreWorkingHours"`
		DayPart            string `json:"dayPart"`
		Limit              int    `json:"limit"`
	}
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err := json.Unmarshal(b, &in); err != nil {
		api.WriteError(w, 400, api.CodeValidation, "invalid JSON", "")
		return
	}
	from, err1 := time.Parse(time.RFC3339, in.FromS)
	to, err2 := time.Parse(time.RFC3339, in.ToS)
	if err1 != nil || err2 != nil {
		api.WriteError(w, 400, api.CodeValidation, "from and to must be RFC 3339 instants", "from")
		return
	}
	q := agenda.FreeTimeQuery{DurationMin: in.DurationMin, From: from, To: to, IgnoreWorkingHours: in.IgnoreWorkingHours, DayPart: in.DayPart, Limit: in.Limit}
	items := s.Data.Agenda(from.AddDate(0, 0, -1), to.AddDate(0, 0, 15), nil)
	ft, err := agenda.FreeTimeFromItems(q, items, s.Data.Settings, s.Data.Now)
	if err != nil {
		if ve, ok := err.(*agenda.ValidationError); ok {
			api.WriteError(w, 400, api.CodeValidation, ve.Msg, ve.Field)
			return
		}
		api.WriteError(w, 500, "internal", "internal error", "")
		return
	}
	api.WriteJSON(w, 200, ft)
}

func (s *Server) quickAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text, Now, TZID string
	}
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if json.Unmarshal(b, &in) != nil {
		api.WriteError(w, 400, api.CodeValidation, "invalid JSON", "")
		return
	}
	req := quickadd.Request{Text: in.Text, TZID: in.TZID}
	if in.Now != "" {
		t, err := time.Parse(time.RFC3339, in.Now)
		if err != nil {
			api.WriteError(w, 400, api.CodeValidation, "now must be an RFC 3339 instant", "now")
			return
		}
		req.Now = t
	}
	if req.TZID == "" {
		req.TZID = *s.Data.Settings.UserTZID
	}
	p := &quickadd.RuleParser{Env: fxEnv{s.Data}, DefaultNow: func() time.Time { return s.Data.Now }}
	d, err := p.Parse(r.Context(), req)
	if err != nil {
		api.WriteError(w, 400, api.CodeValidation, err.Error(), "text")
		return
	}
	api.WriteJSON(w, 200, d)
}

func (s *Server) people(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		api.WriteError(w, 400, api.CodeValidation, "q is required", "q")
		return
	}
	cs, _ := fxEnv{s.Data}.Contacts(r.Context(), q)
	type person struct {
		Email    string     `json:"email"`
		Name     string     `json:"name"`
		Events   int        `json:"events"`
		LastSeen *time.Time `json:"lastSeen"`
	}
	out := make([]person, 0, len(cs))
	seen := s.Data.Now.Add(-26 * time.Hour).Truncate(time.Second)
	for _, c := range cs {
		out = append(out, person{Email: c.Email, Name: c.Name, Events: c.Count + 4, LastSeen: &seen})
	}
	api.WriteJSON(w, 200, map[string]any{"items": out})
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := strings.ToLower(strings.TrimSpace(q.Get("q")))
	from, e1 := time.Parse(time.RFC3339, q.Get("from"))
	to, e2 := time.Parse(time.RFC3339, q.Get("to"))
	if search == "" && (e1 != nil || e2 != nil) {
		api.WriteError(w, 400, api.CodeValidation, "from and to are required unless q is set", "from")
		return
	}
	if search != "" {
		if e1 != nil {
			from = s.Data.Now.AddDate(0, 0, -400)
		}
		if e2 != nil {
			to = s.Data.Now.AddDate(0, 0, 400)
		}
	}
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			api.WriteError(w, 400, api.CodeValidation, "limit must be 1..200", "limit")
			return
		}
		limit = n
	}
	var cals map[string]bool
	if c := q.Get("calendar"); c != "" {
		cals = map[string]bool{c: true}
	}
	items := []api.Event{}
	seen := map[string]bool{}
	occs := s.Data.Agenda(from, to, cals)
	s.Data.st.mu.RLock()
	defer s.Data.st.mu.RUnlock()
	for _, it := range occs {
		if it.Status == "cancelled" {
			continue
		}
		ev := s.Data.details[it.ID]
		if search != "" {
			hay := strings.ToLower(ev.Title + " " + ev.Location + " " + ev.Description)
			for _, a := range ev.Attendees {
				hay += " " + strings.ToLower(a.Name+" "+a.Email)
			}
			ok := true
			for _, word := range strings.Fields(search) {
				if !strings.Contains(hay, word) {
					ok = false
				}
			}
			if !ok || seen[it.EventID] {
				continue
			}
			seen[it.EventID] = true
		}
		items = append(items, ev)
		if len(items) == limit {
			break
		}
	}
	api.WriteJSON(w, 200, api.EventPage{Items: items})
}

// ---- stream ----

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		api.WriteError(w, 500, "internal", "streaming unsupported", "")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	rev := s.Data.Rev()
	fmt.Fprintf(w, "id: %d\nevent: rev\ndata: {\"rev\":%d}\n\n", rev, rev)
	revs, unsub := s.ws.subscribe()
	defer unsub()
	for _, a := range s.Data.Accounts {
		if a.Status == "syncing" {
			fmt.Fprintf(w, "event: sync\ndata: {\"accountId\":%q,\"state\":\"started\"}\n\n", a.ID)
		}
		if a.Status == "needs_reauth" {
			fmt.Fprintf(w, "event: account\ndata: {\"id\":%q,\"status\":\"needs_reauth\"}\n\n", a.ID)
		}
	}
	fl.Flush()
	every := s.PingEvery
	if every <= 0 {
		every = 20 * time.Second
	}
	tk := time.NewTicker(every)
	defer tk.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case n := <-revs:
			fmt.Fprintf(w, "id: %d\nevent: rev\ndata: {\"rev\":%d}\n\n", n, n)
			fl.Flush()
		case <-tk.C:
			fmt.Fprint(w, "event: ping\ndata: {}\n\n")
			fl.Flush()
		}
	}
}
