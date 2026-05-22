# Tasks: 022 — Dispatch Readiness Contract

**Input**: Design documents from `.specify/specs/022-dispatch-readiness-contract/`

**Prerequisites**: plan.md (required), spec.md (required)

## Phase 1: Single Source of Truth + Explicit Config (R1, R2, R5)

- [ ] T001 [R1] Remove hardcoded `BOARDS` dict from `swarm/bin/board-watchdog-bounded.py`; replace all `BOARDS[board]["spec_root"]` calls with `board_resolver.spec_dir_for_board(board)`. Delete the dict entirely.
- [ ] T002 [R2] Add `spec_source` field schema to board config (`~/.hermes/kanban/<board>/config.json`). Values: `"repo"`, `"workspace_overlay"`, `"owned_orgs"` (deprecated). Update `board_resolver.spec_dir_for_board()` to read `spec_source` from config instead of falling back to `owned_orgs` default-set.
- [ ] T003 [R2] Remove the `owned_orgs()` default-set (`{"chitinhq", "red"}`) from `board_resolver.py`. Keep the function for explicit opt-in only. Add deprecation warning when `spec_source: "owned_orgs"` is used.
- [ ] T004 [R5] Add spec-root telemetry to watchdog Discord output: include `spec root: <path> (source: repo|workspace_overlay|owned_orgs)` line in the readybench board section.

## Phase 2: Fail-Fast Assignee Gate (R3)

- [ ] T005 [R3] Add assignee validation to `kanban-flow ready <id>`: reject tickets where `assignee IS NULL`. Error message must list valid values: terminal drivers (`codex`, `copilot`, `claude-code`, `gemini`), routing lane (`clawta`), or operator (`red`).
- [ ] T006 [R3] Add assignee acceptance test: parametrized over `codex`, `copilot`, `claude-code`, `gemini`, `clawta`, `red` — each must pass the `kanban-flow ready` validation.

## Phase 3: Operator Runbook (R4)

- [ ] T007 [R4] Create `docs/governance-setup-extras/dispatch-readiness.md` documenting the full dispatch-readiness checklist: (1) invariants_and_boundaries block in body, (2) spec-kit entry under board-appropriate spec root, (3) assignee set to terminal driver or routing lane, (4) no unresolved Blocked until: in bound spec, (5) not a tracking-epic.

## Phase 4: Test Suite

- [ ] T008 [P] Create `swarm/tests/test_dispatch_readiness_contract.py` with test scaffold.
- [ ] T009 [R1] `test_watchdog_consumes_spec_dir_for_board`: grep `board-watchdog-bounded.py` for import of `spec_dir_for_board` from `board_resolver`; assert no hardcoded `WORKSPACE_ROOT / ".specify" / "specs"` literal remains.
- [ ] T010 [R2] `test_board_config_requires_spec_source`: load each board config; assert `spec_source` key present.
- [ ] T011 [R3] `test_ready_rejects_null_assignee`: integration test — `kanban-flow ready t_xxx` with NULL assignee returns nonzero + named error.
- [ ] T012 [R3] `test_ready_accepts_valid_assignees`: parametrized over codex, copilot, claude-code, gemini, clawta, red.
- [ ] T013 [R4] `test_readiness_runbook_exists_with_all_5_checks`: grep the markdown for each numbered check.
- [ ] T014 [R5] `test_watchdog_post_includes_spec_root_decision`: run watchdog, assert output contains `spec root:` + `source:` lines.

## Dependencies & Execution Order

- T001 depends on T002 (need `spec_dir_for_board` to work with new config before removing dict)
- T002 and T003 can be done together (both modify board_resolver)
- T004 depends on T001 (telemetry needs the new resolution path)
- T005 and T006 are independent of Phase 1
- T007 is independent (documentation only)
- T008–T014 can be written in parallel with implementation but must pass after each phase lands