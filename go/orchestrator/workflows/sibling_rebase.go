package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
)

// SiblingRebaseInput is the typed input to SiblingRebaseWorkflow — one open
// chitin-authored pull request whose sibling just merged to the base branch
// (spec 112 US2). One workflow per sibling, dispatched from the factory-
// listen merge webhook so the operator-host returns immediately and the
// rebase runs durably under Temporal.
type SiblingRebaseInput struct {
	// PRNumber is the open sibling pull request to rebase.
	PRNumber int `json:"pr_number"`
	// PRBranch is the head branch of the open sibling pull request — the
	// branch the rebase rewrites and force-pushes.
	PRBranch string `json:"pr_branch"`
	// TargetRepo is the absolute path the worktree Manager mints the
	// dedicated checkout under.
	TargetRepo string `json:"target_repo"`
	// BaseBranch is the branch the rebase targets — usually "main".
	BaseBranch string `json:"base_branch"`
	// SchedulerRunID is the scheduler run that originally dispatched both
	// siblings. Carried into the chain event payload for correlation.
	SchedulerRunID string `json:"scheduler_run_id"`
	// SourcePRNumber is the sibling PR whose merge to BaseBranch triggered
	// this rebase.
	SourcePRNumber int `json:"source_pr_number"`
	// Repo is the GitHub owner/name pair (e.g. "chitinhq/chitin") —
	// carried so the activity can build operator-facing PR links for
	// escalation notifications. Optional; empty means "no link in
	// notifications" (the helper drops the notice with a warning).
	Repo string `json:"repo,omitempty"`
}

// SiblingRebaseResult mirrors activities.RebaseSiblingPRResult so the
// workflow's typed output is self-describing without forcing every caller
// to import the activities package.
type SiblingRebaseResult struct {
	Rebased       bool     `json:"rebased"`
	NewHeadSHA    string   `json:"new_head_sha"`
	NewBaseSHA    string   `json:"new_base_sha"`
	ConflictFiles []string `json:"conflict_files"`
	Explanation   string   `json:"explanation"`
}

// rebaseActivityTimeout bounds the RebaseSiblingPR activity. Fetch + rebase +
// force-push against a typical chitin spec-dispatch PR completes in well under
// a minute; the 10-minute ceiling allows for slow remotes and large diffs
// without risk of a hung activity dragging the workflow forever.
const rebaseActivityTimeout = 10 * time.Minute

// SiblingRebaseWorkflow is the per-sibling auto-rebase workflow (spec 112
// US2). It invokes the RebaseSiblingPR activity exactly once — the activity
// itself folds every outcome (success, conflict, push failure) into a typed
// result and never returns a Temporal error — so the workflow needs no retry
// policy beyond MaxAttempts=1.
//
// Determinism: this workflow is trivially deterministic. It dispatches a
// single activity and returns its result; there is no time, no signal, no
// child workflow.
//
// One workflow runs per open sibling PR; the factory-listen merge handler
// fires N of these concurrently when a chitin PR merges to main, each
// keyed by PR number so a duplicate webhook delivery is a no-op against
// Temporal's WorkflowID uniqueness.
func SiblingRebaseWorkflow(ctx workflow.Context, in SiblingRebaseInput) (SiblingRebaseResult, error) {
	logger := workflow.GetLogger(ctx)

	if in.PRNumber <= 0 || in.PRBranch == "" || in.TargetRepo == "" {
		return SiblingRebaseResult{}, temporal.NewNonRetryableApplicationError(
			"sibling-rebase: PRNumber, PRBranch, and TargetRepo are required",
			"InvalidSiblingRebaseInput", nil)
	}

	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: rebaseActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			// One attempt: the activity already encodes "rebase produced a
			// conflict" as a non-error outcome. A retry would re-run the
			// rebase against the same base and produce the same result.
			MaximumAttempts: 1,
		},
	})

	workUnitID := fmt.Sprintf("rebase-pr-%d-from-%d", in.PRNumber, in.SourcePRNumber)
	actIn := activities.RebaseSiblingPRInput{
		PRNumber:       in.PRNumber,
		PRBranch:       in.PRBranch,
		TargetRepo:     in.TargetRepo,
		BaseBranch:     in.BaseBranch,
		SchedulerRunID: in.SchedulerRunID,
		SourcePRNumber: in.SourcePRNumber,
		WorkUnitID:     workUnitID,
		Repo:           in.Repo,
	}

	var actRes activities.RebaseSiblingPRResult
	if err := workflow.ExecuteActivity(actx, "RebaseSiblingPR", actIn).Get(ctx, &actRes); err != nil {
		// The activity is engineered to return a nil error for every rebase
		// outcome; an error here means a Temporal-level fault (activity
		// timeout, worker crash). Surface it so the workflow shows failed
		// in Temporal UI rather than silently swallowing.
		logger.Error("sibling-rebase: activity faulted",
			"pr", in.PRNumber, "branch", in.PRBranch, "err", err)
		return SiblingRebaseResult{
			Explanation: fmt.Sprintf("rebase activity faulted: %v", err),
		}, err
	}

	return SiblingRebaseResult{
		Rebased:       actRes.Rebased,
		NewHeadSHA:    actRes.NewHeadSHA,
		NewBaseSHA:    actRes.NewBaseSHA,
		ConflictFiles: actRes.ConflictFiles,
		Explanation:   actRes.Explanation,
	}, nil
}
