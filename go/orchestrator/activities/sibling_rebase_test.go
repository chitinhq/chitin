package activities

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/worktree"
)

// TestRebaseSiblingPR_CleanRebase asserts the success path: a sibling PR
// whose changes are non-overlapping with the merged sibling rebases cleanly,
// force-pushes, and the activity returns Rebased=true with the new head SHA.
//
// Setup is a self-contained two-repo fixture:
//   - bare.git: the "remote" — siblings push branches here, main lives here
//   - local: the operator-host's working checkout — the activity's Manager
//     creates the rebase worktree against this clone
//
// Two sibling branches are created from main: pr-source touches file_a.txt;
// pr-rebase touches file_b.txt. pr-source merges to main via a fast-forward;
// pr-rebase then needs to rebase onto the new main. The rebase succeeds
// (disjoint files), the activity force-pushes, and bare.git ends up holding
// the rewritten pr-rebase branch on top of pr-source's commit.
func TestRebaseSiblingPR_CleanRebase(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "1")

	_, local, cleanup := newSiblingRebaseFixture(t)
	defer cleanup()

	// Two parallel sibling branches off main, each touching its OWN file.
	addBranchCommit(t, local, "pr-source", "file_a.txt", "from source PR\n")
	gitMust(t, local, "push", "origin", "pr-source")
	addBranchCommit(t, local, "pr-rebase", "file_b.txt", "from rebase PR\n")
	gitMust(t, local, "push", "origin", "pr-rebase")

	// Merge pr-source into main so origin/main is one commit ahead of
	// where pr-rebase forked from.
	gitMust(t, local, "checkout", "main")
	gitMust(t, local, "merge", "--no-ff", "-m", "Merge pr-source", "pr-source")
	gitMust(t, local, "push", "origin", "main")

	// Run the activity. The rebase moves pr-rebase onto the new main tip.
	mgr := newTestManager(t)
	act := NewRebaseSiblingPR(mgr)
	res, err := act.Execute(context.Background(), RebaseSiblingPRInput{
		PRNumber:       42,
		PRBranch:       "pr-rebase",
		TargetRepo:     local,
		BaseBranch:     "main",
		SchedulerRunID: "run-clean",
		SourcePRNumber: 41,
		WorkUnitID:     "rebase-pr-42",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v (activity must be fail-soft)", err)
	}
	if !res.Rebased {
		t.Fatalf("expected Rebased=true on clean rebase, got result %+v", res)
	}
	if res.NewHeadSHA == "" {
		t.Fatal("expected non-empty NewHeadSHA on clean rebase")
	}
	if res.NewBaseSHA == "" {
		t.Fatal("expected non-empty NewBaseSHA on clean rebase")
	}
	if len(res.ConflictFiles) != 0 {
		t.Fatalf("expected no conflict files, got %v", res.ConflictFiles)
	}

	// Verify the remote branch was actually rewritten: its tip's parent
	// chain must now include the merge commit on main.
	mainTip := gitMust(t, local, "rev-parse", "origin/main")
	wantContains := mainTip
	prTipParents := gitMust(t, local, "log", "--pretty=%H", "origin/pr-rebase")
	if !strings.Contains(prTipParents, wantContains) {
		t.Fatalf("rebased branch should contain origin/main (%s) in its history, got:\n%s",
			wantContains, prTipParents)
	}
}

