package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const claudeTranscriptFixture = `{"type":"user","timestamp":"2026-04-19T12:00:00.000Z","message":{"content":[{"type":"text","text":"hello"}]}}
{"type":"assistant","timestamp":"2026-04-19T12:00:01.000Z","message":{"id":"msg_1","model":"claude-opus-4-7","usage":{"input_tokens":5,"output_tokens":3},"content":[{"type":"text","text":"hi back"}]}}
{"type":"user","timestamp":"2026-04-19T12:00:02.000Z","message":{"content":[{"type":"text","text":"again"}]}}
{"type":"assistant","timestamp":"2026-04-19T12:00:03.000Z","message":{"id":"msg_2","model":"claude-opus-4-7","usage":{"input_tokens":8,"output_tokens":4,"cache_read_input_tokens":100},"content":[{"type":"thinking","thinking":"let me think"},{"type":"text","text":"sure"}]}}
`

func TestParseAssistantTurns(t *testing.T) {
	turns, err := ParseAssistantTurns([]byte(claudeTranscriptFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("want 2 turns, got %d", len(turns))
	}
	if turns[0].Text != "hi back" {
		t.Errorf("first turn text = %q", turns[0].Text)
	}
	if turns[0].Thinking != "" {
		t.Errorf("first turn should have no thinking, got %q", turns[0].Thinking)
	}
	if turns[0].Usage.InputTokens != 5 {
		t.Errorf("first usage input = %d, want 5", turns[0].Usage.InputTokens)
	}
	if turns[1].Thinking != "let me think" {
		t.Errorf("second turn thinking = %q", turns[1].Thinking)
	}
	if turns[1].Usage.CacheReadInputTokens == nil || *turns[1].Usage.CacheReadInputTokens != 100 {
		t.Errorf("second turn cache_read not extracted: %+v", turns[1].Usage)
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript_checkpoint.json")
	os.WriteFile(path, []byte("{}"), 0o644)

	cp, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cp) != 0 {
		t.Errorf("expected empty checkpoint, got %+v", cp)
	}

	cp["sess-1"] = CheckpointEntry{TranscriptPath: "/tmp/x.jsonl", LastIngestOffset: 123, Status: "complete"}
	if err := SaveCheckpoint(path, cp); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(path)
	var verify map[string]CheckpointEntry
	if err := json.Unmarshal(b, &verify); err != nil {
		t.Fatal(err)
	}
	if verify["sess-1"].LastIngestOffset != 123 {
		t.Errorf("round-trip lost offset: %+v", verify)
	}
}
