package telemetrytest

import (
	"sort"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Attrs is a subset of attribute key -> value (as rendered by Emit) a data point must carry.
type Attrs map[string]string

func (a Attrs) matches(set attribute.Set) bool {
	for k, v := range a {
		got, ok := set.Value(attribute.Key(k))
		if !ok || got.Emit() != v {
			return false
		}
	}
	return true
}

// MetricNames lists every metric name collected now (sorted).
func (r *Recorder) MetricNames(t testing.TB) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, m := range r.Metrics(t) {
		seen[m.Name] = true
	}
	var out []string
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Sum adds up the int/float sum data points (counters) of metric name whose attributes include match.
func (r *Recorder) Sum(t testing.TB, name string, match Attrs) float64 {
	t.Helper()
	var total float64
	for _, m := range r.Metrics(t) {
		if m.Name != name {
			continue
		}
		switch d := m.Data.(type) {
		case metricdata.Sum[int64]:
			for _, p := range d.DataPoints {
				if match.matches(p.Attributes) {
					total += float64(p.Value)
				}
			}
		case metricdata.Sum[float64]:
			for _, p := range d.DataPoints {
				if match.matches(p.Attributes) {
					total += p.Value
				}
			}
		}
	}
	return total
}

// HistCount is the number of samples recorded in histogram name for data points including match.
func (r *Recorder) HistCount(t testing.TB, name string, match Attrs) uint64 {
	t.Helper()
	var n uint64
	for _, m := range r.Metrics(t) {
		if m.Name != name {
			continue
		}
		if d, ok := m.Data.(metricdata.Histogram[float64]); ok {
			for _, p := range d.DataPoints {
				if match.matches(p.Attributes) {
					n += p.Count
				}
			}
		}
	}
	return n
}

// Gauge returns the last observed values of gauge name, one per matching data point.
func (r *Recorder) Gauge(t testing.TB, name string, match Attrs) []float64 {
	t.Helper()
	var out []float64
	for _, m := range r.Metrics(t) {
		if m.Name != name {
			continue
		}
		if d, ok := m.Data.(metricdata.Gauge[float64]); ok {
			for _, p := range d.DataPoints {
				if match.matches(p.Attributes) {
					out = append(out, p.Value)
				}
			}
		}
	}
	return out
}

// GaugeAttrs returns the attribute sets (rendered k=v) of every data point of gauge name.
func (r *Recorder) GaugeAttrs(t testing.TB, name string) []map[string]string {
	t.Helper()
	var out []map[string]string
	for _, m := range r.Metrics(t) {
		if m.Name != name {
			continue
		}
		if d, ok := m.Data.(metricdata.Gauge[float64]); ok {
			for _, p := range d.DataPoints {
				mm := map[string]string{}
				for _, kv := range p.Attributes.ToSlice() {
					mm[string(kv.Key)] = kv.Value.Emit()
				}
				out = append(out, mm)
			}
		}
	}
	return out
}

// SpansNamed returns the ended spans called name.
func (r *Recorder) SpansNamed(name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range r.Spans() {
		if s.Name() == name {
			out = append(out, s)
		}
	}
	return out
}

// SpanNames lists the distinct names of ended spans (sorted).
func (r *Recorder) SpanNames() []string {
	seen := map[string]bool{}
	for _, s := range r.Spans() {
		seen[s.Name()] = true
	}
	var out []string
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// SpanAttr returns the string value of attribute key on s ("" if absent).
func SpanAttr(s sdktrace.ReadOnlySpan, key string) string {
	for _, a := range s.Attributes() {
		if string(a.Key) == key {
			return a.Value.Emit()
		}
	}
	return ""
}

// HasEvent reports whether s carries an event called name.
func HasEvent(s sdktrace.ReadOnlySpan, name string) bool {
	for _, e := range s.Events() {
		if e.Name == name {
			return true
		}
	}
	return false
}
