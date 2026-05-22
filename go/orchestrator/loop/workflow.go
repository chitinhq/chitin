package loop

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// WorkflowName is the stable Temporal workflow type name the loop workflow
// registers under — the name an operator or a starter dispatches a loop cycle
// by.
const WorkflowName = "ImprovementLoopWorkflow"

// ingestActivityTimeout bounds one IngestTelemetry activity — a read against
// one telemetry layer. Generous enough for a slow layer, bounded so an
// unresponsive layer is failed (and swallowed into an empty contribution)
// rather than hanging the cycle.
const ingestActivityTimeout = 2 * time.Minute

// queueActivityTimeout bounds the ProjectProposalQueue activity — a write to
// the queue read-model, off the critical path.
const queueActivityTimeout = 1 * time.Minute

// LoopInput is the typed input to ImprovementLoopWorkflow — one on-demand
// self-improvement cycle (spec 078 US1).
//
// US1 is a single on-demand cycle: the operator (or a test) supplies the
// window bounds, the prior cycle's pending findings and rejected proposals,
// and the cycle counter. The workflow ingests, analyzes, synthesizes, and
// queues — then stops at the human gate.
//
// TODO(spec-078-US3): the continuous loop carries the CycleCheckpoint, the
// pending-finding set, and the rejected set forward across Continue-As-New
// rather than receiving them as input each cycle (FR-011, T023/T024). LoopInput
// already carries those fields so the US3 change is additive — a scheduled
// cycle hands its successor an updated LoopInput.
type LoopInput struct {
	// Cycle is the cycle counter for this run — correlates the window, the
	// findings, and the proposals to the cycle that produced them (FR-013).
	Cycle int `json:"cycle"`
	// Since is the exclusive lower bound of the telemetry window — the prior
	// cycle's checkpoint. Zero means "from the beginning" (the first cycle).
	Since time.Time `json:"since"`
	// Until is the inclusive upper bound of the telemetry window — this
	// cycle's start. Zero means "up to the latest telemetry".
	Until time.Time `json:"until"`
	// PendingFindings is the set of findings whose proposals are still pending
	// from earlier cycles, for duplicate suppression (spec 078 FR-014). Empty
	// on a first cycle.
	PendingFindings []Finding `json:"pending_findings"`
	// RejectedProposals is the set of operator-rejected proposals, so the loop
	// does not re-propose an identical change without new evidence (FR-015).
	RejectedProposals []SpecProposal `json:"rejected_proposals"`
	// LiveSpecRefs is the set of spec refs that exist and are current — the
	// stale-spec rule's input (spec 078 edge case). Empty means the workflow
	// runs the spec-catalog activity to discover them; a non-empty slice is
	// used directly (the path tests take).
	LiveSpecRefs []string `json:"live_spec_refs"`
}

// LoopResult is the typed output of one loop cycle — what the cycle produced,
// and the proof it stopped at the human gate (spec 078 FR-005, US1 acceptance
// scenario 3).
type LoopResult struct {
	// Cycle echoes the cycle counter.
	Cycle int `json:"cycle"`
	// WindowRecordCount is how many telemetry records the cycle ingested.
	WindowRecordCount int `json:"window_record_count"`
	// FindingCount is how many findings the analysis produced.
	FindingCount int `json:"finding_count"`
	// ProposalIDs is the ids of the proposals the cycle QUEUED — new and
	// evidence-updated. Every one is pending (FR-005); none was applied.
	ProposalIDs []string `json:"proposal_ids"`
	// NewProposalCount is the count of freshly-produced proposals.
	NewProposalCount int `json:"new_proposal_count"`
	// UpdatedProposalCount is the count of pending proposals whose evidence
	// grew this cycle because a finding recurred (spec 078 FR-014).
	UpdatedProposalCount int `json:"updated_proposal_count"`
	// RefusedCount is the count of findings synthesis refused — out-of-
	// category, rejection-blocked (spec 078 FR-007, FR-015).
	RefusedCount int `json:"refused_count"`
	// StaleCount is the count of findings marked stale — they targeted a
	// missing or superseded spec (spec 078 edge case).
	StaleCount int `json:"stale_count"`
	// EmptyCycle is true when the cycle produced no proposal — a valid
	// outcome; silence is recorded, the checkpoint advances (US3 scenario 4).
	EmptyCycle bool `json:"empty_cycle"`
	// Checkpoint is the advanced cycle checkpoint — its At is the window's
	// Until, carried for the next cycle (spec 078 FR-011). US1 echoes it;
	// US3 carries it across Continue-As-New.
	Checkpoint CycleCheckpoint `json:"checkpoint"`
	// HumanGate is always true — the loop ALWAYS stops at the human gate;
	// nothing in code, policy, or configuration was changed (spec 078 FR-005).
	// It is asserted in the result so a caller (and a test) can prove it.
	HumanGate bool `json:"human_gate"`
}

