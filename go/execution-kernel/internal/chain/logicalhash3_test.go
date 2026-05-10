package chain

import (
	"encoding/json"
	"testing"
)

func TestLogicalHash(t *testing.T) {
	// Empty eventType returns empty string
	if got := LogicalHash("", json.RawMessage(`{}`)); got != "" {
		t.Errorf("LogicalHash(empty type) = %q, want empty", got)
	}
	// Non-empty eventType produces a deterministic hash
	h1 := LogicalHash("model_turn", json.RawMessage(`{"agent":"claude-code"}`))
	h2 := LogicalHash("model_turn", json.RawMessage(`{"agent":"claude-code"}`))
	if h1 != h2 {
		t.Errorf("LogicalHash should be deterministic: %q != %q", h1, h2)
	}
	if h1 == "" {
		t.Error("LogicalHash should not return empty for non-empty eventType")
	}
	// Different eventType produces different hash
	h3 := LogicalHash("decision", json.RawMessage(`{"agent":"claude-code"}`))
	if h1 == h3 {
		t.Error("different eventType should produce different hash")
	}
	// Different payload produces different hash
	h4 := LogicalHash("model_turn", json.RawMessage(`{"agent":"gemini"}`))
	if h1 == h4 {
		t.Error("different payload should produce different hash")
	}
}