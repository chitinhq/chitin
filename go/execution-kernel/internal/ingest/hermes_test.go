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

func TestParseHermesEvents_HappyPath(t *testing.T) {
	raw := loadHermesFixture(t, "post_api_request_happy.jsonl")
	turns, quarantined, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("want 0 quarantined, got %d: %+v", len(quarantined), quarantined)
	}
	if len(turns) != 1 {
		t.Fatalf("want 1 turn, got %d", len(turns))
	}
	mt := turns[0]
	if mt.Surface != "hermes" {
		t.Errorf("Surface: got %q want \"hermes\"", mt.Surface)
	}
	if mt.Provider != "custom" {
		t.Errorf("Provider: got %q want \"custom\" (hermes normalizes custom-endpoint providers)", mt.Provider)
	}
	// response_model preferred over model — spec § Happy path.
	if mt.ModelName != "glm-5.1" {
		t.Errorf("ModelName: got %q want \"glm-5.1\" (response_model strips :cloud)", mt.ModelName)
	}
	if mt.InputTokens != 1024 {
		t.Errorf("InputTokens: got %d", mt.InputTokens)
	}
	if mt.OutputTokens != 256 {
		t.Errorf("OutputTokens: got %d", mt.OutputTokens)
	}
	if mt.CacheReadTokens != 128 {
		t.Errorf("CacheReadTokens: got %d want 128 (top-level usage.cache_read_tokens)", mt.CacheReadTokens)
	}
	if mt.SessionIDExternal != "s1" {
		t.Errorf("SessionIDExternal: got %q", mt.SessionIDExternal)
	}
	if mt.DurationMs != 2345 {
		t.Errorf("DurationMs: got %d (want 2345 from api_duration=2.345)", mt.DurationMs)
	}
	if mt.Ts != "2026-04-21T19:00:00+00:00" {
		t.Errorf("Ts: got %q (line-level ts passthrough)", mt.Ts)
	}
	wantTrace := hermesSyntheticTraceID("s1")
	wantSpan := hermesSyntheticSpanID("s1", 1)
	if mt.TraceID != wantTrace {
		t.Errorf("TraceID: got %q want %q", mt.TraceID, wantTrace)
	}
	if mt.SpanID != wantSpan {
		t.Errorf("SpanID: got %q want %q", mt.SpanID, wantSpan)
	}
}

func TestParseHermesEvents_MissingSessionID_Quarantined(t *testing.T) {
	raw := loadHermesFixture(t, "missing_session_id.jsonl")
	turns, quarantined, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	if len(turns) != 0 {
		t.Fatalf("want 0 turns, got %d", len(turns))
	}
	if len(quarantined) != 1 {
		t.Fatalf("want 1 quarantined, got %d", len(quarantined))
	}
	if !strings.HasPrefix(quarantined[0].Reason, "missing_fields:") {
		t.Errorf("want Reason to start with 'missing_fields:', got %q", quarantined[0].Reason)
	}
	if !strings.Contains(quarantined[0].Reason, "session_id") {
		t.Errorf("Reason should name session_id, got %q", quarantined[0].Reason)
	}
}

func TestParseHermesEvents_DeterministicOrdering(t *testing.T) {
	// Same input, parsed twice, must yield identical turn + quarantine
	// ordering. Guards against map-iteration-order leaking into output.
	raw := []byte(strings.Join([]string{
		`{"event_type": "post_api_request", "ts": "2026-04-21T19:00:02+00:00", "kwargs": {"session_id": "s1", "api_call_count": 2, "model": "m", "usage": {"input_tokens": 5, "output_tokens": 2}, "api_duration": 0.1}}`,
		`{"event_type": "post_api_request", "ts": "2026-04-21T19:00:01+00:00", "kwargs": {"session_id": "s1", "api_call_count": 1, "model": "m", "usage": {"input_tokens": 4, "output_tokens": 1}, "api_duration": 0.1}}`,
		`{"event_type": "on_session_start", "ts": "2026-04-21T19:00:00+00:00", "kwargs": {"session_id": "s1"}}`,
		`{"event_type": "pre_tool_call", "ts": "2026-04-21T19:00:00+00:00", "kwargs": {"tool_name": "t"}}`,
		"",
	}, "\n"))

	turnsA, quarA, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	turnsB, quarB, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}

	if len(turnsA) != 2 {
		t.Fatalf("want 2 turns, got %d", len(turnsA))
	}
	// Turns should be sorted by Ts ascending even though input was Ts descending.
	if turnsA[0].Ts >= turnsA[1].Ts {
		t.Errorf("turns not sorted by Ts ascending: [0]=%q [1]=%q", turnsA[0].Ts, turnsA[1].Ts)
	}
	// Determinism check.
	for i := range turnsA {
		if turnsA[i].SpanID != turnsB[i].SpanID {
			t.Errorf("turn %d span diverges: A=%q B=%q", i, turnsA[i].SpanID, turnsB[i].SpanID)
		}
	}
	if len(quarA) != len(quarB) {
		t.Fatalf("quarantine count diverges: A=%d B=%d", len(quarA), len(quarB))
	}
	for i := range quarA {
		if quarA[i].SpanName != quarB[i].SpanName {
			t.Errorf("quar %d name diverges: A=%q B=%q", i, quarA[i].SpanName, quarB[i].SpanName)
		}
	}
}

func TestParseHermesEvents_MissingUsage_KeepsTurn(t *testing.T) {
	raw := loadHermesFixture(t, "missing_usage.jsonl")
	turns, quarantined, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("want 0 quarantined (usage=null is kept), got %d", len(quarantined))
	}
	if len(turns) != 1 {
		t.Fatalf("want 1 turn, got %d", len(turns))
	}
	if turns[0].InputTokens != 0 || turns[0].OutputTokens != 0 {
		t.Errorf("tokens should be 0 when usage is nil, got in=%d out=%d",
			turns[0].InputTokens, turns[0].OutputTokens)
	}
}
