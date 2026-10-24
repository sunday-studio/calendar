# Observability (OpenTelemetry)

The hub exports traces, metrics and logs over standard OTLP. It is **off** unless you set an OTLP endpoint. With no `OTEL_*` variable set the hub behaves exactly as before: no-op providers, no exporter goroutines, no network, no measurable overhead. Code: `server/internal/telemetry`.

## Enable it

Set variables in `calendard.env` (compose passes the whole file through `env_file`; both `compose.yaml` and `compose.host-tailscale.yaml` do) and restart. Nothing else.

```
OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp.example.com        # the one required line
OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20<token>   # if the backend wants a key
OTEL_SERVICE_NAME=calendard                                 # default calendard
OTEL_RESOURCE_ATTRIBUTES=deployment.environment=prod
```

| Variable | Effect |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT`, `..._TRACES_/_METRICS_/_LOGS_ENDPOINT` | Any one set (and not disabled) turns telemetry on. A per-signal endpoint alone enables only that signal. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` (and per signal) | `http/protobuf` (default) or `grpc`. `http/json` falls back to protobuf. |
| `OTEL_EXPORTER_OTLP_HEADERS`, `_INSECURE`, `_CERTIFICATE`, `_TIMEOUT`, `_COMPRESSION` | Read by the OTel SDK. A space inside a header value is written `%20`. |
| `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES` | Resource. Always added: `service.version` (the binary version), `service.instance.id` (random per start), host, OS, process pid/runtime (never the command line). |
| `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` | Default `parentbased_always_on`. To keep a quarter of traces: `parentbased_traceidratio` and `0.25`. |
| `OTEL_METRIC_EXPORT_INTERVAL` | Milliseconds, default 60000. |
| `OTEL_TRACES_EXPORTER=none`, `OTEL_METRICS_EXPORTER=none`, `OTEL_LOGS_EXPORTER=none` | Switch one signal off. |
| `OTEL_SDK_DISABLED=true` | Everything off, even with an endpoint set. |
| `OTEL_BSP_*`, `OTEL_BLRP_*` | Batch queue/size/delay tuning (SDK defaults are bounded: 2048 spans/logs queued, drops when full). |

Hosted-backend examples (Grafana Cloud, Honeycomb, SigNoz, a self-hosted collector) are in `deploy/calendard.env.example`; the SigNoz and Grafana header syntax is marked there as something to confirm against the vendor's own snippet.

Startup logs one line: `telemetry enabled endpoint=<host> protocol=<p> signals=traces,metrics,logs` or `telemetry disabled`. `calendard doctor` prints a `telemetry` line (endpoint host only, headers masked, protocol, sampler, signals) and probes the endpoint within 2 s: TCP dial for gRPC, HTTP `HEAD` for http/protobuf. An unreachable collector is a warning, never a failure.

**Containers:** in `compose.yaml` the hub shares the Tailscale container's network namespace, so `host.docker.internal` does not resolve there unless the `tailscale` service gets `extra_hosts`. Use a hosted endpoint, or a collector reachable by tailnet name. In `compose.host-tailscale.yaml` the hub is a normal container: `http://host.docker.internal:4318` works on Docker Desktop and OrbStack (macOS).

## Failure behaviour

Exporting is asynchronous (batch span/log processors with bounded queues, periodic metric reader). A down or slow collector never blocks a request or the sync engine: data is dropped, and exporter errors are logged at most once a minute per distinct message (`telemetry export problem (hub unaffected)`). On SIGTERM the hub flushes with a 5 s budget after the HTTP server has drained. If SDK setup itself fails the hub logs a warning and runs without telemetry.

## Try it locally

```
docker compose -f deploy/compose.otel-dev.yaml up -d      # grafana/otel-lgtm: collector + Tempo + Prometheus + Loki + Grafana
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 make run
open http://localhost:3000                                # Explore: Tempo (traces), Prometheus (metrics), Loki (logs)
```

Then `calendard doctor` should say `endpoint reachable`. Make a few requests; traces appear within seconds, metrics after one export interval (set `OTEL_METRIC_EXPORT_INTERVAL=5000` while testing).

## What is recorded today (O1 core)

