package telemetry

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// reqState travels in the request context for the life of one request.
type reqState struct {
	route atomic.Pointer[string] // mux pattern without the method, e.g. /v1/events/{id}
}

type reqStateKey struct{}

// RouteFromContext returns the route pattern of the current request ("" before routing). Use it in logs
// instead of the raw path.
func RouteFromContext(ctx context.Context) string {
	if st, ok := ctx.Value(reqStateKey{}).(*reqState); ok {
		if r := st.route.Load(); r != nil {
			return *r
		}
	}
	return ""
}

// Route wraps the ServeMux (the innermost handler). After the mux has routed the request it records the
// registered pattern, which is the only thing allowed to name spans and label metrics. Safe when telemetry
// is disabled (a few nanoseconds).
func Route(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if st, ok := r.Context().Value(reqStateKey{}).(*reqState); ok {
				route := routeOf(r.Pattern)
				st.route.Store(&route)
			}
		}()
		mux.ServeHTTP(w, r)
	})
}

// routeOf turns "GET /v1/events/{id}" into "/v1/events/{id}"; the catch-all "/" becomes "unmatched" so
// scanners hitting random paths collapse into one series.
func routeOf(pattern string) string {
	if i := strings.IndexByte(pattern, ' '); i >= 0 {
		pattern = pattern[i+1:]
	}
	if pattern == "" || pattern == "/" {
		return "unmatched"
	}
	return pattern
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusRecorder) WriteHeader(c int) {
	if !w.wrote {
		w.status, w.wrote = c, true
	}
	w.ResponseWriter.WriteHeader(c)
}
func (w *statusRecorder) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}
func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func statusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	case code >= 200:
		return "2xx"
	}
	return "1xx"
}

// longLived reports requests that stay open for minutes or hours. They get no span for their lifetime
// and no sample in the request-duration histogram (it would sit in the top bucket and hide real latency).
func longLived(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return r.URL.Path == "/v1/stream" || r.URL.Path == "/mcp"
}

// HTTP wraps the whole handler chain. Per request it provides: a server span named by the route pattern
// (inbound traceparent continued), standard http.server.* duration / in-flight / size metrics labelled
// with http.route, calendar.http.in_flight, a calendar.http.requests counter by route/method/status class, 5xx spans marked as
// errors (4xx are not errors) with the failure counted in calendar.errors, and the request state used by
// RecordError. Build it once, after Setup (it binds to the active providers).
func HTTP(next http.Handler) http.Handler {
	requests := Counter("http.requests", "HTTP requests handled, by route, method and status class", "{request}")
	inflight := UpDownCounter("http.in_flight", "Short-lived HTTP requests currently being handled", "{request}")
	active := UpDownCounter("http.long_lived.active", "Open long-lived connections (SSE, MCP stream), by route", "{connection}")
	lifetime := Histogram("http.long_lived.duration", "How long long-lived connections stay open, by route", "s",
		1, 10, 60, 300, 900, 3600, 4*3600, 12*3600, 24*3600)

	core := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, errs := withErrState(r.Context())
		ctx = context.WithValue(ctx, reqStateKey{}, &reqState{})
		r = r.WithContext(ctx)
		sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		inflight.Add(ctx, 1)
		func() {
			defer inflight.Add(ctx, -1)
			next.ServeHTTP(sw, r)
		}()
		route := RouteFromContext(ctx)
		if route == "" {
			route = "unmatched"
		}
		span := trace.SpanFromContext(ctx)
		span.SetName(r.Method + " " + route)
		span.SetAttributes(KeyRoute.String(route))
		if lb, ok := otelhttp.LabelerFromContext(ctx); ok {
			lb.Add(KeyRoute.String(route))
		}
		requests.Add(ctx, 1, metric.WithAttributes(KeyRoute.String(route), attribute.String("http.request.method", r.Method),
			KeyStatusClass.String(statusClass(sw.status))))
		if sw.status >= 500 && !errs.recorded.Load() {
			// a handler that wrote a 5xx without calling RecordError (unclassified failure)
			addErrorTelemetry(ctx, "http", KindInternal, "http.status", "HTTP "+strconv.Itoa(sw.status), "", nil)
		}
	})
	traced := otelhttp.NewHandler(core, "calendard",
		otelhttp.WithPropagators(propagation.TraceContext{}),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			if r.Pattern != "" {
				return r.Pattern
			}
			return r.Method // placeholder; replaced by "METHOD route" once routed
		}),
		otelhttp.WithTracerProvider(otel.GetTracerProvider()),
		otelhttp.WithMeterProvider(otel.GetMeterProvider()),
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !longLived(r) {
			traced.ServeHTTP(w, r)
			return
		}
		serveLongLived(w, r, next, requests, active, lifetime)
	})
}

func serveLongLived(w http.ResponseWriter, r *http.Request, next http.Handler, requests metric.Int64Counter, active metric.Int64UpDownCounter, life metric.Float64Histogram) {
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	ctx, errs := withErrState(ctx)
	ctx = context.WithValue(ctx, reqStateKey{}, &reqState{})
	route := r.URL.Path // only two fixed paths reach here
	// short "connect" span: ended right away so it exports now and nothing stays open for hours
	cctx, span := Tracer(instrumentationName).Start(ctx, r.Method+" "+route, trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(KeyRoute.String(route), attribute.String("http.request.method", r.Method), attribute.Bool("calendar.long_lived", true)))
	span.End()
	attr := metric.WithAttributes(KeyRoute.String(route))
	active.Add(ctx, 1, attr)
	start := time.Now()
	sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	defer func() {
		active.Add(ctx, -1, attr)
		life.Record(ctx, time.Since(start).Seconds(), attr)
		requests.Add(ctx, 1, metric.WithAttributes(KeyRoute.String(route), attribute.String("http.request.method", r.Method),
			KeyStatusClass.String(statusClass(sw.status))))
		if sw.status >= 500 && !errs.recorded.Load() {
			addErrorTelemetry(ctx, "http", KindInternal, "http.status", "HTTP "+strconv.Itoa(sw.status), "", nil)
		}
	}()
	next.ServeHTTP(sw, r.WithContext(cctx))
}
