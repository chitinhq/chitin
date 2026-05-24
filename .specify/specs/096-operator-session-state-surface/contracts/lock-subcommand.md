# Contract: `chitin-kernel session lock -agent <id>`

**Spec**: 096 | **FRs**: 001, 003, 005, 006, 007, 009

## Synopsis

```text
chitin-kernel session lock -agent <id> [-reason <text>] [--db-path <path>]
```

## Arguments

| Flag | Type | Required | Default | Description |
|---|---|---|---|---|
| `-agent` | string | yes | — | Agent name (matches `agent_state.agent`). May not exist yet — see Behavior step 3. |
| `-reason` | string | no | `""` | Operator-supplied; carried into the `session_locked` chain event. |
| `--db-path` | string | no | `~/.chitin/gov.db` | Path override for tests. |

## Behavior

This is the operator kill-switch CLI wrapping the existing `Counter.Lockdown()` Go API semantics, with the addition of epoch + chain emit.

1. Open gov.db. Exit 2 on IO error.
2. Run schema migration if needed (idempotent). Exit 2 if migration fails.
3. Begin SQL transaction:
   - If `agent_state` has no row for `<id>`: INSERT `(agent, total=10, locked=1, locked_ts=NOW, lock_epoch=1)`. (This matches the bootstrap-lock-an-unseen-agent path under Edge Cases.)
   - Else: UPDATE `locked = 1, locked_ts = NOW, total = MAX(total, 10), lock_epoch = lock_epoch + 1`.
   - Read-back the new `lock_epoch`.
4. Emit `session_locked` chain event via `internal/emit` with `source: "operator_cli"`, carrying `agent, lock_epoch_after, reason`.
5. Print to stdout: `locked agent=<id> lock_epoch=<new> reason="<reason or '(none)'>"`.
6. Exit 0.

## Exit codes

- **0** — lock succeeded (regardless of chain-emit success).
- **1** — user error: (none defined currently; lock against an already-locked agent is idempotent and advances epoch — this is intentional, distinct from unlock).
- **2** — runtime error: gov.db open / migration / SQL error.

### Why lock-of-locked is NOT idempotent

Unlike unlock (D5), `session lock -agent X` against an already-locked X DOES advance `lock_epoch` and DOES emit a chain event. Rationale: an operator deliberately re-locking is an audit-meaningful action (e.g., "re-lock with stricter reason after first reason was incomplete"). The epoch advance reflects a real new operator decision.

## stderr messages

| Condition | Message |
|---|---|
| gov.db open failure | `error: cannot open gov.db at <path>: <underlying>` |
| Migration failure | `error: schema migration failed: <underlying>` |
| SQL transaction failure | `error: lock transaction failed: <underlying>` |
| Chain emit failed (warn only) | `warning: chain emit failed: <error> — lock succeeded; the audit chain lost this entry` |

## Side effects

- Mutates `agent_state`: sets `locked = 1`, populates `locked_ts`, advances `lock_epoch`, sets `total = MAX(total, 10)`.
- Inserts a row if `<id>` was previously unseen.
- Emits one `session_locked` chain event with `source: "operator_cli"`.
- Does NOT touch `denials` or `denial_events`.

## Non-behaviors

- MUST NOT modify `Counter.Reset()` semantics (FR-010).
- MUST NOT silently drop a re-lock (lock-of-locked is intentionally non-idempotent).

## Operator examples

```bash
# Kill-switch an agent:
chitin-kernel session lock -agent rogue-agent -reason "observed leaking secrets in chain"

# Bootstrap-lock an agent that has never been seen:
chitin-kernel session lock -agent new-driver -reason "preemptive — gov.yaml not yet verified"
```
