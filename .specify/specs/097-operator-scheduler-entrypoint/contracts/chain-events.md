# Contract: Chain event schemas (spec 097 additions)

**Spec**: 097 | **FRs**: 009

This spec adds two new chain event types. Both flow through the existing `chitin-kernel emit -event-json -` subprocess path (constitution §1 — kernel is the only chain writer).

## Event 1 — `scheduler_started`

Emitted by `chitin-orchestrator schedule` immediately after `client.ExecuteWorkflow` returns successfully. The chain anchor for "a spec entered the orchestrator at time T."

### Wire shape

```json
{
  "event_type": "scheduler_started",
  "agent_instance_id": "chitin-orchestrator-cli-<pid>",
  "session_id": "chitin-orchestrator-cli-<pid>-<short_uuid>",
  "ts": "2026-05-23T14:30:00Z",
  "payload": {
    "spec_ref": "096-operator-session-state-surface",
    "run_id": "abc123-4567-89ab-cdef-...",
    "node_count": 8,
    "capabilities_required": ["code.implement", "code.refactor", "test.author"]
  }
}
```

### Field semantics

| Field | Type | Required | Notes |
|---|---|---|---|
| `event_type` | `"scheduler_started"` | yes | Discriminator; chain readers filter on this. |
| `agent_instance_id` | string | yes | Convention: `chitin-orchestrator-cli-<pid>` so multiple concurrent CLI invocations are distinguishable. |
| `session_id` | string | yes | Per-invocation identifier; the suffix is a short random token. |
| `ts` | string (RFC3339) | yes | UTC. Set by the CLI; the kernel may rewrite to its own clock as part of chain framing. |
| `payload.spec_ref` | string | yes | The canonical spec ref (directory basename). |
| `payload.run_id` | string | yes | The Temporal WorkflowID (== SchedulerInput.RunID). |
| `payload.node_count` | int | yes | Number of nodes in the compiled DAG. |
| `payload.capabilities_required` | []string | yes | Sorted, deduplicated capability tags across all nodes. |

### Chain framing

Standard chain framing (`chain_id`, `seq`, `prev_hash`, `this_hash`) is supplied by `chitin-kernel emit`. The CLI does not compute these.

### Filtering examples

```bash
# Every spec ever scheduled, in chain order:
jq 'select(.event_type=="scheduler_started")' ~/.chitin/events-*.jsonl

# All scheduler starts for one spec:
jq 'select(.event_type=="scheduler_started" and .payload.spec_ref=="096-operator-session-state-surface")' ~/.chitin/events-*.jsonl

# Counts by spec:
jq -r 'select(.event_type=="scheduler_started") | .payload.spec_ref' ~/.chitin/events-*.jsonl | sort | uniq -c | sort -rn
```

## Event 2 — `scheduler_canceled`

Emitted by `chitin-orchestrator cancel` immediately after `client.CancelWorkflow` returns successfully. The chain anchor for "an operator canceled a run at time T."

### Wire shape

```json
{
  "event_type": "scheduler_canceled",
  "agent_instance_id": "chitin-orchestrator-cli-<pid>",
  "session_id": "chitin-orchestrator-cli-<pid>-<short_uuid>",
  "ts": "2026-05-23T14:45:00Z",
  "payload": {
    "run_id": "abc123-4567-89ab-cdef-...",
    "reason": "policy relaxed; rerun with new tasks.md"
  }
}
```

### Field semantics

| Field | Type | Required | Notes |
|---|---|---|---|
| `event_type` | `"scheduler_canceled"` | yes | Discriminator. |
| `agent_instance_id` | string | yes | Same convention as `scheduler_started`. |
| `session_id` | string | yes | Per-invocation. |
| `ts` | string (RFC3339) | yes | UTC. |
| `payload.run_id` | string | yes | The Temporal WorkflowID that was canceled. |
| `payload.reason` | string | yes | Empty string `""` when operator did not supply `-reason`. |

### Idempotency

Cancel-of-terminal is a no-op (per `cancel-subcommand.md`). When the cancel exits 1 because the run was already terminal, NO `scheduler_canceled` event is emitted. The chain never carries a "phantom cancel" for a run that was never actually canceled by this operator.

## Why only two event types

The CLI exposes three subcommands, but `status` is read-only and emits no chain event — a status query reveals nothing about the chain's lineage of state transitions. Adding `scheduler_status_queried` would add noise (operators run `status` repeatedly while watching a run) without adding audit value.

If future operator UX needs distinguish "operator looked" from "operator decided," that's an amendment after the CLI proves itself.

## Emission mechanism

Same pattern as `emitStopSignalIgnored` in the openclaw plugin (spec 091 v1.0 FR-009):

```go
cmd := exec.CommandContext(ctx, kernelBinPath(), "emit", "-event-json", "-")
cmd.Stdin = bytes.NewReader(eventJSON)
cmd.Stderr = &stderrBuf
if err := cmd.Run(); err != nil {
    log.Printf("warning: chain emit failed: %v (stderr: %s) — user action succeeded; the audit chain lost this entry", err, stderrBuf.String())
    return // fall-through; do NOT propagate as a user-visible failure
}
```

Per spec 097 D8: chain emit failure is logged but does not change the exit code of the user-visible action. The schedule (or cancel) already succeeded; the audit log lost an entry. Operators can later replay from Temporal history if forensic reconstruction is needed.

## Reverse compatibility

These are two genuinely new event types. No existing chain reader will see them and break — chain readers in the current codebase filter on `event_type` and ignore unknown values. Adding `scheduler_started` / `scheduler_canceled` does not affect any existing chain consumer.
