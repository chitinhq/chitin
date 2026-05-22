// Package telemetry is the Chitin Orchestrator's run/tick telemetry export
// surface (spec 070 FR-008). It projects orchestrator scheduling events onto
// OTLP/HTTP+JSON spans and POSTs them to a configured collector.
//
// One-way bridge: the orchestrator's Temporal workflow history is canonical;
// the OTLP projection is a non-authoritative observability feed. A telemetry
// export failure MUST NOT affect orchestration — every Export call logs and
// drops its error, returning nil regardless. This mirrors the kernel's F4
// OTEL emit MVP (execution-kernel internal/emit/otel.go), the model this
// exporter is built on.
//
// The exporter is a no-op when no collector is configured: NewExporter
// returns nil if neither OTEL_EXPORTER_OTLP_TRACES_ENDPOINT nor
// OTEL_EXPORTER_OTLP_ENDPOINT is set, and every method is nil-safe.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// otelHTTPTimeout bounds both the http.Client timeout and the per-export
// context deadline. Single source of truth so the two can never drift —
// drift would either fire the context first and leak an in-flight request,
// or fire the client first and leak the context goroutine. 500ms catches a
// healthy localhost RTT with margin while bounding the worst case.
const otelHTTPTimeout = 500 * time.Millisecond

// serviceName is the OTLP resource service.name stamped on every exported
// span — how an orchestrator span is told apart from a kernel span in a
// shared collector.
const serviceName = "chitin-orchestrator"

// Exporter is the orchestrator's OTLP/HTTP telemetry exporter. A nil Exporter
// is the "telemetry disabled" state: every method is a safe no-op on nil, so
// callers never branch on configuration.
type Exporter struct {
	endpoint string
	client   *http.Client
}

// NewExporter constructs an Exporter from the OTLP endpoint env vars. It
// returns nil — the explicit "disabled" value — when neither
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT nor OTEL_EXPORTER_OTLP_ENDPOINT is set.
// A nil *Exporter is a valid TickTelemetrySink: its methods no-op.
func NewExporter() *Exporter {
	ep := endpointFromEnv()
	if ep == "" {
		return nil
	}
	return &Exporter{
		endpoint: ep,
		client:   &http.Client{Timeout: otelHTTPTimeout},
	}
}

// NewExporterForEndpoint constructs an Exporter that POSTs to an explicit
// endpoint, bypassing env-var resolution. It exists for tests (point it at an
// httptest server) and for callers that resolve the endpoint themselves. An
// empty endpoint returns nil — the disabled state.
func NewExporterForEndpoint(endpoint string) *Exporter {
	if endpoint == "" {
		return nil
	}
	return &Exporter{
		endpoint: endpoint,
		client:   &http.Client{Timeout: otelHTTPTimeout},
	}
}

// Enabled reports whether the exporter will actually POST spans. It is false
// on a nil receiver and on an exporter with no endpoint — callers can use it
// to skip building a span payload they would only drop.
func (x *Exporter) Enabled() bool {
	return x != nil && x.endpoint != ""
}

// Endpoint returns the configured collector endpoint, or "" when the exporter
// is disabled. Exposed for diagnostics and startup logging.
func (x *Exporter) Endpoint() string {
	if x == nil {
		return ""
	}
	return x.endpoint
}

// ExportSpans projects spans onto an OTLP/HTTP+JSON ExportTraceServiceRequest
// and POSTs it to the configured collector. It returns an error only on a
// genuine transport or encoding fault; callers on the orchestration path
// should log and drop it rather than propagate. Safe to call on a nil
// Exporter (no-op, returns nil) and with an empty span slice (no-op).
func (x *Exporter) ExportSpans(ctx context.Context, spans []Span) error {
	if !x.Enabled() {
		return nil
	}
	if len(spans) == 0 {
		return nil
	}
	body, err := encodeRequest(serviceName, spans)
	if err != nil {
		return fmt.Errorf("telemetry: encoding OTLP request: %w", err)
	}
	postCtx, cancel := context.WithTimeout(ctx, otelHTTPTimeout)
	defer cancel()
	if err := x.post(postCtx, body); err != nil {
		return fmt.Errorf("telemetry: posting OTLP request: %w", err)
	}
	return nil
}

// post sends body as application/json to x.endpoint. It returns an error on a
// transport failure or an HTTP status >= 400.
func (x *Exporter) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, x.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := x.client.Do(req)
	if err != nil {
		return err
	}
	// Drain the body before closing so the connection can be reused.
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("otlp http %d", resp.StatusCode)
	}
	return nil
}

// endpointFromEnv resolves the OTLP traces endpoint from the environment,
// preferring the traces-specific var. It mirrors the OpenTelemetry env-var
// contract the kernel follows: a bare OTEL_EXPORTER_OTLP_ENDPOINT is a base
// URL onto which the /v1/traces signal path is appended. Returns "" when
// neither var is set.
func endpointFromEnv() string {
	if e := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); e != "" {
		return e
	}
	if e := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); e != "" {
		return strings.TrimRight(e, "/") + "/v1/traces"
	}
	return ""
}

