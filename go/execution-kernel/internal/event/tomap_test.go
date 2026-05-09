package event

import (
	"encoding/json"
	"testing"
)

func TestToMap_NilPointerFields(t *testing.T) {
	ev := &Event{
		SchemaVersion:    "2",
		RunID:            "run-1",
		SessionID:        "sess-1",
		Surface:          "test",
		DriverIdentity:   DriverIdentity{User: "u", MachineID: "m", MachineFingerprint: "fp"},
		AgentInstanceID:  "agent-1",
		ParentAgentID:    nil, // explicitly nil
		AgentFingerprint: "af",
		EventType:       "decision",
		ChainID:         "chain-1",
		ChainType:       "session",
		ParentChainID:   nil, // explicitly nil
		Seq:             1,
		PrevHash:        nil, // explicitly nil
		ThisHash:        "hash1",
		Ts:              "2026-05-09T12:00:00Z",
		Labels:          map[string]string{"k": "v"},
		Payload:         json.RawMessage(`{"decision":"allow"}`),
	}

	m, err := ev.ToMap()
	if err != nil {
		t.Fatalf("ToMap: %v", err)
	}
	if v, ok := m["parent_agent_id"]; !ok || v != nil {
		t.Errorf("parent_agent_id = %v, want nil", v)
	}
	if v, ok := m["parent_chain_id"]; !ok || v != nil {
		t.Errorf("parent_chain_id = %v, want nil", v)
	}
	if v, ok := m["prev_hash"]; !ok || v != nil {
		t.Errorf("prev_hash = %v, want nil", v)
	}
}

func TestToMap_NonNilPointerFields(t *testing.T) {
	parentAgent := "parent-agent-1"
	parentChain := "parent-chain-1"
	prevHash := "abc123"

	ev := &Event{
		SchemaVersion:    "2",
		RunID:            "run-1",
		SessionID:        "sess-1",
		Surface:          "test",
		DriverIdentity:   DriverIdentity{User: "u", MachineID: "m", MachineFingerprint: "fp"},
		AgentInstanceID:  "agent-1",
		ParentAgentID:    &parentAgent,
		AgentFingerprint: "af",
		EventType:       "decision",
		ChainID:         "chain-1",
		ChainType:       "session",
		ParentChainID:   &parentChain,
		Seq:             1,
		PrevHash:        &prevHash,
		ThisHash:        "hash1",
		Ts:              "2026-05-09T12:00:00Z",
		Labels:          map[string]string{"k": "v"},
		Payload:         json.RawMessage(`{"decision":"allow"}`),
	}

	m, err := ev.ToMap()
	if err != nil {
		t.Fatalf("ToMap: %v", err)
	}
	if v := m["parent_agent_id"]; v != parentAgent {
		t.Errorf("parent_agent_id = %v, want %q", v, parentAgent)
	}
	if v := m["parent_chain_id"]; v != parentChain {
		t.Errorf("parent_chain_id = %v, want %q", v, parentChain)
	}
	if v := m["prev_hash"]; v != prevHash {
		t.Errorf("prev_hash = %v, want %q", v, prevHash)
	}
}

func TestToMap_EmptyPayload(t *testing.T) {
	ev := &Event{
		SchemaVersion:    "2",
		RunID:            "run-1",
		SessionID:        "sess-1",
		Surface:          "test",
		DriverIdentity:   DriverIdentity{User: "u", MachineID: "m", MachineFingerprint: "fp"},
		AgentInstanceID:  "agent-1",
		AgentFingerprint: "af",
		EventType:       "decision",
		ChainID:         "chain-1",
		ChainType:       "session",
		Seq:             1,
		ThisHash:        "hash1",
		Ts:              "2026-05-09T12:00:00Z",
		Labels:          map[string]string{},
		Payload:         nil, // empty payload
	}

	m, err := ev.ToMap()
	if err != nil {
		t.Fatalf("ToMap: %v", err)
	}
	payload, ok := m["payload"].(map[string]any)
	if !ok || len(payload) != 0 {
		t.Errorf("empty payload = %v, want empty map", m["payload"])
	}
}

func TestToMap_InvalidPayloadJSON(t *testing.T) {
	ev := &Event{
		SchemaVersion:    "2",
		RunID:            "run-1",
		SessionID:        "sess-1",
		Surface:          "test",
		DriverIdentity:   DriverIdentity{User: "u", MachineID: "m", MachineFingerprint: "fp"},
		AgentInstanceID:  "agent-1",
		AgentFingerprint: "af",
		EventType:       "decision",
		ChainID:         "chain-1",
		ChainType:       "session",
		Seq:             1,
		ThisHash:        "hash1",
		Ts:              "2026-05-09T12:00:00Z",
		Labels:          map[string]string{},
		Payload:         json.RawMessage(`{invalid json`),
	}

	_, err := ev.ToMap()
	if err == nil {
		t.Error("expected error for invalid JSON payload")
	}
}

func TestStringMapToAnyMap(t *testing.T) {
	m := stringMapToAnyMap(map[string]string{"a": "1", "b": "2"})
	if len(m) != 2 {
		t.Errorf("len = %d, want 2", len(m))
	}
	if m["a"] != "1" || m["b"] != "2" {
		t.Errorf("map = %v, want {a:1, b:2}", m)
	}

	// Empty map
	empty := stringMapToAnyMap(nil)
	if empty == nil || len(empty) != 0 {
		t.Errorf("nil input = %v, want empty non-nil map", empty)
	}
}