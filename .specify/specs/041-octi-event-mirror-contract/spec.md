# 041 — Octi event mirror contract (Temporal → chitin event store)

> Parent: spec 040 (Octi scaffolding, §R8 hook point).
> Closes Clawta critique #1 (agent-bus thread 17, msg 7690): invariant 7
> (replay from telemetry alone) requires Temporal decision events to
> mirror into chitin/Octi event store so audit reconstruction never
> depends on Temporal visibility APIs.

## Summary

Replace the spec-040 §R8 mirror stub with a durable contract: every
Temporal workflow decision event lands in chitin's event store
within a bounded latency budget, with a stable schema, sufficient
to re-derive workflow state, decision history, and "what is every
agent doing right now" answers — without ever calling Temporal's
visibility API or reading Temporal event history directly.

> *"Temporal executes; the event store explains."* — Clawta proposal,
> msg 7689.

This spec makes that maxim a tested invariant.

## Ticket refs

- Parent: spec 040 §R8 (mirror stub hook).
- Closes: Clawta critique #1 (msg 7690).
- Ares' "chitin event log + kernel gate already IS a durable
  execution engine" framing (msg 7685) is the design north star:
  Temporal owns execution; chitin owns audit. This spec defines the
  bridge.

## File-system scope

### MAY write under

- `swarm/octi/mirror/` — new Go package
  - `swarm/octi/mirror/mirror.go` — `MirrorEvent(ctx, event)` real impl
  - `swarm/octi/mirror/schema.go` — `OctiEvent` struct + JSON tags
  - `swarm/octi/mirror/interceptor.go` — Temporal `WorkflowInboundInterceptor`
    and `WorkflowOutboundInterceptor` that emit on every decision
  - `swarm/octi/mirror/sink.go` — write path to
    `~/.chitin/octi-events-YYYY-MM-DD.jsonl`
- `swarm/octi/mirror/tests/` — unit tests
- `swarm/octi/e2e/mirror_test.go` — e2e: workflow runs, mirror file
  contains the full decision trail, replay from mirror alone re-derives
  state
- `~/.chitin/octi-events-YYYY-MM-DD.jsonl` — runtime artifact (not
  committed; daily rotation by sink)
- `swarm/octi/cmd/octi-replay-from-mirror/` — operator CLI tool that
  takes a date range + workflow id and replays state from the mirror
  alone (proves the contract)

### MUST NOT write under

- Temporal's persistence backing (SQLite or Postgres) — read only
- `swarm/octi/workflows/` — workflows MUST NOT call mirror directly;
  the interceptor handles it
- `chitin.yaml` — policy unchanged
- `go/` (chitin kernel)

## Goal

A workflow run on the Octi worker emits, for every decision event
Temporal records, an equivalent `OctiEvent` to
`~/.chitin/octi-events-YYYY-MM-DD.jsonl` within 100ms of the
Temporal event landing. The operator can run `swarm/bin/octi
replay-from-mirror --workflow-id=<id>` and re-derive the same final
state Temporal reports, **without making a single call to the
Temporal visibility API**. Invariant 7 (replay from telemetry
alone) is thereafter an automated assertion in e2e CI, not a
hopeful claim.

## Requirements

### R1 — `OctiEvent` schema (stable)

```go
type OctiEvent struct {
    Schema      string          `json:"schema"`       // "octi.event.v1"
    EventID     string          `json:"event_id"`     // ulid; monotonic per workflow
    WorkflowID  string          `json:"workflow_id"`
    RunID       string          `json:"run_id"`
    WorkflowType string         `json:"workflow_type"`
    EventType   string          `json:"event_type"`   // see R2
    Timestamp   int64           `json:"ts_unix_ns"`   // from Temporal event, not wall clock
    Sequence    int64           `json:"seq"`          // Temporal event id, monotonic
    Payload     json.RawMessage `json:"payload"`      // event-type-specific (see R3)
    GateDecision *string        `json:"gate_decision,omitempty"` // chitin decision id if Activity emitted one
    AgentID     *string         `json:"agent_id,omitempty"`      // hermes / clawta / mini / red, if known
    ParentEventID *string       `json:"parent_event_id,omitempty"` // for child workflows + Activity completions
}
```

Schema is **frozen** at v1. Any field addition is a v2 schema in a
parallel file (`octi-events-v2-YYYY-MM-DD.jsonl`). Deletions are
forbidden.

### R2 — Event types mirrored

At minimum the following Temporal event types MUST mirror to OctiEvent:

| Temporal event | OctiEvent.EventType |
|---|---|
| `WorkflowExecutionStarted` | `workflow.started` |
| `WorkflowExecutionCompleted` | `workflow.completed` |
| `WorkflowExecutionFailed` | `workflow.failed` |
| `WorkflowExecutionTimedOut` | `workflow.timed_out` |
| `WorkflowExecutionCanceled` | `workflow.canceled` |
| `WorkflowExecutionContinuedAsNew` | `workflow.continued_as_new` |
| `ActivityTaskScheduled` | `activity.scheduled` |
| `ActivityTaskStarted` | `activity.started` |
| `ActivityTaskCompleted` | `activity.completed` |
| `ActivityTaskFailed` | `activity.failed` |
| `ActivityTaskTimedOut` | `activity.timed_out` |
| `TimerStarted` | `timer.started` |
| `TimerFired` | `timer.fired` |
| `SignalExternalWorkflowExecutionInitiated` | `signal.sent` |
| `WorkflowExecutionSignaled` | `signal.received` |
| `ChildWorkflowExecutionStarted` | `child_workflow.started` |
| `ChildWorkflowExecutionCompleted` | `child_workflow.completed` |
| `MarkerRecorded` (SideEffect) | `side_effect.recorded` |

Future Temporal event types added to the SDK are non-blocking — they
fall through to OctiEvent.EventType = `"unknown.<temporal_type>"`
and a warning log line. Audit consumers must tolerate unknown types
gracefully.

### R3 — Payload contract

Each EventType has a fixed payload schema declared in
`swarm/octi/mirror/schema.go`. Example for `activity.completed`:

```go
type ActivityCompletedPayload struct {
    ActivityType   string          `json:"activity_type"`
    ActivityID     string          `json:"activity_id"`
    Input          json.RawMessage `json:"input"`
    Result         json.RawMessage `json:"result"`
    AttemptCount   int32           `json:"attempt_count"`
    DurationMs     int64           `json:"duration_ms"`
}
```

Payload struct definitions are part of the v1 schema freeze.

### R4 — Mirror interceptor wires every event

The interceptor is registered on worker startup
(`swarm/octi/worker/register.go`) and wraps every workflow + activity
execution. The interceptor:

1. Captures every inbound + outbound decision via Temporal's
   interceptor API
2. Translates Temporal event types per R2
3. Builds the payload per R3
4. Writes the OctiEvent via the sink (R5)
5. Does NOT block workflow progress — sink writes are buffered + async

If the sink falls behind by more than 1000 events, the worker logs a
loud structured warning (`octi.mirror.sink_backpressure`) and a CI
e2e test asserts this backpressure indicator surfaces on a forced
load.

### R5 — Sink contract

`sink.go` writes OctiEvents to
`~/.chitin/octi-events-YYYY-MM-DD.jsonl` using:

- Append-only, line-delimited JSON
- Daily rotation at 00:00 UTC (matches chitin gov-decisions rotation)
- `fsync()` every 100 events or 100ms, whichever first
- Crash-resilience: an unflushed buffer on worker SIGTERM is flushed
  within the grace period; SIGKILL may lose ≤100 events but each event
  is idempotent on replay (R7)

### R6 — Replay-from-mirror tool

`swarm/octi/cmd/octi-replay-from-mirror/main.go` is a Go binary
installed as `swarm/bin/octi-replay-from-mirror`. Usage:

```
octi-replay-from-mirror --workflow-id=<id> [--run-id=<id>] [--since=YYYY-MM-DD]
```

Behavior:
1. Reads OctiEvents matching the workflow id from
   `~/.chitin/octi-events-*.jsonl` files
2. Sorts by `seq` (Temporal sequence, monotonic per workflow)
3. Reconstructs workflow state by replaying decisions against the
   workflow code in `swarm/octi/workflows/`
4. Outputs final state + decision history as JSON
5. Compares against the Temporal-canonical final state (optionally,
   if `--verify-against-temporal` flag passed) — but this is for
   testing only; production audit MUST NOT depend on it

### R7 — Idempotency

OctiEvent.EventID is a ULID derived deterministically from
(WorkflowID, RunID, Sequence). Re-mirroring the same Temporal event
produces the same EventID. Audit consumers MUST tolerate duplicate
EventIDs (treat second occurrence as a no-op).

### R8 — Latency budget

p99 latency from Temporal event landing to OctiEvent appearing in
the sink file MUST be < 100ms under load (≤100 events/sec sustained).
e2e test asserts.

### R9 — Schema versioning

If R1 changes shape, the new schema lives in a parallel file naming
scheme (`octi-events-v2-*.jsonl`). v1 readers keep reading v1; v2
readers read v2. There is no in-place migration of historical events.

### R10 — Audit query is one query

A canonical "what is every workflow doing right now" query is a
single jq invocation against today's mirror file:

```
jq -r 'select(.event_type | startswith("workflow.")) | "\(.workflow_id) \(.event_type) \(.timestamp)"' \
   ~/.chitin/octi-events-$(date -u +%F).jsonl \
   | sort -u
```

This query is enshrined as `swarm/bin/octi-snapshot` and documented
in spec 040 §R4 as a new operator-CLI verb (`octi snapshot`).

## Acceptance criteria

1. A hello-world workflow run (per spec 040 AC5) produces a complete
   OctiEvent trail in `~/.chitin/octi-events-*.jsonl` covering at
   least: `workflow.started`, `activity.scheduled`,
   `activity.started`, `activity.completed`, `workflow.completed`.
2. `octi-replay-from-mirror --workflow-id=<id>` re-derives the same
   final state Temporal reports, with exit code 0.
3. e2e test `swarm/octi/e2e/mirror_test.go` asserts AC1 + AC2 in
   CI, on every PR touching `swarm/octi/mirror/`.
4. p99 mirror latency < 100ms under 100 events/sec load, measured
   in `swarm/octi/e2e/mirror_perf_test.go`.
5. Schema struct definitions in `swarm/octi/mirror/schema.go` are
   marked with a `// SCHEMA: octi.event.v1 — FROZEN` comment;
   `golangci-lint` is configured to flag PRs that modify these
   structs without an accompanying v2 schema file.
6. `swarm/bin/octi snapshot` returns a JSON listing of every
   currently-in-flight workflow with no Temporal API call (verified
   by network-trace assertion in e2e: no traffic to Temporal
   frontend port during snapshot).
7. Daily rotation: a workflow that crosses 00:00 UTC produces
   OctiEvents in both YYYY-MM-DD.jsonl files cleanly (no missing
   events, no duplicate seq).
8. SIGTERM grace: workflow worker shutdown flushes mirror buffer
   within 5 seconds; e2e test asserts no event loss on graceful
   shutdown.
9. Idempotency: re-running the mirror over the same Temporal history
   produces the same OctiEvent set with identical EventIDs.
10. `golangci-lint` + CI gate refuses PRs that change `OctiEvent`
    field set without bumping the schema version per R9.

## Test coverage

- `swarm/octi/mirror/mirror_test.go` — unit: interceptor maps every
  Temporal event type to the correct OctiEvent.EventType per R2
- `swarm/octi/mirror/sink_test.go` — unit: sink fsync cadence, daily
  rotation, append-only invariant
- `swarm/octi/e2e/mirror_test.go` — **e2e**: AC1, AC2, AC6, AC7
- `swarm/octi/e2e/mirror_perf_test.go` — **e2e**: AC4 (latency)
- `swarm/octi/e2e/mirror_shutdown_test.go` — **e2e**: AC8 (SIGTERM)
- `swarm/octi/e2e/mirror_idempotency_test.go` — **e2e**: AC9

All test files carry `// spec: 041-octi-event-mirror-contract`.

## Invariants

- **I1** (closes Clawta critique #1): audit reconstruction NEVER
  reads from Temporal visibility API or Temporal event history
  directly — only from `~/.chitin/octi-events-*.jsonl`. Enforced
  by AC6's network-trace assertion.
- **I2**: schema v1 is frozen. New fields require v2 in a parallel
  file. Lint-enforced.
- **I3**: every Temporal decision event (R2 set) appears in the
  mirror within the latency budget (R8). e2e-enforced.
- **I4**: mirror writes do not block workflow progress.
  Backpressure surfaces as a loud log line, not a deadlock.
- **I5**: replay from mirror alone is bit-for-bit equivalent to
  replay from Temporal history. e2e-enforced.

## Out of scope

- Replicating mirror to a second store (S3, Postgres, etc.) — file
  is the source-of-truth for v1
- Compaction / archival of old `.jsonl` files — operator-managed
  for v1; spec a rotation policy in a later spec
- Reading mirror from Octi Web UI — spec 048 may add UI; v1 is
  CLI-only
- Mirror to chitin's `gov-decisions-*.jsonl` directly — Octi events
  are a peer file, not a merger. The two streams join by
  GateDecision id (R1).
- Cross-host clock skew — single-host deployment assumed for v1;
  spec 048 (HA) addresses

## References

- Parent: `.specify/specs/040-octi-scaffolding/spec.md` §R8
- Closes: Clawta critique #1, agent-bus thread 17 msg 7690
- Ares' "Temporal becomes a second source of truth" risk callout
  (msg 7687 §7) — this spec is the mitigation
- Temporal interceptor API:
  https://docs.temporal.io/develop/go/interceptors
- chitin gov-decisions format:
  `~/.chitin/gov-decisions-YYYY-MM-DD.jsonl` (existing convention)
