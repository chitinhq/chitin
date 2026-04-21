//go:build ignore

// One-shot helper: run with `go run gen_synthesized_fixture.go <output_path>`.
// Produces a minimal valid OTLP/protobuf payload containing exactly one
// `openclaw.model.usage` span with every required + optional attribute
// populated per the SP-1 spec (§Data flow).
package main

import (
	"fmt"
	"os"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func strAttr(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}
func intAttr(k string, v int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: v}}}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run gen_synthesized_fixture.go <output.pb>")
		os.Exit(2)
	}

	traceID := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
	}
	spanID := []byte{0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8}

	start := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC).UnixNano()
	end := time.Date(2026, 4, 20, 12, 0, 1, 500_000_000, time.UTC).UnixNano()

	span := &tracepb.Span{
		TraceId:           traceID,
		SpanId:            spanID,
		Name:              "openclaw.model.usage",
		StartTimeUnixNano: uint64(start),
		EndTimeUnixNano:   uint64(end),
		Attributes: []*commonpb.KeyValue{
			strAttr("openclaw.provider", "ollama"),
			strAttr("openclaw.model", "qwen2.5:0.5b"),
			strAttr("openclaw.channel", "cli"),
			strAttr("openclaw.sessionKey", "agent:main:main"),
			strAttr("openclaw.sessionId", "sp1-fixture-session"),
			intAttr("openclaw.tokens.input", 42),
			intAttr("openclaw.tokens.output", 17),
			intAttr("openclaw.tokens.cache_read", 3),
			intAttr("openclaw.tokens.cache_write", 0),
		},
	}

	resource := &resourcepb.Resource{
		Attributes: []*commonpb.KeyValue{
			strAttr("service.name", "openclaw-gateway"),
			strAttr("service.version", "2026.4.15"),
		},
	}

	scope := &tracepb.ScopeSpans{
		Scope: &commonpb.InstrumentationScope{Name: "openclaw"},
		Spans: []*tracepb.Span{span},
	}
	rs := &tracepb.ResourceSpans{
		Resource:   resource,
		ScopeSpans: []*tracepb.ScopeSpans{scope},
	}
	req := &tracepb.TracesData{ResourceSpans: []*tracepb.ResourceSpans{rs}}

	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[1], b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "wrote %d bytes to %s\n", len(b), os.Args[1])
}
