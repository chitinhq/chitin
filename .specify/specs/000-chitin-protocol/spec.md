# Chitin Protocol

Status: Draft for publication
Protocol version: `v0.1`
Wire schema version: `schema_version: "2"`

## Purpose

This document defines the open wire format for chitin chain events.
Third-party tools may read, validate, and emit these events without
linking against `chitin-kernel`.

This spec freezes:

- the event envelope
- the canonical JSONL transport
- the hash-link rules
- the governance action enum
- the governance severity ladder
- the `v0.1` event-type set

This spec does not define policy evaluation, routing heuristics,
operator approvals, or SQLite index layouts. Those are implementation
details above or beside the wire protocol.

## Versioning

Two version axes exist and are independent:

- Protocol version: `v0.1` in this document. This version names the
  public open spec.
- Wire schema version: `schema_version`, currently the literal string
  `"2"` on every event row.

Rule: an event conforming to protocol `v0.1` MUST carry
`"schema_version":"2"`.

## Transport

Events are stored and exchanged as UTF-8 JSON Lines:

- one JSON object per line
- newline-delimited
- append-only
- no outer array
- each line is a complete event record

File naming is out of scope for interoperability, but the kernel uses
`events-<run_id>.jsonl`.

## Canonical JSON Form

Hashing uses canonical JSON over the event object with `this_hash`
excluded from the hash input.

Canonicalization rules:

1. Serialize as UTF-8 JSON.
2. Sort object keys lexicographically at every object level.
3. Emit no insignificant whitespace.
4. Preserve array order exactly.
5. Use standard JSON scalars and escaping.
6. Encode SHA-256 digests as 64-character lowercase hex.

Pseudocode:

```text
hash_input = canonical_json(event without this_hash)
this_hash = sha256_hex(hash_input)
```

## Event Envelope

Every event row MUST contain these top-level fields.

| Field | Type | Rules |
|------|------|-------|
| `schema_version` | string | MUST be `"2"` |
| `run_id` | string | Non-empty run identifier |
| `session_id` | string | Non-empty session identifier |
| `surface` | string | Non-empty driver/substrate identifier such as `codex`, `claude-code`, `openclaw` |
| `driver_identity.user` | string | Non-empty |
| `driver_identity.machine_id` | string | Non-empty |
| `driver_identity.machine_fingerprint` | string | 64-char lowercase SHA-256 hex |
| `agent_instance_id` | string | Non-empty agent instance identifier |
| `parent_agent_id` | string or null | Non-empty when present |
| `agent_fingerprint` | string | 64-char lowercase SHA-256 hex |
| `event_type` | string | Member of the frozen event-type set in this spec |
| `chain_id` | string | Non-empty logical chain identifier |
| `chain_type` | string | `session` or `tool_call` |
| `parent_chain_id` | string or null | Non-empty when present |
| `seq` | integer | Zero-based, non-negative |
| `prev_hash` | string or null | 64-char lowercase SHA-256 hex or `null` at chain head |
| `this_hash` | string | 64-char lowercase SHA-256 hex computed from canonical JSON |
| `ts` | string | RFC 3339 timestamp |
| `labels` | object | String-to-string map; MAY be empty |
| `payload` | object | Event-type-specific payload |

Notes:

- `chain_type:"session"` is the default session timeline.
- `chain_type:"tool_call"` is for a per-tool subchain when a producer
  wants finer-grained linkage.
- `parent_chain_id` links a child chain back to its parent chain.

## Chain Integrity Rules

For every `chain_id`, rows MUST form a contiguous linked list.

Required invariants:

1. The first row in a chain MUST have `seq = 0`.
2. The first row in a chain MUST have `prev_hash = null`.
3. For every row with `seq = n > 0`, `prev_hash` MUST equal the
   previous row's `this_hash`.
4. Sequence numbers MUST be contiguous: `0, 1, 2, ...`.
5. `this_hash` MUST equal the SHA-256 of the canonical JSON form of
   the same row with `this_hash` omitted.

Consumers SHOULD reject a chain with:

- a sequence gap
- a duplicate sequence with different `this_hash`
- a non-null `prev_hash` at `seq = 0`
- a `prev_hash` mismatch

## Frozen Event Types for `v0.1`

The public event-type set frozen by this spec is:

