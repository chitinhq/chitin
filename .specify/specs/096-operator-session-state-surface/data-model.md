# Data Model: Operator session-state surface

**Feature**: 096-operator-session-state-surface
**Date**: 2026-05-23

## Entity 1 — `agent_state` row (extended)

The existing table in `~/.chitin/gov.db` gains two columns. Existing columns preserved verbatim.

| Column | Type | Required | Default | Notes |
|---|---|---|---|---|
| `agent` | TEXT | yes | — | Primary key. Pre-existing. |
| `total` | INTEGER | yes | 0 | Lifetime denial weight. Pre-existing. |
| `locked` | INTEGER | yes | 0 | 0 = not locked, 1 = locked. Pre-existing. |
| `locked_ts` | TEXT | no | NULL | RFC3339 of the most recent lock. Pre-existing. |
| `unlock_ts` | TEXT | no | NULL | **NEW** — RFC3339 of the most recent unlock; NULL until first unlock |
| `lock_epoch` | INTEGER | yes | 0 | **NEW** — monotonic per-agent generation counter |

### Migration

```sql
-- Idempotent: PRAGMA table_info(agent_state) is checked before ALTER.
ALTER TABLE agent_state ADD COLUMN unlock_ts TEXT;
ALTER TABLE agent_state ADD COLUMN lock_epoch INTEGER NOT NULL DEFAULT 0;
```

Pre-existing rows get `unlock_ts = NULL, lock_epoch = 0`. All existing API callers (`Counter.RecordDenial`, `Counter.Level`, `Counter.IsLocked`, `Counter.Lockdown`, `Counter.Reset`, auto-escalation in `RecordActionDenial`) continue working byte-identically for their existing column reads.

### Invariants

- `lock_epoch` is monotonic per agent. It NEVER decreases.
- `lock_epoch` advances by exactly 1 on each lock transition (auto-escalation, operator CLI, Go API kill-switch) AND on each NON-IDEMPOTENT unlock (operator CLI against a currently-locked agent, `Counter.Reset()`).
- `lock_epoch` does NOT advance on idempotent unlock (CLI against an already-unlocked agent — see D5).
- `unlock_ts > locked_ts` whenever `locked = 0` and the agent has ever been locked.
- `denials` and `denial_events` rows are NOT touched by `session unlock`; they ARE deleted by `Counter.Reset()`.

## Entity 2 — Subcommand argv

### `chitin-kernel session unlock -agent <id> [-reason <text>]`

| Flag | Type | Required | Default | Description |
|---|---|---|---|---|
| `-agent` | string | yes | — | The agent name (matches `agent_state.agent`). |
| `-reason` | string | no | `""` | Free-text reason carried into the `session_unlocked` chain event. |
| `--db-path` | string | no | `~/.chitin/gov.db` | gov.db path override. |
| `--policy-file` | string | no | (default lookup) | Inherited from existing kernel CLI convention. |

### `chitin-kernel session lock -agent <id> [-reason <text>]`

Same flags as unlock; `-reason` carried into the `session_locked` chain event with `source: "operator_cli"`.

### `chitin-kernel session status [-agent <id>] [--text]`

| Flag | Type | Required | Default | Description |
|---|---|---|---|---|
| `-agent` | string | no | (absent) | Inspect mode if present; list mode if absent. |
| `--text` | bool | no | false | Default JSON; `--text` switches to a fixed-column table. |
| `--db-path` | string | no | `~/.chitin/gov.db` | gov.db path override. |

## Entity 3 — Status query response (JSON)

### List mode (no `-agent`)

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
  },
  {
    "agent": "main",
    "locked": true,
    "locked_ts": "2026-05-23T14:00:00Z",
    "unlock_ts": null,
    "lock_epoch": 1,
    "total": 10,
    "level": "lockdown"
  }
]
```

Array sorted by `agent` ASCII order (deterministic — operators can diff snapshots).

### Inspect mode (`-agent X`)

Single object with the same fields, no array wrapping.

### `--text` mode

```text
AGENT          LOCKED  LEVEL      TOTAL  EPOCH  LOCKED_TS              UNLOCK_TS
clawta         false   normal        12      6  2026-05-23T13:00:00Z   2026-05-23T13:45:00Z
main           true    lockdown      10      1  2026-05-23T14:00:00Z   -
```

## Entity 4 — `session_locked` chain event

| Field | Type | Notes |
|---|---|---|
| `event_type` | `"session_locked"` | Constant. |
| `agent_instance_id` | string | `chitin-kernel-<pid>` or `chitin-kernel-cli-<pid>` per source. |
| `ts` | string (RFC3339) | UTC. |
| `payload.agent` | string | The agent whose state changed. |
| `payload.lock_epoch_after` | int | The `lock_epoch` value after this transition. |
| `payload.source` | string | `"auto_escalation" | "operator_cli" | "operator_go_api"` |
| `payload.reason` | string | Operator-supplied for CLI/Go API; `""` for auto_escalation. |

## Entity 5 — `session_unlocked` chain event

| Field | Type | Notes |
|---|---|---|
| `event_type` | `"session_unlocked"` | Constant. |
| `agent_instance_id` | string | `chitin-kernel-cli-<pid>`. |
| `ts` | string (RFC3339) | UTC. |
| `payload.agent` | string | The agent. |
| `payload.lock_epoch_after` | int | Post-unlock epoch. For idempotent unlock-of-unlocked, equals the pre-call epoch (no advance). |
| `payload.reason` | string | Operator-supplied; `""` if absent. |
| `payload.locked_ts_before` | string | The `locked_ts` value at the moment of unlock (null if never previously locked). |
| `payload.total_at_unlock` | int | The agent's lifetime denial total at the unlock moment — preserves forensic snapshot. |

## Sequence — operator unlock happy path

```
Pre-state: agent_state{agent:"clawta", total:12, locked:1, locked_ts:"T1", unlock_ts:NULL, lock_epoch:5}

