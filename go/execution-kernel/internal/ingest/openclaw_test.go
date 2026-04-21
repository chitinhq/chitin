package ingest

import (
	"os"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// loadFixture reads the SP-1 synthesized fixture and decodes it.
func loadFixture(t *testing.T) []*tracepb.ResourceSpans {
	t.Helper()
	data, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rs, err := DecodeTraces(data)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return rs
}

func TestParseOpenClawSpans_HappyPath(t *testing.T) {
	rs := loadFixture(t)
	spans, quarantined, err := ParseOpenClawSpans(rs)
	if err != nil {
		t.Fatalf("ParseOpenClawSpans: %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("want 0 quarantined, got %d: %+v", len(quarantined), quarantined)
	}
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	mt, ok := spans[0].(ModelTurn)
	if !ok {
		t.Fatalf("want ModelTurn, got %T", spans[0])
	}
	if mt.TraceID != "0102030405060708090a0b0c0d0e0f10" {
		t.Errorf("TraceID: got %q", mt.TraceID)
	}
	if mt.SpanIDHex != "a1a2a3a4a5a6a7a8" {
		t.Errorf("SpanIDHex: got %q", mt.SpanIDHex)
	}
	if mt.TsStr != "2026-04-20T12:00:00Z" {
		t.Errorf("TsStr: got %q", mt.TsStr)
	}
	if mt.SurfaceStr != "openclaw-gateway" {
		t.Errorf("SurfaceStr: got %q", mt.SurfaceStr)
	}
	if mt.Provider != "ollama" {
		t.Errorf("Provider: got %q", mt.Provider)
	}
	if mt.ModelName != "qwen2.5:0.5b" {
		t.Errorf("ModelName: got %q", mt.ModelName)
	}
	if mt.InputTokens != 42 {
		t.Errorf("InputTokens: got %d", mt.InputTokens)
	}
	if mt.OutputTokens != 17 {
		t.Errorf("OutputTokens: got %d", mt.OutputTokens)
	}
	if mt.SessionIDExternal != "sp1-fixture-session" {
		t.Errorf("SessionIDExternal: got %q", mt.SessionIDExternal)
	}
	if mt.DurationMs != 1500 {
		t.Errorf("DurationMs: got %d", mt.DurationMs)
	}
	if mt.CacheReadTokens != 3 {
		t.Errorf("CacheReadTokens: got %d", mt.CacheReadTokens)
	}
	if mt.CacheWriteTokens != 0 {
		t.Errorf("CacheWriteTokens: got %d (should be 0)", mt.CacheWriteTokens)
	}
}

func TestParseOpenClawSpans_RequiredAttrMissing(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*tracepb.ResourceSpans)
		reason string
	}{
		{
			name:   "missing_service_name",
			mutate: func(rs *tracepb.ResourceSpans) { rs.Resource.Attributes = nil },
			reason: "missing_required_attr:service.name",
		},
		{
			name: "zero_trace_id",
			mutate: func(rs *tracepb.ResourceSpans) {
				rs.ScopeSpans[0].Spans[0].TraceId = make([]byte, 16)
			},
			reason: "invalid_trace_id_zero",
		},
		{
			name: "zero_start_time",
			mutate: func(rs *tracepb.ResourceSpans) {
				rs.ScopeSpans[0].Spans[0].StartTimeUnixNano = 0
			},
			reason: "missing_required_attr:start_time_unix_nano",
		},
		{
			name: "missing_openclaw_provider",
			mutate: func(rs *tracepb.ResourceSpans) {
				removeSpanAttr(rs.ScopeSpans[0].Spans[0], "openclaw.provider")
			},
			reason: "missing_required_attr:openclaw.provider",
		},
		{
			name: "missing_openclaw_model",
			mutate: func(rs *tracepb.ResourceSpans) {
				removeSpanAttr(rs.ScopeSpans[0].Spans[0], "openclaw.model")
			},
			reason: "missing_required_attr:openclaw.model",
		},
		{
			name: "missing_input_tokens",
			mutate: func(rs *tracepb.ResourceSpans) {
				removeSpanAttr(rs.ScopeSpans[0].Spans[0], "openclaw.tokens.input")
			},
			reason: "missing_required_attr:openclaw.tokens.input",
		},
		{
			name: "missing_output_tokens",
			mutate: func(rs *tracepb.ResourceSpans) {
				removeSpanAttr(rs.ScopeSpans[0].Spans[0], "openclaw.tokens.output")
			},
			reason: "missing_required_attr:openclaw.tokens.output",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs := loadFixture(t)
			tc.mutate(rs[0])
			spans, q, err := ParseOpenClawSpans(rs)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if len(spans) != 0 {
				t.Errorf("want 0 spans, got %d", len(spans))
			}
			if len(q) != 1 {
				t.Fatalf("want 1 quarantined, got %d", len(q))
			}
			if q[0].Reason != tc.reason {
				t.Errorf("want reason %q, got %q", tc.reason, q[0].Reason)
			}
		})
	}
}

