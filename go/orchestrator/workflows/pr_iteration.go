package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
)

// PRIterationInput is the typed input to PRIterationWorkflow — one Copilot
// review on a chitin-authored pull request that the factory will iterate
// against (spec 113 US1). One workflow runs per review; deterministic
// WorkflowID `iteration-pr-<N>-review-<M>` lets duplicate webhook
// deliveries dedup via Temporal REJECT_DUPLICATE.
type PRIterationInput struct {
	// PRNumber is the chitin-authored pull request.
	PRNumber int `json:"pr_number"`
	// PRBranch is the head branch of the pull request.
	PRBranch string `json:"pr_branch"`
	// TargetRepo is the absolute path the worktree Manager mints the
	// dedicated checkout under.
	TargetRepo string `json:"target_repo"`
	// Repo is the GitHub owner/name pair used to fetch review context.
	Repo string `json:"repo"`
	// ReviewID is the GitHub review id triggering this iteration.
	ReviewID int64 `json:"review_id"`
	// DriverID is the driver that authored the original PR; the iteration
	// re-invokes this same driver so fixup style matches.
	DriverID string `json:"driver_id"`
}

// PRIterationResult mirrors activities.IteratePRReviewResult so callers
// see a self-describing typed output without importing the activities
// package.
type PRIterationResult struct {
	PushedFixup  bool   `json:"pushed_fixup"`
	FixupSHA     string `json:"fixup_sha"`
	CommentCount int    `json:"comment_count"`
	Explanation  string `json:"explanation"`
}

// prIterationActivityTimeout bounds the IteratePRReview activity. The
// driver invocation inside it (claudecode / codex) is the long leg; the
// outer cap matches the work-unit invoke timeout (2h) so a stuck driver
// is killed by the same mechanism that bounds regular dispatches.
const prIterationActivityTimeout = 2 * time.Hour

// PRIterationWorkflow is the spec 113 US1 per-review iteration workflow.
// It invokes the IteratePRReview activity exactly once per round; the
// activity itself folds every outcome (success, no-changes, push failure,
// fetch failure) into the result and never returns a Temporal error, so
// the workflow needs no retry policy beyond MaxAttempts=1.
//
// v1 cap: ONE round per review. Subsequent Copilot reviews on the same PR
// produce fresh workflows with fresh ReviewIDs (and fresh WorkflowIDs);
// the deterministic ID dedups within a single review only. This trades
// iteration depth for simplicity in the MVP; the multi-round cap from
// spec 113 FR-007 lands in a follow-up.
//
// Determinism: workflow code is side-effect-free (single activity call,
// no time, no signals, no children). Replay-stable by construction.
func PRIterationWorkflow(ctx workflow.Context, in PRIterationInput) (PRIterationResult, error) {
	logger := workflow.GetLogger(ctx)

	if in.PRNumber <= 0 || in.PRBranch == "" || in.TargetRepo == "" || in.Repo == "" {
		return PRIterationResult{}, temporal.NewNonRetryableApplicationError(
			"pr-iteration: PRNumber, PRBranch, TargetRepo, and Repo are required",
			"InvalidPRIterationInput", nil)
	}
	if in.DriverID == "" {
		return PRIterationResult{}, temporal.NewNonRetryableApplicationError(
			"pr-iteration: DriverID is required (the iteration re-invokes the authoring driver)",
			"InvalidPRIterationInput", nil)
	}
	if in.ReviewID <= 0 {
		return PRIterationResult{}, temporal.NewNonRetryableApplicationError(
			"pr-iteration: ReviewID is required",
			"InvalidPRIterationInput", nil)
	}

	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: prIterationActivityTimeout,
		// Intentionally NO HeartbeatTimeout. The activity does not call
		// activity.RecordHeartbeat; setting one would reliably time out a
		// real driver invocation at ~2 minutes (claudecode / codex runs
		// regularly take 5-10 minutes). StartToCloseTimeout (2h) bounds
		// the long leg; the worker host's process supervision catches a
		// truly hung subprocess.
		RetryPolicy: &temporal.RetryPolicy{
			// The activity already encodes every outcome as a non-error
			// result. A retry would re-run the driver against the same
			// review and produce the same answer.
			MaximumAttempts: 1,
		},
	})

	workUnitID := fmt.Sprintf("iterate-pr-%d-review-%d", in.PRNumber, in.ReviewID)
	actIn := activities.IteratePRReviewInput{
		PRNumber:   in.PRNumber,
		PRBranch:   in.PRBranch,
		TargetRepo: in.TargetRepo,
		Repo:       in.Repo,
		ReviewID:   in.ReviewID,
		Round:      1, // v1: one round per review
		DriverID:   in.DriverID,
		WorkUnitID: workUnitID,
	}

	var actRes activities.IteratePRReviewResult
	if err := workflow.ExecuteActivity(actx, "IteratePRReview", actIn).Get(ctx, &actRes); err != nil {
		logger.Error("pr-iteration: activity faulted",
			"pr", in.PRNumber, "review", in.ReviewID, "err", err)
		return PRIterationResult{
			Explanation: fmt.Sprintf("iteration activity faulted: %v", err),
		}, err
	}

	return PRIterationResult{
		PushedFixup:  actRes.PushedFixup,
		FixupSHA:     actRes.FixupSHA,
		CommentCount: actRes.CommentCount,
		Explanation:  actRes.Explanation,
	}, nil
}