Traces
- One server span per request, named by route pattern, e.g. `GET /v1/events/{id}` (unmatched paths: `GET unmatched`). Attributes: `http.route`, `http.request.method`, `http.response.status_code`, scheme, host; `url.path` is replaced by the route and `url.query`, `client.address` and peer addresses are dropped. Inbound W3C `traceparent` is continued; no baggage is read or sent.
- 5xx and recovered panics: span status Error plus an `exception` event (type, scrubbed message, `calendar.error.kind`, `calendar.component`; panics add a stack trace). 4xx are not errors.
- One client span per outbound provider attempt (`google GET /calendar/v3/calendars/:id/events/:id`), with `calendar.provider`. URLs are templated (ids become `:id`), no query string, no userinfo, no tokens. No `traceparent` is sent to providers.
- `GET /v1/stream` (SSE) and `GET /mcp` get only a short "connect" span (`calendar.long_lived=true`), not a span for the connection lifetime. Do not make one span per pushed event; use counters, or span events on a short operation span.

Metrics
| Name | Type | Attributes |
|---|---|---|
| `http.server.request.duration` | histogram (s) | `http.route`, method, status code |
| `http.server.request.body.size`, `http.server.response.body.size` | histogram (By) | same |
| `calendar.http.requests` | counter | `http.route`, `http.request.method`, `calendar.http.status_class` (2xx..5xx) |
| `calendar.http.in_flight` | up/down counter | none |
| `calendar.http.long_lived.active`, `calendar.http.long_lived.duration` | up/down counter, histogram (s, buckets to 24 h) | `http.route` |
| `calendar.errors` | counter | `calendar.component`, `calendar.error.kind` |
| `calendar.provider.retries` | counter | `calendar.provider`, `calendar.retry.reason` (`429`, `5xx`, `network`) |
| `http.client.request.duration` etc. | histogram | `calendar.provider`, server address, status |
| `go.*` runtime metrics (goroutines, memory, GC) | gauges/counters | none |

Logs
- Stdout JSON logs are unchanged (plus `trace_id`/`span_id` when the call passes a context with an active span: use `slog.InfoContext(ctx, ...)`, not `slog.Info(...)`). The request log gains `route` when telemetry is on.
- With logs export on, every record is also sent over OTLP through the `otelslog` bridge, with messages scrubbed and sensitive keys (`path`, `title`, `email`, `value`, ...) dropped.

`O2` appends the per-feature catalogue (sync, events, push, automations, MCP, backups) below.

## Writing instrumentation

```go
ctx, span := telemetry.StartSpan(ctx, "sync.account", telemetry.AccountID(id), telemetry.Provider("google"), telemetry.SyncTrigger("schedule"))
defer span.End()
...
if err != nil { telemetry.RecordError(ctx, "sync", err); return err }

var synced = telemetry.Counter("sync.runs", "Sync runs by outcome", "{run}")   // -> calendar.sync.runs
synced.Add(ctx, 1, metric.WithAttributes(telemetry.Provider("google"), telemetry.Outcome("ok")))
```

Helpers: `Tracer`, `Meter`, `StartSpan`, `RecordError` / `RecordSpanError` (no log line) / `RecordPanic`, `Counter`, `UpDownCounter`, `Histogram` (pass bucket bounds for non-latency values), `Gauge`, `WrapClient(rt, provider)` for any other outbound `http.Client` (APNs), `WithProvider(ctx, name)`, `RegisterErrorKind`, `Guard`. Instruments are no-ops until telemetry is on, so call them unconditionally.

Naming and cardinality
- Metric and custom span names start with `calendar.` (spans may be plain verbs like `sync.account`). Units are UCUM (`s`, `By`, `{event}`).
- Attribute keys are the constants in `telemetry/helpers.go`: `calendar.account.id`, `calendar.provider`, `calendar.outcome`, `calendar.component`, `calendar.sync.trigger`, `calendar.error.kind`, `calendar.operation`. Add new keys there.
- Metric attributes must have a small fixed set of values: route patterns, outcomes, providers, kinds, status classes. **Account ULIDs go on spans and logs, not on metrics** unless the metric is explicitly per-account (there are only a handful of accounts). `Guard` caps a value set (`component` is capped at 64 and falls back to `other`).
- Error kinds: `provider`, `db`, `validation`, `panic`, `timeout`, `push`, `auth`, `network`, `canceled`, `internal`. `telemetry.Kind(err)` derives it with `errors.Is/As`; typed errors are mapped in `cmd/calendard/telemetry.go` (telemetry itself imports no domain package). Register new typed errors there.

