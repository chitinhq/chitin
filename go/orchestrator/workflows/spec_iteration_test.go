package workflows

import (
	"context"
	"sort"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
)

// TestSpecIterationWorkflow_T014_Partition is the spec 115 T014 invariant
// test: every Copilot comment on a spec-PR review lands in exactly one
// partition (Mechanical | DesignJudgement); the driver fires iff the
// mechanical partition is non-empty; one `spec_iteration_escalated`
// event emits iff the judgement partition is non-empty. Both can fire
// in the same round.
//
// One test, four sub-tests, four boundary cases — empty review, all
// mechanical, all judgement, mixed. The mixed case is the load-bearing
// invariant: T014 says "both partitions can fire in the same round".

// fetchSpec is shorthand for the fetch-activity input the workflow expects.
// Every sub-test serves a fixed fetched result; the workflow's classification
// is the unit under test, so feeding deterministic input keeps the test
// pure regex against the operator-editable phrase set.

// fixtureMechanicalBody is a Copilot-style mechanical comment — flat
// description of a defect, no judgement words. Classifier returns
// Mechanical. (Verified manually against DefaultJudgementPhrases.)
const fixtureMechanicalBody = "The CLI invocation references `chitin-kernel events`, which does not exist as a subcommand. Update to the documented surface."

// fixtureJudgementBody contains the FR-007 phrase "consider" — the
// classifier returns DesignJudgement.
const fixtureJudgementBody = "Consider whether this user story is duplicative of US1; the trigger conditions overlap substantively."

// runSpecIteration executes SpecIterationWorkflow against a stubbed
// activity environment. fetched is served by FetchSpecReviewComments;
// the captured slices record what the workflow dispatched. Sorting the
// captured ids gives the test a stable comparison regardless of
// iteration order in Go maps.
func runSpecIteration(
	t *testing.T,
	in SpecIterationInput,
	fetched activities.FetchSpecReviewCommentsResult,
) (SpecIterationResult, error, []int64, *activities.EmitSpecIterationEscalationInput, bool) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Stub: FetchSpecReviewComments returns the canned fetched result.
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.FetchSpecReviewCommentsInput) (activities.FetchSpecReviewCommentsResult, error) {
			return fetched, nil
		},
		activity.RegisterOptions{Name: "FetchSpecReviewComments"},
	)

	// Stub: EmitSpecIterationEscalation captures the call so the test can
	// assert it fired exactly when (and with what payload) T014's
	// invariant requires.
	var emitted *activities.EmitSpecIterationEscalationInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.EmitSpecIterationEscalationInput) (activities.EmitSpecIterationEscalationResult, error) {
			captured := in
			emitted = &captured
			return activities.EmitSpecIterationEscalationResult{Emitted: true, Explanation: "stubbed"}, nil
		},
		activity.RegisterOptions{Name: "EmitSpecIterationEscalation"},
	)

	// Stub: SelectDriver always returns a routable driver. The driver
	// pool is not the unit under test; T014 cares whether SelectDriver
	// was DISPATCHED, not what it returned.
	var selectDispatched bool
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SelectDriverInput) (activities.SelectDriverResult, error) {
			selectDispatched = true
			return activities.SelectDriverResult{DriverID: "claudecode-test"}, nil
		},
		activity.RegisterOptions{Name: "SelectDriver"},
	)

	// Stub: IterateSpecReview captures the mechanical-id filter the
	// workflow passed in, so the test can assert the judgement comments
	// were excluded.
	var passedMechanical []int64
	env.RegisterActivityWithOptions(
		func(_ context.Context, in iterateSpecReviewInput) (iterateSpecReviewResult, error) {
			passedMechanical = append([]int64(nil), in.MechanicalCommentIDs...)
			return iterateSpecReviewResult{
				PushedFixup:  true,
				FixupSHA:     "deadbeefcafef00d",
				CommentCount: len(in.MechanicalCommentIDs),
				Explanation:  "stubbed driver round",
			}, nil
		},
		activity.RegisterOptions{Name: "IterateSpecReview"},
	)

	env.ExecuteWorkflow(SpecIterationWorkflow, in)

	if !env.IsWorkflowCompleted() {
		t.Fatalf("SpecIterationWorkflow did not complete")
	}
	wfErr := env.GetWorkflowError()
	var result SpecIterationResult
	if wfErr == nil {
		if err := env.GetWorkflowResult(&result); err != nil {
			t.Fatalf("decoding SpecIterationResult: %v", err)
		}
	}
	sort.Slice(passedMechanical, func(i, j int) bool { return passedMechanical[i] < passedMechanical[j] })
	return result, wfErr, passedMechanical, emitted, selectDispatched
}

