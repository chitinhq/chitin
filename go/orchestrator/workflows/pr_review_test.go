package workflows

import (
	"context"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review"
	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
)

// PRReviewWorkflow testsuite tests (spec 094 US1). Each test mocks every
// side effect — pool selection, snapshot capture, per-reviewer dispatch,
// telemetry — so the workflow runs hermetically and its branching is a
// pure function of the activity outcomes returned by the mocks.
//
// The testsuite environment is the natural fit for these tests because
// the workflow's parallel-dispatch behaviour (SC-009) and its aggregation
// decision tree (FR-009 through FR-012) are observable via the recorded
// dispatch order and the workflow's return value, with no need to stand
// up a real driver registry.

// reviewActivityOpts mirrors activityOpts in scheduler_test.go: an
// activity is registered under a stable name so the workflow's
// ExecuteActivity(name, ...) call binds correctly in the testsuite.
func reviewActivityOpts(name string) activity.RegisterOptions {
	return activity.RegisterOptions{Name: name}
}

// runReviewWorkflow drives one PRReviewWorkflow execution in a fresh
// testsuite environment. Caller provides the mock outcomes for each
// activity surface (slate from selection, snapshot, per-primary
// verdict-or-failure, optional arbiter outcome).
type reviewMocks struct {
	slate           review.ReviewerSlate
	slateErr        error
	snapshot        review.PRSnapshot
	snapshotErr     error
	primary1Outcome verdict.Outcome
	primary2Outcome verdict.Outcome
	arbiterOutcome  *verdict.Outcome
	arbiterErr      error
	// telemetryCalls records each EmitReviewTelemetry invocation in
	// dispatch order so SC-009 / SC-010 telemetry-reconstruction tests
	// can inspect the trace.
	telemetryCalls []review.EmitReviewTelemetryInput
}

func runReviewWorkflow(
	t *testing.T,
	in review.PRReviewInput,
	mocks *reviewMocks,
) (PRReviewOutput, error) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ review.SelectReviewersInput) (review.ReviewerSlate, error) {
			return mocks.slate, mocks.slateErr
		},
		reviewActivityOpts(review.SelectReviewersActivityName),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ review.PRReviewInput) (review.PRSnapshot, error) {
			return mocks.snapshot, mocks.snapshotErr
		},
		reviewActivityOpts("CapturePRSnapshot"),
	)

	// Dispatch mock dispatches both primaries and the arbiter based on
	// the role + slot index encoded in the InvocationID suffix ("p1",
	// "p2", "arb"). Decoupling the dispatch return from the role this
	// way means the workflow's parallel dispatch order doesn't change
	// which mock outcome lands where.
	env.RegisterActivityWithOptions(
		func(_ context.Context, in review.DispatchMachineReviewerInput) (review.DispatchMachineReviewerResult, error) {
			// Find which slot this is by InvocationID suffix.
			startedAt := time.Now().UTC()
			inv := review.ReviewerInvocation{
				InvocationID:    in.InvocationID,
				DriverID:        in.DriverID,
				Role:            in.Role,
				SnapshotHashRef: review.SnapshotHashRef(in.Snapshot),
				StartedAt:       startedAt,
			}
			switch {
			case endsWith(in.InvocationID, ":p1"):
				inv.Outcome = mocks.primary1Outcome
			case endsWith(in.InvocationID, ":p2"):
				inv.Outcome = mocks.primary2Outcome
			case endsWith(in.InvocationID, ":arb"):
				if mocks.arbiterErr != nil {
					return review.DispatchMachineReviewerResult{}, mocks.arbiterErr
				}
				if mocks.arbiterOutcome != nil {
					inv.Outcome = *mocks.arbiterOutcome
				}
			}
			inv.Outcome.InvocationID = in.InvocationID
			inv.Outcome.DriverID = in.DriverID
			inv.Outcome.Role = in.Role
			return review.DispatchMachineReviewerResult{Invocation: inv}, nil
		},
		reviewActivityOpts(review.DispatchMachineReviewerActivityName),
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, in review.EmitReviewTelemetryInput) error {
			mocks.telemetryCalls = append(mocks.telemetryCalls, in)
			return nil
		},
		reviewActivityOpts(review.EmitReviewTelemetryActivityName),
	)

	env.ExecuteWorkflow(PRReviewWorkflow, in)
	if !env.IsWorkflowCompleted() {
		t.Fatal("PRReviewWorkflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var out PRReviewOutput
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatalf("GetWorkflowResult: %v", err)
	}
	return out, nil
}

