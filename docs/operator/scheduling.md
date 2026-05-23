# Operator runbook — scheduling implementation work through the orchestrator

Spec 097 added three subcommands to the `chitin-orchestrator` binary that
let operators dispatch implementation work-units against the spec-DAG
scheduler (spec 076) without manual `temporal workflow start` invocations.

```text
chitin-orchestrator                                  # worker host (default; what chitin-orchestrator.service runs)
chitin-orchestrator schedule <spec-ref>              # compile a spec and start a SchedulerWorkflow run
chitin-orchestrator status [-run-id <id>] [--text]   # list active runs OR inspect one
chitin-orchestrator cancel  -run-id <id> [-reason <text>]   # graceful Temporal cancel
chitin-orchestrator help                             # one-screen usage reference
```

The worker-host default (no-args invocation) is byte-identical to its
pre-spec-097 behavior; the systemd unit `chitin-orchestrator.service`
keeps working without changes.

## When to use each subcommand

| Situation | Subcommand |
|---|---|
| A spec has just merged with a `tasks.md` ready for implementation | `schedule` |
| An incident — "which scheduler runs are active right now?" | `status` (no `-run-id`) |
| Inspecting one run mid-flight — "what's frontier/tick on run X?" | `status -run-id <id>` |
| Policy changed; need to abort a runaway run | `cancel -run-id <id> -reason "..."` |
| Re-running a spec after manually cleaning a partial failure | `schedule <ref>` (new RunID; old run can be canceled or left to terminal) |

## `schedule` — start a new scheduler run

```bash
chitin-orchestrator schedule 096
chitin-orchestrator schedule 096-operator-session-state-surface       # full directory name
chitin-orchestrator schedule operator-session-state-surface           # slug-only
chitin-orchestrator schedule 096 --temporal-host 127.0.0.1:17233      # sandbox Temporal
chitin-orchestrator schedule 096 --repo-root /home/red/workspace/chitin
```

### Flow

