package manifest

import (
	"encoding/json"
	"fmt"
	"time"
)

type EmitOptions struct {
	ChainID       string
	ChainType     string
	ParentChainID *string
	Timestamp     time.Time
	Labels        map[string]string
}

type chainCursor struct {
	Seq      int64
	ThisHash string
}

// validEventTypes / validChainTypes mirror the canonical EventSchema
// discriminated union — EmitEvent rejects anything outside them so the
// SDK can't emit JSONL the kernel's schema would reject.
var validEventTypes = map[string]bool{
	"session_start": true, "user_prompt": true, "assistant_turn": true,
	"compaction": true, "session_end": true, "intended": true,
	"executed": true, "failed": true, "model_turn": true,
	"webhook_received": true, "webhook_failed": true, "session_stuck": true,
}

var validChainTypes = map[string]bool{
	"session": true, "tool_call": true,
}

type Run struct {
	Manifest RunManifest
	events   []Event
	chains   map[string]chainCursor
	closed   bool
}

func NewRun(input RunManifestInput) (*Run, error) {
	manifest, err := NewRunManifest(input)
	if err != nil {
		return nil, err
	}
	return &Run{
		Manifest: manifest,
		chains:   map[string]chainCursor{},
	}, nil
}

func (r *Run) EmitEvent(eventType string, payload any, opts EmitOptions) (Event, error) {
	// A session_end closes the run — the kernel emitter rejects appends
	// after the terminal tail, so the SDK enforces the same.
	if r.closed {
		return Event{}, fmt.Errorf("run is finalized; no further events may be emitted")
	}
	if eventType == "" {
		return Event{}, fmt.Errorf("eventType is required")
	}
	if !validEventTypes[eventType] {
		return Event{}, fmt.Errorf("unknown event_type %q", eventType)
	}
	chainID := opts.ChainID
	if chainID == "" {
		chainID = r.Manifest.SessionID
	}
	chainType := opts.ChainType
	if chainType == "" {
		chainType = "session"
	}
	if !validChainTypes[chainType] {
		return Event{}, fmt.Errorf("unknown chain_type %q", chainType)
	}
	// Copy pointer-typed inputs by value: the event is hashed now but
	// JSONL() re-marshals it later, so a caller mutating *opts.ParentChainID
	// (or the manifest's SessionID) afterwards must not change the event.
	var parentChainID *string
	if opts.ParentChainID != nil {
		v := *opts.ParentChainID
		parentChainID = &v
	} else if chainType == "tool_call" {
		v := r.Manifest.SessionID
		parentChainID = &v
	}
	cursor, ok := r.chains[chainID]
	seq := int64(0)
	var prevHash *string
	if ok {
		seq = cursor.Seq + 1
		prevHash = &cursor.ThisHash
	}
	ts := opts.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	event := Event{
		SchemaVersion:    r.Manifest.SchemaVersion,
		RunID:            r.Manifest.RunID,
		SessionID:        r.Manifest.SessionID,
		Surface:          r.Manifest.Surface,
		DriverIdentity:   r.Manifest.DriverIdentity,
		AgentInstanceID:  r.Manifest.AgentInstanceID,
		ParentAgentID:    r.Manifest.ParentAgentID,
		AgentFingerprint: r.Manifest.AgentFingerprint,
		EventType:        eventType,
		ChainID:          chainID,
		ChainType:        chainType,
		ParentChainID:    parentChainID,
		Seq:              seq,
		PrevHash:         prevHash,
		ThisHash:         "",
		Ts:               ts.Format(time.RFC3339Nano),
		Labels:           mergeLabels(r.Manifest.Labels, opts.Labels),
		Payload:          payload,
	}
	eventMap, err := eventToMap(event)
	if err != nil {
		return Event{}, err
	}
	thisHash, err := hashEvent(eventMap)
	if err != nil {
		return Event{}, err
	}
	event.ThisHash = thisHash
	// Replace the caller's payload reference with the JSON-round-tripped
	// snapshot used for hashing. Otherwise a caller mutating a map/slice
	// payload after EmitEvent would make JSONL() re-marshal different
	// content than this_hash was computed over.
	if snap, ok := eventMap["payload"]; ok {
		event.Payload = snap
	}
	r.chains[chainID] = chainCursor{Seq: event.Seq, ThisHash: event.ThisHash}
	r.events = append(r.events, event)
	return event, nil
}

func (r *Run) Finalize(payload SessionEndPayload, opts EmitOptions) (Event, error) {
	event, err := r.EmitEvent("session_end", payload, opts)
	if err != nil {
		return Event{}, err
	}
	r.closed = true
	return event, nil
}

func (r *Run) Events() []Event {
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}

func (r *Run) JSONL() ([]byte, error) {
	lines := make([][]byte, 0, len(r.events))
	for _, event := range r.events {
		line, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return bytesJoin(lines, '\n'), nil
}

func mergeLabels(base, extra map[string]string) map[string]string {
	out := cloneLabels(base)
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func eventToMap(event Event) (map[string]any, error) {
	raw, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func bytesJoin(parts [][]byte, sep byte) []byte {
	if len(parts) == 0 {
		return nil
	}
	size := 0
	for _, part := range parts {
		size += len(part)
	}
	size += len(parts) - 1
	out := make([]byte, 0, size)
	for i, part := range parts {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, part...)
	}
	return out
}
