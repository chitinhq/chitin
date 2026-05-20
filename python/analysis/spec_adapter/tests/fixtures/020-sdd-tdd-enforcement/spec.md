# 020 — Chitin enforces SDD + TDD as governance policy

**Status**: RATIFIED 2026-05-17

## Requirements

- **R1**: Single source of truth for board→spec-root resolution.
- **R2**: Board config schema gains an explicit `spec_source` field.
- **R3**: `kanban-flow ready <id>` rejects tickets with no assignee.
- **R4**: A new operator-facing runbook documents the full checklist.
- **R5**: The watchdog's section in its Discord post MUST include
  resolution-decision telemetry.

## Acceptance Criteria

- **AC1**: A worker commit that stages code without a test file fails.
- **AC2**: The same commit with a test file staged or escape clause passes.
- **AC3**: Removing `## Test coverage` from a spec fails the hook.
- **AC4**: A new test file without a spec reference fails the hook.
- **AC5**: `gh pr create` without spec in diff or body fails.
- **AC6**: The same `gh pr create` with spec reference succeeds.

## Boundary cases

1. **Spec with no test changes** → code-only refactors use the escape clause.
2. **E2e cost prohibitive** → the spec justifies the layer exception.
3. **Review-burden spike** — too many PRs for one reviewer.

## Open questions

- **Q1** — Should integration tests count as e2e? Proposed: yes for API, no for UI.
- **Q2** — Enforcement at pre-commit vs PR-create? Proposed: both.

## Slice plan

- **Slice 1** — Layer 1+2 pre-commit hooks. AC1–AC4.
- **Slice 2** — Layer 3 gate action. AC5, AC6.