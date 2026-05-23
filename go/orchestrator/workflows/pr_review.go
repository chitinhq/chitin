package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review"
	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
)

// PRReviewOutput is the typed return value of PRReviewWorkflow — what the
// parent PRMergeWorkflow (spec 093) consumes to decide whether to proceed
// with merge or halt the queue.
type PRReviewOutput struct {
	// Decision is the dialectic outcome (passed, blocked, halted) plus
	// the reason and the arbiter-engaged flag.
	Decision verdict.Decision `json:"decision"`
	// Invocations is every reviewer invocation that took place in this
	// dialectic, in dispatch order — two primaries plus optionally a
	// third arbiter entry.
	Invocations []review.ReviewerInvocation `json:"invocations"`
	// Slate is the pool-selection result. Recorded for audit so a future
	// reader can see which drivers were eligible and which was excluded.
	Slate review.ReviewerSlate `json:"slate"`
}

// Activity time-bound constants (spec 094 FR-026). These are the upper
// bounds; typical reviewer dispatches complete in 1–10 minutes.
const (
	// SelectReviewersTimeout — pool selection is fast (registry lookup +
	// Ready probes); 5 seconds is generous.
	SelectReviewersTimeout = 5 * time.Second
	// DispatchMachineReviewerTimeout — per-machine-reviewer time bound
	// from FR-026. The workflow times out the dispatch and the activity
	// returns a closed FailureTimeout outcome rather than a hung activity.
	DispatchMachineReviewerTimeout = 30 * time.Minute
	// EmitReviewTelemetryTimeout — telemetry emit is non-critical write.
	EmitReviewTelemetryTimeout = 5 * time.Second
)

