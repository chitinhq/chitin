package activities

import (
	"context"
	"fmt"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/worktree"
)

// IterateSpecReviewInput is the typed input to the IterateSpecReview
// activity — one spec PR that received a Copilot review (spec 115 US1).
// The activity mints a worktree on the PR branch via
// worktree.Manager.Checkout (spec 112 US2), fetches the review context
// + the linter's violations (FR-006), invokes a spec-author driver
// against the worktree with the spec-tuned prompt (T012), commits any
// fixups, and emits the FR-009 chain events (T016).
type IterateSpecReviewInput struct {
	// PRNumber is the spec pull request being iterated.
	PRNumber int `json:"pr_number"`
	// PRBranch is the head branch of the spec PR — the rebase target.
	PRBranch string `json:"pr_branch"`
	// TargetRepo is the absolute path the worktree Manager mints the
	// dedicated checkout under (spec 112 US2's Manager.Checkout).
	TargetRepo string `json:"target_repo"`
	// Repo is the GitHub owner/name (e.g. "chitinhq/chitin") used to
	// fetch the review body + line comments via gh api.
	Repo string `json:"repo"`
	// ReviewID is the GitHub review id whose comments are being
	// addressed. Carried into chain events for correlation.
	ReviewID int64 `json:"review_id"`
	// SpecDir is the `.specify/specs/NNN-*` directory the spec PR
	// modifies. The activity reads spec.md + tasks.md from it for the
	// FR-006 prompt context.
	SpecDir string `json:"spec_dir"`
	// Round is the 1-based iteration round number (v1: always 1).
	Round int `json:"round"`
	// DriverID is the spec-author driver chosen by the workflow's
	// SelectDriver step.
	DriverID string `json:"driver_id"`
	// WorkUnitID is the orchestration handle for this iteration round;
	// used to slug the worktree directory.
	WorkUnitID string `json:"work_unit_id"`
}

// IterateSpecReviewResult is the typed outcome of one spec-iteration
// round. Like IteratePRReviewResult, the activity always returns a nil
// error and folds every outcome (driver success, no-op, push failure,
// fetch failure, escalation per FR-008) into the result so the
// workflow settles cleanly without blind retries.
type IterateSpecReviewResult struct {
	// PushedFixup is true iff the driver produced changes that were
	// committed and force-pushed.
	PushedFixup bool `json:"pushed_fixup"`
	// FixupSHA is the new HEAD SHA after the force-push; empty when
	// PushedFixup is false.
	FixupSHA string `json:"fixup_sha"`
	// CommentCount is how many line comments the iteration saw on the
	// review; informational, used for telemetry (FR-009).
	CommentCount int `json:"comment_count"`
	// Escalated is true when FR-008 classification fired (all comments
	// were design-judgement) and the round emitted
	// `spec_iteration_escalated` without dispatching the driver.
	Escalated bool `json:"escalated"`
	// EscalationReason is the closed-set FR-010 reason that
	// accompanied an escalation (e.g.
	// `design_judgement_required`); empty when Escalated is false.
	EscalationReason string `json:"escalation_reason"`
	// Explanation is a human-readable account of what happened.
	Explanation string `json:"explanation"`
}

