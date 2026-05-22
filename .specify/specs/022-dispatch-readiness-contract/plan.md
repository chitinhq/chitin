# Implementation Plan: 022 — Dispatch Readiness Contract

**Branch**: `feat/022-dispatch-readiness-contract` | **Date**: 2026-05-22 | **Spec**: `.specify/specs/022-dispatch-readiness-contract/spec.md`

## Summary

Replace three undocumented dispatch gates with a single, written, code-enforced readiness contract. Every board must declare `spec_source` explicitly; `board_resolver.spec_dir_for_board()` becomes the single source of truth for spec-root resolution; `kanban-flow ready` rejects tickets with no assignee; and the watchdog consumes `spec_dir_for_board` instead of a hardcoded dict.

## Technical Context

**Language/Version**: Python 3.11+

**Primary Dependencies**: hermes kanban DB (SQLite), board_resolver, clawta-poller, board-watchdog-bounded

**Storage**: SQLite (`~/.hermes/kanban/<board>/kanban.db`), JSON board configs

**Testing**: pytest (static analysis + integration against temp DB)

**Target Platform**: Linux server (swarm cron/poller)

**Project Type**: CLI tools + cron scripts

**Performance Goals**: No regression to poller/watchdog cycle time

**Constraints**: Must not break existing dispatch behavior for boards that already work

**Scale/Scope**: 2 boards (chitin, readybench) today; contract must generalize

## Constitution Check

✅ All gates pass: single-function resolution, explicit config, fail-fast validation.

## Project Structure

```text
swarm/
├── bin/
│   ├── board_resolver.py       # R1: single source of truth (add spec_source reading)
│   ├── board-watchdog-bounded.py  # R1+R5: consume spec_dir_for_board, add telemetry
│   └── clawta-poller            # R3: enforce assignee at promotion boundary
├── tests/
│   └── test_dispatch_readiness_contract.py  # All 6 AC test cases
docs/
└── governance-setup-extras/
    └── dispatch-readiness.md   # R4: operator runbook
.specify/specs/022-dispatch-readiness-contract/
├── spec.md   # Already merged (PR #744)
├── plan.md   # This file
└── tasks.md  # Task breakdown
```

## Implementation Strategy

Phase 1 (R1+R2): Refactor `board_resolver.py` to read explicit `spec_source` from board config, remove `owned_orgs` default-set fallback. Update `board-watchdog-bounded.py` to call `spec_dir_for_board` instead of hardcoded dict. Add telemetry to watchdog output (R5).

Phase 2 (R3): Add assignee validation to `kanban-flow ready` command. Reject NULL assignee with error naming valid values.

Phase 3 (R4): Write the operator runbook at `docs/governance-setup-extras/dispatch-readiness.md`.

Phase 4: Test suite (`test_dispatch_readiness_contract.py`) covering all 6 AC.