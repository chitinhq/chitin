# Tasks: Chitin Orchestrator

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md)

## Format: `[ID] [P?] [Story] Description`

- **[P]** = parallelizable (different files, no incomplete dependency)
- **[US1/US2/US3]** = the user story a task serves (story phases only)

## Path Conventions

New Go module `go/orchestrator/` — `cmd/chitin-orchestrator/`, `workflows/`,
`activities/`, `worktree/`, `telemetry/`. Installer + unit under `swarm/`.

---

## Phase 1: Setup (Shared Infrastructure)

- [ ] T001 Create the `go/orchestrator/` module structure (`cmd/chitin-orchestrator/`, `workflows/`, `activities/`, `worktree/`, `telemetry/`) per plan.md
- [ ] T002 Add the Temporal Go SDK dependency to `go/orchestrator/go.mod`
- [ ] T003 [P] Add `swarm/systemd/temporal-dev.service` — `temporal server start-dev` as a user unit (quickstart §1)
- [ ] T004 [P] Wire `workflowcheck` (go.temporal.io/sdk/contrib/tools/workflowcheck) as a CI determinism gate (research D4)

## Phase 2: Foundational (Blocking Prerequisites)

- [ ] T005 Implement the worker-host entrypoint in `go/orchestrator/cmd/chitin-orchestrator/main.go` — register workflows + activities, poll the `chitin` task queue (FR-011)
- [ ] T006 [P] Implement the `go/orchestrator/worktree/` package — create / teardown / GC a dedicated git worktree per work unit (FR-013, FR-014)
- [ ] T007 [P] Implement OTel telemetry export to Chitin Telemetry in `go/orchestrator/telemetry/` (FR-008)
- [ ] T008 [P] Implement the activity base in `go/orchestrator/activities/base.go` — retry-policy + timeout conventions + idempotency helpers (FR-004, FR-005)
- [ ] T009 Implement the Migration register (legacy → workflow → status) in `go/orchestrator/activities/migration_register.go` (data-model.md)
- [ ] T010 Implement a hello-world workflow + its replay test in `go/orchestrator/workflows/hello.go` — the Phase 0 exit check (quickstart §3)
- [X] T011 Create `swarm/bin/install-chitin-orchestrator.sh` — idempotent installer + `swarm/systemd/chitin-orchestrator.service` (constitution §4)

## Phase 3: User Story 1 — Orchestration you can see and trust (Priority: P1) 🎯 MVP

**Goal**: the kanban pull-loop runs as a durable, inspectable, replayable workflow.
**Independent test**: run beside the existing cron one day; every tick inspectable + replayable; equivalent board mutations.

- [ ] T012 [US1] Implement the pull-loop workflow in `go/orchestrator/workflows/pull_loop.go` — Continue-As-New for the never-ending loop (research D2, FR-009)
- [ ] T013 [P] [US1] Implement the pull-loop activities (read board, claim/advance ticket, via hermes/kanban — kernel-gated) in `go/orchestrator/activities/pull_loop.go`
- [ ] T014 [US1] Workflow replay/determinism test for the pull-loop in `go/orchestrator/workflows/pull_loop_test.go` (Temporal testsuite — FR-003)
- [ ] T015 [US1] Run the pull-loop workflow beside the existing `kanban-pull-loop` cron; verify equivalent board mutations (FR-006)
- [ ] T016 [US1] Confirm every tick is inspectable + replayable in the Temporal UI over a 7-day soak (acceptance scenarios 1–3, SC-005)
- [ ] T017 [US1] Retire the `kanban-pull-loop` cron once proven; update the Migration register (FR-007)

## Phase 4: User Story 2 — Deterministic failure recovery (Priority: P2)

**Goal**: the dispatch pipeline is a durable workflow — a mid-flight restart resumes exactly, no duplicates.
**Independent test**: kill the host mid-dispatch; confirm resume + exactly one PR / ticket transition.

- [ ] T018 [US2] Implement the dispatch workflow in `go/orchestrator/workflows/dispatch.go` — the Clawta dispatch pipeline as durable steps
- [ ] T019 [P] [US2] Implement the dispatch activities (worker spawn in a worktree, PR open, ticket transition) in `go/orchestrator/activities/dispatch.go` — exactly-once (FR-005)
- [ ] T020 [US2] Kill-and-restart test in `go/orchestrator/workflows/dispatch_test.go` — host killed mid-dispatch resumes from the last completed step, zero duplicate PR/ticket (US2 acceptance)
- [ ] T021 [US2] Run dispatch beside the legacy `kanban-dispatch.lobster` + `swarm/bin/clawta-*` scripts; verify parity (FR-006)
- [ ] T022 [US2] Retire the legacy dispatch path once proven; update the Migration register (FR-007)

## Phase 5: User Story 3 — One orchestrator, zero sprawl (Priority: P3)

**Goal**: pollers, board-engine, and the Icarus bench loop are workflows; all crons/scripts retired.
**Independent test**: inventory before/after — every orchestration action traces to a workflow.

- [ ] T023 [P] [US3] Implement the poller/watchdog workflows in `go/orchestrator/workflows/pollers.go` (scheduled)
- [ ] T024 [P] [US3] Implement the board-engine workflow (auto-merge / retry / archive) in `go/orchestrator/workflows/board_engine.go`
- [ ] T025 [P] [US3] Implement the Icarus bench-loop workflow in `go/orchestrator/workflows/icarus_bench.go` — replaces `icarus-bench.service`
- [ ] T026 [US3] Run each workflow beside its legacy cron/service; verify parity; retire each + update the Migration register
- [ ] T027 [US3] Inventory check — confirm every orchestration action traces to a workflow; zero un-migrated crons/scripts remain (SC-001, SC-004)

## Phase 6: Polish & Cross-Cutting

- [ ] T028 [P] Confirm `workflowcheck` passes on every workflow — determinism gate green (FR-003, FR-012)
- [ ] T029 [P] Write the operator runbook (start / stop / inspect / replay a workflow) in `docs/runbooks/chitin-orchestrator.md` (FR-011)
- [ ] T030 Re-run the Constitution Check — all six principles still PASS post-implementation

---

## Dependencies

- **Phase 1 → Phase 2 → Phase 3**: Setup and Foundational block all stories.
- **US1 (P1)** is the MVP — independently shippable once Phase 1+2 are done.
- **US2 (P2)** depends on Phase 2 (the worktree + activity base); independent of US1.
- **US3 (P3)** depends on Phase 2; best done after US1/US2 prove the model.
- Within a story: workflow + activities (parallel) → test → run-beside → retire.

## Parallel Execution Examples

- Phase 1: T003, T004 in parallel.
- Phase 2: T006, T007, T008 in parallel (distinct packages).
- Phase 5: T023, T024, T025 in parallel (distinct workflow files).

## Implementation Strategy

**MVP = US1 (the pull-loop).** Phase 1 + Phase 2 + Phase 3 deliver a single
durable, inspectable workflow proven beside its cron — that alone validates
the determinism + telemetry thesis. US2 and US3 are incremental; each phase
retires its legacy path only after the workflow is proven (strangler-fig).
