package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// SpecIterationInput is the typed input to SpecIterationWorkflow — one
// Copilot review (or lint pass) on a chitin-authored spec pull request that
// the factory will iterate against (spec 115 US1). One workflow runs per
// review; deterministic WorkflowID
// `spec-iteration-pr-<N>-review-<M>` lets duplicate webhook deliveries
// dedup via Temporal REJECT_DUPLICATE.
//
// Unlike PRIterationWorkflow (spec 113), the spec workflow does NOT take a
// DriverID: spec PRs route to the `spec.author` capability set (FR-005)
// rather than re-invoking the authoring driver, so the workflow selects a
// driver itself via the SelectDriver activity.
type SpecIterationInput struct {
	// PRNumber is the chitin-authored spec pull request.
	PRNumber int `json:"pr_number"`
	// PRBranch is the head branch of the pull request.
	PRBranch string `json:"pr_branch"`
	// TargetRepo is the absolute path the worktree Manager mints the
	// dedicated checkout under (reuses spec 112 US2's Checkout).
	TargetRepo string `json:"target_repo"`
	// Repo is the GitHub owner/name pair used to fetch review context.
	Repo string `json:"repo"`
	// ReviewID is the GitHub review id triggering this iteration.
	ReviewID int64 `json:"review_id"`
	// SpecDir is the `.specify/specs/NNN-*/` directory the PR touches —
	// used by the spec-tuned prompt to scope full-context inclusion
	// (FR-006). Empty is acceptable; the activity falls back to deriving
	// it from the PR's changed files.
	SpecDir string `json:"spec_dir,omitempty"`
}

// SpecIterationResult is the typed outcome of one spec-iteration round.
// Mirrors the shape PRIterationResult exposes plus the spec-specific
// `action_counts` field (FR-009 / T021).
type SpecIterationResult struct {
	PushedFixup  bool                       `json:"pushed_fixup"`
	FixupSHA     string                     `json:"fixup_sha"`
	CommentCount int                        `json:"comment_count"`
	ActionCounts SpecIterationActionCounts  `json:"action_counts"`
	DriverID     string                     `json:"driver_id"`
	Explanation  string                     `json:"explanation"`
}

// SpecIterationActionCounts is the closed-set tally of how the driver
// disposed of each incoming comment + lint violation (FR-009). `lint_fix`
// counts spec changes that addressed a linter violation; `fix` counts
// changes addressing a Copilot comment; `reply` counts comments answered
// with a justification rather than a code change; `skip` counts comments
// the driver explicitly declined.
type SpecIterationActionCounts struct {
	Fix     int `json:"fix"`
	Reply   int `json:"reply"`
	Skip    int `json:"skip"`
	LintFix int `json:"lint_fix"`
}

// specIterationActivityTimeout bounds the IterateSpecReview activity. The
// driver invocation inside it (claudecode / codex with the spec-tuned
// prompt) is the long leg; the outer cap matches PR iteration (spec 113)
// so a stuck driver is killed by the same mechanism that bounds regular
// dispatches.
const specIterationActivityTimeout = 2 * time.Hour

// specIterationSelectTimeout bounds the SelectDriver activity — matches
// the scheduler's selectActivityTimeout for consistency.
const specIterationSelectTimeout = 1 * time.Minute

// iterateSpecReviewInput is the wire-format struct passed to the
// IterateSpecReview activity (implemented in a later task). Kept private
// to this file so T011 lands without touching the activities package; the
// activity, when added, must accept a structurally-compatible struct
// (Temporal serializes JSON over the wire).
type iterateSpecReviewInput struct {
	PRNumber   int    `json:"pr_number"`
	PRBranch   string `json:"pr_branch"`
	TargetRepo string `json:"target_repo"`
	Repo       string `json:"repo"`
	ReviewID   int64  `json:"review_id"`
	Round      int    `json:"round"`
	DriverID   string `json:"driver_id"`
	SpecDir    string `json:"spec_dir"`
	WorkUnitID string `json:"work_unit_id"`
}

// iterateSpecReviewResult is the wire-format struct returned by the
// IterateSpecReview activity. Same private-by-design rationale as the
// input above.
type iterateSpecReviewResult struct {
	PushedFixup  bool                      `json:"pushed_fixup"`
	FixupSHA     string                    `json:"fixup_sha"`
	CommentCount int                       `json:"comment_count"`
	ActionCounts SpecIterationActionCounts `json:"action_counts"`
	Explanation  string                    `json:"explanation"`
}