// PRReviewWorkflow is the dialectic review gate workflow (spec 094). It
// is spawned as a child by the parent PRMergeWorkflow (spec 093) on PRs
// whose policy class has review_required=true. It returns a
// ReviewGateDecision the parent consumes.
//
// Phase 2 foundational + US1 MVP shape:
//
//  1. Select reviewers via SelectReviewers activity → ReviewerSlate.
//     On shortfall (eligible pool < 2), halt with a named-counts reason.
//  2. Dispatch the two primaries in parallel via two DispatchMachineReviewer
//     activities; wait for both.
//  3. Emit one telemetry event per closed invocation.
//  4. Aggregate via verdict.Aggregate. If primaries agree (both
//     approve-shaped or both request-changes), short-circuit.
//  5. Otherwise — disagreement, abstain, or any failure — engage the
//     arbiter:
//     - For ArbiterMachine with Slate.Arbiter set: dispatch a third
//       machine reviewer. (Slate.Arbiter empty here means pool exhausted
//       — halt the workflow per FR-007.)
//     - For ArbiterOperator: NOT YET IMPLEMENTED in this slice; the
//       workflow halts with reason "operator arbiter dispatch not wired
//       in Phase 2 foundational". The follow-up PR (US2 Phase 4) wires
//       the GitHub PR comment surface (R-OPSURF).
//  6. Emit telemetry for the arbiter invocation.
//  7. Aggregate with the arbiter outcome and return the decision.
//
// All workflow-side logic is deterministic — verdict.Aggregate is pure,
// telemetry hash computation is pure. The only I/O happens inside
// activities, which Temporal records exactly once and replays from
// history.
func PRReviewWorkflow(ctx workflow.Context, in review.PRReviewInput) (PRReviewOutput, error) {
	log := workflow.GetLogger(ctx)
	wfInfo := workflow.GetInfo(ctx)
	log.Info("PRReviewWorkflow starting",
		"repo", in.Repo, "pr", in.PRNumber, "class", in.PolicyClass, "arbiter", in.ArbiterType)

	// --- Step 1: select reviewers ---
	selectCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: SelectReviewersTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	var slate review.ReviewerSlate
	if err := workflow.ExecuteActivity(selectCtx, review.SelectReviewersActivityName,
		review.SelectReviewersInput{
			PRAuthor:          in.PRAuthor,
			ArbiterType:       in.ArbiterType,
			PrimariesRequired: 2,
		}).Get(ctx, &slate); err != nil {
		// Activity-level error is a configuration fault (nil registry,
		// bad input) — surface it as a halt with the underlying message.
		return PRReviewOutput{
			Decision: verdict.Decision{
				State:  verdict.GateHalted,
				Reason: "selection activity failed: " + err.Error(),
			},
		}, nil
	}
	// Shortfall is a state, not an error — the activity returned the slate
	// with Primary1 empty as a flag. Halt with the named-counts reason and
	// preserve the slate's audit fields (ExcludedAuthor, EligibleAfterExclusion).
	if review.IsShortfall(slate) {
		return PRReviewOutput{
			Decision: verdict.Decision{
				State:  verdict.GateHalted,
				Reason: review.ShortfallReason(slate, 2),
			},
			Slate: slate,
		}, nil
	}

	// --- Step 2: capture snapshot --- (stubbed for Phase 2; the workflow
	// expects an activity that returns a PRSnapshot but the activity is
	// stubbed at the activity layer. The testsuite mocks it.)
	//
	// TODO(spec-094-impl PR #2): wire CapturePRSnapshot activity that
	// runs `gh pr view --json files,headRefOid,...` and reads spec
	// artifacts from disk. Phase 2 foundational testsuite tests inject
	// a fixture snapshot.
	var snapshot review.PRSnapshot
	snapCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	})
	if err := workflow.ExecuteActivity(snapCtx, "CapturePRSnapshot",
		review.PRReviewInput{Repo: in.Repo, PRNumber: in.PRNumber, PRAuthor: in.PRAuthor}).Get(ctx, &snapshot); err != nil {
		return PRReviewOutput{
			Decision: verdict.Decision{
				State:  verdict.GateHalted,
				Reason: "snapshot: " + err.Error(),
			},
			Slate: slate,
		}, nil
	}

	// --- Step 3: dispatch primaries in parallel ---
	dispatchCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: DispatchMachineReviewerTimeout,
		// HeartbeatTimeout is set by the activity-side activity.RecordHeartbeat
		// pattern; the workflow declares a generous start-to-close.
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1, // failures become FailureKind outcomes, not retries
		},
	})

	p1Future := workflow.ExecuteActivity(dispatchCtx, review.DispatchMachineReviewerActivityName,
		review.DispatchMachineReviewerInput{
			InvocationID: invocationIDFor(wfInfo.WorkflowExecution.ID, "p1"),
			DriverID:     slate.Primary1,
			Role:         verdict.RolePrimary,
			PolicyClass:  in.PolicyClass,
			Snapshot:     snapshot,
		})
	p2Future := workflow.ExecuteActivity(dispatchCtx, review.DispatchMachineReviewerActivityName,
		review.DispatchMachineReviewerInput{
			InvocationID: invocationIDFor(wfInfo.WorkflowExecution.ID, "p2"),
			DriverID:     slate.Primary2,
			Role:         verdict.RolePrimary,
			PolicyClass:  in.PolicyClass,
			Snapshot:     snapshot,
		})

	var p1Result, p2Result review.DispatchMachineReviewerResult
	if err := p1Future.Get(ctx, &p1Result); err != nil {
		// Activity-level error is a configuration fault. Surface it.
		return PRReviewOutput{
			Decision: verdict.Decision{
				State:  verdict.GateHalted,
				Reason: "primary 1 dispatch failed: " + err.Error(),
			},
			Slate: slate,
		}, nil
	}
	if err := p2Future.Get(ctx, &p2Result); err != nil {
		return PRReviewOutput{
			Decision: verdict.Decision{
				State:  verdict.GateHalted,
				Reason: "primary 2 dispatch failed: " + err.Error(),
			},
			Slate:       slate,
			Invocations: []review.ReviewerInvocation{p1Result.Invocation},
		}, nil
	}

	// --- Step 4: emit per-invocation telemetry ---
	telemetryCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: EmitReviewTelemetryTimeout,
	})
	for _, inv := range []review.ReviewerInvocation{p1Result.Invocation, p2Result.Invocation} {
		_ = workflow.ExecuteActivity(telemetryCtx, review.EmitReviewTelemetryActivityName,
			review.EmitReviewTelemetryInput{
				WorkflowID: wfInfo.WorkflowExecution.ID,
				Repo:       in.Repo,
				PRNumber:   in.PRNumber,
				Invocation: inv,
			}).Get(ctx, nil) // telemetry failure is non-fatal
	}

	// --- Step 5: aggregate primaries; engage arbiter if needed ---
	decision := verdict.Aggregate(p1Result.Invocation.Outcome, p2Result.Invocation.Outcome, nil)
	invocations := []review.ReviewerInvocation{p1Result.Invocation, p2Result.Invocation}

	if !decision.ArbiterEngaged && decision.State != verdict.GateHalted {
		// Rules 1 or 2 short-circuited — gate decided without arbiter.
		return PRReviewOutput{Decision: decision, Invocations: invocations, Slate: slate}, nil
	}
	if decision.Reason == "arbiter required but not dispatched" {
		// Disagreement — must engage arbiter.
		arbiterInv, arbiterErr := dispatchArbiter(ctx, in, slate, snapshot, wfInfo.WorkflowExecution.ID)
		if arbiterErr != nil {
			return PRReviewOutput{
				Decision: verdict.Decision{
					State:          verdict.GateHalted,
					Reason:         arbiterErr.Error(),
					ArbiterEngaged: false,
				},
				Slate:       slate,
				Invocations: invocations,
			}, nil
		}
		invocations = append(invocations, arbiterInv)
		_ = workflow.ExecuteActivity(telemetryCtx, review.EmitReviewTelemetryActivityName,
			review.EmitReviewTelemetryInput{
				WorkflowID: wfInfo.WorkflowExecution.ID,
				Repo:       in.Repo,
				PRNumber:   in.PRNumber,
				Invocation: arbiterInv,
			}).Get(ctx, nil)
		decision = verdict.Aggregate(p1Result.Invocation.Outcome, p2Result.Invocation.Outcome, &arbiterInv.Outcome)
	}

	return PRReviewOutput{Decision: decision, Invocations: invocations, Slate: slate}, nil
}