// endsWith is a tiny helper to avoid an import for strings.HasSuffix in
// the mock body. Kept local to the test.
func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// --- US1: happy path tests (SC-001) ---

// TestHappyPath_BothApprove covers the canonical dialectic happy path
// (FR-010): two primary drivers each return verdict=approve. The gate
// returns passed without engaging the arbiter; exactly two invocations
// are recorded; the slate's exclusion fields are present for audit.
func TestHappyPath_BothApprove(t *testing.T) {
	mocks := &reviewMocks{
		slate: review.ReviewerSlate{
			Primary1: "hermes", Primary2: "openclaw",
			EligibleAfterExclusion: []string{"hermes", "openclaw"},
		},
		snapshot: review.PRSnapshot{Repo: "chitinhq/chitin", PRNumber: 928, HeadOID: "abc123"},
		primary1Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{Verdict: verdict.Approve},
		},
		primary2Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{Verdict: verdict.Approve},
		},
	}
	out, _ := runReviewWorkflow(t, review.PRReviewInput{
		Repo: "chitinhq/chitin", PRNumber: 928, PRAuthor: "jpleva91",
		PolicyClass: "spec-only", ArbiterType: review.ArbiterOperator,
	}, mocks)

	if out.Decision.State != verdict.GatePassed {
		t.Errorf("Decision.State = %s, want %s", out.Decision.State, verdict.GatePassed)
	}
	if out.Decision.ArbiterEngaged {
		t.Error("Decision.ArbiterEngaged = true, want false (rule 1 short-circuit)")
	}
	if len(out.Invocations) != 2 {
		t.Errorf("len(Invocations) = %d, want 2 (no arbiter)", len(out.Invocations))
	}
	if got := len(mocks.telemetryCalls); got != 2 {
		t.Errorf("EmitReviewTelemetry called %d times, want 2", got)
	}
}

// TestHappyPath_ApproveAndApproveWithComments confirms the two
// approve-shaped variants combine cleanly — spec.md Edge Case
// "Disagreement between approve and approve-with-comments: NOT a
// disagreement."
func TestHappyPath_ApproveAndApproveWithComments(t *testing.T) {
	mocks := &reviewMocks{
		slate: review.ReviewerSlate{
			Primary1: "hermes", Primary2: "openclaw",
			EligibleAfterExclusion: []string{"hermes", "openclaw"},
		},
		snapshot: review.PRSnapshot{Repo: "chitinhq/chitin", PRNumber: 928},
		primary1Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{Verdict: verdict.Approve},
		},
		primary2Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{
				Verdict:  verdict.ApproveWithComments,
				Concerns: []string{"nit on the lint output format"},
			},
		},
	}
	out, _ := runReviewWorkflow(t, review.PRReviewInput{
		Repo: "chitinhq/chitin", PRNumber: 928, PRAuthor: "jpleva91",
		PolicyClass: "spec-only", ArbiterType: review.ArbiterOperator,
	}, mocks)
	if out.Decision.State != verdict.GatePassed {
		t.Errorf("Decision.State = %s, want passed", out.Decision.State)
	}
	if out.Decision.ArbiterEngaged {
		t.Error("Decision.ArbiterEngaged = true, want false")
	}
}

