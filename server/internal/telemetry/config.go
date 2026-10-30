package telemetry

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Protocols understood by OTEL_EXPORTER_OTLP_PROTOCOL.
const (
	ProtoHTTP = "http/protobuf"
	ProtoGRPC = "grpc"
)

// Signal is the resolved state of one telemetry signal.
type Signal struct {
	Enabled  bool
	Endpoint string // raw endpoint env value ("" = SDK default for the protocol); may hold credentials, never print it
	Protocol string // ProtoHTTP or ProtoGRPC
}

// Config is what Setup needs. The OTLP specifics (endpoint, headers, TLS, timeouts, compression, sampler,
// batch sizes, export interval) are read by the OpenTelemetry SDK from the standard OTEL_* variables;
// Config only records the decisions the SDK does not make for us: on/off and which exporter flavour.
type Config struct {
	Disabled       bool   // OTEL_SDK_DISABLED=true
	ServiceName    string // OTEL_SERVICE_NAME, default "calendard"
	ServiceVersion string
	Traces         Signal
	Metrics        Signal
	Logs           Signal
	Sampler        string // OTEL_TRACES_SAMPLER, default parentbased_always_on
	SamplerArg     string // OTEL_TRACES_SAMPLER_ARG
	Headers        int    // number of configured OTLP headers (values are never kept)
	// ShutdownTimeout bounds the final flush (default 5s).
	ShutdownTimeout time.Duration
}

// Enabled reports whether anything will be exported.
func (c Config) Enabled() bool {
	return !c.Disabled && (c.Traces.Enabled || c.Metrics.Enabled || c.Logs.Enabled)
}

// Signals lists the enabled signals ("traces,metrics,logs").
func (c Config) Signals() []string {
	var s []string
	if c.Traces.Enabled {
		s = append(s, "traces")
	}
	if c.Metrics.Enabled {
		s = append(s, "metrics")
	}
	if c.Logs.Enabled {
		s = append(s, "logs")
	}
	return s
}

func getenv(k string) string { return strings.TrimSpace(os.Getenv(k)) }

func isTrue(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "true") }

// ConfigFromEnv reads the standard OTEL_* environment. Telemetry is enabled only when an OTLP endpoint
// (general or per-signal) is set, the SDK is not disabled, and the signal's exporter is not "none".
func ConfigFromEnv(version string) Config {
	c := Config{
		Disabled:        isTrue(getenv("OTEL_SDK_DISABLED")),
		ServiceName:     getenv("OTEL_SERVICE_NAME"),
		ServiceVersion:  version,
		Sampler:         strings.ToLower(getenv("OTEL_TRACES_SAMPLER")),
		SamplerArg:      getenv("OTEL_TRACES_SAMPLER_ARG"),
		ShutdownTimeout: 5 * time.Second,
	}
	if c.ServiceName == "" {
		c.ServiceName = "calendard"
	}
	if c.Sampler == "" {
		c.Sampler = "parentbased_always_on"
	}
	c.Traces = signalFromEnv("TRACES")
	c.Metrics = signalFromEnv("METRICS")
	c.Logs = signalFromEnv("LOGS")
	if h := getenv("OTEL_EXPORTER_OTLP_HEADERS"); h != "" {
		c.Headers = len(strings.Split(h, ","))
	}
	if getenv("CALENDAR_OTEL_TOKEN") != "" && !hasAuthHeader(getenv("OTEL_EXPORTER_OTLP_HEADERS")) {
		c.Headers++ // applyToken adds an Authorization header in Setup
	}
	if c.Disabled {
		c.Traces.Enabled, c.Metrics.Enabled, c.Logs.Enabled = false, false, false
	}
	return c
}

func signalFromEnv(sig string) Signal {
	s := Signal{Endpoint: getenv("OTEL_EXPORTER_OTLP_" + sig + "_ENDPOINT")}
	general := getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if s.Endpoint == "" {
		s.Endpoint = general
	}
	s.Enabled = s.Endpoint != ""
	if strings.EqualFold(getenv("OTEL_"+sig+"_EXPORTER"), "none") {
		s.Enabled = false
	}
	p := strings.ToLower(getenv("OTEL_EXPORTER_OTLP_" + sig + "_PROTOCOL"))
	if p == "" {
		p = strings.ToLower(getenv("OTEL_EXPORTER_OTLP_PROTOCOL"))
	}
	if p == ProtoGRPC {
		s.Protocol = ProtoGRPC
	} else { // http/protobuf is the default; http/json is not supported and falls back to protobuf
		s.Protocol = ProtoHTTP
	}
	return s
}

// primary is the first enabled signal (traces, metrics, logs): the one shown by doctor and the startup line.
func (c Config) primary() Signal {
	for _, s := range []Signal{c.Traces, c.Metrics, c.Logs} {
		if s.Enabled {
			return s
		}
	}
	return Signal{}
}

// EndpointHost is host[:port] of the primary endpoint with no scheme, path, query or credentials.
func (c Config) EndpointHost() string { return hostOf(c.primary().Endpoint) }

// Protocol is the primary signal's protocol.
func (c Config) Protocol() string { return c.primary().Protocol }

func hostOf(raw string) string {
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "invalid"
	}
	return u.Host
}

// Summary is the one-line startup text: "telemetry enabled endpoint=... protocol=... signals=..." or
// "telemetry disabled".
func (c Config) Summary() string {
	if !c.Enabled() {
		return "telemetry disabled"
	}
	return fmt.Sprintf("telemetry enabled endpoint=%s protocol=%s signals=%s", c.EndpointHost(), c.Protocol(), strings.Join(c.Signals(), ","))
}

// Describe is the doctor text. It never includes header values or URL paths/credentials.
func (c Config) Describe() string {
	if c.Disabled {
		return "disabled (OTEL_SDK_DISABLED=true)"
	}
	if !c.Enabled() {
		return "disabled (no OTEL_EXPORTER_OTLP_ENDPOINT set)"
	}
	s := fmt.Sprintf("enabled: endpoint %s, protocol %s, sampler %s", c.EndpointHost(), c.Protocol(), c.Sampler)
	if c.SamplerArg != "" {
		s += "(" + c.SamplerArg + ")"
	}
	s += ", signals " + strings.Join(c.Signals(), ",")
	if c.Headers > 0 {
		s += fmt.Sprintf(", %d header(s) set (masked)", c.Headers)
	}
	return s + ", service " + c.ServiceName
}

// Probe makes a best-effort reachability check of the primary endpoint (TCP dial for gRPC, HTTP HEAD for
// http/protobuf) within 2 seconds. It sends no headers and no data; the error, if any, is advisory.
func (c Config) Probe(ctx context.Context) error {
	p := c.primary()
	if !c.Enabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	raw := p.Endpoint
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("endpoint is not a valid URL")
	}
	if p.Protocol == ProtoGRPC {
		host := u.Host
		if u.Port() == "" {
			host = net.JoinHostPort(u.Hostname(), "4317")
		}
		var d net.Dialer
		conn, err := d.DialContext(ctx, "tcp", host)
		if err != nil {
			return scrubErr(err)
		}
		return conn.Close()
	}
	u.User = nil
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, u.String(), nil)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return scrubErr(err)
	}
	return resp.Body.Close() // any HTTP status proves the collector is listening
}

// scrubErr keeps only the reason of a network error (url.Error would repeat the endpoint).
func scrubErr(err error) error {
	if ue, ok := err.(*url.Error); ok {
		err = ue.Err
	}
	return fmt.Errorf("%s", Sanitize(err.Error()))
}