- `session_start`
- `user_prompt`
- `assistant_turn`
- `compaction`
- `session_end`
- `intended`
- `executed`
- `failed`
- `decision`
- `model_turn`
- `webhook_received`
- `webhook_failed`
- `session_stuck`

Reserved but not part of `v0.1`:

- `policy_decided`
- `rewritten`
- `denied`

Router and sentinel-facing signals are not `v0.1` chain event types.
Current kernel router work stamps signal metadata such as
`predicted_blast`, `floundering_score`, `drift_score`, and
`routing_decision` onto `gov-decisions-*.jsonl` rows with
`action_type:"router.signal"`. Those governance decision rows are
outside this `events-*.jsonl` wire contract, so a value such as `drift`
is a signal name, not a conforming `v0.1` `event_type`.

## Payload Schemas

### `session_start`

```json
{
  "cwd": "string",
  "client_info": { "name": "string", "version": "string" },
  "model": {
    "name": "string",
    "provider": "string",
    "version": "string?",
    "context_window": "integer?"
  },
  "system_prompt_hash": "sha256hex",
  "tool_allowlist_hash": "sha256hex",
  "soul_id": "string?",
  "soul_hash": "sha256hex?",
  "agent_version": "string",
  "spawning_tool_call_id": "string?",
  "task_description": "string?"
}
```

### `user_prompt`

```json
{
  "text": "string",
  "attachments": [
    {
      "kind": "string",
      "path": "string?",
      "data": "string?"
    }
  ]
}
```

### `assistant_turn`

```json
{
  "text": "string",
  "thinking": "string?",
  "model_used": {
    "name": "string",
    "provider": "string",
    "version": "string?",
    "context_window": "integer?"
  },
  "usage": {
    "input_tokens": "integer",
    "output_tokens": "integer",
    "cache_creation_input_tokens": "integer?",
    "cache_read_input_tokens": "integer?",
    "thinking_tokens": "integer?"
  },
  "ts_start": "rfc3339",
  "ts_end": "rfc3339"
}
```

### `compaction`

```json
{
  "reason": "string",
  "pre_token_count": "integer?",
  "post_token_count": "integer?",
  "summary": "string?"
}
```

### `session_end`

```json
{
  "reason": "string",
  "totals": {
    "turn_count": "integer",
    "tool_call_count": "integer",
    "total_input_tokens": "integer",
    "total_output_tokens": "integer",
    "total_duration_ms": "integer"
  }
}
```

### `intended`

`intended` records a proposed tool call before execution.

```json
{
  "tool_name": "string",
  "raw_input": { "...": "any" },
  "canonical_form": { "...": "any" },
  "action_type": "enum"
}
```

The `intended.action_type` enum is the coarse lifecycle enum:

- `read`
- `write`
- `exec`
- `git`
- `net`
- `dangerous`

### `executed`

```json
{
  "duration_ms": "integer",
  "output_preview": "string?",
  "output_bytes_total": "integer?"
}
```

### `failed`

```json
{
  "duration_ms": "integer",
  "error_kind": "string",
  "error": "string",
  "output_preview": "string?"
}
```

### `decision`

`decision` is the governance event emitted for a gated tool call. It is
part of the frozen `v0.1` protocol even though some in-repo schema code
still models the older subset.

Required fields:

```json
{
  "tool_name": "string",
  "action_type": "string",
  "action_target": "string",
  "decision": "allow|deny",
  "rule_id": "string"
}
```

Optional fields commonly emitted:

```json
{
  "reason": "string?",
  "suggestion": "string?",
  "corrected_command": "string?",
  "driver": "string?",
  "agent": "string?",
  "agent_instance_id": "string?"
}
```

`decision.action_type` SHOULD use the canonical governance action enum
defined below.

### `model_turn`

```json
{
  "model_name": "string",
  "provider": "string",
  "input_tokens": "integer",
  "output_tokens": "integer",
  "session_id_external": "string?",
  "duration_ms": "integer?",
  "cache_read_tokens": "integer?",
  "cache_write_tokens": "integer?"
}
```

### `webhook_received`

```json
{
  "channel": "string",
  "webhook_type": "string",
  "duration_ms": "integer",
  "chat_id": "string?"
}
```

### `webhook_failed`

```json
{
  "channel": "string",
  "webhook_type": "string",
  "error_message": "string",
  "chat_id": "string?"
}
```

