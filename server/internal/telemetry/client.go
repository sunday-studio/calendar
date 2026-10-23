package telemetry

import (
	"context"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type providerKey struct{}
type origURLKey struct{}

// WithProvider names the provider for outbound calls made with ctx (calendar.provider on client spans and
// retry metrics). Without it the provider is inferred from the host (googleapis.com -> google).
func WithProvider(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, providerKey{}, word(name, "other"))
}

// ProviderFor is the provider name for an outbound request: context, else the host.
func ProviderFor(req *http.Request, fallback string) string {
	if p, ok := req.Context().Value(providerKey{}).(string); ok && p != "" {
		return p
	}
	if fallback != "" {
		return word(fallback, "other")
	}
	h := strings.ToLower(req.URL.Hostname())
	switch {
	case h == "googleapis.com" || strings.HasSuffix(h, ".googleapis.com") || h == "google.com" || strings.HasSuffix(h, ".google.com"):
		return "google"
	case h == "push.apple.com" || strings.HasSuffix(h, ".push.apple.com"):
		return "apns"
	}
	return "other"
}

// WrapClient returns rt wrapped with an OpenTelemetry client span per attempt (and http.client.* metrics).
// The span sees only scheme://host and the templated path ("/calendar/v3/calendars/:id/events"): never
// ids, query strings, userinfo or tokens; the real request still goes out unchanged. Outbound requests
// carry no traceparent header (provider hosts do not need our trace ids). provider is the default
// calendar.provider value ("" = infer per request).
func WrapClient(rt http.RoundTripper, provider string) http.RoundTripper {
	if rt == nil {
		rt = http.DefaultTransport
	}
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// restore the real URL; the instrumented copy only carried the templated one
		if orig, ok := req.Context().Value(origURLKey{}).(*url.URL); ok {
			r := req.Clone(req.Context())
			r.URL = orig
			return rt.RoundTrip(r)
		}
		return rt.RoundTrip(req)
	})
	traced := otelhttp.NewTransport(inner,
		otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator()),
		otelhttp.WithTracerProvider(otel.GetTracerProvider()),
		otelhttp.WithMeterProvider(otel.GetMeterProvider()),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return ProviderFor(r, provider) + " " + r.Method + " " + r.URL.Path
		}),
		otelhttp.WithClientTrace(func(ctx context.Context) *httptrace.ClientTrace {
			trace.SpanFromContext(ctx).SetAttributes(KeyProvider.String(providerFromCtx(ctx)), KeyComponent.String("provider.http"))
			return &httptrace.ClientTrace{}
		}),
	)
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !Enabled() {
			return rt.RoundTrip(req)
		}
		p := ProviderFor(req, provider)
		safe := *req.URL
		safe.User, safe.RawQuery, safe.Fragment, safe.RawPath, safe.ForceQuery = nil, "", "", "", false
		safe.Path = TemplatePath(req.URL.Path)
		ctx := context.WithValue(req.Context(), origURLKey{}, req.URL)
		ctx = context.WithValue(ctx, providerKey{}, p)
		lb := &otelhttp.Labeler{}
		lb.Add(KeyProvider.String(p))
		ctx = otelhttp.ContextWithClientLabeler(ctx, lb)
		sreq := req.Clone(ctx)
		sreq.URL = &safe
		resp, err := traced.RoundTrip(sreq)
		if resp != nil {
			resp.Request = req
		}
		return resp, err
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func providerFromCtx(ctx context.Context) string {
	p, _ := ctx.Value(providerKey{}).(string)
	return p
}
