// Package ingest — openclaw_session_stuck.go translates openclaw.session.stuck
// spans. Pinned to @openclaw/diagnostics-otel@2026.4.15-beta.1. Attributes
// verified at plugin lines 53783–53797. Instant span with status=ERROR and
// message="session stuck".
package ingest

import (
	"encoding/json"
	"fmt"
)

// SessionStuck is the translated form of openclaw.session.stuck.
type SessionStuck struct {
	TraceIDBytes      []byte
	SpanIDBytes       []byte
	TraceID           string
	SpanIDHex         string
	ParentSpanIDHex   string // "" when root span
	TsStr             string
	SurfaceStr        string
	State             string
	AgeMs             int64
	SessionIDExternal string // optional
	SessionKey        string // optional
	QueueDepth        int64  // paired with QueueDepthPresent; nil semantics via the bool
	QueueDepthPresent bool
}

type sessionStuckPayload struct {
	State             string `json:"state"`
	AgeMs             int64  `json:"age_ms"`
	SessionIDExternal string `json:"session_id_external,omitempty"`
	SessionKey        string `json:"session_key,omitempty"`
	QueueDepth        *int64 `json:"queue_depth,omitempty"`
}

func (s SessionStuck) EventType() string { return "session_stuck" }
func (s SessionStuck) ChainID() string   { return buildChainID(s.TraceIDBytes, s.SpanIDBytes) }
func (s SessionStuck) Ts() string        { return s.TsStr }
func (s SessionStuck) Surface() string   { return s.SurfaceStr }
func (s SessionStuck) SpanID() string    { return s.SpanIDHex }

func (s SessionStuck) Payload() (json.RawMessage, error) {
	p := sessionStuckPayload{
		State:             s.State,
		AgeMs:             s.AgeMs,
		SessionIDExternal: s.SessionIDExternal,
		SessionKey:        s.SessionKey,
	}
	if s.QueueDepthPresent {
		qd := s.QueueDepth
		p.QueueDepth = &qd
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal session_stuck payload: %w", err)
	}
	return b, nil
}

func (s SessionStuck) Labels() map[string]string {
	return buildOtelLabels(s.TraceID, s.SpanIDHex, s.ParentSpanIDHex)
}

var _ TranslatedSpan = SessionStuck{}
