# Implementation Plan: Self-Improvement Loop

**Branch**: `078-self-improvement-loop` | **Date**: 2026-05-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/078-self-improvement-loop/spec.md`

## Summary

Make the platform's thesis literal: chitin is a **self-improvement loop**,
not a code factory. Telemetry from every layer → analysis → findings →
spec proposals → [human gate] → implementation via the orchestrator → new
telemetry. The loop runs as durable Temporal workflows + activities inside
the spec-070 orchestrator module — a scheduled cycle that ingests the
telemetry accumulated since its last checkpoint, analyzes it for recurring
failures and missed opportunities, and emits an evidence-backed **spec
proposal** the operator reviews and approves before anything is built.

The loop **generalizes Sentinel (spec 064)**: Sentinel's single arc —
ingest telemetry → analyze → mine governance-policy proposals — becomes one
configured instance of the loop, not a parallel pipeline. The loop mines
improvements to *any* spec from telemetry *anywhere*.

The economics that make it run *continuously* come from the 076 node split:
every loop step with a mappable decision tree (review a PR against
acceptance criteria, check code against a deterministic spec, scan a
telemetry window for anomalies) runs as a `deterministic` node or via the
spec-075 local-LLM driver — a frontier agent is reserved for the one
genuinely ambiguous step, synthesizing proposal prose. The loop never
self-applies; it produces proposals and stops at the human gate (FR-005).

## Technical Context

**Language/Version**: Go 1.25+ — matches the Chitin Kernel and the spec-070 orchestrator module; the Temporal Go SDK is first-class.

**Primary Dependencies**: the Temporal Go SDK; the spec-070 orchestrator (`go/orchestrator/` — worker host, `worktree/`, telemetry export); the spec-076 scheduler/DAG for `agent`/`deterministic` node execution of loop steps; the spec-075 driver registry, including the local-LLM driver for the small-model tier; the Chitin Telemetry layer as the read surface for all cross-layer ingest.

**Storage**: Temporal's own persistence holds loop-workflow state and history. The proposal queue and cycle checkpoints are **projected** to a durable read-model by activities (consistent with 070 FR-016 — written, never read back to decide what runs next). The chitin chain and Chitin Telemetry are written by activities. Telemetry is *read*, not owned (spec assumption — the loop is a consumer).

**Testing**: `go test`; the Temporal `testsuite` for replay/determinism tests of the loop workflow; `workflowcheck` in CI as the determinism gate.

**Target Platform**: Linux, single box (chimera-ant); self-hosted.

**Project Type**: Go packages within the spec-070 orchestrator module — a new `go/orchestrator/loop/` package (telemetry-ingest activities, analysis, proposal generation) plus the loop workflow and a proposal-queue projection activity.

**Performance Goals**: The loop is low-throughput (a cycle on the order of a cadence interval). The goal is **affordability + determinism** — every mappable step runs at zero frontier-token cost (SC-003, SC-004), and the loop runs continuously for 7 days without overlap or skipped windows (SC-005).

**Constraints**: The loop workflow is a Temporal workflow — it MUST be deterministic: workflow-deterministic time only, never `time.Now`; all side effects (telemetry reads, proposal-queue writes, chain writes) in activities; Continue-As-New to bound the always-on cycle's history. Cycles MUST NOT overlap (FR-012). The loop only ever proposes within gate-able net-positive categories (FR-007); a proposal for a dangerous or ungated category is refused at synthesis.

**Scale/Scope**: One loop workflow (scheduled, Continue-As-New), the `loop/` package (ingest + analysis + proposal generation), one proposal-queue projection activity; one operator, one box. Sentinel becomes one configuration of this loop.

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

| Principle | Assessment |
|-----------|------------|
| §1 Side-effect boundary | PASS — the loop produces **proposals**, never self-applies (FR-003, FR-005). It *analyzes* and *proposes*; every side effect (telemetry reads, proposal-queue projection, chain writes) runs in an activity. Implementation of an approved proposal flows through the human gate, then the orchestrator and the spec-076 scheduler (FR-006) — the loop has no side channel into the codebase. The kernel still gates every agent action the loop's approved work later triggers. |
| §2 Branch & worktree (amended: always workers + worktrees) | PASS — the loop is orchestrator workflows; any work it eventually causes runs through spec-070's worktree isolation (070 FR-013/14). The loop itself spawns no work surface. |
| §3 Spec-kit promotion gate | PASS — 078 has `spec.md` + this `plan.md`; `tasks.md` follows. |
| §4 Tracked installers | N/A — 078 is library + workflow code *inside* the spec-070 orchestrator binary; it ships no standalone operator script. The orchestrator's own installer (070 §4) covers it. |
| §5 Board-aware scripts | N/A — 078 ships no kanban-touching swarm script. The proposal queue is a loop-owned read-model, not the Chitin Board. |
| §6 Swarm tooling is the exception | PASS — the loop is genuine kernel-adjacent infra; it lives under `go/orchestrator/`, not `swarm/`. |

No violations → Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/078-self-improvement-loop/
├── plan.md          # This file
├── research.md      # Phase 0 — telemetry-ingest + Continue-As-New patterns; Sentinel-as-config
├── data-model.md    # Phase 1 — Telemetry Window / Finding / Spec Proposal / Cycle Checkpoint entities
├── quickstart.md    # Phase 1 — run one loop cycle over a fixed telemetry window, inspect the proposal
└── tasks.md         # Phase 2 — /speckit-tasks output
```

