package loop

import (
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
)

// Deps are the runtime dependencies the self-improvement loop's activities
// need bound at worker-host startup (spec 078). They are constructed in main
// once the telemetry read surface and the proposal-queue store exist, then
// handed to Register.
//
// Every field is OPTIONAL: a nil field degrades to a safe default rather than
// failing the worker host — consistent with the orchestrator's other Register
// helpers (activities.RegisterSchedulerActivities). A loop run with all
// defaults reads no telemetry (every source unreachable → empty cycle) and
// logs its proposals; it never crashes.
type Deps struct {
	// Readers is the per-layer telemetry read surface — one TelemetryReader
	// per source the loop ingests (spec 078 FR-002). A source with no reader
	// is an unreachable layer and contributes nothing (FR-002 edge case). Nil
	// or partial is fine.
	//
	// TODO(spec-078-US1/T009): main builds the concrete per-layer readers — a
	// gov-decisions chain reader, a Temporal run-history reader, a CI/bench/PR
	// reader against the Chitin Telemetry layer — and passes them here. Until
	// then a nil Readers yields empty cycles, which is a safe no-op loop.
	Readers []TelemetryReader
	// ProposalQueue is the write-only sink the loop projects its proposals to
	// (spec 078 FR-013). A nil ProposalQueue falls back to the logging sink.
	ProposalQueue ProposalSink
}

// Register wires the self-improvement loop into the orchestrator worker host:
// it registers the ImprovementLoopWorkflow and the loop's activities. It is
// the loop's half of spec 078 — the workflow and activity names the loop
// dispatches to.
//
// Activities registered:
//
//   - "IngestTelemetry"      — per-source telemetry read into the cycle's
//     window (spec 078 FR-002).
//   - "ProjectProposalQueue" — the cycle's proposals projected to the queue
//     read-model (spec 078 FR-013).
//
// Workflow registered:
//
//   - ImprovementLoopWorkflow — the durable on-demand loop cycle (US1).
//
// main calls Register once, alongside activities.Register and
// activities.RegisterSchedulerActivities. Register is intentionally separate
// because the loop's activities carry startup-bound dependencies (the
// telemetry readers, the proposal-queue sink) the smoke activities do not —
// the same split spec 076 makes between Register and RegisterSchedulerActivities.
//
// Register does NOT touch cmd/chitin-orchestrator/main.go — wiring Register
// into main is done separately (spec 078 plan: the loop ships as library +
// workflow code inside the orchestrator binary).
func Register(w worker.Worker, deps Deps) {
	w.RegisterWorkflow(ImprovementLoopWorkflow)

	ingest := NewIngestActivity(deps.Readers)
	w.RegisterActivityWithOptions(ingest.Execute, activity.RegisterOptions{
		Name: ingest.ActivityName(),
	})

	queue := NewQueueActivity(deps.ProposalQueue)
	w.RegisterActivityWithOptions(queue.Execute, activity.RegisterOptions{
		Name: queue.ActivityName(),
	})

	// TODO(spec-078-US1/T016): register a "ScanSpecCatalog" activity that
	// scans specs/ for the live, non-superseded spec set, backing the
	// stale-spec rule for live operation (resolveSpecCatalog in workflow.go).
	// US1 supplies LiveSpecRefs on LoopInput, so the stale-spec edge case is
	// already exercised without this activity.
}
