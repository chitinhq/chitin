package copilot

import (
	"testing"
)

func TestLockdownError_Error(t *testing.T) {
	e := &LockdownError{Agent: "test-agent", Count: 5}
	msg := e.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if !contains(msg, "test-agent") {
		t.Errorf("expected agent name in message, got %q", msg)
	}
	if !contains(msg, "5") {
		t.Errorf("expected count in message, got %q", msg)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPrintLockdownSummary(t *testing.T) {
	// Just verify it doesn't panic and writes to stderr
	e := &LockdownError{Agent: "glm-agent", Count: 12}
	printLockdownSummary(e)
}

func TestSummarizeArgs(t *testing.T) {
	// nil
	if got := summarizeArgs(nil); got != "" {
		t.Errorf("summarizeArgs(nil) = %q, want empty", got)
	}
	// map with command
	args := map[string]any{"command": "ls -la"}
	if got := summarizeArgs(args); got != "ls -la" {
		t.Errorf("summarizeArgs(command) = %q, want %q", got, "ls -la")
	}
	// map with path
	args = map[string]any{"path": "/etc/hosts"}
	if got := summarizeArgs(args); got != "/etc/hosts" {
		t.Errorf("summarizeArgs(path) = %q, want %q", got, "/etc/hosts")
	}
	// map with filePath
	args = map[string]any{"filePath": "/tmp/test.go"}
	if got := summarizeArgs(args); got != "/tmp/test.go" {
		t.Errorf("summarizeArgs(filePath) = %q, want %q", got, "/tmp/test.go")
	}
	// map with empty command falls back to JSON
	args = map[string]any{"command": "", "other": "val"}
	if got := summarizeArgs(args); got == "" {
		t.Error("summarizeArgs with empty command should fall back to JSON")
	}
	// non-map falls through to JSON
	if got := summarizeArgs("hello"); got != `"hello"` {
		t.Errorf("summarizeArgs(string) = %q, want %q", got, `"hello"`)
	}
	// long JSON gets truncated
	bigArgs := map[string]any{"data": string(make([]byte, 200))}
	got := summarizeArgs(bigArgs)
	if len(got) > 124 { // 120 + "…"
		t.Errorf("summarizeArgs long JSON = %d chars, should be truncated", len(got))
	}
}