// removeSpanAttr deletes every occurrence of key from the span's Attributes.
func removeSpanAttr(s *tracepb.Span, key string) {
	out := s.Attributes[:0]
	for _, kv := range s.Attributes {
		if kv.Key != key {
			out = append(out, kv)
		}
	}
	s.Attributes = out
}

func TestParseOpenClawSpans_UnknownValueRejected(t *testing.T) {
	cases := []struct {
		attr   string
		reason string
	}{
		{"openclaw.provider", "unknown_value:openclaw.provider"},
		{"openclaw.model", "unknown_value:openclaw.model"},
	}
	for _, tc := range cases {
		t.Run(tc.attr, func(t *testing.T) {
			rs := loadFixture(t)
			for _, kv := range rs[0].ScopeSpans[0].Spans[0].Attributes {
				if kv.Key == tc.attr {
					kv.Value = &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "unknown"}}
				}
			}
			spans, q, _ := ParseOpenClawSpans(rs)
			if len(spans) != 0 {
				t.Errorf("want 0 spans, got %d", len(spans))
			}
			if len(q) != 1 || q[0].Reason != tc.reason {
				t.Errorf("want quarantine reason %q, got %+v", tc.reason, q)
			}
		})
	}
}

func TestParseOpenClawSpans_UnmappedSpanName(t *testing.T) {
	rs := loadFixture(t)
	rs[0].ScopeSpans[0].Spans[0].Name = "openclaw.webhook.processed"
	spans, q, _ := ParseOpenClawSpans(rs)
	if len(spans) != 0 || len(q) != 1 {
		t.Fatalf("want 0/1, got %d/%d", len(spans), len(q))
	}
	if q[0].Reason != "unmapped_span_name:openclaw.webhook.processed" {
		t.Errorf("reason: got %q", q[0].Reason)
	}
}

func TestParseOpenClawSpans_OptionalAbsent(t *testing.T) {
	rs := loadFixture(t)
	// Remove all optional attrs.
	removeSpanAttr(rs[0].ScopeSpans[0].Spans[0], "openclaw.sessionId")
	removeSpanAttr(rs[0].ScopeSpans[0].Spans[0], "openclaw.tokens.cache_read")
	removeSpanAttr(rs[0].ScopeSpans[0].Spans[0], "openclaw.tokens.cache_write")
	rs[0].ScopeSpans[0].Spans[0].EndTimeUnixNano = 0
	spans, q, _ := ParseOpenClawSpans(rs)
	if len(q) != 0 || len(spans) != 1 {
		t.Fatalf("want 1/0, got %d/%d", len(spans), len(q))
	}
	mt, ok := spans[0].(ModelTurn)
	if !ok {
		t.Fatalf("want ModelTurn, got %T", spans[0])
	}
	if mt.SessionIDExternal != "" || mt.DurationMs != 0 ||
		mt.CacheReadTokens != 0 || mt.CacheWriteTokens != 0 {
		t.Errorf("optional fields should be zero, got %+v", mt)
	}
}

func TestParseOpenClawSpans_MultipleSpansOrderedByTime(t *testing.T) {
	rs := loadFixture(t)
	origSpan := rs[0].ScopeSpans[0].Spans[0]
	laterSpan := cloneSpan(origSpan)
	laterSpan.StartTimeUnixNano = origSpan.StartTimeUnixNano + 1_000_000_000 // +1s
	laterSpan.EndTimeUnixNano = origSpan.EndTimeUnixNano + 1_000_000_000
	laterSpan.SpanId = []byte{0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8}
	rs[0].ScopeSpans[0].Spans = append(rs[0].ScopeSpans[0].Spans, laterSpan)
	spans, q, _ := ParseOpenClawSpans(rs)
	if len(q) != 0 || len(spans) != 2 {
		t.Fatalf("want 2 spans, got spans=%d q=%d", len(spans), len(q))
	}
	if spans[0].Ts() > spans[1].Ts() {
		t.Errorf("ordering wrong: %q before %q", spans[0].Ts(), spans[1].Ts())
	}
}

func TestParseOpenClawSpans_TieBreakerSpanID(t *testing.T) {
	rs := loadFixture(t)
	origSpan := rs[0].ScopeSpans[0].Spans[0]
	twin := cloneSpan(origSpan)
	// Same start time, larger span_id → should sort after.
	twin.SpanId = []byte{0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8}
	rs[0].ScopeSpans[0].Spans = append(rs[0].ScopeSpans[0].Spans, twin)
	spans, _, _ := ParseOpenClawSpans(rs)
	if len(spans) != 2 {
		t.Fatalf("want 2, got %d", len(spans))
	}
	if spans[0].SpanID() >= spans[1].SpanID() {
		t.Errorf("tie-breaker: spans[0].SpanID %q should be < spans[1].SpanID %q",
			spans[0].SpanID(), spans[1].SpanID())
	}
}