// TestBothReject_NoArbiter covers FR-011: when both primaries return
// request-changes, halt without arbiter. This is US3's anchor scenario,
// but the aggregator behaviour is exercised by the US1 happy-path
// workflow shape — verifies the workflow correctly routes through
// verdict.Aggregate's rule 2.
func TestBothReject_NoArbiter(t *testing.T) {
	mocks := &reviewMocks{
		slate: review.ReviewerSlate{
			Primary1: "hermes", Primary2: "openclaw",
			EligibleAfterExclusion: []string{"hermes", "openclaw"},
		},
		snapshot: review.PRSnapshot{Repo: "chitinhq/chitin", PRNumber: 928},
		primary1Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{Verdict: verdict.RequestChanges, Blockers: []string{"no tests"}},
		},
		primary2Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{Verdict: verdict.RequestChanges, Blockers: []string{"no plan"}},
		},
	}
	out, _ := runReviewWorkflow(t, review.PRReviewInput{
		Repo: "chitinhq/chitin", PRNumber: 928, PRAuthor: "jpleva91",
		PolicyClass: "spec-only", ArbiterType: review.ArbiterOperator,
	}, mocks)
	if out.Decision.State != verdict.GateBlocked {
		t.Errorf("Decision.State = %s, want blocked", out.Decision.State)
	}
	if out.Decision.ArbiterEngaged {
		t.Error("Decision.ArbiterEngaged = true, want false (rule 2 short-circuit)")
	}
	if out.Decision.Reason != "both primaries request-changes" {
		t.Errorf("Decision.Reason = %q", out.Decision.Reason)
	}
}

// TestDisagreement_OperatorArbiterNotWired confirms the documented stub:
// on primary disagreement with ArbiterType=operator, this Phase 2
// foundational slice halts with the named stub reason. The follow-up PR
// (US2 Phase 4) flips this to a real operator-arbiter dispatch.
func TestDisagreement_OperatorArbiterNotWired(t *testing.T) {
	mocks := &reviewMocks{
		slate: review.ReviewerSlate{
			Primary1: "hermes", Primary2: "openclaw",
			EligibleAfterExclusion: []string{"hermes", "openclaw"},
		},
		snapshot: review.PRSnapshot{Repo: "chitinhq/chitin", PRNumber: 928},
		primary1Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{Verdict: verdict.Approve},
		},
		primary2Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{Verdict: verdict.RequestChanges, Blockers: []string{"nope"}},
		},
	}
	out, _ := runReviewWorkflow(t, review.PRReviewInput{
		Repo: "chitinhq/chitin", PRNumber: 928, PRAuthor: "jpleva91",
		PolicyClass: "spec-only", ArbiterType: review.ArbiterOperator,
	}, mocks)
	if out.Decision.State != verdict.GateHalted {
		t.Errorf("Decision.State = %s, want halted (stub)", out.Decision.State)
	}
	if !endsWith(out.Decision.Reason, "Phase 2 foundational (US2 follow-up)") {
		t.Errorf("Decision.Reason = %q, want stub reason about US2 follow-up", out.Decision.Reason)
	}
}

// TestDisagreement_MachineArbiter_Approves covers the FR-012 third-driver
// arbitration path. Three reviewers are dispatched (2 primary + 1
// arbiter); the arbiter's approve-shaped verdict supersedes the primary
// disagreement.
func TestDisagreement_MachineArbiter_Approves(t *testing.T) {
	arbApprove := verdict.Outcome{
		Verdict: &verdict.StructuredVerdict{Verdict: verdict.Approve},
	}
	mocks := &reviewMocks{
		slate: review.ReviewerSlate{
			Primary1: "hermes", Primary2: "openclaw", Arbiter: "codex",
			EligibleAfterExclusion: []string{"codex", "hermes", "openclaw"},
		},
		snapshot: review.PRSnapshot{Repo: "chitinhq/chitin", PRNumber: 928},
		primary1Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{Verdict: verdict.Approve},
		},
		primary2Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{Verdict: verdict.RequestChanges, Blockers: []string{"no"}},
		},
		arbiterOutcome: &arbApprove,
	}
	out, _ := runReviewWorkflow(t, review.PRReviewInput{
		Repo: "chitinhq/chitin", PRNumber: 928, PRAuthor: "jpleva91",
		PolicyClass: "impl", ArbiterType: review.ArbiterMachine,
	}, mocks)
	if out.Decision.State != verdict.GatePassed {
		t.Errorf("Decision.State = %s, want passed", out.Decision.State)
	}
	if !out.Decision.ArbiterEngaged {
		t.Error("Decision.ArbiterEngaged = false, want true")
	}
	if len(out.Invocations) != 3 {
		t.Errorf("len(Invocations) = %d, want 3 (2 primary + arbiter)", len(out.Invocations))
	}
	if got := len(mocks.telemetryCalls); got != 3 {
		t.Errorf("EmitReviewTelemetry called %d times, want 3", got)
	}
}

