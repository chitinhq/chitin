// Package worktree creates, tears down, and garbage-collects the dedicated
// git worktrees in which the Chitin Orchestrator runs every dispatched work
// unit (spec 070 FR-013/FR-014).
//
// The platform invariant is absolute: a work unit NEVER executes in the
// primary/shared repository checkout. Each work unit gets a fresh worktree on
// a new branch, created from a named target repository at a named base ref,
// and that worktree is torn down on completion. A worktree orphaned by a
// crashed worker is reclaimable — GC finds and removes it.
//
// This package is pure Go shelling out to `git worktree`; it holds no state
// of its own beyond a root directory and a registry path, so two Managers
// over the same root behave identically.
package worktree

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultGCThreshold is the age past which an untracked registered worktree is
// presumed orphaned by a crashed worker and is reclaimable by GC. A worktree
// younger than this is left alone — a live worker may still be using it.
const DefaultGCThreshold = 6 * time.Hour

// Manager creates and reclaims git worktrees for dispatched work units.
//
// A Manager is safe for concurrent use: every mutation of the on-disk active
// registry is serialized by mu, and each Create produces a uniquely-named
// worktree directory and branch, so concurrent Create calls never collide.
type Manager struct {
	// root is the directory under which every managed worktree is created.
	// It is never the primary checkout — worktrees live in a dedicated tree.
	root string

	// gcThreshold is the minimum age an untracked worktree must reach before
	// GC will reclaim it. Zero means DefaultGCThreshold.
	gcThreshold time.Duration

	// now returns the current time; overridable in tests for deterministic
	// age comparisons.
	now func() time.Time

	// mu serializes reads and writes of the active-registry file so two
	// concurrent Create/Teardown calls never corrupt it.
	mu sync.Mutex

	// gitMu guards the per-repository serialization of `git worktree`
	// mutations. `git worktree add` / `remove` update the repo's
	// .git/worktrees administrative directory non-atomically, so two
	// concurrent invocations against the same repo can corrupt each other.
	// Serializing per repo makes concurrent Create calls safe; they still
	// produce distinct, isolated worktrees — only git's metadata write is
	// serialized, not the work.
	gitMu   sync.Mutex
	repoMus map[string]*sync.Mutex
}

// record is one entry in the active-worktree registry: a worktree the Manager
// currently considers in use. GC reclaims any registered git worktree that has
// no matching active record (or whose record is older than the threshold).
type record struct {
	// Path is the absolute worktree directory.
	Path string `json:"path"`
	// Branch is the dedicated branch the worktree was created on.
	Branch string `json:"branch"`
	// BaseSHA is the commit the dedicated branch was created at — the resolved
	// SHA of the base ref. Teardown compares the branch's tip against it: a tip
	// still equal to BaseSHA means the work unit produced no commits, so the
	// branch is empty litter Teardown deletes; a tip past BaseSHA is a work
	// product (an agent node's commits, perhaps an open PR) the branch keeps.
	// Empty for a record written before this field existed, or when the base
	// ref could not be resolved — Teardown then conservatively keeps the branch.
	BaseSHA string `json:"base_sha"`
	// WorkUnitID is the work unit the worktree was created for.
	WorkUnitID string `json:"work_unit_id"`
	// CreatedAt is when Create produced the worktree.
	CreatedAt time.Time `json:"created_at"`
}

// NewManager returns a Manager that creates worktrees under root, using the
// default GC threshold and the system clock. root is created if absent.
func NewManager(root string) (*Manager, error) {
	return NewManagerWithOptions(root, DefaultGCThreshold, time.Now)
}

