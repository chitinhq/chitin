package emit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
)

func TestAppendEvent_writesOneJSONLLine(t *testing.T) {
	dir := t.TempDir()

	ev := event.Event{
		RunID:         "550e8400-e29b-41d4-a716-446655440000",
		SessionID:     "550e8400-e29b-41d4-a716-446655440001",
		Surface:       "claude-code",
		Driver:        "claude",
		AgentID:       "agent-xyz",
		ToolName:      "Bash",
		RawInput:      map[string]any{"command": "git status"},
		CanonicalForm: map[string]any{"tool": "git", "action": "status"},
		ActionType:    event.ActionGit,
		Result:        event.ResultSuccess,
		DurationMs:    12,
		TS:            time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		Metadata:      map[string]any{},
	}

	if err := AppendEvent(dir, ev); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	path := filepath.Join(dir, ".chitin", "events-"+ev.RunID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lines := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "{") {
			t.Fatalf("line not JSON: %q", line)
		}
		var decoded event.Event
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded.ToolName != "Bash" {
			t.Errorf("roundtrip lost tool_name: got %q", decoded.ToolName)
		}
		lines++
	}
	if lines != 1 {
		t.Fatalf("expected 1 line, got %d", lines)
	}
}

func TestAppendEvent_appendsMultiple(t *testing.T) {
	dir := t.TempDir()
	runID := "550e8400-e29b-41d4-a716-446655440000"

	for i := 0; i < 3; i++ {
		ev := event.Event{
			RunID:         runID,
			SessionID:     "sid",
			Surface:       "claude-code",
			Driver:        "claude",
			ToolName:      "Bash",
			ActionType:    event.ActionExec,
			Result:        event.ResultSuccess,
			TS:            time.Now().UTC(),
			RawInput:      map[string]any{},
			CanonicalForm: map[string]any{},
			Metadata:      map[string]any{},
		}
		if err := AppendEvent(dir, ev); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	path := filepath.Join(dir, ".chitin", "events-"+runID+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
}