// TestDisagreement_MachineArbiterPoolExhausted covers FR-007 +
// Acceptance Scenario 4.4 — primary disagreement on a machine-arbiter
// class with no third driver in the slate. Workflow halts with the
// named "arbiter pool exhausted" reason.
func TestDisagreement_MachineArbiterPoolExhausted(t *testing.T) {
	mocks := &reviewMocks{
		slate: review.ReviewerSlate{
			Primary1: "hermes", Primary2: "openclaw", // Arbiter intentionally empty
			EligibleAfterExclusion: []string{"hermes", "openclaw"},
		},
		snapshot: review.PRSnapshot{Repo: "chitinhq/chitin", PRNumber: 928},
		primary1Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{Verdict: verdict.Approve},
		},
		primary2Outcome: verdict.Outcome{
			Verdict: &verdict.StructuredVerdict{Verdict: verdict.RequestChanges, Blockers: []string{"no"}},
		},
	}
	out, _ := runReviewWorkflow(t, review.PRReviewInput{
		Repo: "chitinhq/chitin", PRNumber: 928, PRAuthor: "jpleva91",
		PolicyClass: "impl", ArbiterType: review.ArbiterMachine,
	}, mocks)
	if out.Decision.State != verdict.GateHalted {
		t.Errorf("Decision.State = %s, want halted", out.Decision.State)
	}
	if out.Decision.Reason != "arbiter pool exhausted" {
		t.Errorf("Decision.Reason = %q, want %q", out.Decision.Reason, "arbiter pool exhausted")
	}
}

// TestSelectionShortfall_HaltsBeforeDispatch covers FR-007 + Acceptance
// Scenario 4.2 — when the eligible pool can't fill both primary slots,
// the activity returns an "empty primaries" slate as a state flag and the
// workflow halts at selection without dispatching any primary.
func TestSelectionShortfall_HaltsBeforeDispatch(t *testing.T) {
	dispatchCount := 0
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(
		// Shortfall state: slate carries the diagnostic fields, primaries
		// are empty. The workflow's review.IsShortfall(slate) check fires.
		func(_ context.Context, _ review.SelectReviewersInput) (review.ReviewerSlate, error) {
			return review.ReviewerSlate{
				ExcludedAuthor:         "hermes",
				EligibleAfterExclusion: []string{"openclaw"},
			}, nil
		},
		reviewActivityOpts(review.SelectReviewersActivityName),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ review.PRReviewInput) (review.PRSnapshot, error) {
			return review.PRSnapshot{}, nil
		},
		reviewActivityOpts("CapturePRSnapshot"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ review.DispatchMachineReviewerInput) (review.DispatchMachineReviewerResult, error) {
			dispatchCount++
			return review.DispatchMachineReviewerResult{}, nil
		},
		reviewActivityOpts(review.DispatchMachineReviewerActivityName),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ review.EmitReviewTelemetryInput) error { return nil },
		reviewActivityOpts(review.EmitReviewTelemetryActivityName),
	)

	env.ExecuteWorkflow(PRReviewWorkflow, review.PRReviewInput{
		Repo: "chitinhq/chitin", PRNumber: 928, PRAuthor: "hermes-bot",
		PolicyClass: "spec-only", ArbiterType: review.ArbiterOperator,
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var out PRReviewOutput
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatalf("GetWorkflowResult: %v", err)
	}
	if out.Decision.State != verdict.GateHalted {
		t.Errorf("Decision.State = %s, want halted", out.Decision.State)
	}
	if dispatchCount != 0 {
		t.Errorf("DispatchMachineReviewer called %d times, want 0 (must halt before dispatch)", dispatchCount)
	}
	if got := out.Slate.ExcludedAuthor; got != "hermes" {
		t.Errorf("Slate.ExcludedAuthor = %q, want %q", got, "hermes")
	}
	if got := len(out.Slate.EligibleAfterExclusion); got != 1 {
		t.Errorf("len(Slate.EligibleAfterExclusion) = %d, want 1", got)
	}
	// The reason should name the counts so the operator can act on the
	// audit without re-fetching workflow history.
	if !endsWith(out.Decision.Reason, "(excluded_author=\"hermes\")") {
		t.Errorf("Decision.Reason = %q, want it to end with excluded_author marker", out.Decision.Reason)
	}
}
