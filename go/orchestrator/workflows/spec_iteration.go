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
// Copilot review on a chitin-class spec pull request that the factory
// will iterate against (spec 115 US1). One workflow runs per review;
// deterministic WorkflowID `spec-iteration-pr-<N>-review-<M>` (set at
// dispatch — see T015) lets duplicate webhook deliveries dedup via
// Temporal REJECT_DUPLICATE.
//
// Unlike spec 113's PRIterationInput, this workflow does NOT carry a
// DriverID: spec PRs are not authored by the factory, so there is no
// "re-invoke the authoring driver" semantics. The workflow selects a
// driver from the `spec.author` capability set at runtime via the
// SelectDriver activity (FR-005).
type SpecIterationInput struct {
	// PRNumber is the spec pull request being iterated.
	PRNumber int `json:"pr_number"`
	// PRBranch is the head branch of the spec PR — the rebase target the
	// downstream activity will check out via worktree.Manager.Checkout
	// (reuses spec 112 US2).
	PRBranch string `json:"pr_branch"`
	// TargetRepo is the absolute path the worktree Manager mints the
	// dedicated checkout under.
	TargetRepo string `json:"target_repo"`
	// Repo is the GitHub owner/name pair used to fetch review context.
	Repo string `json:"repo"`
	// ReviewID is the GitHub review id triggering this iteration.
	ReviewID int64 `json:"review_id"`
	// SpecDir is the `.specify/specs/NNN-*` directory the spec PR
	// touches; carried through so the activity can locate spec.md +
	// tasks.md for the FR-006 prompt context and the FR-003 lint
	// violations input.
	SpecDir string `json:"spec_dir"`
}

// SpecIterationResult mirrors activities.IterateSpecReviewResult so
// callers see a self-describing typed output without importing the
// activities package. The DriverID is surfaced so the dispatcher and
// telemetry can record which spec-author driver handled the round.
type SpecIterationResult struct {
	// PushedFixup is true iff the driver produced changes that were
	// committed and force-pushed to the spec PR branch.
	PushedFixup bool `json:"pushed_fixup"`
	// FixupSHA is the new HEAD SHA after the force-push; empty when
	// PushedFixup is false.
	FixupSHA string `json:"fixup_sha"`
	// CommentCount is how many line comments the iteration saw on the
	// review; informational, used for telemetry (FR-009).
	CommentCount int `json:"comment_count"`
	// DriverID is the driver chosen by SelectDriver; empty when the
	// workflow short-circuited before driver selection (e.g.
	// unroutable).
	DriverID string `json:"driver_id"`
	// Unroutable is true when SelectDriver could not find a ready
	// driver for the `spec.author` capability; the dispatcher should
	// treat the round as a no-op and escalate.
	Unroutable bool `json:"unroutable"`
	// Explanation is a human-readable account of what happened.
	Explanation string `json:"explanation"`
}

// specIterationActivityTimeout bounds the IterateSpecReview activity.
// The driver invocation inside it (claudecode / codex with the spec-
// tuned prompt) is the long leg; the cap matches PRIteration's so a
// stuck spec-author driver is killed by the same mechanism.
const specIterationActivityTimeout = 2 * time.Hour

// specSelectDriverTimeout bounds the SelectDriver activity — fast
// in-memory registry lookup plus a driver Ready probe.
const specSelectDriverTimeout = 1 * time.Minute

// SpecIterationWorkflow is the spec 115 US1 per-review iteration
// workflow for spec PRs. It mirrors spec 113's PRIterationWorkflow
// shape (single iteration activity, deterministic WorkflowID set at
// dispatch, MaxAttempts=1 retry policy, no in-flight signals) but
// differs in two ways grounded in FR-005:
//
//  1. Driver is selected at runtime via SelectDriver against the
//     `spec.author` capability instead of being carried in input. A
//     spec PR has no "authoring driver" to reuse — the factory did not
//     write it — so capability routing is the source of truth.
//  2. The IterateSpecReview activity (the long leg) uses spec 112 US2's
//     `worktree.Manager.Checkout` for the spec-PR branch and reads
//     spec.md + tasks.md from SpecDir to assemble the FR-006 prompt
//     context. Building that prompt is T012's helper; classifier-
//     driven partitioning of mechanical vs design-judgement comments
//     is T014's wiring; FR-009 chain events are emitted by T016.
//
// v1 cap: ONE round per review, same as spec 113. Subsequent Copilot
// reviews on the same spec PR produce fresh workflows with fresh
// ReviewIDs (and fresh WorkflowIDs); the deterministic ID dedups
// within a single review only.
//
// Determinism: workflow code is side-effect-free (two sequential
// activity calls, no time, no signals, no children). Replay-stable by
// construction — SelectDriver's result is recorded in history so a
// replay reads it back rather than re-probing.
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
	if in.SpecDir == "" {
		return SpecIterationResult{}, temporal.NewNonRetryableApplicationError(
			"spec-iteration: SpecDir is required — the activity reads spec.md + tasks.md from it",
			"InvalidSpecIterationInput", nil)
	}

	workUnitID := fmt.Sprintf("iterate-spec-%d-review-%d", in.PRNumber, in.ReviewID)

	// Driver selection runs as an activity because the registry's
	// Select probes each candidate driver's Ready callback (live I/O).
	// A short retry policy covers transient driver health flakes; an
	// Unroutable verdict is folded into the result, not retried.
	selectCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: specSelectDriverTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	var sel activities.SelectDriverResult
	if err := workflow.ExecuteActivity(selectCtx, "SelectDriver", activities.SelectDriverInput{
		NodeID:     workUnitID,
		Capability: string(driver.CapSpecAuthor),
	}).Get(ctx, &sel); err != nil {
		logger.Error("spec-iteration: SelectDriver faulted",
			"pr", in.PRNumber, "review", in.ReviewID, "err", err)
		return SpecIterationResult{
			Explanation: fmt.Sprintf("driver selection faulted: %v", err),
		}, err
	}
	if sel.Unroutable {
		logger.Info("spec-iteration: no spec-author driver ready",
			"pr", in.PRNumber, "review", in.ReviewID, "reason", sel.Reason)
		return SpecIterationResult{
			Unroutable:  true,
			Explanation: sel.Reason,
		}, nil
	}

	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: specIterationActivityTimeout,
		// Intentionally NO HeartbeatTimeout — same reasoning as
		// PRIteration: claudecode/codex regularly runs 5-10 minutes and
		// the activity does not call RecordHeartbeat.
		RetryPolicy: &temporal.RetryPolicy{
			// The activity folds every outcome (success, no-changes,
			// push failure, fetch failure) into the result and returns
			// nil error, so a retry would re-run the driver against
			// the same review and produce the same answer.
			MaximumAttempts: 1,
		},
	})

	actIn := activities.IterateSpecReviewInput{
		PRNumber:   in.PRNumber,
		PRBranch:   in.PRBranch,
		TargetRepo: in.TargetRepo,
		Repo:       in.Repo,
		ReviewID:   in.ReviewID,
		SpecDir:    in.SpecDir,
		Round:      1, // v1: one round per review
		DriverID:   sel.DriverID,
		WorkUnitID: workUnitID,
	}

	var actRes activities.IterateSpecReviewResult
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
