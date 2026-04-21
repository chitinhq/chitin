package ingest

import (
	"encoding/hex"
	"os"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
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
	turns, quarantined, err := ParseOpenClawSpans(rs)
	if err != nil {
		t.Fatalf("ParseOpenClawSpans: %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("want 0 quarantined, got %d: %+v", len(quarantined), quarantined)
	}
	if len(turns) != 1 {
		t.Fatalf("want 1 turn, got %d", len(turns))
	}
	mt := turns[0]
	if mt.TraceID != "0102030405060708090a0b0c0d0e0f10" {
		t.Errorf("TraceID: got %q", mt.TraceID)
	}
	if mt.SpanID != "a1a2a3a4a5a6a7a8" {
		t.Errorf("SpanID: got %q", mt.SpanID)
	}
	if mt.Ts != "2026-04-20T12:00:00Z" {
		t.Errorf("Ts: got %q", mt.Ts)
	}
	if mt.Surface != "openclaw-gateway" {
		t.Errorf("Surface: got %q", mt.Surface)
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
	// These imports may be unused once the test is fleshed out in Task 5;
	// silence the lint proactively so this test file compiles on its own.
	_ = hex.EncodeToString
	_ = commonpb.KeyValue{}
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
			turns, q, err := ParseOpenClawSpans(rs)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if len(turns) != 0 {
				t.Errorf("want 0 turns, got %d", len(turns))
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
