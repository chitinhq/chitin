# Contract: `chitin-orchestrator cancel -run-id <id>`

**Spec**: 097 | **FRs**: 001, 008, 009, 010, 011

## Synopsis

```text
chitin-orchestrator cancel -run-id <id> [-reason <text>] [--temporal-host <host:port>]
```

## Arguments

| Flag | Type | Required | Default | Description |
|---|---|---|---|---|
| `-run-id` | string | yes | — | The Temporal WorkflowID to cancel (== SchedulerInput.RunID per spec 097 D7). |
| `-reason` | string | no | `""` | Operator-supplied free-text reason; carried into the `scheduler_canceled` chain event. |
| `--temporal-host` | string | no | `$TEMPORAL_HOSTPORT` ?? `127.0.0.1:7233` | Temporal frontend host:port. |

## Behavior

1. Connect to Temporal. Exit 2 on unreachable.
2. Probe the workflow's state via `client.DescribeWorkflowExecution(ctx, runID, "")`.
   - If the workflow does not exist: exit 1 with stderr `error: no scheduler run with run_id "<id>"`.
   - If the workflow is in a terminal state (Completed, Failed, Canceled, Terminated, TimedOut): exit 1 with stderr `error: run_id "<id>" already in terminal state "<state>"`. No cancel issued. No chain event emitted. (Idempotency — cancel-of-terminal is a no-op, not a double-cancel.)
3. Call `client.CancelWorkflow(ctx, runID, "")`. Exit 2 on Temporal SDK error other than not-found.
4. Emit `scheduler_canceled` chain event via `chitin-kernel emit -event-json -` with payload `{event_type, run_id, reason, ts}`. Emit failure logs warn to stderr but does NOT change exit code (D8).
5. Print to stdout: `canceled run_id=<id> reason=<reason or "(none)">`.
6. Exit 0.

Note: exit 0 is returned as soon as Temporal accepts the cancellation, NOT once the workflow has fully wound down. The workflow honors the cancel at its next scheduler tick (≤30 seconds default per SC-004). Operators wanting to confirm wind-down can poll `status -run-id <id>` until the workflow is in Canceled state.

## Exit codes

- **0** — cancel signal accepted by Temporal (workflow may still be winding down).
- **1** — user error: invalid `-run-id` (no matching workflow), or already-terminal (idempotent rejection).
- **2** — runtime error: Temporal unreachable, SDK error.

## stderr messages

| Condition | Message |
|---|---|
| `-run-id` not found | `error: no scheduler run with run_id "<id>"` |
| Already terminal | `error: run_id "<id>" already in terminal state "<state>"` (state ∈ Completed/Failed/Canceled/Terminated/TimedOut) |
| Temporal unreachable | `error: Temporal unreachable at <host:port> — is the temporal-dev service running?` |
| Temporal SDK error | `error: cancel failed: <underlying>` |
| Chain emit failed (warn only) | `warning: chain emit failed: <error> — cancel succeeded; the audit chain lost this entry` |

## Side effects

- Sends a Temporal cancellation signal to the workflow.
- Emits one `scheduler_canceled` chain event (best-effort).
- Does not delete, archive, or modify Temporal history.
- Does not write to `.specify/`, `~/.chitin/gov.db`, or any other on-disk state.

## Non-behaviors

- MUST NOT cancel runs in terminal state (idempotency — see step 2).
- MUST NOT block on workflow wind-down; return as soon as Temporal accepts the signal.
- MUST NOT cascade-cancel child WorkUnitWorkflows directly; the scheduler workflow's own cancel-handling path (spec 076) is responsible for orderly teardown.

## Operator examples

```bash
# Cancel a run, no reason:
chitin-orchestrator cancel -run-id abc123-4567-...

# Cancel with reason:
chitin-orchestrator cancel -run-id abc123-... -reason "policy relaxed; rerun with new tasks.md"

# Cancel then confirm wind-down:
chitin-orchestrator cancel -run-id abc123-... -reason "operator abort"
while chitin-orchestrator status -run-id abc123-... 2>/dev/null | jq -e '.tick'; do sleep 5; done
echo "wound down"

# Idempotent retry — re-running cancel on the same id is safe:
chitin-orchestrator cancel -run-id abc123-...   # first time: exit 0, chain event emitted
chitin-orchestrator cancel -run-id abc123-...   # second time: exit 1, "already in terminal state Canceled"
```