## Privacy rules (enforced by tests)

Never put these in span attributes, span events, metric attributes or log attributes: event titles, descriptions, locations, attendee names or e-mail addresses, account e-mail addresses or display names, access/refresh/API tokens or `cal_` secrets, Authorization headers, ICS feed URLs, query strings, provider-side ids (calendar ids, event ids, sync tokens). Use the account ULID and provider name only.

Defence in depth, so a mistake does not leak: `AccountID` replaces anything that is not a ULID with `invalid`; every exported span passes through a scrubber (templates `url.path`/`url.full`, drops keys such as `url.query`, `client.address`, `*email*`, `*token*`, scrubs string values); the log bridge scrubs messages and attribute values and drops denied keys; `RecordError` records the typed kind plus the message scrubbed (e-mails, bearer/refresh/Google tokens, URL queries, long opaque blobs removed) and cut to 200 characters; panic values are reduced to their type unless they are Go runtime errors. Tests: `TestTelemetryNeverCarriesUserContent` (`internal/api`), `TestSanitize`, `TestTemplatePath`, `TestOutboundCallsGetTemplatedClientSpans`.

The local stdout log keeps its existing content rules ("no event titles at info"); the OTLP copy is stricter.

## Errors and alerting

Every failure that goes through `RecordError` (and every 5xx or panic at the HTTP layer) appears three ways: an `exception` event on the active span, a tick on `calendar.errors{calendar.component, calendar.error.kind}`, and an error log line with `trace_id`/`span_id` (stdout and OTLP). To investigate: find the log or the trace in your backend, follow the trace id.

Starting alerts (PromQL names; backends rename dots to underscores):
- Any server errors: `increase(calendar_errors_total{calendar_error_kind=~"internal|db|panic"}[10m]) > 0`
- Provider trouble: `sum(rate(calendar_errors_total{calendar_error_kind="provider"}[15m])) > 0.05` or a rising `calendar_provider_retries_total`
- Panics: `increase(calendar_errors_total{calendar_error_kind="panic"}[5m]) > 0` (page)
- 5xx ratio: `sum(rate(calendar_http_requests_total{calendar_http_status_class="5xx"}[5m])) / sum(rate(calendar_http_requests_total[5m])) > 0.02`
- Latency: p95 of `http_server_request_duration_seconds` by `http_route`
- Hub down: no data from `go_goroutine_count` for 5 minutes (alert on absence)

# Instrumentation catalogue (O2)

Every metric is `calendar.<name>` in OpenTelemetry and `calendar_<name>[_unit][_total]` in Prometheus (dots become underscores; counters gain `_total`; unit `s` gains `_seconds`; histograms expose `_bucket`/`_sum`/`_count`). Attribute keys below are written without the `calendar.` prefix they carry on the wire (`provider` is `calendar.provider`, `outcome` is `calendar.outcome`). Every attribute has a small fixed value set: `telemetry.Attr` only accepts short lowercase words, anything else becomes `other`.

Call sites use `telemetry.Inc / Add / Record / UpDown` (instruments and their units are declared once, in `internal/telemetry/instruments.go`; `TestCatalogueIsDocumented` fails if a metric there is missing from this page) and `telemetry.GaugeSet` for observable gauges. Gauges are registered once at start-up from `cmd/calendard/telemetry.go` (`registerGauges`).

## Metrics

Counters and histograms

