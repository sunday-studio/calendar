// Package provider defines the contract every calendar backend implements (ENGINEERING §5.1).
package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"calendar/server/internal/domain"
)

type Kind string

const (
	KindGoogle Kind = "google"
	KindICS    Kind = "ics"
)

// Caps are static per-provider capabilities.
type Caps struct {
	CanWrite, CanRSVP, CanConference, HasPush bool
	SyncStyle                                 string // "token" | "delta" | "ctag" | "poll"
}

// Typed errors the sync engine understands (never string-matched).
var (
	ErrNeedsReauth        = errors.New("provider: credentials revoked or expired")
	ErrCursorExpired      = errors.New("provider: sync cursor expired")
	ErrPreconditionFailed = errors.New("provider: etag precondition failed")
	ErrForbiddenAdmin     = errors.New("provider: blocked by administrator policy")
	ErrNotSupported       = errors.New("provider: operation not supported")
	ErrNotFound           = errors.New("provider: remote event not found")
)

// ErrRateLimited asks the caller to wait. RetryAfter == 0 means "unspecified".
type ErrRateLimited struct{ RetryAfter time.Duration }

func (e ErrRateLimited) Error() string {
	return fmt.Sprintf("provider: rate limited (retry after %s)", e.RetryAfter)
}

// RemoteCalendar is one entry of a provider's calendar list.
type RemoteCalendar struct {
	RemoteID, Name, Color, TZID string
	Access                      string // owner|writer|reader|freebusy
	IsPrimary                   bool
	Visible                     bool // provider's hint, used only when first inserting
}

// SyncRequest is one step of a calendar sync. Cursor == "" means full sync.
type SyncRequest struct {
	Cursor    string
	PageToken string
}

// SyncBatch is the result of one step.
type SyncBatch struct {
	Events    []domain.Event // Status == Cancelled means tombstone
	Deleted   []string       // remote ids removed without further detail
	PageToken string         // non-empty: call again for the next page
	// Replace marks Events as the complete current content of the calendar (feeds without a delta
	// API): live events not listed are tombstoned when the final page is applied.
	Replace    bool
	NextCursor string // set on the final page only
}

// ---- write path (ENGINEERING 5.1, 6.4) ----

type Scope string

const (
	ScopeThis      Scope = "this"
	ScopeFollowing Scope = "following"
	ScopeAll       Scope = "all"
)

// Opt is a patch field: Set=false means untouched, Set with a nil Val means "clear".
type Opt[T any] struct {
	Set bool
	Val *T
}

func Some[T any](v T) Opt[T] { return Opt[T]{Set: true, Val: &v} }
func Clear[T any]() Opt[T]   { return Opt[T]{Set: true} }

// EventInput describes a new event. When is complete (UTC instants + tzid, or dates).
type EventInput struct {
	// ID is an optional client-chosen remote id. Providers that allow it use it so a retried
	// create is detected (duplicate id) instead of creating a second event.
	ID                           string
	Title, Description, Location string
	When                         domain.When
	Attendees                    []string
	Recurrence                   *domain.Recurrence
	Conference                   bool
	// Silent suppresses guest notifications (used when copying an event between accounts).
	Silent bool
}

// Patch is a partial edit; When, when set, is complete.
type Patch struct {
	Title       *string
	Description Opt[string]
	Location    Opt[string]
	When        *domain.When
	Attendees   Opt[[]string]
	Recurrence  Opt[domain.Recurrence]
	// Conference: Set+Val "meet" adds a Meet link, Set+nil removes the conference.
	Conference Opt[string]
}

// Ref addresses the remote event a write acts on.
type Ref struct {
	Acct  domain.Account
	Cal   domain.Calendar
	Event domain.Event // the row addressed: single, master or exception
	// Master is the series master when Event is an exception (nil otherwise; Event is then the master itself).
	Master *domain.Event
	// OccKey is the original start (unix s; all-day: UTC midnight) of the occurrence addressed,
	// for an occurrence of a master that has no exception row. nil otherwise.
	OccKey *int64
	// Exceptions are the live exception rows of the series (used to drop later overrides on a split).
	Exceptions []domain.Event
}

// ConflictError is returned (wrapping ErrPreconditionFailed) when the remote event changed since the
// mirror was written. Latest is the provider's current state of everything the write targeted;
// an event with Status Cancelled means it no longer exists.
type ConflictError struct{ Latest Result }

func (e *ConflictError) Error() string { return ErrPreconditionFailed.Error() }
func (e *ConflictError) Unwrap() error { return ErrPreconditionFailed }

// Result is what the provider confirmed. Events with Status Cancelled are tombstones.
type Result struct {
	Events  []domain.Event
	Deleted []string // remote ids removed without further detail
}

type Provider interface {
	Kind() Kind
	Caps() Caps
	Discover(ctx context.Context, acct domain.Account) ([]RemoteCalendar, error)
	Sync(ctx context.Context, acct domain.Account, cal domain.Calendar, req SyncRequest) (SyncBatch, error)
	Create(ctx context.Context, acct domain.Account, cal domain.Calendar, in EventInput) (Result, error)
	Update(ctx context.Context, ref Ref, p Patch, scope Scope) (Result, error)
	Delete(ctx context.Context, ref Ref, scope Scope) (Result, error)
	RSVP(ctx context.Context, ref Ref, r domain.Response, note string, scope Scope) (Result, error)
	AddConference(ctx context.Context, ref Ref) (Result, error)
	// Move changes the calendar within the same account.
	Move(ctx context.Context, ref Ref, to domain.Calendar) (Result, error)
}