// SpecIterationWorkflow is the spec 115 US1 per-review iteration workflow.
// It mirrors PRIterationWorkflow (spec 113 T004) in shape — input
// validation, one activity dispatch, typed result — and differs in two
// ways:
//
//   1. The workflow selects a driver itself via the SelectDriver activity
//      with capability `spec.author` (FR-005) rather than re-invoking the
//      authoring driver. The selected driver id is carried into the
//      activity input and echoed on the result for telemetry.
//   2. The iteration activity (IterateSpecReview, future task) builds a
//      spec-tuned prompt that includes the full current spec.md +
//      tasks.md and the linter's violations (FR-006). Worktree minting
//      reuses spec 112 US2's worktree.Manager.Checkout exactly the same
//      way IteratePRReview does.
//
// v1 cap: ONE round per review. Subsequent Copilot reviews on the same
// spec PR produce fresh workflows with fresh ReviewIDs. Multi-round
// classification + escalation routing lands in T014.
//
// Determinism: workflow code is side-effect-free (two activity calls, no
// time, no signals, no children). Replay-stable by construction; the
// SelectDriver result is recorded in history on first execution and
// re-read on replay rather than re-probing the registry.
func SpecIterationWorkflow(ctx workflow.Context, in SpecIterationInput) (SpecIterationResult, error) {
	logger := workflow.GetLogger(ctx)

	if in.PRNumber <= 0 || in.PRBranch == "" || in.TargetRepo == "" || in.Repo == "" {
		return SpecIterationResult{}, temporal.NewNonRetryableApplicationError(
			"spec-iteration: PRNumber, PRBranch, TargetRepo, and Repo are required",
			"InvalidSpecIterationInput", nil)
	}
	if in.ReviewID <= 0 {
		return SpecIterationResult{}, temporal.NewNonRetryableApplicationError(
			"spec-iteration: ReviewID is required",
			"InvalidSpecIterationInput", nil)
	}

	workUnitID := fmt.Sprintf("iterate-spec-pr-%d-review-%d", in.PRNumber, in.ReviewID)

	// Select a driver from the `spec.author` capability set. SelectDriver
	// is a side effect (Ready probes), so it MUST run in an activity.
	selectCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: specIterationSelectTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	var sel activities.SelectDriverResult
	if err := workflow.ExecuteActivity(selectCtx, "SelectDriver", activities.SelectDriverInput{
		NodeID:     workUnitID,
		Capability: string(driver.CapSpecAuthor),
	}).Get(selectCtx, &sel); err != nil {
		logger.Error("spec-iteration: SelectDriver faulted",
			"pr", in.PRNumber, "review", in.ReviewID, "err", err)
		return SpecIterationResult{
			Explanation: fmt.Sprintf("driver selection faulted: %v", err),
		}, err
	}
	if sel.Unroutable {
		// No `spec.author`-capable driver is ready. Surface as a non-error
		// settled outcome — the dispatcher (T015) can emit
		// `spec_iteration_escalated { reason: "lint_violation_unresolvable" }`
		// or similar based on the empty DriverID.
		return SpecIterationResult{
			Explanation: fmt.Sprintf(
				"no ready driver for capability %q: %s",
				driver.CapSpecAuthor, sel.Reason),
		}, nil
	}

	actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: specIterationActivityTimeout,
		// Intentionally NO HeartbeatTimeout — same rationale as
		// PRIterationWorkflow (spec 113): the activity doesn't heartbeat
		// and a real driver invocation (claudecode / codex) regularly runs
		// 5-10 minutes; StartToCloseTimeout (2h) bounds the long leg.
		RetryPolicy: &temporal.RetryPolicy{
			// The activity folds every outcome into its result and never
			// returns a Temporal error; a retry would re-run the driver
			// against the same review and produce the same answer.
			MaximumAttempts: 1,
		},
	})

	actIn := iterateSpecReviewInput{
		PRNumber:   in.PRNumber,
		PRBranch:   in.PRBranch,
		TargetRepo: in.TargetRepo,
		Repo:       in.Repo,
		ReviewID:   in.ReviewID,
		Round:      1, // v1: one round per review
		DriverID:   sel.DriverID,
		SpecDir:    in.SpecDir,
		WorkUnitID: workUnitID,
	}

	var actRes iterateSpecReviewResult
	if err := workflow.ExecuteActivity(actCtx, "IterateSpecReview", actIn).Get(ctx, &actRes); err != nil {
		logger.Error("spec-iteration: activity faulted",
			"pr", in.PRNumber, "review", in.ReviewID, "driver", sel.DriverID, "err", err)
		return SpecIterationResult{
			DriverID:    sel.DriverID,
			Explanation: fmt.Sprintf("iteration activity faulted: %v", err),
		}, err
	}

	return SpecIterationResult{
		PushedFixup:  actRes.PushedFixup,
		FixupSHA:     actRes.FixupSHA,
		CommentCount: actRes.CommentCount,
		ActionCounts: actRes.ActionCounts,
		DriverID:     sel.DriverID,
		Explanation:  actRes.Explanation,
	}, nil
}
