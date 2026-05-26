package activities

import (
	"context"
	"encoding/json"
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

func fakeKernelBinForDeliver(t *testing.T) (binPath, sentinelPath string) {
	t.Helper()
	dir := t.TempDir()
	binPath = filepath.Join(dir, "chitin-kernel")
	sentinelPath = filepath.Join(dir, "captured.json")
	script := "#!/usr/bin/env bash\n" +
		"set -e\n" +
		"event_file=\"\"\n" +
		"while [[ $# -gt 0 ]]; do\n" +
		"  case \"$1\" in\n" +
		"    -event-file) event_file=\"$2\"; shift 2 ;;\n" +
		"    *) shift ;;\n" +
		"  esac\n" +
		"done\n" +
		"cp \"$event_file\" " + sentinelPath + "\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("setup fake kernel: %v", err)
	}
	return binPath, sentinelPath
}

func readDeliverEvent(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read emitted event: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal emitted event: %v\n%s", err, body)
	}
	return got
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

func TestDeliverWorkProduct_EmitsSilentDropReasons(t *testing.T) {
	bin, sentinel := fakeKernelBinForDeliver(t)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	t.Setenv("CHITIN_DIR", t.TempDir())

	t.Run("no changes to commit", func(t *testing.T) {
		_, worktree, _ := newDeliveryWorktree(t)
		_ = os.Remove(sentinel)
		res, err := NewDeliverWorkProduct().Execute(context.Background(), DeliverWorkProductInput{
			WorkUnitID: "wu-no-changes", SpecRef: "118-test", TaskRef: "T001",
			WorktreePath: worktree, BaseRef: "main",
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		assertSilentDropEvent(t, readDeliverEvent(t, sentinel), "wu-no-changes", "T001", "118-test", MissingDeliverableNoChangesToCommit)
		if res.PRURL != "" || !strings.Contains(res.Explanation, "missing deliverable: pr") {
			t.Fatalf("unexpected result: %+v", res)
		}
	})

	t.Run("activity declined without failure", func(t *testing.T) {
		_, worktree, _ := newDeliveryWorktree(t)
		if err := os.WriteFile(filepath.Join(worktree, "greeting.go"), []byte("package greeting\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(sentinel)
		res, err := NewDeliverWorkProduct().Execute(context.Background(), DeliverWorkProductInput{
			WorkUnitID: "wu-no-origin", SpecRef: "118-test", TaskRef: "T002",
			Description: "Implement greeting", WorktreePath: worktree, BaseRef: "main",
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		assertSilentDropEvent(t, readDeliverEvent(t, sentinel), "wu-no-origin", "T002", "118-test", MissingDeliverableActivityDeclinedWithoutFailure)
		if !res.Committed || res.Pushed || !strings.Contains(res.Explanation, "missing deliverable: pr") {
			t.Fatalf("unexpected result: %+v", res)
		}
	})

	t.Run("git push failed", func(t *testing.T) {
		repo, worktree, _ := newDeliveryWorktree(t)
		gitT(t, repo, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing-bare-repo"))
		if err := os.WriteFile(filepath.Join(worktree, "greeting.go"), []byte("package greeting\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(sentinel)
		res, err := NewDeliverWorkProduct().Execute(context.Background(), DeliverWorkProductInput{
			WorkUnitID: "wu-push", SpecRef: "118-test", TaskRef: "T003",
			Description: "Implement greeting", WorktreePath: worktree, BaseRef: "main",
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		assertSilentDropEvent(t, readDeliverEvent(t, sentinel), "wu-push", "T003", "118-test", MissingDeliverableGitPushFailed)
		if !res.Committed || res.Pushed || !strings.Contains(res.Explanation, "missing deliverable: pr") {
			t.Fatalf("unexpected result: %+v", res)
		}
	})

	t.Run("gh pr create failed", func(t *testing.T) {
		repo, worktree, _ := newDeliveryWorktree(t)
		bare := t.TempDir()
		gitT(t, bare, "init", "--bare", "-b", "main")
		gitT(t, repo, "remote", "add", "origin", bare)
		if err := os.WriteFile(filepath.Join(worktree, "greeting.go"), []byte("package greeting\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		ghDir := t.TempDir()
		gh := filepath.Join(ghDir, "gh")
		if err := os.WriteFile(gh, []byte("#!/usr/bin/env bash\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", ghDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		_ = os.Remove(sentinel)
		res, err := NewDeliverWorkProduct().Execute(context.Background(), DeliverWorkProductInput{
			WorkUnitID: "wu-gh", SpecRef: "118-test", TaskRef: "T004",
			Description: "Implement greeting", WorktreePath: worktree, BaseRef: "main",
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		assertSilentDropEvent(t, readDeliverEvent(t, sentinel), "wu-gh", "T004", "118-test", MissingDeliverableGHPRCreateFailed)
		if !res.Committed || !res.Pushed || res.PRURL != "" || !strings.Contains(res.Explanation, "missing deliverable: pr") {
			t.Fatalf("unexpected result: %+v", res)
		}
	})
}

func assertSilentDropEvent(t *testing.T, got map[string]any, workUnitID, taskID, specRef, reason string) {
	t.Helper()
	if got["event_type"] != WorkUnitCompletedWithoutDeliverableEventType {
		t.Fatalf("event_type=%v", got["event_type"])
	}
	payload := got["payload"].(map[string]any)
	if payload["work_unit_id"] != workUnitID {
		t.Fatalf("work_unit_id=%v want %s", payload["work_unit_id"], workUnitID)
	}
	if payload["task_id"] != taskID {
		t.Fatalf("task_id=%v want %s", payload["task_id"], taskID)
	}
	if payload["spec_ref"] != specRef {
		t.Fatalf("spec_ref=%v want %s", payload["spec_ref"], specRef)
	}
	if payload["deliverable_kind"] != DeliverableKindPR {
		t.Fatalf("deliverable_kind=%v", payload["deliverable_kind"])
	}
	if payload["reason"] != reason {
		t.Fatalf("reason=%v want %s", payload["reason"], reason)
	}
}
