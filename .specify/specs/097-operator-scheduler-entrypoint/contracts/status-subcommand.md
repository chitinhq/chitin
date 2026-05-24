# Contract: `chitin-orchestrator status [-run-id <id>] [--text]`

**Spec**: 097 | **FRs**: 001, 006, 007, 010, 011

## Synopsis

```text
chitin-orchestrator status [-run-id <id>] [--text] [--temporal-host <host:port>]
```

## Arguments

| Flag | Type | Required | Default | Description |
|---|---|---|---|---|
| `-run-id` | string | no | (absent) | Absent: list all active scheduler runs. Present: query one run. |
| `--text` | bool | no | false | Default JSON; `--text` switches to a fixed-column table. |
| `--temporal-host` | string | no | `$TEMPORAL_HOSTPORT` ?? `127.0.0.1:7233` | Temporal frontend host:port. |

## Behavior — no `-run-id` (list mode)

1. Connect to Temporal via `client.Dial`. Exit 2 on unreachable.
2. Call `client.ListWorkflow` with query `WorkflowType="SchedulerWorkflow" AND ExecutionStatus IN ("Running", "ContinuedAsNew")`.
3. For each returned execution, issue a Temporal `Query(workflowID, "status")` to fetch the live `SchedulerStatus` and read the `RunID`, `Tick`, `Frontier`. Combine with the execution's `StartTime` for `started_at` and (if available from the execution's input or memo) `spec_ref` for the listing.
4. Sort the result list by `started_at` descending.
5. Render to stdout: JSON array by default; fixed-column table under `--text`.
6. Exit 0.

JSON shape (one entry per active run):

```json
[
  {
    "run_id": "<uuid>",
    "spec_ref": "096-operator-session-state-surface",
    "tick": 5,
    "frontier_size": 2,
    "started_at": "2026-05-23T14:30:00Z"
  }
]
```

If no active runs exist, the JSON output is `[]` and exit code is 0 (an empty active set is not an error).

## Behavior — with `-run-id <id>` (inspect mode)

1. Connect to Temporal. Exit 2 on unreachable.
2. Call `client.QueryWorkflow(ctx, runID, "", "status")` to invoke spec 076's existing `status` query handler.
3. If the workflow does not exist, exit 1 with stderr `error: no scheduler run with run_id "<id>"`.
4. Render the returned `SchedulerStatus` JSON unchanged to stdout.
5. Exit 0.

JSON shape (the full SchedulerStatus from spec 076):

```json
{
  "run_id": "<uuid>",
  "tick": 5,
  "node_status": {
    "node-1": "done",
    "node-2": "running",
    "node-3": "pending"
  },
  "frontier": ["node-2"]
}
```

## Exit codes

- **0** — query succeeded (including the empty-list case).
- **1** — user error: invalid `-run-id` (no matching workflow).
- **2** — runtime error: Temporal unreachable, query error other than not-found.

## stderr messages

| Condition | Message |
|---|---|
| `-run-id` not found | `error: no scheduler run with run_id "<id>"` |
| Temporal unreachable | `error: Temporal unreachable at <host:port> — is the temporal-dev service running?` |
| Query handler returns error | `error: scheduler query failed: <underlying>` |

## Side effects

**None.** `status` is strictly read-only. No chain event is emitted. No state is mutated.

## Non-behaviors

- MUST NOT emit any chain event (no `scheduler_status_queried` or similar).
- MUST NOT block on workflow completion or wait for the next tick — return immediately with whatever the query handler reports right now.
- MUST NOT auto-refresh / poll — operators who want a loop wrap the subcommand in `watch -n 5 chitin-orchestrator status`.

## Operator examples

```bash
# List active scheduler runs:
chitin-orchestrator status

# List as a table:
chitin-orchestrator status --text

# Inspect one run:
chitin-orchestrator status -run-id abc123-4567-89ab-cdef-...

# Pipe through jq:
chitin-orchestrator status -run-id abc123-... | jq '.node_status'

# Watch (5s refresh):
watch -n 5 'chitin-orchestrator status --text'
```