// TestRebaseSiblingPR_ConflictAborts asserts the conflict path: two siblings
// both add the SAME file with different content. The first merges to main;
// the second cannot rebase ("both added" conflict). The activity must abort
// the rebase cleanly, list the conflicting file, return Rebased=false, and
// leave the remote branch untouched (no force-push of a half-rebased state).
func TestRebaseSiblingPR_ConflictAborts(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "1")

	bare, local, cleanup := newSiblingRebaseFixture(t)
	defer cleanup()

	// Two parallel sibling branches each ADD review_mode.go with different
	// content — the canonical "both added" conflict from the 2026-05-24
	// dogfood that motivated US2.
	addBranchCommit(t, local, "pr-source-conflict", "review_mode.go",
		"package claudecode\n\n// from source\nfunc SourceFn() {}\n")
	gitMust(t, local, "push", "origin", "pr-source-conflict")
	addBranchCommit(t, local, "pr-rebase-conflict", "review_mode.go",
		"package claudecode\n\n// from rebase\nfunc RebaseFn() {}\n")
	gitMust(t, local, "push", "origin", "pr-rebase-conflict")

	// Capture the remote tip BEFORE rebase so we can prove the activity
	// did not clobber it on conflict.
	remoteTipBefore := gitMust(t, local, "rev-parse", "origin/pr-rebase-conflict")

	gitMust(t, local, "checkout", "main")
	gitMust(t, local, "merge", "--no-ff", "-m", "Merge pr-source-conflict", "pr-source-conflict")
	gitMust(t, local, "push", "origin", "main")

	mgr := newTestManager(t)
	act := NewRebaseSiblingPR(mgr)
	res, err := act.Execute(context.Background(), RebaseSiblingPRInput{
		PRNumber:       99,
		PRBranch:       "pr-rebase-conflict",
		TargetRepo:     local,
		BaseBranch:     "main",
		SchedulerRunID: "run-conflict",
		SourcePRNumber: 98,
		WorkUnitID:     "rebase-pr-99",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v (activity must be fail-soft on conflict)", err)
	}
	if res.Rebased {
		t.Fatalf("expected Rebased=false on conflict, got result %+v", res)
	}
	if !reflect.DeepEqual(res.ConflictFiles, []string{"review_mode.go"}) {
		t.Fatalf("expected ConflictFiles=[review_mode.go], got %v", res.ConflictFiles)
	}
	if res.NewBaseSHA == "" {
		t.Fatal("expected NewBaseSHA to be populated even on conflict (operator sees what the rebase aimed at)")
	}

	// Critical invariant: the remote branch tip is unchanged. A failed
	// rebase MUST NOT force-push a half-rebased state.
	remoteTipAfter := gitMust(t, local, "rev-parse", "origin/pr-rebase-conflict")
	if remoteTipBefore != remoteTipAfter {
		t.Fatalf("conflict path must not push: tip before=%s after=%s", remoteTipBefore, remoteTipAfter)
	}

	_ = bare // bare exists only to host the remote — referenced via origin.
}

// TestRebaseSiblingPR_NonConflictFailureCarriesGitError asserts the
// distinguished-failure path: a non-zero rebase exit with NO conflict files
// (e.g. a missing base ref) surfaces as Rebased=false with the underlying
// git error in Explanation, NOT as a "0 conflict files" mislabeling.
func TestRebaseSiblingPR_NonConflictFailureCarriesGitError(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "1")

	_, local, cleanup := newSiblingRebaseFixture(t)
	defer cleanup()

	addBranchCommit(t, local, "pr-needs-rebase", "file_c.txt", "content\n")
	gitMust(t, local, "push", "origin", "pr-needs-rebase")
	// Switch back to main so the local checkout of pr-needs-rebase below
	// (in the Manager's worktree) doesn't collide with the local repo's
	// own current branch — `git worktree add -B` refuses to check out a
	// branch already checked out elsewhere.
	gitMust(t, local, "checkout", "main")

	mgr := newTestManager(t)
	act := NewRebaseSiblingPR(mgr)
	// BaseBranch points at a ref that does not exist on origin. The rebase
	// fails immediately — NOT because of a merge conflict.
	res, err := act.Execute(context.Background(), RebaseSiblingPRInput{
		PRNumber:       55,
		PRBranch:       "pr-needs-rebase",
		TargetRepo:     local,
		BaseBranch:     "nonexistent-branch",
		SchedulerRunID: "run-fault",
		SourcePRNumber: 54,
		WorkUnitID:     "rebase-pr-55",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v (must be fail-soft)", err)
	}
	if res.Rebased {
		t.Fatal("expected Rebased=false on git fault")
	}
	if len(res.ConflictFiles) != 0 {
		t.Fatalf("expected zero conflict files on non-conflict fault, got %v", res.ConflictFiles)
	}
	if !strings.Contains(res.Explanation, "git fault") {
		t.Fatalf("expected explanation to mention git fault, got %q", res.Explanation)
	}
}

// TestRebaseSiblingPR_NoManager asserts the guard: an activity with a nil
// Manager returns a populated result rather than panicking.
func TestRebaseSiblingPR_NoManager(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "1")
	act := NewRebaseSiblingPR(nil)
	res, err := act.Execute(context.Background(), RebaseSiblingPRInput{
		PRNumber:   1,
		PRBranch:   "any",
		TargetRepo: "any",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v (must be fail-soft)", err)
	}
	if res.Rebased {
		t.Fatal("expected Rebased=false when no Manager bound")
	}
	if !strings.Contains(res.Explanation, "no worktree Manager bound") {
		t.Fatalf("expected explanation to name the missing Manager, got %q", res.Explanation)
	}
}

// TestRebaseSiblingPR_MissingFields asserts the guard for partial inputs.
func TestRebaseSiblingPR_MissingFields(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "1")
	mgr := newTestManager(t)
	act := NewRebaseSiblingPR(mgr)
	res, err := act.Execute(context.Background(), RebaseSiblingPRInput{
		PRNumber: 1,
		// PRBranch + TargetRepo deliberately empty.
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v (must be fail-soft)", err)
	}
	if res.Rebased {
		t.Fatal("expected Rebased=false with missing fields")
	}
	if !strings.Contains(res.Explanation, "missing PRBranch or TargetRepo") {
		t.Fatalf("expected explanation to name the missing fields, got %q", res.Explanation)
	}
}