| Name | Type | Unit | Attributes | Meaning |
|---|---|---|---|---|
| `calendar.sync.runs` | counter | {run} | provider, `sync.trigger` (tick, manual, after_write, reauthed), outcome (ok, error, needs_reauth, rate_limited, skipped) | One account sync pass. `skipped` = the account was not eligible (paused, needs sign-in). |
| `calendar.sync.duration` | histogram | s | provider | Wall time of one pass. |
| `calendar.sync.events` | counter | {event} | provider, change (added, changed, removed) | Events the mirror gained, changed or tombstoned by sync. |
| `calendar.sync.pages` | counter | {page} | provider | Provider pages fetched. |
| `calendar.sync.cursor_resets` | counter | {reset} | provider, reason (`410` expired token, `manual` feed re-pointed) | Sync cursors discarded; a full re-list follows. |
| `calendar.sync.backoff` | counter | {backoff} | provider | Failed passes that scheduled a retry with backoff. |
| `calendar.sync_runs.pruned` | counter | {row} | none | `sync_runs` rows deleted by the daily pruner (a counter, not a gauge, so rates work). |
| `calendar.provider.requests` | counter | {request} | provider, operation (list_events, list_calendars, fetch, insert, patch, delete, rsvp, move, token_refresh), `status_class` (2xx..5xx, none), outcome (ok, not_found, conflict, rate_limited, needs_reauth, not_modified, error) | One attempt at a provider API (Google, ICS feed). `none` = no response (network). |
| `calendar.provider.latency` | histogram | s | provider, operation | Latency of the same attempts. |
| `calendar.provider.rate_limited` | counter | {response} | provider | Responses asking us to slow down (429, or 403 with a rate-limit reason). |
| `calendar.provider.token_refresh` | counter | {refresh} | provider, outcome (ok, failed, invalid_grant) | OAuth access-token refreshes; `invalid_grant` also records an error of kind `reauth`. |
| `calendar.provider.retries` | counter | {retry} | provider, `retry.reason` | O1: retries done inside the HTTP transport. |
| `calendar.writes` | counter | {write} | op (create, patch, delete, rsvp, meet, move, reminder), provider, outcome (ok, conflict, precondition_failed, provider_error, validation, not_supported, rate_limited, replayed) | One user write. `validation` includes not-found and forbidden; `replayed` is an idempotency replay. |
| `calendar.write.duration` | histogram | s | op, provider | Provider call plus mirror. |
| `calendar.idempotency.replays` | counter | {replay} | op | Writes answered from the idempotency record. |
| `calendar.writes.provider_rollbacks` | counter | {write} | op, kind (mirror_failed, move_copy_not_removed, split_undo_failed) | Provider and mirror disagree after a write. Always accompanied by an error of kind `consistency`. |
| `calendar.agenda.query.duration` | histogram | s | kind (agenda, events, search, freebusy, freetime, summary) | Read-model query time. |
| `calendar.search.queries` | counter | {query} | outcome | Search queries (the text is never recorded). |
| `calendar.recur.rebuilds` | counter | {rebuild} | none | Series (or single events) rebuilt in the occurrence read model. |
| `calendar.recur.rebuild.duration` | histogram | s | none | One `Rebuild` batch. |
| `calendar.recur.occurrences` | histogram | {occurrence} | none | Occurrences written per series rebuild. |
| `calendar.recur.cap_hits` | counter | {series} | none | A series hit the per-series cap (5000) and was truncated. A warning metric, not an error. |
| `calendar.recur.extend_window.runs` | counter | {run} | outcome (ok, error) | Daily ExtendWindow job. |
| `calendar.db.write.duration` | histogram | s | none | Write transaction including the wait for the single writer. |
| `calendar.db.write.transactions` | counter | {transaction} | outcome (committed, rolled_back, failed) | `rolled_back` is the caller's own error (often an expected condition); `failed` is begin/commit failing. |
| `calendar.db.busy` | counter | {event} | reason (wait: queued behind another writer over 50 ms, timeout: SQLITE_BUSY) | Lock contention. |
| `calendar.db.slow_queries` | counter | {transaction} | none | Write transactions over 50 ms (also a `slow write transaction` span event). |
| `calendar.db.migrations.applied` | counter | {migration} | none | Migrations applied at start-up (`db.migrate` span carries known/applied). |
| `calendar.push.sent` | counter | {notification} | kind (sync, moved, cancelled, changed, reauth, summary, reminder), outcome (ok, failed, dead_token, throttled) | One push to one device. |
| `calendar.push.send.duration` | histogram | s | kind | APNs round trip. |
| `calendar.push.dead_tokens_removed` | counter | {token} | none | Device tokens dropped after APNs said they are gone. |
| `calendar.push.summary_sent` | counter | {summary} | none | Morning summaries handed to APNs. |
| `calendar.push.reminder_ticks` | counter | {tick} | none | Reminder scheduler ticks (one a minute). |
| `calendar.automation.fires` | counter | {fire} | trigger, action (webhook, ntfy), outcome (ok, failed) | One automation delivery (after retries). |
| `calendar.automation.webhook.requests` | counter | {request} | action, `status_class`, outcome | One delivery attempt. |
| `calendar.automation.webhook.duration` | histogram | s | action, `status_class` | Latency of an attempt. No URL or host is recorded. |
| `calendar.automation.retries` | counter | {retry} | action | Delivery retries. |
| `calendar.automation.dead_letters` | counter | {delivery} | action | Deliveries given up on. |
| `calendar.mcp.calls` | counter | {call} | tool, outcome (ok, denied, confirm_required, expired, error) | One MCP tool call. `denied` covers validation, not found, forbidden, conflict, invalid or used tokens. |
| `calendar.mcp.call.duration` | histogram | s | tool | Tool call time. |
| `calendar.mcp.confirmations` | counter | {confirmation} | state (minted, approved, expired, denied) | Confirmation tokens. |
| `calendar.mcp.rate_limited` | counter | {call} | none | Calls rejected by the per-token limiter (`hubmcp.RecordRateLimited`, called by the HTTP layer). |
| `calendar.quickadd.requests` | counter | {request} | source (alfather, rules), degraded (true, false), outcome (ok, ambiguous, error) | Quick-add parses. `rules`+`degraded=true` means alfather failed and rules answered. |
| `calendar.quickadd.ambiguities` | counter | {parse} | none | Parses that came back needing answers. |
| `calendar.alfather.latency` | histogram | s | none | The alfather call. |
| `calendar.alfather.timeouts` | counter | {call} | none | Alfather calls that timed out (also an error of kind `timeout`). |
| `calendar.sse.connections` | counter | {connection} | event (connect, disconnect) | SSE lifecycle (the HTTP layer adds the short connect span). |
| `calendar.sse.drops` | counter | {client} | none | Slow clients dropped. |
| `calendar.sse.events_published` | counter | {event} | type (rev, account, sync) | Frames fanned out. |
| `calendar.auth.requests` | counter | {request} | outcome (ok, invalid, revoked, scope_denied), `token_kind` (device, script, none) | Token checks. `authn.RecordScopeDenied` and `RecordThrottled` are called by the HTTP layer. |
| `calendar.auth.throttled` | counter | {request} | none | Requests rejected by the invalid-token throttle. |
| `calendar.auth.tokens` | counter | {token} | action (created, revoked), `token_kind` | Token lifecycle (span events carry the token ULID, never the secret or name). |
| `calendar.backup.runs` | counter | {run} | outcome (ok, failed) | Backup runs. |
| `calendar.backup.duration` | histogram | s | none | Backup time. |
| `calendar.account.events` | counter | {event} | provider, event (connected, reconnected, removed, needs_reauth, error, declined, invalid_state, identity_mismatch, no_refresh_token) | Account lifecycle; each is also a span event and an info log with the account ULID. |
| `calendar.goroutine.panics` | counter | {panic} | goroutine | Panics recovered at a background-goroutine boundary (`telemetry.Safe`, `Go`, `Supervise`). Also an error of kind `panic` with the stack as an exception event. |
| `calendar.goroutine.restarts` | counter | {restart} | goroutine | Supervised loops restarted after a panic. |
| `calendar.errors` | counter | {error} | component, `error.kind` | Every `RecordError`. New kinds: `reauth`, `consistency`. |

