# Operator runbook — session-state surface (spec 096)

Spec 096 added three `chitin-kernel` subcommands that let an operator
mutate per-agent governance lock state with chain-anchored audit:

```text
chitin-kernel session unlock -agent <id> [-reason <text>]      # soft unlock; preserves audit history
chitin-kernel session lock   -agent <id> [-reason <text>]      # operator kill-switch
chitin-kernel session status [-agent <id>] [--text]            # inspect or list
```

All three are first-class kernel CLI subcommands — no shelling out, no
sqlite manipulation. The existing `Counter.Reset()` Go API is preserved
unchanged (it's the destructive sibling that DELETES the row + denial
history; `session unlock` is the audit-preserving operation).

## When to use each subcommand

| Situation | Subcommand |
|---|---|
| Agent stuck in lockdown after a false-positive denial cascade; you've fixed the policy and want it productive again | `session unlock` |
| An agent is misbehaving and you want to preemptively block it from any further gated action | `session lock` |
| Incident triage — "which agents are locked right now? what's their epoch?" | `session status` (no `-agent`) |
| Mid-incident inspection — "what's clawta's lock state and what was the last reason?" | `session status -agent clawta` |
| Decommissioning an agent and you want to clear ALL state, including denial history | `Counter.Reset()` via existing test fixtures (not exposed as CLI) |

## `session unlock` — clear a lock, preserve the audit

```bash
chitin-kernel session unlock -agent clawta -reason "policy relaxed in PR #999"
```

### Flow

1. Opens `~/.chitin/gov.db` (overridable via `--db-path`).
2. Runs the idempotent schema migration — adds `unlock_ts` and `lock_epoch` columns to any pre-spec-096 database transparently.
3. `Counter.Unlock(agent)` transitions the row from locked → unlocked. `denials` and `denial_events` rows are NOT touched.
4. Emits a `session_unlocked` chain event with `lock_epoch_after`, `locked_ts_before`, `total_at_unlock`, and the operator's reason.
5. Prints success JSON.

### Idempotent behavior (per spec 096 D5)

Unlocking an already-unlocked agent succeeds, prints `"idempotent":true`, and **does NOT advance `lock_epoch`** — but **does emit a chain event** for forensic completeness. The chain entry records that the operator performed the action; the epoch comparison consumers use to detect transitions stays clean.

```bash
$ chitin-kernel session unlock -agent already-unlocked -reason "safety re-run"
{"agent":"already-unlocked","idempotent":true,"lock_epoch_after":5,"ok":true,"reason":"safety re-run"}
```

### Errors

| Error envelope | When | Fix |
|---|---|---|
| `{"error":"no_agent","message":"no agent_state row for \"X\""}` | The agent has never been seen by the gate; there's no row to unlock | Confirm spelling; if the agent really is new, use `session lock` to bootstrap then unlock |
| `{"error":"open_govdb","message":"..."}` | gov.db unreachable / corrupted | Check `--db-path`, file permissions, disk space |

## `session lock` — operator kill-switch

```bash
chitin-kernel session lock -agent rogue-agent -reason "observed leaking secrets in chain"
```

### Flow

1. Opens gov.db (same as unlock).
2. `Counter.OperatorLock(agent)` — INSERTs a fresh `agent_state` row if none exists (bootstraps with `total=10, locked=1, lock_epoch=1`), or UPDATEs an existing row to `locked=1, total=MAX(total, 10), lock_epoch=lock_epoch+1`.
3. Emits a `session_locked` chain event with `source: "operator_cli"`.

### Not idempotent (per spec 096 R6 + contracts/lock-subcommand.md)

Unlike unlock, lock-of-locked **does** advance `lock_epoch` and **does** emit a chain event. Rationale: a re-lock is an audit-meaningful action — the second invocation may carry a stricter reason or document a re-decision the operator wants logged.

```bash
$ chitin-kernel session lock -agent x -reason "first"
{"agent":"x","lock_epoch_after":1,"ok":true,"reason":"first"}
$ chitin-kernel session lock -agent x -reason "second decision"
{"agent":"x","lock_epoch_after":2,"ok":true,"reason":"second decision"}
```

## `session status` — read-only inspection

### Inspect a single agent

```bash
chitin-kernel session status -agent clawta
```

JSON output:

```json
{
  "agent": "clawta",
  "locked": false,
  "locked_ts": "2026-05-23T13:00:00Z",
  "unlock_ts": "2026-05-23T13:45:00Z",
  "lock_epoch": 6,
  "total": 12,
  "level": "lockdown"
}
```

**Note on `level`**: derived from `total + locked` per the existing `Counter.Level()` semantics — an agent that has ever crossed `total >= 10` reports `level: "lockdown"` regardless of current lock state. This is intentional: the `locked` field is the live verdict, `level` is the lifetime escalation tier. Consumers that want "is currently locked" should read `locked`, not `level`.

### List all agents

```bash
chitin-kernel session status               # JSON array
chitin-kernel session status --text        # fixed-column table
```

```bash
# Pipe to jq for ad-hoc queries:
chitin-kernel session status | jq '.[] | select(.locked == true)'   # only locked agents
chitin-kernel session status | jq '.[] | {agent, epoch: .lock_epoch}'   # who's at high generation
```

JSON output is **sorted by agent ASCII** for deterministic diff (FR-009) — diffing two snapshots reliably tells you what changed.

### Strictly read-only

Status emits **no chain event** and mutates **no state**. Safe to wrap in `watch -n 5 chitin-kernel session status --text` for incident monitoring.

## Schema migration (spec 096 contracts/schema-migration.md)

