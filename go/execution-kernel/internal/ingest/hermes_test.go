package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadHermesFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hermes", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestBuildHermesChainID_UniformFormat(t *testing.T) {
	traceHex := "00112233445566778899aabbccddeeff"
	spanHex := "0102030405060708"
	got := buildHermesChainID(traceHex, spanHex)
	want := "hermes:00112233445566778899aabbccddeeff:0102030405060708"
	if got != want {
		t.Fatalf("buildHermesChainID mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestHermesSyntheticIDs_DeterministicFromSessionAndCall(t *testing.T) {
	trace1 := hermesSyntheticTraceID("session-abc")
	trace2 := hermesSyntheticTraceID("session-abc")
	if trace1 != trace2 {
		t.Fatalf("trace IDs should be deterministic; got %q vs %q", trace1, trace2)
	}
	if len(trace1) != 32 {
		t.Fatalf("trace ID should be 32 hex chars (128 bits); got len=%d", len(trace1))
	}

	span1 := hermesSyntheticSpanID("session-abc", 1)
	span2 := hermesSyntheticSpanID("session-abc", 1)
	if span1 != span2 {
		t.Fatalf("span IDs should be deterministic; got %q vs %q", span1, span2)
	}
	if len(span1) != 16 {
		t.Fatalf("span ID should be 16 hex chars (64 bits); got len=%d", len(span1))
	}

	spanCall2 := hermesSyntheticSpanID("session-abc", 2)
	if span1 == spanCall2 {
		t.Fatalf("different api_call_count should give different span IDs")
	}

	traceOther := hermesSyntheticTraceID("session-xyz")
	if trace1 == traceOther {
		t.Fatalf("different session_id should give different trace IDs")
	}
}

func TestParseHermesEvents_V1ScopeQuarantine(t *testing.T) {
	raw := loadHermesFixture(t, "v1_scope_quarantine.jsonl")
	turns, quarantined, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	if len(turns) != 0 {
		t.Fatalf("want 0 turns (no post_api_request in fixture), got %d", len(turns))
	}
	if len(quarantined) != 4 {
		t.Fatalf("want 4 quarantined (one per non-primary event), got %d", len(quarantined))
	}
	for _, q := range quarantined {
		if q.Reason != "v1-scope" {
			t.Errorf("every line should quarantine with v1-scope, got Reason=%q", q.Reason)
		}
	}
}

func TestParseHermesEvents_MalformedLineQuarantined(t *testing.T) {
	raw := loadHermesFixture(t, "malformed_line.jsonl")
	_, quarantined, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	// One malformed line should appear as parse_error. The two valid
	// post_api_request lines — task 8 leaves them as "not_yet_implemented"
	// quarantine; task 9 turns them into ModelTurns. We only assert the
	// malformed one here so this test stays green across both tasks.
	var parseErrors []Quarantine
	for _, q := range quarantined {
		if q.Reason == "parse_error" {
			parseErrors = append(parseErrors, q)
		}
	}
	if len(parseErrors) != 1 {
		t.Fatalf("want 1 parse_error quarantine, got %d", len(parseErrors))
	}
	if !strings.Contains(string(parseErrors[0].SpanRaw), "not valid json") {
		t.Errorf("quarantined SpanRaw should preserve the malformed line, got %q", string(parseErrors[0].SpanRaw))
	}
}
