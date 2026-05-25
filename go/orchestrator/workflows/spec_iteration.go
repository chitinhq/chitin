package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// SpecLintViolation is one finding from the spec-lint subcommand (spec 115
// FR-003 rules L01-L07). The workflow carries the linter's findings into the
// iteration activity so the driver sees them as structured input alongside
// the Copilot review (FR-006).
//
// Field shape matches spec-lint's stdout JSON contract (T002) so the wire
// payload round-trips cleanly between the dispatcher (T015), the workflow,
// and the activity.
type SpecLintViolation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// SpecIterationInput is the typed input to SpecIterationWorkflow — one
// Copilot review on a spec PR (spec 115 US1). Mirrors PRIterationInput
// (spec 113) except the driver is selected from the spec-author capability
// pool inside the workflow rather than carried in (a spec PR can come from
// any author, so there is no "authoring driver" to re-invoke).
//
// Deterministic WorkflowID `spec-iteration-pr-<N>-review-<M>` (set by the
// dispatcher, T015) lets duplicate webhook deliveries dedup via Temporal
// REJECT_DUPLICATE.
type SpecIterationInput struct {
	// PRNumber is the spec pull request being iterated.
	PRNumber int `json:"pr_number"`
	// PRBranch is the head branch of the PR — the rebase target.
	PRBranch string `json:"pr_branch"`
	// TargetRepo is the absolute path the worktree Manager mints the
	// dedicated checkout under (reuses spec 112 US2's Checkout).
	TargetRepo string `json:"target_repo"`
	// Repo is the GitHub owner/name pair used to fetch review context.
	Repo string `json:"repo"`
	// ReviewID is the GitHub review id triggering this iteration.
	ReviewID int64 `json:"review_id"`
	// LintViolations is the spec-lint output the dispatcher gathered before
	// firing this workflow (FR-004). Carried through to the activity so the
	// driver prompt (T012 BuildSpecIterationPrompt) can present them as
	// distinct from Copilot's comments (FR-006).
	LintViolations []SpecLintViolation `json:"lint_violations,omitempty"`
}

// SpecIterationResult mirrors the activity's outcome. PushedFixup/FixupSHA/
// CommentCount/Explanation match PRIterationResult's shape; DriverID and
// Unroutable are added so the dispatcher can record which driver ran (or why
// none did) without having to introspect the chain event.
type SpecIterationResult struct {
	PushedFixup  bool   `json:"pushed_fixup"`
	FixupSHA     string `json:"fixup_sha"`
	CommentCount int    `json:"comment_count"`
	DriverID     string `json:"driver_id"`
	Unroutable   bool   `json:"unroutable"`
	Explanation  string `json:"explanation"`
}

const (
	// specIterationSelectTimeout bounds the SelectDriver activity — a fast
	// in-memory registry lookup plus driver Ready probes. Same budget as
	// the scheduler's selectActivityTimeout.
	specIterationSelectTimeout = 1 * time.Minute
	// specIterationActivityTimeout bounds the IterateSpecReview activity.
	// The driver invocation (claudecode / codex with the spec-tuned prompt)
	// is the long leg; the outer cap matches PRIterationWorkflow's 2h
	// budget so a stuck driver is killed by the same mechanism.
	specIterationActivityTimeout = 2 * time.Hour
)