The `agent_state` table gained two new columns in spec 096:

```sql
ALTER TABLE agent_state ADD COLUMN unlock_ts TEXT;
ALTER TABLE agent_state ADD COLUMN lock_epoch INTEGER NOT NULL DEFAULT 0;
```

The migration runs **automatically** on every `OpenCounter()` invocation. It's idempotent — uses `PRAGMA table_info` to detect what's already present and only ALTERs the missing columns. Safe to re-run after a SIGKILL between the two ALTERs.

A pre-spec-096 database opened by a post-spec-096 kernel binary gets the new columns transparently. All existing API methods (`Counter.RecordDenial`, `Counter.Level`, `Counter.IsLocked`, `Counter.Lockdown`, `Counter.Reset`) continue working byte-identically for callers that don't read the new columns.

## Chain events emitted

| Event type | Emitted by | Payload fields |
|---|---|---|
| `session_locked` | `session lock` (CLI), `Counter.Lockdown()` (Go API), eventually auto-escalation in `RecordActionDenial` | `agent`, `lock_epoch_after`, `source` (`"operator_cli"` \| `"auto_escalation"` \| `"operator_go_api"`), `reason` |
| `session_unlocked` | `session unlock` | `agent`, `lock_epoch_after`, `reason`, `locked_ts_before`, `total_at_unlock` |

Events land in `~/.chitin/events-session-<agent>.jsonl` — keyed by agent so per-agent audit is one `cat` away:

```bash
# Every state transition for clawta, chronological:
cat ~/.chitin/events-session-clawta.jsonl | jq

# All operator-initiated locks across all agents:
jq -c 'select(.event_type=="session_locked" and .payload.source=="operator_cli")' ~/.chitin/events-session-*.jsonl

# Count locks-vs-unlocks per agent:
for f in ~/.chitin/events-session-*.jsonl; do
  agent=$(basename "$f" .jsonl | sed 's/events-session-//')
  locked=$(jq -c 'select(.event_type=="session_locked")' "$f" | wc -l)
  unlocked=$(jq -c 'select(.event_type=="session_unlocked")' "$f" | wc -l)
  echo "$agent: $locked locked / $unlocked unlocked"
done
```

## The `lock_epoch` model

`lock_epoch` is a per-agent monotonic counter. It advances by 1 on:

- Auto-escalation (transition from `locked=0` to `locked=1` inside `RecordActionDenial`)
- Operator CLI lock (`session lock` against any agent — including already-locked, since re-lock is audit-meaningful)
- Operator Go API lock (`Counter.Lockdown()`)
- Operator CLI unlock (`session unlock` against a currently-locked agent)

It does **NOT** advance on:

- Subsequent denials against an already-locked agent (only the initial transition advances)
- Idempotent unlock (`session unlock` against an already-unlocked agent — chain event still emits, but epoch stays put)

### Consumer pattern (spec 091 v1.1 — the first consumer)

A consumer caches `lock_epoch` at the moment it observes a lock state. Later, to detect transitions:

```python
current = query_status(agent).lock_epoch
if current > cached_epoch:
    # something happened — re-evaluate
```

The combination "lock_epoch advanced" + "locked=false" tells the consumer: an unlock happened that I should react to. The combination "lock_epoch advanced" + "locked=true" tells the consumer: a new lock generation, possibly with a different reason.

## Environment variables

| Variable | Used by | Purpose |
|---|---|---|
| `--db-path <path>` flag | All three subcommands | gov.db path override (default: `~/.chitin/gov.db`) |
| `--dir <path>` flag | unlock + lock (NOT status) | Chain state dir for chain emit (default: `~/.chitin`) |
| `--text` flag | status | Fixed-column table output (default: JSON) |

## Constitution alignment

| § | How |
|---|---|
| §1 (kernel is only chain writer) | Chain emit flows through the same in-process `emit.Emitter` the kernel's `emit` subcommand uses. No new chain-write seam. |
| §6 (swarm tooling is exception) | New code lives under `go/execution-kernel/`, not `swarm/`. |
| §7 (swarm is orchestrator) | The session subcommands are operator-facing infrastructure that supports orchestrator-driven implementations (recovery for stuck driver sessions). They don't introduce a driver-bypass surface — operators use them for incident recovery; drivers don't invoke them. |

## Troubleshooting

**Q: I ran `session unlock` but the agent is still locked in `gov.db`.**
A: Check `session status -agent X` — what does `locked` actually say? Sometimes operators conflate `locked: true` with `level: "lockdown"` — they're different. If `locked: false` but `level: "lockdown"`, the agent is unlocked but its lifetime denial total still earns it the lockdown tier label. That's not a bug; that's data preservation.

**Q: The chain event didn't land in `~/.chitin/events-session-<agent>.jsonl`.**
A: Look at stderr from your `session unlock`/`lock` invocation — chain emit is fail-soft (D9). A warning line `warning: chain emit failed: ...` would have been printed. The state mutation in gov.db succeeded regardless.

**Q: I unlocked an agent twice in a row and saw the second one say `idempotent:true`. Did the second event get emitted?**
A: Yes — per D5, the chain event IS emitted on idempotent unlock for forensic completeness (the operator action happened). What does NOT happen is the epoch advance — the second event has the same `lock_epoch_after` as the first.

**Q: After `session lock -agent x`, why does `level` say `"lockdown"` even though I haven't run RecordDenial against x?**
A: `OperatorLock` sets `total = MAX(total, 10)` to put the agent in the escalation tier corresponding to lockdown. This means subsequent `Counter.Level()` calls also report `lockdown` (consistent with what auto-escalation would have produced).
