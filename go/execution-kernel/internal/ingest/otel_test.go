package ingest

import (
	"os"
	"path/filepath"
	"testing"

	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// fixturePath returns the SP-1 synthesized fixture path, resolved as a
// path relative to this package's test CWD. `go test` always runs with
// CWD == the package dir, so this path resolves correctly regardless of
// where the `go test` command was invoked from.
func fixturePath(t *testing.T) string {
	t.Helper()
	// go test CWD = package dir (internal/ingest). Climb 4 levels to repo root.
	return filepath.Join("..", "..", "..", "..",
		"docs", "observations", "fixtures",
		"2026-04-20-openclaw-otel-capture", "sp1",
		"synthesized-model-usage.pb")
}

func TestDecodeTraces_SynthesizedFixture(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rs, err := DecodeTraces(data)
	if err != nil {
		t.Fatalf("DecodeTraces: %v", err)
	}
	if len(rs) != 1 {
		t.Fatalf("want 1 ResourceSpans, got %d", len(rs))
	}
	var got []*tracepb.Span
	IterSpans(rs, func(_ *resourcepb.Resource, s *tracepb.Span) { got = append(got, s) })
	if len(got) != 1 {
		t.Fatalf("want 1 span via IterSpans, got %d", len(got))
	}
	if got[0].Name != "openclaw.model.usage" {
		t.Fatalf("want span name openclaw.model.usage, got %q", got[0].Name)
	}
}

func TestDecodeTraces_Malformed(t *testing.T) {
	_, err := DecodeTraces([]byte("not valid protobuf"))
	if err == nil {
		t.Fatal("want error on malformed input, got nil")
	}
}