// SpecIterationWorkflow is the spec 115 US1 per-review iteration workflow
// for spec PRs. It mirrors PRIterationWorkflow's shape (spec 113 T004) with
// two structural differences (FR-005):
//
//  1. The driver is selected by capability `spec.author` rather than carried
//     in — a spec PR can come from any author, so there is no original
//     driver to re-invoke.
//  2. The activity uses the spec-tuned prompt template (T012) and folds the
//     linter's violations (FR-006) in alongside the review comments.
//
// The downstream IterateSpecReview activity mints a worktree on the PR
// branch via worktree.Manager.Checkout (spec 112 US2), invokes the selected
// driver, and force-pushes any resulting fixup. It always returns a nil
// error and folds every outcome into the result, so the workflow needs no
// retry policy beyond MaxAttempts=1.
//
// v1 cap: ONE round per review, matching PRIterationWorkflow. Subsequent
// Copilot reviews on the same PR produce fresh workflows with fresh
// ReviewIDs.
//
// Determinism: workflow code is side-effect-free (two activity calls, no
// time, no signals, no children). Replay-stable by construction.
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

	// Select a spec-author-capable driver. Selection is a side effect (it
	// probes each candidate's Ready check) so it MUST run in an activity;
	// the recorded result is replay-stable.
	sctx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: specIterationSelectTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	var sel activities.SelectDriverResult
	if err := workflow.ExecuteActivity(sctx, "SelectDriver", activities.SelectDriverInput{
		NodeID:     workUnitID,
		Capability: string(driver.CapSpecAuthor),
	}).Get(sctx, &sel); err != nil {
		logger.Error("spec-iteration: select-driver faulted",
			"pr", in.PRNumber, "review", in.ReviewID, "err", err)
		return SpecIterationResult{
			Explanation: fmt.Sprintf("select-driver activity faulted: %v", err),
		}, err
	}
	if sel.Unroutable {
		// No registered driver satisfies spec.author. Return a settled
		// result rather than an error so the dispatcher can escalate
		// cleanly instead of retrying against the same empty pool.
		return SpecIterationResult{
			Unroutable:  true,
			Explanation: sel.Reason,
		}, nil
	}

	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: specIterationActivityTimeout,
		// Intentionally NO HeartbeatTimeout. The activity does not call
		// activity.RecordHeartbeat; setting one would reliably time out
		// a real driver invocation at ~2 minutes (claudecode / codex
		// runs regularly take 5-10 minutes). StartToCloseTimeout (2h)
		// bounds the long leg; the worker host's process supervision
		// catches a truly hung subprocess. (Same rationale as
		// PRIterationWorkflow.)
		RetryPolicy: &temporal.RetryPolicy{
			// The activity encodes every outcome as a non-error result.
			// A retry would re-run the driver against the same review +
			// linter findings and produce the same answer.
			MaximumAttempts: 1,
		},
	})

	actIn := iterateSpecReviewInput{
		PRNumber:       in.PRNumber,
		PRBranch:       in.PRBranch,
		TargetRepo:     in.TargetRepo,
		Repo:           in.Repo,
		ReviewID:       in.ReviewID,
		Round:          1, // v1: one round per review
		DriverID:       sel.DriverID,
		WorkUnitID:     workUnitID,
		LintViolations: in.LintViolations,
	}

	var actRes iterateSpecReviewResult
	if err := workflow.ExecuteActivity(actx, "IterateSpecReview", actIn).Get(ctx, &actRes); err != nil {
		logger.Error("spec-iteration: activity faulted",
			"pr", in.PRNumber, "review", in.ReviewID, "err", err)
		return SpecIterationResult{
			DriverID:    sel.DriverID,
			Explanation: fmt.Sprintf("iteration activity faulted: %v", err),
		}, err
	}

	return SpecIterationResult{
		PushedFixup:  actRes.PushedFixup,
		FixupSHA:     actRes.FixupSHA,
		CommentCount: actRes.CommentCount,
		DriverID:     sel.DriverID,
		Explanation:  actRes.Explanation,
	}, nil
}

// iterateSpecReviewInput is the wire payload sent to the IterateSpecReview
// activity. Defined here while the activity is implemented in a follow-up
// task; the activity's input struct must mirror these JSON tags so payloads
// round-trip cleanly through Temporal's codec.
type iterateSpecReviewInput struct {
	PRNumber       int                 `json:"pr_number"`
	PRBranch       string              `json:"pr_branch"`
	TargetRepo     string              `json:"target_repo"`
	Repo           string              `json:"repo"`
	ReviewID       int64               `json:"review_id"`
	Round          int                 `json:"round"`
	DriverID       string              `json:"driver_id"`
	WorkUnitID     string              `json:"work_unit_id"`
	LintViolations []SpecLintViolation `json:"lint_violations,omitempty"`
}

// iterateSpecReviewResult is the wire payload the IterateSpecReview activity
// returns. Mirrors IteratePRReviewResult's shape; the activity will fold
// every outcome into a non-error result.
type iterateSpecReviewResult struct {
	PushedFixup  bool   `json:"pushed_fixup"`
	FixupSHA     string `json:"fixup_sha"`
	CommentCount int    `json:"comment_count"`
	Explanation  string `json:"explanation"`
}
