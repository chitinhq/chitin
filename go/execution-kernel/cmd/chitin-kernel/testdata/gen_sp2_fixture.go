//go:build ignore

// One-shot helper to produce sp2-mixed-fixture.pb — a TracesData payload
// containing one span of each openclaw span-type (model.usage,
// webhook.processed, webhook.error, session.stuck) plus one unmapped span
// name that should quarantine. Fixture bytes are wire-identical on every
// run given deterministic proto marshaling + sorted attribute keys.
//
// Run: go run gen_sp2_fixture.go sp2-mixed-fixture.pb
package main

import (
	"fmt"
	"os"
	"sort"
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

func fill(n int, b byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

type spanDef struct {
	name          string
	trace, span   []byte
	startNanos    uint64
	endNanos      uint64
	stringAttrs   map[string]string
	intAttrs      map[string]int64
	statusCode    tracepb.Status_StatusCode
	statusMessage string
}

func buildAttrs(s map[string]string, i map[string]int64) []*commonpb.KeyValue {
	var out []*commonpb.KeyValue
	sk := make([]string, 0, len(s))
	for k := range s {
		sk = append(sk, k)
	}
	sort.Strings(sk)
	for _, k := range sk {
		out = append(out, strAttr(k, s[k]))
	}
	ik := make([]string, 0, len(i))
	for k := range i {
		ik = append(ik, k)
	}
	sort.Strings(ik)
	for _, k := range ik {
		out = append(out, intAttr(k, i[k]))
	}
	return out
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gen_sp2_fixture.go <output.pb>")
		os.Exit(1)
	}
	output := os.Args[1]

	baseNanos := uint64(time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC).UnixNano())
	oneSecond := uint64(time.Second)

	defs := []spanDef{
		{
			name: "openclaw.model.usage", trace: fill(16, 0x01), span: fill(8, 0x01),
			startNanos: baseNanos, endNanos: baseNanos + oneSecond,
			stringAttrs: map[string]string{
				"openclaw.provider": "ollama",
				"openclaw.model":    "qwen2.5:0.5b",
			},
			intAttrs: map[string]int64{
				"openclaw.tokens.input":  10,
				"openclaw.tokens.output": 20,
			},
		},
		{
			name: "openclaw.webhook.processed", trace: fill(16, 0x02), span: fill(8, 0x02),
			startNanos: baseNanos, endNanos: baseNanos + oneSecond,
			stringAttrs: map[string]string{
				"openclaw.channel": "telegram",
				"openclaw.webhook": "message",
			},
		},
		{
			name: "openclaw.webhook.error", trace: fill(16, 0x03), span: fill(8, 0x03),
			startNanos: baseNanos, endNanos: baseNanos,
			stringAttrs: map[string]string{
				"openclaw.channel": "telegram",
				"openclaw.webhook": "message",
				"openclaw.error":   "boom",
			},
			statusCode:    tracepb.Status_STATUS_CODE_ERROR,
			statusMessage: "boom",
		},
		{
			name: "openclaw.session.stuck", trace: fill(16, 0x04), span: fill(8, 0x04),
			startNanos: baseNanos, endNanos: baseNanos,
			stringAttrs: map[string]string{"openclaw.state": "awaiting_model"},
			intAttrs:    map[string]int64{"openclaw.ageMs": 120000},
		},
		{
			name: "openclaw.message.processed", trace: fill(16, 0x05), span: fill(8, 0x05),
			startNanos: baseNanos, endNanos: baseNanos + oneSecond,
		},
	}

	spans := make([]*tracepb.Span, 0, len(defs))
	for _, d := range defs {
		sp := &tracepb.Span{
			Name:              d.name,
			TraceId:           d.trace,
			SpanId:            d.span,
			StartTimeUnixNano: d.startNanos,
			EndTimeUnixNano:   d.endNanos,
			Attributes:        buildAttrs(d.stringAttrs, d.intAttrs),
		}
		if d.statusCode != tracepb.Status_STATUS_CODE_UNSET {
			sp.Status = &tracepb.Status{Code: d.statusCode, Message: d.statusMessage}
		}
		spans = append(spans, sp)
	}

	td := &tracepb.TracesData{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				strAttr("service.name", "openclaw"),
			}},
			ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}},
		}},
	}

	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(td)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(output, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d bytes to %s\n", len(data), output)
}
