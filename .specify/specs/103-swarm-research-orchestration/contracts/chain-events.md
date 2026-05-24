# Contract — Chain event schemas (spec 103)

Three new event types, all emitted via the existing kernel emit path (`chitin-kernel emit -event-json -`). All wrap in the standard chain envelope (`event_id`, `prev_hash`, `this_hash`, `ts`, `event_type`, `workflow_run_id`, `payload`).

## Event 1: `swarm_invocation`

Emitted once per `SwarmInvocationWorkflow` firing (the workflow that scheduled invocations and `swarm-ask` calls dispatch through).

```json
{
  "event_type": "swarm_invocation",
  "workflow_run_id": "<temporal-run-id>",
  "payload": {
    "schedule_id": "ares-arxiv-scan",
    "agent": "ares",
    "gateway": "hermes-mcp",
    "gateway_session": "ares-default",
    "cadence": "6h",
    "message": "<literal message sent>",
    "tag": "research-scan",
    "skills": ["web-search", "arxiv-fetcher", "obsidian-vault"],
    "temporal_run_id": "<run-id>",
    "ts": "2026-05-24T12:00:00Z"
  }
}
```

**Required:** `schedule_id`, `agent`, `gateway`, `message`, `temporal_run_id`, `ts`. Optional: `gateway_session`, `tag`, `skills`, `cadence` (omitted for ad-hoc `swarm-ask` calls).

**For `swarm-ask` ad-hoc invocations:** `schedule_id` is `ad-hoc-<uuid>` and `cadence` is null/omitted.

## Event 2: `swarm_finding_queued`

Emitted by `FetchAndRead` activity after a queue row is inserted or upserted from a vault file.

```json
{
  "event_type": "swarm_finding_queued",
  "workflow_run_id": "<ingestion-workflow-run-id>",
  "payload": {
    "queue_id": "01HZQK9X8N0YR4S5T2VWMBJDFE",
    "source": "obsidian-vault",
    "agent_attribution": "ares",
    "tag": "research-scan",
    "topic": "AI Agent Governance",
    "file_path": "Research/AI Agent Governance/sources/2026-05-24-paper.md",
    "triggered_by_chain_event": "01HZQK..." | null,
    "ts": "2026-05-24T12:15:23Z"
  }
}
```

**Required:** `queue_id`, `source`, `file_path`, `ts`.
**Optional:** `agent_attribution`, `tag`, `topic`, `triggered_by_chain_event`.

Emitted on both insert AND upsert (re-ingestion). Operator can de-dupe by `(source, file_path)` if needed.

## Event 3: `swarm_finding_triaged`

Emitted by `swarm-queue mark` after a status transition.

```json
{
  "event_type": "swarm_finding_triaged",
  "workflow_run_id": "",
  "payload": {
    "queue_id": "01HZQK9X8N0YR4S5T2VWMBJDFE",
    "from_status": "unprocessed",
    "to_status": "spec_drafted",
    "spec_ref": "120-arxiv-spec-name" | null,
    "actor": "operator",
    "notes": "Promoted from research-scan tag." | null,
    "ts": "2026-05-25T09:00:00Z"
  }
}
```

**Required:** `queue_id`, `from_status`, `to_status`, `actor`, `ts`.
**Optional:** `spec_ref` (populated when `to_status == spec_drafted`), `notes`.

`workflow_run_id` is empty — this event is operator-CLI-initiated, not workflow-initiated.

## Failure event types (additional)

The spec mentions edge cases that need observable failure events. These are NOT in the spec.md FR-018 list but are necessary for the edge cases. Adding them here as part of the contract:

### `swarm_invocation_failed`

```json
{
  "event_type": "swarm_invocation_failed",
  "workflow_run_id": "<run-id>",
  "payload": {
    "schedule_id": "...",
    "agent": "ares",
    "gateway": "hermes-mcp",
    "failure_kind": "gateway_unreachable" | "session_unresolved" | "gateway_not_installed",
    "detail": "<error message, truncated 1 KiB>",
    "ts": "..."
  }
}
```

### `swarm_invocation_timeout`

```json
{
  "event_type": "swarm_invocation_timeout",
  "workflow_run_id": "<run-id>",
  "payload": {
    "schedule_id": "...",
    "agent": "...",
    "timeout_seconds": 300,
    "ts": "..."
  }
}
```

Both are emitted by `SwarmInvocationWorkflow` failure handlers.

## Dedup invariants

| Event | Dedup key | Source of truth |
|---|---|---|
| `swarm_invocation` | none (every firing emits one) | — |
| `swarm_finding_queued` | none (every Upsert emits one) | — |
| `swarm_finding_triaged` | none (every CLI invocation emits one) | — |
| `swarm_invocation_failed` | none | — |
| `swarm_invocation_timeout` | none | — |

No dedup: this is a stream, not a state. The chain is the audit log; if operators want deduped views they aggregate via `swarm-summary`.

## Replay invariants

All events participate in the standard `prev_hash`/`this_hash` chain. No special handling for replay.
