package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

// Error kinds added by the per-feature instrumentation.
const (
	KindReauth      = "reauth"      // the provider refused our credentials: the user must sign in again
	KindConsistency = "consistency" // provider and mirror disagree after a write (the dangerous state)
)

// RecordErrorKind is RecordError with an explicit kind instead of the one Kind(err) derives. Use it where
// the call site knows better than the error type (reauth, consistency, timeout). kind must be a short
// lowercase word; the number of distinct kinds is bounded.
func RecordErrorKind(ctx context.Context, component, kind string, err error, attrs ...attribute.KeyValue) {
	if err == nil {
		return
	}
	kind = kindsGuard.Allow(word(kind, KindInternal))
	addErrorTelemetry(ctx, word(component, "other"), kind, typeName(err), Sanitize(err.Error()), "", attrs)
	if Enabled() {
		logger().ErrorContext(ctx, "error", "component", word(component, "other"), "kind", kind, "err", Sanitize(err.Error()))
	}
}
