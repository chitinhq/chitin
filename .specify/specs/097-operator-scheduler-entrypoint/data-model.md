# Data Model: Operator entrypoint for the spec-DAG scheduler

**Feature**: 097-operator-scheduler-entrypoint
**Date**: 2026-05-23

Structural decomposition of the data the implementation touches — argv parsing, CLI input/output shapes, chain event payloads, and the values passed to Temporal client calls. This spec adds no new database schema and no new on-disk state; everything is either Temporal-owned or chain-event-owned.

## Entity 1 — Subcommand argv

The shell-level input. Parsed by Go's standard `flag` package, scoped per subcommand.

### `schedule <spec-ref> [--temporal-host <host:port>] [--repo-root <path>]`

| Position / Flag | Type | Required | Notes |
|---|---|---|---|
| `<spec-ref>` (positional) | string | yes | Per D9 resolution. Exact dir name, numeric prefix, or slug. |
| `--temporal-host` | string | no | Default: `$TEMPORAL_HOSTPORT` else `127.0.0.1:7233`. |
| `--repo-root` | string | no | Default: `$CHITIN_REPO_ROOT` else `git rev-parse --show-toplevel`. |

### `status [-run-id <id>] [--text] [--temporal-host <host:port>]`

| Flag | Type | Required | Notes |
|---|---|---|---|
| `-run-id` | string | no | Absent: list all active scheduler runs. Present: query one run. |
| `--text` | bool | no | Default JSON; `--text` switches to a fixed-column table. |
| `--temporal-host` | string | no | Same default as schedule. |

### `cancel -run-id <id> [-reason <text>] [--temporal-host <host:port>]`

| Flag | Type | Required | Notes |
|---|---|---|---|
| `-run-id` | string | yes | The Temporal WorkflowID (== SchedulerInput.RunID per D7). |
| `-reason` | string | no | Operator-supplied; carried into the `scheduler_canceled` chain event. |
| `--temporal-host` | string | no | Same default as schedule. |

## Entity 2 — Spec ref resolution result

The intermediate produced by `schedule` after parsing `<spec-ref>`. Internal to the subcommand handler; not exposed to the operator.

```go
type SpecRefResolution struct {
    SpecDir   string // absolute path: "/home/red/workspace/chitin/specs/096-operator-session-state-surface"
    SpecRef   string // canonical form: "096-operator-session-state-surface" (the directory's basename)
    Numeric   string // "096"
    Slug      string // "operator-session-state-surface"
}
```

State transitions:
- input arg → tried as exact dir match → tried as numeric prefix → tried as slug → either success or `error 1` with candidate list.

## Entity 3 — Validation result

Produced by `validate.go`'s `ValidateForDispatch(dag.DAG, driver.Registry)`. Returns empty for a valid DAG; populated for invalid.

```go
type ValidationError struct {
    NodeID       string  // the offending DAG node
    Capability   string  // the capability the node declares (or "" if absent)
    Kind         string  // "needs_clarification" | "unroutable" | "missing_capability"
    Detail       string  // operator-readable explanation
}

// Empty slice = valid; non-empty slice = invalid (refuse dispatch).
type ValidationResult []ValidationError
```

**State invariants**:
- A DAG that produces empty `ValidationResult` is dispatched (`ExecuteWorkflow` called).
- A DAG that produces non-empty `ValidationResult` exits 1 with the list rendered to stderr; no workflow starts; no chain event emitted.

## Entity 4 — Temporal client call inputs

The values the subcommand passes to the Temporal SDK. These are pass-through to existing spec-076 types; this spec does not redefine them.

### `client.ExecuteWorkflow` (from `schedule`)

```go
runID := uuid.NewString()
options := client.StartWorkflowOptions{
    ID:        runID,
    TaskQueue: TaskQueue, // existing constant from cmd/chitin-orchestrator/main.go
}
input := workflows.SchedulerInput{
    RunID: runID,
    Nodes: compiledNodes, // from speckit.New().CompileSpec(repoRoot, specRef)
    Edges: compiledEdges,
    Tick:  0,
}
client.ExecuteWorkflow(ctx, options, "SchedulerWorkflow", input)
```

### `client.QueryWorkflow` (from `status -run-id <id>`)