// ImprovementLoopWorkflow is the durable self-improvement loop workflow — one
// on-demand cycle of spec 078's irreducible arc: telemetry ingest → analysis →
// finding → spec-proposal synthesis → enqueue, stopping at the human gate
// (spec 078 FR-001, FR-003, FR-005; US1).
//
// What the cycle does, in order:
//
//  1. Ingest — dispatch one IngestTelemetry activity per telemetry source
//     (AllSources) and merge every contribution into the cycle's window. A
//     missing or unreachable source is an empty contribution, never a failure
//     (spec 078 FR-002, edge case).
//  2. Analyze — run the deterministic analyzer over the window. The analyzer
//     is PURE (no Template, no I/O, no clock), so it runs directly in workflow
//     code and stays replay-deterministic (spec 078 FR-008).
//  3. Suppress & filter — drop findings duplicating a still-pending proposal
//     (attaching their evidence instead — FR-014) and findings a prior
//     rejection forbids without new evidence (FR-015).
//  4. Synthesize — turn each surviving finding into a SpecProposal; refuse an
//     out-of-category finding, mark stale a finding against a dead spec
//     (FR-003, FR-007, edge case).
//  5. Enqueue — project the cycle's proposals to the queue read-model via the
//     ProjectProposalQueue activity (FR-013).
//
// The cycle then ENDS — at the human gate. It never implements a proposal:
// every proposal is queued StatusProposalPending, and nothing in code, policy,
// or configuration is changed (spec 078 FR-005, SC-002). An approved proposal
// is implemented separately, through the orchestrator and the spec-076
// scheduler — the loop has no side channel into the codebase (FR-006).
//
// Determinism (spec 078 plan Constraints): ImprovementLoopWorkflow is strictly
// deterministic. It reads NO wall clock — the window bounds are workflow input
// (US1) or workflow.Now (US3, a deterministic source). Every side effect —
// telemetry reads, the queue projection, the spec-catalog scan — runs in an
// activity. The analyzer, the synthesis, and the duplicate/rejection filters
// are pure functions over the activity results, so a replay re-derives the
// identical proposals. workflowcheck (workflowcheck.config.yaml) guards it.
//
// TODO(spec-078-US3): make this workflow schedulable on a cadence and add
// Continue-As-New to bound an always-on run's history, with the no-overlap
// guard so a cadence firing while a cycle runs waits rather than double-
// ingesting (FR-011, FR-012; T023–T025). US1 is one on-demand cycle: the
// workflow runs once and returns.
func ImprovementLoopWorkflow(ctx workflow.Context, in LoopInput) (LoopResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("loop: cycle starting", "cycle", in.Cycle)

	// --- 1. Ingest the telemetry window ------------------------------------
	window := TelemetryWindow{Since: in.Since, Until: in.Until}
	ingested, ingestErr := ingestAllSources(ctx, in, &window)
	if ingestErr != nil {
		// ingestAllSources never returns an error for an unreachable layer —
		// only for a non-retryable malformed input. Surface that.
		return LoopResult{}, ingestErr
	}
	logger.Info("loop: telemetry ingested",
		"cycle", in.Cycle, "records", len(window.Records), "sources", ingested)

	// --- 2. Analyze the window (PURE — runs in workflow code) ---------------
	// The analyzer has no Temporal import, no I/O, no clock: it is a pure
	// function of the window, so running it in workflow code is replay-safe
	// (spec 078 FR-008; plan Structure Decision: analysis is pure).
	analyzer := NewDeterministicAnalyzer(nil)
	detected := analyzer.Analyze(window)
	logger.Info("loop: analysis complete", "cycle", in.Cycle, "findings", len(detected))

	// --- 3. Suppress duplicates & honor rejections (PURE) ------------------
	// A finding duplicating a still-pending proposal is NOT re-proposed — its
	// evidence is merged into the pending finding (spec 078 FR-014).
	fresh, updatedFindings := SuppressDuplicates(detected, in.PendingFindings)
	// A finding a prior rejection forbids (no new evidence) is dropped (FR-015).
	rejected := NewRejectedSet(in.RejectedProposals)
	fresh = rejected.FilterReProposable(fresh)

	// --- 4. Synthesize proposals -------------------------------------------
	// The spec catalog backs the stale-spec rule. US1 takes the live-refs
	// from input when supplied; otherwise the spec-catalog activity scans for
	// them. Either way the catalog is a value the pure synthesis consumes.
	catalog, catErr := resolveSpecCatalog(ctx, in)
	if catErr != nil {
		return LoopResult{}, catErr
	}

	result := LoopResult{Cycle: in.Cycle, WindowRecordCount: len(window.Records),
		FindingCount: len(detected), HumanGate: true}
	var newProposals []SpecProposal

	for _, f := range fresh {
		// Proposal-prose synthesis is the ONE frontier-eligible step
		// (spec 078 FR-009). US1 uses the deterministic StructuredProse floor;
		// the frontier ProseSynthesizer plugs in here behind the same seam.
		// TODO(spec-078-US1/T012): drive a frontier-agent ProseSynthesizer via
		// the spec-075 driver registry for richer proposal prose.
		res := SynthesizeProposal(f, catalog, rejected, StructuredProse{}, in.Cycle)
		switch {
		case res.Produced:
			newProposals = append(newProposals, res.Proposal)
		case res.Stale:
			result.StaleCount++
			logger.Info("loop: finding marked stale", "cycle", in.Cycle,
				"finding", f.Identity(), "reason", res.Reason)
		case res.Refused:
			result.RefusedCount++
			logger.Info("loop: finding refused at synthesis", "cycle", in.Cycle,
				"finding", f.Identity(), "reason", res.Reason)
		}
	}

	// An updated finding's pending proposal gets its new evidence re-attached —
	// the SAME proposal id, never a duplicate (spec 078 FR-014, SC-006).
	var updatedProposals []SpecProposal
	for _, f := range updatedFindings {
		// Re-synthesize against the merged finding: same id (from Identity),
		// updated evidence. Stale/refused verdicts still apply.
		res := SynthesizeProposal(f, catalog, rejected, StructuredProse{}, in.Cycle)
		if res.Produced {
			updatedProposals = append(updatedProposals, res.Proposal)
		}
	}

	SortProposals(newProposals)
	SortProposals(updatedProposals)
	result.NewProposalCount = len(newProposals)
	result.UpdatedProposalCount = len(updatedProposals)
	result.EmptyCycle = len(newProposals) == 0 && len(updatedProposals) == 0

	// --- 5. Enqueue for the operator — the human gate ----------------------
	// The cycle ends at QUEUED. Every proposal is StatusProposalPending; the
	// loop applies nothing (spec 078 FR-005, SC-002). An approved proposal is
	// implemented separately via the orchestrator + spec-076 scheduler (FR-006)
	// — there is no implement step in this workflow, by design.
	if err := enqueueProposals(ctx, in.Cycle, newProposals, updatedProposals, result.EmptyCycle); err != nil {
		return LoopResult{}, err
	}

	for _, p := range newProposals {
		result.ProposalIDs = append(result.ProposalIDs, p.ID)
	}
	for _, p := range updatedProposals {
		result.ProposalIDs = append(result.ProposalIDs, p.ID)
	}
	sortStrings(result.ProposalIDs)

	// The checkpoint advances to the window's upper bound — including for an
	// empty cycle, because silence still consumed the window (spec 078
	// FR-011, US3 scenario 4). US1 echoes it; US3 carries it across CAN.
	result.Checkpoint = CycleCheckpoint{At: in.Until, Cycle: in.Cycle}

	logger.Info("loop: cycle complete — proposals queued for operator, human gate held",
		"cycle", in.Cycle, "new", result.NewProposalCount, "updated", result.UpdatedProposalCount,
		"refused", result.RefusedCount, "stale", result.StaleCount, "empty", result.EmptyCycle)
	return result, nil
}

