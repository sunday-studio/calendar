// Package telemetry is the hub's OpenTelemetry core: traces, metrics and logs over standard OTLP, switched
// on only by the standard OTEL_* environment variables. With none of them set it does nothing: no exporters,
// no goroutines, no network, the global no-op providers stay in place and every helper returns immediately.
//
// # Enabling
//
// Set OTEL_EXPORTER_OTLP_ENDPOINT (or the per-signal _TRACES_/_METRICS_/_LOGS_ endpoint) and, for hosted
// backends, OTEL_EXPORTER_OTLP_HEADERS. Everything else (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES,
// OTEL_EXPORTER_OTLP_PROTOCOL, OTEL_TRACES_SAMPLER[_ARG], OTEL_METRIC_EXPORT_INTERVAL, OTEL_*_EXPORTER=none,
// OTEL_SDK_DISABLED) is optional. See docs/engineering/observability.md.
//
// # API for the rest of the hub (stable)
//
//	telemetry.Setup(ctx, telemetry.ConfigFromEnv(version))  // main only; returns a shutdown func (5 s flush)
//	telemetry.NewLogger(w)                                   // stdout JSON slog + trace ids + OTel log bridge
//	telemetry.Tracer(name) / telemetry.Meter(name)
//	ctx, span := telemetry.StartSpan(ctx, "sync.account", telemetry.Provider("google"), telemetry.AccountID(id))
//	defer span.End()
//	telemetry.RecordError(ctx, "sync", err, attrs...)        // span error + calendar.errors{component,kind} + error log
//	telemetry.Counter / UpDownCounter / Histogram / Gauge    // names get the "calendar." prefix
//	telemetry.HTTP(next) / telemetry.Route(mux)              // server middleware (internal/api wires it)
//	telemetry.WrapClient(rt, provider)                       // client spans for outbound calls (httpx wires it)
//	telemetry.RegisterErrorKind(kind, matcher)               // classify typed errors (registered from main)
//
// # Rules
//
//   - Span names and metric attributes are low cardinality: use the route pattern, never a raw path or id.
//   - Identify accounts by ULID (AccountID) and providers by name (Provider). Never put event titles,
//     descriptions, locations, attendees, e-mail addresses, tokens, Authorization headers, query strings or
//     provider-side ids into attributes, events or logs. AccountID and Provider validate their input; spans
//     and the log bridge additionally scrub values and drop known-sensitive keys as a second line of defence.
//   - Errors recorded on spans are classified (Kind), scrubbed and cut to 200 characters (Sanitize).
//   - Long-lived requests (SSE, GET /mcp) never get a span for their whole lifetime: HTTP records a short
//     connect span, an active-connections gauge and a connection-duration histogram with long buckets. Record
//     per-message happenings as span events on a short operation span or as counters, not as one span each.
//   - Slog calls only carry trace_id/span_id when the context-taking variants (InfoContext, ErrorContext) are used.
package telemetry
