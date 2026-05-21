package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

// newTestRepo initializes a fresh git repository in a temp directory, makes
// one commit so HEAD exists, and returns the repo path. A worktree cannot be
// created from a repo with no commits, so the initial commit is mandatory.
func newTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		// A deterministic identity so `git commit` does not depend on global config.
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("seeding repo: %v", err)
	}
	run("add", "README.md")
	run("commit", "-m", "seed")
	return repo
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// TestCreateTeardown_RoundTrip proves the core lifecycle: Create yields a
// fresh worktree directory distinct from the primary checkout, on a new
// branch at the requested base ref, and Teardown removes it cleanly
// (spec 070 FR-013).
func TestCreateTeardown_RoundTrip(t *testing.T) {
	repo := newTestRepo(t)
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	path, err := mgr.Create(repo, "main", "wu-001")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !isDir(path) {
		t.Fatalf("Create returned %q, but no directory exists there", path)
	}
	// FR-013: the worktree is never the primary/shared checkout.
	if path == repo {
		t.Fatalf("Create returned the primary checkout %q — must be a dedicated worktree", repo)
	}
	if !underRoot(mgr.root, path) {
		t.Errorf("worktree %q is not under the manager root %q", path, mgr.root)
	}
	// The seed commit's file must be present — the worktree is a real checkout.
	if !isDir(path) || func() bool {
		_, err := os.Stat(filepath.Join(path, "README.md"))
		return err != nil
	}() {
		t.Errorf("worktree %q is missing the seeded README.md", path)
	}

	// The worktree is registered as active.
	mgr.mu.Lock()
	recs, _ := mgr.readActive()
	mgr.mu.Unlock()
	if len(recs) != 1 || recs[0].Path != path {
		t.Errorf("active registry = %v, want one record for %q", recs, path)
	}

	if err := mgr.Teardown(path); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if isDir(path) {
		t.Errorf("Teardown left worktree directory %q on disk", path)
	}
	mgr.mu.Lock()
	recs, _ = mgr.readActive()
	mgr.mu.Unlock()
	if len(recs) != 0 {
		t.Errorf("active registry = %v after Teardown, want empty", recs)
	}
}

// TestCreate_FromBaseRefAtCommit proves Create honors an arbitrary base ref —
// here a specific commit SHA, not just a branch name (spec 076 FR-013: each
// work unit carries a target repository and base ref).
func TestCreate_FromBaseRefAtCommit(t *testing.T) {
	repo := newTestRepo(t)

	// Resolve HEAD to a concrete SHA and use it as the base ref.
	sha, err := runGit(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}

	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	path, err := mgr.Create(repo, sha, "wu-sha")
	if err != nil {
		t.Fatalf("Create from SHA %s: %v", sha, err)
	}
	got, err := runGit(path, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD in worktree: %v", err)
	}
	if got != sha {
		t.Errorf("worktree HEAD = %s, want base ref %s", got, sha)
	}
}

// TestCreate_RejectsBadInput proves the input guards: empty repo, ref, or
// work-unit ID are rejected before any git command runs.
func TestCreate_RejectsBadInput(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	cases := []struct {
		name             string
		repo, ref, wuID  string
	}{
		{"empty repo", "", "main", "wu"},
		{"empty ref", "/tmp/repo", "", "wu"},
		{"empty work unit", "/tmp/repo", "main", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := mgr.Create(tc.repo, tc.ref, tc.wuID); err == nil {
				t.Errorf("Create(%q,%q,%q) = nil error, want rejection", tc.repo, tc.ref, tc.wuID)
			}
		})
	}
}

