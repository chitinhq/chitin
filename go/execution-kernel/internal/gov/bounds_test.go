package gov

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBounds_NonPushShapedSkips(t *testing.T) {
	// Bounds only fires for git.push / github.pr.create.
	// Other actions return no-op (allow, empty rule).
	p := Policy{Bounds: Bounds{MaxFilesChanged: 1, MaxLinesChanged: 1}}
	d := CheckBounds(Action{Type: ActFileRead, Target: "/tmp/x"}, p, ".")
	if !d.Allowed {
		t.Errorf("non-push-shaped action should pass bounds, got %+v", d)
	}
}

func TestBounds_ParseNumstat(t *testing.T) {
	out := "10\t5\tsrc/a.go\n" +
		"3045\t42\tpnpm-lock.yaml\n" +
		"-\t-\tassets/logo.png\n" +
		"1\t0\tapps/x/pnpm-lock.yaml\n"

	// No exclusions: every file counts; binary contributes 0 lines.
	f, ins, del, err := parseNumstat(out, nil)
	if err != nil {
		t.Fatalf("parseNumstat: %v", err)
	}
	if f != 4 || ins != 3056 || del != 47 {
		t.Errorf("no-exclude: got (%d,%d,%d) want (4,3056,47)", f, ins, del)
	}

	// Excluding lockfiles drops both pnpm-lock.yaml files.
	f, ins, del, err = parseNumstat(out, []string{"**/pnpm-lock.yaml"})
	if err != nil {
		t.Fatalf("parseNumstat exclude: %v", err)
	}
	if f != 2 || ins != 10 || del != 5 {
		t.Errorf("exclude lockfiles: got (%d,%d,%d) want (2,10,5)", f, ins, del)
	}
}

func TestBounds_ParseNumstat_Empty(t *testing.T) {
	f, ins, del, err := parseNumstat("", nil)
	if err != nil || f != 0 || ins != 0 || del != 0 {
		t.Errorf("empty should parse to zeros with no error, got (%d,%d,%d) err=%v", f, ins, del, err)
	}
	f, ins, del, err = parseNumstat("   \n  ", nil)
	if err != nil || f != 0 || ins != 0 || del != 0 {
		t.Errorf("whitespace-only should parse to zeros, got (%d,%d,%d) err=%v", f, ins, del, err)
	}
}

func TestBounds_ParseNumstat_AllExcludedPasses(t *testing.T) {
	// Boundary: every changed file is excluded — measures as 0/0/0.
	out := "9000\t9000\tpnpm-lock.yaml\n5000\t0\tvendor/big.js\n"
	f, ins, del, err := parseNumstat(out, []string{"**/pnpm-lock.yaml", "vendor/**"})
	if err != nil {
		t.Fatalf("parseNumstat: %v", err)
	}
	if f != 0 || ins != 0 || del != 0 {
		t.Errorf("fully-excluded diff should be zeros, got (%d,%d,%d)", f, ins, del)
	}
}

func TestBounds_ParseNumstat_Unparseable(t *testing.T) {
	// Fail-closed: a line without three tab fields is an error, not a skip.
	if _, _, _, err := parseNumstat("garbage line no tabs\n", nil); err == nil {
		t.Errorf("unparseable line should error (fail-closed)")
	}
	// Fail-closed: a non-numeric count is an error.
	if _, _, _, err := parseNumstat("abc\t5\tsrc/a.go\n", nil); err == nil {
		t.Errorf("non-numeric added count should error")
	}
}

func TestBounds_MatchGlob(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"**/pnpm-lock.yaml", "pnpm-lock.yaml", true},
		{"**/pnpm-lock.yaml", "apps/x/pnpm-lock.yaml", true},
		{"**/pnpm-lock.yaml", "apps/x/pnpm-lock.yaml.bak", false},
		{"vendor/**", "vendor/a/b.js", true},
		{"vendor/**", "vendor", false},
		{"vendor/**", "src/vendor/a.js", false},
		{"pnpm-lock.yaml", "pnpm-lock.yaml", true},
		{"pnpm-lock.yaml", "apps/x/pnpm-lock.yaml", false},
		{"*.lock", "Cargo.lock", true},
		{"*.lock", "a/Cargo.lock", false},
		{"**/*.snap", "test/__snapshots__/a.snap", true},
		{"", "anything", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.path); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v want %v", c.pattern, c.path, got, c.want)
		}
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

func TestCollectDiffStats_GithubPRCreateUsesHeadBranchNotCwdHead(t *testing.T) {
	repo := initBoundsTestRepo(t)
	runGitBounds(t, repo, "checkout", "-b", "feature/small")
	writeAndCommitBoundsFile(t, repo, "small.txt", strings.Repeat("a\n", 20), "small branch change")
	runGitBounds(t, repo, "checkout", "main")

	writeAndCommitBoundsFile(t, repo, "large.txt", strings.Repeat("b\n", 2200), "large cwd change")

	files, ins, del, err := collectDiffStats(Action{
		Type:   ActGithubPRCreate,
		Target: "gh pr create --base main --head feature/small",
	}, repo, nil)
	if err != nil {
		t.Fatalf("collectDiffStats: %v", err)
	}
	if files != 1 || ins != 20 || del != 0 {
		t.Fatalf("github.pr.create should diff main...feature/small, got files=%d ins=%d del=%d", files, ins, del)
	}
}

