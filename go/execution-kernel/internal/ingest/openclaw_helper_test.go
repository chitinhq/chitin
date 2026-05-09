package ingest

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func TestParentSpanIDHex(t *testing.T) {
	t.Run("8-byte non-zero parent", func(t *testing.T) {
		span := &tracepb.Span{
			ParentSpanId: []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}
		got := parentSpanIDHex(span)
		if got == "" {
			t.Error("expected non-empty hex for non-zero parent span ID")
		}
	})

	t.Run("empty parent", func(t *testing.T) {
		span := &tracepb.Span{}
		got := parentSpanIDHex(span)
		if got != "" {
			t.Errorf("expected empty string for no parent, got %q", got)
		}
	})

	t.Run("all-zero parent", func(t *testing.T) {
		span := &tracepb.Span{
			ParentSpanId: []byte{0, 0, 0, 0, 0, 0, 0, 0},
		}
		got := parentSpanIDHex(span)
		if got != "" {
			t.Errorf("expected empty for all-zero parent, got %q", got)
		}
	})

	t.Run("short parent (4 bytes)", func(t *testing.T) {
		span := &tracepb.Span{
			ParentSpanId: []byte{1, 2, 3, 4},
		}
		got := parentSpanIDHex(span)
		if got != "" {
			t.Errorf("expected empty for short parent, got %q", got)
		}
	})
}

func TestGetResourceStringAttr(t *testing.T) {
	t.Run("nil resource returns empty", func(t *testing.T) {
		got := getResourceStringAttr(nil, "service.name")
		if got != "" {
			t.Errorf("expected empty for nil resource, got %q", got)
		}
	})

	t.Run("present attribute", func(t *testing.T) {
		r := &resourcepb.Resource{
			Attributes: []*commonpb.KeyValue{
				{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "my-service"}}},
			},
		}
		got := getResourceStringAttr(r, "service.name")
		if got != "my-service" {
			t.Errorf("expected 'my-service', got %q", got)
		}
	})

	t.Run("missing attribute", func(t *testing.T) {
		r := &resourcepb.Resource{
			Attributes: []*commonpb.KeyValue{
				{Key: "other.key", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "other-value"}}},
			},
		}
		got := getResourceStringAttr(r, "service.name")
		if got != "" {
			t.Errorf("expected empty for missing attr, got %q", got)
		}
	})
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"clean filename", "gen_ai.client.tool", "gen_ai.client.tool"},
		{"slashes become underscores", "path/to/file", "path_to_file"},
		{"backslashes", `path\to\file`, "path_to_file"},
		{"colons", "2026-05-09T10:30:00Z", "2026-05-09T10_30_00Z"},
		{"mixed", `a/b:c\d`, "a_b_c_d"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}