// TestTeardown_Idempotent proves Teardown is a no-op the second time: tearing
// down an already-removed worktree returns nil, never an error.
func TestTeardown_Idempotent(t *testing.T) {
	repo := newTestRepo(t)
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	path, err := mgr.Create(repo, "main", "wu-idem")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mgr.Teardown(path); err != nil {
		t.Fatalf("first Teardown: %v", err)
	}
	// Second Teardown of the same path — must be a clean no-op.
	if err := mgr.Teardown(path); err != nil {
		t.Errorf("second Teardown returned %v, want nil (idempotent)", err)
	}
	// Tearing down a path that was never a worktree is also a no-op.
	never := filepath.Join(t.TempDir(), "never-a-worktree")
	if err := mgr.Teardown(never); err != nil {
		t.Errorf("Teardown of a non-worktree path returned %v, want nil", err)
	}
}

// TestConcurrentCreate_IsolatedWorktrees proves two concurrent Create calls
// get distinct, isolated worktrees — never the same directory, never the same
// branch, never the shared checkout (spec 070 edge case: two work units run
// concurrently, each in its own isolated worktree).
func TestConcurrentCreate_IsolatedWorktrees(t *testing.T) {
	repo := newTestRepo(t)
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	const n = 8
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		paths []string
		errs  []error
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Deliberately the SAME work unit ID for every goroutine — the
			// random suffix is the only thing keeping them apart.
			p, err := mgr.Create(repo, "main", "wu-concurrent")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			paths = append(paths, p)
		}()
	}
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("concurrent Create errors: %v", errs)
	}
	if len(paths) != n {
		t.Fatalf("got %d worktrees, want %d", len(paths), n)
	}
	// Every path must be unique and distinct from the primary checkout.
	seen := map[string]bool{}
	for _, p := range paths {
		if p == repo {
			t.Errorf("a concurrent Create returned the primary checkout %q", repo)
		}
		if seen[p] {
			t.Errorf("duplicate worktree path %q across concurrent Create calls", p)
		}
		seen[p] = true
		if !isDir(p) {
			t.Errorf("worktree %q does not exist on disk", p)
		}
	}
	// The registry must hold exactly one record per worktree.
	mgr.mu.Lock()
	recs, _ := mgr.readActive()
	mgr.mu.Unlock()
	if len(recs) != n {
		t.Errorf("active registry has %d records, want %d", len(recs), n)
	}

	// Writing into one worktree must not be observable in another — true
	// filesystem isolation.
	marker := filepath.Join(paths[0], "isolated-marker")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing isolation marker: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths[1], "isolated-marker")); err == nil {
		t.Errorf("marker written in %q is visible in %q — worktrees not isolated", paths[0], paths[1])
	}

	for _, p := range paths {
		if err := mgr.Teardown(p); err != nil {
			t.Errorf("Teardown(%q): %v", p, err)
		}
	}
}

// TestGC_ReclaimsOrphan proves GC removes an orphaned worktree — one left by
// a crashed worker — while leaving a healthy, tracked worktree alone
// (spec 070 FR-014).
func TestGC_ReclaimsOrphan(t *testing.T) {
	repo := newTestRepo(t)
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// A healthy worktree — created normally, still tracked, recent.
	healthy, err := mgr.Create(repo, "main", "wu-healthy")
	if err != nil {
		t.Fatalf("Create healthy: %v", err)
	}

	// Simulate a crashed worker: a real git worktree under the manager root
	// that the manager has NO active record for. `git worktree add` directly,
	// bypassing the manager, models the worktree surviving a crash that lost
	// the registration.
	orphan := filepath.Join(mgr.root, "wu-orphan-deadbeef")
	if _, err := runGit(repo, "worktree", "add", "-b", "chitin/wu/orphan", orphan, "main"); err != nil {
		t.Fatalf("creating orphan worktree: %v", err)
	}
	if !isDir(orphan) {
		t.Fatalf("orphan worktree %q was not created", orphan)
	}

	reclaimed, err := mgr.GC(repo)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("GC reclaimed %d worktrees, want 1: %+v", len(reclaimed), reclaimed)
	}
	if reclaimed[0].Path != orphan {
		t.Errorf("GC reclaimed %q, want the orphan %q", reclaimed[0].Path, orphan)
	}
	if isDir(orphan) {
		t.Errorf("GC left the orphan directory %q on disk", orphan)
	}
	// The healthy, tracked worktree must survive GC untouched.
	if !isDir(healthy) {
		t.Errorf("GC removed the healthy worktree %q — it was tracked and recent", healthy)
	}

	if err := mgr.Teardown(healthy); err != nil {
		t.Errorf("Teardown healthy: %v", err)
	}
}

