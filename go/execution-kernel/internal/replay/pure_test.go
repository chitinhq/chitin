package replay

import (
	"errors"
	"strings"
	"testing"
)

func TestReverseToolName(t *testing.T) {
	tests := []struct {
		actionType string
		fallback   string
		want       string
	}{
		{"shell.exec", "", "Bash"},
		{"file.write", "", "Edit"},
		{"file.read", "", "Read"},
		{"http.request", "", "WebFetch"},
		{"delegate.task", "", "Task"},
		{"git.worktree.add", "", "EnterWorktree"},
		{"git.worktree.remove", "", "ExitWorktree"},
		{"unknown_action", "Fallback", "Fallback"},
		{"unknown_action", "", ""},
	}
	for _, tt := range tests {
		got := reverseToolName(tt.actionType, tt.fallback)
		if got != tt.want {
			t.Errorf("reverseToolName(%q, %q) = %q, want %q", tt.actionType, tt.fallback, got, tt.want)
		}
	}
}

func TestIsPolicyAbsentError(t *testing.T) {
	if isPolicyAbsentError(nil) {
		t.Error("nil error should not be policy-absent")
	}
	if isPolicyAbsentError(errors.New("something else")) {
		t.Error("unrelated error should not be policy-absent")
	}
	if !isPolicyAbsentError(errors.New("no_policy_found: walked up to /")) {
		t.Error("no_policy_found error should be policy-absent")
	}
}

func TestIsLikelyFilePath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"src/main.go", true},
		{"README.md", true},
		{"config.yaml", true},
		{"run.sh", true},
		{"index.ts", true},
		{"index.tsx", true},
		{"app.js", true},
		{"data.json", true},
		{"just_a_word", false},
		{"command with spaces", false},
		{"just-a-command", false},
	}
	for _, tt := range tests {
		got := isLikelyFilePath(tt.input)
		if got != tt.want {
			t.Errorf("isLikelyFilePath(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestShortPath(t *testing.T) {
	if shortPath("short/path") != "short/path" {
		t.Errorf("short path should not be truncated")
	}
	long := strings.Repeat("a", 60)
	got := shortPath(long)
	if len(got) > 50 {
		t.Errorf("shortPath result too long: %d chars", len(got))
	}
	if !strings.HasPrefix(got, "...") {
		t.Errorf("truncated path should start with ..., got %q", got)
	}
}

func TestShortID(t *testing.T) {
	if shortID("abc") != "abc" {
		t.Error("short ID should not be truncated")
	}
	got := shortID("abcdefghijklmnop")
	if !strings.Contains(got, "…") {
		t.Errorf("long ID should contain ellipsis, got %q", got)
	}
	if got != "abcdefgh…" {
		t.Errorf("expected 'abcdefgh…', got %q", got)
	}
}

func TestBoolToStr(t *testing.T) {
	if boolToStr(true) != "allow" {
		t.Error("boolToStr(true) should be 'allow'")
	}
	if boolToStr(false) != "deny" {
		t.Error("boolToStr(false) should be 'deny'")
	}
}

func TestWriteJSONReport(t *testing.T) {
	r := &Result{
		SessionID:    "json-test",
		TotalEvents:  3,
		Decisions:    2,
		GovRuleCount: 1,
		PolicyPath:   "test.yaml",
		Summary:      Summary{UnchangedDecisions: 1, NowDenied: 1},
		Diffs: []DecisionDiff{
			{
				Ts:             "2026-05-03T10:00:00Z",
				ToolName:       "Bash",
				ActionTarget:   "rm -rf /tmp",
				OriginalRule:   "default-allow",
				ReplayedRule:   "deny-dangerous",
				OriginalAllow:  true,
				ReplayedAllow:  false,
				Layer:          "kernel",
				ReplayedReason: "recursive-delete",
			},
		},
	}
	var buf strings.Builder
	if err := WriteJSONReport(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`"session_id"`, `"json-test"`, `"total_events"`, `"gov_rule_count"`,
		`"now_denied"`, `"diffs"`, `"kernel"`, `"recursive-delete"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON output missing %q\nout:\n%s", want, out)
		}
	}
}

func TestAgentToTier(t *testing.T) {
	tests := []struct {
		agent string
		want  string
	}{
		{"local-qwen", "T0"},
		{"local-glm-flash", "T0"},
		{"local-glm", "T0"},
		{"local-deepseek", "T0"},
		{"copilot", "T1"},
		{"claude-code", "T3"},
		{"unknown-agent", ""},
	}
	for _, tt := range tests {
		got := AgentToTier(tt.agent)
		if got != tt.want {
			t.Errorf("AgentToTier(%q) = %q, want %q", tt.agent, got, tt.want)
		}
	}
}

func TestRecommendStartingTier_InsufficientSample(t *testing.T) {
	// RecommendStartingTier with no events should return T0 with insufficient_signal
	// (HOME points to a temp dir with no .chitin/events files)
	t.Setenv("HOME", t.TempDir())
	rec, err := RecommendStartingTier("shell.exec", 0.85, 10)
	if err != nil {
		t.Fatal(err)
	}
	if rec.RecommendedTier != "T0" {
		t.Errorf("expected T0 for insufficient sample, got %q", rec.RecommendedTier)
	}
	if !rec.InsufficientSignal {
		t.Error("expected InsufficientSignal=true")
	}
}