// NewManagerWithOptions returns a Manager with an explicit GC threshold and
// clock — the test-friendly constructor. A non-positive threshold falls back
// to DefaultGCThreshold; a nil clock falls back to time.Now. root is created
// if absent.
func NewManagerWithOptions(root string, gcThreshold time.Duration, now func() time.Time) (*Manager, error) {
	if root == "" {
		return nil, errors.New("worktree: root directory must not be empty")
	}
	if gcThreshold <= 0 {
		gcThreshold = DefaultGCThreshold
	}
	if now == nil {
		now = time.Now
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("worktree: resolving root %q: %w", root, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("worktree: creating root %q: %w", abs, err)
	}
	return &Manager{
		root:        abs,
		gcThreshold: gcThreshold,
		now:         now,
		repoMus:     make(map[string]*sync.Mutex),
	}, nil
}

// repoLock returns the mutex that serializes `git worktree` mutations against
// the repository at repoAbs. One mutex is minted per repo path on first use.
func (m *Manager) repoLock(repoAbs string) *sync.Mutex {
	m.gitMu.Lock()
	defer m.gitMu.Unlock()
	mu, ok := m.repoMus[repoAbs]
	if !ok {
		mu = &sync.Mutex{}
		m.repoMus[repoAbs] = mu
	}
	return mu
}

// Create produces a FRESH dedicated git worktree for one work unit and returns
// its absolute path (spec 070 FR-013).
//
// The worktree is created from targetRepo, checked out on a NEW branch whose
// HEAD is baseRef. It is never the primary/shared checkout: a unique directory
// under the Manager root and a unique branch name are minted for every call,
// so the result is always a private work surface. An orphan is never silently
// reused — Create always allocates fresh.
//
// targetRepo is the repository to branch from (spec 076 FR-013: each work unit
// carries a target repository and base ref). baseRef is any ref `git` accepts
// — a branch, tag, or commit SHA. workUnitID identifies the work unit and is
// woven into the branch and directory names so an operator can read them.
//
// On any failure the partially-created worktree is cleaned up before the error
// is returned, so a failed Create leaves no orphan of its own.
func (m *Manager) Create(targetRepo, baseRef, workUnitID string) (string, error) {
	if targetRepo == "" {
		return "", errors.New("worktree: target repo must not be empty")
	}
	if baseRef == "" {
		return "", errors.New("worktree: base ref must not be empty")
	}
	if workUnitID == "" {
		return "", errors.New("worktree: work unit ID must not be empty")
	}
	repoAbs, err := filepath.Abs(targetRepo)
	if err != nil {
		return "", fmt.Errorf("worktree: resolving target repo %q: %w", targetRepo, err)
	}
	// Canonicalize to the owning repository toplevel so the per-repo lock key
	// is stable no matter whether the caller passed the toplevel, a
	// subdirectory, or a non-canonical path — Teardown locks on the same key.
	repoAbs, lockKey, err := resolveRepo(repoAbs)
	if err != nil {
		return "", fmt.Errorf("worktree: target repo %q is not a git repository: %w", targetRepo, err)
	}

	// A random suffix guarantees two concurrent Create calls for the SAME
	// work unit still get distinct directories and branches.
	suffix, err := randomSuffix()
	if err != nil {
		return "", fmt.Errorf("worktree: generating unique suffix: %w", err)
	}
	slug := sanitize(workUnitID)
	dirName := fmt.Sprintf("wu-%s-%s", slug, suffix)
	worktreePath := filepath.Join(m.root, dirName)
	branch := fmt.Sprintf("chitin/wu/%s-%s", slug, suffix)

	// `git worktree add -b <branch> <path> <baseRef>` creates the new branch
	// at baseRef and checks it out into a brand-new directory. The per-repo
	// lock serializes this mutation: git's .git/worktrees metadata is updated
	// non-atomically, so concurrent adds against the same repo must not race.
	repoMu := m.repoLock(lockKey)
	repoMu.Lock()
	_, addErr := runGit(repoAbs, "worktree", "add", "-b", branch, worktreePath, baseRef)
	repoMu.Unlock()
	if err := addErr; err != nil {
		// Best-effort cleanup of any half-created state — Create leaves no orphan.
		repoMu.Lock()
		_, _ = runGit(repoAbs, "worktree", "remove", "--force", worktreePath)
		repoMu.Unlock()
		_ = os.RemoveAll(worktreePath)
		return "", fmt.Errorf("worktree: creating worktree for work unit %q from %s@%s: %w",
			workUnitID, targetRepo, baseRef, err)
	}

	// Resolve the base ref to the concrete commit the new branch starts at.
	// Teardown compares the branch tip against this to tell an empty worktree
	// (delete the branch) from one carrying a work product (keep it). A ref
	// that will not resolve leaves baseSHA empty — Teardown then keeps the
	// branch rather than risk deleting work. This is a pure read, so it needs
	// no per-repo lock.
	baseSHA, baseErr := runGit(repoAbs, "rev-parse", "--verify", "--quiet", baseRef+"^{commit}")
	if baseErr != nil {
		baseSHA = ""
	}

	rec := record{
		Path:       worktreePath,
		Branch:     branch,
		BaseSHA:    baseSHA,
		WorkUnitID: workUnitID,
		CreatedAt:  m.now().UTC(),
	}
	if err := m.addActive(rec); err != nil {
		// Registry write failed — undo the worktree so it is not orphaned.
		repoMu.Lock()
		_, _ = runGit(repoAbs, "worktree", "remove", "--force", worktreePath)
		repoMu.Unlock()
		_ = os.RemoveAll(worktreePath)
		return "", fmt.Errorf("worktree: registering worktree for work unit %q: %w", workUnitID, err)
	}
	return worktreePath, nil
}

// Checkout mints a dedicated worktree that checks out an EXISTING remote
// branch — the sibling-rebase path (spec 112 US2) where the activity must
// operate on a PR's branch rather than open a fresh chitin/wu/* one.
//
// It fetches origin first so the branch and its base are at their newest
// remote tip, then does `git worktree add -B <branch> <path> origin/<branch>`,
// which creates or resets the local branch to the freshly fetched ref. The
// worktree is registered like a Create-minted one — Teardown reclaims it via
// the same path, and GC sweeps it if the caller crashes — so the same
// lifecycle guarantees apply.
//
// BaseSHA is left empty in the registry record because the branch already
// carries work; Teardown's empty-branch cleanup (which deletes a branch whose
// tip equals its base) MUST NOT run here — we never want to delete the PR's
// branch out from under an open pull request.
//
// Returns the absolute worktree path. The caller is responsible for calling
// Teardown(path); on any failure the partially-created worktree is cleaned up
// before the error is returned.
func (m *Manager) Checkout(targetRepo, branch, workUnitID string) (string, error) {
	if targetRepo == "" {
		return "", errors.New("worktree: target repo must not be empty")
	}
	if branch == "" {
		return "", errors.New("worktree: branch must not be empty")
	}
	if workUnitID == "" {
		return "", errors.New("worktree: work unit ID must not be empty")
	}
	repoAbs, err := filepath.Abs(targetRepo)
	if err != nil {
		return "", fmt.Errorf("worktree: resolving target repo %q: %w", targetRepo, err)
	}
	repoAbs, lockKey, err := resolveRepo(repoAbs)
	if err != nil {
		return "", fmt.Errorf("worktree: target repo %q is not a git repository: %w", targetRepo, err)
	}

	// Fetch the branch (and the remote's HEAD, which carries the base) so the
	// upcoming worktree has the newest commits. A fetch failure is fatal — a
	// rebase against a stale origin would produce the wrong answer.
	repoMu := m.repoLock(lockKey)
	repoMu.Lock()
	_, fetchErr := runGit(repoAbs, "fetch", "origin", "--prune")
	repoMu.Unlock()
	if fetchErr != nil {
		return "", fmt.Errorf("worktree: fetching origin for %q: %w", targetRepo, fetchErr)
	}

	suffix, err := randomSuffix()
	if err != nil {
		return "", fmt.Errorf("worktree: generating unique suffix: %w", err)
	}
	slug := sanitize(workUnitID)
	dirName := fmt.Sprintf("co-%s-%s", slug, suffix)
	worktreePath := filepath.Join(m.root, dirName)

	// `git worktree add -B <branch> <path> origin/<branch>` creates or resets
	// the local branch to the freshly fetched remote tip and checks it out in
	// a brand-new directory. `-B` is force-create — necessary so a pre-existing
	// stale local branch of the same name does not block the checkout.
	repoMu.Lock()
	_, addErr := runGit(repoAbs, "worktree", "add", "-B", branch, worktreePath, "origin/"+branch)
	repoMu.Unlock()
	if addErr != nil {
		repoMu.Lock()
		_, _ = runGit(repoAbs, "worktree", "remove", "--force", worktreePath)
		repoMu.Unlock()
		_ = os.RemoveAll(worktreePath)
		return "", fmt.Errorf("worktree: checking out branch %q for work unit %q: %w",
			branch, workUnitID, addErr)
	}

	rec := record{
		Path:       worktreePath,
		Branch:     branch,
		BaseSHA:    "", // see doc: empty so Teardown never deletes the PR's branch.
		WorkUnitID: workUnitID,
		CreatedAt:  m.now().UTC(),
	}
	if err := m.addActive(rec); err != nil {
		repoMu.Lock()
		_, _ = runGit(repoAbs, "worktree", "remove", "--force", worktreePath)
		repoMu.Unlock()
		_ = os.RemoveAll(worktreePath)
		return "", fmt.Errorf("worktree: registering checkout worktree for work unit %q: %w", workUnitID, err)
	}
	return worktreePath, nil
}

// Teardown removes the worktree at path and prunes git's worktree registry.
// It is idempotent: tearing down a path that is already gone (or was never a
// worktree) is a no-op that returns nil, never an error — a second Teardown,
// or a Teardown of a worktree a crash already removed, is safe.
//
// Teardown also drops path from the active registry, so a torn-down worktree
// is no longer counted as in use by GC.
func (m *Manager) Teardown(path string) error {
	if path == "" {
		return errors.New("worktree: path must not be empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("worktree: resolving path %q: %w", path, err)
	}

	// Take the active record first — unconditionally, so the registry is clean
	// even if a crash already removed the worktree directory on disk. The
	// record carries the dedicated branch and its base SHA, which drive the
	// empty-branch cleanup below; an untracked path yields tracked=false.
	rec, tracked, err := m.takeActive(abs)
	if err != nil {
		return fmt.Errorf("worktree: deregistering worktree %q: %w", abs, err)
	}

	owner, err := repoForWorktree(abs)
	if err != nil {
		// The directory is gone, or it was never a git worktree — nothing on
		// disk to remove. Idempotent: a no-op, not an error.
		return nil
	}

	// Serialize the git mutations on the SAME per-repo lock Create uses, keyed
	// by the owning repository — `git worktree remove`/`prune` write the same
	// non-atomic .git/worktrees metadata that `git worktree add` does.
	repoMu := m.repoLock(owner)
	repoMu.Lock()
	defer repoMu.Unlock()

	// `--force` removes the worktree even if it has local modifications — a
	// dispatched work unit's worktree is disposable by design. A non-zero exit
	// here means the worktree was already detached/removed; treat as a no-op.
	_, _ = runGit(owner, "worktree", "remove", "--force", abs)
	// Defensively remove any directory remnant `git` left behind.
	_ = os.RemoveAll(abs)
	// Prune stale administrative entries so git's registry matches disk.
	_, _ = runGit(owner, "worktree", "prune")

	// Delete the worktree's dedicated branch IFF it carries no work product —
	// its tip is still the base commit, so the work unit produced no commits
	// and the branch is empty litter. A branch advanced past its base holds a
	// work product (an agent node's commits, perhaps an open PR) and is kept.
	// An untracked teardown — GC reclaiming an orphan with no record — has no
	// base SHA to compare and so conservatively keeps the branch. `git branch
	// -D` refuses to delete a branch still checked out in any worktree, so a
	// failed `worktree remove` above cannot cause a live branch to be dropped.
	if tracked && rec.Branch != "" && rec.BaseSHA != "" {
		tip, tipErr := runGit(owner, "rev-parse", "--verify", "--quiet", rec.Branch+"^{commit}")
		if tipErr == nil && tip == rec.BaseSHA {
			// A failed delete is non-fatal: the worktree itself is already
			// gone, and a leftover empty branch is cosmetic — a GC-class sweep
			// can reclaim it later. Swallowed to match the best-effort handling
			// of `worktree remove` above; the package keeps no logger.
			_, _ = runGit(owner, "branch", "-D", rec.Branch)
		}
	}
	return nil
}

// Reclaimed describes one orphaned worktree that GC removed.
type Reclaimed struct {
	// Path is the worktree directory GC removed.
	Path string
	// Branch is the worktree's checked-out branch, when git reported one.
	Branch string
	// Reason is why GC reclaimed it — human-readable, for telemetry.
	Reason string
}

// GC finds and removes worktrees of targetRepo that were orphaned by crashed
// workers, returning what it reclaimed (spec 070 FR-014).
//
// A registered git worktree under the Manager root is reclaimed iff EITHER:
//
//   - it has no matching active record — the Manager never created it, or a
//     crash lost the record; or
//   - its active record is older than the Manager's GC threshold — a worker
//     that should have finished long ago never tore its worktree down.
//
// The primary checkout and any worktree NOT under the Manager root are never
// touched — GC only reclaims worktrees it owns. A worktree with a young,
// matched record is left alone: a live worker may still be using it. GC is
// therefore safe to run on a schedule against a busy repository.
//
// The returned slice is sorted by path so the result is deterministic.
func (m *Manager) GC(targetRepo string) ([]Reclaimed, error) {
	if targetRepo == "" {
		return nil, errors.New("worktree: target repo must not be empty")
	}
	repoAbs, err := filepath.Abs(targetRepo)
	if err != nil {
		return nil, fmt.Errorf("worktree: resolving target repo %q: %w", targetRepo, err)
	}

	worktrees, err := listWorktrees(repoAbs)
	if err != nil {
		return nil, fmt.Errorf("worktree: listing worktrees of %q: %w", targetRepo, err)
	}

	m.mu.Lock()
	active, err := m.readActive()
	m.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("worktree: reading active registry: %w", err)
	}
	// Index active records by absolute path for O(1) lookup.
	activeByPath := make(map[string]record, len(active))
	for _, r := range active {
		activeByPath[r.Path] = r
	}

	cutoff := m.now().Add(-m.gcThreshold)

	var reclaimed []Reclaimed
	for _, w := range worktrees {
		// Only reclaim worktrees the Manager owns — those under its root.
		// The primary checkout and foreign worktrees are never candidates.
		if !underRoot(m.root, w.Path) {
			continue
		}
		rec, tracked := activeByPath[w.Path]
		var reason string
		switch {
		case !tracked:
			reason = "orphaned: no active record (worker crashed before registration, or record lost)"
		case rec.CreatedAt.Before(cutoff):
			reason = fmt.Sprintf("orphaned: active record older than %s threshold", m.gcThreshold)
		default:
			continue // young, tracked — a live worker may still hold it.
		}

		// Tear it down. Teardown is idempotent and also clears any active
		// record, so the registry is left consistent.
		if err := m.Teardown(w.Path); err != nil {
			return reclaimed, fmt.Errorf("worktree: reclaiming orphan %q: %w", w.Path, err)
		}
		reclaimed = append(reclaimed, Reclaimed{Path: w.Path, Branch: w.Branch, Reason: reason})
	}

	sort.Slice(reclaimed, func(i, j int) bool { return reclaimed[i].Path < reclaimed[j].Path })
	return reclaimed, nil
}

// --- active registry --------------------------------------------------------

// registryPath is the file under the Manager root holding the active-worktree
// records as a JSON array.
func (m *Manager) registryPath() string {
	return filepath.Join(m.root, ".active-worktrees.json")
}

// readActive returns the active records, or an empty slice if the registry
// does not yet exist. The caller must hold mu (GC reads it under its own lock).
func (m *Manager) readActive() ([]record, error) {
	data, err := os.ReadFile(m.registryPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var recs []record
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("decoding registry: %w", err)
	}
	return recs, nil
}

// writeActive atomically replaces the registry with recs. The caller must
// hold mu.
func (m *Manager) writeActive(recs []record) error {
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding registry: %w", err)
	}
	tmp := m.registryPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.registryPath())
}

