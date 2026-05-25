package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/internal/speclint"
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
//
// T014 adds the partition counts and Escalated bool so the dispatcher can
// see — without parsing the chain event — that judgement comments fired an
// escalation in this round. Both partitions can fire in a single round
// (FR-008): MechanicalCount>0 dispatches the driver; JudgementCount>0
// emits `spec_iteration_escalated{reason: design_judgement_required}`.
type SpecIterationResult struct {
	PushedFixup      bool   `json:"pushed_fixup"`
	FixupSHA         string `json:"fixup_sha"`
	CommentCount     int    `json:"comment_count"`
	MechanicalCount  int    `json:"mechanical_count"`
	JudgementCount   int    `json:"judgement_count"`
	Escalated        bool   `json:"escalated"`
	EscalationReason string `json:"escalation_reason,omitempty"`
	DriverID         string `json:"driver_id"`
	Unroutable       bool   `json:"unroutable"`
	Explanation      string `json:"explanation"`
}

const (
	// specIterationFetchTimeout bounds the FetchSpecReviewComments activity
	// — two gh-api calls (review body + paginated comments). 2 minutes
	// covers slow network + large reviews without holding the workflow
	// hostage on a stuck gh subprocess.
	specIterationFetchTimeout = 2 * time.Minute
	// specIterationEmitTimeout bounds the EmitSpecIterationEscalation
	// activity — one kernel-emit shell-out (5s internal cap). 30s leaves
	// margin for cold-start binaries without making the workflow wait.
	specIterationEmitTimeout = 30 * time.Second
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

// EscalationReasonDesignJudgement is the closed-taxonomy reason string
// (spec 115 FR-010) the workflow uses when the DesignJudgement partition
// of a review is non-empty. Pulled out as a constant so the workflow,
// the activity, and the test all reference the same literal — drift
// here breaks the operator queue's filter (spec 114 / T017).
const EscalationReasonDesignJudgement = "design_judgement_required"

// SpecIterationWorkflow is the spec 115 US1 + US3 per-review iteration
// workflow for spec PRs. It mirrors PRIterationWorkflow's shape (spec 113
// T004) with three structural differences:
//
//  1. The driver is selected by capability `spec.author` rather than carried
//     in — a spec PR can come from any author, so there is no original
//     driver to re-invoke (FR-005).
//  2. The activity uses the spec-tuned prompt template (T012) and folds the
//     linter's violations (FR-006) in alongside the review comments.
//  3. T014 — comments are fetched up front, classified in workflow code via
//     speclint.ClassifyDesignJudgement (FR-007), and partitioned into
//     Mechanical + DesignJudgement. The driver fires iff Mechanical is
//     non-empty; a `spec_iteration_escalated{reason: design_judgement_required}`
//     event fires iff DesignJudgement is non-empty (FR-008). Both can fire
//     in the same round.
//
// Activity composition (in dispatch order):
//
//  1. FetchSpecReviewComments — gh api → typed comment list (workflow can't
//     classify without bodies; the only side effect — gh api — is wrapped
//     so the workflow remains replay-stable).
//  2. (workflow code) ClassifyDesignJudgement → partition.
//  3. (if JudgementCount > 0) EmitSpecIterationEscalation — fire-and-forget
//     chain event. Independent of driver dispatch so an Unroutable driver
//     pool doesn't suppress the escalation operators need to see.
//  4. (if MechanicalCount > 0) SelectDriver → IterateSpecReview — drive
//     the mechanical subset, passing the comment-id filter so the activity
//     drops judgement comments from the prompt.
//
// Determinism: workflow code is side-effect-free (three activity calls,
// pure regex classification, no time, no signals, no children). Replay-
// stable by construction; ClassifyDesignJudgement is a pure regex match
// over the fetched-and-recorded comment bodies.
//
// v1 cap: ONE round per review, matching PRIterationWorkflow. Subsequent
// Copilot reviews on the same PR produce fresh workflows with fresh
// ReviewIDs.
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
	const round = 1 // v1: one round per review

	// Step 1: fetch the review's line comments. Workflow cannot classify
	// without the bodies; gh api is a side effect, so it runs in an
	// activity and the result is recorded for replay.
	fctx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: specIterationFetchTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	var fetched activities.FetchSpecReviewCommentsResult
	if err := workflow.ExecuteActivity(fctx, "FetchSpecReviewComments", activities.FetchSpecReviewCommentsInput{
		Repo:     in.Repo,
		PRNumber: in.PRNumber,
		ReviewID: in.ReviewID,
	}).Get(fctx, &fetched); err != nil {
		logger.Error("spec-iteration: fetch-comments faulted",
			"pr", in.PRNumber, "review", in.ReviewID, "err", err)
		return SpecIterationResult{
			Explanation: fmt.Sprintf("fetch-comments activity faulted: %v", err),
		}, err
	}

	// Step 2: classify each comment in workflow code. ClassifyDesignJudgement
	// is a pure regex match over the recorded comment body — replay-safe.
	// DefaultJudgementPhrases is also pure (regexp.MustCompile of constant
	// patterns at function call). The operator-editable file is loaded by
	// the activity that wraps the classifier in production (out of scope
	// for T014); the workflow uses the baseline set so partitioning is
	// deterministic and replay-stable for a given (review, classifier-
	// version) pair.
	phrases := speclint.DefaultJudgementPhrases()
	mechanicalIDs := make([]int64, 0, len(fetched.Comments))
	judgementIDs := make([]int64, 0, len(fetched.Comments))
	for _, c := range fetched.Comments {
		switch speclint.ClassifyDesignJudgement(c.Body, phrases) {
		case speclint.DesignJudgement:
			judgementIDs = append(judgementIDs, c.ID)
		default:
			mechanicalIDs = append(mechanicalIDs, c.ID)
		}
	}

	res := SpecIterationResult{
		CommentCount:    len(fetched.Comments),
		MechanicalCount: len(mechanicalIDs),
		JudgementCount:  len(judgementIDs),
	}

	// Step 3: if any judgement comments, emit the escalation. Runs BEFORE
	// driver dispatch so an empty driver pool (Unroutable) does not
	// suppress the operator-facing escalation event.
	if len(judgementIDs) > 0 {
		ectx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: specIterationEmitTimeout,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
		})
		var emit activities.EmitSpecIterationEscalationResult
		if err := workflow.ExecuteActivity(ectx, "EmitSpecIterationEscalation", activities.EmitSpecIterationEscalationInput{
			PRNumber:            in.PRNumber,
			ReviewID:            in.ReviewID,
			Round:               round,
			RoundsAttempted:     round,
			Reason:              EscalationReasonDesignJudgement,
			JudgementCommentIDs: judgementIDs,
		}).Get(ectx, &emit); err != nil {
			// Emit is fail-soft inside the activity, so an error here is
			// a Temporal-level fault. Surface it; the partition decision
			// is recorded in the result so the operator still sees the
			// escalation happened conceptually.
			logger.Error("spec-iteration: emit-escalation faulted",
				"pr", in.PRNumber, "review", in.ReviewID, "err", err)
			res.Explanation = fmt.Sprintf("emit-escalation activity faulted: %v", err)
			return res, err
		}
		// Honor the activity's Emitted flag — a kernel-emit that no-op'd
		// (CHITIN_DISABLE_CHAIN_EMIT=1) or fail-softed (kernel exec
		// error swallowed inside the activity) returns Emitted=false with
		// err=nil. Reporting Escalated=true in that case would mislead
		// operators relying on the chain event. Reason is only set when
		// the emit actually landed.
		if emit.Emitted {
			res.Escalated = true
			res.EscalationReason = EscalationReasonDesignJudgement
		} else {
			logger.Warn("spec-iteration: escalation requested but emit was a no-op",
				"pr", in.PRNumber, "review", in.ReviewID, "explanation", emit.Explanation)
			res.Explanation = fmt.Sprintf("escalation skipped: %s", emit.Explanation)
		}
	}

	// Step 4: if no mechanical comments, the workflow's work is done.
	// Either everything was judgement (escalated above) or the review
	// had no inline comments at all (rare — review body without
	// comments is informational; T016 will fold the body into a
	// separate `spec_iteration_skipped` event).
	if len(mechanicalIDs) == 0 {
		switch {
		case res.Escalated:
			res.Explanation = fmt.Sprintf(
				"escalated review #%d (pr=%d): all %d comment(s) classified as design judgement",
				in.ReviewID, in.PRNumber, res.JudgementCount)
		case res.JudgementCount > 0:
			// Judgement-only review where the emit was a no-op — keep
			// the "escalation skipped: ..." explanation set above so
			// operators can see why the chain event didn't land.
		default:
			res.Explanation = fmt.Sprintf(
				"skipped review #%d (pr=%d): no inline comments to iterate",
				in.ReviewID, in.PRNumber)
		}
		return res, nil
	}

	// Step 5: select a spec-author-capable driver. Selection is a side
	// effect (it probes each candidate's Ready check) so it MUST run in
	// an activity; the recorded result is replay-stable.
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
		res.Explanation = fmt.Sprintf("select-driver activity faulted: %v", err)
		return res, err
	}
	if sel.Unroutable {
		// No registered driver satisfies spec.author. Return a settled
		// result rather than an error so the dispatcher can escalate
		// cleanly instead of retrying against the same empty pool.
		// If we already escalated for judgement, that escalation still
		// fired — the workflow result records both signals.
		res.Unroutable = true
		res.Explanation = sel.Reason
		return res, nil
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
		PRNumber:            in.PRNumber,
		PRBranch:            in.PRBranch,
		TargetRepo:          in.TargetRepo,
		Repo:                in.Repo,
		ReviewID:            in.ReviewID,
		Round:               round,
		DriverID:            sel.DriverID,
		WorkUnitID:          workUnitID,
		LintViolations:      in.LintViolations,
		MechanicalCommentIDs: mechanicalIDs,
	}

	var actRes iterateSpecReviewResult
	if err := workflow.ExecuteActivity(actx, "IterateSpecReview", actIn).Get(ctx, &actRes); err != nil {
		logger.Error("spec-iteration: activity faulted",
			"pr", in.PRNumber, "review", in.ReviewID, "err", err)
		res.DriverID = sel.DriverID
		res.Explanation = fmt.Sprintf("iteration activity faulted: %v", err)
		return res, err
	}

	res.DriverID = sel.DriverID
	res.PushedFixup = actRes.PushedFixup
	res.FixupSHA = actRes.FixupSHA
	if actRes.Explanation != "" {
		res.Explanation = actRes.Explanation
	} else {
		res.Explanation = fmt.Sprintf(
			"iterated %d mechanical comment(s) on review #%d (pr=%d); escalated=%t",
			res.MechanicalCount, in.ReviewID, in.PRNumber, res.Escalated)
	}
	return res, nil
}

// iterateSpecReviewInput is the wire payload sent to the IterateSpecReview
// activity. Defined here while the activity is implemented in a follow-up
// task; the activity's input struct must mirror these JSON tags so payloads
// round-trip cleanly through Temporal's codec.
//
// T014 adds MechanicalCommentIDs — the comment-id filter the activity
// applies before building the driver prompt. Judgement comments are
// excluded (they escalated separately via EmitSpecIterationEscalation),
// so the driver sees only the mechanical subset.
type iterateSpecReviewInput struct {
	PRNumber             int                 `json:"pr_number"`
	PRBranch             string              `json:"pr_branch"`
	TargetRepo           string              `json:"target_repo"`
	Repo                 string              `json:"repo"`
	ReviewID             int64               `json:"review_id"`
	Round                int                 `json:"round"`
	DriverID             string              `json:"driver_id"`
	WorkUnitID           string              `json:"work_unit_id"`
	LintViolations       []SpecLintViolation `json:"lint_violations,omitempty"`
	MechanicalCommentIDs []int64             `json:"mechanical_comment_ids,omitempty"`
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
