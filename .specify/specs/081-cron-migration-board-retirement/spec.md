# Feature Specification: Cron-to-Workflow Migration and Board Retirement

**Feature Branch**: `081-cron-migration-board-retirement`

**Created**: 2026-05-21

**Status**: Draft

**Input**: User description: "Cron-to-workflow migration and board retirement: migrate the swarm crons and watchdogs to Temporal scheduled workflows, and retire the kanban-era board read-model"

## Overview

This is Phase 3–5 of the spec-070 orchestrator rollout, made concrete. The
Chitin Orchestrator replaced the dispatch pull-loop (Phases 0–2); ~15 standalone
systemd cron/timer jobs still run beside it. This feature inventories those
jobs, migrates the periodic ones to Temporal **scheduled workflows**, and
retires each cron as its workflow is proven — so the orchestrator becomes the
single process it was specified to be (spec 070 FR-001).

It also retires the last residue of the decommissioned kanban (spec 069): the
**board read-model** — the `ProjectToBoard` activity, the `SQLiteBoardProjector`,
the scheduler's projection step, and the console's `/board` page — together with
the kanban-coupled crons that have nothing left to do.

### Job inventory

The 15 chitin cron/timer jobs, classified:

**Retire outright** — the underlying work no longer exists:

- the **board read-model** (`ProjectToBoard`, `SQLiteBoardProjector`, the
  scheduler projection step, the console `/board` page)
- `argus-ingest-kanban` — ingests a kanban that is gone
- `clawta-poller` — kanban-pull dispatch, superseded by the spec-DAG scheduler

**Migrate — periodic, read-mostly** (lowest-harm; proves the pattern):

- `architecture-audit` (weekly), `swarm-audit` (daily)
- `argus-ingest-beliefs`, `argus-ingest-git`, `argus-ingest-logs`
- `chitin-codex-chain-ingest`, `chitin-codex-usage-feed`

**Migrate — watchdog, mutation, ops** (highest-risk; migrate last):

- `chitin-chain-watch` (watchdog), `chitin-agent-unlock` (mutation),
  `chitin-envelope-rotate` (mutation), `chitin-kernel-redeploy` (deploy —
  currently in a failed state), `openclaw-gateway-restart` (ops)

**Phase 4 — the bench**: `chitin-bench` (the Icarus bench loop).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Retire the board read-model (Priority: P1)

The operator decommissioned the kanban; the board read-model is its surviving
ghost — code the orchestrator carries and a console page nobody steers by. Remove
it: the `ProjectToBoard` activity and `SQLiteBoardProjector`, the scheduler's
projection step and its `Board` dependency, the console `/board` page, route,
and service, and the `argus-ingest-kanban` cron.

**Why this priority**: It is the cleanest, lowest-risk slice — almost entirely
deletion of dead code — and it makes an honest architecture diagram possible.
The scheduler's node-state remains fully inspectable via Temporal history and
tick telemetry, so nothing observable is lost.

**Independent Test**: Build and run the orchestrator with no board projection;
build the console with no `/board` route; confirm the `argus-ingest-kanban`
timer is disabled. Fully testable without the cron-migration work.

**Acceptance Scenarios**:

1. **Given** the orchestrator, **When** a scheduler tick runs, **Then** no
   `ProjectToBoard` activity is invoked and no board datastore is opened.
2. **Given** the console, **When** it is loaded, **Then** there is no `/board`
   page, route, or navigation entry, and the build succeeds.
3. **Given** the systemd timers, **When** they are listed, **Then**
   `argus-ingest-kanban` is disabled and removed.
4. **Given** the `/orchestrator` diagram, **When** it is viewed, **Then** it no
   longer shows a "Board" node.

---

### User Story 2 - Migrate the periodic read-mostly crons (Priority: P2)

The operator wants the periodic, low-harm jobs — the audits and the telemetry
ingests — to run as Temporal scheduled workflows instead of systemd timers, so
they are durable, inspectable, and retried like every other orchestrator
workflow. This story establishes the **Schedule-backed migration pattern**: a
Temporal Schedule (a cron expression) triggers a workflow, which runs the job's
work in an activity.

**Why this priority**: These jobs are read-mostly and idempotent — a missed or
double cycle is low-harm — so they are the safe tranche to prove the Schedule
pattern before the watchdog and mutation crons.

**Independent Test**: Create a Temporal Schedule for one migrated job; confirm
it triggers the workflow at its cron time; disable the corresponding systemd
timer and confirm the workflow is now the sole runner.

**Acceptance Scenarios**:

1. **Given** a migrated cron's Temporal Schedule, **When** its scheduled time
   arrives, **Then** the Schedule triggers the job's workflow.
2. **Given** a migrated job's workflow is proven, **When** the migration lands,
   **Then** the corresponding systemd timer is disabled in the same change — the
   job never runs from both a timer and a Schedule at once.
3. **Given** the orchestrator worker is down at a scheduled time, **When** it
   recovers, **Then** the Schedule's catch-up policy governs whether the missed
   run executes — never silent indefinite loss.

---

### User Story 3 - Migrate the watchdog, mutation, and bench jobs (Priority: P3)