// validInput is the canonical valid SpecIterationInput. Sub-tests share
// it so a change to the validation surface only edits one place.
func validInput() SpecIterationInput {
	return SpecIterationInput{
		PRNumber:   1057,
		PRBranch:   "spec/115-spec-review-gate",
		TargetRepo: "/tmp/spec-iter-target",
		Repo:       "chitinhq/chitin",
		ReviewID:   4358581784,
	}
}

// TestSpecIterationWorkflow_T014_NoComments: zero-input boundary. Empty
// review (no inline comments) — no partition, no driver, no escalation.
// The workflow settles cleanly with explanation noting the skip.
func TestSpecIterationWorkflow_T014_NoComments(t *testing.T) {
	res, err, passed, emit, selectDispatched := runSpecIteration(t, validInput(),
		activities.FetchSpecReviewCommentsResult{
			ReviewBody: "LGTM, nothing inline.",
			Comments:   nil,
		})

	if err != nil {
		t.Fatalf("workflow errored on a zero-comment review: %v", err)
	}
	if res.CommentCount != 0 || res.MechanicalCount != 0 || res.JudgementCount != 0 {
		t.Errorf("zero-comment review must produce zero counts, got: %+v", res)
	}
	if res.Escalated {
		t.Error("zero-comment review must not escalate")
	}
	if res.PushedFixup {
		t.Error("zero-comment review must not push a fixup")
	}
	if emit != nil {
		t.Error("zero-comment review must not dispatch EmitSpecIterationEscalation")
	}
	if selectDispatched {
		t.Error("zero-comment review must not dispatch SelectDriver")
	}
	if len(passed) != 0 {
		t.Errorf("zero-comment review must not dispatch IterateSpecReview, got mechanical=%v", passed)
	}
}

// TestSpecIterationWorkflow_T014_AllMechanical: one partition, no
// escalation. The driver fires with all comment ids; no
// `spec_iteration_escalated` event emits.
func TestSpecIterationWorkflow_T014_AllMechanical(t *testing.T) {
	res, err, passed, emit, selectDispatched := runSpecIteration(t, validInput(),
		activities.FetchSpecReviewCommentsResult{
			Comments: []activities.SpecReviewComment{
				{ID: 100, Path: "specs/115/spec.md", Line: 42, Body: fixtureMechanicalBody},
				{ID: 101, Path: "specs/115/spec.md", Line: 88, Body: "Broken cross-reference to spec 999; no such directory."},
				{ID: 102, Path: "specs/115/tasks.md", Line: 5, Body: "Trailing whitespace on this line."},
			},
		})

	if err != nil {
		t.Fatalf("workflow errored on a mechanical-only review: %v", err)
	}
	if res.MechanicalCount != 3 || res.JudgementCount != 0 {
		t.Errorf("expected 3 mechanical / 0 judgement, got mech=%d judg=%d",
			res.MechanicalCount, res.JudgementCount)
	}
	if res.Escalated || emit != nil {
		t.Errorf("all-mechanical review must not escalate; escalated=%t, emit=%+v", res.Escalated, emit)
	}
	if !selectDispatched {
		t.Error("all-mechanical review must dispatch SelectDriver")
	}
	wantIDs := []int64{100, 101, 102}
	if !equalInt64Slices(passed, wantIDs) {
		t.Errorf("driver should receive all 3 mechanical ids, got %v want %v", passed, wantIDs)
	}
	if !res.PushedFixup {
		t.Error("all-mechanical review with a working driver should produce a fixup")
	}
}

