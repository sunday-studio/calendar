package telemetry

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Attr is a bounded string attribute "calendar.<name>": the value must be a short lowercase word
// (anything else becomes "other"), so a metric attribute built with it can never carry free text.
func Attr(name, value string) attribute.KeyValue {
	value = strings.ToLower(value)
	if !attrShape.MatchString(value) {
		value = "other"
	}
	return attribute.String("calendar."+name, value)
}

var attrShape = regexp.MustCompile(`^[a-z0-9][a-z0-9_.\-]{0,31}$`)

// StatusClass renders an HTTP status code as 2xx/3xx/4xx/5xx (0 = "none": no response at all).
func StatusClass(code int) string {
	if code <= 0 {
		return "none"
	}
	return statusClass(code)
}

type instKind int

const (
	kCounter instKind = iota
	kHist
	kUpDown
)

type instDef struct {
	kind   instKind
	unit   string
	desc   string
	bounds []float64
}

// Instruments lists every metric the hub records through Add / Record / UpDown (names without the
// "calendar." prefix). docs/engineering/observability.md carries the same table; a test keeps them in step.
func Instruments() map[string]string {
	out := make(map[string]string, len(catalogue))
	for n, d := range catalogue {
		out[prefixed(n)] = d.desc
	}
	return out
}

var (
	instMu   sync.Mutex
	instGen  uint64
	instVals = map[string]any{}
)

// instrument returns the cached instrument for name, rebuilt when providers change (tests) so that
// package-level code never holds an instrument bound to a stale provider.
func instrument(name string, want instKind) any {
	instMu.Lock()
	defer instMu.Unlock()
	if g := generation.Load(); g != instGen {
		instGen = g
		instVals = map[string]any{}
	}
	if v, ok := instVals[name]; ok {
		return v
	}
	d, ok := catalogue[name]
	if !ok {
		d = instDef{kind: want, unit: "1", desc: name}
	}
	var v any
	switch d.kind {
	case kHist:
		v = Histogram(name, d.desc, d.unit, d.bounds...)
	case kUpDown:
		v = UpDownCounter(name, d.desc, d.unit)
	default:
		v = Counter(name, d.desc, d.unit)
	}
	instVals[name] = v
	return v
}

// Add increments the catalogue counter name (calendar.<name>) by n.
func Add(ctx context.Context, name string, n int64, attrs ...attribute.KeyValue) {
	if c, ok := instrument(name, kCounter).(metric.Int64Counter); ok {
		c.Add(ctx, n, metric.WithAttributes(attrs...))
	}
}

// Inc is Add(ctx, name, 1, attrs...).
func Inc(ctx context.Context, name string, attrs ...attribute.KeyValue) { Add(ctx, name, 1, attrs...) }

// Record adds a sample to the catalogue histogram name.
func Record(ctx context.Context, name string, v float64, attrs ...attribute.KeyValue) {
	if h, ok := instrument(name, kHist).(metric.Float64Histogram); ok {
		h.Record(ctx, v, metric.WithAttributes(attrs...))
	}
}

// UpDown adjusts the catalogue up/down counter name.
func UpDown(ctx context.Context, name string, n int64, attrs ...attribute.KeyValue) {
	if c, ok := instrument(name, kUpDown).(metric.Int64UpDownCounter); ok {
		c.Add(ctx, n, metric.WithAttributes(attrs...))
	}
}

// Observation is one gauge sample.
type Observation struct {
	Value float64
	Attrs []attribute.KeyValue
}

// GaugeSet registers an observable gauge calendar.<name> whose callback returns any number of labelled
// samples at each export (per-account gauges, status gauges). The callback must be cheap and never block.
// Call it after Setup. The returned function unregisters the gauge (tests).
func GaugeSet(name, desc, unit string, observe func(ctx context.Context) []Observation) func() {
	g, err := Meter("").Float64ObservableGauge(prefixed(name), metric.WithDescription(desc), metric.WithUnit(unit))
	if err != nil {
		otel.Handle(err)
		return func() {}
	}
	reg, err := Meter("").RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		defer func() { _ = recover() }() // a gauge must never take the exporter down
		for _, ob := range observe(ctx) {
			o.ObserveFloat64(g, ob.Value, metric.WithAttributes(ob.Attrs...))
		}
		return nil
	}, g)
	if err != nil {
		otel.Handle(err)
		return func() {}
	}
	return func() { _ = reg.Unregister() }
}

var (
	secBounds   = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	longSec     = []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600}
	countBounds = []float64{0, 1, 5, 10, 50, 100, 500, 1000, 5000, 10000}
)

