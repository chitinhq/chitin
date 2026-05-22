---
description: "Task list for spec 081 — Cron-to-Workflow Migration and Board Retirement"
---

# Tasks: Cron-to-Workflow Migration and Board Retirement

**Input**: Design documents from `specs/081-cron-migration-board-retirement/`

**Prerequisites**: plan.md, spec.md

**Tests**: Workflow and activity tests accompany each migration; the board
retirement is verified by the existing scheduler suite staying green.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story the task serves (US1, US2, US3)

## Notes

The three user stories are independent. US1 is mostly deletion. US2 establishes
the Schedule-backed migration pattern; US3 reuses it. Within US2/US3 each cron
is its own task and may be its own PR — a retired timer is deleted in the same
change that proves its workflow (mutation jobs may soak first).

---

## Phase 1: User Story 1 — Retire the board read-model (Priority: P1)

**Goal**: The orchestrator and console carry zero kanban-era board code.

**Independent test**: Orchestrator builds and a scheduler tick invokes no
`ProjectToBoard`; the console builds with no `/board` route.

- [ ] T001 [US1] Remove the scheduler's board projection: drop the `project()`
  step and the `Board` dependency from `workflows/scheduler.go`, and the `Board`
  field + `ProjectToBoard` registration from `workflows/scheduler_activities.go`
  and `activities/scheduler_activities.go`.
- [ ] T002 [US1] Delete `activities/board_projection.go`,
  `activities/sqlite_board_projector.go`, and their tests.
- [ ] T003 [US1] Remove board wiring from
  `go/orchestrator/cmd/chitin-orchestrator/main.go` (the board projector
  construction and the `Board` dep).
- [ ] T004 [P] [US1] Delete the console `/board` page (`pages/board.page.ts/html/css`)
  and `board.service.ts`; drop the `/board` route from `app.routes.ts` and the
  nav entry from `app.ts`.
- [ ] T005 [P] [US1] Disable and delete `argus-ingest-kanban` and `clawta-poller`
  — their `swarm/systemd` units and any `swarm/bin` scripts.
- [ ] T006 [US1] Update `apps/chitin-console/.../orchestrator-diagram.page.ts` —
  remove the "Board" node and the kanban-era "board" wording.
- [ ] T007 [US1] Verify: `go build ./...`, `go test ./...`, `nx build chitin-console`;
  confirm no `ProjectToBoard` activity remains registered.

**Checkpoint**: US1 is independently shippable here.

---

## Phase 2: User Story 2 — Migrate the periodic read-mostly crons (Priority: P2)

**Goal**: The audits and telemetry-ingest jobs run as Temporal scheduled
workflows; their systemd timers are retired.

**Independent test**: A Temporal Schedule fires its workflow on the cron;
the corresponding systemd timer is disabled.

- [ ] T008 [US2] Establish the Schedule-backed pattern: a `schedules/` package
  that registers a Temporal Schedule (cron expression + catch-up policy) per
  job at worker-host startup, and a `workflows/scheduled_jobs.go` +
  `activities/schedule_jobs.go` shape for a job workflow + activity.
- [ ] T009 [P] [US2] Migrate `swarm-audit` (daily) — scheduled workflow + the
  audit activity; disable + delete `swarm-audit.{service,timer}`.
- [ ] T010 [P] [US2] Migrate `architecture-audit` (weekly); disable + delete its
  units.
- [ ] T011 [P] [US2] Migrate `argus-ingest-beliefs`, `argus-ingest-git`,
  `argus-ingest-logs`; disable + delete their timers.
- [ ] T012 [P] [US2] Migrate `chitin-codex-chain-ingest` and
  `chitin-codex-usage-feed`; disable + delete their timers.
- [ ] T013 [US2] Update the `/orchestrator` diagram — add the Temporal server
  node and a Schedules node feeding the scheduled-job workflows.
- [ ] T014 [US2] Verify: each migrated job's Schedule fires its workflow; no job
  runs from both a timer and a Schedule.

**Checkpoint**: US2 is independently shippable; the Schedule pattern is proven.

---

## Phase 3: User Story 3 — Migrate the watchdog, mutation, and bench jobs (Priority: P3)

**Goal**: Every remaining cron runs as a scheduled workflow; the bench is a
workflow; the systemd timer count trends to zero.

**Independent test**: Each migrated job runs under its Schedule; the timer is
disabled after a soak; re-enabling the timer is a one-command rollback.

- [ ] T015 [P] [US3] Migrate `chitin-chain-watch` (watchdog — runaway-lockdown
  detection) to a scheduled workflow.
- [ ] T016 [P] [US3] Migrate `chitin-agent-unlock` (mutation — auto-unlock); soak
  with the timer authoritative, then retire the timer.
- [ ] T017 [P] [US3] Migrate `chitin-envelope-rotate` (mutation — governance
  envelope rotation); soak, then retire.
- [ ] T018 [P] [US3] Migrate `chitin-kernel-redeploy` (deploy — diagnose its
  current failed state first); soak, then retire.
- [ ] T019 [P] [US3] Migrate `openclaw-gateway-restart` (ops); soak, then retire.
- [ ] T020 [US3] Migrate the Icarus bench (`chitin-bench`, Phase 4) to a
  scheduled workflow; retire `chitin-bench.service`.
- [ ] T021 [US3] Verify: the chitin systemd timer/service count is zero except
  the orchestrator, console, and Temporal units (SC-004).

**Checkpoint**: spec 070 Phase 3–5 complete — the orchestrator is the single
process.

---

## Dependencies

- US1, US2, US3 are mutually independent. US2's T008 (the pattern) precedes
  T009–T012; T015–T020 reuse the T008 pattern.
- Within US1: T001–T003 sequential (same packages); T004, T005 are `[P]`; T006
  after T001–T005; T007 last.
- Within US2: T008 first; T009–T012 are `[P]`; T013–T014 last.
- Within US3: T015–T020 are `[P]` (distinct jobs); T021 last.

## Out of scope

Redesigning any job's behavior — a migration replicates what the cron did. New
telemetry surfaces beyond what each job already emits.
