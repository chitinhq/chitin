package activities

import (
	"context"
	"fmt"

	"github.com/chitinhq/chitin/go/orchestrator/worktree"
)

// CreateWorktreeInput is the typed input to the CreateWorktree activity — the
// repository, base ref, and work-unit id a fresh worktree is minted for
// (spec 076 FR-013).
type CreateWorktreeInput struct {
	// WorkUnitID is the work unit the worktree is created for; it is woven
	// into the worktree directory and branch names.
	WorkUnitID string `json:"work_unit_id"`
	// TargetRepo is the repository the worktree branches from. It is an
	// input, never hard-coded — the same scheduler runs work over any repo.
	TargetRepo string `json:"target_repo"`
	// BaseRef is the git ref the worktree's new branch is created at — a
	// branch, tag, or commit SHA.
	BaseRef string `json:"base_ref"`
}

// CreateWorktreeResult is the typed output of the CreateWorktree activity.
type CreateWorktreeResult struct {
	// Path is the absolute path to the freshly created, dedicated worktree.
	Path string `json:"path"`
}

// TeardownWorktreeInput is the typed input to the TeardownWorktree activity.
type TeardownWorktreeInput struct {
	// Path is the absolute worktree directory to remove. Teardown is
	// idempotent — tearing down an already-removed path is a safe no-op.
	Path string `json:"path"`
}

// Worktrees is the worktree-lifecycle activity pair (spec 076 FR-008,
// spec 070 FR-013/14). Creating and removing a git worktree shells out to
// `git worktree` — filesystem and subprocess I/O — so it MUST run in an
// activity, never in workflow code. The activity is bound to the
// orchestrator's worktree Manager at worker-host startup.
//
// The Manager guarantees a FRESH worktree per call and idempotent teardown,
// so a retried CreateWorktree never reuses an orphan and a retried
// TeardownWorktree never errors on an already-gone path — both safe under
// Temporal's at-least-once activity execution.
type Worktrees struct {
	// manager is the orchestrator's worktree Manager, constructed at startup
	// over a dedicated worktree root.
	manager *worktree.Manager
}

// NewWorktrees returns a worktree activity pair bound to mgr.
func NewWorktrees(mgr *worktree.Manager) *Worktrees {
	return &Worktrees{manager: mgr}
}

// CreateActivityName is the Temporal activity name CreateWorktree registers
// under.
func (w *Worktrees) CreateActivityName() string { return "CreateWorktree" }

// TeardownActivityName is the Temporal activity name TeardownWorktree
// registers under.
func (w *Worktrees) TeardownActivityName() string { return "TeardownWorktree" }

// CreateWorktree mints a fresh dedicated git worktree for one work unit from
// the named repo at the named base ref (spec 076 FR-013, acceptance
// scenario 1). It is the activity function registered with the Temporal
// worker.
func (w *Worktrees) CreateWorktree(_ context.Context, in CreateWorktreeInput) (CreateWorktreeResult, error) {
	if w.manager == nil {
		return CreateWorktreeResult{}, fmt.Errorf("activities: CreateWorktree has no worktree Manager bound")
	}
	path, err := w.manager.Create(in.TargetRepo, in.BaseRef, in.WorkUnitID)
	if err != nil {
		return CreateWorktreeResult{}, fmt.Errorf(
			"activities: CreateWorktree for work unit %q from %s@%s: %w",
			in.WorkUnitID, in.TargetRepo, in.BaseRef, err)
	}
	return CreateWorktreeResult{Path: path}, nil
}

// TeardownWorktree removes the dedicated worktree at the given path. It is
// the activity function registered with the Temporal worker. Teardown is
// idempotent — a second teardown, or a teardown of a worktree a crash
// already removed, returns nil.
func (w *Worktrees) TeardownWorktree(_ context.Context, in TeardownWorktreeInput) error {
	if w.manager == nil {
		return fmt.Errorf("activities: TeardownWorktree has no worktree Manager bound")
	}
	if in.Path == "" {
		// Nothing was created (e.g. CreateWorktree failed before producing a
		// path). A teardown with no path is a no-op, not an error.
		return nil
	}
	if err := w.manager.Teardown(in.Path); err != nil {
		return fmt.Errorf("activities: TeardownWorktree of %q: %w", in.Path, err)
	}
	return nil
}
