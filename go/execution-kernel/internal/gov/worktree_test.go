package gov

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGate_WorktreeRequirementDeniesPrimaryCheckout(t *testing.T) {
	repo, _ := newWorktreeFixture(t)
	g := newWorktreeGate(t, repo, "guide")

	d := g.Evaluate(Action{Type: ActFileWrite, Target: "README.md"}, "agent1", nil)
	if d.Allowed {
		t.Fatalf("file.write in primary checkout should be denied, got %+v", d)
	}
	if d.RuleID != "worktree-required" {
		t.Fatalf("RuleID=%q want worktree-required", d.RuleID)
	}
	if d.Mode != "guide" {
		t.Fatalf("Mode=%q want guide", d.Mode)
	}
	if !strings.Contains(d.Suggestion, "git worktree add") {
		t.Fatalf("suggestion should guide toward git worktree add, got %q", d.Suggestion)
	}
}

func TestGate_WorktreeRequirementAllowsLinkedWorktree(t *testing.T) {
	_, linked := newWorktreeFixture(t)
	g := newWorktreeGate(t, linked, "guide")

	d := g.Evaluate(Action{Type: ActFileWrite, Target: "README.md"}, "agent1", nil)
	if !d.Allowed {
		t.Fatalf("file.write in linked worktree should be allowed, got %+v", d)
	}
	if d.RuleID != "allow-write" {
		t.Fatalf("RuleID=%q want allow-write", d.RuleID)
	}
}

func TestGate_WorktreeRequirementFailsClosedOutsideGit(t *testing.T) {
	g := newWorktreeGate(t, t.TempDir(), "enforce")

	d := g.Evaluate(Action{Type: ActGitCommit, Target: "commit"}, "agent1", nil)
	if d.Allowed {
		t.Fatalf("git.commit outside git should be denied, got %+v", d)
	}
	if d.RuleID != "worktree-required" {
		t.Fatalf("RuleID=%q want worktree-required", d.RuleID)
	}
	if d.Mode != "enforce" {
		t.Fatalf("Mode=%q want enforce", d.Mode)
	}
}

func TestGate_WorktreeRequirementSkipsUnlistedReadActions(t *testing.T) {
	repo, _ := newWorktreeFixture(t)
	g := newWorktreeGate(t, repo, "guide")

	d := g.Evaluate(Action{Type: ActFileRead, Target: "README.md"}, "agent1", nil)
	if !d.Allowed {
		t.Fatalf("file.read should not require a linked worktree, got %+v", d)
	}
	if d.RuleID != "allow-read" {
		t.Fatalf("RuleID=%q want allow-read", d.RuleID)
	}
}

func TestPolicy_LoadsWorktreeRequirement(t *testing.T) {
	p, err := parsePolicyYAML([]byte(`
id: worktree-policy
mode: enforce
worktree:
  require_for: [file.write, git.commit]
  mode: guide
  protected_roots:
    - /home/red/workspace/chitin
rules:
  - id: allow-write
    action: file.write
    effect: allow
`))
	if err != nil {
		t.Fatalf("parsePolicyYAML: %v", err)
	}
	if !p.Worktree.RequireFor.Matches(ActFileWrite) || !p.Worktree.RequireFor.Matches(ActGitCommit) {
		t.Fatalf("worktree require_for did not parse: %+v", p.Worktree.RequireFor)
	}
	if p.Worktree.Mode != "guide" {
		t.Fatalf("worktree mode=%q want guide", p.Worktree.Mode)
	}
	if len(p.Worktree.ProtectedRoots) != 1 {
		t.Fatalf("protected_roots=%v want one root", p.Worktree.ProtectedRoots)
	}
}

func newWorktreeGate(t *testing.T, cwd, mode string) *Gate {
	t.Helper()
	p := Policy{
		Mode: "enforce",
		Worktree: WorktreeConfig{
			RequireFor: ActionMatcher{
				string(ActFileWrite),
				string(ActFileDelete),
				string(ActFileMove),
				string(ActShellExec),
				string(ActGitCommit),
				string(ActGitPush),
				string(ActGithubPRCreate),
			},
			Mode: mode,
		},
		Rules: []Rule{
			{ID: "allow-read", Action: ActionMatcher{string(ActFileRead)}, Effect: "allow"},
			{ID: "allow-write", Action: ActionMatcher{string(ActFileWrite)}, Effect: "allow"},
			{ID: "allow-commit", Action: ActionMatcher{string(ActGitCommit)}, Effect: "allow"},
		},
	}
	if err := p.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	counter, err := OpenCounter(filepath.Join(t.TempDir(), "gov.db"))
	if err != nil {
		t.Fatalf("OpenCounter: %v", err)
	}
	t.Cleanup(func() { counter.Close() })
	return &Gate{
		Policy:  p,
		Counter: counter,
		LogDir:  filepath.Join(t.TempDir(), "decisions"),
		Cwd:     cwd,
	}
}

func newWorktreeFixture(t *testing.T) (primary, linked string) {
	t.Helper()
	root := t.TempDir()
	primary = filepath.Join(root, "repo")
	linked = filepath.Join(root, "repo-task")

	runWorktreeGit(t, root, "init", primary)
	runWorktreeGit(t, primary, "config", "user.email", "test@example.com")
	runWorktreeGit(t, primary, "config", "user.name", "Test User")
	runWorktreeGit(t, primary, "checkout", "-b", "main")
	writeWorktreeFile(t, filepath.Join(primary, "README.md"), "base\n")
	runWorktreeGit(t, primary, "add", "README.md")
	runWorktreeGit(t, primary, "commit", "-m", "base")
	runWorktreeGit(t, primary, "worktree", "add", "-b", "task/worktree-policy", linked)
	return primary, linked
}

func runWorktreeGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func writeWorktreeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
