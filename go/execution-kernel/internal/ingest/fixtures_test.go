// fixtures_test.go — test-only helper for building synthesized OTLP protobuf
// payloads for openclaw span translators. One source of truth for span
// assembly; individual test files pass attribute maps and get back a
// ready-to-decode []byte.
package ingest

import (
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type fixtureSpan struct {
	name          string
	traceID       []byte // must be 16 bytes
	spanID        []byte // must be 8 bytes
	parentSpanID  []byte // 0 or 8 bytes
	startNanos    uint64
	endNanos      uint64
	stringAttrs   map[string]string
	intAttrs      map[string]int64
	statusCode    tracepb.Status_StatusCode
	statusMessage string
}

func buildFixture(t *testing.T, serviceName string, spans []fixtureSpan) []byte {
	t.Helper()

	toAttrs := func(s map[string]string, i map[string]int64) []*commonpb.KeyValue {
		var out []*commonpb.KeyValue
		for k, v := range s {
			out = append(out, &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}})
		}
		for k, v := range i {
			out = append(out, &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: v}}})
		}
		return out
	}

	pbSpans := make([]*tracepb.Span, 0, len(spans))
	for _, s := range spans {
		sp := &tracepb.Span{
			Name:              s.name,
			TraceId:           s.traceID,
			SpanId:            s.spanID,
			ParentSpanId:      s.parentSpanID,
			StartTimeUnixNano: s.startNanos,
			EndTimeUnixNano:   s.endNanos,
			Attributes:        toAttrs(s.stringAttrs, s.intAttrs),
		}
		if s.statusCode != tracepb.Status_STATUS_CODE_UNSET {
			sp.Status = &tracepb.Status{Code: s.statusCode, Message: s.statusMessage}
		}
		pbSpans = append(pbSpans, sp)
	}

	td := &tracepb.TracesData{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: serviceName}}},
			}},
			ScopeSpans: []*tracepb.ScopeSpans{{Spans: pbSpans}},
		}},
	}

	data, err := proto.Marshal(td)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return data
}

// sampleTraceID returns a deterministic 16-byte trace_id.
func sampleTraceID(n byte) []byte {
	out := make([]byte, 16)
	for i := range out {
		out[i] = n
	}
	return out
}

// sampleSpanID returns a deterministic 8-byte span_id.
func sampleSpanID(n byte) []byte {
	out := make([]byte, 8)
	for i := range out {
		out[i] = n
	}
	return out
}

// sampleTs returns a deterministic start timestamp (2026-04-21T12:00:00Z).
func sampleTs(offsetSeconds int64) (startNanos, endNanos uint64) {
	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC).UnixNano()
	startNanos = uint64(base + offsetSeconds*int64(time.Second))
	endNanos = startNanos + uint64(time.Second) // 1s default duration
	return
}

// translateSingle bypasses the dispatcher and calls a per-span translator
// on every span in a decoded OTLP/protobuf payload. Used by Tasks 7, 8, 9
// before Task 10 wires ParseOpenClawSpans to the new translators.
// T is the per-span translated-struct type (WebhookReceived, WebhookFailed,
// SessionStuck, or ModelTurn).
func translateSingle[T any](t *testing.T, payload []byte, fn func(r *resourcepb.Resource, s *tracepb.Span) (T, string)) ([]T, []Quarantine) {
	t.Helper()
	rs, err := DecodeTraces(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var out []T
	var quarantined []Quarantine
	IterSpans(rs, func(r *resourcepb.Resource, s *tracepb.Span) {
		result, reason := fn(r, s)
		if reason != "" {
			quarantined = append(quarantined, makeQuarantine(reason, s))
			return
		}
		out = append(out, result)
	})
	return out, quarantined
}
