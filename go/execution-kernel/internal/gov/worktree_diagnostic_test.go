package gov

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGate_StampsWorktreeDiagnosticForPrimarySideEffect(t *testing.T) {
	repo, _ := tempGitRepoWithLinkedWorktree(t)
	g, _ := newTestGate(t)
	g.Cwd = repo
	g.Policy.Rules = append(g.Policy.Rules, Rule{
		ID:     "allow-write",
		Action: ActionMatcher{string(ActFileWrite)},
		Effect: "allow",
		Reason: "writes ok",
	})

	d := g.Evaluate(Action{Type: ActFileWrite, Target: "src/main.go"}, "agent1", nil)
	if !d.Allowed {
		t.Fatalf("diagnostic must not deny primary-checkout side effects: %+v", d)
	}
	if d.RuleID != "allow-write" {
		t.Fatalf("RuleID=%q want allow-write", d.RuleID)
	}
	if d.WorktreeDiagnosticRuleID != worktreeDiagnosticRuleID {
		t.Fatalf("WorktreeDiagnosticRuleID=%q want %s", d.WorktreeDiagnosticRuleID, worktreeDiagnosticRuleID)
	}
	if d.WorktreeStatus != "primary" {
		t.Fatalf("WorktreeStatus=%q want primary", d.WorktreeStatus)
	}
	if !strings.Contains(d.WorktreeReason, "primary git checkout") {
		t.Fatalf("WorktreeReason=%q should explain primary checkout", d.WorktreeReason)
	}
}

func TestGate_DoesNotStampWorktreeDiagnosticForLinkedWorktree(t *testing.T) {
	_, linked := tempGitRepoWithLinkedWorktree(t)
	g, _ := newTestGate(t)
	g.Cwd = linked
	g.Policy.Rules = append(g.Policy.Rules, Rule{
		ID:     "allow-write",
		Action: ActionMatcher{string(ActFileWrite)},
		Effect: "allow",
		Reason: "writes ok",
	})

	d := g.Evaluate(Action{Type: ActFileWrite, Target: "src/main.go"}, "agent1", nil)
	if !d.Allowed {
		t.Fatalf("linked worktree write should remain allowed: %+v", d)
	}
	if d.WorktreeDiagnosticRuleID != "" || d.WorktreeStatus != "" || d.WorktreeReason != "" {
		t.Fatalf("linked worktree should not carry worktree diagnostic fields: %+v", d)
	}
}

func TestGate_DoesNotStampWorktreeDiagnosticForReadOnlyAction(t *testing.T) {
	repo, _ := tempGitRepoWithLinkedWorktree(t)
	g, _ := newTestGate(t)
	g.Cwd = repo

	d := g.Evaluate(Action{Type: ActFileRead, Target: "README.md"}, "agent1", nil)
	if !d.Allowed {
		t.Fatalf("read should remain allowed: %+v", d)
	}
	if d.WorktreeDiagnosticRuleID != "" || d.WorktreeStatus != "" || d.WorktreeReason != "" {
		t.Fatalf("read-only action should not carry worktree diagnostic fields: %+v", d)
	}
}

func TestGate_DoesNotStampWorktreeDiagnosticForT0ShellCat(t *testing.T) {
	repo, _ := tempGitRepoWithLinkedWorktree(t)
	g, _ := newTestGate(t)
	g.Cwd = repo
	g.Policy.Rules = append(g.Policy.Rules, Rule{
		ID:     "allow-shell",
		Action: ActionMatcher{string(ActShellExec)},
		Effect: "allow",
		Reason: "shell ok",
	})

	d := g.Evaluate(Action{
		Type:   ActShellExec,
		Target: "cat README.md",
		Params: map[string]any{"sub_action": "shell.cat"},
	}, "agent1", nil)
	if !d.Allowed {
		t.Fatalf("shell.cat should remain allowed: %+v", d)
	}
	if d.WorktreeDiagnosticRuleID != "" || d.WorktreeStatus != "" || d.WorktreeReason != "" {
		t.Fatalf("shell.cat should not carry worktree diagnostic fields: %+v", d)
	}
}

func TestGate_DoesNotStampWorktreeDiagnosticForHTTPRequest(t *testing.T) {
	repo, _ := tempGitRepoWithLinkedWorktree(t)
	g, _ := newTestGate(t)
	g.Cwd = repo
	g.Policy.Rules = append(g.Policy.Rules, Rule{
		ID:     "allow-http",
		Action: ActionMatcher{string(ActHTTPRequest)},
		Effect: "allow",
		Reason: "http ok",
	})

	d := g.Evaluate(Action{Type: ActHTTPRequest, Target: "https://example.test"}, "agent1", nil)
	if !d.Allowed {
		t.Fatalf("http.request should remain allowed: %+v", d)
	}
	if d.WorktreeDiagnosticRuleID != "" || d.WorktreeStatus != "" || d.WorktreeReason != "" {
		t.Fatalf("http.request should not carry worktree diagnostic fields: %+v", d)
	}
}

func TestDetectGitWorktreeStatusHandlesNestedCwd(t *testing.T) {
	repo, linked := tempGitRepoWithLinkedWorktree(t)
	repoNested := filepath.Join(repo, "src", "pkg")
	linkedNested := filepath.Join(linked, "src", "pkg")
	if err := os.MkdirAll(repoNested, 0o755); err != nil {
		t.Fatalf("mkdir repo nested: %v", err)
	}
	if err := os.MkdirAll(linkedNested, 0o755); err != nil {
		t.Fatalf("mkdir linked nested: %v", err)
	}

	if got := detectGitWorktreeStatus(repoNested); got != "primary" {
		t.Fatalf("primary nested status=%q want primary", got)
	}
	if got := detectGitWorktreeStatus(linkedNested); got != "linked" {
		t.Fatalf("linked nested status=%q want linked", got)
	}
}

func TestWriteLog_PersistsWorktreeDiagnosticFields(t *testing.T) {
	dir := t.TempDir()
	d := Decision{
		Allowed:                  true,
		Mode:                     "guide",
		RuleID:                   "allow-write",
		Reason:                   "writes ok",
		Action:                   Action{Type: ActFileWrite, Target: "src/main.go"},
		Ts:                       "2026-05-10T00:00:00Z",
		WorktreeDiagnosticRuleID: worktreeDiagnosticRuleID,
		WorktreeStatus:           "primary",
		WorktreeReason:           "side-effect action evaluated from primary git checkout",
	}

	if err := WriteLog(d, dir); err != nil {
		t.Fatalf("WriteLog: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gov-decisions-2026-05-10.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	var row map[string]any
	if err := json.Unmarshal(bytesTrimSpace(data), &row); err != nil {
		t.Fatalf("unmarshal log row: %v", err)
	}
	if row["worktree_diagnostic_rule_id"] != worktreeDiagnosticRuleID {
		t.Fatalf("worktree_diagnostic_rule_id=%v", row["worktree_diagnostic_rule_id"])
	}
	if row["worktree_status"] != "primary" {
		t.Fatalf("worktree_status=%v", row["worktree_status"])
	}
	if row["worktree_reason"] == "" {
		t.Fatalf("worktree_reason should be persisted")
	}
}

func tempGitRepoWithLinkedWorktree(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	linked := filepath.Join(dir, "linked")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	runGit(t, repo, "worktree", "add", "-b", "linked-test", linked)
	return repo, linked
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
