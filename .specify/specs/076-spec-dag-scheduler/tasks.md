# Tasks: Spec-DAG Scheduler

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md)

## Format: `[ID] [P?] [Story] Description`

- **[P]** = parallelizable (different files, no incomplete dependency)
- **[US1/US2/US3]** = the user story a task serves (story phases only)

## Path Conventions

Packages within the spec-070 orchestrator module — the pure DAG library
`go/orchestrator/dag/`, the workflows `go/orchestrator/workflows/`, the
projection activity `go/orchestrator/activities/`. Depends on spec 075's
`go/orchestrator/driver/` registry and spec 077's `go/orchestrator/adapter/`.

---

## Phase 1: Setup (Shared Infrastructure)

- [ ] T001 Create the `go/orchestrator/dag/` package skeleton — `dag.go`, `acyclic.go`, `frontier.go` with package doc and exported stubs (plan.md Project Structure)
- [ ] T002 Create the scheduler workflow file skeleton at `go/orchestrator/workflows/scheduler.go` — package, imports, workflow registration stub (FR-006)
- [ ] T003 [P] Wire `workflowcheck` against `go/orchestrator/workflows/scheduler.go` in the orchestrator CI determinism gate (FR-005, SC-007)

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ The DAG library is pure and deterministic — no Temporal import. It blocks every user story.**

- [ ] T004 Implement the DAG node/edge types in `go/orchestrator/dag/dag.go` — `Node` (id, source spec/task ref, capability tag, priority, tier hint, target repo + base ref, worktree requirement, status), `Edge` (`depends_on`), `DAG`, and the `NodeStatus` enum `pending → runnable → running → done | failed | blocked-unroutable | blocked-dependency-failed` (FR-001, Key Entities: DAG Node, Dependency Edge, Node Status)
- [ ] T005 Implement the acyclic / cycle-detection check in `go/orchestrator/dag/acyclic.go` — rejects a non-acyclic graph and **names the cycle** in the error (FR-002)
- [ ] T006 Implement the runnable-frontier computation in `go/orchestrator/dag/frontier.go` — exactly the nodes whose every dependency is `done` (FR-003)
- [ ] T007 Implement the deterministic ordering in `go/orchestrator/dag/frontier.go` — priority descending, then a stable node-id tie-breaker; never map-iteration or insertion order (FR-004, Key Entities: Runnable Frontier)
- [ ] T008 [P] Exhaustively unit-test `go/orchestrator/dag/` in `acyclic_test.go` and `frontier_test.go` — boundaries: empty graph, single node, N nodes of equal priority, named-cycle assertion, frontier with all dependencies satisfied / partially satisfied / none (FR-002, FR-003, FR-004, SC-003)

**Checkpoint**: the DAG library compiles, orders deterministically, and names cycles — workflows can now build on it.

## Phase 3: User Story 1 — Deterministic scheduling from the spec graph (Priority: P1) 🎯 MVP

**Goal**: the scheduler runs as a durable Temporal workflow that, each tick, computes the runnable frontier, orders it deterministically, and dispatches each node to a capability-matched driver — replacing the kanban pull-loop.

**Independent test**: feed a fixed DAG with a known dependency structure; run a tick; confirm the frontier and dispatch order are exactly what the topological + priority ordering predicts; replay the tick and confirm identical decisions.

- [ ] T009 [US1] Implement the scheduler tick loop in `go/orchestrator/workflows/scheduler.go` — per tick: compute the runnable frontier (`dag.Frontier`), order it (`dag.Order`), dispatch each runnable node, update node states; using only workflow-deterministic time (FR-003, FR-004, FR-005)
- [ ] T010 [US1] Add Continue-As-New to the scheduler workflow in `go/orchestrator/workflows/scheduler.go` — bound history, carry forward the DAG + node states, lose no in-flight dispatch (FR-006, edge case: history limit)
- [ ] T011 [US1] Implement capability-based driver selection in the tick loop — for each runnable node, query the spec-075 registry by the node's required capability tag; selection deterministic (FR-007; 075 FR-005)
- [ ] T012 [US1] Mark a node with no satisfiable driver as `blocked-unroutable` naming the missing capability; the rest of the frontier still proceeds (FR-010; 075 FR-012; acceptance scenario 4)
- [ ] T013 [US1] Mark a node whose dependency permanently failed as `blocked-dependency-failed`; it never runs (FR-011; edge case: dependency permanently fails)
- [ ] T014 [US1] Enforce exactly-once dispatch in the tick loop — a node already `running` is not re-dispatched on a later tick (FR-009; edge case: child still running at next tick)
- [ ] T015 [P] [US1] Implement the per-node child work-unit workflow in `go/orchestrator/workflows/work_unit.go` — create a fresh worktree (spec-070 `worktree/`), invoke the driver via the spec-075 contract, tear the worktree down (FR-008; 070 FR-013/14)
- [ ] T016 [US1] Replay/determinism test for the scheduler in `go/orchestrator/workflows/scheduler_test.go` — Temporal `testsuite`; replaying a tick over the same DAG and node states yields identical dispatch decisions (FR-005; SC-001; acceptance scenario 3)
- [ ] T017 [US1] Run the scheduler beside the legacy `kanban-pull-loop` cron; confirm an equivalent, explainable work order over a soak; zero double-dispatch (FR-006; SC-002, SC-004)

**Checkpoint**: the scheduler is a durable, inspectable, replayable workflow — the MVP. The kanban pull-loop has a deterministic replacement.

