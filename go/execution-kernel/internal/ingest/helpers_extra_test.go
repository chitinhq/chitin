package ingest

import (
	"strings"
	"testing"
)

func TestGetKwargInt(t *testing.T) {
	m := map[string]interface{}{
		"float64":  float64(42),
		"float32":  float32(7),
		"int":      99,
		"int64":    int64(123),
		"string":   "not-a-number",
	}

	tests := []struct {
		key  string
		want int64
		ok   bool
	}{
		{"float64", 42, true},
		{"float32", 7, true},
		{"int", 99, true},
		{"int64", 123, true},
		{"missing", 0, false},
		{"string", 0, false},
	}
	for _, tc := range tests {
		got, ok := getKwargInt(m, tc.key)
		if ok != tc.ok || got != tc.want {
			t.Errorf("getKwargInt(%q) = (%d, %v), want (%d, %v)", tc.key, got, ok, tc.want, tc.ok)
		}
	}
}

func TestGetKwargFloat(t *testing.T) {
	m := map[string]interface{}{
		"float64":  float64(42.5),
		"float32":  float32(7.5),
		"int":      99,
		"int64":    int64(123),
		"string":   "not-a-number",
	}

	tests := []struct {
		key  string
		want float64
		ok   bool
	}{
		{"float64", 42.5, true},
		{"float32", float64(float32(7.5)), true},
		{"int", 99.0, true},
		{"int64", 123.0, true},
		{"missing", 0, false},
		{"string", 0, false},
	}
	for _, tc := range tests {
		got, ok := getKwargFloat(m, tc.key)
		if ok != tc.ok {
			t.Errorf("getKwargFloat(%q) ok = %v, want %v", tc.key, ok, tc.ok)
		}
		if ok && got != tc.want {
			t.Errorf("getKwargFloat(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestBuildChainID(t *testing.T) {
	traceID := []byte{0x01, 0x02, 0x03, 0x04}
	spanID := []byte{0x05, 0x06}
	got := buildChainID(traceID, spanID)
	if got != "otel:01020304:0506" {
		t.Errorf("buildChainID = %q, want 'otel:01020304:0506'", got)
	}
}

func TestBuildOtelLabels(t *testing.T) {
	labels := buildOtelLabels("trace123", "span456", "parent789")
	if labels["source"] != "otel" {
		t.Errorf("source = %q, want otel", labels["source"])
	}
	if labels["dialect"] != "openclaw" {
		t.Errorf("dialect = %q, want openclaw", labels["dialect"])
	}
	if labels["otel_trace_id"] != "trace123" {
		t.Errorf("trace_id = %q, want trace123", labels["otel_trace_id"])
	}
	if labels["otel_parent_span_id"] != "parent789" {
		t.Errorf("parent = %q, want parent789", labels["otel_parent_span_id"])
	}

	// Empty parent should omit the key
	labels2 := buildOtelLabels("trace123", "span456", "")
	if _, exists := labels2["otel_parent_span_id"]; exists {
		t.Error("parent_span_id should be absent when empty")
	}
}

func TestGetResourceStringAttr_NilResource(t *testing.T) {
	if got := getResourceStringAttr(nil, "any"); got != "" {
		t.Errorf("nil resource should return empty, got %q", got)
	}
}

func TestIsAllZero(t *testing.T) {
	if !isAllZero([]byte{0, 0, 0}) {
		t.Error("all-zero bytes should be true")
	}
	if isAllZero([]byte{0, 1, 0}) {
		t.Error("mixed bytes should be false")
	}
	if !isAllZero([]byte{}) {
		t.Error("empty slice should be true")
	}
}

func TestSessionStuckInterfaceMethods(t *testing.T) {
	s := SessionStuck{
		TraceIDBytes:  []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanIDBytes:   []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00, 0x11},
		TsStr:         "2026-05-09T12:00:00Z",
		SurfaceStr:    "openclaw",
		SpanIDHex:     "aabbccddeeff0011",
		TraceID:       "0102030405060708090a0b0c0d0e0f10",
		ParentSpanIDHex: "parent123",
		State:         "stuck",
		AgeMs:         5000,
	}

	if s.EventType() != "session_stuck" {
		t.Errorf("EventType = %q, want session_stuck", s.EventType())
	}
	if s.Ts() != "2026-05-09T12:00:00Z" {
		t.Errorf("Ts = %q, want 2026-05-09T12:00:00Z", s.Ts())
	}
	if s.Surface() != "openclaw" {
		t.Errorf("Surface = %q, want openclaw", s.Surface())
	}
	if s.SpanID() != "aabbccddeeff0011" {
		t.Errorf("SpanID = %q, want aabbccddeeff0011", s.SpanID())
	}
	chainID := s.ChainID()
	if chainID != "otel:0102030405060708090a0b0c0d0e0f10:aabbccddeeff0011" {
		t.Errorf("ChainID = %q, unexpected", chainID)
	}

	payload, err := s.Payload()
	if err != nil {
		t.Fatalf("Payload error: %v", err)
	}
	if string(payload) == "" {
		t.Error("Payload should not be empty")
	}

	// Test with QueueDepth present
	s2 := SessionStuck{
		TraceIDBytes: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanIDBytes:   []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00, 0x11},
		TsStr:         "2026-05-09T12:00:00Z",
		SurfaceStr:    "openclaw",
		SpanIDHex:     "aabbccddeeff0011",
		TraceID:       "0102030405060708090a0b0c0d0e0f10",
		State:         "stuck",
		AgeMs:         3000,
		QueueDepthPresent: true,
		QueueDepth:   5,
	}
	payload2, err := s2.Payload()
	if err != nil {
		t.Fatalf("Payload with QueueDepth error: %v", err)
	}
	if !strings.Contains(string(payload2), "queue_depth") {
		t.Errorf("Payload should contain queue_depth when present: %s", string(payload2))
	}

	labels := s.Labels()
	if labels["source"] != "otel" {
		t.Errorf("label source = %q, want otel", labels["source"])
	}
}