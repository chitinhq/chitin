# Tasks — Spec 022: Dispatch readiness contract

- [ ] T1: Update `board_resolver.spec_dir_for_board()` to read `spec_source` from board config; remove implicit `owned_orgs` default-set fallback; add `spec_source_resolution(board)` helper returning `(path, source_tag)` (R2)
- [ ] T2: Remove `BOARDS` dict from `board-watchdog-bounded.py`; replace with `board_resolver.resolve_db(board)` and `board_resolver.spec_dir_for_board(board)` calls (R1)
- [ ] T3: Add NULL-assignee rejection to `kanban-flow ready` — when no `--assignee` override and current assignee is NULL/empty, die with error naming valid assignees (R3)
- [ ] T4: Create `docs/governance-setup-extras/dispatch-readiness.md` with the five-item dispatch readiness checklist (R4)
- [ ] T5: Extend watchdog `report_board()` to emit `spec root: <path> (source: <tag>)` telemetry per board (R5)
- [ ] T6: Write `swarm/tests/test_dispatch_readiness_contract.py` with test cases for R1–R5 per spec AC table (R1–R5)
- [ ] T7: Run full test suite and fix any regressions