// dispatchArbiter handles the arbiter slot per the workflow's ArbiterType.
// Returns the closed invocation on success, or an error whose message
// names the dispositive halt reason on failure.
func dispatchArbiter(
	ctx workflow.Context,
	in review.PRReviewInput,
	slate review.ReviewerSlate,
	snapshot review.PRSnapshot,
	workflowID string,
) (review.ReviewerInvocation, error) {
	switch in.ArbiterType {
	case review.ArbiterMachine:
		if slate.Arbiter == "" {
			return review.ReviewerInvocation{}, fmt.Errorf("arbiter pool exhausted")
		}
		dispatchCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: DispatchMachineReviewerTimeout,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		var res review.DispatchMachineReviewerResult
		if err := workflow.ExecuteActivity(dispatchCtx, review.DispatchMachineReviewerActivityName,
			review.DispatchMachineReviewerInput{
				InvocationID: invocationIDFor(workflowID, "arb"),
				DriverID:     slate.Arbiter,
				Role:         verdict.RoleArbiter,
				PolicyClass:  in.PolicyClass,
				Snapshot:     snapshot,
			}).Get(ctx, &res); err != nil {
			return review.ReviewerInvocation{}, fmt.Errorf("arbiter dispatch failed: %s", err.Error())
		}
		return res.Invocation, nil
	case review.ArbiterOperator:
		// TODO(spec-094-impl Phase 4 PR): wire R-OPSURF surface.
		return review.ReviewerInvocation{},
			fmt.Errorf("operator arbiter dispatch not wired in Phase 2 foundational (US2 follow-up)")
	default:
		return review.ReviewerInvocation{},
			fmt.Errorf("unknown arbiter type: %q", in.ArbiterType)
	}
}

// invocationIDFor builds a stable per-invocation id from the workflow id
// and a slot suffix. Within one workflow execution the suffix differs per
// slot ("p1"/"p2"/"arb"), so a replay reproduces identical ids.
func invocationIDFor(workflowID, slot string) string {
	return workflowID + ":" + slot
}
