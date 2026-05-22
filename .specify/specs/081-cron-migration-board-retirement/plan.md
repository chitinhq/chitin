# Implementation Plan: Cron-to-Workflow Migration and Board Retirement

**Branch**: `081-cron-migration-board-retirement` | **Date**: 2026-05-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/081-cron-migration-board-retirement/spec.md`

## Summary

Phase 3–5 of the spec-070 rollout: retire the kanban-era board read-model, then
migrate the ~15 standalone systemd cron/timer jobs to Temporal scheduled
workflows, retiring each timer as its workflow is proven. No new architecture —
the orchestrator already runs durable workflows; this adds **Temporal Schedules**
(the cron-expression trigger) and removes dead subsystems. The three user
stories ship as independent PRs; within US2/US3 each cron is its own task.

## Technical Context

**Language/Version**: Go 1.23 (`go/orchestrator/`); TypeScript / Angular
(`apps/chitin-console/` — board-page removal only); Bash (systemd units).

**Primary Dependencies**: Temporal Go SDK — specifically the **Schedule API**
(`client.ScheduleClient`); the existing `workflows` / `activities` packages.

**Storage**: Removes one — the `SQLiteBoardProjector`'s board datastore. The
migrated jobs persist nothing the cron did not.

**Testing**: `go test ./...` (workflow + activity tests); `nx build chitin-console`;
systemd verification per migrated job; Temporal Schedule inspection via the
`temporal schedule` CLI.

**Target Platform**: Linux — the orchestrator worker host (systemd `--user`).

**Project Type**: Extension + subtraction on the existing orchestrator service,
the console app, and the `swarm/systemd` + `swarm/bin` job set.

**Constraints**: A migrated job MUST NOT run from both a timer and a Schedule
(FR-006). Mutation-job migrations carry a soak window. Retiring the board MUST
preserve scheduler observability via Temporal history + tick telemetry.

**Scale/Scope**: 15 jobs — 3 retired outright, ~10 migrated, the bench (Phase 4).

## Constitution Check

*GATE: Must pass before implementation. Re-checked after each slice.*

- **Workflows over agents** — every migrated cron becomes a deterministic
  workflow + activity; the watchdog/mutation jobs are mechanical, not agent
  work. PASS.
- **Determinism** — each scheduled workflow reads the clock only through
  `workflow.Now` and does its I/O in activities. PASS.
- **Governance** — every migrated job's work stays kernel-gated exactly as the
  cron's process was (FR-008). PASS.
- **No silent loss** — a missed scheduled run is governed by a declared
  catch-up policy (FR-007). PASS.
- **Spec-in / PR-out** — this work is spec-driven (this spec) and ships as
  reviewable PRs, one tranche per PR. PASS.

No violations; Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/081-cron-migration-board-retirement/
├── spec.md      # feature specification
├── plan.md      # this file
└── tasks.md     # task breakdown (/speckit-tasks output)
```

### Source Code (repository root)

```text
go/orchestrator/
├── activities/
│   ├── board_projection.go        # US1 — DELETE
│   ├── sqlite_board_projector.go  # US1 — DELETE
│   └── schedule_jobs.go           # US2 — new: per-cron job activities
├── workflows/
│   ├── scheduler.go               # US1 — drop the project() step + Board dep
│   ├── scheduler_activities.go    # US1 — drop Board dep + ProjectToBoard reg
│   └── scheduled_jobs.go          # US2 — new: scheduled-job workflows
├── schedules/                     # US2 — new: Temporal Schedule registration
└── cmd/chitin-orchestrator/main.go  # US1 drop board; US2 ensure Schedules

apps/chitin-console/src/app/
├── pages/board.page.*             # US1 — DELETE
├── pages/orchestrator-diagram.page.ts  # US1/US2 — drop Board, add Temporal+Schedules
├── board.service.ts               # US1 — DELETE
├── app.routes.ts                  # US1 — drop /board route
└── app.ts                         # US1 — drop /board nav entry

swarm/systemd/   # US2/US3 — delete each timer/service as its workflow lands
swarm/bin/       # US2/US3 — delete each retired job script
```

**Structure Decision**: US1 is mostly deletion across the orchestrator and the
console. US2 introduces one new pattern — a `scheduled_jobs.go` workflow file, a
`schedule_jobs.go` activity file, and a `schedules/` package that registers the
Temporal Schedules at worker-host startup. US3 reuses that pattern per remaining
cron. No new top-level project.

## Implementation Phases

The three user stories are independent; within US2/US3 each cron is its own
task and may be its own PR.

- **Phase 1 — US1: Retire the board (P1).** Delete `board_projection.go`,
  `sqlite_board_projector.go`, the scheduler's `project()` step and `Board`
  dep, the `ProjectToBoard` registration, and the board wiring in `main.go`.
  Delete the console `/board` page, route, nav entry, and `board.service.ts`.
  Disable + delete `argus-ingest-kanban` and `clawta-poller`. Update the
  `/orchestrator` diagram to drop the Board node.

- **Phase 2 — US2: Migrate the periodic read-mostly crons (P2).** Establish the
  Schedule-backed pattern: a `schedules/` package that creates a Temporal
  Schedule per job at startup; a scheduled-job workflow that runs the job's work
  in an activity. Migrate `architecture-audit`, `swarm-audit`, the three
  `argus-ingest-*` jobs, and the two codex telemetry jobs — each migration
  disables its systemd timer in the same change. Update the diagram with the
  Temporal server + Schedules.

- **Phase 3 — US3: Migrate the watchdog, mutation, and bench jobs (P3).** Apply
  the US2 pattern to `chitin-chain-watch`, `chitin-agent-unlock`,
  `chitin-envelope-rotate`, `chitin-kernel-redeploy`, `openclaw-gateway-restart`,
  and the Icarus bench (`chitin-bench`). Mutation jobs may soak with the timer
  authoritative before the timer is retired.

## Complexity Tracking

No constitution violations — this section is intentionally empty.
