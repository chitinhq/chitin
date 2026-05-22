# Plan — Spec 022: Dispatch readiness contract

## Approach

Implement the five requirements (R1–R5) as a single PR touching the
watchdog, board_resolver, kanban-flow, and docs. All changes share one
theme: making dispatch readiness a code-enforced contract with a single
source of truth.

## Work order

1. **R2 first** — Update `board_resolver.spec_dir_for_board()` to read
   the explicit `spec_source` field from board config and remove the
   `owned_orgs` default-set fallback. This is the foundation for R1 and
   R5.

2. **R1** — Remove the hardcoded `BOARDS` dict from
   `board-watchdog-bounded.py`. Replace with calls to
   `board_resolver.spec_dir_for_board(board)` and
   `board_resolver.resolve_db(board)`. The watchdog imports
   `board_resolver` and consumes it.

3. **R3** — Add assignee validation to the `ready` subcommand of
   `kanban-flow`. When `--assignee` is not passed and the current
   assignee is NULL/empty, refuse the promotion with an error naming
   the valid set.

4. **R4** — Write `docs/governance-setup-extras/dispatch-readiness.md`
   documenting the five-item checklist.

5. **R5** — Extend the watchdog's `report_board()` to emit
   `spec root: <path> (source: repo|workspace_overlay|owned_orgs)`
   per board in the Discord-bound output.

6. **Tests** — Write `swarm/tests/test_dispatch_readiness_contract.py`
   covering all AC from the spec's test-coverage table.

## Branch strategy

Feature branch: `022-dispatch-readiness-contract` off `main`.
Squash-merge on approval.

## Risks

- Removing `owned_orgs` fallback may break boards that rely on the
  default without declaring `spec_source`. Mitigated: both chitin and
  readybench configs already have `spec_source` set.
- `kanban-flow ready` rejecting NULL assignee is a behaviour change.
  The current default (`clawta`) means it should almost never fire, but
  callers that previously passed `--assignee ''` will get rejected.