1. Operator: chitin-kernel session unlock -agent clawta -reason "policy relaxed"
2. CLI parses argv → SessionUnlockArgs{Agent:"clawta", Reason:"policy relaxed"}
3. CLI dials gov.db (db-path resolution)
4. CLI begins SQL transaction:
   a. SELECT locked, locked_ts, lock_epoch, total FROM agent_state WHERE agent=?
      → {locked:1, locked_ts:"T1", lock_epoch:5, total:12}
   b. UPDATE agent_state SET locked=0, unlock_ts=?, lock_epoch=lock_epoch+1 WHERE agent=?
      → row now {locked:0, unlock_ts:"T2", lock_epoch:6}
   c. SELECT lock_epoch FROM agent_state WHERE agent=?  (read-after-write per D10)
      → 6
   d. COMMIT
5. CLI emits chain event via internal/emit:
   {event_type:"session_unlocked", agent_instance_id, ts:"T2",
    payload:{agent:"clawta", lock_epoch_after:6, reason:"policy relaxed",
             locked_ts_before:"T1", total_at_unlock:12}}
6. CLI prints to stdout: "unlocked agent=clawta lock_epoch=6 reason=\"policy relaxed\""
7. CLI exits 0
```

## Sequence — auto-escalation lock with chain emit

```
Pre-state: agent_state{agent:"clawta", total:9, locked:0, lock_epoch:5}

1. Driver calls Counter.RecordActionDenial("clawta", "shell.exec", "fp-x", weight=1)
2. RecordActionDenial begins tx:
   a. INSERT/UPSERT denials row
   b. UPDATE agent_state SET total=total+1 → total now 10
   c. SELECT total → 10; >= 10 threshold met
   d. UPDATE agent_state SET locked=1, locked_ts=?, lock_epoch=lock_epoch+1 → epoch now 6
   e. INSERT denial_events row
   f. SELECT lock_epoch → 6  (read-after-write)
   g. COMMIT
3. RecordActionDenial calls internal/emit.SessionLocked(agent:"clawta", lock_epoch:6, source:"auto_escalation", reason:"")
4. Chain event emitted (in-process; no subprocess). If emit fails: log warn, return nil (D9).
5. RecordActionDenial returns nil
```

## Sequence — idempotent unlock-of-unlocked

```
Pre-state: agent_state{agent:"clawta", locked:0, lock_epoch:6}

1. Operator: chitin-kernel session unlock -agent clawta -reason "safety re-run"
2. CLI begins tx:
   a. SELECT locked FROM agent_state WHERE agent=? → 0
3. CLI detects idempotent case (already unlocked):
   - Does NOT issue UPDATE
   - Does NOT advance lock_epoch
4. CLI emits chain event with epoch unchanged:
   {event_type:"session_unlocked", payload:{agent, lock_epoch_after:6, reason:"safety re-run", ...}}
5. CLI prints to stdout: "unlocked agent=clawta lock_epoch=6 (was already unlocked)"
6. CLI exits 0
```

Consumers comparing epochs see `6 == 6` and correctly conclude "no transition since I last cached." Forensic chain still records the operator action.

## Validation invariants (tests must assert)

| Invariant | How to check |
|---|---|
| Schema migration is idempotent | Run migration twice; assert second run is no-op and no errors |
| Pre-existing rows survive migration | Fixture gov.db with old schema; assert all old API methods work byte-identically after migration |
| `lock_epoch` advances on every lock transition | Lock → assert epoch +1; auto-escalation → assert epoch +1; Counter.Lockdown() Go API → assert epoch +1 |
| `lock_epoch` advances on non-idempotent unlock | Lock + unlock → assert epoch advanced by 2 (lock +1, unlock +1) |
| `lock_epoch` does NOT advance on idempotent unlock | Unlock already-unlocked → assert epoch unchanged |
| Auto-escalation emits `session_locked` | Force 10 denials → assert one chain event with source:"auto_escalation" |
| Chain emit failure during auto-escalation does not roll back | Set kernel-bin to missing during auto-escalation; assert agent_state.locked=1 + warning logged |
| Status JSON sorted by agent ASCII | Insert agents in non-sorted order; assert list response is sorted |
| `Counter.Reset()` behavior preserved | Reset an agent; assert row + denials + denial_events all deleted |
