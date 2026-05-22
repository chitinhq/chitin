package activities

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitT runs a git command in dir with a deterministic identity, failing the
// test on error and returning the trimmed combined output.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newDeliveryWorktree builds a git repo with one seed commit and adds a
// dedicated worktree on a fresh branch — the shape DeliverWorkProduct expects.
// It returns the repo toplevel, the worktree path, and the branch name.
func newDeliveryWorktree(t *testing.T) (repo, worktree, branch string) {
	t.Helper()
	repo = t.TempDir()
	gitT(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("seeding repo: %v", err)
	}
	gitT(t, repo, "add", "README.md")
	gitT(t, repo, "commit", "-m", "seed")
	worktree = filepath.Join(t.TempDir(), "wt")
	branch = "chitin/wu/deliver-test"
	gitT(t, repo, "worktree", "add", "-b", branch, worktree, "main")
	return repo, worktree, branch
}

// TestDeliverWorkProduct_NoChanges proves a worktree the agent left unchanged
// is not committed — there is nothing to deliver, and the empty branch is left
// for teardown to reclaim.
func TestDeliverWorkProduct_NoChanges(t *testing.T) {
	_, worktree, branch := newDeliveryWorktree(t)

	res, err := NewDeliverWorkProduct().Execute(context.Background(), DeliverWorkProductInput{
		WorkUnitID: "wu-1", WorktreePath: worktree, BaseRef: "main",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Committed {
		t.Errorf("Committed = true for an unchanged worktree, want false")
	}
	if res.Branch != branch {
		t.Errorf("Branch = %q, want %q", res.Branch, branch)
	}
	if !strings.Contains(res.Explanation, "no changes") {
		t.Errorf("explanation = %q, want it to report no changes", res.Explanation)
	}
}

// TestDeliverWorkProduct_CommitsWithoutRemote proves an agent's changes are
// committed to the dedicated branch even when the target repo has no origin
// remote — the work product survives, it is simply not pushed.
func TestDeliverWorkProduct_CommitsWithoutRemote(t *testing.T) {
	repo, worktree, branch := newDeliveryWorktree(t)
	if err := os.WriteFile(filepath.Join(worktree, "greeting.go"),
		[]byte("package greeting\n"), 0o644); err != nil {
		t.Fatalf("writing work product: %v", err)
	}

	res, err := NewDeliverWorkProduct().Execute(context.Background(), DeliverWorkProductInput{
		WorkUnitID: "wu-2", SpecRef: "070", TaskRef: "T001",
		Description: "Implement the greeting", WorktreePath: worktree, BaseRef: "main",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Committed {
		t.Fatalf("Committed = false, want true; explanation: %s", res.Explanation)
	}
	if res.CommitSHA == "" {
		t.Error("CommitSHA is empty after a commit")
	}
	if res.Pushed {
		t.Error("Pushed = true with no origin remote")
	}
	// The branch carries the work product: exactly one commit past main.
	if count := gitT(t, repo, "rev-list", "--count", "main.."+branch); count != "1" {
		t.Errorf("branch %s is %s commits ahead of main, want 1", branch, count)
	}
}

// TestDeliverWorkProduct_PushesToRemote proves delivery commits and pushes the
// dedicated branch to the target repo's origin. The origin here is a bare local
// repo, not a GitHub host, so no PR is opened — delivery degrades to
// pushed-without-PR, which is the expected outcome off GitHub.
func TestDeliverWorkProduct_PushesToRemote(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	repo, worktree, branch := newDeliveryWorktree(t)
	bare := t.TempDir()
	gitT(t, bare, "init", "--bare", "-b", "main")
	gitT(t, repo, "remote", "add", "origin", bare)

	if err := os.WriteFile(filepath.Join(worktree, "greeting.go"),
		[]byte("package greeting\n"), 0o644); err != nil {
		t.Fatalf("writing work product: %v", err)
	}

	res, err := NewDeliverWorkProduct().Execute(ctx, DeliverWorkProductInput{
		WorkUnitID: "wu-3", SpecRef: "070", TaskRef: "T001",
		Description: "Implement the greeting", WorktreePath: worktree, BaseRef: "main",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Committed || !res.Pushed {
		t.Fatalf("Committed=%v Pushed=%v, want both true; explanation: %s",
			res.Committed, res.Pushed, res.Explanation)
	}
	// origin received the dedicated branch — gitT fails the test if the ref
	// does not resolve in the bare repo.
	gitT(t, bare, "rev-parse", "--verify", branch)
}
