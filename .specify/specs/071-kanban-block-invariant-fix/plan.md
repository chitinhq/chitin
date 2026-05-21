# Plan — Spec 071: Kanban Block-Invariant Fix

## Approach

Direct code fix in `hermes_cli/kanban_db.py`. Small, surgical changes.

## Steps

1. **Audit all `'running'` references in WHERE clauses** — grep for `status.*=.*'running'` and `status IN.*'running'` across `kanban_db.py`.
2. **Add `'in_progress'` to `VALID_STATUSES`** on line 93.
3. **Fix `block_task()`** — lines 2634, 2637 → add `'in_progress'` to both WHERE clause variants.
4. **Fix `cancel_run()`** — line 2172 → add `'in_progress'`.
5. **Fix any other transition functions** found in step 1.
6. **Write unit tests** — `test_block_in_progress`, `test_cancel_in_progress`, `test_complete_in_progress`.
7. **Run existing test suite** — verify no regressions.

## Risk assessment

- **Low risk.** Adding `in_progress` to status sets and WHERE clauses is strictly additive — it widens the match condition without narrowing existing ones.
- **No migration needed.** The chitin board already stores `in_progress` in the DB; the fix just teaches the Python layer to recognize it.

## Dependencies

None — this is a standalone hermes-agent fix.