// ingestAllSources dispatches one IngestTelemetry activity per telemetry
// source (AllSources, a fixed deterministic order) and merges each
// contribution into the window. It returns the count of sources that were
// reachable.
//
// A single source's ingest fault — an unreachable layer, a reader error — is
// SWALLOWED into an empty contribution: one dead source must never fail the
// whole cycle (spec 078 FR-002, edge case). The function returns a non-nil
// error only for a non-retryable malformed input, which cannot happen here —
// it is kept in the signature so the caller's error path is explicit.
func ingestAllSources(ctx workflow.Context, in LoopInput, window *TelemetryWindow) (int, error) {
	logger := workflow.GetLogger(ctx)
	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: ingestActivityTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})

	reachable := 0
	// AllSources is a fixed sorted slice — iterating it is deterministic.
	for _, source := range AllSources {
		var res IngestResult
		err := workflow.ExecuteActivity(actx, "IngestTelemetry", IngestInput{
			Source: source,
			Window: TelemetryWindow{Since: in.Since, Until: in.Until},
		}).Get(actx, &res)
		if err != nil {
			// A reachable layer that genuinely faulted, or an unreachable one:
			// either way, an empty contribution — the cycle proceeds. One dead
			// source never fails the cycle (spec 078 FR-002 edge case).
			logger.Warn("loop: telemetry source ingest faulted — empty contribution",
				"cycle", in.Cycle, "source", source, "err", err)
			continue
		}
		if res.Reachable {
			reachable++
		}
		window.Merge(res.Records)
	}
	return reachable, nil
}

