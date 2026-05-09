package gov

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/canon"
)

func TestClassifyGit(t *testing.T) {
	t.Run("git status", func(t *testing.T) {
		c := canon.Command{Action: "status"}
		a := classifyGit(c, "git status")
		if a.Type != ActGitStatus {
			t.Errorf("expected ActGitStatus, got %v", a.Type)
		}
	})

	t.Run("git log", func(t *testing.T) {
		c := canon.Command{Action: "log"}
		a := classifyGit(c, "git log --oneline")
		if a.Type != ActGitLog {
			t.Errorf("expected ActGitLog, got %v", a.Type)
		}
	})

	t.Run("git diff", func(t *testing.T) {
		c := canon.Command{Action: "diff"}
		a := classifyGit(c, "git diff HEAD")
		if a.Type != ActGitDiff {
			t.Errorf("expected ActGitDiff, got %v", a.Type)
		}
	})

	t.Run("git commit", func(t *testing.T) {
		c := canon.Command{Action: "commit"}
		a := classifyGit(c, "git commit -m 'fix'")
		if a.Type != ActGitCommit {
			t.Errorf("expected ActGitCommit, got %v", a.Type)
		}
	})

	t.Run("git checkout", func(t *testing.T) {
		c := canon.Command{Action: "checkout"}
		a := classifyGit(c, "git checkout main")
		if a.Type != ActGitCheckout {
			t.Errorf("expected ActGitCheckout, got %v", a.Type)
		}
	})

	t.Run("git push --force-with-lease", func(t *testing.T) {
		c := canon.Command{Action: "push", Flags: map[string]string{"force-with-lease": ""}}
		a := classifyGit(c, "git push --force-with-lease origin main")
		if a.Type != ActGitForcePush {
			t.Errorf("expected ActGitForcePush, got %v", a.Type)
		}
	})

	t.Run("git push -f", func(t *testing.T) {
		c := canon.Command{Action: "push", Flags: map[string]string{"f": ""}}
		a := classifyGit(c, "git push -f origin main")
		if a.Type != ActGitForcePush {
			t.Errorf("expected ActGitForcePush, got %v", a.Type)
		}
	})

	t.Run("git worktree list", func(t *testing.T) {
		c := canon.Command{Action: "worktree"}
		a := classifyGit(c, "git worktree list")
		if a.Type != ActGitWorktreeList {
			t.Errorf("expected ActGitWorktreeList, got %v", a.Type)
		}
	})

	t.Run("git worktree add", func(t *testing.T) {
		c := canon.Command{Action: "worktree"}
		a := classifyGit(c, "git worktree add ../feature")
		if a.Type != ActGitWorktreeAdd {
			t.Errorf("expected ActGitWorktreeAdd, got %v", a.Type)
		}
	})

	t.Run("git worktree remove", func(t *testing.T) {
		c := canon.Command{Action: "worktree"}
		a := classifyGit(c, "git worktree remove ../feature")
		if a.Type != ActGitWorktreeRemove {
			t.Errorf("expected ActGitWorktreeRemove, got %v", a.Type)
		}
	})

	t.Run("git unknown subcmd falls through to shell", func(t *testing.T) {
		c := canon.Command{Action: "stash"}
		a := classifyGit(c, "git stash")
		if a.Type != ActShellExec {
			t.Errorf("expected ActShellExec for unknown git subcmd, got %v", a.Type)
		}
	})

	t.Run("git worktree unknown subcmd falls through to shell", func(t *testing.T) {
		c := canon.Command{Action: "worktree"}
		a := classifyGit(c, "git worktree move old new")
		if a.Type != ActShellExec {
			t.Errorf("expected ActShellExec for unknown worktree subcmd, got %v", a.Type)
		}
	})
}

