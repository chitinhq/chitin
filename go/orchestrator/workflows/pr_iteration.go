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
// package. Spec 116 adds the re-review fields populated when the iteration
// pushed a fixup AND the internal re-review path ran.
type PRIterationResult struct {
	PushedFixup  bool   `json:"pushed_fixup"`
	FixupSHA     string `json:"fixup_sha"`
	CommentCount int    `json:"comment_count"`
	Explanation  string `json:"explanation"`

	// RereviewRan is true when DispatchInternalReview was invoked (which
	// requires PushedFixup=true). When false, all the other rereview_*
	// fields are zero-valued and RereviewSkipReason carries the reason
	// the path was bypassed.
	RereviewRan bool `json:"rereview_ran"`
	// RereviewSkipReason is populated when the workflow chose not to run
	// the re-review (no fixup pushed) OR when DispatchInternalReview ran
	// but returned Skipped=true (no eligible re-reviewer).
	RereviewSkipReason string `json:"rereview_skip_reason,omitempty"`
	// RereviewVerdict is the re-reviewer's verdict enum on success.
	RereviewVerdict string `json:"rereview_verdict,omitempty"`
	// RereviewConfidence is the re-reviewer's confidence (high|medium|low).
	RereviewConfidence string `json:"rereview_confidence,omitempty"`
	// RereviewerDriver is the driver id that produced the verdict.
	RereviewerDriver string `json:"rereviewer_driver,omitempty"`
	// ReadyToMergeLabeled is true when the workflow applied the
	// chitin/ready-to-merge label (approve-shaped verdict path).
	ReadyToMergeLabeled bool `json:"ready_to_merge_labeled"`
	// Escalated is true when the workflow fired a Discord operator
	// escalation for this PR.
	Escalated bool `json:"escalated"`
	// EscalationReason names the spec-116 reason taxonomy value used
	// for the escalation when Escalated is true.
	EscalationReason string `json:"escalation_reason,omitempty"`
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

	out := PRIterationResult{
		PushedFixup:  actRes.PushedFixup,
		FixupSHA:     actRes.FixupSHA,
		CommentCount: actRes.CommentCount,
		Explanation:  actRes.Explanation,
	}

	// Spec 116 US1: if the iteration produced a fixup, dispatch the
	// internal re-review chain (different driver validates the fixup,
	// then label / escalate based on the verdict). Skip when no fixup
	// was pushed — nothing to re-review.
	if !actRes.PushedFixup {
		out.RereviewSkipReason = "no_fixup_to_rereview"
		return out, nil
	}
	runSpec116Rereview(ctx, in, actRes, &out)
	return out, nil
}

