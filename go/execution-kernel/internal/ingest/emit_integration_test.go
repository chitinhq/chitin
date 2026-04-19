package ingest

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
)

// fixtureTranscript has 3 assistant turns (2 plain, 1 with thinking + cache tokens)
// and 2 user turns that must be ignored.
const fixtureTranscript = `{"type":"user","timestamp":"2026-04-19T10:00:00.000Z","message":{"content":[{"type":"text","text":"first user msg"}]}}
{"type":"assistant","timestamp":"2026-04-19T10:00:01.000Z","message":{"id":"msg_1","model":"claude-opus-4-7","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"text","text":"turn one"}]}}
{"type":"user","timestamp":"2026-04-19T10:00:02.000Z","message":{"content":[{"type":"text","text":"second user msg"}]}}
{"type":"assistant","timestamp":"2026-04-19T10:00:03.000Z","message":{"id":"msg_2","model":"claude-opus-4-7","usage":{"input_tokens":15,"output_tokens":8,"cache_read_input_tokens":200},"content":[{"type":"thinking","thinking":"let me reason"},{"type":"text","text":"turn two"}]}}
{"type":"assistant","timestamp":"2026-04-19T10:00:05.000Z","message":{"id":"msg_3","model":"claude-opus-4-7","usage":{"input_tokens":20,"output_tokens":12,"cache_creation_input_tokens":50},"content":[{"type":"text","text":"turn three"}]}}
`

func validTemplate() *event.Event {
	return &event.Event{
		SchemaVersion:   "2",
		RunID:           "550e8400-e29b-41d4-a716-446655440000",
		SessionID:       "550e8400-e29b-41d4-a716-446655440001",
		Surface:         "claude-code",
		AgentInstanceID: "550e8400-e29b-41d4-a716-446655440002",
		ChainID:         "550e8400-e29b-41d4-a716-446655440003",
		ChainType:       "session",
	}
}

func newTestEmitter(t *testing.T) (*emit.Emitter, string) {
	t.Helper()
	dir := t.TempDir()
	idx, err := chain.OpenIndex(filepath.Join(dir, "chain_index.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	logPath := filepath.Join(dir, "events.jsonl")
	return &emit.Emitter{LogPath: logPath, Index: idx}, logPath
}

func readJSONLLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad JSON line: %v", err)
		}
		lines = append(lines, m)
	}
	return lines
}