// IterateSpecReview is the spec 115 US1 spec-PR iteration activity. On
// a spec PR that received a Copilot review, this activity is the
// analog of IteratePRReview but tuned for spec content:
//
//  1. Mints a worktree on the spec PR branch via
//     worktree.Manager.Checkout (reuses spec 112 US2's infra — same
//     entry point IteratePRReview uses).
//  2. Reads spec.md + tasks.md from SpecDir for the FR-006 prompt
//     context.
//  3. Reads the linter's violations (FR-003 output) from the spec PR's
//     review-comment surface, distinguishing them from Copilot's
//     comments.
//  4. Partitions Copilot comments via T013's ClassifyDesignJudgement
//     into mechanical vs design-judgement (T014 wiring); if all are
//     judgement, emits `spec_iteration_escalated` and returns without
//     invoking the driver.
//  5. Otherwise builds the spec-iteration prompt (T012's
//     BuildSpecIterationPrompt) and invokes the spec-author driver
//     against the worktree.
//  6. Commits + force-with-lease pushes any fixup.
//  7. Emits the FR-009 chain events via T016's
//     EmitSpecIterationTelemetry.
//
// Worktree is reclaimed via deferred Teardown regardless of outcome.
// Like IteratePRReview, the activity is engineered to return a nil
// error for every outcome — push failure, driver no-op, gh api fault
// all fold into the result so the workflow doesn't retry blindly.
//
// Note: T011 (this task) only wires the activity contract — the
// Execute body is the union of T012 + T014 + T016 and lands in those
// tasks. Today the activity is registerable on the worker host but
// returns a documented not-yet-implemented result so the workflow can
// be exercised end-to-end in tests via TestActivityEnvironment stubs
// (T021).
type IterateSpecReview struct {
	manager  *worktree.Manager
	registry *driver.Registry
}

// NewIterateSpecReview returns an IterateSpecReview activity bound to
// mgr + the driver registry. Both are required at runtime even though
// the placeholder Execute does not yet exercise them — the contract
// matches IteratePRReview so wiring at worker-host startup is uniform.
func NewIterateSpecReview(mgr *worktree.Manager, reg *driver.Registry) *IterateSpecReview {
	return &IterateSpecReview{manager: mgr, registry: reg}
}

// ActivityName is the stable Temporal activity name.
func (a *IterateSpecReview) ActivityName() string { return "IterateSpecReview" }

// Execute is the activity entrypoint. Always returns a nil error.
//
// T011 placeholder: the worktree+driver+commit+push pipeline is
// implemented across T012 (prompt builder), T014 (classifier wiring),
// and T016 (telemetry emission). Until those land, Execute records a
// no-op outcome so the workflow's two-activity shape is exercisable
// end-to-end. The activity contract (input/output types, nil-error
// fold) is locked in here so the workflow can settle on the typed
// surface tests will mock.
func (a *IterateSpecReview) Execute(_ context.Context, in IterateSpecReviewInput) (IterateSpecReviewResult, error) {
	// Mirror IteratePRReview's wiring guards so a misconfigured worker
	// host (registry/manager not bound) surfaces in the result rather
	// than silently appearing as a "not yet implemented" no-op.
	if a.manager == nil || a.registry == nil {
		return IterateSpecReviewResult{
			Explanation: "no Manager or Registry bound — spec iteration not attempted",
		}, nil
	}
	// Input guards: same closed set the workflow validates, restated at
	// the activity boundary so misconfigured callers (tests, future
	// dispatchers) get a populated Explanation instead of a partial-state
	// run when T012/T014/T016 land the body.
	if in.PRNumber <= 0 || in.PRBranch == "" || in.TargetRepo == "" || in.Repo == "" {
		return IterateSpecReviewResult{
			Explanation: "missing PRNumber, PRBranch, TargetRepo, or Repo — spec iteration not attempted",
		}, nil
	}
	if in.ReviewID <= 0 {
		return IterateSpecReviewResult{
			Explanation: "missing ReviewID — spec iteration not attempted",
		}, nil
	}
	if in.SpecDir == "" {
		return IterateSpecReviewResult{
			Explanation: "missing SpecDir — spec iteration not attempted (need spec.md + tasks.md for FR-006 prompt)",
		}, nil
	}
	if in.DriverID == "" {
		return IterateSpecReviewResult{
			Explanation: "missing DriverID — spec iteration not attempted (workflow's SelectDriver must populate)",
		}, nil
	}
	return IterateSpecReviewResult{
		Explanation: fmt.Sprintf(
			"IterateSpecReview not yet implemented (T012/T014/T016 land the body); "+
				"received pr=%d review=%d driver=%s spec_dir=%s",
			in.PRNumber, in.ReviewID, in.DriverID, in.SpecDir),
	}, nil
}