## Phase 4: User Story 2 — Discovered work joins the graph (Priority: P2)

**Goal**: a running scheduler accepts new nodes/edges via a signal; the next tick recomputes the frontier including them; an append that would create a cycle is rejected; oversized discoveries are flagged for a spec amendment.

**Independent test**: run a scheduler over a DAG; send an append signal adding a node that depends on an in-flight node; confirm the new node becomes runnable only after its dependency completes.

- [ ] T018 [US2] Implement the append signal handler in `go/orchestrator/workflows/scheduler.go` — adds nodes/edges to the running DAG; the next tick's frontier honors their dependencies (FR-012; acceptance scenario 1)
- [ ] T019 [US2] Reject an append that would introduce a cycle — re-run `dag.Acyclic` against the candidate graph; the scheduler continues unaffected on rejection (FR-012; acceptance scenario 2)
- [ ] T020 [US2] Implement the spec-amendment flag for oversized discovered work in `go/orchestrator/workflows/scheduler.go` — discovered work past a size threshold is flagged for a spec amendment, not silently absorbed (FR-012; acceptance scenario 3)
- [ ] T021 [P] [US2] Append/cycle-rejection test in `go/orchestrator/workflows/scheduler_test.go` — appended node runnable only after its dependency completes; a cycle-introducing append rejected with the scheduler unaffected (FR-012; US2 acceptance scenarios)

**Checkpoint**: a running scheduler absorbs discovered work safely — no cycles, no silent scope creep.

## Phase 5: User Story 3 — One scheduler, any repo (Priority: P3)

**Goal**: the scheduler runs work over any target repository on any base ref; the target repo and base ref are inputs to the DAG and its work units, never hard-coded.

**Independent test**: run the scheduler against two DAGs whose work units target different repos / base refs; confirm each work unit's worktree is created from the correct repo at the correct base ref.

- [ ] T022 [US3] Carry target repository + base ref on each work unit — surface the `Node` fields (T004) through `WorkUnitInput` in `go/orchestrator/workflows/work_unit.go`; record the base ref on the run (FR-013; acceptance scenario 3)
- [ ] T023 [US3] Create every worktree from the work unit's named repo at its named base ref in `go/orchestrator/workflows/work_unit.go` — multi-repo worktree creation via the spec-070 `worktree/` package (FR-013; 070 FR-013; acceptance scenario 1)
- [ ] T024 [P] [US3] Two-repo isolation test in `go/orchestrator/workflows/work_unit_test.go` — two DAGs target distinct repos; confirm no work unit observes another repo's checkout (FR-013; SC-006; acceptance scenario 2)

**Checkpoint**: the same scheduler runs chitin-building-chitin and ReadyBench work with correct per-repo worktree isolation.

## Phase 6: Polish & Cross-Cutting

- [ ] T025 [P] Confirm `workflowcheck` passes on `go/orchestrator/workflows/scheduler.go` and `work_unit.go` — the determinism gate is green (FR-005; SC-007)
- [ ] T026 Implement the board-projection activity in `go/orchestrator/activities/board_projection.go` — projects node-state transitions to the Chitin Board read-model; the board reflects scheduler state, never drives it (FR-014; 070 FR-016)
- [ ] T027 [P] Emit the per-tick telemetry record from the scheduler — frontier, dispatches, driver selections and their reasons — to Chitin Telemetry (FR-015; 070 FR-008; Key Entities: Tick Record)
- [ ] T028 Implement the explicit stalled-graph state in `go/orchestrator/workflows/scheduler.go` — no runnable and no running nodes with undone nodes remaining surfaces as an explicit state, never a silent spin (FR-016; edge case: every node blocked)
- [ ] T029 Retire the `kanban-pull-loop` cron once the scheduler is proven beside it (plan.md Migration Phases; 070 SC-005)
- [ ] T030 Re-run the Constitution Check — all six principles still PASS post-implementation

---

## Dependencies

- **Phase 1 → Phase 2 → Phase 3**: Setup and the pure DAG library block all stories.
- **Phase 2 (the DAG library)** is the hard prerequisite — frontier + ordering must be deterministic and tested before any workflow builds on it.
- **US1 (P1)** is the MVP — independently shippable once Phases 1+2 are done; depends on spec 075's driver registry and spec 077's adapter.
- **US2 (P2)** depends on Phase 3 (the running scheduler workflow); the append signal extends it.
- **US3 (P3)** depends on Phase 3 (the work-unit workflow); independent of US2.
- Within a story: types/library before workflow; workflow before its replay/isolation test; run-beside before retire.

## Parallel Execution Examples

- Phase 1: T003 in parallel with T001/T002 (distinct concern — CI wiring).
- Phase 2: T008 follows T004–T007 but runs alongside no incomplete dependency once they land.
- Phase 3: T015 (the work-unit workflow file) in parallel with T009–T014 (the scheduler file) — distinct files.
- Phase 4: T021 in parallel with downstream work — distinct test file.
- Phase 6: T025 and T027 in parallel — distinct concerns/files.

## Implementation Strategy

**MVP = US1 (deterministic static-DAG scheduling).** Phase 1 + Phase 2 + Phase 3
deliver the pure DAG library plus a single durable, inspectable, replayable
scheduler workflow proven beside the `kanban-pull-loop` cron — that alone is
spec 070's FR-015 made real and the determinism-and-telemetry thesis proven.
US2 (discovered work) and US3 (any repo) are incremental; the cron is retired
only after the scheduler is proven (strangler-fig).
