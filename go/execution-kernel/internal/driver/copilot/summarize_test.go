package copilot

import (
	"strings"
	"testing"
)

func TestSummarizeArgs_Nil(t *testing.T) {
	if got := summarizeArgs(nil); got != "" {
		t.Errorf("expected empty for nil, got %q", got)
	}
}

func TestSummarizeArgs_Command(t *testing.T) {
	args := map[string]any{"command": "ls -la /tmp"}
	if got := summarizeArgs(args); got != "ls -la /tmp" {
		t.Errorf("expected command, got %q", got)
	}
}

func TestSummarizeArgs_Path(t *testing.T) {
	args := map[string]any{"path": "/home/user/file.py"}
	if got := summarizeArgs(args); got != "/home/user/file.py" {
		t.Errorf("expected path, got %q", got)
	}
}

func TestSummarizeArgs_FilePath(t *testing.T) {
	args := map[string]any{"filePath": "/home/user/code.ts"}
	if got := summarizeArgs(args); got != "/home/user/code.ts" {
		t.Errorf("expected filePath, got %q", got)
	}
}

func TestSummarizeArgs_EmptyStringKeys(t *testing.T) {
	args := map[string]any{"command": "", "path": "", "filePath": ""}
	got := summarizeArgs(args)
	// All keys are empty strings, should fall through to JSON marshal
	if !strings.Contains(got, "command") {
		t.Errorf("expected JSON marshal fallback, got %q", got)
	}
}

func TestSummarizeArgs_NonMapArgs(t *testing.T) {
	got := summarizeArgs("simple string")
	if !strings.Contains(got, "simple string") {
		t.Errorf("expected JSON marshal for non-map, got %q", got)
	}
}

func TestSummarizeArgs_Truncation(t *testing.T) {
	// Create a map that marshals to > 120 chars
	longVal := strings.Repeat("x", 150)
	args := map[string]any{"data": longVal}
	got := summarizeArgs(args)
	if len(got) > 124 { // 120 + "…" (3 bytes in UTF-8 for …)
		t.Errorf("expected truncation, got %d chars: %q", len(got), got[:50])
	}
}