The operator wants the remaining jobs — the watchdog (`chitin-chain-watch`), the
mutation crons (`chitin-agent-unlock`, `chitin-envelope-rotate`,
`chitin-kernel-redeploy`, `openclaw-gateway-restart`), and the Icarus bench
(`chitin-bench`, Phase 4) — migrated to scheduled workflows and their timers
retired (Phase 5).

**Why this priority**: These jobs mutate governance and infrastructure state —
the highest blast radius — so they migrate last, on the pattern proven by US2,
each with its own soak before the timer is disabled.

**Independent Test**: Each migrated job's workflow runs correctly under its
Temporal Schedule; the timer is disabled only after a soak; re-enabling the
timer is a one-command rollback.

**Acceptance Scenarios**:

1. **Given** a migrated mutation job, **When** its workflow runs, **Then** it
   performs exactly the mutation the cron performed, governed by the kernel.
2. **Given** a migrated job whose workflow has not yet soaked, **When** the
   change lands, **Then** the systemd timer remains the authority until the
   soak completes (the migration and the retirement may be separate changes
   for a mutation job).

---

### Edge Cases

- A migrated job's Temporal Schedule and its old systemd timer are both active →
  double-execution. The retirement gate: a read-mostly job disables its timer in
  the migrating change; a mutation job may soak with the timer still authoritative
  and disable it in a later change.
- The orchestrator worker is down across a scheduled fire → the Temporal
  Schedule's catch-up / overlap policy decides; a missed run is never silently
  lost without a policy.
- `argus-ingest-kanban` and `clawta-poller` have no workflow replacement — the
  work is gone with the kanban; they are removed, not migrated.
- `chitin-kernel-redeploy` is currently in a failed systemd state — its
  migration must diagnose and not inherit the failure.
- Retiring the board read-model must not remove scheduler observability — node
  state stays inspectable through Temporal history and tick telemetry.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST remove the board read-model from the orchestrator
  — the `ProjectToBoard` activity, the `SQLiteBoardProjector`, the
  `BoardProjector` dependency, and the scheduler's projection step — without
  removing scheduler observability (Temporal history and tick telemetry remain).
- **FR-002**: The system MUST remove the console `/board` page, its route, its
  navigation entry, and the board data service.
- **FR-003**: The system MUST disable and remove the `argus-ingest-kanban` and
  `clawta-poller` jobs — kanban-coupled work with no workflow replacement.
- **FR-004**: The system MUST provide a Schedule-backed migration pattern: a
  Temporal Schedule (cron expression) that triggers a workflow which performs a
  former cron's work in an activity.
- **FR-005**: Each migrated periodic job MUST run as a Temporal scheduled
  workflow with the same cadence as its retired systemd timer.
- **FR-006**: A migrated job MUST NOT run from both a systemd timer and a
  Temporal Schedule simultaneously — the migrating change disables the timer, or
  records an explicit soak window during which the timer stays authoritative.
- **FR-007**: A migrated job's missed run (worker downtime) MUST be governed by
  a declared Temporal Schedule catch-up/overlap policy — never silently lost.
- **FR-008**: Every former cron's work, once migrated, MUST remain governed by
  the chitin kernel exactly as the cron's process was.
- **FR-009**: The `/orchestrator` console diagram MUST be updated to drop the
  "Board" node and add the Temporal server and Temporal Schedules.
- **FR-010**: Each retired systemd timer/service file MUST be deleted from the
  repository once its workflow is the proven authority.

### Key Entities

- **Scheduled Workflow**: a Temporal workflow triggered by a Temporal Schedule
  (a cron expression) — the durable replacement for a systemd timer.
- **Cron Inventory**: the classified list of the 15 jobs — retire-outright,
  migrate-read-mostly, migrate-watchdog/mutation/ops, and the bench.
- **Board read-model**: the retired kanban-era projection of scheduler state —
  the `ProjectToBoard` activity, `SQLiteBoardProjector`, and console `/board`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The orchestrator builds and runs with zero board-projection code;
  a scheduler run invokes no `ProjectToBoard` activity.
- **SC-002**: The console builds and serves with no `/board` route; every other
  page is unaffected.
- **SC-003**: Every migrated job runs on its Temporal Schedule at its former
  cadence, and its systemd timer is disabled — no job runs twice.
- **SC-004**: After the rollout, the count of chitin systemd timers/services
  trends to zero (excluding the orchestrator, console, and Temporal units
  themselves).
- **SC-005**: A worker outage across a scheduled fire results in either a
  caught-up run or an explicitly-policied skip — never an undetected miss.

## Assumptions

- The Temporal server (running as `temporal-dev.service`) supports Temporal
  Schedules — the standard Schedule API of the Temporal Go SDK.
- The kanban is fully decommissioned (spec 069); no consumer of the board
  read-model remains that is not itself being retired here.
- Each cron's current behavior is discoverable from its `swarm/bin` script or
  `swarm/systemd` unit; migration replicates that behavior, it does not redesign
  the job.
- A mutation job's migration and the retirement of its timer MAY be separate
  changes — the spec permits a soak window where the timer stays authoritative.
- The Icarus bench (`chitin-bench`) migration is Phase 4 of spec 070 and is
  scoped here as US3; it is the largest single job and may itself span more than
  one change.
- This feature completes spec 070's Phase 3–5; spec 070 remains the parent.
