package gtest

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// RefreshToken and Subject are the fake's identity contract: the refresh token of an account is
// derived from its email so "calendard dev seed-account" can create one with no browser consent.
func RefreshToken(email string) string { return "fake-refresh-" + email }
func Subject(email string) string      { return "fake-sub-" + email }

// Multi is a stand-alone fake Google serving several accounts on one port: OAuth auth/token,
// userinfo, one Calendar v3 per account (routed by the access token) and /_admin/* controls.
type Multi struct {
	Emails []string
	Now    func() time.Time

	mu    sync.RWMutex
	fakes map[string]*Fake
}

func NewMulti(emails ...string) *Multi {
	m := &Multi{Emails: emails}
	m.Reset()
	return m
}

func (m *Multi) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Reset restores the seeded state (clears revocations, failures and every change).
func (m *Multi) Reset() {
	fk := map[string]*Fake{}
	for i, e := range m.Emails {
		f := NewStandalone(e)
		Seed(f, i, m.now())
		fk[e] = f
	}
	m.mu.Lock()
	m.fakes = fk
	m.mu.Unlock()
}

func (m *Multi) fake(email string) *Fake {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.fakes[email]
}

func (m *Multi) targets(email string) []*Fake {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if email != "" {
		if f := m.fakes[email]; f != nil {
			return []*Fake{f}
		}
		return nil
	}
	var out []*Fake
	for _, e := range m.Emails {
		out = append(out, m.fakes[e])
	}
	return out
}

func emailOfToken(tok string) string {
	tok = strings.TrimPrefix(tok, "at-")
	return strings.TrimPrefix(tok, "fake-refresh-")
}

func (m *Multi) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"status": "ok"}) })
	mux.HandleFunc("GET /auth", m.auth)
	mux.HandleFunc("POST /token", m.token)
	mux.HandleFunc("GET /userinfo", m.userinfo)
	m.adminRoutes(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f := m.fake(emailOfToken(bearer))
		if bearer == r.Header.Get("Authorization") || f == nil {
			writeJSON(w, 401, map[string]any{"error": map[string]any{"code": 401}})
			return
		}
		f.Handler().ServeHTTP(w, r)
	})
	return mux
}

// auth stands in for Google's consent page: it approves at once and redirects back.
func (m *Multi) auth(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	email := q.Get("login_hint")
	if m.fake(email) == nil {
		email = m.Emails[0]
	}
	ru, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || ru.Scheme == "" {
		http.Error(w, "redirect_uri required", 400)
		return
	}
	v := ru.Query()
	v.Set("code", "fake-code-"+email)
	v.Set("state", q.Get("state"))
	ru.RawQuery = v.Encode()
	http.Redirect(w, r, ru.String(), http.StatusFound)
}

func (m *Multi) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	invalid := func() { writeJSON(w, 400, map[string]any{"error": "invalid_grant"}) }
	switch r.Form.Get("grant_type") {
	case "refresh_token":
		rt := r.Form.Get("refresh_token")
		f := m.fake(emailOfToken(rt))
		if f == nil || !strings.HasPrefix(rt, "fake-refresh-") {
			invalid()
			return
		}
		f.mu.Lock()
		rev := f.revoked[rt]
		f.mu.Unlock()
		if rev {
			invalid()
			return
		}
		writeJSON(w, 200, map[string]any{"access_token": "at-" + rt, "token_type": "Bearer", "expires_in": 3600})
	case "authorization_code":
		code := r.Form.Get("code")
		email := strings.TrimPrefix(code, "fake-code-")
		if !strings.HasPrefix(code, "fake-code-") || m.fake(email) == nil {
			invalid()
			return
		}
		b64 := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
		idt := b64(map[string]any{"alg": "none"}) + "." + b64(map[string]any{"sub": Subject(email), "email": email, "name": strings.Title(strings.Split(email, "@")[0])}) + ".x"
		rt := RefreshToken(email)
		writeJSON(w, 200, map[string]any{"access_token": "at-" + rt, "refresh_token": rt, "token_type": "Bearer", "expires_in": 3600,
			"scope": "https://www.googleapis.com/auth/calendar openid email profile", "id_token": idt})
	default:
		writeJSON(w, 400, map[string]any{"error": "unsupported_grant_type"})
	}
}

func (m *Multi) userinfo(w http.ResponseWriter, r *http.Request) {
	email := emailOfToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if m.fake(email) == nil {
		writeJSON(w, 401, map[string]any{"error": "unauthorized"})
		return
	}
	writeJSON(w, 200, map[string]any{"sub": Subject(email), "email": email, "name": strings.Title(strings.Split(email, "@")[0])})
}

