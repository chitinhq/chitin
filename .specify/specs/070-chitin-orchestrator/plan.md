# Implementation Plan: Chitin Orchestrator

**Branch**: `070-chitin-orchestrator` | **Date**: 2026-05-20 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/070-chitin-orchestrator/spec.md`

## Summary

Replace the orchestration sprawl — ~36 cron jobs across the Hermes and
OpenClaw gateways, ~52 `swarm/bin/` shell scripts, lobster dispatch, the
agent-bus — with **one deterministic, observable orchestrator built on
Temporal (Go SDK)**. Each former cron/script becomes a durable Temporal
workflow. Migration is incremental: the **spec-DAG scheduler** first
(spec 076 — it *replaces* the human-managed kanban pull-loop, running
beside the existing cron until proven), then the dispatch pipeline, then
pollers/watchdogs, then the Icarus bench loop — each cron/script retired
as its workflow proves out. Every work unit runs as a worker in its own
dedicated git worktree (FR-013/14).

The engine choice (Temporal) is settled — see
`docs/strategy/chitin-orchestrator-options-2026-05-20.md`. This plan does
not re-litigate it.

## Technical Context

**Language/Version**: Go 1.23+ — matches the Chitin Kernel; the Temporal Go SDK is first-class.

**Primary Dependencies**: Temporal (self-hosted via `temporal server start-dev`, a single binary) + the Temporal Go SDK + `workflowcheck` (determinism static analysis).

**Storage**: Temporal's own persistence holds workflow state and history (the dev server uses an embedded store). The Chitin Board (SQLite) is a **read-projection written by activities** — orchestrator state rendered for humans, never read back to decide what runs next (FR-016). The chitin chain is likewise written by activities, not the workflow engine.

**Testing**: `go test`; Temporal's `testsuite` for workflow replay/determinism tests; `workflowcheck` in CI as a determinism gate.

**Target Platform**: Linux, single box (chimera-ant); self-hosted.

**Project Type**: A Go service — the orchestrator — plus workflow/activity packages and a worker entrypoint.

**Performance Goals**: Orchestration is low-throughput (ticks on the order of minutes). The goal is **determinism + observability**, not QPS.

**Constraints**: Single-box, self-hosted. MUST NOT depend on the agent-bus (decommissioned, spec 069). Every work unit isolated in its own git worktree (FR-013/14). Indefinitely-running workflows (the pull-loop) must not grow history unbounded — use Continue-As-New.

**Scale/Scope**: ~6–10 workflows replacing ~36 crons + ~52 scripts; one operator.

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

| Principle | Assessment |
|-----------|------------|
| §1 Side-effect boundary | PASS — the orchestrator *coordinates*; side effects (PRs, kanban writes) still flow through hermes/`gh`, gated by the kernel. Activities call those paths; they never bypass the kernel to write chain events. |
| §2 Branch & worktree (amended: always workers + worktrees) | PASS — FR-013/14 enforce exactly §2; the orchestrator *is* the enforcement mechanism. |
| §3 Spec-kit promotion gate | PASS — 070 has `spec.md` + this `plan.md`; `tasks.md` follows. |
| §4 Tracked installers | PASS (planned) — the orchestrator service ships an idempotent installer under `swarm/bin/install-*.sh`. |
| §5 Board-aware scripts | PASS (planned) — workflows that touch the kanban honor the board parameter. |
| §6 Swarm tooling is the exception | PASS — the orchestrator is genuine kernel-adjacent infra; it lives under `go/`, not `swarm/`. |

No violations → Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/070-chitin-orchestrator/
├── plan.md          # This file
├── research.md      # Phase 0 — decisions (Temporal patterns)
├── data-model.md    # Phase 1 — Workflow/Activity/Worker/Worktree entities
├── quickstart.md    # Phase 1 — stand up the dev server, run the first workflow
└── tasks.md         # Phase 2 — /speckit-tasks output (not created here)
```

### Source Code (repository root)

```text
go/orchestrator/
├── cmd/chitin-orchestrator/   # the worker-host entrypoint
├── workflows/                 # one file per migrated unit (scheduler, dispatch, …)
├── activities/                # side-effecting steps (board ops, gh calls, agent invokes)
├── worktree/                  # worktree create/teardown/GC — enforces FR-013/14
└── telemetry/                 # OTel export → Chitin Telemetry

swarm/bin/install-chitin-orchestrator.sh   # idempotent installer (constitution §4)
swarm/systemd/chitin-orchestrator.service  # the worker-host service unit
```

**Structure Decision**: A new `go/orchestrator/` module beside the kernel —
one language across kernel + orchestrator (constitution §6: kernel-adjacent
infra belongs under `go/`, not `swarm/`). The Temporal *server* runs as its
own systemd unit (`temporal server start-dev`); the orchestrator's
*worker host* is `cmd/chitin-orchestrator`.

## Migration Phases

Each phase migrates one unit, runs it beside the legacy cron/script until
proven, then retires the legacy path. Every work unit = a worker in a fresh
worktree.

- **Phase 0 — Foundation.** Stand up `temporal server start-dev`; scaffold
  `go/orchestrator/`; a hello-world workflow; `workflowcheck` CI gate; OTel
  export to Chitin Telemetry. Exit: a trivial workflow runs and is inspectable.
- **Phase 1 — Scheduler (the P1 slice).** Build the spec-DAG scheduler
  (spec 076) as a durable workflow — Continue-As-New for the never-ending
  loop. It *replaces* the human-managed kanban pull-loop; it does not port
  it. Run beside the existing cron 7 days (SC-005); confirm every tick
  inspectable + replayable.
- **Phase 2 — Dispatch (the P2 slice).** Migrate the Clawta dispatch pipeline;
  exactly-once PR/ticket transitions; kill-and-restart test (US2).
- **Phase 3 — Pollers / watchdogs.** Migrate as scheduled workflows. (The legacy board-engine is retired, not migrated — the board is now a read-projection, FR-016.)
- **Phase 4 — Icarus bench loop.** Migrate `icarus-bench.service` to a workflow.
- **Phase 5 — Retirement.** Delete each cron/script as its workflow is proven;
  the orchestrator becomes the single origin of orchestration (SC-001, SC-004).

## Complexity Tracking

None — no constitution violations to justify.
