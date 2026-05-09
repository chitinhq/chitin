package chain

import (
	"encoding/json"
	"testing"
)

func TestLogicalHash_EmptyEventType(t *testing.T) {
	if got := LogicalHash("", json.RawMessage(`{}`)); got != "" {
		t.Errorf("LogicalHash with empty eventType should return empty string, got %q", got)
	}
}

func TestLogicalHash_Deterministic(t *testing.T) {
	payload := json.RawMessage(`{"action":"shell.exec","tool":"bash"}`)
	h1 := LogicalHash("session_start", payload)
	h2 := LogicalHash("session_start", payload)
	if h1 != h2 {
		t.Errorf("LogicalHash should be deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("LogicalHash should produce 64-char hex string, got %d chars", len(h1))
	}
}

func TestLogicalHash_DifferentType(t *testing.T) {
	payload := json.RawMessage(`{}`)
	h1 := LogicalHash("session_start", payload)
	h2 := LogicalHash("session_end", payload)
	if h1 == h2 {
		t.Error("different event types should produce different hashes")
	}
}

func TestLogicalHash_DifferentPayload(t *testing.T) {
	h1 := LogicalHash("session_start", json.RawMessage(`{"a":"1"}`))
	h2 := LogicalHash("session_start", json.RawMessage(`{"a":"2"}`))
	if h1 == h2 {
		t.Error("different payloads should produce different hashes")
	}
}

func TestLogicalHash_NilPayload(t *testing.T) {
	h := LogicalHash("session_start", nil)
	if h == "" {
		t.Error("nil payload should still produce a hash")
	}
}