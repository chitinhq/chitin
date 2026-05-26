package workflows

import (
	"context"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
)

// PRIterationWorkflow testsuite tests for spec 113 US1 + spec 116 US1.
// Every activity is mocked so the workflow's branching is observable as a
// pure function of the activity outcomes — no driver registry, no real
// git, no real gh.

// iterMocks aggregates the outcomes the workflow's four activities return
// across a single test run. Pointers let nil mean "this activity was not
// expected; if the workflow dispatches it the test fails".
type iterMocks struct {
	iterate  activities.IteratePRReviewResult
	rereview *activities.DispatchInternalReviewResult
	post     *activities.PostStructuredReviewResult
	label    *activities.ApplyReadyToMergeLabelResult
	escalate *activities.EscalateInternalRereviewResult

	// Call-counters so the tests can assert "label NOT called" or
	// "escalate called exactly once" without parsing workflow history.
	iterateCalls  int
	rereviewCalls int
	postCalls     int
	labelCalls    int
	escalateCalls int
}

func iterActivityOpts(name string) activity.RegisterOptions {
	return activity.RegisterOptions{Name: name}
}

// runIterationWorkflow drives one PRIterationWorkflow execution in a
// fresh testsuite environment.
func runIterationWorkflow(t *testing.T, in PRIterationInput, mocks *iterMocks) PRIterationResult {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.IteratePRReviewInput) (activities.IteratePRReviewResult, error) {
			mocks.iterateCalls++
			return mocks.iterate, nil
		},
		iterActivityOpts("IteratePRReview"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.DispatchInternalReviewInput) (activities.DispatchInternalReviewResult, error) {
			mocks.rereviewCalls++
			if mocks.rereview == nil {
				t.Fatalf("unexpected DispatchInternalReview call")
			}
			return *mocks.rereview, nil
		},
		iterActivityOpts("DispatchInternalReview"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.PostStructuredReviewInput) (activities.PostStructuredReviewResult, error) {
			mocks.postCalls++
			if mocks.post == nil {
				t.Fatalf("unexpected PostStructuredReview call")
			}
			return *mocks.post, nil
		},
		iterActivityOpts("PostStructuredReview"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ApplyReadyToMergeLabelInput) (activities.ApplyReadyToMergeLabelResult, error) {
			mocks.labelCalls++
			if mocks.label == nil {
				t.Fatalf("unexpected ApplyReadyToMergeLabel call")
			}
			return *mocks.label, nil
		},
		iterActivityOpts("ApplyReadyToMergeLabel"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.EscalateInternalRereviewInput) (activities.EscalateInternalRereviewResult, error) {
			mocks.escalateCalls++
			if mocks.escalate == nil {
				t.Fatalf("unexpected EscalateInternalRereview call")
			}
			return *mocks.escalate, nil
		},
		iterActivityOpts("EscalateInternalRereview"),
	)

	env.ExecuteWorkflow(PRIterationWorkflow, in)
	if !env.IsWorkflowCompleted() {
		t.Fatal("PRIterationWorkflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var out PRIterationResult
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatalf("GetWorkflowResult: %v", err)
	}
	return out
}

// validIterInput is the minimum-shape input the workflow accepts.
func validIterInput() PRIterationInput {
	return PRIterationInput{
		PRNumber:   1057,
		PRBranch:   "chitin/wu/113",
		TargetRepo: "/tmp/repo",
		Repo:       "chitinhq/chitin",
		ReviewID:   4357372119,
		DriverID:   "claudecode",
	}
}

// TestPRIteration_NoFixupSkipsRereview proves the workflow shortcuts the
// entire spec 116 chain when the iteration produced no fixup: no
// DispatchInternalReview, no Post, no Label, no Escalate. Skip reason
// "no_fixup_to_rereview" is recorded so a downstream chain-event consumer
// can distinguish this from "rereview ran but skipped".
func TestPRIteration_NoFixupSkipsRereview(t *testing.T) {
	mocks := &iterMocks{
		iterate: activities.IteratePRReviewResult{
			PushedFixup: false,
			Explanation: "iteration produced no changes",
		},
	}
	out := runIterationWorkflow(t, validIterInput(), mocks)

	if mocks.iterateCalls != 1 {
		t.Errorf("IteratePRReview called %d times, want 1", mocks.iterateCalls)
	}
	if mocks.rereviewCalls != 0 || mocks.postCalls != 0 || mocks.labelCalls != 0 || mocks.escalateCalls != 0 {
		t.Errorf("spec-116 chain dispatched unexpectedly: rereview=%d post=%d label=%d escalate=%d",
			mocks.rereviewCalls, mocks.postCalls, mocks.labelCalls, mocks.escalateCalls)
	}
	if out.RereviewRan {
		t.Error("RereviewRan = true, want false (no fixup)")
	}
	if out.RereviewSkipReason != "no_fixup_to_rereview" {
		t.Errorf("RereviewSkipReason = %q, want no_fixup_to_rereview", out.RereviewSkipReason)
	}
}

