package ingest

import (
	"testing"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func TestTranslateWebhookProcessed_ValidMinimal(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.processed",
		traceID:    sampleTraceID(0x11),
		spanID:     sampleSpanID(0x22),
		startNanos: start,
		endNanos:   end,
		stringAttrs: map[string]string{
			"openclaw.channel": "telegram",
			"openclaw.webhook": "message",
		},
	}})

	got, quarantined := translateSingle(t, payload, translateWebhookProcessed)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined=%v, want none", quarantined)
	}
	if len(got) != 1 {
		t.Fatalf("got=%d, want 1", len(got))
	}
	if got[0].Channel != "telegram" || got[0].WebhookType != "message" {
		t.Fatalf("channel/webhook mismatch: %+v", got[0])
	}
	if got[0].DurationMs != 1000 {
		t.Fatalf("duration_ms=%d, want 1000", got[0].DurationMs)
	}
	if got[0].EventType() != "webhook_received" {
		t.Fatalf("event_type=%q, want webhook_received", got[0].EventType())
	}
}

func TestTranslateWebhookProcessed_QuarantinesUnknownChannel(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.processed",
		traceID:    sampleTraceID(0x11),
		spanID:     sampleSpanID(0x22),
		startNanos: start,
		endNanos:   end,
		stringAttrs: map[string]string{
			"openclaw.channel": "unknown",
			"openclaw.webhook": "message",
		},
	}})

	got, quarantined := translateSingle(t, payload, translateWebhookProcessed)
	if len(got) != 0 {
		t.Fatalf("got=%d, want 0", len(got))
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined=%d, want 1", len(quarantined))
	}
	if quarantined[0].Reason != "unknown_value:openclaw.channel" {
		t.Fatalf("reason=%q", quarantined[0].Reason)
	}
}

func TestTranslateWebhookProcessed_OptionalChatID(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.processed",
		traceID:    sampleTraceID(0x11),
		spanID:     sampleSpanID(0x22),
		startNanos: start,
		endNanos:   end,
		stringAttrs: map[string]string{
			"openclaw.channel": "telegram",
			"openclaw.webhook": "message",
			"openclaw.chatId":  "chat-42",
		},
	}})

	got, _ := translateSingle(t, payload, translateWebhookProcessed)
	if got[0].ChatID != "chat-42" {
		t.Fatalf("chat_id=%q, want chat-42", got[0].ChatID)
	}
}

func TestTranslateWebhookProcessed_MissingChannel(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.processed",
		traceID:    sampleTraceID(0x11),
		spanID:     sampleSpanID(0x22),
		startNanos: start,
		endNanos:   end,
		stringAttrs: map[string]string{
			"openclaw.webhook": "message",
		},
	}})

	_, quarantined := translateSingle(t, payload, translateWebhookProcessed)
	if len(quarantined) != 1 || quarantined[0].Reason != "missing_required_attr:openclaw.channel" {
		t.Fatalf("want missing_required_attr:openclaw.channel, got %+v", quarantined)
	}
}

func TestTranslateWebhookError_ValidMinimal(t *testing.T) {
	start, _ := sampleTs(0)
	// Instant span: end == start.
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.error",
		traceID:    sampleTraceID(0x33),
		spanID:     sampleSpanID(0x44),
		startNanos: start,
		endNanos:   start, // instant
		stringAttrs: map[string]string{
			"openclaw.channel": "telegram",
			"openclaw.webhook": "message",
			"openclaw.error":   "transport failed: timeout",
		},
		statusCode:    tracepb.Status_STATUS_CODE_ERROR,
		statusMessage: "transport failed: timeout",
	}})

	got, quarantined := translateSingle(t, payload, translateWebhookError)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined=%v", quarantined)
	}
	if len(got) != 1 {
		t.Fatalf("got=%d, want 1", len(got))
	}
	if got[0].Channel != "telegram" || got[0].WebhookType != "message" {
		t.Fatalf("mismatch: %+v", got[0])
	}
	if got[0].ErrorMessage != "transport failed: timeout" {
		t.Fatalf("error_message=%q", got[0].ErrorMessage)
	}
	if got[0].EventType() != "webhook_failed" {
		t.Fatalf("event_type=%q, want webhook_failed", got[0].EventType())
	}
}

func TestTranslateWebhookError_MissingError(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.error",
		traceID:    sampleTraceID(0x33),
		spanID:     sampleSpanID(0x44),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{
			"openclaw.channel": "telegram",
			"openclaw.webhook": "message",
		},
	}})
	_, quarantined := translateSingle(t, payload, translateWebhookError)
	if len(quarantined) != 1 || quarantined[0].Reason != "missing_required_attr:openclaw.error" {
		t.Fatalf("reason=%+v", quarantined)
	}
}

func TestTranslateWebhookError_UnknownChannel(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.error",
		traceID:    sampleTraceID(0x33),
		spanID:     sampleSpanID(0x44),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{
			"openclaw.channel": "unknown",
			"openclaw.webhook": "message",
			"openclaw.error":   "boom",
		},
	}})
	_, quarantined := translateSingle(t, payload, translateWebhookError)
	if len(quarantined) != 1 || quarantined[0].Reason != "unknown_value:openclaw.channel" {
		t.Fatalf("reason=%+v", quarantined)
	}
}
