package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// captureServer is an httptest server that records every OTLP request body it
// receives so a test can assert on the exported payload.
type captureServer struct {
	mu     sync.Mutex
	bodies [][]byte
	status int // HTTP status to return; 0 → 200
	srv    *httptest.Server
}

func newCaptureServer(t *testing.T) *captureServer {
	t.Helper()
	c := &captureServer{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		status := c.status
		c.mu.Unlock()
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *captureServer) received() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.bodies))
	copy(out, c.bodies)
	return out
}

// sampleSpan builds a representative orchestrator span for export tests.
func sampleSpan() Span {
	ts := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	return Span{
		TraceID: TraceIDForRun("run-x"),
		SpanID:  SpanIDForTick("run-x", 3),
		Name:    "scheduler.tick",
		Start:   ts,
		End:     ts,
		Attributes: []Attr{
			StringAttr("scheduler.run_id", "run-x"),
			IntAttr("scheduler.tick", 3),
		},
	}
}

// TestExporter_ExportSpansPostsOTLP proves a configured exporter POSTs a
// well-formed OTLP/HTTP+JSON ExportTraceServiceRequest to the collector.
func TestExporter_ExportSpansPostsOTLP(t *testing.T) {
	srv := newCaptureServer(t)
	x := NewExporterForEndpoint(srv.srv.URL)
	if x == nil {
		t.Fatal("NewExporterForEndpoint returned nil for a non-empty endpoint")
	}

	if err := x.ExportSpans(context.Background(), []Span{sampleSpan()}); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}

	bodies := srv.received()
	if len(bodies) != 1 {
		t.Fatalf("collector received %d requests, want 1", len(bodies))
	}

	var req otlpRequest
	if err := json.Unmarshal(bodies[0], &req); err != nil {
		t.Fatalf("exported body is not valid JSON OTLP request: %v", err)
	}
	if len(req.ResourceSpans) != 1 {
		t.Fatalf("resourceSpans = %d, want 1", len(req.ResourceSpans))
	}
	rs := req.ResourceSpans[0]
	if got := serviceNameAttr(rs.Resource.Attributes); got != serviceName {
		t.Errorf("service.name = %q, want %q", got, serviceName)
	}
	if len(rs.ScopeSpans) != 1 || len(rs.ScopeSpans[0].Spans) != 1 {
		t.Fatalf("scopeSpans/spans shape wrong: %+v", rs.ScopeSpans)
	}
	span := rs.ScopeSpans[0].Spans[0]
	if span.Name != "scheduler.tick" {
		t.Errorf("span name = %q, want scheduler.tick", span.Name)
	}
	if len(span.TraceID) != 32 {
		t.Errorf("traceId %q is %d chars, want 32", span.TraceID, len(span.TraceID))
	}
	if len(span.SpanID) != 16 {
		t.Errorf("spanId %q is %d chars, want 16", span.SpanID, len(span.SpanID))
	}
}

// TestExporter_IntAttributeEncoding proves int attributes encode as decimal
// strings, the OTLP/HTTP+JSON wire form for int64.
func TestExporter_IntAttributeEncoding(t *testing.T) {
	srv := newCaptureServer(t)
	x := NewExporterForEndpoint(srv.srv.URL)

	if err := x.ExportSpans(context.Background(), []Span{sampleSpan()}); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}
	var req otlpRequest
	if err := json.Unmarshal(srv.received()[0], &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	attrs := req.ResourceSpans[0].ScopeSpans[0].Spans[0].Attributes
	var found bool
	for _, a := range attrs {
		if a.Key == "scheduler.tick" {
			found = true
			if a.Value.IntValue == nil {
				t.Fatal("scheduler.tick attr has no intValue")
			}
			if *a.Value.IntValue != "3" {
				t.Errorf("scheduler.tick intValue = %q, want \"3\"", *a.Value.IntValue)
			}
		}
	}
	if !found {
		t.Error("scheduler.tick attribute not present in exported span")
	}
}