### `session_stuck`

```json
{
  "state": "string",
  "age_ms": "integer",
  "session_id_external": "string?",
  "session_key": "string?",
  "queue_depth": "integer?"
}
```

## Canonical Governance Action Enum

When a producer emits governance-aware events, especially `decision`,
it SHOULD classify actions using this canonical enum:

- `shell.exec`
- `file.read`
- `file.write`
- `file.delete`
- `file.move`
- `file.recursive_delete`
- `git.diff`
- `git.log`
- `git.status`
- `git.commit`
- `git.checkout`
- `git.branch.create`
- `git.branch.delete`
- `git.merge`
- `git.push`
- `git.force-push`
- `git.worktree.list`
- `git.worktree.add`
- `git.worktree.remove`
- `github.pr.create`
- `github.pr.view`
- `github.pr.list`
- `github.pr.merge`
- `github.pr.close`
- `github.issue.list`
- `github.issue.view`
- `github.issue.create`
- `github.issue.close`
- `github.api`
- `delegate.task`
- `http.request`
- `npm.install`
- `npm.script.run`
- `test.run`
- `mcp.call`
- `memory.access`
- `tool.custom`
- `hook.invoke`
- `kanban.call`
- `hermes.process`
- `infra.destroy`
- `unknown`

`unknown` is fail-closed vocabulary. Producers SHOULD prefer a specific
typed action whenever they can normalize one safely.

## Governance Severity Ladder

The governance severity ladder is part of the open protocol because
readers need to interpret decision streams consistently across drivers.

Levels:

- `normal`
- `elevated`
- `high`
- `lockdown`

Thresholds by cumulative weighted denials for a single agent:

- `normal`: fewer than 3
- `elevated`: 3 through 6
- `high`: 7 through 9
- `lockdown`: 10 or more

Rules:

- `lockdown` is sticky until reset by an operator or equivalent control
  plane action.
- Weighted denials MAY increment the cumulative total by more than 1.
- Windowed analytics MAY prune timestamped denial events for recent
  behavior detection, but the severity ladder is lifetime-spanning.

## JSONL Example

```json
{"agent_fingerprint":"0000000000000000000000000000000000000000000000000000000000000001","agent_instance_id":"550e8400-e29b-41d4-a716-446655440002","chain_id":"550e8400-e29b-41d4-a716-446655440001","chain_type":"session","driver_identity":{"machine_fingerprint":"0000000000000000000000000000000000000000000000000000000000000000","machine_id":"test-box","user":"jared"},"event_type":"session_start","labels":{"env":"test"},"parent_agent_id":null,"parent_chain_id":null,"payload":{},"prev_hash":null,"run_id":"550e8400-e29b-41d4-a716-446655440000","schema_version":"2","seq":0,"session_id":"550e8400-e29b-41d4-a716-446655440001","surface":"claude-code","this_hash":"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789","ts":"2026-05-02T22:00:00.000Z"}
```

## Conformance Requirements

A producer conforms to protocol `v0.1` if it:

1. emits UTF-8 JSONL, one event per line
2. emits the full envelope with `schema_version:"2"`
3. computes `this_hash` from canonical JSON with `this_hash` omitted
4. preserves contiguous per-chain sequence and hash linkage
5. uses only the frozen `v0.1` event-type set

A consumer conforms to protocol `v0.1` if it:

1. validates the envelope shape
2. validates `this_hash`
3. validates per-chain linkage
4. tolerates unknown `labels`
5. tolerates optional payload fields it does not use

## Compatibility Notes

- New optional fields may be added to payloads or labels in a backward-
  compatible way.
- `run_id`, `session_id`, `agent_instance_id`, and `parent_agent_id`
  MAY be UUIDs, but protocol `v0.1` requires only stable non-empty
  strings. Drivers and tests currently use substrate-native identifiers
  such as `sess-sidecar` and `agent-1`.
- Router signal rows in `gov-decisions-*.jsonl` are outside this
  compatibility contract unless a future protocol version promotes them
  into canonical chain events.
- A new top-level envelope field, a changed hash rule, a changed action
  enum member meaning, or a changed severity threshold requires a new
  protocol version.
- Producers and consumers SHOULD treat this document as the authority
  for `v0.1` if code and docs temporarily drift.
