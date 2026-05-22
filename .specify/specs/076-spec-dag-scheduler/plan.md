# Implementation Plan: Spec-DAG Scheduler

**Branch**: `076-spec-dag-scheduler` | **Date**: 2026-05-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/076-spec-dag-scheduler/spec.md`

## Summary

Make spec 070's FR-015 real: derive work sequencing **mathematically** from
the spec task graph — no heuristic optimizer, no human-managed kanban
deciding order. Specs are compiled into a dependency DAG; a deterministic
Temporal workflow walks it, computes the runnable frontier on each tick,
orders it by a named tie-breaker, and dispatches each runnable node to a
capability-matched driver (spec 075). It **replaces** the kanban pull-loop
outright — it is the P1 slice of spec 070, run beside the legacy
`kanban-pull-loop` cron until proven.

The DAG itself is a pure, deterministic library — node/edge types, the
acyclic check, the topological + priority ordering — separable from
Temporal and exhaustively unit-testable. The scheduler and the per-node
work-unit are durable Temporal workflows on top of it. The spec→DAG
compiler (the kit adapters) is spec 077; 076 owns the DAG's normalized
schema as the consumer contract.

## Technical Context

**Language/Version**: Go 1.23+ — matches the Chitin Kernel and the spec-070 orchestrator; the Temporal Go SDK is first-class.

**Primary Dependencies**: the Temporal Go SDK; spec 075's driver registry (`go/orchestrator/driver/`) for capability-based driver selection; spec 077's spec-kit adapter (`go/orchestrator/adapter/`) for compiling specs into the DAG; the spec-070 orchestrator's `worktree/` package and telemetry export.

**Storage**: Temporal's own persistence holds scheduler workflow state and history. Node-state transitions are **projected** to the Chitin Board read-model by an activity (070 FR-016) — the board is written, never read to decide what runs next. The chitin chain is likewise written by activities.

**Testing**: `go test`; the Temporal `testsuite` for replay/determinism tests of the scheduler workflow; `workflowcheck` in CI as the determinism gate.

**Target Platform**: Linux, single box (chimera-ant); self-hosted.

**Project Type**: Go packages within the spec-070 orchestrator module — a pure DAG library plus two workflows and a projection activity.

**Performance Goals**: Scheduling is low-throughput (ticks on the order of minutes). The goal is **determinism + replayability**, not QPS — the same DAG always produces the same work order (SC-001).

**Constraints**: The scheduler is a Temporal workflow — it MUST be deterministic: workflow-deterministic time only, never `time.Now`; all side effects in activities; Continue-As-New to bound history. Every work unit isolated in its own fresh git worktree (070 FR-013/14). Exactly-once dispatch per node.

**Scale/Scope**: One pure DAG package, two workflows (scheduler + work-unit), one projection activity; one operator, one box.

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

| Principle | Assessment |
|-----------|------------|
| §1 Side-effect boundary | PASS — the scheduler *coordinates*; it computes the frontier and ordering and nothing else. Every side effect (worktree create/teardown, driver invoke, board projection, telemetry, chain writes) runs in an activity, gated by the kernel. The workflow never bypasses the kernel to write chain events. |
| §2 Branch & worktree (amended: always workers + worktrees) | PASS — each dispatched node runs as a child work-unit workflow that creates its own **fresh** worktree and tears it down (FR-008, 070 FR-013/14); the shared checkout is never a work surface. |
| §3 Spec-kit promotion gate | PASS — 076 has `spec.md` + this `plan.md`; `tasks.md` follows. |
| §4 Tracked installers | N/A — 076 is library + workflow code *inside* the spec-070 orchestrator binary; it ships no standalone operator script. The orchestrator's own installer (070 §4) covers it. |
| §5 Board-aware scripts | PASS — 076 **writes** the board projection (FR-014) and never reads the board as a decision input; the frontier is computed purely from the DAG and node states. This is fully consistent with 070 FR-016 (the board is a read-projection, never a decider). |
| §6 Swarm tooling is the exception | PASS — the scheduler is genuine kernel-adjacent infra; it lives under `go/orchestrator/`, not `swarm/`. |

No violations → Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/076-spec-dag-scheduler/
├── plan.md          # This file
├── research.md      # Phase 0 — DAG ordering + Continue-As-New patterns
├── data-model.md    # Phase 1 — DAG / Node / Edge / Frontier / Tick Record entities
├── quickstart.md    # Phase 1 — compile a spec, run a tick, replay it
└── tasks.md         # Phase 2 — /speckit-tasks output
```

### Source Code (repository root)

```text
go/orchestrator/
├── dag/                       # pure, deterministic DAG library (no Temporal import)
│   ├── dag.go                 # Node / Edge / DAG types, Node Status enum
│   ├── acyclic.go             # cycle detection — names the cycle
│   ├── frontier.go            # runnable frontier + topological/priority ordering
│   └── *_test.go              # exhaustive unit tests (boundaries: 0, 1, N-equal)
├── workflows/
│   ├── scheduler.go           # the durable scheduler workflow — tick loop, Continue-As-New
│   ├── scheduler_test.go      # replay/determinism test (Temporal testsuite)
│   ├── work_unit.go           # per-node child workflow — worktree → driver → teardown
│   └── work_unit_test.go      # two-repo isolation test
└── activities/
    └── board_projection.go    # projects node-state transitions to the Chitin Board (FR-014)
```

**Structure Decision**: The DAG is a **pure library** under `go/orchestrator/dag/`
with no Temporal dependency — node/edge types, the acyclic check, and the
topological + priority ordering are deterministic functions, unit-tested in
isolation. The scheduler and work-unit are Temporal **workflows** under
`go/orchestrator/workflows/`, the projection a Temporal **activity** under
`go/orchestrator/activities/` — reusing the spec-070 module layout. Keeping
the DAG pure lets the determinism that matters most (frontier + ordering) be
proven by `go test` without a Temporal harness; `workflowcheck` then guards
the workflow layer.

## Migration Phases

The scheduler **replaces** the kanban pull-loop; it does not port it. It runs
beside the legacy `kanban-pull-loop` cron until proven (070 SC-005), then the
cron is retired. Each phase is shippable.

- **Phase 0 — Foundation.** Scaffold `go/orchestrator/dag/`; scheduler
  workflow file skeleton. Exit: the package compiles and `workflowcheck` is
  wired against the scheduler file.
- **Phase 1 — The DAG library.** Node/edge types, the acyclic check that
  names the cycle, the frontier + ordering with the named tie-breaker
  (priority desc, then node-id) — pure and exhaustively unit-tested. Exit:
  ordering is deterministic across boundaries (0, 1, N-equal-priority).
- **Phase 2 — Static-DAG scheduling (US1, P1 — the MVP).** The scheduler
  tick loop (frontier → order → dispatch → update → Continue-As-New); the
  per-node work-unit workflow (worktree → driver → teardown); the
  replay/determinism test. Run beside `kanban-pull-loop`. Exit: replay yields
  identical decisions; exactly-once dispatch holds.
- **Phase 3 — Discovered work (US2, P2).** The append signal; cycle
  rejection on append; the spec-amendment flag for oversized discoveries.
- **Phase 4 — Any repo (US3, P3).** Target repo + base ref on work units;
  multi-repo worktree creation; the two-repo isolation test.
- **Phase 5 — Polish.** `workflowcheck` green; the board-projection activity;
  per-tick telemetry; the explicit stalled-graph state; retire the cron;
  re-run the Constitution Check.

## Complexity Tracking

None — no constitution violations to justify.