// addActive appends rec to the registry under the Manager lock.
func (m *Manager) addActive(rec record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	recs, err := m.readActive()
	if err != nil {
		return err
	}
	recs = append(recs, rec)
	return m.writeActive(recs)
}

// takeActive atomically removes the registry record for path and returns it.
// The boolean reports whether a record existed: Teardown reads the returned
// record's branch and base SHA to decide whether the worktree's branch is
// empty litter to delete. Taking a path that is not registered is a no-op that
// returns tracked=false — Teardown idempotency depends on it.
func (m *Manager) takeActive(path string) (rec record, tracked bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	recs, err := m.readActive()
	if err != nil {
		return record{}, false, err
	}
	kept := recs[:0]
	for _, r := range recs {
		if r.Path == path && !tracked {
			rec, tracked = r, true
			continue
		}
		kept = append(kept, r)
	}
	if err := m.writeActive(kept); err != nil {
		return record{}, false, err
	}
	return rec, tracked, nil
}

// --- git plumbing -----------------------------------------------------------

// worktreeInfo is one entry from `git worktree list --porcelain`.
type worktreeInfo struct {
	// Path is the absolute worktree directory.
	Path string
	// Branch is the checked-out branch (refs/heads/... stripped), or empty
	// for a detached HEAD.
	Branch string
}