// TestGC_ReclaimsStaleTrackedWorktree proves the age branch: a worktree the
// manager DOES have a record for, but whose record is older than the GC
// threshold, is presumed orphaned and reclaimed.
func TestGC_ReclaimsStaleTrackedWorktree(t *testing.T) {
	repo := newTestRepo(t)

	// A controllable clock so "old" and "young" are deterministic.
	base := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	clock := base
	mgr, err := NewManagerWithOptions(t.TempDir(), time.Hour, func() time.Time { return clock })
	if err != nil {
		t.Fatalf("NewManagerWithOptions: %v", err)
	}

	// Created "now"; its record timestamps at base.
	stale, err := mgr.Create(repo, "main", "wu-stale")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// A second worktree created later — it will still be young at GC time.
	clock = base.Add(90 * time.Minute)
	young, err := mgr.Create(repo, "main", "wu-young")
	if err != nil {
		t.Fatalf("Create young: %v", err)
	}

	// Advance the clock so the first worktree's record is > 1h old but the
	// second's is not.
	clock = base.Add(2 * time.Hour)

	reclaimed, err := mgr.GC(repo)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(reclaimed) != 1 || reclaimed[0].Path != stale {
		t.Fatalf("GC reclaimed %+v, want exactly the stale worktree %q", reclaimed, stale)
	}
	if isDir(stale) {
		t.Errorf("GC left the stale worktree %q on disk", stale)
	}
	if !isDir(young) {
		t.Errorf("GC removed the young worktree %q — its record was within the threshold", young)
	}

	if err := mgr.Teardown(young); err != nil {
		t.Errorf("Teardown young: %v", err)
	}
}

// TestGC_NoOrphans proves GC over a repo with only healthy worktrees reclaims
// nothing and returns no error — safe to run on a schedule.
func TestGC_NoOrphans(t *testing.T) {
	repo := newTestRepo(t)
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	a, err := mgr.Create(repo, "main", "wu-a")
	if err != nil {
		t.Fatalf("Create a: %v", err)
	}
	b, err := mgr.Create(repo, "main", "wu-b")
	if err != nil {
		t.Fatalf("Create b: %v", err)
	}

	reclaimed, err := mgr.GC(repo)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(reclaimed) != 0 {
		t.Errorf("GC reclaimed %+v, want nothing — all worktrees are healthy", reclaimed)
	}
	if !isDir(a) || !isDir(b) {
		t.Errorf("GC removed a healthy worktree (a=%v, b=%v)", isDir(a), isDir(b))
	}

	for _, p := range []string{a, b} {
		if err := mgr.Teardown(p); err != nil {
			t.Errorf("Teardown(%q): %v", p, err)
		}
	}
}

// TestGC_DeterministicOrder proves GC returns reclaimed worktrees sorted by
// path — a stable, deterministic result across runs.
func TestGC_DeterministicOrder(t *testing.T) {
	repo := newTestRepo(t)
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// Three orphans created directly, in a deliberately unsorted order.
	for i, name := range []string{"wu-orphan-c", "wu-orphan-a", "wu-orphan-b"} {
		p := filepath.Join(mgr.root, name)
		branch := "chitin/wu/orphan-" + name
		if _, err := runGit(repo, "worktree", "add", "-b", branch, p, "main"); err != nil {
			t.Fatalf("orphan %d: %v", i, err)
		}
	}
	reclaimed, err := mgr.GC(repo)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	got := make([]string, len(reclaimed))
	for i, r := range reclaimed {
		got[i] = r.Path
	}
	want := make([]string, len(got))
	copy(want, got)
	sort.Strings(want)
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("GC result not sorted by path: got %v, want %v", got, want)
			break
		}
	}
}
