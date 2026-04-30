# Canonical Event Model

The contract every adapter conforms to, regardless of surface. Owned by `libs/contracts/`. Schema source of truth is the zod schema in TypeScript; Go types are generated via the Nx target `contracts:generate-go-types`.

## Envelope (v2)

Every event is `{envelope, payload}`. The envelope is surface-neutral; the payload is event-typed.

```json
{
  "envelope": {
    "schema_version": "2",
    "event_id": "uuid",
    "event_type": "session_start | user_prompt | pre_tool_use | decision | post_tool_use | session_end | …",
    "ts": "2026-04-30T01:39:37.647Z",
    "chain_id": "uuid",
    "chain_type": "session | run | subagent",
    "parent_chain_id": "uuid | null",
    "seq": 12,
    "prev_hash": "sha256:…",
    "this_hash": "sha256:…",
    "surface": "claude-code | copilot-cli | openclaw | ollama-local | …",
    "driver": "string",
    "agent_id": "string",
    "soul_id": "davinci | knuth | curie | … | null",
    "soul_hash": "sha256:… | null"
  },
  "payload": { /* event-typed; varies by event_type */ }
}
```

## Chain shape

Events form a hash-linked chain per `chain_id`. Subagents and tool-call subchains link upward via `parent_chain_id`.

```
session_start ──► user_prompt ──► pre_tool_use ──► decision ──► post_tool_use ──► … ──► session_end
   chain A          chain A         chain A         chain A         chain A             chain A
                                       │
                                       ├── (subagent invoked)
                                       ▼
                              session_start (chain B, parent_chain_id = A)
                                  ──► … ──► session_end (chain B)
```

- **`prev_hash`** points at the SHA-256 of the previous event in the same chain. The first event in a chain has `prev_hash = null`.
- **`this_hash`** is the canonical-JSON SHA-256 of the entire envelope+payload (computed by the Go kernel; TS computes the same hash via shared canonicalization).
- **`seq`** is the monotonic event index within a chain, starting at 0.

The chain is **canonical** — it is the source of truth for what happened. OTEL emit is a projection of this chain (see below).

## Event types (open vocabulary)

Adapters emit whichever subset their surface can observe. Today's payloads:

| `event_type` | Emitter | Payload (shape) |
|---|---|---|
| `session_start` | every adapter | `agent_id, soul_id, soul_hash, machine_fingerprint, env` |
| `user_prompt` | claude-code, copilot-cli | `prompt_text, prompt_id` |
| `pre_tool_use` | claude-code, copilot-cli, openclaw | `tool_name, raw_input, canonical_form, action_type` |
| `decision` | gov.Gate | `decision (allow/deny/guide), reason, suggestion, corrected_command` |
| `post_tool_use` | every adapter | `tool_name, result (success/error/denied), duration_ms, output_summary` |
| `pre_compact` | claude-code | `tokens_in, tokens_out` |
| `subagent_stop` | claude-code | `subagent_chain_id, exit_status` |
| `session_end` | every adapter | `summary, total_events` |

Reserved (not yet wired): `policy_decided`, `rewritten`, `denied`, `chain_verify`.

## Field ownership

- **`raw_input`** — verbatim from the agent's tool call.
- **`canonical_form`** — produced by the kernel's `canon` package (shell command → canonical `Command`/`Pipeline`).
- **`action_type`** — produced by the kernel's `normalize` package; **closed enum** of 6 classes: `read | write | exec | git | net | dangerous`. Unknown actions are denied (this is the dogfood signal that produced "extend the normalizer" feedback).
- **`decision.decision`** — produced by `gov.Gate.Evaluate` against `chitin.yaml`.
- **`schema_version`** — owned by `libs/contracts/`; the kernel rejects events with a missing or stale version.

## OTEL projection (one-way bridge)

When OTEL emit is enabled (F4, ships before 2026-05-07), the kernel projects events onto OTEL spans **after** the chain write succeeds. The chain is authoritative; OTEL is non-authoritative.

```
event chain (canonical)         OTEL span (projection)
──────────────────────         ──────────────────────
chain_id              ──────►  trace_id
event_id              ──────►  span_id
parent_chain_id       ──────►  parent_span_id
ts (start) + duration ──────►  start_time / end_time
event_type            ──────►  span.name
agent_id              ──────►  attribute: agent.id
tool_name             ──────►  attribute: tool.name
decision.decision     ──────►  attribute: decision.type
```

**Invariants:**

- One-way only. Never `OTEL → event chain` as primary path.
- Derivability. OTEL must be derivable from the chain; not vice versa.
- No policy on OTEL. All policy decisions derive from the chain or kernel-observed data.
- Kernel survives OTEL failure. If OTEL emit fails, kernel write must still succeed.

F4 ships 4 event types only (`session_start`, `pre_tool_use`, `decision`, `post_tool_use`) over OTLP HTTP JSON. Full `gen_ai.*` semconv compliance, OTLP-grpc, batching, and multi-exporter support are post-talk.

## Surface neutrality

`surface` + `driver` are the only fields that vary between Claude Code, Copilot CLI, openclaw, etc. The same downstream tooling (replay, ledger, gate, envelope) works against any surface.

## Local-only by default

Phase 1 never sends events over the network. OTEL emit is opt-in (configured per-deployment) and the kernel-write-survives-OTEL-failure invariant means the on-disk chain is always complete even when no OTEL collector is reachable.