Observable gauges (sampled at each export; the two account gauges are the only metrics that carry the account ULID, a deliberate exception because there are a handful of accounts)

| Name | Unit | Attributes | Meaning |
|---|---|---|---|
| `calendar.account.last_sync_age_seconds` | s | `account.id`, provider | Seconds since the last **successful** sync (`last_ok_at`; creation time if never). |
| `calendar.account.status` | 1 | `account.id`, provider, status (active, syncing, needs_reauth, error, paused) | 1 for the account's current status only. |
| `calendar.accounts` | {account} | provider, status | Accounts per status. |
| `calendar.sync.backoff_seconds` | s | none | Longest retry delay currently scheduled (0 when nothing is failing). |
| `calendar.db.rev` | 1 | none | `meta.rev`; moves with every user-visible change. |
| `calendar.db.size_bytes` / `calendar.db.wal_bytes` | By | none | Database and write-ahead-log size. |
| `calendar.events` / `calendar.occurrences` | {event} / {occurrence} | none | Live events and occurrence rows (cached 30 s). |
| `calendar.sse.clients` | {client} | none | Connected SSE clients. |
| `calendar.backup.last_success_age_seconds` | s | none | Seconds since the last good backup (newest of this process and `meta.last_backup_at`; since start-up if never). Only registered when scheduled backups are on. |
| `calendar.backup.last_size_bytes` | By | none | Size of the last backup this process wrote. |
| `calendar.process.uptime_seconds` | s | none | Seconds since the process started; a drop means a restart. |
| `calendar.build.info` | 1 | `version` | Always 1; the label is the running build. |
| `go.*` | | none | Go runtime metrics (goroutines, memory, GC), enabled by O1. |