func TestClassifyGh(t *testing.T) {
	t.Run("gh api", func(t *testing.T) {
		c := canon.Command{Action: "api"}
		a := classifyGh(c, "gh api /repos/owner/repo")
		if a.Type != ActGithubAPI {
			t.Errorf("expected ActGithubAPI, got %v", a.Type)
		}
	})

	t.Run("gh pr view", func(t *testing.T) {
		c := canon.Command{Action: "pr", Args: []string{"view", "123"}}
		a := classifyGh(c, "gh pr view 123")
		if a.Type != ActGithubPRView {
			t.Errorf("expected ActGithubPRView, got %v", a.Type)
		}
	})

	t.Run("gh pr list", func(t *testing.T) {
		c := canon.Command{Action: "pr", Args: []string{"list"}}
		a := classifyGh(c, "gh pr list")
		if a.Type != ActGithubPRList {
			t.Errorf("expected ActGithubPRList, got %v", a.Type)
		}
	})

	t.Run("gh pr merge", func(t *testing.T) {
		c := canon.Command{Action: "pr", Args: []string{"merge", "123"}}
		a := classifyGh(c, "gh pr merge 123")
		if a.Type != ActGithubPRMerge {
			t.Errorf("expected ActGithubPRMerge, got %v", a.Type)
		}
	})

	t.Run("gh pr close", func(t *testing.T) {
		c := canon.Command{Action: "pr", Args: []string{"close", "123"}}
		a := classifyGh(c, "gh pr close 123")
		if a.Type != ActGithubPRClose {
			t.Errorf("expected ActGithubPRClose, got %v", a.Type)
		}
	})

	t.Run("gh issue create", func(t *testing.T) {
		c := canon.Command{Action: "issue", Args: []string{"create"}}
		a := classifyGh(c, "gh issue create --title 'Bug'")
		if a.Type != ActGithubIssueCreate {
			t.Errorf("expected ActGithubIssueCreate, got %v", a.Type)
		}
	})

	t.Run("gh issue view", func(t *testing.T) {
		c := canon.Command{Action: "issue", Args: []string{"view", "456"}}
		a := classifyGh(c, "gh issue view 456")
		if a.Type != ActGithubIssueView {
			t.Errorf("expected ActGithubIssueView, got %v", a.Type)
		}
	})

	t.Run("gh issue list", func(t *testing.T) {
		c := canon.Command{Action: "issue", Args: []string{"list"}}
		a := classifyGh(c, "gh issue list")
		if a.Type != ActGithubIssueList {
			t.Errorf("expected ActGithubIssueList, got %v", a.Type)
		}
	})

	t.Run("gh issue close", func(t *testing.T) {
		c := canon.Command{Action: "issue", Args: []string{"close", "456"}}
		a := classifyGh(c, "gh issue close 456")
		if a.Type != ActGithubIssueClose {
			t.Errorf("expected ActGithubIssueClose, got %v", a.Type)
		}
	})

	t.Run("gh unknown subcmd falls through to shell", func(t *testing.T) {
		c := canon.Command{Action: "release", Args: []string{"create"}}
		a := classifyGh(c, "gh release create v1.0")
		if a.Type != ActShellExec {
			t.Errorf("expected ActShellExec for unknown gh subcmd, got %v", a.Type)
		}
	})

	t.Run("gh with no args falls through to shell", func(t *testing.T) {
		c := canon.Command{Action: "pr"}
		a := classifyGh(c, "gh pr")
		if a.Type != ActShellExec {
			t.Errorf("expected ActShellExec for gh with no args, got %v", a.Type)
		}
	})
}

func TestWriteLog(t *testing.T) {
	t.Run("writes decision to JSONL file", func(t *testing.T) {
		dir := t.TempDir()
		d := Decision{
			Allowed: false,
			Mode:    "enforce",
			RuleID:  "no-rm-rf",
			Reason:  "destructive command",
			Action:  Action{Type: ActShellExec, Target: "rm -rf /"},
			Agent:   "test-agent",
			Ts:      "2026-05-09T12:00:00Z",
		}
		if err := WriteLog(d, dir); err != nil {
			t.Fatalf("WriteLog: %v", err)
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 {
			t.Fatalf("expected 1 log file, got %d", len(files))
		}
		if !strings.HasPrefix(files[0].Name(), "gov-decisions-2026-05-09") {
			t.Errorf("expected filename starting with gov-decisions-2026-05-09, got %s", files[0].Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "no-rm-rf") {
			t.Errorf("expected log to contain rule_id 'no-rm-rf', got: %s", string(data))
		}
	})

	t.Run("auto-fills Ts when empty", func(t *testing.T) {
		dir := t.TempDir()
		d := Decision{
			Allowed: true,
			Action:  Action{Type: ActShellExec, Target: "ls"},
			Mode:   "enforce",
		}
		if err := WriteLog(d, dir); err != nil {
			t.Fatalf("WriteLog: %v", err)
		}
		files, _ := os.ReadDir(dir)
		data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		// Ts should have been auto-filled with current time
		if !strings.Contains(string(data), "202") {
			t.Errorf("expected Ts to be auto-filled with a date, got: %s", string(data))
		}
	})

	t.Run("creates directory when missing", func(t *testing.T) {
		dir := t.TempDir()
		nested := filepath.Join(dir, "sub", "dir")
		d := Decision{
			Allowed: true,
			Action:  Action{Type: ActShellExec, Target: "echo"},
			Mode:    "enforce",
			Ts:      "2026-05-09T00:00:00Z",
		}
		if err := WriteLog(d, nested); err != nil {
			t.Fatalf("WriteLog with missing dir: %v", err)
		}
		if _, err := os.Stat(filepath.Join(nested, "gov-decisions-2026-05-09.jsonl")); err != nil {
			t.Fatalf("expected file to exist: %v", err)
		}
	})
}