// TestPRIteration_RereviewSkippedEmptyPoolEscalates proves the workflow
// fires the operator escalation when the re-review path runs but is
// short-circuited (no eligible re-reviewer). The Copilot-only review is
// the only sign-off so the operator must triage.
func TestPRIteration_RereviewSkippedEmptyPoolEscalates(t *testing.T) {
	mocks := &iterMocks{
		iterate: activities.IteratePRReviewResult{
			PushedFixup: true,
			FixupSHA:    "deadbeef",
		},
		rereview: &activities.DispatchInternalReviewResult{
			Skipped:     true,
			SkipReason:  string(activities.PoolReasonEmptyAfterExclusion),
			Explanation: "no eligible re-reviewer: empty_after_author_exclusion",
		},
		escalate: &activities.EscalateInternalRereviewResult{Notified: true},
	}
	out := runIterationWorkflow(t, validIterInput(), mocks)

	if mocks.rereviewCalls != 1 {
		t.Errorf("DispatchInternalReview calls = %d, want 1", mocks.rereviewCalls)
	}
	if mocks.postCalls != 0 || mocks.labelCalls != 0 {
		t.Errorf("post/label called on skip path; want zero (post=%d label=%d)",
			mocks.postCalls, mocks.labelCalls)
	}
	if mocks.escalateCalls != 1 {
		t.Errorf("Escalate calls = %d, want 1", mocks.escalateCalls)
	}
	if !out.Escalated {
		t.Error("Escalated = false, want true")
	}
	if out.EscalationReason != string(activities.ReasonRereviewSkipped) {
		t.Errorf("EscalationReason = %q, want %q",
			out.EscalationReason, activities.ReasonRereviewSkipped)
	}
}

// TestPRIteration_ApproveHighConfidenceLabels proves the canonical
// happy path: re-reviewer returns approve with confidence=high, workflow
// posts the review, applies the ready-to-merge label, does NOT escalate
// (autopilot keeps going silently).
func TestPRIteration_ApproveHighConfidenceLabels(t *testing.T) {
	body := `{"verdict":"approve","concerns":null,"recommendations":null,"blockers":null,"confidence":"high"}`
	mocks := &iterMocks{
		iterate: activities.IteratePRReviewResult{
			PushedFixup: true, FixupSHA: "abc123",
		},
		rereview: &activities.DispatchInternalReviewResult{
			ReviewerDriver: "codex",
			Verdict:        "approve",
			Confidence:     "high",
			Body:           body,
		},
		post:  &activities.PostStructuredReviewResult{Posted: true, ReviewEvent: "APPROVE"},
		label: &activities.ApplyReadyToMergeLabelResult{Applied: true},
	}
	out := runIterationWorkflow(t, validIterInput(), mocks)

	if mocks.postCalls != 1 || mocks.labelCalls != 1 {
		t.Errorf("post calls=%d (want 1), label calls=%d (want 1)", mocks.postCalls, mocks.labelCalls)
	}
	if mocks.escalateCalls != 0 {
		t.Errorf("Escalate called %d times on high-confidence approve; want 0", mocks.escalateCalls)
	}
	if !out.ReadyToMergeLabeled {
		t.Error("ReadyToMergeLabeled = false, want true")
	}
	if out.Escalated {
		t.Error("Escalated = true on high-confidence approve; want false")
	}
}

// TestPRIteration_ApproveLowConfidenceLabelsAndEscalates proves FR-010:
// approve-shaped verdict with confidence=low STILL applies the label
// (autopilot proceeds) AND fires the operator escalation (visible gap
// between "approve" and "rubber-stamped").
func TestPRIteration_ApproveLowConfidenceLabelsAndEscalates(t *testing.T) {
	body := `{"verdict":"approve-with-comments","concerns":["unsure about boundary"],"recommendations":null,"blockers":null,"confidence":"low"}`
	mocks := &iterMocks{
		iterate: activities.IteratePRReviewResult{
			PushedFixup: true, FixupSHA: "abc123",
		},
		rereview: &activities.DispatchInternalReviewResult{
			ReviewerDriver: "claudecode",
			Verdict:        "approve-with-comments",
			Confidence:     "low",
			Body:           body,
		},
		post:     &activities.PostStructuredReviewResult{Posted: true, ReviewEvent: "APPROVE"},
		label:    &activities.ApplyReadyToMergeLabelResult{Applied: true},
		escalate: &activities.EscalateInternalRereviewResult{Notified: true},
	}
	out := runIterationWorkflow(t, validIterInput(), mocks)

	if !out.ReadyToMergeLabeled {
		t.Error("ReadyToMergeLabeled = false, want true (low confidence still labels)")
	}
	if !out.Escalated {
		t.Error("Escalated = false, want true (low confidence escalates)")
	}
	if out.EscalationReason != string(activities.ReasonRereviewLowConfidence) {
		t.Errorf("EscalationReason = %q, want %q",
			out.EscalationReason, activities.ReasonRereviewLowConfidence)
	}
}

