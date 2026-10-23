package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// RegisterProcessGauges adds calendar.process.uptime_seconds and calendar.build.info{version}=1. Call once
// after Setup. A reset of the uptime gauge is how "the hub restarted" is alerted on.
func RegisterProcessGauges(version string) {
	start := time.Now()
	GaugeSet("process.uptime_seconds", "Seconds since the hub process started", "s", func(context.Context) []Observation {
		return []Observation{{Value: time.Since(start).Seconds()}}
	})
	v := Scrub(version)
	if len(v) > 64 {
		v = v[:64]
	}
	GaugeSet("build.info", "Always 1; the version label carries the running build", "1", func(context.Context) []Observation {
		return []Observation{{Value: 1, Attrs: []attribute.KeyValue{attribute.String("version", v)}}}
	})
}
