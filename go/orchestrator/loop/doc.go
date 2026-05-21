// Package loop is the Chitin self-improvement loop (spec 078) — the platform's
// thesis made literal: chitin is a self-improvement loop, not a code factory.
//
// The loop's irreducible arc is telemetry → analysis → finding → spec proposal
// → [human gate]. Cross-layer telemetry — governance decisions, orchestrator
// run history, CI outcomes, bench results, PR outcomes, agent run telemetry —
// accumulates as the swarm works; the loop ingests a checkpoint-bounded slice
// of it, analyzes it for recurring failures and missed opportunities, and
// emits an evidence-backed SpecProposal: a concrete, reviewable change against
// a named chitin spec. The proposal is queued for the operator and NEVER
// applied — the human gate is absolute (spec 078 FR-005). Autonomy lives in
// analysis and proposal generation; authority stays with the human.
//
// # Layering
//
// The package keeps a hard line between PURE code and Temporal code, mirroring
// the spec-076 dag/ library:
//
//   - window.go, finding.go, proposal.go, category.go, analysis.go are PURE —
//     no Temporal import, no wall clock, no I/O. They are the loop's detection
//     and proposal logic and are exhaustively unit-tested by plain `go test`.
//   - ingest.go and queue.go are Temporal ACTIVITIES — telemetry reads and the
//     proposal-queue projection are side effects (spec 078 plan Constraints).
//   - workflow.go is the durable Temporal WORKFLOW — strictly deterministic:
//     workflow-deterministic time only, never time.Now; every side effect in
//     an activity. workflowcheck (workflowcheck.config.yaml) guards it.
//
// # Scope
//
// This package implements spec 078's foundational tasks and User Story 1 (the
// P1 MVP): one on-demand loop cycle that turns a fixed telemetry window into a
// single evidence-backed, operator-queued spec proposal. The deterministic-
// review tier (US2) and continuous scheduling (US3) are documented TODOs —
// see the TODO(spec-078-US2) / TODO(spec-078-US3) markers in this package.
//
// The loop generalizes Sentinel (spec 064): Sentinel's single arc — ingest
// telemetry → analyze → mine governance-policy proposals — becomes one
// configured instance of this loop rather than a parallel pipeline (FR-018).
// Folding Sentinel in is a Phase-6 task, also a documented TODO here.
package loop
