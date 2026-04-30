# F4 — OTEL Emit MVP Design

**Date:** 2026-04-29
**Status:** Spec — pre-implementation
**Forcing function:** 2026-05-07 talk demo beat
**Supersedes (in direction):** [`2026-04-20-otel-genai-ingest-workstream-design.md`](./2026-04-20-otel-genai-ingest-workstream-design.md) — chitin emits, no longer ingests (C2 framing v1, locked 2026-04-29)

## Preamble

F4 is the thin OTEL emit MVP that ships before the 2026-05-07 talk. It is a **parallel slice** to cost-gov v3 — F4 must not bloat that work. The framing v1 sequence (`/grill-me` 9 questions, GLT consensus, locked 2026-04-29) names F4 as the talk's OTEL trace beat: the demo where chitin's event chain shows up as spans in a standard OTEL collector, proving the closed-loop differentiator works against off-the-shelf observability infra.

This spec is small on purpose. Full `gen_ai.*` semconv compliance, OTLP-grpc, batching, retries, multi-exporter, and end-to-end auth are explicitly **deferred post-talk**.

## What F4 ships

A `go/execution-kernel/internal/emit/otel.go` projector that fires after the canonical event chain commit succeeds, projects 4 event types onto OTEL spans, and POSTs them via OTLP/HTTP JSON to a configured endpoint. Async, fire-and-forget, kernel-write-survives-OTEL-failure.

## Locked decisions (from framing v1)

| Field | Value |
|-------|-------|
| Event types in scope | `session_start`, `pre_tool_use`, `decision`, `post_tool_use` |
| Transport | OTLP/HTTP JSON only (no gRPC) |
| Batching | None — one HTTP POST per event |
| Retries | None beyond basic timeout |
| Module location | `go/execution-kernel/internal/emit/otel.go` |
| Failure invariant | OTEL emit failure must not abort kernel JSONL/index commit |
| Derivability invariant | OTEL spans must be derivable from the chain; never the reverse |
| Policy invariant | No policy decision may depend on OTEL data |
| Semconv coverage | Generic `code.*` + `service.*` only; **not** `gen_ai.*` (post-talk) |

## Span mapping

| Chain field | OTEL field | Encoding |
|-------------|------------|----------|
| `chain_id` | `traceId` | UUID hex without hyphens (32 chars) |
| `this_hash` | `spanId` | First 16 hex chars of SHA-256 |
| (see below) | `parentSpanId` | Conditional, see "parent rules" |
| `event_type` | `name` | Verbatim |
| `ts` (start) + duration | `startTimeUnixNano`, `endTimeUnixNano` | For `pre_tool_use`/`decision`/`session_start`, end = start (point-in-time). For `post_tool_use`, end = start, but `attributes.duration_ms` carries the original interval. |
| `agent_instance_id` | `attributes["agent.id"]` | string |
| `payload.tool_name` | `attributes["tool.name"]` | string, when present |
| `payload.decision` | `attributes["decision.type"]` | string, when present |
| `payload.input_bytes` | `attributes["input_bytes"]` | int, when present (optional) |

### Parent rules

The framing memory's `parent_chain_id → parent_span_id` mapping captures only cross-chain linkage. Within-chain linkage needs its own rule. The full rule:

```
For each event:
  if prev_hash != null:
    parentSpanId = prev_hash[:16]      # within-chain parent
  elif parent_chain_id != null:
    parentSpanId = last_hash_of(parent_chain_id)[:16]   # cross-chain parent
  else:
    parentSpanId = null                # root event of root chain
```

This preserves the framing-v1 commitment (cross-chain linkage uses `parent_chain_id`) while making within-chain parenting explicit. The first event of a subchain bridges to the parent chain's last event; subsequent events form a normal in-trace chain.

**Rationale for this expansion (lock-by-spec, not lock-by-memory):** The locked memory only named the cross-chain mapping. Within-chain mapping was implicit. This spec resolves that gap. If the user disagrees with the within-chain rule, the change is one branch in `otel.go`.

## Configuration

Standard OTEL env var. If unset, OTEL emit is **disabled** (no goroutine spawned, no HTTP).

```
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://localhost:4318/v1/traces
```

Falls back to `OTEL_EXPORTER_OTLP_ENDPOINT` + `/v1/traces` if traces-specific is unset. No other config needed for v1.

## OTLP/HTTP JSON body shape