## Spans

| Span | Attributes | Notes |
|---|---|---|
| `sync.run` | `account.id`, provider, `sync.trigger`, `sync.outcome`, `sync.added/changed/removed` | One pass. Events: `account.needs_reauth`, `account.error`, `sync.rate_limited`, `exception`. |
| `sync.calendar` | `account.id`, provider, `sync.pages`, `sync.full` | Child of `sync.run`, one per visible calendar. Events: `sync.cursor_reset`, `sync.rate_limit_wait`. |
| `db.write` | `db.outcome`, `db.rev` | Every write transaction (child of whoever called). Events: `slow write transaction`, `waited for the writer lock`. |
| `db.migrate` | `migrations.known`, `migrations.applied` | Start-up. One event per migration. |
| `sync.prune_runs`, `recur.extend_window`, `recur.rebuild`, `push.process`, `push.summary`, `push.reminder_tick`, `automation.tick` | | Background jobs. `recur.rebuild` has event `recur.cap_hit`. |
| `google <METHOD> /templated/path`, `ics GET /feed`, `webhook POST /webhook`, `apns POST /3/device/:id`, `alfather POST /mcp`, `google.token_refresh`, `oauth.exchange` | provider, host | Client spans. Paths are templated or fixed labels; feed and webhook paths and the device token never appear. |
| `events.create` / `patch` / `delete` / `rsvp` / `meet` / `move` / `reminder` | operation, `scope`, provider, `calendar.access`, outcome | One user write; child `db.write` and client spans. |
| `agenda.agenda` / `events` / `search` / `freebusy` / `freetime` / `summary` | `kind` | Read-model queries. |
| `push.send` | `kind`, `env` (sandbox, production), outcome | One push to one device. Event `push.dead_token`. |
| `automation.fire` | trigger, action, `automation.id`, outcome, `automation.test` | One delivery including retries. Event `automation.retry`. |
| `mcp.tool` | `tool`, `scope`, outcome, `code` | One tool call. |
| `quickadd.parse`, `alfather.ask` | `source`, `degraded` | Quick add; the sentence is never recorded. |
| `accounts.oauth_callback` | provider | Sign-in callback. |
| `backup.run`, `backup.restore`, `rekey.run` | `backup.encrypted`, `backup.bytes`, outcome; rekey counts | Operations. |

Span events for account lifecycle: `account.connected`, `account.reconnected`, `account.removed`, `account.needs_reauth`, `account.error`; for tokens: `auth.token_created`, `auth.token_revoked`, `auth.rejected`, `auth.scope_denied`.

## Error kinds and what is an error

`RecordError(ctx, component, err)` is the only way a failure is recorded. Components: `sync`, `events`, `accounts`, `authn`, `db`, `agenda`, `recur`, `push`, `automate`, `mcp`, `quickadd`, `alfather`, `backup`, `rekey`, `stream`, `provider.google`, plus `http` (O1). Kinds: the O1 list plus `reauth` (the provider refused our credentials; the user must sign in) and `consistency` (provider and mirror disagree after a write). Use `RecordErrorKind` when the call site knows the kind better than the error type.

**Not errors, counted as outcomes instead:** validation failures, not found, conflicts, precondition failures, rate limiting, 4xx, a declined sign-in, an unknown or expired confirmation, a dead device token, a cap hit, an ICS feed answering 304. A failed quick-add falls back to rules and is `degraded=true`, not an error (the alfather failure itself is recorded as a `timeout` or other error under component `alfather`).

