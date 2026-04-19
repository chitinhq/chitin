package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestReadClaudeInput_stdinJSON(t *testing.T) {
	payload := map[string]any{
		"session_id":      "550e8400-e29b-41d4-a716-446655440000",
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "ls"},
		"cwd":             "/tmp",
		"model":           "claude-3.5-sonnet",
	}
	data, _ := json.Marshal(payload)

	in, err := parseClaudeInput(bytes.NewReader(data), func(string) string { return "" })
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if in.Event != "PreToolUse" {
		t.Errorf("Event: got %q", in.Event)
	}
	if in.Tool != "Bash" {
		t.Errorf("Tool: got %q", in.Tool)
	}
	if in.SessionID != payload["session_id"] {
		t.Errorf("SessionID: got %q", in.SessionID)
	}
	cmd, _ := in.Input["command"].(string)
	if cmd != "ls" {
		t.Errorf("Input.command: got %q", cmd)
	}
}

func TestReadClaudeInput_envFallback(t *testing.T) {
	env := map[string]string{
		"CLAUDE_HOOK_EVENT_NAME": "PreToolUse",
		"CLAUDE_TOOL_NAME":       "Write",
		"CLAUDE_SESSION_ID":      "abc-123",
		"CLAUDE_TOOL_INPUT":      `{"file_path":"/tmp/foo.txt","content":"hi"}`,
	}
	getenv := func(k string) string { return env[k] }

	in, err := parseClaudeInput(strings.NewReader(""), getenv)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if in.Event != "PreToolUse" {
		t.Errorf("Event: got %q", in.Event)
	}
	if in.Tool != "Write" {
		t.Errorf("Tool: got %q", in.Tool)
	}
	path, _ := in.Input["file_path"].(string)
	if path != "/tmp/foo.txt" {
		t.Errorf("file_path: got %q", path)
	}
}

func TestReadClaudeInput_noInput(t *testing.T) {
	_, err := parseClaudeInput(strings.NewReader(""), func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error for empty stdin + no env")
	}
}

var _ = os.Stdin
