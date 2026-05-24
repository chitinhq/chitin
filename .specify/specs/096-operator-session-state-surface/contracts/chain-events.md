# Contract: Chain event schemas (spec 096 additions)

**Spec**: 096 | **FRs**: 005, 006, 008, 009

Two new chain event types emitted via the canonical `chitin-kernel emit` path (D6 — preserves §1's single chain-write seam even when the writer is the kernel binary).

## Event 1 — `session_locked`

Emitted on every lock transition: auto-escalation in `RecordActionDenial`, `chitin-kernel session lock` CLI, or `Counter.Lockdown()` Go API call.

### Wire shape

```json
{
  "event_type": "session_locked",
  "agent_instance_id": "chitin-kernel-<pid>",
  "ts": "2026-05-23T14:00:00Z",
  "payload": {
    "agent": "clawta",
    "lock_epoch_after": 6,
    "source": "auto_escalation",
    "reason": ""
  }
}
```

### Field semantics

| Field | Type | Required | Notes |
|---|---|---|---|
| `event_type` | `"session_locked"` | yes | Discriminator. |
| `agent_instance_id` | string | yes | The kernel process emitting the event. |
| `ts` | string (RFC3339) | yes | UTC, set by the emitter. |
| `payload.agent` | string | yes | The agent whose state changed. |
| `payload.lock_epoch_after` | int | yes | Post-transition epoch (read-after-write per D10). |
| `payload.source` | string | yes | One of: `"auto_escalation"`, `"operator_cli"`, `"operator_go_api"`. |
| `payload.reason` | string | yes | For `operator_cli` / `operator_go_api`: the operator-supplied reason (may be `""`). For `auto_escalation`: always `""`. |

### Chain framing

Standard chain framing (`chain_id`, `seq`, `prev_hash`, `this_hash`) supplied by the emit code path.

### Filtering examples

```bash
# All auto-escalations in the last hour:
jq 'select(.event_type=="session_locked" and .payload.source=="auto_escalation")' ~/.chitin/events-*.jsonl

# All operator-initiated locks:
jq 'select(.event_type=="session_locked" and .payload.source=="operator_cli")' ~/.chitin/events-*.jsonl

# Lock sources by frequency:
jq -r 'select(.event_type=="session_locked") | .payload.source' ~/.chitin/events-*.jsonl | sort | uniq -c | sort -rn
```

## Event 2 — `session_unlocked`

Emitted on every unlock operation — including idempotent unlock-of-unlocked (the chain entry exists for forensic completeness; `lock_epoch_after` reflects no advance per D5).

### Wire shape

```json
{
  "event_type": "session_unlocked",
  "agent_instance_id": "chitin-kernel-cli-<pid>",
  "ts": "2026-05-23T13:45:00Z",
  "payload": {
    "agent": "clawta",
    "lock_epoch_after": 6,
    "reason": "policy relaxed",
    "locked_ts_before": "2026-05-23T13:00:00Z",
    "total_at_unlock": 12
  }
}
```

### Field semantics

| Field | Type | Required | Notes |
|---|---|---|---|
| `event_type` | `"session_unlocked"` | yes | Discriminator. |
| `agent_instance_id` | string | yes | The kernel process emitting. |
| `ts` | string (RFC3339) | yes | UTC. |
| `payload.agent` | string | yes | The agent. |
| `payload.lock_epoch_after` | int | yes | Post-unlock epoch. For idempotent unlock-of-unlocked, equals the pre-call epoch (no advance — D5). |
| `payload.reason` | string | yes | Operator-supplied; `""` if absent. |
| `payload.locked_ts_before` | string \| null | yes | `agent_state.locked_ts` value at the moment of unlock. `null` if the agent had never been locked. |
| `payload.total_at_unlock` | int | yes | Lifetime denial total at the unlock moment — preserves forensic snapshot. |

## Why two events, not three

A single `session_event` with a `verb` field was considered (D4 alternative) and rejected — separate types are more queryable and let each payload schema evolve independently.

Three event types (`session_locked_auto` / `session_locked_operator_cli` / `session_locked_operator_go_api`) were also considered and rejected — the `source` field provides the discrimination without exploding the event-type surface.

## Emission mechanism

Two paths share the same in-process code:

1. **CLI subcommands** (`session unlock`, `session lock`) call `internal/emit.SessionLocked(...)` or `internal/emit.SessionUnlocked(...)` directly.
2. **Auto-escalation** in `RecordActionDenial` calls the same in-process function on the lock-transition branch.

Both paths reach the same `internal/emit` package which is the SINGLE chain-write seam (preserves §1).

Per D9: emit failure logs a warning to stderr but never blocks the load-bearing action (lock state in gov.db is the source of truth).

## Reverse compatibility

These are two genuinely new event types. No existing chain reader will see them and break — chain readers in the current codebase filter on `event_type` and ignore unknown values.