// TestReadConflictFiles_ParseShape asserts the porcelain parser picks UU/AA/DD
// codes and rejects clean lines. Pure function — no git involved.
func TestReadConflictFiles_ParseShape(t *testing.T) {
	// A real conflict scenario: write a tiny worktree with conflict markers
	// in `git status --porcelain` by simulating it through a bare staging
	// directory. The simpler path here is to spin a fresh repo with two
	// branches that both add the same file and then `git merge` so status
	// emits the AA codes — exactly what isConflictCode promises to match.
	dir := t.TempDir()
	gitMust(t, dir, "init", "-q", "-b", "main", dir)
	gitMust(t, dir, "config", "user.email", "test@chitin.local")
	gitMust(t, dir, "config", "user.name", "test")
	writeFile(t, filepath.Join(dir, "seed.txt"), "seed\n")
	gitMust(t, dir, "add", "seed.txt")
	gitMust(t, dir, "commit", "-q", "-m", "seed")

	gitMust(t, dir, "checkout", "-q", "-b", "branch-a")
	writeFile(t, filepath.Join(dir, "fight.txt"), "from A\n")
	gitMust(t, dir, "add", "fight.txt")
	gitMust(t, dir, "commit", "-q", "-m", "A adds fight.txt")

	gitMust(t, dir, "checkout", "-q", "main")
	gitMust(t, dir, "checkout", "-q", "-b", "branch-b")
	writeFile(t, filepath.Join(dir, "fight.txt"), "from B\n")
	gitMust(t, dir, "add", "fight.txt")
	gitMust(t, dir, "commit", "-q", "-m", "B adds fight.txt")

	// Merge produces an "both added" AA conflict. Use --no-commit so we stop
	// in the conflict state for the porcelain read.
	_, _ = gitTry(t, dir, "merge", "--no-commit", "--no-ff", "branch-a")

	conflicts := readConflictFiles(context.Background(), dir)
	if !reflect.DeepEqual(conflicts, []string{"fight.txt"}) {
		t.Fatalf("expected [fight.txt], got %v", conflicts)
	}

	// Cleanup: abort the merge so the temp dir is in a clean state for
	// t.TempDir's auto-removal.
	_, _ = gitTry(t, dir, "merge", "--abort")
}

// --- fixture helpers --------------------------------------------------------

// newSiblingRebaseFixture sets up the two-repo fixture: a bare "origin" and a
// working clone whose `origin` points at the bare. Seeds main with one commit
// so branches have something to fork from. Returns (bareDir, workingDir,
// cleanup).
func newSiblingRebaseFixture(t *testing.T) (string, string, func()) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "bare.git")
	local := filepath.Join(root, "local")

	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir bare: %v", err)
	}
	gitMust(t, root, "init", "-q", "--bare", "-b", "main", bare)

	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}
	gitMust(t, local, "init", "-q", "-b", "main", local)
	gitMust(t, local, "config", "user.email", "test@chitin.local")
	gitMust(t, local, "config", "user.name", "test")
	gitMust(t, local, "remote", "add", "origin", bare)

	writeFile(t, filepath.Join(local, "README.md"), "seed\n")
	gitMust(t, local, "add", "README.md")
	gitMust(t, local, "commit", "-q", "-m", "seed")
	gitMust(t, local, "push", "-u", "origin", "main")

	return bare, local, func() {} // t.TempDir auto-cleans.
}

// newTestManager returns a fresh worktree.Manager rooted in a temp dir so the
// rebase activity's checkout lifecycle stays hermetic.
func newTestManager(t *testing.T) *worktree.Manager {
	t.Helper()
	mgr, err := worktree.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr
}

// addBranchCommit creates `branch` off the current HEAD of `repo`, writes
// path with content, commits it.
func addBranchCommit(t *testing.T, repo, branch, path, content string) {
	t.Helper()
	gitMust(t, repo, "checkout", "-q", "main")
	gitMust(t, repo, "checkout", "-q", "-b", branch)
	writeFile(t, filepath.Join(repo, path), content)
	gitMust(t, repo, "add", path)
	gitMust(t, repo, "commit", "-q", "-m", fmt.Sprintf("%s: add %s", branch, path))
}

// writeFile writes content to path, creating directories as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// gitMust runs git in dir and fatals on non-zero exit; returns trimmed stdout.
func gitMust(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitTry(t, dir, args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v\nout: %s", strings.Join(args, " "), dir, err, out)
	}
	return out
}

// gitTry runs git in dir and returns trimmed stdout. Non-zero exit yields an
// error carrying stderr. Named `gitTry` to avoid colliding with the
// package-private `git` helper in deliver.go.
func gitTry(_ *testing.T, dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(stdout.String()),
			fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
