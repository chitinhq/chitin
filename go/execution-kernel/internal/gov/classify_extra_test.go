package gov

import (
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/canon"
)

func TestClassifyGit(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ActionType
	}{
		{"force push", "git push --force origin main", ActGitForcePush},
		{"force-with-lease push", "git push --force-with-lease", ActGitForcePush},
		{"short force push", "git push -f origin", ActGitForcePush},
		{"normal push", "git push origin main", ActGitPush},
		{"bare push", "git push", ActGitPush},
		{"status", "git status", ActGitStatus},
		{"log", "git log --oneline", ActGitLog},
		{"diff", "git diff HEAD", ActGitDiff},
		{"commit", "git commit -m fix", ActGitCommit},
		{"checkout", "git checkout -b fix", ActGitCheckout},
		{"worktree list", "git worktree list", ActGitWorktreeList},
		{"worktree add", "git worktree add ../fix", ActGitWorktreeAdd},
		{"worktree remove", "git worktree remove ../fix", ActGitWorktreeRemove},
		{"unknown action", "git stash", ActShellExec},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := canon.ParseOne(tt.raw)
			got := classifyGit(c, tt.raw)
			if got.Type != tt.want {
				t.Errorf("classifyGit(%q) type = %v, want %v", tt.raw, got.Type, tt.want)
			}
		})
	}
}

func TestClassifyGh(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ActionType
	}{
		{"gh api", "gh api /repos/org/repo", ActGithubAPI},
		{"gh pr create", "gh pr create --title fix", ActGithubPRCreate},
		{"gh pr view", "gh pr view 123", ActGithubPRView},
		{"gh pr list", "gh pr list", ActGithubPRList},
		{"gh pr merge", "gh pr merge 123", ActGithubPRMerge},
		{"gh pr close", "gh pr close 123", ActGithubPRClose},
		{"gh issue create", "gh issue create --title bug", ActGithubIssueCreate},
		{"gh issue view", "gh issue view 456", ActGithubIssueView},
		{"gh issue list", "gh issue list", ActGithubIssueList},
		{"gh issue close", "gh issue close 456", ActGithubIssueClose},
		{"gh pr unknown-verb", "gh pr unknown 123", ActShellExec},
		{"gh issue unknown-verb", "gh issue unknown", ActShellExec},
		{"gh unknown-subcommand", "gh repo clone org/repo", ActShellExec},
		{"gh no args", "gh", ActShellExec},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := canon.ParseOne(tt.raw)
			got := classifyGh(c, tt.raw)
			if got.Type != tt.want {
				t.Errorf("classifyGh(%q) type = %v, want %v", tt.raw, got.Type, tt.want)
			}
		})
	}
}

func TestClassifyShellCommand_Branches(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ActionType
	}{
		{"rm -rf", "rm -rf /tmp/x", ActFileRecursiveDelete},
		{"git action", "git status", ActGitStatus},
		{"gh action", "gh pr list", ActGithubPRList},
		{"generic shell", "make build", ActShellExec},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyShellCommand(tt.raw)
			if got.Type != tt.want {
				t.Errorf("classifyShellCommand(%q) type = %v, want %v", tt.raw, got.Type, tt.want)
			}
		})
	}
}

func TestExtractShellIntent(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		wantSub string // non-empty = got must contain this substring
		wantMT  bool   // true = got must be non-empty; false = got must be empty
	}{
		{"subprocess list form", `subprocess.run(["rm", "-rf", "go/"])`, "rm -rf go/", true},
		{"subprocess string form", `subprocess.run("rm -rf go/", shell=True)`, "rm -rf go/", true},
		{"os.system", `os.system("rm -rf go/")`, "rm -rf go/", true},
		{"shutil.rmtree", `shutil.rmtree("go/")`, "rm -rf go/", true},
		{"os.remove", `os.remove("file.txt")`, "rm file.txt", true},
		{"os.unlink", `os.unlink("file.txt")`, "rm file.txt", true},
		{"bare rm -rf substring", "some code with rm -rf in it", "rm -rf", true},
		{"no dangerous pattern", "print('hello')", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractShellIntent(tt.code)
			if tt.wantMT {
				if got == "" {
					t.Errorf("extractShellIntent(%q) = empty, want non-empty", tt.code)
				}
				if tt.wantSub != "" && !strings.Contains(got, tt.wantSub) {
					t.Errorf("extractShellIntent(%q) = %q, want to contain %q", tt.code, got, tt.wantSub)
				}
			} else {
				if got != "" {
					t.Errorf("extractShellIntent(%q) = %q, want empty", tt.code, got)
				}
			}
		})
	}
}