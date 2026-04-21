// Package ingest — openclaw_model_turn.go translates openclaw.model.usage spans.
//
// Pinned to @openclaw/diagnostics-otel@2026.4.15-beta.1. Required attributes
// derive from the static source inventory at plugin lines 53644–53695.
package ingest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// openclawModelUsageSpanName is the span name mapped to model_turn events.
const openclawModelUsageSpanName = "openclaw.model.usage"

// ModelTurn is the parse-stage output for one openclaw.model.usage span.
type ModelTurn struct {
	TraceIDBytes      []byte
	SpanIDBytes       []byte
	TraceID           string
	SpanIDHex         string // renamed from SpanID to free the SpanID() interface method
	ParentSpanIDHex   string // "" when root span
	TsStr             string // renamed from Ts to free the Ts() interface method
	SurfaceStr        string // renamed from Surface to free the Surface() interface method
	Provider          string
	ModelName         string
	InputTokens       int64
	OutputTokens      int64
	SessionIDExternal string
	DurationMs        int64
	CacheReadTokens   int64
	CacheWriteTokens  int64
}

// modelTurnPayload is the typed shape of the model_turn event payload.
type modelTurnPayload struct {
	ModelName         string `json:"model_name"`
	Provider          string `json:"provider"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	SessionIDExternal string `json:"session_id_external,omitempty"`
	DurationMs        int64  `json:"duration_ms,omitempty"`
	CacheReadTokens   int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens  int64  `json:"cache_write_tokens,omitempty"`
}

// translateModelUsage is the per-span required+optional extraction for
// openclaw.model.usage. Returns (ModelTurn, "") on success, or
// (zero-ModelTurn, reason) with a typed reason on any required-attr failure.
func translateModelUsage(resource *resourcepb.Resource, span *tracepb.Span) (ModelTurn, string) {
	surface, ts, reason := validateOpenClawEnvelope(resource, span)
	if reason != "" {
		return ModelTurn{}, reason
	}

	provider := getSpanStringAttr(span, "openclaw.provider")
	if provider == "" {
		return ModelTurn{}, "missing_required_attr:openclaw.provider"
	}
	if provider == "unknown" {
		return ModelTurn{}, "unknown_value:openclaw.provider"
	}

	modelName := getSpanStringAttr(span, "openclaw.model")
	if modelName == "" {
		return ModelTurn{}, "missing_required_attr:openclaw.model"
	}
	if modelName == "unknown" {
		return ModelTurn{}, "unknown_value:openclaw.model"
	}

	inputTokens, inputPresent := getSpanIntAttr(span, "openclaw.tokens.input")
	if !inputPresent {
		return ModelTurn{}, "missing_required_attr:openclaw.tokens.input"
	}
	if inputTokens < 0 {
		return ModelTurn{}, "invalid_value:openclaw.tokens.input"
	}
	outputTokens, outputPresent := getSpanIntAttr(span, "openclaw.tokens.output")
	if !outputPresent {
		return ModelTurn{}, "missing_required_attr:openclaw.tokens.output"
	}
	if outputTokens < 0 {
		return ModelTurn{}, "invalid_value:openclaw.tokens.output"
	}

	mt := ModelTurn{
		TraceIDBytes:    span.TraceId,
		SpanIDBytes:     span.SpanId,
		TraceID:         hex.EncodeToString(span.TraceId),
		SpanIDHex:       hex.EncodeToString(span.SpanId),
		ParentSpanIDHex: parentSpanIDHex(span),
		TsStr:           ts,
		SurfaceStr:      surface,
		Provider:        provider,
		ModelName:       modelName,
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
	}
	if sid := getSpanStringAttr(span, "openclaw.sessionId"); sid != "" {
		mt.SessionIDExternal = sid
	}
	if span.EndTimeUnixNano != 0 && span.EndTimeUnixNano >= span.StartTimeUnixNano {
		mt.DurationMs = int64((span.EndTimeUnixNano - span.StartTimeUnixNano) / 1_000_000)
	}
	if cr, ok := getSpanIntAttr(span, "openclaw.tokens.cache_read"); ok {
		if cr < 0 {
			return ModelTurn{}, "invalid_value:openclaw.tokens.cache_read"
		}
		mt.CacheReadTokens = cr
	}
	if cw, ok := getSpanIntAttr(span, "openclaw.tokens.cache_write"); ok {
		if cw < 0 {
			return ModelTurn{}, "invalid_value:openclaw.tokens.cache_write"
		}
		mt.CacheWriteTokens = cw
	}
	return mt, ""
}

// --- TranslatedSpan implementation ---

func (m ModelTurn) EventType() string { return "model_turn" }

func (m ModelTurn) ChainID() string {
	return buildChainID(m.TraceIDBytes, m.SpanIDBytes)
}

func (m ModelTurn) Ts() string      { return m.TsStr }
func (m ModelTurn) Surface() string { return m.SurfaceStr }
func (m ModelTurn) SpanID() string  { return m.SpanIDHex }

func (m ModelTurn) Payload() (json.RawMessage, error) {
	return buildModelTurnPayload(m)
}

func (m ModelTurn) Labels() map[string]string {
	return buildOtelLabels(m.TraceID, m.SpanIDHex, m.ParentSpanIDHex)
}

var _ TranslatedSpan = ModelTurn{}

// buildModelTurnPayload marshals the typed payload struct for the
// event envelope. Returns an error only on JSON encoding failure.
func buildModelTurnPayload(mt ModelTurn) (json.RawMessage, error) {
	p := modelTurnPayload{
		ModelName:         mt.ModelName,
		Provider:          mt.Provider,
		InputTokens:       mt.InputTokens,
		OutputTokens:      mt.OutputTokens,
		SessionIDExternal: mt.SessionIDExternal,
		DurationMs:        mt.DurationMs,
		CacheReadTokens:   mt.CacheReadTokens,
		CacheWriteTokens:  mt.CacheWriteTokens,
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal model_turn payload: %w", err)
	}
	return b, nil
}