func TestParseOpenClawSpans_DuplicateAttrKeyLastWins(t *testing.T) {
	rs := loadFixture(t)
	attrs := rs[0].ScopeSpans[0].Spans[0].Attributes
	// Add a second openclaw.model attribute AFTER the first — the last should win.
	attrs = append(attrs, &commonpb.KeyValue{
		Key:   "openclaw.model",
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "surprise:7b"}},
	})
	rs[0].ScopeSpans[0].Spans[0].Attributes = attrs
	spans, q, _ := ParseOpenClawSpans(rs)
	if len(q) != 0 || len(spans) != 1 {
		t.Fatalf("want 1/0, got %d/%d", len(spans), len(q))
	}
	mt, ok := spans[0].(ModelTurn)
	if !ok {
		t.Fatalf("want ModelTurn, got %T", spans[0])
	}
	if mt.ModelName != "surprise:7b" {
		t.Errorf("last-write-wins failed; got %q", mt.ModelName)
	}
}

// cloneSpan returns a deep copy so mutations in a test do not bleed
// between sub-tests. Uses proto.Clone to avoid copying the embedded mutex.
func cloneSpan(s *tracepb.Span) *tracepb.Span {
	return proto.Clone(s).(*tracepb.Span)
}

func TestParseOpenClawSpans_InvalidTraceIDLength(t *testing.T) {
	rs := loadFixture(t)
	// Set trace_id to 8 bytes (valid span_id length, invalid trace_id length).
	rs[0].ScopeSpans[0].Spans[0].TraceId = []byte{1, 2, 3, 4, 5, 6, 7, 8}
	spans, q, _ := ParseOpenClawSpans(rs)
	if len(spans) != 0 || len(q) != 1 {
		t.Fatalf("want 0/1, got %d/%d", len(spans), len(q))
	}
	if q[0].Reason != "invalid_trace_id_length" {
		t.Errorf("reason: got %q", q[0].Reason)
	}
}

func TestParseOpenClawSpans_InvalidSpanIDLength(t *testing.T) {
	rs := loadFixture(t)
	// Set span_id to 4 bytes (too short).
	rs[0].ScopeSpans[0].Spans[0].SpanId = []byte{1, 2, 3, 4}
	spans, q, _ := ParseOpenClawSpans(rs)
	if len(spans) != 0 || len(q) != 1 {
		t.Fatalf("want 0/1, got %d/%d", len(spans), len(q))
	}
	if q[0].Reason != "invalid_span_id_length" {
		t.Errorf("reason: got %q", q[0].Reason)
	}
}

func TestBuildChainID_UniformFormat(t *testing.T) {
	trace := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
		0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff,
	}
	span := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	got := buildChainID(trace, span)
	want := "otel:00112233445566778899aabbccddeeff:0102030405060708"
	if got != want {
		t.Fatalf("buildChainID mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildOtelLabels_OmitsEmptyParent(t *testing.T) {
	got := buildOtelLabels("abc", "def", "")
	if _, ok := got["otel_parent_span_id"]; ok {
		t.Fatalf("empty parent should be omitted, got %+v", got)
	}
	if got["otel_trace_id"] != "abc" || got["otel_span_id"] != "def" {
		t.Fatalf("trace/span mismatch: %+v", got)
	}
	if got["source"] != "otel" || got["dialect"] != "openclaw" {
		t.Fatalf("constant labels missing: %+v", got)
	}
}

func TestBuildOtelLabels_IncludesParentWhenSet(t *testing.T) {
	got := buildOtelLabels("abc", "def", "parent-hex")
	if got["otel_parent_span_id"] != "parent-hex" {
		t.Fatalf("parent label missing: %+v", got)
	}
}

func TestParseOpenClawSpans_NegativeTokens(t *testing.T) {
	cases := []struct {
		name   string
		attr   string
		reason string
	}{
		{"negative_input", "openclaw.tokens.input", "invalid_value:openclaw.tokens.input"},
		{"negative_output", "openclaw.tokens.output", "invalid_value:openclaw.tokens.output"},
		{"negative_cache_read", "openclaw.tokens.cache_read", "invalid_value:openclaw.tokens.cache_read"},
		{"negative_cache_write", "openclaw.tokens.cache_write", "invalid_value:openclaw.tokens.cache_write"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs := loadFixture(t)
			// Handle cache_write which is absent by default in fixture; append it.
			span := rs[0].ScopeSpans[0].Spans[0]
			found := false
			for _, kv := range span.Attributes {
				if kv.Key == tc.attr {
					kv.Value = &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: -1}}
					found = true
					break
				}
			}
			if !found {
				span.Attributes = append(span.Attributes, &commonpb.KeyValue{
					Key:   tc.attr,
					Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: -1}},
				})
			}
			spans, q, _ := ParseOpenClawSpans(rs)
			if len(spans) != 0 || len(q) != 1 {
				t.Fatalf("want 0/1, got %d/%d", len(spans), len(q))
			}
			if q[0].Reason != tc.reason {
				t.Errorf("reason: got %q, want %q", q[0].Reason, tc.reason)
			}
		})
	}
}