// TestExporter_DisabledWhenNoEndpoint proves an empty endpoint yields a nil
// exporter — the explicit telemetry-disabled state — and that the nil
// exporter is a safe no-op sink.
func TestExporter_DisabledWhenNoEndpoint(t *testing.T) {
	if x := NewExporterForEndpoint(""); x != nil {
		t.Fatal("NewExporterForEndpoint(\"\") must return nil")
	}
	var x *Exporter
	if x.Enabled() {
		t.Error("nil exporter reports Enabled() true")
	}
	if x.Endpoint() != "" {
		t.Error("nil exporter has a non-empty Endpoint()")
	}
	if err := x.ExportSpans(context.Background(), []Span{sampleSpan()}); err != nil {
		t.Errorf("ExportSpans on nil exporter must no-op, got %v", err)
	}
}

// TestNewExporter_FromEnv proves NewExporter honours the OTLP env vars: nil
// when unset, configured when OTEL_EXPORTER_OTLP_ENDPOINT is set (with the
// /v1/traces signal path appended), and the traces-specific var taking
// precedence.
func TestNewExporter_FromEnv(t *testing.T) {
	t.Run("disabled when unset", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
		if x := NewExporter(); x != nil {
			t.Error("NewExporter must return nil when no OTLP env var is set")
		}
	})
	t.Run("base endpoint appends signal path", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
		x := NewExporter()
		if x == nil || x.Endpoint() != "http://collector:4318/v1/traces" {
			t.Errorf("endpoint = %q, want http://collector:4318/v1/traces", x.Endpoint())
		}
	})
	t.Run("traces-specific var wins", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://base:4318")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://traces:4318/v1/traces")
		x := NewExporter()
		if x == nil || x.Endpoint() != "http://traces:4318/v1/traces" {
			t.Errorf("endpoint = %q, want the traces-specific var verbatim", x.Endpoint())
		}
	})
}

// TestExporter_HTTPErrorSurfaces proves a collector returning >= 400 produces
// an error from ExportSpans — the caller decides to log and drop.
func TestExporter_HTTPErrorSurfaces(t *testing.T) {
	srv := newCaptureServer(t)
	srv.status = http.StatusInternalServerError
	x := NewExporterForEndpoint(srv.srv.URL)

	if err := x.ExportSpans(context.Background(), []Span{sampleSpan()}); err == nil {
		t.Fatal("ExportSpans must return an error on an HTTP 500 from the collector")
	}
}

// TestExporter_EmptySpanSliceIsNoOp proves exporting zero spans makes no HTTP
// call.
func TestExporter_EmptySpanSliceIsNoOp(t *testing.T) {
	srv := newCaptureServer(t)
	x := NewExporterForEndpoint(srv.srv.URL)
	if err := x.ExportSpans(context.Background(), nil); err != nil {
		t.Fatalf("ExportSpans(nil): %v", err)
	}
	if got := len(srv.received()); got != 0 {
		t.Errorf("collector received %d requests for an empty span slice, want 0", got)
	}
}

// TestTraceAndSpanIDs_Deterministic proves the id derivation is stable for a
// given input and distinct across runs/ticks — the property a trace grouping
// relies on.
func TestTraceAndSpanIDs_Deterministic(t *testing.T) {
	if TraceIDForRun("run-a") != TraceIDForRun("run-a") {
		t.Error("TraceIDForRun is not deterministic")
	}
	if TraceIDForRun("run-a") == TraceIDForRun("run-b") {
		t.Error("TraceIDForRun collides across distinct run ids")
	}
	if len(TraceIDForRun("run-a")) != 32 {
		t.Errorf("trace id length = %d, want 32", len(TraceIDForRun("run-a")))
	}
	if SpanIDForTick("run-a", 1) != SpanIDForTick("run-a", 1) {
		t.Error("SpanIDForTick is not deterministic")
	}
	if SpanIDForTick("run-a", 1) == SpanIDForTick("run-a", 2) {
		t.Error("SpanIDForTick collides across distinct ticks")
	}
	if SpanIDForTick("run-a", 1) == SpanIDForTick("run-b", 1) {
		t.Error("SpanIDForTick collides across distinct runs")
	}
	if len(SpanIDForTick("run-a", 1)) != 16 {
		t.Errorf("span id length = %d, want 16", len(SpanIDForTick("run-a", 1)))
	}
}

// serviceNameAttr extracts the service.name string attribute value, or "".
func serviceNameAttr(attrs []otlpAttr) string {
	for _, a := range attrs {
		if a.Key == "service.name" {
			return a.Value.StringValue
		}
	}
	return ""
}