// TestSpecIterationWorkflow_T014_AllJudgement: the inverse — no
// mechanical, so no driver dispatch; the escalation fires exactly once
// with reason=design_judgement_required and carries every comment id.
func TestSpecIterationWorkflow_T014_AllJudgement(t *testing.T) {
	res, err, passed, emit, selectDispatched := runSpecIteration(t, validInput(),
		activities.FetchSpecReviewCommentsResult{
			Comments: []activities.SpecReviewComment{
				{ID: 200, Path: "specs/115/spec.md", Line: 12, Body: fixtureJudgementBody},
				{ID: 201, Path: "specs/115/spec.md", Line: 60, Body: "Is this really a P2 user story?"},
			},
		})

	if err != nil {
		t.Fatalf("workflow errored on a judgement-only review: %v", err)
	}
	if res.MechanicalCount != 0 || res.JudgementCount != 2 {
		t.Errorf("expected 0 mechanical / 2 judgement, got mech=%d judg=%d",
			res.MechanicalCount, res.JudgementCount)
	}
	if !res.Escalated {
		t.Error("judgement-only review must set Escalated=true")
	}
	if res.EscalationReason != EscalationReasonDesignJudgement {
		t.Errorf("escalation reason = %q, want %q", res.EscalationReason, EscalationReasonDesignJudgement)
	}
	if emit == nil {
		t.Fatal("judgement-only review must dispatch EmitSpecIterationEscalation")
	}
	if emit.Reason != EscalationReasonDesignJudgement {
		t.Errorf("emit.Reason = %q, want %q", emit.Reason, EscalationReasonDesignJudgement)
	}
	gotIDs := append([]int64(nil), emit.JudgementCommentIDs...)
	sort.Slice(gotIDs, func(i, j int) bool { return gotIDs[i] < gotIDs[j] })
	wantIDs := []int64{200, 201}
	if !equalInt64Slices(gotIDs, wantIDs) {
		t.Errorf("emit.JudgementCommentIDs = %v, want %v", gotIDs, wantIDs)
	}
	if selectDispatched {
		t.Error("judgement-only review must NOT dispatch SelectDriver (no mechanical work)")
	}
	if len(passed) != 0 {
		t.Errorf("judgement-only review must NOT dispatch IterateSpecReview, got mechanical=%v", passed)
	}
	if res.PushedFixup {
		t.Error("judgement-only review must not push a fixup")
	}
}

// TestSpecIterationWorkflow_T014_MixedSameRound is the T014 load-bearing
// invariant: a review containing both mechanical AND judgement comments
// fires BOTH the driver (with the mechanical subset only) AND the
// escalation (with the judgement subset only) in the same round.
func TestSpecIterationWorkflow_T014_MixedSameRound(t *testing.T) {
	res, err, passed, emit, selectDispatched := runSpecIteration(t, validInput(),
		activities.FetchSpecReviewCommentsResult{
			Comments: []activities.SpecReviewComment{
				{ID: 300, Path: "specs/115/spec.md", Line: 10, Body: fixtureMechanicalBody},
				{ID: 301, Path: "specs/115/spec.md", Line: 20, Body: fixtureJudgementBody},
				{ID: 302, Path: "specs/115/spec.md", Line: 30, Body: "Undefined event_type `spec_lint_blew_up` referenced in FR-009."},
				{ID: 303, Path: "specs/115/spec.md", Line: 40, Body: "Should this be split into two specs?"},
			},
		})

	if err != nil {
		t.Fatalf("workflow errored on a mixed-classification review: %v", err)
	}
	if res.MechanicalCount != 2 || res.JudgementCount != 2 {
		t.Errorf("expected 2 mechanical / 2 judgement, got mech=%d judg=%d",
			res.MechanicalCount, res.JudgementCount)
	}

	// Both partitions must fire in the same round (T014 invariant).
	if !res.Escalated {
		t.Error("mixed review must escalate the judgement partition")
	}
	if emit == nil {
		t.Fatal("mixed review must dispatch EmitSpecIterationEscalation")
	}
	if !selectDispatched {
		t.Error("mixed review must dispatch SelectDriver for the mechanical partition")
	}
	if !res.PushedFixup {
		t.Error("mixed review must push a fixup for the mechanical partition")
	}

	// The mechanical subset only — judgement ids must NOT reach the driver.
	wantMech := []int64{300, 302}
	if !equalInt64Slices(passed, wantMech) {
		t.Errorf("driver received %v mechanical ids, want %v (judgement ids must be filtered out)",
			passed, wantMech)
	}

	// The judgement subset only — mechanical ids must NOT reach the escalation.
	gotJudg := append([]int64(nil), emit.JudgementCommentIDs...)
	sort.Slice(gotJudg, func(i, j int) bool { return gotJudg[i] < gotJudg[j] })
	wantJudg := []int64{301, 303}
	if !equalInt64Slices(gotJudg, wantJudg) {
		t.Errorf("escalation carried %v judgement ids, want %v (mechanical ids must be filtered out)",
			gotJudg, wantJudg)
	}
}

// TestSpecIterationWorkflow_T014_InvalidInput proves the validation guard
// surface — missing required fields return a non-retryable application
// error so the dispatcher (T015) can fail fast rather than retry against
// the same empty input.
func TestSpecIterationWorkflow_T014_InvalidInput(t *testing.T) {
	_, err, _, _, _ := runSpecIteration(t, SpecIterationInput{
		PRNumber: 1057,
		// missing PRBranch, TargetRepo, Repo, ReviewID
	}, activities.FetchSpecReviewCommentsResult{})
	if err == nil {
		t.Fatal("missing required fields must produce a workflow error, not silently proceed")
	}
}

// equalInt64Slices is a small helper — reflect.DeepEqual works but
// produces noisy failure output; a hand-written compare reads better
// in test logs.
func equalInt64Slices(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
