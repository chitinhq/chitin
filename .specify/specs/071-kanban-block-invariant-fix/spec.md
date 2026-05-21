# Spec 071: Kanban Block-Invariant Fix

> **Status:** draft
> **Author:** ares
> **Created:** 2026-05-20
> **Bound ticket:** t_54b9c01d

## Problem

`kanban_db.py` has a status-naming mismatch between `running` (used in SQL) and `in_progress` (used on the chitin board). This causes:

1. **`block_task()` fails for in_progress tickets.** The WHERE clause `status IN ('running', 'ready', 'review')` never matches `in_progress`, so `hermes kanban block` silently returns False for the most common state agents work in.
2. **`VALID_STATUSES` set omits `in_progress`.** Line 93: `VALID_STATUSES = {"triage", "todo", "ready", "running", ...}` — no `in_progress`. Any status-transition validation against this set rejects `in_progress`.
3. **Other transition functions have the same gap.** `cancel_run()`, `release_stale_claims()`, and `complete_task()` all use `'running'` in WHERE clauses without `'in_progress'`.

### Root cause

Hermes kanban has two status-name conventions:
- **Canonical (internal SQL):** `running` — used throughout `kanban_db.py`
- **Board alias (chitin):** `in_progress` — used by the chitin board's kanban-flow and CLI display

The board adapter in `hermes kanban` translates between these, but `block_task()` and `VALID_STATUSES` bypass the adapter and check raw SQL values.

## Solution

Add `'in_progress'` alongside `'running'` in:

1. **`VALID_STATUSES`** (line 93) — add `'in_progress'` to the set.
2. **`block_task()` WHERE clauses** (lines 2634, 2647) — change `IN ('running', 'ready', 'review')` to `IN ('running', 'in_progress', 'ready', 'review')`.
3. **`cancel_run()`** (line 2172) — change `IN ('running', 'ready', 'blocked')` to `IN ('running', 'in_progress', 'ready', 'blocked')`.
4. **Any other transition WHERE** that uses `'running'` without `'in_progress'` — audit and add.

### Bonus: poller BLOCKED: comment parsing

The chitin poller cannot transition in_progress tickets to blocked via `block_task()`, so it falls back to posting `BLOCKED:` comments. The poller should read these as interim block signals. This is a separate concern (poller behavior, not state machine) and tracked in a follow-up ticket.

## Acceptance criteria

- [ ] `hermes kanban block <id>` succeeds when ticket status is `in_progress`
- [ ] `hermes kanban block <id>` still works for `running`, `ready`, `review`
- [ ] `VALID_STATUSES` includes `in_progress`
- [ ] `block_task()` WHERE clause includes `in_progress`
- [ ] `cancel_run()` WHERE clause includes `in_progress`
- [ ] All other transition WHERE clauses that reference `running` also reference `in_progress`
- [ ] Unit tests: block from in_progress, cancel from in_progress, complete from in_progress
- [ ] Existing tests still pass

## Out of scope

- Poller `BLOCKED:` comment parsing (separate ticket)
- Merging `running` and `in_progress` into one canonical name (larger refactor)