// catalogue is the single source of truth for descriptions and units of the O2 metrics.
var catalogue = map[string]instDef{
	"sync.runs":                   {kCounter, "{run}", "Sync passes by provider, trigger and outcome (ok|error|needs_reauth|rate_limited|skipped)", nil},
	"sync.duration":               {kHist, "s", "Duration of one account sync pass, by provider", longSec},
	"sync.events":                 {kCounter, "{event}", "Events changed in the mirror by sync, by provider and change (added|changed|removed)", nil},
	"sync.pages":                  {kCounter, "{page}", "Provider pages fetched by sync, by provider", nil},
	"sync.cursor_resets":          {kCounter, "{reset}", "Sync cursors discarded, by provider and reason (410|manual)", nil},
	"sync.backoff":                {kCounter, "{backoff}", "Failed sync passes that scheduled a retry with backoff, by provider", nil},
	"sync_runs.pruned":            {kCounter, "{row}", "sync_runs rows deleted by the retention pruner", nil},
	"provider.requests":           {kCounter, "{request}", "Provider API calls by provider, operation, status class and outcome", nil},
	"provider.latency":            {kHist, "s", "Provider API call latency, by provider and operation", secBounds},
	"provider.rate_limited":       {kCounter, "{response}", "Provider responses that asked us to slow down (429/403 rate limit), by provider", nil},
	"provider.token_refresh":      {kCounter, "{refresh}", "OAuth access-token refreshes by provider and outcome (ok|failed|invalid_grant)", nil},
	"writes":                      {kCounter, "{write}", "User writes by op, provider and outcome", nil},
	"write.duration":              {kHist, "s", "Duration of a user write (provider call plus mirror), by op and provider", secBounds},
	"idempotency.replays":         {kCounter, "{replay}", "Writes answered from the idempotency record without touching the provider", nil},
	"writes.provider_rollbacks":   {kCounter, "{write}", "Writes where provider and mirror disagree afterwards (provider ok / mirror failed, or the reverse)", nil},
	"agenda.query.duration":       {kHist, "s", "Read-model query duration by kind (agenda|events|search|freebusy|freetime|summary)", secBounds},
	"search.queries":              {kCounter, "{query}", "Search queries served", nil},
	"recur.rebuilds":              {kCounter, "{rebuild}", "Occurrence rebuilds of one series", nil},
	"recur.rebuild.duration":      {kHist, "s", "Duration of one rebuild batch", secBounds},
	"recur.occurrences":           {kHist, "{occurrence}", "Occurrences written per series rebuild", countBounds},
	"recur.cap_hits":              {kCounter, "{series}", "Series that hit the per-series occurrence cap (5000) and were truncated", nil},
	"recur.extend_window.runs":    {kCounter, "{run}", "ExtendWindow materialiser job runs by outcome", nil},
	"db.write.duration":           {kHist, "s", "Duration of a write transaction including the wait for the single writer", secBounds},
	"db.write.transactions":       {kCounter, "{transaction}", "Write transactions by outcome (committed|rolled_back|failed)", nil},
	"db.busy":                     {kCounter, "{event}", "Write transactions that hit SQLITE_BUSY / lock waits", nil},
	"db.slow_queries":             {kCounter, "{transaction}", "Write transactions slower than 50 ms", nil},
	"db.migrations.applied":       {kCounter, "{migration}", "Migrations applied at start-up", nil},
	"push.sent":                   {kCounter, "{notification}", "Push notifications by kind and outcome (ok|failed|dead_token|throttled)", nil},
	"push.send.duration":          {kHist, "s", "APNs send latency by kind", secBounds},
	"push.dead_tokens_removed":    {kCounter, "{token}", "Device tokens removed after APNs said they are dead", nil},
	"push.summary_sent":           {kCounter, "{summary}", "Daily summary pushes handed to APNs", nil},
	"push.reminder_ticks":         {kCounter, "{tick}", "Reminder scheduler ticks", nil},
	"automation.fires":            {kCounter, "{fire}", "Automation firings by trigger, action and outcome", nil},
	"automation.webhook.duration": {kHist, "s", "Webhook delivery latency by host-free status class", secBounds},
	"automation.webhook.requests": {kCounter, "{request}", "Webhook deliveries by status class and outcome", nil},
	"automation.retries":          {kCounter, "{retry}", "Webhook delivery retries", nil},
	"automation.dead_letters":     {kCounter, "{delivery}", "Webhook deliveries given up on after all retries", nil},
	"mcp.calls":                   {kCounter, "{call}", "MCP tool calls by tool and outcome (ok|denied|confirm_required|expired|error)", nil},
	"mcp.call.duration":           {kHist, "s", "MCP tool call duration by tool", secBounds},
	"mcp.confirmations":           {kCounter, "{confirmation}", "MCP confirmation tokens by state (minted|approved|expired|denied)", nil},
	"mcp.rate_limited":            {kCounter, "{call}", "MCP calls rejected by the rate limiter", nil},
	"quickadd.requests":           {kCounter, "{request}", "Quick-add parses by source (alfather|rules), degraded and outcome", nil},
	"alfather.latency":            {kHist, "s", "Latency of the Alfather language-model call", longSec},
	"alfather.timeouts":           {kCounter, "{call}", "Alfather calls that timed out", nil},
	"quickadd.ambiguities":        {kCounter, "{parse}", "Quick-add parses that came back ambiguous", nil},
	"sse.connections":             {kCounter, "{connection}", "SSE stream connects and disconnects by event (connect|disconnect)", nil},
	"sse.drops":                   {kCounter, "{client}", "SSE clients dropped because they were too slow", nil},
	"sse.events_published":        {kCounter, "{event}", "SSE events published by type", nil},
	"auth.requests":               {kCounter, "{request}", "Authentication attempts by outcome and token kind", nil},
	"auth.throttled":              {kCounter, "{request}", "Authentication attempts rejected by the failure throttle", nil},
	"auth.tokens":                 {kCounter, "{token}", "API tokens created or revoked, by action and kind", nil},
	"backup.runs":                 {kCounter, "{run}", "Backup runs by outcome", nil},
	"backup.duration":             {kHist, "s", "Backup run duration", longSec},
	"goroutine.panics":            {kCounter, "{panic}", "Panics recovered at a background-goroutine boundary, by name", nil},
	"goroutine.restarts":          {kCounter, "{restart}", "Supervised background goroutines restarted after a panic", nil},
	"account.events":              {kCounter, "{event}", "Account lifecycle events (connected|reconnected|removed|needs_reauth|error), by provider", nil},
}
