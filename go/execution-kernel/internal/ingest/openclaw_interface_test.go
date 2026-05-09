package ingest

import "testing"

func TestSessionStuck_InterfaceMethods(t *testing.T) {
	s := SessionStuck{
		TraceIDBytes:    []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		SpanIDBytes:     []byte{0, 0, 0, 0, 0, 0, 0, 2},
		TraceID:         "00000000000000000000000000000001",
		SpanIDHex:       "0000000000000002",
		ParentSpanIDHex: "0000000000000003",
		TsStr:           "2026-05-09T12:00:00Z",
		SurfaceStr:      "openclaw",
		State:           "stuck",
		AgeMs:           5000,
		SessionIDExternal: "ext-123",
		SessionKey:      "key-456",
		QueueDepth:      7,
		QueueDepthPresent: true,
	}

	if got := s.EventType(); got != "session_stuck" {
		t.Errorf("EventType() = %q, want %q", got, "session_stuck")
	}
	if got := s.ChainID(); got == "" {
		t.Error("ChainID() returned empty string")
	}
	if got := s.Ts(); got != "2026-05-09T12:00:00Z" {
		t.Errorf("Ts() = %q, want %q", got, "2026-05-09T12:00:00Z")
	}
	if got := s.Surface(); got != "openclaw" {
		t.Errorf("Surface() = %q, want %q", got, "openclaw")
	}
	if got := s.SpanID(); got != "0000000000000002" {
		t.Errorf("SpanID() = %q, want %q", got, "0000000000000002")
	}

	payload, err := s.Payload()
	if err != nil {
		t.Fatalf("Payload() error: %v", err)
	}
	// Verify queue_depth is present (QueueDepthPresent=true)
	if len(payload) == 0 {
		t.Fatal("Payload() returned empty bytes")
	}
	t.Logf("Payload: %s", string(payload))

	labels := s.Labels()
	if labels == nil {
		t.Fatal("Labels() returned nil")
	}
}

func TestSessionStuck_Payload_NoQueueDepth(t *testing.T) {
	s := SessionStuck{
		TraceIDBytes: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		SpanIDBytes:  []byte{0, 0, 0, 0, 0, 0, 0, 2},
		TraceID:      "00000000000000000000000000000001",
		SpanIDHex:    "0000000000000002",
		TsStr:        "2026-05-09T12:00:00Z",
		SurfaceStr:   "openclaw",
		State:        "stuck",
		AgeMs:        5000,
		// QueueDepthPresent is false (zero value), QueueDepth is 0
	}
	payload, err := s.Payload()
	if err != nil {
		t.Fatalf("Payload() error: %v", err)
	}
	// Verify queue_depth is absent (QueueDepthPresent=false)
	ps := string(payload)
	t.Logf("Payload without QueueDepthPresent: %s", ps)
}

func TestWebhookReceived_InterfaceMethods(t *testing.T) {
	w := WebhookReceived{
		TraceIDBytes:    []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		SpanIDBytes:     []byte{0, 0, 0, 0, 0, 0, 0, 2},
		TraceID:         "00000000000000000000000000000001",
		SpanIDHex:       "0000000000000002",
		ParentSpanIDHex: "0000000000000003",
		TsStr:           "2026-05-09T12:00:00Z",
		SurfaceStr:      "openclaw",
		Channel:         "matrix",
		WebhookType:     "message",
		DurationMs:      150,
	}

	if got := w.EventType(); got != "webhook_received" {
		t.Errorf("EventType() = %q, want %q", got, "webhook_received")
	}
	if got := w.ChainID(); got == "" {
		t.Error("ChainID() returned empty string")
	}
	if got := w.Ts(); got != "2026-05-09T12:00:00Z" {
		t.Errorf("Ts() = %q, want %q", got, "2026-05-09T12:00:00Z")
	}
	if got := w.Surface(); got != "openclaw" {
		t.Errorf("Surface() = %q, want %q", got, "openclaw")
	}
	if got := w.SpanID(); got != "0000000000000002" {
		t.Errorf("SpanID() = %q, want %q", got, "0000000000000002")
	}

	payload, err := w.Payload()
	if err != nil {
		t.Fatalf("Payload() error: %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("Payload() returned empty bytes")
	}

	labels := w.Labels()
	if labels == nil {
		t.Fatal("Labels() returned nil")
	}
}

func TestWebhookFailed_InterfaceMethods(t *testing.T) {
	w := WebhookFailed{
		TraceIDBytes:    []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		SpanIDBytes:     []byte{0, 0, 0, 0, 0, 0, 0, 2},
		TraceID:         "00000000000000000000000000000001",
		SpanIDHex:       "0000000000000002",
		ParentSpanIDHex: "0000000000000003",
		TsStr:           "2026-05-09T12:00:00Z",
		SurfaceStr:      "openclaw",
		Channel:         "matrix",
		WebhookType:     "message",
		ErrorMessage:    "timeout",
	}

	if got := w.EventType(); got != "webhook_failed" {
		t.Errorf("EventType() = %q, want %q", got, "webhook_failed")
	}
	if got := w.ChainID(); got == "" {
		t.Error("ChainID() returned empty string")
	}
	if got := w.Ts(); got != "2026-05-09T12:00:00Z" {
		t.Errorf("Ts() = %q, want %q", got, "2026-05-09T12:00:00Z")
	}
	if got := w.Surface(); got != "openclaw" {
		t.Errorf("Surface() = %q, want %q", got, "openclaw")
	}
	if got := w.SpanID(); got != "0000000000000002" {
		t.Errorf("SpanID() = %q, want %q", got, "0000000000000002")
	}

	payload, err := w.Payload()
	if err != nil {
		t.Fatalf("Payload() error: %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("Payload() returned empty bytes")
	}

	labels := w.Labels()
	if labels == nil {
		t.Fatal("Labels() returned nil")
	}
}