```go
client.QueryWorkflow(ctx, runID, "" /* runID empty = latest */, "status")
// Returns workflows.SchedulerStatus from the existing query handler.
```

### `client.ListWorkflows` (from `status` with no `-run-id`)

```go
client.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
    Query: `WorkflowType="SchedulerWorkflow" AND ExecutionStatus IN ("Running", "ContinuedAsNew")`,
})
```

### `client.CancelWorkflow` (from `cancel`)

```go
client.CancelWorkflow(ctx, runID, "" /* runID empty = latest */)
```

## Entity 5 — Subcommand output shapes

### `schedule` stdout (success)

```text
scheduled spec 096-operator-session-state-surface (8 nodes, 3 capabilities required); run_id=<uuid>
```

JSON-only mode is not provided in v1 — `schedule` is a one-shot operator action; the single human-readable line is sufficient.

### `status` (no `-run-id`) — JSON default

```json
[
  {
    "run_id": "<uuid>",
    "spec_ref": "096-operator-session-state-surface",
    "tick": 5,
    "frontier_size": 2,
    "started_at": "2026-05-23T14:30:00Z"
  },
  ...
]
```

Sorted by `started_at` descending.

### `status` (no `-run-id`) — `--text` mode

```text
RUN_ID                                SPEC_REF                                    TICK  FRONTIER  STARTED_AT
abc123-4567-89ab-cdef-...             096-operator-session-state-surface             5         2  2026-05-23T14:30:00Z
def456-...                            091-fix-clawta-lockdown-loop                  12         0  2026-05-23T13:45:00Z
```

### `status -run-id <id>` — JSON (matches `workflows.SchedulerStatus`)

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

### `cancel` stdout

```text
canceled run_id=<uuid> reason="<reason or '(none)'>"
```

## Entity 6 — `scheduler_started` chain event

Emitted via `chitin-kernel emit -event-json -` immediately after `client.ExecuteWorkflow` returns successfully from `schedule`.

