package event

import (
	"encoding/json"
	"testing"
)

func TestToMap_FullEvent(t *testing.T) {
	parentAgent := "parent-123"
	parentChain := "chain-parent"
	prevHash := "abc123"
	ev := &Event{
		SchemaVersion:    "2",
		RunID:            "run-1",
		SessionID:        "sess-1",
		Surface:          "claude-code",
		DriverIdentity:   DriverIdentity{User: "alice", MachineID: "m1", MachineFingerprint: "fp1"},
		AgentInstanceID:  "agent-1",
		ParentAgentID:    &parentAgent,
		AgentFingerprint: "fp-agent",
		EventType:        "tool_call",
		ChainID:          "chain-1",
		ChainType:        "session",
		ParentChainID:    &parentChain,
		Seq:              3,
		PrevHash:         &prevHash,
		ThisHash:         "def456",
		Ts:               "2026-05-09T00:00:00Z",
		Labels:           map[string]string{"env": "prod"},
		Payload:          json.RawMessage(`{"tool":"Bash","command":"ls"}`),
	}
	m, err := ev.ToMap()
	if err != nil {
		t.Fatal(err)
	}
	// Verify pointer fields are set correctly
	if m["parent_agent_id"] != parentAgent {
		t.Errorf("parent_agent_id=%v, want %q", m["parent_agent_id"], parentAgent)
	}
	if m["parent_chain_id"] != parentChain {
		t.Errorf("parent_chain_id=%v, want %q", m["parent_chain_id"], parentChain)
	}
	if m["prev_hash"] != prevHash {
		t.Errorf("prev_hash=%v, want %q", m["prev_hash"], prevHash)
	}
	if m["seq"].(float64) != 3 {
		t.Errorf("seq=%v, want 3", m["seq"])
	}
	payload, ok := m["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type=%T, want map[string]any", m["payload"])
	}
	if payload["tool"] != "Bash" {
		t.Errorf("payload[tool]=%v, want Bash", payload["tool"])
	}
	if m["labels"].(map[string]any)["env"] != "prod" {
		t.Error("labels[env] should be prod")
	}
}

func TestToMap_NilPointerFields(t *testing.T) {
	ev := &Event{
		SchemaVersion:    "2",
		RunID:            "run-1",
		SessionID:        "sess-1",
		Surface:          "claude-code",
		DriverIdentity:   DriverIdentity{User: "u", MachineID: "m", MachineFingerprint: "f"},
		AgentInstanceID:  "agent-1",
		ParentAgentID:    nil,
		AgentFingerprint: "fp",
		EventType:        "session_start",
		ChainID:          "chain-1",
		ChainType:        "session",
		ParentChainID:    nil,
		Seq:              0,
		PrevHash:         nil,
		ThisHash:         "",
		Ts:               "2026-05-09T00:00:00Z",
		Labels:           map[string]string{},
		Payload:          json.RawMessage(`{}`),
	}
	m, err := ev.ToMap()
	if err != nil {
		t.Fatal(err)
	}
	if m["parent_agent_id"] != nil {
		t.Errorf("parent_agent_id=%v, want nil", m["parent_agent_id"])
	}
	if m["parent_chain_id"] != nil {
		t.Errorf("parent_chain_id=%v, want nil", m["parent_chain_id"])
	}
	if m["prev_hash"] != nil {
		t.Errorf("prev_hash=%v, want nil", m["prev_hash"])
	}
}

func TestToMap_EmptyPayload(t *testing.T) {
	ev := &Event{
		SchemaVersion:    "2",
		RunID:            "r",
		SessionID:        "s",
		Surface:          "test",
		DriverIdentity:   DriverIdentity{User: "u", MachineID: "m", MachineFingerprint: "f"},
		AgentInstanceID:  "a",
		AgentFingerprint: "fp",
		EventType:        "session_start",
		ChainID:          "c",
		ChainType:        "session",
		Seq:              0,
		ThisHash:         "",
		Ts:               "2026-05-09T00:00:00Z",
		Labels:           map[string]string{},
		Payload:          json.RawMessage(""),
	}
	m, err := ev.ToMap()
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := m["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type=%T, want map[string]any", m["payload"])
	}
	if len(payload) != 0 {
		t.Errorf("empty payload should be empty map, got %v", payload)
	}
}

func TestToMap_InvalidPayloadJSON(t *testing.T) {
	ev := &Event{
		SchemaVersion:    "2",
		RunID:            "r",
		SessionID:        "s",
		Surface:          "test",
		DriverIdentity:   DriverIdentity{User: "u", MachineID: "m", MachineFingerprint: "f"},
		AgentInstanceID:  "a",
		AgentFingerprint: "fp",
		EventType:        "session_start",
		ChainID:          "c",
		ChainType:        "session",
		Seq:              0,
		ThisHash:         "",
		Ts:               "2026-05-09T00:00:00Z",
		Labels:           map[string]string{},
		Payload:          json.RawMessage(`{broken`),
	}
	_, err := ev.ToMap()
	if err == nil {
		t.Fatal("expected error for invalid JSON payload")
	}
}

func TestStringMapToAnyMap(t *testing.T) {
	m := map[string]string{"a": "1", "b": "2"}
	result := stringMapToAnyMap(m)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if result["a"] != "1" || result["b"] != "2" {
		t.Errorf("expected a=1,b=2, got %v", result)
	}
}