// ---- admin ----

type adminReq struct {
	Account    string         `json:"account"`
	Cal        string         `json:"cal"`
	ID         string         `json:"id"`
	Event      map[string]any `json:"event"`
	Delete     bool           `json:"delete"`
	Summary    string         `json:"summary"`
	Method     string         `json:"method"`
	Path       string         `json:"path"`
	Code       int            `json:"code"`
	N          int            `json:"n"`
	RetryAfter string         `json:"retryAfter"`
}

func (m *Multi) adminRoutes(mux *http.ServeMux) {
	post := func(path string, h func(a adminReq, fs []*Fake) (any, int)) {
		mux.HandleFunc("POST /_admin/"+path, func(w http.ResponseWriter, r *http.Request) {
			var a adminReq
			if b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20)); len(b) > 0 {
				if err := json.Unmarshal(b, &a); err != nil {
					writeJSON(w, 400, map[string]any{"error": err.Error()})
					return
				}
			}
			fs := m.targets(a.Account)
			if len(fs) == 0 {
				writeJSON(w, 404, map[string]any{"error": "unknown account " + a.Account})
				return
			}
			out, code := h(a, fs)
			writeJSON(w, code, out)
		})
	}
	// fail: the next N requests (any account unless given) matching method+path substring answer code.
	post("fail", func(a adminReq, fs []*Fake) (any, int) {
		if a.Code == 0 {
			a.Code = 500
		}
		if a.N == 0 {
			a.N = 1
		}
		for _, f := range fs {
			f.FailNext(a.Method, a.Path, a.Code, a.N, a.RetryAfter)
		}
		return map[string]any{"ok": true}, 200
	})
	mux.HandleFunc("POST /_admin/reset", func(w http.ResponseWriter, r *http.Request) {
		m.Reset()
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	// seed: put (or with delete:true remove) one event as an outside change.
	post("seed", func(a adminReq, fs []*Fake) (any, int) {
		f := fs[0]
		if !hasCal(f, a.Cal) || a.ID == "" {
			return map[string]any{"error": "need account, cal and id of an existing calendar"}, 400
		}
		if a.Delete {
			f.Delete(a.Cal, a.ID)
		} else {
			if a.Event == nil {
				return map[string]any{"error": "event required"}, 400
			}
			f.Put(a.Cal, a.ID, a.Event)
		}
		return map[string]any{"ok": true}, 200
	})
	// etag-bump: an outside edit (new etag; summary changes if given), the "provider copy changed" case.
	post("etag-bump", func(a adminReq, fs []*Fake) (any, int) {
		f := fs[0]
		if !hasCal(f, a.Cal) || f.Get(a.Cal, a.ID) == nil {
			return map[string]any{"error": "no such event"}, 404
		}
		f.Edit(a.Cal, a.ID, func(raw map[string]any) {
			if a.Summary != "" {
				raw["summary"] = a.Summary
			}
			raw["updated"] = time.Now().UTC().Format(time.RFC3339)
		})
		return map[string]any{"etag": f.Get(a.Cal, a.ID)["etag"]}, 200
	})
	// revoke: the account's refresh token stops working (token endpoint invalid_grant, API 401).
	post("revoke", func(a adminReq, fs []*Fake) (any, int) {
		for _, f := range fs {
			f.Revoke(RefreshToken(f.Self))
		}
		return map[string]any{"ok": true}, 200
	})
	post("unrevoke", func(a adminReq, fs []*Fake) (any, int) {
		for _, f := range fs {
			f.mu.Lock()
			delete(f.revoked, RefreshToken(f.Self))
			f.mu.Unlock()
		}
		return map[string]any{"ok": true}, 200
	})
	// expire-sync-tokens: every syncToken issued so far answers 410.
	post("expire-sync-tokens", func(a adminReq, fs []*Fake) (any, int) {
		for _, f := range fs {
			f.InvalidateSyncTokens()
		}
		return map[string]any{"ok": true}, 200
	})
	// state: what each account's Google holds, plus the writes it received.
	mux.HandleFunc("GET /_admin/state", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{}
		for _, f := range m.targets(r.URL.Query().Get("account")) {
			cals := map[string]any{}
			for _, c := range f.CalIDs() {
				cals[c] = f.Dump(c)
			}
			out[f.Self] = map[string]any{"calendars": cals, "writes": f.Writes()}
		}
		writeJSON(w, 200, out)
	})
}

func hasCal(f *Fake, id string) bool {
	for _, c := range f.CalIDs() {
		if c == id {
			return true
		}
	}
	return false
}
