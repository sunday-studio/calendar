package fixtures

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"calendar/server/internal/accounts"
	"calendar/server/internal/api"
	"calendar/server/internal/provider/ics"
)

// icsAccount handles POST /v1/accounts/ics in fixtures mode: a valid URL adds a read-only ICS
// account (201); a URL containing "bad" or one that is not http(s)/webcal answers 422 field url.
// Nothing is fetched; the account has no calendar or events.
func (s *Server) icsAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL   string `json:"url"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		api.WriteError(w, 422, api.CodeValidation, "invalid JSON body", "")
		return
	}
	norm, err := ics.Normalize(body.URL)
	if err != nil || strings.Contains(strings.ToLower(body.URL), "bad") {
		msg := "We couldn't read a calendar from that link. Check it and try again."
		if err != nil {
			msg = err.Error()
		}
		api.WriteError(w, 422, api.CodeValidation, msg, "url")
		return
	}
	if body.Color != "" && !accounts.ValidColor(body.Color) {
		api.WriteError(w, 422, api.CodeValidation, "unknown colour", "color")
		return
	}
	host := strings.TrimPrefix(norm, "https://")
	host = strings.TrimPrefix(host, "http://")
	host, _, _ = strings.Cut(host, "/")
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = host
	}
	d := s.Data
	d.st.mu.Lock()
	for _, a := range d.Accounts {
		if a.Provider == "ics" && a.Email == host && a.DisplayName == name {
			d.st.mu.Unlock()
			api.WriteJSON(w, 200, a)
			return
		}
	}
	color := body.Color
	if color == "" {
		color = accounts.Palette[len(d.Accounts)%len(accounts.Palette)]
	}
	now := time.Now().UTC().Truncate(time.Second)
	d.st.seq++
	acc := api.Account{ID: fmt.Sprintf("01FIXICS%018d", d.st.seq), Provider: "ics", Email: host, DisplayName: name,
		Color: color, Status: "active", LastSyncAt: &now, LastOKAt: &now, CreatedAt: now}
	d.Accounts = append(append([]api.Account(nil), d.Accounts...), acc)
	d.st.mu.Unlock()
	api.WriteJSON(w, 201, acc)
}