// Span is a transport-agnostic span the orchestrator builds from a scheduling
// event. It is encoded onto the OTLP/HTTP+JSON wire types by encodeRequest;
// callers never construct OTLP types directly.
type Span struct {
	// TraceID is 32 lowercase hex chars (16 bytes) — typically derived from
	// the scheduler run id so a run's ticks share a trace.
	TraceID string
	// SpanID is 16 lowercase hex chars (8 bytes), unique within the trace.
	SpanID string
	// ParentSpanID is the enclosing span, or "" for a root span.
	ParentSpanID string
	// Name is the span name — e.g. "scheduler.tick".
	Name string
	// Start and End bound the span. For a point-in-time event set both equal.
	Start time.Time
	End   time.Time
	// Attributes are the span's string- and int-valued attributes.
	Attributes []Attr
}

// Attr is one span attribute. Exactly one of Str / Int carries the value;
// IsInt selects which. A zero Attr ("", "", 0, false) encodes as an empty
// string attribute, which is harmless.
type Attr struct {
	Key   string
	Str   string
	Int   int64
	IsInt bool
}

// StringAttr builds a string-valued span attribute.
func StringAttr(key, value string) Attr { return Attr{Key: key, Str: value} }

// IntAttr builds an int-valued span attribute.
func IntAttr(key string, value int64) Attr { return Attr{Key: key, Int: value, IsInt: true} }

// encodeRequest builds the OTLP/HTTP+JSON ExportTraceServiceRequest body for
// spans under the given service name. Per the OTLP/HTTP+JSON encoding:
// lowerCamelCase field names, hex-encoded trace/span ids (not base64), and
// int64 nanosecond timestamps as decimal strings.
func encodeRequest(svc string, spans []Span) ([]byte, error) {
	wire := make([]otlpSpan, 0, len(spans))
	for _, s := range spans {
		wire = append(wire, toOTLPSpan(s))
	}
	req := otlpRequest{
		ResourceSpans: []otlpResourceSpans{{
			Resource: otlpResource{
				Attributes: []otlpAttr{
					{Key: "service.name", Value: otlpValue{StringValue: svc}},
					{Key: "service.version", Value: otlpValue{StringValue: "0.0.0"}},
				},
			},
			ScopeSpans: []otlpScopeSpans{{
				Scope: otlpScope{Name: "chitin-orchestrator"},
				Spans: wire,
			}},
		}},
	}
	return json.Marshal(req)
}

// toOTLPSpan maps a transport-agnostic Span onto the OTLP/HTTP+JSON wire span.
func toOTLPSpan(s Span) otlpSpan {
	attrs := make([]otlpAttr, 0, len(s.Attributes))
	for _, a := range s.Attributes {
		if a.IsInt {
			v := strconv.FormatInt(a.Int, 10)
			attrs = append(attrs, otlpAttr{Key: a.Key, Value: otlpValue{IntValue: &v}})
			continue
		}
		attrs = append(attrs, otlpAttr{Key: a.Key, Value: otlpValue{StringValue: a.Str}})
	}
	return otlpSpan{
		TraceID:           s.TraceID,
		SpanID:            s.SpanID,
		ParentSpanID:      s.ParentSpanID,
		Name:              s.Name,
		Kind:              1, // SPAN_KIND_INTERNAL
		StartTimeUnixNano: strconv.FormatInt(s.Start.UnixNano(), 10),
		EndTimeUnixNano:   strconv.FormatInt(s.End.UnixNano(), 10),
		Attributes:        attrs,
	}
}

// logExportError logs an export failure at a single, greppable prefix. The
// orchestrator path calls this rather than propagating — telemetry is a
// non-authoritative projection (see the package doc).
func logExportError(context string, err error) {
	if err == nil {
		return
	}
	log.Printf("telemetry: %s: %v", context, err)
}

// OTLP/HTTP+JSON wire types. Field tags use lowerCamelCase per the proto3 →
// JSON mapping the OpenTelemetry Protocol Specification mandates.

type otlpRequest struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpAttr `json:"attributes"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name string `json:"name"`
}

type otlpSpan struct {
	TraceID           string     `json:"traceId"`
	SpanID            string     `json:"spanId"`
	ParentSpanID      string     `json:"parentSpanId,omitempty"`
	Name              string     `json:"name"`
	Kind              int        `json:"kind"`
	StartTimeUnixNano string     `json:"startTimeUnixNano"`
	EndTimeUnixNano   string     `json:"endTimeUnixNano"`
	Attributes        []otlpAttr `json:"attributes,omitempty"`
}

type otlpAttr struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	StringValue string  `json:"stringValue,omitempty"`
	IntValue    *string `json:"intValue,omitempty"` // OTLP/HTTP+JSON encodes int64 as a decimal string
}