Panics in background goroutines are recovered at the boundary (sync worker pass, sync reconcile, pruner, ExtendWindow, push dispatcher and summary ticks, automation tick, backup schedule, MCP tool): `telemetry.Safe` records the exception with its stack, counts `calendar.goroutine.panics`, and the loop carries on (a sync pass backs off first). `telemetry.Supervise` restarts a whole loop with 1 s doubling to 1 min. The panic value is never exported (only its type, or the message of a Go runtime error), including when the SDK itself records a panic unwinding through a span.

# What to alert on

Written as plain conditions with a PromQL starting point (Prometheus names). Tune thresholds after a week of real data.

| Condition | PromQL | Severity |
|---|---|---|
| Any account needs sign-in for more than 5 minutes | `max by (calendar_account_id) (calendar_account_status{calendar_status="needs_reauth"}) == 1` for 5m | Ticket (someone must tap Reconnect) |
| An account that should be syncing has not synced for 10 minutes (fast cadence: app open) | `calendar_account_last_sync_age_seconds > 600 and on(calendar_account_id) calendar_account_status{calendar_status=~"active|syncing"} == 1` for 2m, only if `calendar_sse_clients > 0` | Warning |
| ... for 30 minutes (slow cadence, always) | same with `> 1800` | Page |
| Provider error ratio above 5% for 15 minutes | `sum by (calendar_provider) (rate(calendar_provider_requests_total{calendar_outcome="error"}[15m])) / sum by (calendar_provider) (rate(calendar_provider_requests_total[15m])) > 0.05` | Warning |
| Provider is rate limiting us | `sum(rate(calendar_provider_rate_limited_total[15m])) > 0` and rising `calendar_sync_backoff_seconds > 300` | Warning |
| Writes failing at the provider | `sum(increase(calendar_writes_total{calendar_outcome="provider_error"}[15m])) > 0` | Warning |
| Frequent write conflicts (informational) | `sum(rate(calendar_writes_total{calendar_outcome="conflict"}[1h])) / sum(rate(calendar_writes_total[1h])) > 0.1` | Info |
| **Provider and mirror disagree after a write** | `sum(increase(calendar_errors_total{calendar_error_kind="consistency"}[5m])) > 0` or `increase(calendar_writes_provider_rollbacks_total[5m]) > 0` | Page (any, always) |
| Push: dead tokens spiking | `sum(increase(calendar_push_sent_total{calendar_outcome="dead_token"}[1h])) > 3` | Warning |
| Push: failures | `sum(rate(calendar_push_sent_total{calendar_outcome=~"failed|throttled"}[30m])) / sum(rate(calendar_push_sent_total[30m])) > 0.2` | Warning |
| Backup overdue | `calendar_backup_last_success_age_seconds > 36 * 3600` (also `increase(calendar_backup_runs_total{calendar_outcome="failed"}[1d]) > 0`) | Page |
| Errors by component | `sum by (calendar_component, calendar_error_kind) (rate(calendar_errors_total[10m])) > 0` (ignore kinds `validation`, `canceled`) | Warning; `panic`, `db`, `consistency` page |
| Goroutine panics | `increase(calendar_goroutine_panics_total[10m]) > 0` | Page |
| 5xx ratio | `sum(rate(calendar_http_requests_total{calendar_http_status_class="5xx"}[5m])) / sum(rate(calendar_http_requests_total[5m])) > 0.02` | Page |
| Hub restarted | `resets(calendar_process_uptime_seconds[30m]) > 0` (one restart is a deploy; more than two in 30 minutes is a crash loop) | Info / Page |
| Hub gone | no data from `calendar_process_uptime_seconds` for 5 minutes (alert on absence) | Page |
| Database growth | `predict_linear(calendar_db_size_bytes[6h], 7 * 24 * 3600) > 2e9`; WAL stuck: `calendar_db_wal_bytes > 256 * 1024 * 1024` for 30m | Warning |
| Database contention | `rate(calendar_db_busy_total[10m]) > 0.1` or p95 `calendar_db_write_duration_seconds` above 0.25 s | Warning |
| SSE clients being dropped | `increase(calendar_sse_drops_total[15m]) > 3` | Warning |
| Slow reads | `histogram_quantile(0.95, sum by (le, calendar_kind) (rate(calendar_agenda_query_duration_seconds_bucket[10m]))) > 0.25` | Warning |
| Automations failing | `sum(increase(calendar_automation_dead_letters_total[1h])) > 0` | Warning |
| Assistant down | `sum(rate(calendar_quickadd_requests_total{calendar_degraded="true"}[15m])) > 0` for 15m | Info |
| Revoked token still presented | `increase(calendar_auth_requests_total{calendar_outcome="revoked"}[1h]) > 0`, or invalid tokens above 20 an hour | Info / Warning |
| Series truncated | `increase(calendar_recur_cap_hits_total[1d]) > 0` | Info |

