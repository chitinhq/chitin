package activities

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/telemetry"
)

// TestOTLPTickTelemetrySink_ExportsSpan proves the concrete sink projects a
// TickRecord onto an OTLP span and POSTs it to the collector (spec 076 FR-015,
// spec 070 FR-008).
func TestOTLPTickTelemetrySink_ExportsSpan(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies [][]byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := NewOTLPTickTelemetrySink(telemetry.NewExporterForEndpoint(srv.URL))
	rec := TickRecord{
		SchedulerRunID:    "run-z",
		Tick:              4,
		Frontier:          []string{"a", "b", "c"},
		Dispatched:        []DispatchRecord{{NodeID: "a", DriverID: "claudecode"}},
		BlockedUnroutable: []string{"x"},
		Completed:         []string{"d"},
		Stalled:           true,
	}
	if err := sink.Emit(context.Background(), rec); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	mu.Lock()
	got := len(bodies)
	var payload []byte
	if got > 0 {
		payload = bodies[0]
	}
	mu.Unlock()
	if got != 1 {
		t.Fatalf("collector received %d requests, want 1", got)
	}

	// The body must be valid JSON with a span named scheduler.tick.
	var req map[string]any
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("exported body is not valid JSON: %v", err)
	}
	if !jsonContains(string(payload), `"name":"scheduler.tick"`) {
		t.Errorf("exported span is not named scheduler.tick: %s", payload)
	}
	if !jsonContains(string(payload), `"run-z"`) {
		t.Errorf("exported span does not carry the run id: %s", payload)
	}
}

// TestOTLPTickTelemetrySink_NoCollectorIsNoOp proves a sink with no configured
// exporter (the disabled state) emits silently without faulting — the
// scheduler runs telemetry-disabled rather than failing.
func TestOTLPTickTelemetrySink_NoCollectorIsNoOp(t *testing.T) {
	sink := NewOTLPTickTelemetrySink(telemetry.NewExporterForEndpoint(""))
	if err := sink.Emit(context.Background(), TickRecord{SchedulerRunID: "run", Tick: 1}); err != nil {
		t.Errorf("Emit on a disabled sink must no-op, got %v", err)
	}
}

// TestOTLPTickTelemetrySink_ExportFaultSwallowed proves a collector returning
// an HTTP error does not propagate from Emit — telemetry is a non-
// authoritative projection (spec 070 FR-008), so an export fault is logged and
// dropped, never stalling the scheduler.
func TestOTLPTickTelemetrySink_ExportFaultSwallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	sink := NewOTLPTickTelemetrySink(telemetry.NewExporterForEndpoint(srv.URL))
	if err := sink.Emit(context.Background(), TickRecord{SchedulerRunID: "run", Tick: 1}); err != nil {
		t.Errorf("Emit must swallow an export fault, got %v", err)
	}
}

// jsonContains is a small substring check for asserting on a JSON payload.
func jsonContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