```json
{
  "resourceSpans": [{
    "resource": {
      "attributes": [
        {"key": "service.name", "value": {"stringValue": "chitin-kernel"}},
        {"key": "service.version", "value": {"stringValue": "<kernel build id>"}}
      ]
    },
    "scopeSpans": [{
      "scope": {"name": "chitin"},
      "spans": [{
        "traceId": "<32 hex>",
        "spanId": "<16 hex>",
        "parentSpanId": "<16 hex>",
        "name": "pre_tool_use",
        "kind": 1,
        "startTimeUnixNano": "1714521577000000000",
        "endTimeUnixNano":   "1714521577000000000",
        "attributes": [
          {"key": "agent.id", "value": {"stringValue": "claude-code-..."}},
          {"key": "tool.name", "value": {"stringValue": "Bash"}}
        ]
      }]
    }]
  }]
}
```

Field names use lowerCamelCase per the OTLP/HTTP+JSON proto3 mapping (`google.protobuf.Timestamp` JSON encoding uses `unixNano` strings to preserve int64 precision).

## Failure semantics

```
emit.Emit(event) {
  // existing chain write — unchanged, must complete first
  if !chainCommit succeeds: return error  // caller sees failure
  // F4 addition:
  if otelEnabled():
    go projectAndPost(event)              // fire-and-forget
  return nil
}

projectAndPost(event) {
  span = projectToSpan(event)
  body = encodeOtlpJson(span)
  ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
  defer cancel()
  resp, err := httpPost(endpoint, body, ctx)
  if err != nil: log.Warn("otel emit failed", err)   // never propagate
  if resp.StatusCode >= 400: log.Warn(...)
}
```

Goroutine is detached. On process shutdown, in-flight POSTs may be dropped — that's acceptable for v1; the chain on disk is canonical. No queue, no flush, no retries.

## Tests

Three test fixtures sit in `go/execution-kernel/internal/emit/otel_test.go`:

1. **`TestProjectToSpan_Mapping`** — for each of 4 event types, given a fixture event, assert the resulting OTEL span has the right `traceId`/`spanId`/`parentSpanId`/`name`/`attributes`. Table-driven.
2. **`TestParentSpanIdRules`** — three branches (within-chain prev_hash, cross-chain parent_chain_id, root event nil). Assert correct parent encoding.
3. **`TestKernelSurvivesOTELFailure`** — point the endpoint at `localhost:1` (refused), invoke `Emit`, assert (a) chain commit succeeded, (b) JSONL line written, (c) function returned `nil`. Verifies the failure invariant.

A 4th integration test (manual): run a local OTLP collector (e.g. `otelcol-contrib --config=collector.yaml`) on `:4318`, set `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, run any chitin-instrumented Claude Code session, and verify spans land in the collector. This is the talk demo path; it lives as a runbook, not a CI test.

## Out of scope (post-talk)

| Deferred | Why |
|----------|-----|
| Full `gen_ai.*` semconv | Bigger surface; needs producer-side mapping work |
| OTLP/gRPC | Bytes-on-wire optimization not relevant for talk |
| Batching | Single-event POSTs are fine for demo throughput |
| Retries beyond basic timeout | Adds queue + persistence concerns |
| Multi-exporter | One endpoint is enough for demo |
| Auth (mTLS, bearer) | Endpoint is local for the demo |
| Sampling | All-on for demo; sampling is a post-talk knob |
| `code.*` resource attrs beyond service.name/version | Marginal demo value |
| Span events / links | Plain spans suffice for the trace beat |
| Error span status codes | All spans report `STATUS_CODE_UNSET` (default) |

Each of these has a likely follow-up sub-spec post-talk. Do not pull them forward.

## Risks

1. **OTEL Collector compatibility.** The OTLP/HTTP JSON body format follows proto3 JSON encoding. Real-world collectors (otelcol-contrib, Jaeger, Honeycomb) all parse this — but the manual integration test (#4 above) is the only thing that catches a real wire-incompatibility before the talk.
2. **Within-chain parent rule.** If the user disagrees with the prev_hash → parentSpanId rule, this spec is the place to say so before code lands.
3. **Trace_id encoding.** UUID-without-hyphens fits 32 hex chars exactly, but assumes UUID v4. Other chain_id formats (post-v1.5) would need re-encoding. Documented as an assumption.
4. **Timestamp precision.** `unixNano` requires int64 nanoseconds. Go's `time.UnixNano()` returns int64 — fine. JSON encoding must wrap in string to avoid precision loss. Standard OTLP/HTTP+JSON convention.

## Open questions for the user

- **Within-chain parent rule** — accept the spec's prev_hash → parentSpanId expansion, or restrict to cross-chain only?
- **Endpoint resolution** — env-var-only is OTEL-idiomatic; do we want a fallback chitin.yaml `otel.endpoint:` key for in-repo demo runbook simplicity?
- **Service version attribute** — read from build-time ldflag, or hard-code "0.0.0" until release tagging exists?

Default answers if no objection: accept within-chain rule, env-var-only, hard-code "0.0.0".
