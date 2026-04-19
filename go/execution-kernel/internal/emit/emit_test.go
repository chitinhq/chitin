package emit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
)

func newEnv(t *testing.T) (string, *chain.Index) {
	t.Helper()
	dir := t.TempDir()
	idx, err := chain.OpenIndex(filepath.Join(dir, "chain_index.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	return dir, idx
}

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var m map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			t.Fatalf("bad JSON: %v", err)
		}
		lines = append(lines, m)
	}
	return lines
}

func minimalSessionStart(chainID string, seq int64) *event.Event {
	return &event.Event{
		SchemaVersion:    "2",
		RunID:            "550e8400-e29b-41d4-a716-446655440000",
		SessionID:        "550e8400-e29b-41d4-a716-446655440001",
		Surface:          "claude-code",
		DriverIdentity:   event.DriverIdentity{User: "u", MachineID: "m", MachineFingerprint: "a" + repeat("0", 63)},
		AgentInstanceID:  "550e8400-e29b-41d4-a716-446655440002",
		AgentFingerprint: "b" + repeat("0", 63),
		EventType:        "session_start",
		ChainID:          chainID,
		ChainType:        "session",
		Seq:              seq, // ignored by Emit — recomputed
		Ts:               "2026-04-19T12:00:00.000Z",
		Labels:           map[string]string{},
		Payload:          json.RawMessage(`{"cwd":"/","client_info":{"name":"claude-code","version":"1"},"model":{"name":"x","provider":"y"},"system_prompt_hash":"` + repeat("0", 64) + `","tool_allowlist_hash":"` + repeat("0", 64) + `","agent_version":"1"}`),
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n*len(s))
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func TestEmit_FirstInChainHasZeroSeqAndNilPrev(t *testing.T) {
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}
	ev := minimalSessionStart("chainA", 0)
	if err := e.Emit(ev); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, e.LogPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0]["seq"].(float64) != 0 {
		t.Errorf("expected seq=0, got %v", lines[0]["seq"])
	}
	if lines[0]["prev_hash"] != nil {
		t.Errorf("expected prev_hash=null, got %v", lines[0]["prev_hash"])
	}
	if lines[0]["this_hash"] == "" {
		t.Errorf("this_hash must be non-empty")
	}
}

func TestEmit_SecondInChainLinksToFirst(t *testing.T) {
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}
	a := minimalSessionStart("chainA", 0)
	if err := e.Emit(a); err != nil {
		t.Fatal(err)
	}
	b := minimalSessionStart("chainA", 0) // seq ignored; emitter computes 1
	b.EventType = "user_prompt"
	b.Payload = json.RawMessage(`{"text":"hi"}`)
	if err := e.Emit(b); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, e.LogPath)
	if lines[1]["seq"].(float64) != 1 {
		t.Errorf("expected seq=1, got %v", lines[1]["seq"])
	}
	if lines[1]["prev_hash"] != lines[0]["this_hash"] {
		t.Errorf("prev_hash should equal previous this_hash: prev=%v prior_this=%v", lines[1]["prev_hash"], lines[0]["this_hash"])
	}
}

func TestEmit_TwoChainsAreIndependent(t *testing.T) {
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}
	a := minimalSessionStart("chainA", 0)
	b := minimalSessionStart("chainB", 0)
	e.Emit(a)
	e.Emit(b)
	lines := readLines(t, e.LogPath)
	if lines[0]["seq"].(float64) != 0 || lines[1]["seq"].(float64) != 0 {
		t.Errorf("each new chain should start at seq=0; got %v, %v", lines[0]["seq"], lines[1]["seq"])
	}
}
