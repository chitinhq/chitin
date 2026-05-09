package replay

import (
	"errors"
	"testing"
)

func TestReverseToolName(t *testing.T) {
	tests := []struct {
		actionType string
		fallback   string
		want       string
	}{
		{"shell.exec", "Unknown", "Bash"},
		{"file.write", "Unknown", "Edit"},
		{"file.read", "Unknown", "Read"},
		{"http.request", "Unknown", "WebFetch"},
		{"delegate.task", "Unknown", "Task"},
		{"git.worktree.add", "Unknown", "EnterWorktree"},
		{"git.worktree.remove", "Unknown", "ExitWorktree"},
		{"unknown.type", "Fallback", "Fallback"},
	}
	for _, tc := range tests {
		got := reverseToolName(tc.actionType, tc.fallback)
		if got != tc.want {
			t.Errorf("reverseToolName(%q, %q) = %q, want %q", tc.actionType, tc.fallback, got, tc.want)
		}
	}
}

func TestIsPolicyAbsentError(t *testing.T) {
	if isPolicyAbsentError(nil) {
		t.Error("nil error should not be policy absent")
	}
	if !isPolicyAbsentError(errors.New("no_policy_found in /foo")) {
		t.Error("error containing 'no_policy_found' should be true")
	}
	if isPolicyAbsentError(errors.New("other error")) {
		t.Error("other error should not be policy absent")
	}
}

func TestBoolToStr(t *testing.T) {
	if boolToStr(true) != "allow" {
		t.Errorf("boolToStr(true) = %q, want 'allow'", boolToStr(true))
	}
	if boolToStr(false) != "deny" {
		t.Errorf("boolToStr(false) = %q, want 'deny'", boolToStr(false))
	}
}

func TestIsLikelyFilePath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"src/main.go", true},
		{"config.yaml", true},
		{"README.md", true},
		{"script.sh", true},
		{"hello", false},
		{"simple-text", false},
		{"path/to/file.ts", true},
		{"path/to/file.tsx", true},
		{"path/to/file.js", true},
		{"path/to/file.py", true},
		{"path/to/file.json", true},
		{"path/to/file.yml", true},
	}
	for _, tc := range tests {
		got := isLikelyFilePath(tc.input)
		if got != tc.want {
			t.Errorf("isLikelyFilePath(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestShortPath(t *testing.T) {
	if got := shortPath("short"); got != "short" {
		t.Errorf("shortPath of short string should be identity, got %q", got)
	}
	long := "this-is-a-very-long-path-that-exceeds-fifty-characters-easily-and-should-be-truncated"
	if got := shortPath(long); len(got) > 50 {
		t.Errorf("shortPath result should be <= 50 chars, got %d", len(got))
	}
	if len(long) <= 50 {
		t.Error("test assumes long string > 50 chars")
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("abc"); got != "abc" {
		t.Errorf("shortID of short string should be identity, got %q", got)
	}
	longID := "0123456789abcdef0123456789abcdef"
	if got := shortID(longID); len(got) > 12 {
		t.Errorf("shortID result should be <= 12 chars, got %d", len(got))
	}
}

func TestExtractAxis(t *testing.T) {
	ev := map[string]interface{}{"agent_instance_id": "glm-agent"}
	payload := map[string]interface{}{
		"tool_name":   "Bash",
		"action_type": "shell.exec",
		"rule_id":     "default-allow",
		"decision":    "allow",
	}
	if got := extractAxis(ev, payload, "tool_name"); got != "Bash" {
		t.Errorf("tool_name: got %q", got)
	}
	if got := extractAxis(ev, payload, "action_type"); got != "shell.exec" {
		t.Errorf("action_type: got %q", got)
	}
	if got := extractAxis(ev, payload, "rule_id"); got != "default-allow" {
		t.Errorf("rule_id: got %q", got)
	}
	if got := extractAxis(ev, payload, "decision"); got != "allow" {
		t.Errorf("decision: got %q", got)
	}
	if got := extractAxis(ev, payload, "agent"); got != "glm-agent" {
		t.Errorf("agent: got %q", got)
	}
	if got := extractAxis(ev, payload, "unknown_axis"); got != "" {
		t.Errorf("unknown axis: got %q, want empty", got)
	}
}