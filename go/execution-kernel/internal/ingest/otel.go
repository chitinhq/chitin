// Package ingest — otel.go decodes OTLP/protobuf payloads into span slices
// for downstream dialect translators. Dialect-agnostic.
package ingest

import (
	"fmt"

	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// DecodeTraces reads one binary OTLP/protobuf payload (a TracesData or
// ExportTraceServiceRequest body — both have the same wire shape for
// ResourceSpans) and returns the span slice. Malformed input is fatal.
func DecodeTraces(data []byte) ([]*tracepb.ResourceSpans, error) {
	var td tracepb.TracesData
	if err := proto.Unmarshal(data, &td); err != nil {
		return nil, fmt.Errorf("otlp_decode_failed: %w", err)
	}
	return td.ResourceSpans, nil
}

// IterSpans walks every span across all ResourceSpans / ScopeSpans,
// yielding each span with the resource it belongs to. Hides protobuf
// nesting from dialect callers.
func IterSpans(rs []*tracepb.ResourceSpans, fn func(r *resourcepb.Resource, s *tracepb.Span)) {
	for _, r := range rs {
		for _, scope := range r.ScopeSpans {
			for _, span := range scope.Spans {
				fn(r.Resource, span)
			}
		}
	}
}
