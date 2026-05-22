package loop

import (
	"context"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

// Spec 078 US1 replay/determinism tests for ImprovementLoopWorkflow (FR-001,
// SC-001, SC-002): a fixed telemetry window with a known recurring failure
// yields exactly one evidence-backed spec proposal, queued for the operator
// and never applied. Each test mocks every side effect — telemetry ingest, the
// proposal-queue projection — so the loop runs hermetically and its proposal
// is a pure function of the window. The TestWorkflowEnvironment drives a
// replay-equivalent execution and panics on any non-determinism.

// loopActivityOpts names an activity by its string name — the name the loop
// workflow dispatches to.
func loopActivityOpts(name string) activity.RegisterOptions {
	return activity.RegisterOptions{Name: name}
}

// runLoopCycle executes ImprovementLoopWorkflow once over a fixed telemetry
// window, mocking IngestTelemetry (serving `windowRecords` for the matching
// source) and ProjectProposalQueue (capturing the enqueued input). It returns
// the loop result and the proposal-queue input the cycle projected.
func runLoopCycle(
	t *testing.T,
	in LoopInput,
	windowRecords []TelemetryRecord,
) (LoopResult, ProposalQueueInput) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Mock IngestTelemetry: a per-source static reader over the fixed window.
	// This is the "fixed telemetry window" of the US1 Independent Test.
	env.RegisterActivityWithOptions(
		func(ctx context.Context, ingestIn IngestInput) (IngestResult, error) {
			reader := NewStaticTelemetryReader(ingestIn.Source, windowRecords)
			recs, err := reader.Read(ctx, ingestIn.Window)
			if err != nil {
				return IngestResult{Source: ingestIn.Source}, err
			}
			return IngestResult{Source: ingestIn.Source, Records: recs,
				Reachable: true}, nil
		},
		loopActivityOpts("IngestTelemetry"),
	)

	// Mock ProjectProposalQueue: capture what the cycle enqueued — the
	// authoritative record that the loop QUEUED its proposal (FR-013) and
	// stopped there (FR-005).
	var enqueued ProposalQueueInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, qIn ProposalQueueInput) error {
			enqueued = qIn
			return nil
		},
		loopActivityOpts("ProjectProposalQueue"),
	)

	env.ExecuteWorkflow(ImprovementLoopWorkflow, in)

	if !env.IsWorkflowCompleted() {
		t.Fatalf("loop workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("loop workflow errored: %v", err)
	}
	var result LoopResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decoding loop result: %v", err)
	}
	return result, enqueued
}

// TestLoop_KnownRecurringFailure_OneQueuedProposal is the US1 Independent Test
// (spec 078 SC-001, SC-002): a fixed telemetry window containing a known
// recurring failure produces EXACTLY ONE evidence-backed spec proposal, named
// against a real spec, queued for the operator — and never applied.
func TestLoop_KnownRecurringFailure_OneQueuedProposal(t *testing.T) {
	// A fixed window: the same CI failure signature, twice — a known recurrence.
	window := []TelemetryRecord{
		rec("ci-fail-1", SourceCI, 10, "failure", "go test ./... exit 1", "076"),
		rec("ci-fail-2", SourceCI, 30, "failure", "go test ./... exit 1", "076"),
	}
	result, enqueued := runLoopCycle(t, LoopInput{
		Cycle: 1, Since: ts(0), Until: ts(60),
		LiveSpecRefs: []string{"076"}, // 076 is a real, live spec.
	}, window)

	// Exactly one proposal (SC-001).
	if result.NewProposalCount != 1 {
		t.Fatalf("loop produced %d proposals, want exactly 1 (SC-001)", result.NewProposalCount)
	}
	if len(enqueued.Proposals) != 1 {
		t.Fatalf("queue received %d proposals, want exactly 1", len(enqueued.Proposals))
	}
	p := enqueued.Proposals[0]

	// It names the failure and the real spec.
	if p.TargetSpec != "076" {
		t.Errorf("proposal target spec = %q, want 076 — a concrete change to a named spec", p.TargetSpec)
	}
	if p.Finding.Signature != "go test ./... exit 1" {
		t.Errorf("proposal does not name the failure; signature = %q", p.Finding.Signature)
	}

	// It carries the telemetry that grounds it (FR-004).
	ev := p.EvidenceIDs()
	if len(ev) != 2 || ev[0] != "ci-fail-1" || ev[1] != "ci-fail-2" {
		t.Errorf("proposal evidence = %v, want both grounding CI records", ev)
	}

	// It is QUEUED and NOT applied — the human gate (FR-005, SC-002).
	if p.Status != StatusProposalPending {
		t.Errorf("proposal status = %q, want pending — nothing is applied (FR-005)", p.Status)
	}
	if !result.HumanGate {
		t.Error("loop result must assert the human gate held — the cycle ends at queued")
	}
	if result.EmptyCycle {
		t.Error("a cycle with a finding is not an empty cycle")
	}
}