func TestCollectDiffStats_GithubPRCreateUsesRemoteBaseWhenLocalBaseIsStale(t *testing.T) {
	repo := initBoundsTestRepo(t)
	oldMain := gitOutputBounds(t, repo, "rev-parse", "main")

	writeAndCommitBoundsFile(t, repo, "already-merged.txt", strings.Repeat("b\n", 2200), "remote base change")
	runGitBounds(t, repo, "push", "origin", "main")
	runGitBounds(t, repo, "update-ref", "refs/heads/main", oldMain)

	runGitBounds(t, repo, "checkout", "-b", "feature/small", "origin/main")
	writeAndCommitBoundsFile(t, repo, "small.txt", strings.Repeat("a\n", 20), "small branch change")

	files, ins, del, err := collectDiffStats(Action{
		Type:   ActGithubPRCreate,
		Target: "gh pr create --base main --head feature/small",
	}, repo, nil)
	if err != nil {
		t.Fatalf("collectDiffStats: %v", err)
	}
	if files != 1 || ins != 20 || del != 0 {
		t.Fatalf("github.pr.create should diff origin/main...feature/small, got files=%d ins=%d del=%d", files, ins, del)
	}
}

// Integration: ExcludePaths drops a generated lockfile from the totals
// measured by collectDiffStats against a real git diff.
func TestCollectDiffStats_ExcludePathsDropsLockfile(t *testing.T) {
	repo := initBoundsTestRepo(t)
	writeAndCommitBoundsFile(t, repo, "pnpm-lock.yaml", strings.Repeat("dep\n", 3000), "regen lockfile")
	writeAndCommitBoundsFile(t, repo, "app_change.go", strings.Repeat("x\n", 40), "real code change")

	// Without exclusion: lockfile churn dominates the total.
	files, ins, del, err := collectDiffStats(Action{
		Type:   ActGitPush,
		Target: "git push origin main",
	}, repo, nil)
	if err != nil {
		t.Fatalf("collectDiffStats no-exclude: %v", err)
	}
	if files != 2 || ins != 3040 || del != 0 {
		t.Fatalf("no-exclude: got files=%d ins=%d del=%d want (2,3040,0)", files, ins, del)
	}

	// With exclusion: only the real code change counts.
	files, ins, del, err = collectDiffStats(Action{
		Type:   ActGitPush,
		Target: "git push origin main",
	}, repo, []string{"**/pnpm-lock.yaml"})
	if err != nil {
		t.Fatalf("collectDiffStats exclude: %v", err)
	}
	if files != 1 || ins != 40 || del != 0 {
		t.Fatalf("exclude lockfile: got files=%d ins=%d del=%d want (1,40,0)", files, ins, del)
	}
}

func TestCollectDiffStats_GitPushStillUsesCwdHead(t *testing.T) {
	repo := initBoundsTestRepo(t)
	writeAndCommitBoundsFile(t, repo, "large.txt", strings.Repeat("b\n", 2200), "large cwd change")

	files, ins, del, err := collectDiffStats(Action{
		Type:   ActGitPush,
		Target: "git push origin main",
	}, repo, nil)
	if err != nil {
		t.Fatalf("collectDiffStats: %v", err)
	}
	if files != 1 || ins != 2200 || del != 0 {
		t.Fatalf("git.push should diff cwd HEAD, got files=%d ins=%d del=%d", files, ins, del)
	}
}

func TestCollectDiffStats_GithubPRCreateMissingHeadFallsBackToCwdHeadAndWarns(t *testing.T) {
	repo := initBoundsTestRepo(t)
	writeAndCommitBoundsFile(t, repo, "cwd.txt", strings.Repeat("c\n", 15), "cwd change")

	stderr := captureStderr(t, func() {
		files, ins, del, err := collectDiffStats(Action{
			Type:   ActGithubPRCreate,
			Target: "gh pr create --base main",
		}, repo, nil)
		if err != nil {
			t.Fatalf("collectDiffStats: %v", err)
		}
		if files != 1 || ins != 15 || del != 0 {
			t.Fatalf("missing --head should fall back to cwd HEAD, got files=%d ins=%d del=%d", files, ins, del)
		}
	})

	if !strings.Contains(stderr, "bounds: using cwd HEAD; pass --head/--base for accurate PR-size measurement") {
		t.Fatalf("missing --head warning not emitted, stderr=%q", stderr)
	}
}

func TestCollectDiffStats_GithubPRCreateOmittedBaseUsesOriginHead(t *testing.T) {
	repo := initBoundsTestRepo(t)
	runGitBounds(t, repo, "checkout", "-b", "feature/small")
	writeAndCommitBoundsFile(t, repo, "small.txt", strings.Repeat("a\n", 12), "small branch change")
	runGitBounds(t, repo, "checkout", "main")

	files, ins, del, err := collectDiffStats(Action{
		Type:   ActGithubPRCreate,
		Target: "gh pr create --head feature/small",
	}, repo, nil)
	if err != nil {
		t.Fatalf("collectDiffStats: %v", err)
	}
	if files != 1 || ins != 12 || del != 0 {
		t.Fatalf("omitted --base should default to origin/HEAD target, got files=%d ins=%d del=%d", files, ins, del)
	}
}

func initBoundsTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	repo := filepath.Join(root, "repo")

	runGitBounds(t, root, "init", "--bare", remote)
	runGitBounds(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	runGitBounds(t, root, "clone", remote, repo)
	runGitBounds(t, repo, "config", "user.email", "test@example.com")
	runGitBounds(t, repo, "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGitBounds(t, repo, "add", "README.md")
	runGitBounds(t, repo, "commit", "-m", "init")
	runGitBounds(t, repo, "push", "-u", "origin", "main")
	runGitBounds(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return repo
}

func writeAndCommitBoundsFile(t *testing.T, repo, name, contents, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	runGitBounds(t, repo, "add", name)
	runGitBounds(t, repo, "commit", "-m", message)
}

func runGitBounds(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func gitOutputBounds(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()

	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll stderr: %v", err)
	}
	return string(out)
}