### Source Code (repository root)

```text
go/orchestrator/
├── loop/                       # the self-improvement loop — pure where it can be
│   ├── window.go               # Telemetry Window — checkpoint-bounded slice; cross-layer source set
│   ├── ingest.go               # telemetry-ingest activities — one per layer (governance, runs, CI, bench, PR, agent)
│   ├── analysis.go             # finding detection — recurring failure / missed opportunity / regression passes
│   ├── finding.go              # Finding type — observation + evidence records; duplicate/regression matching
│   ├── proposal.go             # Spec Proposal type — concrete diff against a named spec + finding + evidence
│   ├── category.go             # the closed Gate-able Category set — synthesis refuses outside it
│   └── *_test.go               # unit tests (boundaries: empty window, duplicate finding, stale spec, regression)
├── workflows/
│   ├── improvement_loop.go     # the durable loop workflow — scheduled cycle, Continue-As-New, no-overlap
│   └── improvement_loop_test.go# replay/determinism + empty-cycle + no-overlap tests (Temporal testsuite)
└── activities/
    └── proposal_queue.go       # projects proposals + cycle checkpoints to the loop read-model (FR-013)
```

**Structure Decision**: A new `go/orchestrator/loop/` package beside the
spec-076 `dag/` library, reusing the spec-070 module layout. Analysis and
finding/proposal logic are kept **pure** (no Temporal import) so the
detection passes and the duplicate/regression/stale matching can be
exhaustively unit-tested by `go test` without a Temporal harness. The
telemetry-ingest steps and the loop itself are Temporal **activities** and
a **workflow**; `workflowcheck` guards the workflow layer. The loop's
review steps are dispatched as spec-076 `deterministic` nodes or
small-model invocations — the loop *configures* the 076 scheduler, it does
not re-implement node execution.

## Implementation Phases

The loop is built smallest-arc-first: P1 closes the loop once (one
on-demand cycle), P2 makes it affordable (the deterministic tier), P3
makes it continuous. Each phase is shippable.

- **Phase 0 — Foundation.** Scaffold `go/orchestrator/loop/`; the loop
  workflow file skeleton; wire `workflowcheck` against it. Exit: the
  package compiles, the determinism gate is wired.
- **Phase 1 — The loop's irreducible core (US1, P1 — the MVP).** The
  cross-layer telemetry-ingest activities, the analysis passes, the
  Finding type, the Spec Proposal type with its evidence, and a single
  on-demand loop workflow: telemetry window → analysis → finding → one
  evidence-backed proposal, queued for the operator, never applied.
  Includes duplicate-suppression and the stale-spec / rejection-record
  rules. Exit: a fixed window with a known recurring failure emits exactly
  one grounded proposal (SC-001, SC-002).
- **Phase 2 — The deterministic tier (US2, P2).** Route every mappable
  review step (PR-against-spec, code-against-deterministic-spec, telemetry
  anomaly scan, e2e) as a spec-076 `deterministic` node or a spec-075
  local-LLM-driver invocation; reserve the frontier agent for
  proposal-prose synthesis alone; the unmapped-input escalation path. Exit:
  every mappable step runs at zero frontier-token cost (SC-003, SC-004).
- **Phase 3 — Continuous operation (US3, P3).** Schedule the loop on a
  cadence; per-cycle checkpoint advance (including empty cycles);
  Continue-As-New; the no-overlap guard; regression detection on prior
  approved-and-implemented proposals. Exit: a 7-day soak with no overlap
  and no skipped window (SC-005, SC-008).
- **Phase 4 — Sentinel generalization & polish.** Re-express Sentinel's
  ingest → analyze → policy-proposal arc as one configuration of the loop;
  retire the parallel Sentinel-only pipeline; per-cycle self-telemetry
  emission; `workflowcheck` green; re-run the Constitution Check. Exit:
  no remaining Sentinel-only pipeline (SC-007).

## Complexity Tracking

None — no constitution violations to justify.
