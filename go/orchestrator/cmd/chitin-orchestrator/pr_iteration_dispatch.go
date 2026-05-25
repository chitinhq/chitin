// pr_iteration_dispatch.go — spec 113 US1 PR-iteration dispatch from the
// factory-listen /webhook/pr handler.
//
// Mirrors spec 112 US2's `dispatchSiblingRebase` pattern (injectable
// temporal dialer, nil-stderr guard, deterministic WorkflowID +
// AlreadyStarted dedup). The `PRIterationWorkflow` it ultimately starts
// is implemented under task T004 — until that lands this dispatcher is a
// structural stub that resolves the WorkflowID but does not call
// ExecuteWorkflow; FailureKind="workflow_not_implemented" surfaces in
// the response so operators can see the wiring is in place ahead of the
// workflow itself.

package main

import (
	"context"
	"fmt"
	"io"
)

// prIterationDispatchInput is the closed input to dispatchPRIteration.
// Populated by handlePR from a parsed prPayload after
// checkPullRequestReviewEvent passes.
type prIterationDispatchInput struct {
	Repo           string
	PRNumber       int
	PRBranch       string
	BaseBranch     string
	ReviewID       int64
	ReviewerLogin  string
	ReviewState    string
	SchedulerRunID string
	TargetRepo     string
	TemporalHost   string
}

// prIterationDispatchResult is what dispatchPRIteration returns. handlePR
// uses it to populate the prResponse body fields.
type prIterationDispatchResult struct {
	Dispatched   bool
	WorkflowID   string
	DedupSkipped bool
	FailureKind  string
	Detail       string
}

// dispatchPRIteration is the dedup-gated dispatch entry point invoked
// from handlePR. The full implementation per task T002 wires Temporal +
// emits chain events; this stub captures the deterministic WorkflowID
// so wiring tests can assert on it, and reports
// FailureKind="workflow_not_implemented" until T004's
// PRIterationWorkflow lands.
func dispatchPRIteration(
	ctx context.Context,
	in prIterationDispatchInput,
	dialer temporalDialer,
	stderr io.Writer,
) prIterationDispatchResult {
	if in.PRNumber == 0 || in.ReviewID == 0 {
		return prIterationDispatchResult{FailureKind: "invalid_input"}
	}
	_ = ctx
	_ = dialer
	_ = stderr
	return prIterationDispatchResult{
		WorkflowID:  fmt.Sprintf("iteration-pr-%d-review-%d", in.PRNumber, in.ReviewID),
		FailureKind: "workflow_not_implemented",
	}
}
