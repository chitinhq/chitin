// Package event defines the v2 canonical event record (envelope + typed payload).
// Generated conceptually from libs/contracts; hand-maintained to stay in sync
// until the generator is updated in a later task.
package event

import "encoding/json"

// DriverIdentity names who/where the agent is running.
type DriverIdentity struct {
	User               string `json:"user"`
	MachineID          string `json:"machine_id"`
	MachineFingerprint string `json:"machine_fingerprint"`
}

// Event is the v2 event record: envelope fields + a free-form Payload typed by EventType.
// Payload is stored as json.RawMessage so the kernel does not need to know the shape
// of every event_type's payload to compute hashes / append JSONL.
type Event struct {
	SchemaVersion    string            `json:"schema_version"`
	RunID            string            `json:"run_id"`
	SessionID        string            `json:"session_id"`
	Surface          string            `json:"surface"`
	DriverIdentity   DriverIdentity    `json:"driver_identity"`
	AgentInstanceID  string            `json:"agent_instance_id"`
	ParentAgentID    *string           `json:"parent_agent_id"`
	AgentFingerprint string            `json:"agent_fingerprint"`
	EventType        string            `json:"event_type"`
	ChainID          string            `json:"chain_id"`
	ChainType        string            `json:"chain_type"`
	ParentChainID    *string           `json:"parent_chain_id"`
	Seq              int64             `json:"seq"`
	PrevHash         *string           `json:"prev_hash"`
	ThisHash         string            `json:"this_hash"`
	Ts               string            `json:"ts"`
	Labels           map[string]string `json:"labels"`
	Payload          json.RawMessage   `json:"payload"`
}

// ToMap returns a map representation suitable for passing to hash.HashEvent.
// Payload is unmarshaled so the canonical JSON serializer sees real structure.
func (e *Event) ToMap() (map[string]any, error) {
	m := map[string]any{
		"schema_version": e.SchemaVersion,
		"run_id":         e.RunID,
		"session_id":     e.SessionID,
		"surface":        e.Surface,
		"driver_identity": map[string]any{
			"user":                e.DriverIdentity.User,
			"machine_id":          e.DriverIdentity.MachineID,
			"machine_fingerprint": e.DriverIdentity.MachineFingerprint,
		},
		"agent_instance_id": e.AgentInstanceID,
		"agent_fingerprint": e.AgentFingerprint,
		"event_type":        e.EventType,
		"chain_id":          e.ChainID,
		"chain_type":        e.ChainType,
		"seq":               float64(e.Seq),
		"this_hash":         e.ThisHash,
		"ts":                e.Ts,
		"labels":            stringMapToAnyMap(e.Labels),
	}
	if e.ParentAgentID != nil {
		m["parent_agent_id"] = *e.ParentAgentID
	} else {
		m["parent_agent_id"] = nil
	}
	if e.ParentChainID != nil {
		m["parent_chain_id"] = *e.ParentChainID
	} else {
		m["parent_chain_id"] = nil
	}
	if e.PrevHash != nil {
		m["prev_hash"] = *e.PrevHash
	} else {
		m["prev_hash"] = nil
	}
	var payload any
	if len(e.Payload) == 0 {
		payload = map[string]any{}
	} else {
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return nil, err
		}
	}
	m["payload"] = payload
	return m, nil
}

func stringMapToAnyMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
