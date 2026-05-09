package copilot

import (
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestSummarizeArgs_Nil(t *testing.T) {
	if got := summarizeArgs(nil); got != "" {
		t.Errorf("summarizeArgs(nil) = %q, want empty", got)
	}
}

func TestSummarizeArgs_BashCommand(t *testing.T) {
	args := map[string]any{"command": "ls -la /tmp"}
	if got := summarizeArgs(args); got != "ls -la /tmp" {
		t.Errorf("summarizeArgs(command) = %q, want %q", got, "ls -la /tmp")
	}
}

func TestSummarizeArgs_FilePath(t *testing.T) {
	args := map[string]any{"path": "/home/user/file.go"}
	if got := summarizeArgs(args); got != "/home/user/file.go" {
		t.Errorf("summarizeArgs(path) = %q, want %q", got, "/home/user/file.go")
	}
}

func TestSummarizeArgs_FilePathAlt(t *testing.T) {
	args := map[string]any{"filePath": "/src/main.go"}
	if got := summarizeArgs(args); got != "/src/main.go" {
		t.Errorf("summarizeArgs(filePath) = %q, want %q", got, "/src/main.go")
	}
}

func TestSummarizeArgs_CommandPreferredOverPath(t *testing.T) {
	args := map[string]any{"command": "rm -rf /", "path": "/safe/path"}
	if got := summarizeArgs(args); got != "rm -rf /" {
		t.Errorf("command should take priority over path, got %q", got)
	}
}

func TestSummarizeArgs_EmptyCommand(t *testing.T) {
	args := map[string]any{"command": "", "path": "/fallback/path"}
	if got := summarizeArgs(args); got != "/fallback/path" {
		t.Errorf("empty command should fall back to path, got %q", got)
	}
}

func TestSummarizeArgs_OtherMap(t *testing.T) {
	args := map[string]any{"query": "find all bugs"}
	got := summarizeArgs(args)
	if got == "" {
		t.Error("summarizeArgs with non-command/path map should produce JSON")
	}
}

func TestSummarizeArgs_Truncation(t *testing.T) {
	longStr := ""
	for i := 0; i < 150; i++ {
		longStr += "x"
	}
	args := map[string]any{"data": longStr}
	got := summarizeArgs(args)
	if len(got) > 123 { // 120 + "…"
		t.Errorf("summarizeArgs should truncate at 120 chars, got len=%d: %q", len(got), got)
	}
}

func TestSummarizeArgs_NonMap(t *testing.T) {
	got := summarizeArgs("just a string")
	if got == "" {
		t.Error("summarizeArgs(non-map) should produce JSON string")
	}
}

func TestDefaultChitinDir_Home(t *testing.T) {
	t.Setenv("HOME", "/tmp/testhome")
	dir, err := defaultChitinDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/tmp/testhome/.chitin" {
		t.Errorf("defaultChitinDir = %q, want /tmp/testhome/.chitin", dir)
	}
}

func TestFormatGuideError_ReasonOnly(t *testing.T) {
	d := gov.Decision{Reason: "too many files"}
	got := formatGuideError(d)
	want := "chitin: too many files"
	if got != want {
		t.Errorf("formatGuideError = %q, want %q", got, want)
	}
}

func TestFormatGuideError_WithSuggestion(t *testing.T) {
	d := gov.Decision{Reason: "dangerous command", Suggestion: "use git stash instead"}
	got := formatGuideError(d)
	want := "chitin: dangerous command | suggest: use git stash instead"
	if got != want {
		t.Errorf("formatGuideError = %q, want %q", got, want)
	}
}

func TestFormatGuideError_WithCorrectedCommand(t *testing.T) {
	d := gov.Decision{Reason: "wrong syntax", CorrectedCommand: "rm -rf /tmp/build"}
	got := formatGuideError(d)
	want := "chitin: wrong syntax | try: rm -rf /tmp/build"
	if got != want {
		t.Errorf("formatGuideError = %q, want %q", got, want)
	}
}

func TestLockdownError_Error(t *testing.T) {
	lde := &LockdownError{Agent: "copilot-cli", Count: 5}
	s := lde.Error()
	if !strings.Contains(s, "copilot-cli") || !strings.Contains(s, "5") {
		t.Errorf("LockdownError.Error() = %q, want agent+count", s)
	}
}

func TestFormatGuideError_Full(t *testing.T) {
	d := gov.Decision{
		Reason:           "unsafe",
		Suggestion:       "use safe mode",
		CorrectedCommand: "safe-command",
	}
	got := formatGuideError(d)
	want := "chitin: unsafe | suggest: use safe mode | try: safe-command"
	if got != want {
		t.Errorf("formatGuideError = %q, want %q", got, want)
	}
}