// TestIngestTranscript_EmitsOnePerTurn verifies:
//   - Exactly 3 assistant_turn events written for a 3-turn fixture.
//   - Seqs are 0, 1, 2 in order.
//   - prev_hash linkage is valid (seq 0 nil, each subsequent matches prior this_hash).
//   - Payload fields (text, thinking, usage) match the fixture turns.
func TestIngestTranscript_EmitsOnePerTurn(t *testing.T) {
	turns, err := ParseAssistantTurns([]byte(fixtureTranscript))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 3 {
		t.Fatalf("expected 3 turns from fixture, got %d", len(turns))
	}

	em, logPath := newTestEmitter(t)
	tmpl := validTemplate()

	n, err := EmitTurns(em, tmpl, turns)
	if err != nil {
		t.Fatalf("EmitTurns: %v", err)
	}
	if n != 3 {
		t.Errorf("EmitTurns returned %d, want 3", n)
	}

	lines := readJSONLLines(t, logPath)
	if len(lines) != 3 {
		t.Fatalf("JSONL has %d lines, want 3", len(lines))
	}

	// Seq 0, 1, 2.
	for i, line := range lines {
		seq := int64(line["seq"].(float64))
		if seq != int64(i) {
			t.Errorf("line %d: seq=%d, want %d", i, seq, i)
		}
	}

	// Seq 0 must have null prev_hash.
	if lines[0]["prev_hash"] != nil {
		t.Errorf("seq=0 prev_hash must be null, got %v", lines[0]["prev_hash"])
	}

	// Hash linkage.
	for k := 1; k < 3; k++ {
		prevThis := lines[k-1]["this_hash"]
		curPrev := lines[k]["prev_hash"]
		if prevThis != curPrev {
			t.Errorf("seq %d: prev_hash=%v != seq %d this_hash=%v", k, curPrev, k-1, prevThis)
		}
	}

	// Verify event_type.
	for i, line := range lines {
		if line["event_type"] != "assistant_turn" {
			t.Errorf("line %d: event_type=%v, want assistant_turn", i, line["event_type"])
		}
	}

	// Payload assertions: unmarshal and check key fields.
	type payloadShape struct {
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
		ModelUsed struct {
			Name     string `json:"name"`
			Provider string `json:"provider"`
		} `json:"model_used"`
		Usage struct {
			InputTokens              int64  `json:"input_tokens"`
			OutputTokens             int64  `json:"output_tokens"`
			CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}

	// Turn 0: text "turn one", no thinking, 10/5 tokens.
	var p0 payloadShape
	rawP0, _ := json.Marshal(lines[0]["payload"])
	json.Unmarshal(rawP0, &p0)
	if p0.Text != "turn one" {
		t.Errorf("turn 0 text=%q, want 'turn one'", p0.Text)
	}
	if p0.Thinking != "" {
		t.Errorf("turn 0 thinking must be empty, got %q", p0.Thinking)
	}
	if p0.Usage.InputTokens != 10 || p0.Usage.OutputTokens != 5 {
		t.Errorf("turn 0 usage mismatch: %+v", p0.Usage)
	}
	if p0.ModelUsed.Provider != "anthropic" {
		t.Errorf("turn 0 provider=%q, want anthropic", p0.ModelUsed.Provider)
	}

	// Turn 1: thinking + cache_read.
	var p1 payloadShape
	rawP1, _ := json.Marshal(lines[1]["payload"])
	json.Unmarshal(rawP1, &p1)
	if p1.Thinking != "let me reason" {
		t.Errorf("turn 1 thinking=%q, want 'let me reason'", p1.Thinking)
	}
	if p1.Usage.CacheReadInputTokens == nil || *p1.Usage.CacheReadInputTokens != 200 {
		t.Errorf("turn 1 cache_read_input_tokens: %+v", p1.Usage)
	}

	// Turn 2: cache_creation.
	var p2 payloadShape
	rawP2, _ := json.Marshal(lines[2]["payload"])
	json.Unmarshal(rawP2, &p2)
	if p2.Text != "turn three" {
		t.Errorf("turn 2 text=%q, want 'turn three'", p2.Text)
	}
	if p2.Usage.CacheCreationInputTokens == nil || *p2.Usage.CacheCreationInputTokens != 50 {
		t.Errorf("turn 2 cache_creation_input_tokens: %+v", p2.Usage)
	}
}

// TestIngestTranscript_InvalidTemplate_Errors verifies that illegal template
// states are caught before any emission occurs.
func TestIngestTranscript_InvalidTemplate_Errors(t *testing.T) {
	turns, _ := ParseAssistantTurns([]byte(fixtureTranscript))

	cases := []struct {
		name   string
		mutate func(t *event.Event)
		errSub string
	}{
		{
			name:   "missing schema_version",
			mutate: func(t *event.Event) { t.SchemaVersion = "" },
			errSub: "schema_version",
		},
		{
			name:   "wrong schema_version",
			mutate: func(t *event.Event) { t.SchemaVersion = "1" },
			errSub: "schema_version",
		},
		{
			name:   "empty run_id",
			mutate: func(t *event.Event) { t.RunID = "" },
			errSub: "run_id",
		},
		{
			name:   "empty session_id",
			mutate: func(t *event.Event) { t.SessionID = "" },
			errSub: "session_id",
		},
		{
			name:   "empty surface",
			mutate: func(t *event.Event) { t.Surface = "" },
			errSub: "surface",
		},
		{
			name:   "empty chain_id",
			mutate: func(t *event.Event) { t.ChainID = "" },
			errSub: "chain_id",
		},
		{
			name:   "wrong chain_type",
			mutate: func(t *event.Event) { t.ChainType = "tool_call" },
			errSub: "chain_type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			em, _ := newTestEmitter(t)
			tmpl := validTemplate()
			tc.mutate(tmpl)

			_, err := EmitTurns(em, tmpl, turns)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tc.errSub != "" {
				errStr := err.Error()
				found := false
				for _, s := range []string{tc.errSub} {
					if len(errStr) > 0 && contains(errStr, s) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("error %q does not mention %q", errStr, tc.errSub)
				}
			}
		})
	}
}

// TestIngestTranscript_OmitsOptionalFields verifies that a turn with no
// thinking and no optional token counts produces a payload without those keys.
func TestIngestTranscript_OmitsOptionalFields(t *testing.T) {
	minimalTurn := []AssistantTurn{
		{
			Text:      "hello",
			Thinking:  "", // explicitly empty
			ModelName: "claude-opus-4-7",
			Ts:        "2026-04-19T10:00:00.000Z",
			Usage: Usage{
				InputTokens:  5,
				OutputTokens: 3,
				// CacheCreationInputTokens, CacheReadInputTokens, ThinkingTokens all nil
			},
		},
	}

	em, logPath := newTestEmitter(t)
	tmpl := validTemplate()

	n, err := EmitTurns(em, tmpl, minimalTurn)
	if err != nil {
		t.Fatalf("EmitTurns: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1 emitted, got %d", n)
	}

	lines := readJSONLLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("want 1 JSONL line, got %d", len(lines))
	}

	// Round-trip the payload through JSON to check field presence.
	rawPayload, _ := json.Marshal(lines[0]["payload"])
	var payloadMap map[string]any
	if err := json.Unmarshal(rawPayload, &payloadMap); err != nil {
		t.Fatal(err)
	}

	// "thinking" must be absent (omitempty + empty string).
	if _, present := payloadMap["thinking"]; present {
		t.Error("payload must not contain 'thinking' key when thinking is empty")
	}

	// Drill into usage.
	usage, ok := payloadMap["usage"].(map[string]any)
	if !ok {
		t.Fatalf("payload.usage is not a map: %T", payloadMap["usage"])
	}

	for _, optField := range []string{"cache_creation_input_tokens", "cache_read_input_tokens", "thinking_tokens"} {
		if _, present := usage[optField]; present {
			t.Errorf("payload.usage must not contain %q when nil, but it does", optField)
		}
	}
}

// contains is a simple substring check to avoid importing strings in test file.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
