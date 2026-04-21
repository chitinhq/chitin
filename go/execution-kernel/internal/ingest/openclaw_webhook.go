// Package ingest — openclaw_webhook.go translates openclaw.webhook.processed
// and openclaw.webhook.error spans.
//
// Pinned to @openclaw/diagnostics-otel@2026.4.15-beta.1. Attributes verified
// at plugin lines 53697–53734. openclaw.webhook.processed carries a duration;
// openclaw.webhook.error is an instant span with status=ERROR and an
// openclaw.error attribute carrying the redacted error message.
package ingest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

const openclawWebhookProcessedSpanName = "openclaw.webhook.processed"
const openclawWebhookErrorSpanName = "openclaw.webhook.error"

// translateWebhookProcessed is the per-span required+optional extraction for
// openclaw.webhook.processed. Returns (WebhookReceived, "") on success, or
// (zero-WebhookReceived, reason) with a typed reason on any required-attr
// failure. Mirrors translateModelUsage in shape: validate trace/span ids,
// require service.name + start time, require channel + webhook (rejecting
// "unknown" sentinels), then attach optional chat_id and derive duration.
func translateWebhookProcessed(resource *resourcepb.Resource, span *tracepb.Span) (WebhookReceived, string) {
	if len(span.TraceId) != 16 {
		return WebhookReceived{}, "invalid_trace_id_length"
	}
	if isAllZero(span.TraceId) {
		return WebhookReceived{}, "invalid_trace_id_zero"
	}
	if len(span.SpanId) != 8 {
		return WebhookReceived{}, "invalid_span_id_length"
	}
	if isAllZero(span.SpanId) {
		return WebhookReceived{}, "invalid_span_id_zero"
	}

	surface := getResourceStringAttr(resource, "service.name")
	if surface == "" {
		return WebhookReceived{}, "missing_required_attr:service.name"
	}

	if span.StartTimeUnixNano == 0 {
		return WebhookReceived{}, "missing_required_attr:start_time_unix_nano"
	}
	ts := time.Unix(0, int64(span.StartTimeUnixNano)).UTC().Format(time.RFC3339)

	channel := getSpanStringAttr(span, "openclaw.channel")
	if channel == "" {
		return WebhookReceived{}, "missing_required_attr:openclaw.channel"
	}
	if channel == "unknown" {
		return WebhookReceived{}, "unknown_value:openclaw.channel"
	}

	webhookType := getSpanStringAttr(span, "openclaw.webhook")
	if webhookType == "" {
		return WebhookReceived{}, "missing_required_attr:openclaw.webhook"
	}
	if webhookType == "unknown" {
		return WebhookReceived{}, "unknown_value:openclaw.webhook"
	}

	var durationMs int64
	if span.EndTimeUnixNano != 0 && span.EndTimeUnixNano >= span.StartTimeUnixNano {
		durationMs = int64((span.EndTimeUnixNano - span.StartTimeUnixNano) / 1_000_000)
	}

	w := WebhookReceived{
		TraceIDBytes: span.TraceId,
		SpanIDBytes:  span.SpanId,
		TraceID:      hex.EncodeToString(span.TraceId),
		SpanIDHex:    hex.EncodeToString(span.SpanId),
		TsStr:        ts,
		SurfaceStr:   surface,
		Channel:      channel,
		WebhookType:  webhookType,
		DurationMs:   durationMs,
	}
	if len(span.ParentSpanId) == 8 && !isAllZero(span.ParentSpanId) {
		w.ParentSpanIDHex = hex.EncodeToString(span.ParentSpanId)
	}
	if chatID := getSpanStringAttr(span, "openclaw.chatId"); chatID != "" {
		w.ChatID = chatID
	}
	return w, ""
}

// WebhookReceived is the translated form of openclaw.webhook.processed.
type WebhookReceived struct {
	TraceIDBytes    []byte
	SpanIDBytes     []byte
	TraceID         string
	SpanIDHex       string
	ParentSpanIDHex string // "" when root span
	TsStr           string
	SurfaceStr      string
	Channel         string
	WebhookType     string
	DurationMs      int64
	ChatID          string // optional; "" when absent
}

// WebhookFailed is the translated form of openclaw.webhook.error.
type WebhookFailed struct {
	TraceIDBytes    []byte
	SpanIDBytes     []byte
	TraceID         string
	SpanIDHex       string
	ParentSpanIDHex string // "" when root span
	TsStr           string
	SurfaceStr      string
	Channel         string
	WebhookType     string
	ErrorMessage    string
	ChatID          string // optional
}

type webhookReceivedPayload struct {
	Channel     string `json:"channel"`
	WebhookType string `json:"webhook_type"`
	DurationMs  int64  `json:"duration_ms"`
	ChatID      string `json:"chat_id,omitempty"`
}

type webhookFailedPayload struct {
	Channel      string `json:"channel"`
	WebhookType  string `json:"webhook_type"`
	ErrorMessage string `json:"error_message"`
	ChatID       string `json:"chat_id,omitempty"`
}

// --- TranslatedSpan impl: WebhookReceived ---

func (w WebhookReceived) EventType() string { return "webhook_received" }
func (w WebhookReceived) ChainID() string   { return buildChainID(w.TraceIDBytes, w.SpanIDBytes) }
func (w WebhookReceived) Ts() string        { return w.TsStr }
func (w WebhookReceived) Surface() string   { return w.SurfaceStr }
func (w WebhookReceived) SpanID() string    { return w.SpanIDHex }

func (w WebhookReceived) Payload() (json.RawMessage, error) {
	p := webhookReceivedPayload{
		Channel:     w.Channel,
		WebhookType: w.WebhookType,
		DurationMs:  w.DurationMs,
		ChatID:      w.ChatID,
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook_received payload: %w", err)
	}
	return b, nil
}

func (w WebhookReceived) Labels() map[string]string {
	return buildOtelLabels(w.TraceID, w.SpanIDHex, w.ParentSpanIDHex)
}

// --- TranslatedSpan impl: WebhookFailed ---

func (w WebhookFailed) EventType() string { return "webhook_failed" }
func (w WebhookFailed) ChainID() string   { return buildChainID(w.TraceIDBytes, w.SpanIDBytes) }
func (w WebhookFailed) Ts() string        { return w.TsStr }
func (w WebhookFailed) Surface() string   { return w.SurfaceStr }
func (w WebhookFailed) SpanID() string    { return w.SpanIDHex }

func (w WebhookFailed) Payload() (json.RawMessage, error) {
	p := webhookFailedPayload{
		Channel:      w.Channel,
		WebhookType:  w.WebhookType,
		ErrorMessage: w.ErrorMessage,
		ChatID:       w.ChatID,
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook_failed payload: %w", err)
	}
	return b, nil
}

func (w WebhookFailed) Labels() map[string]string {
	return buildOtelLabels(w.TraceID, w.SpanIDHex, w.ParentSpanIDHex)
}

var _ TranslatedSpan = WebhookReceived{}
var _ TranslatedSpan = WebhookFailed{}