// runSpec116Rereview executes the spec 116 internal re-review chain:
// DispatchInternalReview → (on verdict) PostStructuredReview → (on
// approve) ApplyReadyToMergeLabel and/or EscalateInternalRereview.
// Mutates out in place so the caller doesn't have to juggle the
// every-branch return-value plumbing. Workflow-deterministic — no goroutines, no
// time.Now, no map-iteration randomness; every branch is decided by the
// activity results which Temporal records in history.
func runSpec116Rereview(ctx workflow.Context, in PRIterationInput, iter activities.IteratePRReviewResult, out *PRIterationResult) {
	logger := workflow.GetLogger(ctx)

	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// DispatchInternalReview shells out to a driver — same 2h ceiling
		// as the iteration activity, same retry policy (one shot, results
		// are fail-soft so retries would re-run a non-idempotent driver
		// invocation for no win).
		StartToCloseTimeout: prIterationActivityTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	postActx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// gh-api activities — short bound, single retry on transient
		// network fault. They're fail-soft too so this is mostly belt-
		// and-suspenders.
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	prURL := buildPRURL(in.Repo, in.PRNumber)
	workUnitID := fmt.Sprintf("rereview-pr-%d-review-%d", in.PRNumber, in.ReviewID)

	// 1. DispatchInternalReview
	rrIn := activities.DispatchInternalReviewInput{
		PRNumber:         in.PRNumber,
		PRBranch:         in.PRBranch,
		Repo:             in.Repo,
		TargetRepo:       in.TargetRepo,
		FixupSHA:         iter.FixupSHA,
		OriginalReviewID: in.ReviewID,
		FixupAuthor:      in.DriverID,
		WorkUnitID:       workUnitID,
	}
	var rrRes activities.DispatchInternalReviewResult
	if err := workflow.ExecuteActivity(actx, "DispatchInternalReview", rrIn).Get(ctx, &rrRes); err != nil {
		// Activity is fail-soft — a non-nil error here means the worker
		// or Temporal infra broke. Record + return without firing the
		// follow-on activities; the chain event the activity already
		// emitted (or didn't) is the source-of-truth.
		logger.Error("spec-116: DispatchInternalReview faulted", "pr", in.PRNumber, "err", err)
		out.Explanation += " | spec-116 rereview faulted: " + err.Error()
		return
	}
	out.RereviewRan = true
	out.RereviewerDriver = rrRes.ReviewerDriver
	out.RereviewVerdict = string(rrRes.Verdict)
	out.RereviewConfidence = string(rrRes.Confidence)

	if rrRes.Skipped {
		// Pool empty or driver unregistered — escalate so the operator
		// knows the Copilot-only review is the only sign-off.
		out.RereviewSkipReason = rrRes.SkipReason
		fireRereviewEscalation(postActx, ctx, in, prURL, rrRes,
			activities.ReasonRereviewSkipped, rrRes.Explanation, out)
		return
	}
	if rrRes.Body == "" {
		// FailureKind path — driver faulted or returned malformed JSON.
		out.RereviewSkipReason = "rereview_failed:" + rrRes.FailureKind
		fireRereviewEscalation(postActx, ctx, in, prURL, rrRes,
			activities.ReasonRereviewFailed, rrRes.Explanation, out)
		return
	}

	// 2. PostStructuredReview — body and event derived from the verdict
	// shape. We post even for request-changes (the operator wants to see
	// the JSON verdict on the timeline) but the post is fail-soft.
	postIn := activities.PostStructuredReviewInput{
		PRNumber:       in.PRNumber,
		Repo:           in.Repo,
		WorktreePath:   in.TargetRepo,
		ReviewerDriver: rrRes.ReviewerDriver,
		VerdictBody:    rrRes.Body,
	}
	var postRes activities.PostStructuredReviewResult
	if err := workflow.ExecuteActivity(postActx, "PostStructuredReview", postIn).Get(ctx, &postRes); err != nil {
		// Same fail-soft treatment as above.
		logger.Error("spec-116: PostStructuredReview faulted",
			"pr", in.PRNumber, "err", err)
		out.Explanation += " | spec-116 post faulted: " + err.Error()
	}

	// 3. Verdict-dispatch: label on approve-shaped, escalate on every
	// case the operator should look at. Comparing the verdict as the
	// string subtype avoids importing the verdict package into workflow
	// code (Temporal isolate-purity hygiene).
	switch string(rrRes.Verdict) {
	case "approve", "approve-with-comments":
		// Label the PR. Fail-soft.
		labelIn := activities.ApplyReadyToMergeLabelInput{
			PRURL:        prURL,
			WorktreePath: in.TargetRepo,
		}
		var labelRes activities.ApplyReadyToMergeLabelResult
		if err := workflow.ExecuteActivity(postActx, "ApplyReadyToMergeLabel", labelIn).Get(ctx, &labelRes); err != nil {
			logger.Error("spec-116: ApplyReadyToMergeLabel faulted",
				"pr", in.PRNumber, "err", err)
		} else {
			out.ReadyToMergeLabeled = labelRes.Applied
		}
		// Low confidence is the FR-010 case: label STILL applied (the
		// loop should proceed on autopilot) but the operator gets pinged
		// because the reviewer flagged uncertainty.
		if string(rrRes.Confidence) == "low" {
			fireRereviewEscalation(postActx, ctx, in, prURL, rrRes,
				activities.ReasonRereviewLowConfidence,
				"approve-shaped verdict but reviewer confidence=low",
				out)
		}
	case "request-changes":
		// No label, escalate so the operator decides what to do (revert
		// the fixup, write the change themselves, kick a new round).
		fireRereviewEscalation(postActx, ctx, in, prURL, rrRes,
			activities.ReasonRereviewRequestChanges,
			rrRes.Explanation, out)
	case "abstain":
		// Escalate — the reviewer explicitly declined to render a
		// verdict, so a human must decide.
		fireRereviewEscalation(postActx, ctx, in, prURL, rrRes,
			activities.ReasonRereviewAbstain,
			rrRes.Explanation, out)
	}
}

// fireRereviewEscalation dispatches one EscalateInternalRereview
// activity and mirrors the outcome into out. Pulled into a helper so the
// switch arms above stay flat. ctx is the parent (for logger); actx is
// the workflow activity context.
func fireRereviewEscalation(
	actx workflow.Context,
	ctx workflow.Context,
	in PRIterationInput,
	prURL string,
	rrRes activities.DispatchInternalReviewResult,
	reason activities.EscalateInternalRereviewReason,
	detail string,
	out *PRIterationResult,
) {
	logger := workflow.GetLogger(ctx)
	escIn := activities.EscalateInternalRereviewInput{
		PRNumber:       in.PRNumber,
		PRURL:          prURL,
		Reason:         reason,
		ReviewerDriver: rrRes.ReviewerDriver,
		Detail:         detail,
	}
	var escRes activities.EscalateInternalRereviewResult
	if err := workflow.ExecuteActivity(actx, "EscalateInternalRereview", escIn).Get(ctx, &escRes); err != nil {
		logger.Error("spec-116: EscalateInternalRereview faulted",
			"pr", in.PRNumber, "err", err)
		return
	}
	out.Escalated = escRes.Notified
	out.EscalationReason = string(reason)
}

// buildPRURL renders the GitHub PR HTML URL from (owner/name, prNumber).
// Used for the Discord ping's clickable link and the
// ApplyReadyToMergeLabel input's PR identifier.
func buildPRURL(repo string, prNumber int) string {
	return fmt.Sprintf("https://github.com/%s/pull/%d", repo, prNumber)
}

