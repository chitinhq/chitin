# Contract: `chitin-kernel session unlock -agent <id>`

**Spec**: 096 | **FRs**: 001, 004, 006, 007, 008, 009

## Synopsis

```text
chitin-kernel session unlock -agent <id> [-reason <text>] [--db-path <path>]
```

## Arguments

| Flag | Type | Required | Default | Description |
|---|---|---|---|---|
| `-agent` | string | yes | — | Agent name (matches `agent_state.agent`). |
| `-reason` | string | no | `""` | Operator-supplied; carried into the `session_unlocked` chain event. |
| `--db-path` | string | no | `~/.chitin/gov.db` | Path override for tests. |

## Behavior

1. Open gov.db (creates if missing — matches `OpenCounter` semantics). Exit 2 on IO error.
2. Run schema migration if needed (idempotent). Exit 2 if migration fails.
3. Begin SQL transaction:
   - `SELECT locked, locked_ts, lock_epoch, total FROM agent_state WHERE agent = ?`
   - If no row: exit 1 with stderr `error: no agent_state row for "<id>"`. Rollback.
   - If `locked = 0` (already unlocked): IDEMPOTENT path — skip the UPDATE; capture pre-state for the chain event; commit (no-op).
   - If `locked = 1`: UPDATE `locked = 0, unlock_ts = NOW, lock_epoch = lock_epoch + 1`. Read-back the new `lock_epoch` (per D10).
4. Emit `session_unlocked` chain event via `internal/emit` (carries `agent, lock_epoch_after, reason, locked_ts_before, total_at_unlock`).
5. Print to stdout:
   - Non-idempotent path: `unlocked agent=<id> lock_epoch=<new> reason="<reason or '(none)'>"`
   - Idempotent path: `unlocked agent=<id> lock_epoch=<unchanged> (was already unlocked)`
6. Exit 0.

## Exit codes

- **0** — unlock succeeded (regardless of chain-emit success; non-idempotent or idempotent path).
- **1** — user error: no such agent.
- **2** — runtime error: gov.db open / migration / SQL error.

## stderr messages (stable strings)

| Condition | Message |
|---|---|
| Agent doesn't exist in gov.db | `error: no agent_state row for "<id>"` |
| gov.db open failure | `error: cannot open gov.db at <path>: <underlying>` |
| Migration failure | `error: schema migration failed: <underlying>` |
| SQL transaction failure | `error: unlock transaction failed: <underlying>` |
| Chain emit failed (warn only) | `warning: chain emit failed: <error> — unlock succeeded; the audit chain lost this entry` |

## Side effects

- Mutates `agent_state` row: sets `locked = 0`, populates `unlock_ts`, advances `lock_epoch` (NON-idempotent path only).
- Emits one `session_unlocked` chain event (best-effort; failure does not roll back).
- Does NOT touch `denials`, `denial_events`, or any other table.

## Non-behaviors

- MUST NOT touch the `total` field, the `denials` table, or `denial_events`. Unlock preserves audit history; `Counter.Reset()` is the destructive sibling.
- MUST NOT advance `lock_epoch` on the idempotent path (D5).
- MUST NOT block on chain emission for more than 5 seconds; on timeout, log warn and continue.

## Operator examples

```bash
# Unlock with reason:
chitin-kernel session unlock -agent clawta -reason "policy relaxed in PR #999"

# Unlock with no reason (safety re-run):
chitin-kernel session unlock -agent clawta

# Verify the unlock:
chitin-kernel session status -agent clawta
```
