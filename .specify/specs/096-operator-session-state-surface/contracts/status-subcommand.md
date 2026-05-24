# Contract: `chitin-kernel session status [-agent <id>] [--text]`

**Spec**: 096 | **FRs**: 001, 002, 006, 007, 009

## Synopsis

```text
chitin-kernel session status [-agent <id>] [--text] [--db-path <path>]
```

## Arguments

| Flag | Type | Required | Default | Description |
|---|---|---|---|---|
| `-agent` | string | no | (absent) | Inspect mode if present; list mode if absent. |
| `--text` | bool | no | false | Default JSON; `--text` switches to fixed-column table. |
| `--db-path` | string | no | `~/.chitin/gov.db` | Path override for tests. |

## Behavior — list mode (no `-agent`)

1. Open gov.db read-only. Exit 2 on IO error.
2. Migration ran on every open is idempotent — no special read-only path needed.
3. `SELECT agent, total, locked, locked_ts, unlock_ts, lock_epoch FROM agent_state ORDER BY agent ASC`
4. For each row, compute `level` (matches `Counter.Level()` semantics — locked → "lockdown", else by total threshold).
5. Render JSON array (default) or fixed-column table (`--text`).
6. Empty result: JSON `[]` or table with header only. Exit 0.

## Behavior — inspect mode (`-agent <id>`)

1. Open gov.db read-only.
2. `SELECT ... FROM agent_state WHERE agent = ?`
3. If no row: exit 1 with stderr `error: no agent_state row for "<id>"`.
4. Render single JSON object (no array wrapping) or fixed-column table (`--text` with header + single data row).
5. Exit 0.

## Exit codes

- **0** — query succeeded (empty list is success).
- **1** — user error: `-agent` specified for a non-existent agent.
- **2** — runtime error: gov.db open / SQL error.

## stderr messages

| Condition | Message |
|---|---|
| `-agent` doesn't exist | `error: no agent_state row for "<id>"` |
| gov.db open failure | `error: cannot open gov.db at <path>: <underlying>` |
| SQL error | `error: status query failed: <underlying>` |

## Output shapes

### List mode JSON

```json
[
  {
    "agent": "clawta",
    "locked": false,
    "locked_ts": "2026-05-23T13:00:00Z",
    "unlock_ts": "2026-05-23T13:45:00Z",
    "lock_epoch": 6,
    "total": 12,
    "level": "normal"
  }
]
```

Sorted by `agent` ASCII order. Determinism is contractual — operators must be able to diff successive snapshots.

### Inspect mode JSON

Single object, same fields, no array wrapping.

### `--text` mode

```text
AGENT          LOCKED  LEVEL      TOTAL  EPOCH  LOCKED_TS              UNLOCK_TS
clawta         false   normal        12      6  2026-05-23T13:00:00Z   2026-05-23T13:45:00Z
main           true    lockdown      10      1  2026-05-23T14:00:00Z   -
```

Fixed-column widths: `AGENT 14, LOCKED 6, LEVEL 10, TOTAL 5, EPOCH 5, LOCKED_TS 22, UNLOCK_TS 22`. Truncate longer values with `…`; pad shorter with spaces.

## Side effects

**None.** `status` is strictly read-only. No chain event emitted. No state mutated. (Migration runs on every kernel invocation, but that's an idempotent no-op if the columns already exist.)

## Non-behaviors

- MUST NOT emit a chain event for status queries — they're operator inspection, not state transitions.
- MUST NOT block on a write transaction — SQLite WAL allows reads without blocking writers.
- MUST NOT silently truncate JSON output for any agent — always render the full row.

## Operator examples

```bash
# List all agents:
chitin-kernel session status

# List as a table:
chitin-kernel session status --text

# Inspect one agent:
chitin-kernel session status -agent clawta

# Pipe to jq:
chitin-kernel session status | jq '.[] | select(.locked==true)'

# Diff two snapshots:
chitin-kernel session status > /tmp/before.json
# ... operator action ...
chitin-kernel session status > /tmp/after.json
diff /tmp/before.json /tmp/after.json
```