// TestPRIteration_RequestChangesEscalatesNoLabel proves request-changes
// blocks the label AND fires the escalation. The operator decides whether
// to revert the fixup, write the change themselves, or kick a fresh round.
func TestPRIteration_RequestChangesEscalatesNoLabel(t *testing.T) {
	body := `{"verdict":"request-changes","concerns":null,"recommendations":null,"blockers":["fixup misses the actual concern"],"confidence":"medium"}`
	mocks := &iterMocks{
		iterate: activities.IteratePRReviewResult{
			PushedFixup: true, FixupSHA: "abc123",
		},
		rereview: &activities.DispatchInternalReviewResult{
			ReviewerDriver: "codex",
			Verdict:        "request-changes",
			Confidence:     "medium",
			Body:           body,
		},
		post:     &activities.PostStructuredReviewResult{Posted: true, ReviewEvent: "REQUEST_CHANGES"},
		escalate: &activities.EscalateInternalRereviewResult{Notified: true},
	}
	out := runIterationWorkflow(t, validIterInput(), mocks)

	if mocks.labelCalls != 0 {
		t.Errorf("Label called %d times on request-changes; want 0", mocks.labelCalls)
	}
	if out.ReadyToMergeLabeled {
		t.Error("ReadyToMergeLabeled = true on request-changes; want false")
	}
	if out.EscalationReason != string(activities.ReasonRereviewRequestChanges) {
		t.Errorf("EscalationReason = %q, want %q",
			out.EscalationReason, activities.ReasonRereviewRequestChanges)
	}
}

// TestPRIteration_AbstainEscalates proves abstain escalates without
// labeling — a reviewer abstention is explicitly the "human must decide"
// case.
func TestPRIteration_AbstainEscalates(t *testing.T) {
	body := `{"verdict":"abstain","concerns":null,"recommendations":null,"blockers":null,"reason":"unfamiliar with this area","confidence":"low"}`
	mocks := &iterMocks{
		iterate: activities.IteratePRReviewResult{
			PushedFixup: true, FixupSHA: "abc123",
		},
		rereview: &activities.DispatchInternalReviewResult{
			ReviewerDriver: "claudecode",
			Verdict:        "abstain",
			Confidence:     "low",
			Body:           body,
		},
		post:     &activities.PostStructuredReviewResult{Posted: true, ReviewEvent: "COMMENT"},
		escalate: &activities.EscalateInternalRereviewResult{Notified: true},
	}
	out := runIterationWorkflow(t, validIterInput(), mocks)

	if mocks.labelCalls != 0 {
		t.Errorf("Label called on abstain; want 0 calls")
	}
	if out.EscalationReason != string(activities.ReasonRereviewAbstain) {
		t.Errorf("EscalationReason = %q, want %q",
			out.EscalationReason, activities.ReasonRereviewAbstain)
	}
}

// TestPRIteration_RereviewFailedEscalates proves a driver-fault or
// malformed-verdict outcome from DispatchInternalReview fires the
// rereview_failed escalation — neither post nor label dispatch because
// there's no valid verdict to act on.
func TestPRIteration_RereviewFailedEscalates(t *testing.T) {
	mocks := &iterMocks{
		iterate: activities.IteratePRReviewResult{
			PushedFixup: true, FixupSHA: "abc123",
		},
		rereview: &activities.DispatchInternalReviewResult{
			ReviewerDriver: "codex",
			FailureKind:    "malformed_verdict_json",
			Explanation:    "driver output not StructuredVerdict JSON",
			Body:           "",
		},
		escalate: &activities.EscalateInternalRereviewResult{Notified: true},
	}
	out := runIterationWorkflow(t, validIterInput(), mocks)

	if mocks.postCalls != 0 || mocks.labelCalls != 0 {
		t.Errorf("post/label called on failure path; want zero (post=%d label=%d)",
			mocks.postCalls, mocks.labelCalls)
	}
	if out.EscalationReason != string(activities.ReasonRereviewFailed) {
		t.Errorf("EscalationReason = %q, want %q",
			out.EscalationReason, activities.ReasonRereviewFailed)
	}
}
