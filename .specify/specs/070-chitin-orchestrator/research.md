# 070 — Phase 0 Research

The engine decision (Temporal) is settled in
`docs/strategy/chitin-orchestrator-options-2026-05-20.md` and is not
re-opened here. This file records the Temporal-specific design decisions.

## D1 — Engine: Temporal, Go SDK, self-hosted

- **Decision**: Temporal via the Go SDK; self-hosted with `temporal server
  start-dev` (single binary) on the one box.
- **Rationale**: determinism + an inspectable event-history UI are the
  product; first-class Go SDK matches the Kernel; the retired Octi specs
  040–048 already chose Temporal Go.
- **Alternatives**: LangGraph — a different layer (agent reasoning, not
  durable macro-orchestration). Restate — viable lighter fallback. DBOS —
  no Go SDK. All rejected in the options doc.

## D2 — Never-ending workflows: Continue-As-New

- **Decision**: long-lived workflows (the pull-loop) periodically call
  Continue-As-New to start a fresh run with carried-over state.
- **Rationale**: a workflow that ticks forever would grow event history
  unbounded (FR-009). Continue-As-New caps history per run while the
  logical loop never stops.

## D3 — Workflows are pure; activities own all side effects

- **Decision**: workflow code is deterministic and side-effect-free; every
  board write, `gh` call, agent invocation, and worktree operation is an
  **activity**.
- **Rationale**: FR-003 (determinism / replay) requires it; FR-005
  (exactly-once) is delivered by activity idempotency + Temporal's retry
  semantics. Constitution §1 — activities call hermes/`gh`/the kernel; they
  never bypass the kernel.

## D4 — Determinism enforced in CI: `workflowcheck`

- **Decision**: run Temporal's `workflowcheck` static analyzer as a CI gate.
- **Rationale**: FR-003/FR-012 — catch non-deterministic constructs (time,
  randomness, map iteration) before they break replay.

## D5 — Worker-per-work-unit in a worktree

- **Decision**: a `worktree/` package creates a fresh git worktree per work
  unit, hands it to the worker, and tears it down on completion; orphans are
  garbage-collected.
- **Rationale**: FR-013/14 and constitution §2 — the platform flow always
  uses workers + worktrees; the shared checkout is never a work surface.

## D6 — Telemetry: OTel → Chitin Telemetry

- **Decision**: the orchestrator exports run telemetry over OpenTelemetry to
  the Chitin Telemetry layer; the Temporal UI is the live operational view.
- **Rationale**: FR-008 — orchestration telemetry feeds the observability
  thesis, not just Temporal's own UI.

## D7 — Migration: strangler-fig, beside the legacy path

- **Decision**: each workflow runs **beside** the cron/script it replaces
  until proven (FR-006); the legacy path is deleted only after (FR-007).
- **Rationale**: incremental, reversible migration; no big-bang cutover.
