package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var (
	enabled    atomic.Bool
	logProv    atomic.Pointer[otellogProvider]
	appLogger  atomic.Pointer[slog.Logger] // stdout + trace ids + OTel bridge (RecordError)
	plainLog   atomic.Pointer[slog.Logger] // stdout only: used for exporter problems so they never feed the exporter
	errHandler = &limitedErrors{every: time.Minute}
)

type otellogProvider struct{ p otellog.LoggerProvider }

// Enabled reports whether Setup turned telemetry on in this process.
func Enabled() bool { return enabled.Load() }

func logger() *slog.Logger {
	if l := appLogger.Load(); l != nil {
		return l
	}
	return slog.Default()
}

// limitedErrors is the OTel error handler: exporter and collector failures are logged at most once a
// minute per distinct message so an outage cannot flood the log or slow the hub.
type limitedErrors struct {
	every time.Duration
	mu    sync.Mutex
	last  map[string]time.Time
}

func (l *limitedErrors) Handle(err error) {
	if err == nil {
		return
	}
	msg := Sanitize(err.Error())
	l.mu.Lock()
	if l.last == nil {
		l.last = map[string]time.Time{}
	}
	if len(l.last) > 64 {
		clear(l.last)
	}
	now := time.Now()
	if t, ok := l.last[msg]; ok && now.Sub(t) < l.every {
		l.mu.Unlock()
		return
	}
	l.last[msg] = now
	l.mu.Unlock()
	// plain slog, no ctx: this must never feed back into the OTel log bridge
	lg := plainLog.Load()
	if lg == nil {
		lg = slog.Default()
	}
	lg.Warn("telemetry export problem (hub unaffected)", "err", msg)
}

func noop(context.Context) error { return nil }

// Setup installs the OpenTelemetry SDK when cfg is enabled and returns the function that flushes and
// stops it (bounded by cfg.ShutdownTimeout, default 5 s). When disabled it installs nothing, starts no
// goroutine and returns a no-op shutdown. It never blocks on the collector: exporters connect lazily and
// export from background batch processors with bounded queues.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if !cfg.Enabled() {
		return noop, nil
	}
	res, err := newResource(ctx, cfg)
	if err != nil {
		return noop, err
	}
	var (
		tp *sdktrace.TracerProvider
		mp *sdkmetric.MeterProvider
		lp *sdklog.LoggerProvider
	)
	if cfg.Traces.Enabled {
		exp, err := newTraceExporter(ctx, cfg.Traces)
		if err != nil {
			return noop, err
		}
		tp = sdktrace.NewTracerProvider(sdktrace.WithResource(res),
			sdktrace.WithSpanProcessor(&scrubProcessor{next: sdktrace.NewBatchSpanProcessor(exp)}))
	}
	if cfg.Metrics.Enabled {
		exp, err := newMetricExporter(ctx, cfg.Metrics)
		if err != nil {
			return noop, err
		}
		mp = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)))
	}
	if cfg.Logs.Enabled {
		exp, err := newLogExporter(ctx, cfg.Logs)
		if err != nil {
			return noop, err
		}
		lp = sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)))
	}
	uninstall := install(tp, mp, lp)
	if mp != nil {
		if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
			otel.Handle(err)
		}
	}
	timeout := cfg.ShutdownTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	var once sync.Once
	var serr error
	return func(c context.Context) error {
		once.Do(func() {
			// the caller's ctx may already be cancelled (SIGTERM): the flush gets its own budget
			fctx, cancel := context.WithTimeout(context.WithoutCancel(c), timeout)
			defer cancel()
			uninstall()
			var errs []error
			if tp != nil {
				errs = append(errs, tp.Shutdown(fctx))
			}
			if mp != nil {
				errs = append(errs, mp.Shutdown(fctx))
			}
			if lp != nil {
				errs = append(errs, lp.Shutdown(fctx))
			}
			serr = errors.Join(errs...)
		})
		return serr
	}, nil
}

// install makes the given providers the process-wide ones and returns a function that turns telemetry
// off again. Tests use it directly with in-memory providers.
func install(tp *sdktrace.TracerProvider, mp *sdkmetric.MeterProvider, lp *sdklog.LoggerProvider) func() {
	otel.SetErrorHandler(errHandler)
	otel.SetTextMapPropagator(propagation.TraceContext{}) // no baggage: nothing user-defined crosses the wire
	if tp != nil {
		otel.SetTracerProvider(tp)
	}
	if mp != nil {
		otel.SetMeterProvider(mp)
	}
	if lp != nil {
		logglobal.SetLoggerProvider(lp)
		logProv.Store(&otellogProvider{p: lp})
	}
	generation.Add(1)
	enabled.Store(true)
	return func() {
		enabled.Store(false)
		logProv.Store(nil)
		generation.Add(1)
	}
}

func newResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
			attribute.String("service.version", cfg.ServiceVersion),
			attribute.String("service.instance.id", ulid.Make().String()),
		),
		resource.WithFromEnv(), // OTEL_RESOURCE_ATTRIBUTES (deployment.environment=...) and OTEL_SERVICE_NAME
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithOS(),
		// process pid/runtime only: never the command line or the owner
		resource.WithProcessPID(),
		resource.WithProcessExecutableName(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithProcessRuntimeDescription(),
	)
}

func newTraceExporter(ctx context.Context, s Signal) (sdktrace.SpanExporter, error) {
	if s.Protocol == ProtoGRPC {
		return otlptracegrpc.New(ctx)
	}
	return otlptracehttp.New(ctx)
}

func newMetricExporter(ctx context.Context, s Signal) (sdkmetric.Exporter, error) {
	if s.Protocol == ProtoGRPC {
		return otlpmetricgrpc.New(ctx)
	}
	return otlpmetrichttp.New(ctx)
}

func newLogExporter(ctx context.Context, s Signal) (sdklog.Exporter, error) {
	if s.Protocol == ProtoGRPC {
		return otlploggrpc.New(ctx)
	}
	return otlploghttp.New(ctx)
}

// InstallProviders installs already-built providers as the process-wide ones, for tests with in-memory
// exporters (see package telemetrytest). It returns the function that uninstalls them.
func InstallProviders(tp *sdktrace.TracerProvider, mp *sdkmetric.MeterProvider, lp *sdklog.LoggerProvider) func() {
	return install(tp, mp, lp)
}