// TestLoop_DeterministicReplay proves FR-001 / SC-001: 30 runs of the loop over
// the identical fixed window produce the identical proposal id every time —
// the loop's analysis and synthesis are replay-deterministic.
func TestLoop_DeterministicReplay(t *testing.T) {
	window := []TelemetryRecord{
		rec("ci-1", SourceCI, 10, "failure", "build broke", "076"),
		rec("ci-2", SourceCI, 20, "failure", "build broke", "076"),
	}
	in := LoopInput{Cycle: 1, Since: ts(0), Until: ts(60), LiveSpecRefs: []string{"076"}}

	first, _ := runLoopCycle(t, in, window)
	if len(first.ProposalIDs) != 1 {
		t.Fatalf("first run produced %d proposal ids, want 1", len(first.ProposalIDs))
	}
	for i := 0; i < 30; i++ {
		got, _ := runLoopCycle(t, in, window)
		if len(got.ProposalIDs) != 1 || got.ProposalIDs[0] != first.ProposalIDs[0] {
			t.Fatalf("run %d proposal id drifted: %v != %v",
				i, got.ProposalIDs, first.ProposalIDs)
		}
	}
}

// TestLoop_EmptyWindow_EmptyCycle proves the spec-078 edge case: an empty
// telemetry window completes as an empty cycle — no proposal, the cycle ends
// cleanly, the checkpoint still advances. The loop never blocks or fails loud.
func TestLoop_EmptyWindow_EmptyCycle(t *testing.T) {
	result, enqueued := runLoopCycle(t, LoopInput{
		Cycle: 5, Since: ts(0), Until: ts(60), LiveSpecRefs: []string{"076"},
	}, nil /* empty window */)

	if !result.EmptyCycle {
		t.Error("an empty window must yield an empty cycle")
	}
	if result.NewProposalCount != 0 {
		t.Errorf("an empty cycle produces no proposal; got %d", result.NewProposalCount)
	}
	if !enqueued.EmptyCycle {
		t.Error("the queue projection must record the empty cycle")
	}
	// The checkpoint advances even for an empty cycle (FR-011).
	if !result.Checkpoint.At.Equal(ts(60)) {
		t.Errorf("checkpoint did not advance on an empty cycle: At = %v, want %v",
			result.Checkpoint.At, ts(60))
	}
	if !result.HumanGate {
		t.Error("even an empty cycle holds the human gate")
	}
}

// TestLoop_SingleFailureProducesNoProposal proves a window with a failure that
// appears only ONCE — noise, not a recurrence — produces no proposal.
func TestLoop_SingleFailureProducesNoProposal(t *testing.T) {
	window := []TelemetryRecord{
		rec("ci-once", SourceCI, 10, "failure", "a one-off blip", "076"),
	}
	result, _ := runLoopCycle(t, LoopInput{
		Cycle: 1, Since: ts(0), Until: ts(60), LiveSpecRefs: []string{"076"},
	}, window)
	if result.NewProposalCount != 0 {
		t.Errorf("a single failure is noise — no proposal; got %d", result.NewProposalCount)
	}
	if !result.EmptyCycle {
		t.Error("a cycle that finds only noise is an empty cycle")
	}
}

// TestLoop_StaleSpec_NoProposalAgainstDeadSpec proves the stale-spec edge
// case end-to-end: a recurring failure on a spec the catalog does NOT list as
// live is marked stale — the cycle emits no proposal against the dead spec.
func TestLoop_StaleSpec_NoProposalAgainstDeadSpec(t *testing.T) {
	window := []TelemetryRecord{
		rec("ci-1", SourceCI, 10, "failure", "dead spec failure", "999"),
		rec("ci-2", SourceCI, 20, "failure", "dead spec failure", "999"),
	}
	// The catalog lists 076 live, but the finding targets 999.
	result, enqueued := runLoopCycle(t, LoopInput{
		Cycle: 1, Since: ts(0), Until: ts(60), LiveSpecRefs: []string{"076"},
	}, window)

	if result.NewProposalCount != 0 {
		t.Errorf("no proposal must be emitted against a dead spec; got %d", result.NewProposalCount)
	}
	if result.StaleCount != 1 {
		t.Errorf("the finding against a dead spec must be marked stale; StaleCount = %d", result.StaleCount)
	}
	if len(enqueued.Proposals) != 0 {
		t.Errorf("queue received %d proposals, want 0 — nothing against a dead spec", len(enqueued.Proposals))
	}
}