# Reading an incident

1. **Start from the alert.** The labels tell you where to look: `calendar_component` + `calendar_error_kind` for `calendar_errors_total`, `calendar_account_id` for the account gauges, `calendar_provider` for provider metrics.
2. **Find the failing trace.** In the trace backend filter spans by status = error, then by `calendar.component` or `calendar.account.id`. `sync.run` spans carry the account, provider, trigger and outcome; its `exception` event holds the error kind and a scrubbed, truncated message. For a write, find `events.<op>` (outcome attribute); a `consistency` error sits on the span of the write that caused it, and its `db.write` and client child spans show which half succeeded.
3. **Follow the trace id into the logs.** Every `RecordError` and lifecycle event is also a log line (stdout JSON and OTLP) with `trace_id` and `span_id`: `{trace_id="<id>"}` in Loki, or `jq 'select(.trace_id=="<id>")'` on the container log. The lines around the error (sync cursor reset, account needs_reauth, retry waits) are on the same trace.
4. **Check the neighbours.** For a sync problem look at `calendar_provider_requests_total` by `calendar_status_class` (is Google answering 5xx, or are we sending something it rejects), `calendar_sync_backoff_seconds`, and `calendar_db_busy_total` / `calendar_db_write_duration_seconds` (is the single writer starved). For a push problem, `calendar_push_sent_total` by outcome and the `apns` client spans' status code.
5. **Common shapes.** `reauth` errors: open the app, Reconnect the account. `consistency`: stop writing to that account, run a manual sync (`POST /v1/sync`) and compare; a `move_copy_not_removed` rollback leaves a duplicate event in the destination account to delete by hand. `panic`: the stack is on the exception event; the loop kept running, the data of that pass was not applied. Backup overdue: check `calendar_backup_runs_total{calendar_outcome="failed"}` and the `backup.run` span error; run `calendard backup`.
6. **Nothing is lost when telemetry is down.** Stdout logs are unchanged and complete; the OTLP copy is stricter (scrubbed, no titles) and best effort.

Privacy for all new signals follows the rules above and is enforced by `TestTelemetryPrivacyAcrossEverySignal` (`internal/events/privacy_test.go`): distinctive titles, descriptions, locations, attendee and account e-mails, refresh and API tokens, remote ids, webhook URLs with secret paths and queries, device tokens, search and quick-add text, alfather URLs and token names are pushed through sync, writes, push, automations, MCP, quick-add, auth and backup, and none may appear in any span, event, metric attribute, OTLP log or stdout line. Webhook and ICS destinations are redacted to a fixed path label (`/webhook`, `/feed`) before they reach a client span, and `telemetry.SafeError` strips the request URL from transport errors.

## Orion (logs only)

Orion's ingest endpoint accepts logs, so enable logs and switch the other two signals off. Put these in `deploy/calendard.env` on the server (or in `.local/otel.env` for a local run, which `scripts/run-local.sh` loads automatically). **The ingest key is a secret: keep it out of git** (`deploy/calendard.env` and `.local/` are git-ignored).

```
OTEL_SERVICE_NAME=calendard
OTEL_RESOURCE_ATTRIBUTES=deployment.environment=prod        # local for a local run
OTEL_TRACES_EXPORTER=none
OTEL_METRICS_EXPORTER=none
OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=https://orion.labs.casprine.com/v1/ingest/otlp/v1/logs
OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20<your orion_ik_… key>
```

Notes: the hub's Go exporter sends OTLP over `http/protobuf`; Orion's quickstart shows `http/json`, which the Go SDK does not offer, and Orion accepts the protobuf payload (verified 2026-10-24: records appeared in Service logs). The space after `Bearer` must be written `%20` in this variable. When Orion gets traces and metrics endpoints, replace the two `none` lines with `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` and `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`.
