// Package events is the write service (ENGINEERING 6.4): provider first, mirror second, under the
// per-account lock shared with sync. It knows nothing about HTTP; internal/api maps its errors.
package events

import (
	"errors"
	"time"

	"calendar/server/internal/agenda"
	"calendar/server/internal/provider"
)

type Kind int

const (
	KValidation   Kind = iota + 1 // 400
	KNotFound                     // 404
	KForbidden                    // 403
	KPrecondition                 // 412 If-Match no longer matches the mirror
	KConflict                     // 409 the provider's copy changed elsewhere
	KProvider                     // 502
	KNeedsReauth                  // account needs sign-in again
	KRateLimited                  // 429
	KUnavailable                  // 503 no provider for the account
)

// Error is what every service method returns on failure.
type Error struct {
	Kind       Kind
	Msg, Field string
	// Current is the latest state of the event for KPrecondition/KConflict (nil when it is gone).
	Current    *agenda.Event
	RetryAfter time.Duration
}

func (e *Error) Error() string { return e.Msg }

func fail(k Kind, field, msg string) *Error { return &Error{Kind: k, Field: field, Msg: msg} }

// AsError returns err as *Error, if it is one.
func AsError(err error) (*Error, bool) {
	var e *Error
	return e, errors.As(err, &e)
}

// providerErr maps a provider failure to a service error.
func providerErr(err error) *Error {
	var rl provider.ErrRateLimited
	switch {
	case errors.Is(err, provider.ErrNeedsReauth):
		return fail(KNeedsReauth, "", "this account needs to sign in again")
	case errors.As(err, &rl):
		e := fail(KRateLimited, "", "the calendar provider is rate limiting requests; try again shortly")
		e.RetryAfter = rl.RetryAfter
		return e
	case errors.Is(err, provider.ErrForbiddenAdmin):
		return fail(KForbidden, "", "blocked by your administrator's policy")
	case errors.Is(err, provider.ErrNotSupported):
		return fail(KValidation, "", "not supported: "+short(err.Error()))
	}
	return fail(KProvider, "", "the calendar provider rejected the change: "+short(err.Error()))
}

func short(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