// TestLoop_UnreachableLayer_DoesNotFailCycle proves the FR-002 edge case: a
// loop cycle whose telemetry sources are all unreachable completes as a clean
// empty cycle — a dead telemetry layer never fails or blocks the loop.
func TestLoop_UnreachableLayer_DoesNotFailCycle(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// IngestTelemetry always reports the layer unreachable — empty contribution.
	env.RegisterActivityWithOptions(
		func(_ context.Context, in IngestInput) (IngestResult, error) {
			return IngestResult{Source: in.Source, Reachable: false}, nil
		},
		loopActivityOpts("IngestTelemetry"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ ProposalQueueInput) error { return nil },
		loopActivityOpts("ProjectProposalQueue"),
	)

	env.ExecuteWorkflow(ImprovementLoopWorkflow, LoopInput{
		Cycle: 1, Since: ts(0), Until: ts(60), LiveSpecRefs: []string{"076"},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("loop must complete even when every telemetry layer is unreachable")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("an unreachable telemetry layer must not fail the cycle: %v", err)
	}
	var result LoopResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decoding result: %v", err)
	}
	if !result.EmptyCycle {
		t.Error("a cycle over unreachable layers is an empty cycle")
	}
}

// TestLoop_DuplicateSuppression_AttachesEvidence proves FR-014 / SC-006
// end-to-end: a cycle whose detected finding matches a still-pending finding
// from a prior cycle does NOT queue a fresh proposal — it queues an UPDATED
// proposal at the same id, carrying the merged evidence.
func TestLoop_DuplicateSuppression_AttachesEvidence(t *testing.T) {
	// The pending finding from a prior cycle, grounded in one record.
	pending := finding(FindingRecurringFailure, "076", "go test ./... exit 1",
		CategoryCodeGeneration,
		rec("ci-old", SourceCI, 5, "failure", "go test ./... exit 1", "076"))

	// This cycle's window: the SAME failure, recurring, with two new records.
	window := []TelemetryRecord{
		rec("ci-new-1", SourceCI, 10, "failure", "go test ./... exit 1", "076"),
		rec("ci-new-2", SourceCI, 30, "failure", "go test ./... exit 1", "076"),
	}
	result, enqueued := runLoopCycle(t, LoopInput{
		Cycle: 2, Since: ts(0), Until: ts(60),
		LiveSpecRefs:    []string{"076"},
		PendingFindings: []Finding{pending},
	}, window)

	// No fresh proposal — the finding duplicates a pending one (FR-014).
	if result.NewProposalCount != 0 {
		t.Errorf("a recurrence must not queue a fresh proposal; NewProposalCount = %d",
			result.NewProposalCount)
	}
	// Exactly one updated proposal — the pending one, with new evidence.
	if result.UpdatedProposalCount != 1 {
		t.Fatalf("a recurrence must update the pending proposal; UpdatedProposalCount = %d",
			result.UpdatedProposalCount)
	}
	if len(enqueued.UpdatedProposals) != 1 {
		t.Fatalf("queue received %d updated proposals, want 1", len(enqueued.UpdatedProposals))
	}
	updated := enqueued.UpdatedProposals[0]
	// The merged evidence: the old record plus the two new ones.
	if len(updated.Finding.Evidence) != 3 {
		t.Errorf("updated proposal evidence = %d, want 3 (old + 2 new) — evidence attached, not duplicated",
			len(updated.Finding.Evidence))
	}
	if len(enqueued.Proposals) != 0 {
		t.Errorf("queue received %d fresh proposals, want 0 — no duplicate (SC-006)",
			len(enqueued.Proposals))
	}
}

// TestLoop_RejectionHonored proves FR-015 end-to-end: a recurring failure
// whose identical proposal an operator already rejected — with no new evidence
// — is not re-proposed.
func TestLoop_RejectionHonored(t *testing.T) {
	// The window: a recurring failure, two records.
	failRecs := []TelemetryRecord{
		rec("ci-1", SourceCI, 10, "failure", "rejected before", "076"),
		rec("ci-2", SourceCI, 20, "failure", "rejected before", "076"),
	}
	// The operator already rejected the identical-evidence proposal: build the
	// rejected proposal's finding from the SAME two records.
	rejectedFinding := finding(FindingRecurringFailure, "076", "rejected before",
		CategoryCodeGeneration, failRecs[0], failRecs[1])
	rejectedFinding.Occurrences = 2

	result, enqueued := runLoopCycle(t, LoopInput{
		Cycle: 3, Since: ts(0), Until: ts(60),
		LiveSpecRefs: []string{"076"},
		RejectedProposals: []SpecProposal{{
			ID: proposalIDForFinding(rejectedFinding), TargetSpec: "076",
			Finding: rejectedFinding, Status: StatusProposalRejected,
		}},
	}, failRecs)

	if result.NewProposalCount != 0 {
		t.Errorf("a rejected change with no new evidence must not be re-proposed; got %d",
			result.NewProposalCount)
	}
	if len(enqueued.Proposals) != 0 {
		t.Errorf("queue received %d proposals, want 0 — the rejection is honored (FR-015)",
			len(enqueued.Proposals))
	}
}