// listWorktrees returns every worktree registered for the repository at
// repoDir, parsed from `git worktree list --porcelain`. The primary checkout
// is included in git's output; callers filter it by root.
func listWorktrees(repoDir string) ([]worktreeInfo, error) {
	out, err := runGit(repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var infos []worktreeInfo
	var cur worktreeInfo
	flush := func() {
		if cur.Path != "" {
			infos = append(infos, cur)
		}
		cur = worktreeInfo{}
	}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush() // a new record begins — emit the previous one.
			p := strings.TrimPrefix(line, "worktree ")
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
			cur.Path = p
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush() // emit the final record.
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parsing worktree list: %w", err)
	}
	return infos, nil
}

// resolveRepo canonicalizes a target-repo path. It returns toplevel, the
// repository's main working-tree toplevel (the directory `git worktree add`
// is run from), and lockKey, the stable per-repo lock key — the parent of the
// shared .git directory. For a normal repository these are equal; resolving
// both explicitly makes the lock key identical to the one Teardown derives
// from an existing worktree, so Create and Teardown serialize correctly.
//
// It returns an error if path is not inside a git repository.
func resolveRepo(path string) (toplevel, lockKey string, err error) {
	toplevel, err = runGit(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	common, err := runGit(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", "", err
	}
	return toplevel, filepath.Dir(common), nil
}

// repoForWorktree inspects the git worktree at path and returns owner, the
// toplevel of the OWNING repository — the main worktree that holds the shared
// .git/worktrees metadata. owner serves two roles in Teardown: it is the
// per-repo lock key (so Teardown's git mutations serialize against Create's),
// and it is the working directory every Teardown git command runs from. The
// owner toplevel outlives the worktree, so a `prune`, `rev-parse`, or
// `branch -D` issued after the worktree directory is removed still has a valid
// directory to run in — running them from the worktree path itself would fail
// once that path is gone.
//
// It returns an error if path does not exist or is not a git worktree — the
// signal Teardown uses to no-op idempotently.
func repoForWorktree(path string) (owner string, err error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	if _, err := runGit(path, "rev-parse", "--is-inside-work-tree"); err != nil {
		return "", fmt.Errorf("%q is not a git worktree: %w", path, err)
	}
	// --git-common-dir is the SHARED .git directory; its parent is the main
	// worktree's toplevel — the owning repository.
	common, err := runGit(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("resolving common dir of %q: %w", path, err)
	}
	return filepath.Dir(common), nil
}

// runGit runs `git <args...>` with dir as the working directory and returns
// its trimmed stdout. A non-zero exit yields an error carrying git's stderr.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// --- helpers ----------------------------------------------------------------

// underRoot reports whether path lies inside root — the test that decides
// whether GC may reclaim a worktree. Both are resolved to absolute paths; a
// path equal to root itself is not "under" it.
func underRoot(root, path string) bool {
	ra, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pa, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(ra, pa)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}

// randomSuffix returns 8 hex characters of cryptographic randomness — the
// uniqueness guarantee that lets two concurrent Create calls for the same
// work unit get distinct directories and branches.
func randomSuffix() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// sanitize reduces a work unit ID to a filesystem- and ref-safe slug:
// lowercase alphanumerics and hyphens only. An ID that sanitizes to empty
// becomes "unit" so a directory and branch name can still be formed.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '/' || r == ' ':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unit"
	}
	return out
}
