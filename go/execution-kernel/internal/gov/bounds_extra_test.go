package gov

import (
	"testing"
)

func TestParseDiffStatLine(t *testing.T) {
	tests := []struct {
		input string
		files int
		ins   int
		del   int
	}{
		{"3 files changed, 10 insertions(+), 5 deletions(-)", 3, 10, 5},
		{"1 file changed, 7 insertions(+)", 1, 7, 0},
		{"2 files changed, 3 deletions(-)", 2, 0, 3},
		{"1 file changed, 1 insertion(+), 1 deletion(-)", 1, 1, 1},
		{"garbage line", 0, 0, 0},
		{"", 0, 0, 0},
	}
	for _, tc := range tests {
		f, i, d := parseDiffStatLine(tc.input)
		if f != tc.files || i != tc.ins || d != tc.del {
			t.Errorf("parseDiffStatLine(%q) = (%d, %d, %d), want (%d, %d, %d)",
				tc.input, f, i, d, tc.files, tc.ins, tc.del)
		}
	}
}

func TestEvaluateBoundsFromStats_WithinLimits(t *testing.T) {
	action := Action{Type: ActGitPush}
	policy := Policy{Bounds: Bounds{MaxFilesChanged: 10, MaxLinesChanged: 500}}
	eff := ActionBounds{MaxFilesChanged: 10, MaxLinesChanged: 500}
	d := evaluateBoundsFromStats(action, policy, eff, 3, 10, 5)
	if !d.Allowed {
		t.Errorf("should be allowed within limits: %s", d.Reason)
	}
	if d.RuleID != "bounds:within-ceilings" {
		t.Errorf("ruleID = %q, want bounds:within-ceilings", d.RuleID)
	}
}

func TestEvaluateBoundsFromStats_ExceedsFiles(t *testing.T) {
	action := Action{Type: ActGitPush}
	policy := Policy{Bounds: Bounds{MaxFilesChanged: 5, MaxLinesChanged: 500}}
	eff := ActionBounds{MaxFilesChanged: 5, MaxLinesChanged: 500}
	d := evaluateBoundsFromStats(action, policy, eff, 10, 5, 5)
	if d.Allowed {
		t.Error("should deny when exceeding MaxFilesChanged")
	}
	if d.RuleID != "bounds:max_files_changed" {
		t.Errorf("ruleID = %q, want bounds:max_files_changed", d.RuleID)
	}
}

func TestEvaluateBoundsFromStats_ExceedsLines(t *testing.T) {
	action := Action{Type: ActGitPush}
	policy := Policy{Bounds: Bounds{MaxFilesChanged: 10, MaxLinesChanged: 50}}
	eff := ActionBounds{MaxFilesChanged: 10, MaxLinesChanged: 50}
	d := evaluateBoundsFromStats(action, policy, eff, 2, 40, 20)
	if d.Allowed {
		t.Error("should deny when exceeding MaxLinesChanged")
	}
	if d.RuleID != "bounds:max_lines_changed" {
		t.Errorf("ruleID = %q, want bounds:max_lines_changed", d.RuleID)
	}
}

func TestEvaluateBoundsFromStats_NoCeiling(t *testing.T) {
	action := Action{Type: ActGitPush}
	policy := Policy{}
	eff := ActionBounds{MaxFilesChanged: 0, MaxLinesChanged: 0}
	d := evaluateBoundsFromStats(action, policy, eff, 100, 5000, 5000)
	if !d.Allowed {
		t.Error("no ceiling should always allow")
	}
}

func TestCollectDiffStats_EmptyOutput(t *testing.T) {
	// Test the parse path only — collectDiffStats itself needs a git repo
	// so we test parseDiffStatLine with empty string which maps to empty diff
	f, i, d := parseDiffStatLine("")
	if f != 0 || i != 0 || d != 0 {
		t.Errorf("empty diff stat = (%d, %d, %d), want zeros", f, i, d)
	}
}