// resolveSpecCatalog returns the SpecCatalog backing the stale-spec rule. When
// LoopInput carries an explicit LiveSpecRefs set, it is used directly — the
// path US1 tests take. Otherwise the spec-catalog activity scans for the live
// spec set.
//
// TODO(spec-078-US1/T016): implement and dispatch a "ScanSpecCatalog" activity
// that reads specs/ for the live, non-superseded spec set. US1 supplies
// LiveSpecRefs on input so the stale-spec edge case is provable today; the
// activity is the live-operation path.
func resolveSpecCatalog(_ workflow.Context, in LoopInput) (SpecCatalog, error) {
	if len(in.LiveSpecRefs) > 0 {
		return NewStaticSpecCatalog(in.LiveSpecRefs), nil
	}
	// No explicit live-refs and no catalog activity yet: a nil catalog is
	// permissive (every spec assumed live). The stale-spec rule is exercised
	// by supplying LiveSpecRefs; live operation gets the scan activity above.
	return nil, nil
}

// enqueueProposals projects a cycle's proposals to the queue read-model via
// the ProjectProposalQueue activity (spec 078 FR-013). The queue is write-only
// (070 FR-016); a queue write fault IS surfaced — losing the loop's output
// silently would defeat the cycle — so unlike telemetry, this is on the
// cycle's critical path.
func enqueueProposals(
	ctx workflow.Context, cycle int,
	newProposals, updatedProposals []SpecProposal, empty bool,
) error {
	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: queueActivityTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	err := workflow.ExecuteActivity(actx, "ProjectProposalQueue", ProposalQueueInput{
		Cycle:            cycle,
		Proposals:        newProposals,
		UpdatedProposals: updatedProposals,
		EmptyCycle:       empty,
	}).Get(actx, nil)
	if err != nil {
		return fmt.Errorf("loop: enqueueing cycle %d proposals: %w", cycle, err)
	}
	return nil
}
