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
	a := Action{Type: ActGitPush, Target: "fix"}
	d := evaluateBoundsFromStats(a, p, p.Bounds.effectiveBounds(string(a.Type)), 20, 100, 100)
	if d.Allowed {
		t.Errorf("20 files > 10 should reject")
	}
	if d.RuleID != "bounds:max_files_changed" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
	if d.Mode != "enforce" {
		t.Errorf("bounds must default to enforce, got %q", d.Mode)
	}
}

func TestBounds_OverLines(t *testing.T) {
	p := Policy{Bounds: Bounds{MaxFilesChanged: 100, MaxLinesChanged: 50}}
	a := Action{Type: ActGitPush, Target: "fix"}
	d := evaluateBoundsFromStats(a, p, p.Bounds.effectiveBounds(string(a.Type)), 5, 40, 40)
	if d.Allowed {
		t.Errorf("80 total lines > 50 should reject")
	}
	if d.RuleID != "bounds:max_lines_changed" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
}

func TestBounds_WithinCeilings(t *testing.T) {
	p := Policy{Bounds: Bounds{MaxFilesChanged: 25, MaxLinesChanged: 500}}
	a := Action{Type: ActGitPush, Target: "fix"}
	d := evaluateBoundsFromStats(a, p, p.Bounds.effectiveBounds(string(a.Type)), 3, 10, 5)
	if !d.Allowed {
		t.Errorf("3 files / 15 lines should pass, got %+v", d)
	}
}

func TestBounds_NoCeiling(t *testing.T) {
	// Bounds with all zeros should be a no-op (no ceilings set).
	p := Policy{Bounds: Bounds{}}
	a := Action{Type: ActGitPush, Target: "fix"}
	d := evaluateBoundsFromStats(a, p, p.Bounds.effectiveBounds(string(a.Type)), 1000, 100000, 100000)
	if !d.Allowed {
		t.Errorf("zero bounds should be no-op, got %+v", d)
	}
}

// Closes #70: per-action bounds override. git.push allowed a higher
// ceiling for doc-batch pushes, while github.pr.create stays tight.
func TestBounds_PerActionOverride(t *testing.T) {
	p := Policy{Bounds: Bounds{
		MaxFilesChanged: 25,
		MaxLinesChanged: 500,
		PerAction: map[string]ActionBounds{
			string(ActGitPush): {MaxFilesChanged: 200, MaxLinesChanged: 5000},
		},
	}}

	// 50-file push under git.push: passes (200 ceiling).
	gp := Action{Type: ActGitPush, Target: "feat"}
	d := evaluateBoundsFromStats(gp, p, p.Bounds.effectiveBounds(string(gp.Type)), 50, 1000, 1000)
	if !d.Allowed {
		t.Errorf("50 files via git.push under 200-file override should pass, got %+v", d)
	}

	// 50-file PR create: blocked (default 25 ceiling, no override).
	pc := Action{Type: ActGithubPRCreate, Target: "feat"}
	d2 := evaluateBoundsFromStats(pc, p, p.Bounds.effectiveBounds(string(pc.Type)), 50, 1000, 1000)
	if d2.Allowed {
		t.Errorf("50 files via github.pr.create under default 25-file ceiling should fail, got %+v", d2)
	}
}

// Closes #70 (option 3): invariantModes override flips a bounds rule
// from enforce to monitor, so the gate's monitor-mode override flips
// Allowed=true for that decision (callers see allow but the audit log
// records the bounds breach).
func TestBounds_InvariantModeOverridesToMonitor(t *testing.T) {
	p := Policy{
		Bounds: Bounds{MaxFilesChanged: 10, MaxLinesChanged: 100},
		InvariantModes: map[string]string{
			"bounds:max_files_changed": "monitor",
		},
	}
	a := Action{Type: ActGitPush, Target: "fix"}
	d := evaluateBoundsFromStats(a, p, p.Bounds.effectiveBounds(string(a.Type)), 50, 10, 10)

	if d.Mode != "monitor" {
		t.Errorf("invariantModes override should set Mode=monitor, got %q", d.Mode)
	}
	// Note: evaluateBoundsFromStats returns Allowed=false; the gate's
	// monitor-mode override (in gate.go) flips it to true. This test
	// just asserts the mode propagation; gate-level integration is
	// covered in TestGate_MonitorModeOverridesBoundsDeny.
}

// Integration: monitor-mode override flips a bounds deny to allow at
// the gate layer. Uses a custom policy because newTestGate's default
// policy doesn't allow git.push (so bounds wouldn't be reached without
// it). Closes #70 (option 3 — operator-opt-in soft-kill on bounds).
func TestGate_MonitorModeOverridesBoundsDeny(t *testing.T) {
	dir := t.TempDir()
	pol := Policy{
		Mode: "enforce",
		Rules: []Rule{
			{ID: "allow-push", Action: ActionMatcher{string(ActGitPush)}, Effect: "allow"},
		},
		Bounds: Bounds{MaxFilesChanged: 10, MaxLinesChanged: 100},
		InvariantModes: map[string]string{
			// In a non-git tempdir, bounds:undetermined fires (git diff fails).
			"bounds:undetermined": "monitor",
		},
	}
	if err := pol.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	counter, err := OpenCounter(dir + "/gov.db")
	if err != nil {
		t.Fatalf("OpenCounter: %v", err)
	}
	defer counter.Close()
	g := &Gate{Policy: pol, Counter: counter, LogDir: dir, Cwd: dir}

	a := Action{Type: ActGitPush, Target: "fix"}
	d := g.Evaluate(a, "agent1", nil)

	// Without the invariantModes override, CheckBounds would return
	// bounds:undetermined with Mode=enforce, the gate would NOT flip
	// it via monitor-override, and the test would see Allowed=false.
	// With the override, Mode=monitor, gate's monitor-override flips
	// it to Allowed=true.
	if !d.Allowed {
		t.Errorf("monitor-mode override should flip bounds deny to allow, got Mode=%q RuleID=%q",
			d.Mode, d.RuleID)
	}
}
