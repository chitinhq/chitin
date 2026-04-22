package gov

import "testing"

func TestBounds_NonPushShapedSkips(t *testing.T) {
	// Bounds only fires for git.push / github.pr.create.
	// Other actions return no-op (allow, empty rule).
	p := Policy{Bounds: Bounds{MaxFilesChanged: 1, MaxLinesChanged: 1}}
	d := CheckBounds(Action{Type: ActFileRead, Target: "/tmp/x"}, p, ".")
	if !d.Allowed {
		t.Errorf("non-push-shaped action should pass bounds, got %+v", d)
	}
}

func TestBounds_ParseStatLine(t *testing.T) {
	cases := []struct {
		line     string
		wantF    int
		wantIns  int
		wantDel  int
	}{
		{" 3 files changed, 10 insertions(+), 5 deletions(-)", 3, 10, 5},
		{" 1 file changed, 1 insertion(+)", 1, 1, 0},
		{" 60 files changed, 42 insertions(+), 8874 deletions(-)", 60, 42, 8874},
		{" 2 files changed, 0 insertions(+), 7 deletions(-)", 2, 0, 7},
	}
	for _, c := range cases {
		f, ins, del := parseDiffStatLine(c.line)
		if f != c.wantF || ins != c.wantIns || del != c.wantDel {
			t.Errorf("parseDiffStatLine(%q) = (%d,%d,%d) want (%d,%d,%d)",
				c.line, f, ins, del, c.wantF, c.wantIns, c.wantDel)
		}
	}
}

func TestBounds_ParseStatLine_Empty(t *testing.T) {
	f, ins, del := parseDiffStatLine("")
	if f != 0 || ins != 0 || del != 0 {
		t.Errorf("empty should parse to zeros, got (%d,%d,%d)", f, ins, del)
	}
}

func TestBounds_OverFiles(t *testing.T) {
	p := Policy{Bounds: Bounds{MaxFilesChanged: 10, MaxLinesChanged: 1000}}
	d := evaluateBoundsFromStats(Action{Type: ActGitPush, Target: "fix"}, p, 20, 100, 100)
	if d.Allowed {
		t.Errorf("20 files > 10 should reject")
	}
	if d.RuleID != "bounds:max_files_changed" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
	if d.Mode != "enforce" {
		t.Errorf("bounds must always be enforce, got %q", d.Mode)
	}
}

func TestBounds_OverLines(t *testing.T) {
	p := Policy{Bounds: Bounds{MaxFilesChanged: 100, MaxLinesChanged: 50}}
	d := evaluateBoundsFromStats(Action{Type: ActGitPush, Target: "fix"}, p, 5, 40, 40)
	if d.Allowed {
		t.Errorf("80 total lines > 50 should reject")
	}
	if d.RuleID != "bounds:max_lines_changed" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
}

func TestBounds_WithinCeilings(t *testing.T) {
	p := Policy{Bounds: Bounds{MaxFilesChanged: 25, MaxLinesChanged: 500}}
	d := evaluateBoundsFromStats(Action{Type: ActGitPush, Target: "fix"}, p, 3, 10, 5)
	if !d.Allowed {
		t.Errorf("3 files / 15 lines should pass, got %+v", d)
	}
}

func TestBounds_NoCeiling(t *testing.T) {
	// Bounds with all zeros should be a no-op (no ceilings set).
	p := Policy{Bounds: Bounds{}}
	d := evaluateBoundsFromStats(Action{Type: ActGitPush, Target: "fix"}, p, 1000, 100000, 100000)
	if !d.Allowed {
		t.Errorf("zero bounds should be no-op, got %+v", d)
	}
}