1. Resolve `<spec-ref>` to a unique `.specify/specs/NNN-name/` directory (exact → numeric prefix → slug; ambiguous matches exit 1).
2. Compile via the spec-077 adapter (`speckit.New().CompileSpec`).
3. **Pre-validate** the DAG — refuse to dispatch if any node carries an unclassified capability (the spec-077 adapter couldn't map its description to a capability keyword set) or an unroutable capability (no registered driver declares it).
4. ExecuteWorkflow against the `chitin` task queue. A fresh UUID is generated as both the Temporal `WorkflowID` and the `SchedulerInput.RunID`.
5. Emit a `scheduler_started` chain event via `chitin-kernel emit`.
6. Print the RunID on success.

### Success output

```text
scheduled spec 096-operator-session-state-surface (8 nodes, 3 capabilities required); run_id=<uuid>
```

### Common errors

| Operator-visible message | What happened | Fix |
|---|---|---|
| `error: no spec matching ref "..."` (followed by a list of available specs) | Typo or missing spec | Re-type with the right ref |
| `error: ref "..." is ambiguous — matched N specs:` | Numeric prefix matched more than one spec dir | Use a longer prefix or the slug |
| `error: spec X compile failed: tasks.md not found` | Spec exists but has no tasks.md yet | Run `/speckit-tasks` against the spec first |
| `error: DAG validation failed — N node(s) have unclassified capability` | A task description in tasks.md didn't match any capability keyword set | Edit `tasks.md` to use recognized verbs ("implement the …", "refactor …", "review …", etc.) |
| `error: DAG validation failed — N node(s) require capability not declared by any registered driver` | A capability is valid but no driver in the registry declares it | Either register a driver that does, or amend tasks.md |
| `error: Temporal unreachable at <host:port> — is the temporal-dev service running?` | Temporal dev server is down | `systemctl --user start temporal-dev.service` (or however your box runs Temporal) |

### Exit codes (FR-011)

- **0** — success (workflow scheduled; chain emit may still have warned but the schedule succeeded)
- **1** — user error (bad ref, ambiguous, missing tasks.md, validation failure)
- **2** — runtime error (Temporal unreachable, IO failure, ExecuteWorkflow internal error)

## `status` — inspect active runs

### List mode

```bash
chitin-orchestrator status                # JSON array, all active runs
chitin-orchestrator status --text         # human-readable table
chitin-orchestrator status | jq '.[] | select(.tick > 5)'   # active runs past tick 5
```

JSON output shape:

```json
[
  {
    "run_id": "<uuid>",
    "spec_ref": "",
    "tick": 5,
    "frontier_size": 2,
    "started_at": "2026-05-23T14:30:00Z"
  }
]
```

Sorted by `started_at` descending (most recent first), with `run_id` as the
tie-breaker for stability. `spec_ref` is currently always empty in v1 (the
spec ref isn't stored on the Temporal execution; an amendment in a future
PR may fix this if it proves useful).

### Inspect mode

```bash
chitin-orchestrator status -run-id <uuid>           # full SchedulerStatus JSON
chitin-orchestrator status -run-id <uuid> --text    # summary line + node-status table
```

JSON output shape matches `workflows.SchedulerStatus`:

```json
{
  "run_id": "<uuid>",
  "tick": 5,
  "node_status": {
    "node-1": "done",
    "node-2": "running",
    "node-3": "pending"
  },
  "frontier": ["node-2"],
  "running": [],
  "stalled": false,
  "complete": false
}
```

### Common errors

| Message | Fix |
|---|---|
| `error: no scheduler run with run_id "..."` | Typo'd run_id, or the run has been purged from Temporal history |
| `error: Temporal unreachable at ...` | Same fix as schedule's Temporal-unreachable case |

### Side effects

**None.** `status` is strictly read-only — no chain event, no Temporal state mutation. Safe to wrap in `watch -n 5`.

## `cancel` — gracefully stop a run

```bash
chitin-orchestrator cancel -run-id <uuid>
chitin-orchestrator cancel -run-id <uuid> -reason "policy relaxed; rerun with new tasks.md"
```

### Flow

1. DescribeWorkflowExecution to probe state.
2. If the workflow is in any terminal state (Completed, Failed, Canceled, Terminated, TimedOut) → exit 1 with `already in terminal state X`. **Idempotent** — re-running cancel against a finished run is a no-op, NOT a double-cancel.
3. Otherwise, send the Temporal cancellation signal. Returns 0 as soon as Temporal accepts (not after wind-down).
4. Emit a `scheduler_canceled` chain event with the operator's reason (fail-soft).
5. Print confirmation.

The workflow honors the cancel at its next scheduler tick (≤30 seconds in default config). To confirm wind-down, poll `status -run-id <id>`.

### Common errors

| Message | Fix |
|---|---|
| `error: -run-id is required` | Pass the `-run-id` flag |
| `error: no scheduler run with run_id "..."` | Typo'd run_id |
| `error: run_id "..." already in terminal state "Canceled"` | The run already wound down; nothing to cancel |

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `TEMPORAL_HOSTPORT` | `127.0.0.1:7233` | Temporal frontend host:port. Overridden by `--temporal-host` flag. |
| `CHITIN_REPO_ROOT` | `git rev-parse --show-toplevel` from cwd | Repo root for spec-ref resolution. Overridden by `--repo-root` flag. |
| `CHITIN_KERNEL_BIN` | `chitin-kernel` (PATH lookup) | Path to the kernel binary used for chain emit. Override for sandboxes where the kernel isn't on PATH. |
| `CHITIN_WORKTREE_ROOT` | `/tmp/chitin-worktrees` | Worker-host's worktree manager root. Only used in worker-host mode. |

Flag overrides env; env overrides default.

## Chain events emitted

| Event type | Emitted by | When |
|---|---|---|
| `scheduler_started` | `schedule` | After ExecuteWorkflow returns successfully |
| `scheduler_canceled` | `cancel` | After CancelWorkflow returns successfully (NOT emitted for cancel-of-terminal — that's an idempotent no-op) |

Both events are emitted via the canonical `chitin-kernel emit -event-json -` path per constitution §1. Their schemas are documented in `specs/097-operator-scheduler-entrypoint/contracts/chain-events.md`.

To audit historic scheduler activity:

```bash
# Every spec ever scheduled, chronological:
jq 'select(.event_type=="scheduler_started")' ~/.chitin/events-*.jsonl

# All cancels with reasons:
jq -c 'select(.event_type=="scheduler_canceled") | {run_id: .payload.run_id, reason: .payload.reason, ts}' ~/.chitin/events-*.jsonl
```

## Constitution §7 alignment

Spec 097 closed the gap that made §7's *"implementation MUST flow through the orchestrator"* enforceable. Every implementation work-unit that lands going forward can trace its origin back to a `scheduler_started` chain event for the spec it derives from. The `scheduler_started` event IS the audit anchor.

Operators should treat ad-hoc `temporal workflow start --type SchedulerWorkflow` invocations as a smell — there's now a first-class path that handles compile, validate, and audit in one go.

## Troubleshooting

**Q: I scheduled a run but `status` doesn't list it.**
A: Check that you're querying the same Temporal cluster — `chitin-orchestrator status --temporal-host <host:port>` matches whatever you passed to `schedule`. Also note `status` filters to `ExecutionStatus = "Running"` — a workflow that already completed won't appear in the list.

**Q: My schedule succeeded but I see no `scheduler_started` event in the chain.**
A: Chain emit is fail-soft (D8 per the spec). Check stderr from your schedule invocation for a `warning: chain emit failed:` line. Usually means `chitin-kernel` isn't on PATH or `$CHITIN_KERNEL_BIN` points at a missing binary. The schedule did succeed — the workflow IS in Temporal — but the audit log lost the entry.

**Q: My DAG validation fails with `unclassified capability` but I'm sure my tasks are coherent.**
A: The spec-077 adapter uses keyword-based mapping (`adapter/context.go` has the table). A task description with no recognized keyword maps to `NEEDS CLARIFICATION`. Edit `tasks.md` to include one of the recognized verbs (e.g., "implement the X", "refactor Y", "review the Z"), or amend the capability taxonomy if a new category is warranted (that's a spec amendment, not an ad-hoc change).

**Q: Cancel exited 1 with `already in terminal state Completed`. Did my cancel work?**
A: The workflow finished on its own before your cancel landed. No cancel was issued (idempotent reject); no chain event was emitted; the run is in its natural terminal state.
