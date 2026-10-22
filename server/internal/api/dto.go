package api

import (
	"time"

	"calendar/server/internal/agenda"
	"calendar/server/internal/hubmcp"
)

// Wire types for /v1, hand-written to match api/openapi.yaml (DECISIONS D2).
// Optional fields use pointers or omitempty; nullable fields marshal as null.

type Hello struct {
	Server     string `json:"server"`
	Version    string `json:"version"`
	APIVersion int    `json:"apiVersion"`
	Scope      string `json:"scope"`
	TokenID    string `json:"tokenId"`
}

// Read-model types live in internal/agenda (no HTTP dependency); aliased here so the wire
// vocabulary stays in one place for the fixtures hub and tests.
type (
	Join          = agenda.Join
	AgendaItem    = agenda.AgendaItem
	Attendee      = agenda.Attendee
	Person        = agenda.Person
	Recurrence    = agenda.Recurrence
	Event         = agenda.Event
	EventPage     = agenda.EventPage
	Calendar      = agenda.Calendar
	WorkingHours  = agenda.WorkingHours
	AlertDefaults = agenda.AlertDefaults
	Settings      = agenda.Settings
	FreeBusy      = agenda.FreeBusy
)

type AgendaResponse struct {
	Rev   int64        `json:"rev"`
	From  time.Time    `json:"from"`
	To    time.Time    `json:"to"`
	Items []AgendaItem `json:"items"`
}

type CalendarList struct {
	Items []Calendar `json:"items"`
}

type Account struct {
	ID           string     `json:"id"`
	Provider     string     `json:"provider"`
	Email        string     `json:"email"`
	DisplayName  string     `json:"displayName"`
	Color        string     `json:"color"`
	Status       string     `json:"status"`
	StatusDetail string     `json:"statusDetail,omitempty"`
	LastSyncAt   *time.Time `json:"lastSyncAt"`
	LastOKAt     *time.Time `json:"lastOkAt"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type AccountList struct {
	Items []Account `json:"items"`
}

type AccountSyncStatus struct {
	AccountID  string     `json:"accountId"`
	Email      string     `json:"email"`
	Status     string     `json:"status"`
	LastSyncAt *time.Time `json:"lastSyncAt"`
	LastOKAt   *time.Time `json:"lastOkAt"`
	NextSyncAt *time.Time `json:"nextSyncAt"`
	LastError  *string    `json:"lastError"`
	FailCount  int        `json:"failCount"`
}

type Warning struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type Status struct {
	HubVersion string              `json:"hubVersion"`
	Rev        int64               `json:"rev"`
	DBSize     int64               `json:"dbSizeBytes"`
	SSEClients int                 `json:"sseClients"`
	Accounts   []AccountSyncStatus `json:"accounts"`
	Warnings   []Warning           `json:"warnings"`
	MCP        *hubmcp.Stats       `json:"mcp,omitempty"`
}

type SyncRun struct {
	ID         int64     `json:"id"`
	AccountID  string    `json:"accountId"`
	CalendarID *string   `json:"calendarId"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
	Trigger    string    `json:"trigger"`
	Added      int       `json:"added"`
	Changed    int       `json:"changed"`
	Removed    int       `json:"removed"`
	ErrorKind  *string   `json:"errorKind"`
	Error      *string   `json:"error"`
}

// SSE payloads (event names: rev, account, sync, ping).
type SseRev struct {
	Rev int64 `json:"rev"`
}
type SseAccount struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
type SseSync struct {
	AccountID string `json:"accountId"`
	State     string `json:"state"`
}
