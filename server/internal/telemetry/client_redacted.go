package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

type redactedURLKey struct{}

// WrapClientRedacted is WrapClient for destinations whose path or query may embed a secret (ICS feed
// links, user-configured webhooks): client spans see only scheme://host plus the fixed pathLabel
// ("/feed", "/webhook"), never the real path, query or userinfo. The request itself is unchanged.
func WrapClientRedacted(rt http.RoundTripper, provider, pathLabel string) http.RoundTripper {
	if rt == nil {
		rt = http.DefaultTransport
	}
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if orig, ok := req.Context().Value(redactedURLKey{}).(*url.URL); ok {
			c := req.Clone(req.Context())
			c.URL = orig
			return rt.RoundTrip(c)
		}
		return rt.RoundTrip(req)
	})
	traced := WrapClient(inner, provider)
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		u := *req.URL
		u.Path, u.RawPath, u.RawQuery, u.User, u.Fragment = pathLabel, "", "", nil, ""
		r := req.Clone(context.WithValue(req.Context(), redactedURLKey{}, req.URL))
		r.URL = &u
		resp, err := traced.RoundTrip(r)
		if resp != nil {
			resp.Request = req
		}
		return resp, err
	})
}

// SafeError returns err without any request URL: a *url.Error (what net/http returns) embeds the full
// URL, which for webhooks and ICS feeds can contain a secret. Only the operation and the cause remain.
func SafeError(err error) error {
	var ue *url.Error
	if err == nil {
		return nil
	}
	if asURLError(err, &ue) {
		return &safeErr{op: ue.Op, cause: ue.Err}
	}
	return err
}

type safeErr struct {
	op    string
	cause error
}

func (e *safeErr) Error() string { return e.op + ": " + e.cause.Error() }
func (e *safeErr) Unwrap() error { return e.cause }

func asURLError(err error, target **url.Error) bool { return errors.As(err, target) }
