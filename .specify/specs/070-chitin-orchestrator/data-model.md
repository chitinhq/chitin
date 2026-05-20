# 070 — Phase 1 Data Model

The orchestrator's domain entities. "State" here means Temporal-managed
durable state unless noted.

## Workflow

A durable, deterministic orchestration unit — one per former cron/script.

- `name` — stable id (e.g. `pull-loop`, `dispatch`, `icarus-bench`).
- `run history` — Temporal-owned; the complete inspectable/replayable timeline.
- `schedule` — cron-style or continuous (Continue-As-New) for never-ending loops.
- Relationships: invokes Activities; may start child Workflows.
- Invariant: deterministic — replay over the same history yields the same decisions.

## Activity

A single side-effecting step within a workflow.

- `name`, `input`, `output`, `retry policy`, `timeout`.
- Examples: a board mutation, a `gh` call, an agent invocation, a worktree op.
- Invariant: idempotent w.r.t. its workflow run — a retry/restart never
  duplicates a PR, ticket transition, or dispatch (FR-005).

## Worker

An isolated process that executes one work unit.

- `driver` — codex / copilot / gemini / claude-code, or a generic task runner.
- `worktree` — the worktree it runs in (1:1).
- Lifecycle: spawned by a workflow activity, short-lived, exits on completion.

## Worktree

A dedicated git worktree — the worker's isolated filesystem (FR-013/14).

- `path`, `branch`, `work-unit id`, `created-at`.
- Lifecycle: created fresh per work unit → handed to the worker → torn down
  on completion. Orphaned worktrees (crashed worker) are GC-reclaimable.
- Invariant: the primary/shared checkout is never a worktree / work surface.

## Migration register

The record mapping each legacy cron/script to its replacement.

- `legacy` — the cron name or `swarm/bin/` script path.
- `workflow` — the replacing workflow name.
- `status` — `not-started` → `running-beside` → `proven` → `legacy-retired`.
- Purpose: makes the strangler-fig migration auditable (SC-001, SC-004).

## Orchestrator service

The worker-host runtime (`cmd/chitin-orchestrator`).

- Registers all workflows + activities with the Temporal server.
- Exposes start / stop / inspect / replay for the operator (FR-011).
- One systemd unit; the Temporal server is a separate unit.
