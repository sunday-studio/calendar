package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// scrubProcessor is the privacy gate in front of the span exporter. Instrumentation libraries add
// url.path / url.query / client.address on their own; this drops or templates them, scrubs every string
// value and event, and removes keys that could hold user content. It wraps the real processor, so
// nothing reaches the batch queue unscrubbed.
type scrubProcessor struct{ next sdktrace.SpanProcessor }

func (p *scrubProcessor) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) {
	p.next.OnStart(ctx, s)
}
func (p *scrubProcessor) OnEnd(s sdktrace.ReadOnlySpan)        { p.next.OnEnd(scrubbedSpan{s}) }
func (p *scrubProcessor) Shutdown(ctx context.Context) error   { return p.next.Shutdown(ctx) }
func (p *scrubProcessor) ForceFlush(ctx context.Context) error { return p.next.ForceFlush(ctx) }

type scrubbedSpan struct{ sdktrace.ReadOnlySpan }

func (s scrubbedSpan) Attributes() []attribute.KeyValue {
	in := s.ReadOnlySpan.Attributes()
	route := ""
	for _, kv := range in {
		if kv.Key == KeyRoute {
			route = kv.Value.AsString()
		}
	}
	out := make([]attribute.KeyValue, 0, len(in))
	for _, kv := range in {
		switch string(kv.Key) {
		case "url.path":
			if route != "" {
				out = append(out, kv.Key.String(route))
			} else {
				out = append(out, kv.Key.String(TemplatePath(kv.Value.AsString())))
			}
			continue
		case "url.full":
			out = append(out, kv.Key.String(scrubURL(kv.Value.AsString())))
			continue
		}
		if kv, ok := scrubKV(kv); ok {
			out = append(out, kv)
		}
	}
	return out
}

func (s scrubbedSpan) Events() []sdktrace.Event {
	in := s.ReadOnlySpan.Events()
	out := make([]sdktrace.Event, len(in))
	for i, e := range in {
		e.Attributes = scrubKVs(withholdForeignPanic(e))
		out[i] = e
	}
	return out
}

func (s scrubbedSpan) Status() sdktrace.Status {
	st := s.ReadOnlySpan.Status()
	st.Description = Sanitize(st.Description)
	return st
}

func scrubKVs(in []attribute.KeyValue) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(in))
	for _, kv := range in {
		if kv, ok := scrubKV(kv); ok {
			out = append(out, kv)
		}
	}
	return out
}

func scrubKV(kv attribute.KeyValue) (attribute.KeyValue, bool) {
	if denied(string(kv.Key)) {
		return kv, false
	}
	switch kv.Value.Type() {
	case attribute.STRING:
		if kv.Key == "exception.stacktrace" {
			return kv, true
		}
		return kv.Key.String(Scrub(kv.Value.AsString())), true
	case attribute.STRINGSLICE:
		ss := kv.Value.AsStringSlice()
		o := make([]string, len(ss))
		for i, x := range ss {
			o[i] = Scrub(x)
		}
		return kv.Key.StringSlice(o), true
	}
	return kv, true
}

// NewScrubProcessor wraps a span processor with the privacy scrubber (used by Setup and by test recorders).
func NewScrubProcessor(next sdktrace.SpanProcessor) sdktrace.SpanProcessor {
	return &scrubProcessor{next: next}
}

// withholdForeignPanic handles exception events that RecordError / RecordPanic did not create (they carry
// calendar.component): the SDK itself records the value of a panic that unwinds through a span ended with
// `defer span.End()`, and a panic value can be any text, including user content. The message of such an
// event is withheld unless the panic is a Go runtime error; the stack trace stays.
func withholdForeignPanic(e sdktrace.Event) []attribute.KeyValue {
	if e.Name != "exception" {
		return e.Attributes
	}
	typ := ""
	for _, kv := range e.Attributes {
		if kv.Key == KeyComponent {
			return e.Attributes
		}
		if kv.Key == "exception.type" {
			typ = kv.Value.AsString()
		}
	}
	if len(typ) >= 8 && typ[:8] == "runtime." {
		return e.Attributes
	}
	out := make([]attribute.KeyValue, 0, len(e.Attributes))
	for _, kv := range e.Attributes {
		if kv.Key == "exception.message" {
			kv = kv.Key.String("exception value withheld (" + word(typ, "other") + ")")
		}
		out = append(out, kv)
	}
	return out
}
