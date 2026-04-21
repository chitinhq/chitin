// Package ingest — openclaw_session_stuck.go translates openclaw.session.stuck
// spans. Pinned to @openclaw/diagnostics-otel@2026.4.15-beta.1. Attributes
// verified at plugin lines 53783–53797. Instant span with status=ERROR and
// message="session stuck".
package ingest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

const openclawSessionStuckSpanName = "openclaw.session.stuck"

// translateSessionStuck translates openclaw.session.stuck spans into
// SessionStuck. Instant span (start == end); no duration computed.
// "unknown" sentinel checks do NOT apply to openclaw.state: the plugin
// emits arbitrary state identifiers (awaiting_model, queued, processing,
// etc.) and "unknown" would be a legitimate state name if it ever appears.
// Numeric attrs openclaw.ageMs and openclaw.queueDepth are read via
// getSpanIntOrDoubleAsIntAttr because OTEL JS SDK can emit either
// IntValue or DoubleValue for the same JS number.
func translateSessionStuck(resource *resourcepb.Resource, span *tracepb.Span) (SessionStuck, string) {
	surface, ts, reason := validateOpenClawEnvelope(resource, span)
	if reason != "" {
		return SessionStuck{}, reason
	}

	state := getSpanStringAttr(span, "openclaw.state")
	if state == "" {
		return SessionStuck{}, "missing_required_attr:openclaw.state"
	}

	ageMs, agePresent := getSpanIntOrDoubleAsIntAttr(span, "openclaw.ageMs")
	if !agePresent {
		return SessionStuck{}, "missing_required_attr:openclaw.ageMs"
	}
	if ageMs < 0 {
		return SessionStuck{}, "invalid_value:openclaw.ageMs"
	}

	s := SessionStuck{
		TraceIDBytes:    span.TraceId,
		SpanIDBytes:     span.SpanId,
		TraceID:         hex.EncodeToString(span.TraceId),
		SpanIDHex:       hex.EncodeToString(span.SpanId),
		ParentSpanIDHex: parentSpanIDHex(span),
		TsStr:           ts,
		SurfaceStr:      surface,
		State:           state,
		AgeMs:           ageMs,
	}
	if sid := getSpanStringAttr(span, "openclaw.sessionId"); sid != "" {
		s.SessionIDExternal = sid
	}
	if sk := getSpanStringAttr(span, "openclaw.sessionKey"); sk != "" {
		s.SessionKey = sk
	}
	if qd, ok := getSpanIntOrDoubleAsIntAttr(span, "openclaw.queueDepth"); ok {
		if qd < 0 {
			return SessionStuck{}, "invalid_value:openclaw.queueDepth"
		}
		s.QueueDepth = qd
		s.QueueDepthPresent = true
	}
	return s, ""
}

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