| Field | Type | Notes |
|---|---|---|
| `event_type` | `"scheduler_started"` | Constant. |
| `agent_instance_id` | string | The invoking CLI's identifier: `chitin-orchestrator-cli-<pid>`. |
| `payload.spec_ref` | string | The canonical spec ref (Entity 2's `SpecRef`). |
| `payload.run_id` | string | The Temporal RunID == SchedulerInput.RunID. |
| `payload.node_count` | int | Number of nodes in the compiled DAG. |
| `payload.capabilities_required` | []string | Sorted, deduplicated list of capability tags across all nodes. |
| `ts` | string (RFC3339) | Emission timestamp. |
| `chain_id` / `seq` / `prev_hash` / `this_hash` | (chain frame) | Standard chain framing — supplied by `chitin-kernel emit`. |

**Emission mechanism**: same subprocess pattern as `emitStopSignalIgnored` in the openclaw plugin (spec 091 FR-009) — `spawn("chitin-kernel", ["emit", "-event-json", "-"], stdio: pipe)`; write JSON to stdin; close.

**Failure behavior**: per D8 — log a warning to stderr, exit 0 (the schedule already succeeded; the audit lost an entry, not a transaction).

## Entity 7 — `scheduler_canceled` chain event

Emitted via `chitin-kernel emit` immediately after `client.CancelWorkflow` returns successfully from `cancel`.

| Field | Type | Notes |
|---|---|---|
| `event_type` | `"scheduler_canceled"` | Constant. |
| `agent_instance_id` | string | `chitin-orchestrator-cli-<pid>` (same form as `scheduler_started`). |
| `payload.run_id` | string | The canceled Temporal RunID. |
| `payload.reason` | string | Operator-supplied via `-reason`; `""` if absent. |
| `ts` | string (RFC3339) | Emission timestamp. |
| `chain_id` / `seq` / `prev_hash` / `this_hash` | (chain frame) | Standard chain framing. |

Same emission and failure behavior as `scheduler_started`.

## Sequence — happy path (schedule)

```
1. Operator: chitin-orchestrator schedule 096
2. CLI parses argv → SubcommandArgs{Subcommand: "schedule", SpecRef: "096"}
3. CLI resolves spec ref → SpecRefResolution{SpecDir: ".../specs/096-...", SpecRef: "096-operator-session-state-surface", ...}
4. CLI compiles spec → speckit.New().CompileSpec(repoRoot, specRef) → dag.DAG{nodes, edges}
5. CLI validates → ValidateForDispatch(dag, registry) → ValidationResult{} (empty = valid)
6. CLI generates RunID → uuid.NewString() → "abc123-..."
7. CLI calls Temporal → client.ExecuteWorkflow(ctx, StartWorkflowOptions{ID: "abc123-...", ...}, "SchedulerWorkflow", SchedulerInput{RunID: "abc123-...", Nodes, Edges, Tick: 0})
8. ExecuteWorkflow returns successfully (workflow scheduled, not yet started running)
9. CLI emits chain event → chitin-kernel emit -event-json - {event_type: "scheduler_started", payload: {spec_ref, run_id, node_count, capabilities_required}, ts}
10. CLI prints to stdout: "scheduled spec 096-operator-session-state-surface (N nodes, M capabilities required); run_id=abc123-..."
11. CLI exits 0
```

## Sequence — validation failure (schedule)

```
1-4. Same as happy path
5. CLI validates → ValidateForDispatch returns non-empty ValidationResult
6. CLI renders to stderr:
     "error: DAG validation failed — refusing to dispatch a run with unroutable nodes:
        - node-3: capability 'needs_clarification' (task description didn't match a capability keyword)
        - node-7: capability 'code.compile' is not declared by any registered driver"
7. CLI exits 1 (NOT 2 — operator-fixable input)
8. No workflow started, no chain event emitted
```

## Sequence — missing or ambiguous spec ref (schedule)

```
1. Operator: chitin-orchestrator schedule 09
2. CLI parses argv → SubcommandArgs{Subcommand: "schedule", SpecRef: "09"}
3. CLI attempts resolution:
   - Exact match: no .specify/specs/09/ directory → continue
   - Numeric prefix: ".specify/specs/09*-*" matches {091-, 092-, 093-, 094-, 095-, 096-} → AMBIGUOUS
4. CLI renders to stderr:
     "error: ref "09" is ambiguous — matched 6 specs:
        091-fix-clawta-lockdown-loop
        092-codify-swarm-orchestrator
        093-merge-queue-orchestrator
        094-pr-review-mechanism
        095-continue-checks-pilot
        096-operator-session-state-surface
        097-operator-scheduler-entrypoint"
5. CLI exits 1
6. No further action.
```

## Sequence — Temporal unreachable

```
1-5. Same as happy path through validation
6. CLI attempts client.ExecuteWorkflow → connection error from Temporal SDK
7. CLI renders to stderr: "error: Temporal unreachable at 127.0.0.1:7233 — is the temporal-dev service running?"
8. CLI exits 2 (runtime error, distinct from user-error exit code 1)
9. No chain event emitted
```

## Validation invariants (what tests must assert)

| Invariant | How to check |
|---|---|
| No-args invocation runs the worker host | Spawn `chitin-orchestrator` with no args; assert the process registers workflows and stays alive |
| Subcommand invocation does NOT register workflows | Run `chitin-orchestrator schedule <fixture>`; assert worker registration code is not invoked |
| `schedule` exits 0 on valid input and emits a `scheduler_started` event | Round-trip integration test against fixture spec |
| `schedule` exits 1 (not 2) on bad spec ref | Run `chitin-orchestrator schedule 999`; assert exit code 1 and stderr contains "no spec matching" |
| `schedule` exits 1 (not 2) on ambiguous ref | Run with prefix that matches multiple specs; assert exit 1 and stderr lists candidates |
| `schedule` exits 2 on Temporal unreachable | Set TEMPORAL_HOSTPORT to a closed port; assert exit 2 |
| `schedule` exits 0 even when chain emit fails (per D8) | Rename `chitin-kernel`; assert schedule still exits 0, prints success line, with a warning to stderr |
| `status` (no -run-id) returns JSON sorted by started_at descending | Start two known runs at known times; assert JSON output ordering |
| `cancel` of terminal-state run exits 1 with terminal-state name | Schedule, wait for completion, then cancel; assert exit 1 and stderr names "completed" |
| `cancel` is idempotent for already-canceled runs | Cancel twice in succession; assert second exit code 1 with "already canceled" or "terminal" |
| Chain emit failures log warn to stderr but do not roll back | Cover for both schedule and cancel paths |
