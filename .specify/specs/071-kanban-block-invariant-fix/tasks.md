# Tasks — Spec 071: Kanban Block-Invariant Fix

## Task 1: Audit running-only WHERE clauses
- Grep `kanban_db.py` for all `status` references using `'running'` without `'in_progress'`
- Document each location (function name, line number)

## Task 2: Add in_progress to VALID_STATUSES
- Edit line 93: add `'in_progress'` to the `VALID_STATUSES` set

## Task 3: Fix block_task() WHERE clauses
- Lines 2634, 2647: add `'in_progress'` to `IN ('running', 'ready', 'review')` → `IN ('running', 'in_progress', 'ready', 'review')`

## Task 4: Fix cancel_run() WHERE clause
- Line 2172: add `'in_progress'` to `IN ('running', 'ready', 'blocked')` → `IN ('running', 'in_progress', 'ready', 'blocked')`

## Task 5: Fix all other transition WHERE clauses from audit
- Apply the same pattern to any other functions found in Task 1

## Task 6: Write tests
- Test `block_task()` succeeds on `in_progress` ticket
- Test `cancel_run()` succeeds on `in_progress` ticket  
- Test `complete_task()` succeeds on `in_progress` ticket

## Task 7: Run full test suite
- `pytest tests/ -x -q` — zero regressions