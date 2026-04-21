# SP-2 — Complete openclaw Translator (Spans-Only v1)

**Date:** 2026-04-21
**Parent:** `docs/superpowers/specs/2026-04-20-otel-genai-ingest-workstream-design.md` (workstream meta-spec, §SP-2)
**Predecessor:** `docs/superpowers/specs/2026-04-20-sp1-openclaw-dialect-translator-design.md` (SP-1, shipped 2026-04-21 as PR #35, `ce71d10`)
**Informed by:** `docs/observations/2026-04-20-openclaw-otel-capture.md` (SP-0 span/metric inventory) and `docs/observations/2026-04-20-sp1-dogfood-gate.md` (SP-1 deferred dogfood gate)
**Status:** Design — ready for implementation-plan cycle.

## Preamble

SP-1 landed the first dialect translator, mapping `openclaw.model.usage`
to the new `model_turn` event type, with a deliberate one-span-per-trace
assumption captured in an in-source `TODO` (`openclaw.go:283–291`). SP-2
extends that translator to cover every remaining `openclaw.*` span-type
inventoried in SP-0 and revisits the assumption SP-1 marked.

Scope is **spans-only v1**. Metric instruments (18 counters/histograms
per SP-0's inventory) are deferred to a separate mini-spec because the
metrics-disposition decision (events vs session-end rollup vs separate
store) is an architectural coin-flip that does not belong in translator-
extension work. This keeps SP-2's judgement surface to one thing:
turning spans into events honestly.

The brainstorming decisions (2026-04-21) that shape this design:

1. **v1 scope:** spans-only — three new span types mapped
   (`openclaw.webhook.processed`, `openclaw.webhook.error`,
   `openclaw.session.stuck`), every metric instrument deferred.
2. **Event types:** three new chitin event types — `webhook_received`,
   `webhook_failed`, `session_stuck`. Existing types (`user_prompt`,
   `assistant_turn`, `compaction`, `session_end`, `intended`,
   `executed`, `failed`, `model_turn`) cannot hold openclaw's
   operational-surface events honestly. Webhooks are surface-ingress,
   not agent turns; stuck-detection is system-observation, not agent-
   action. Stretching existing types would encode three small lies.
3. **Chain-id scheme:** uniform across all OTEL-ingested event types —
   `chain_id = "otel:" + hex(trace_id) + ":" + hex(span_id)`.
   `model_turn` migrates from SP-1's `"otel:" + trace_id` scheme. The
   migration is paper-only (no production data — SP-1's dogfood gate
   deferred), and the uniform rule eliminates the multi-span-per-trace
   collision SP-1's TODO flagged.
4. **Fixtures:** synthesized from static plugin-source inspection
   (same pattern SP-0 → SP-1 validated). No real-capture blocker.
   Real captures for webhook/stuck paths belong outside SP-2 — they
   require operational setup (ngrok, webhook sources, long-running
   sessions) that is not translator-correctness work.
5. **File organization:** per-span-type files. `openclaw.go` becomes
   the dispatcher + shared helpers; each span's translation moves to
   its own file (`openclaw_model_turn.go`, `openclaw_webhook.go`,
   `openclaw_session_stuck.go`). Knuth: naming is half the algorithm
   — forcing each file to name its span-type tightens the scope of
   what lives there.

## One-sentence invariant

Every span in an OTLP/protobuf payload whose name is in the openclaw
dialect's v2 mapping table (`openclaw.model.usage`,
`openclaw.webhook.processed`, `openclaw.webhook.error`,
`openclaw.session.stuck`) produces exactly one chitin event with
`chain_id = "otel:" + hex(trace_id) + ":" + hex(span_id)` and
`schema_version = "v2.1"`; every other span, and every span that fails
a required-attr check, produces exactly one quarantine side-car with a
typed reason, written before any event is emitted.

## Architecture

```
OTLP/proto payload
      │
      ▼
DecodeTraces (otel.go, unchanged from SP-1)
      │
      ▼
IterSpans walks every span
      │
      ▼
ParseOpenClawSpans (openclaw.go — dispatcher)
      │
      ├─ span.Name == "openclaw.model.usage"       → translateModelUsage       (openclaw_model_turn.go)
      ├─ span.Name == "openclaw.webhook.processed" → translateWebhookProcessed (openclaw_webhook.go)
      ├─ span.Name == "openclaw.webhook.error"     → translateWebhookError     (openclaw_webhook.go)
      ├─ span.Name == "openclaw.session.stuck"     → translateSessionStuck     (openclaw_session_stuck.go)
      └─ else                                       → quarantine("unmapped_span_name:<name>")
      │
      ▼
EmitEvents (openclaw.go): quarantine side-cars first, then events in deterministic order
```

### Four load-bearing rules

1. **Chain-id uniformity.** `chain_id = "otel:" + hex(trace_id) + ":" + hex(span_id)`
   for every OTEL-ingested event type. Constructed by the single
   helper `buildChainID(traceID, spanID []byte) string`; no string
   assembly lives outside that function. Intra-trace causality is
   deliberately not modeled at the chain-id level; labels carry
   `otel_trace_id` and optional `otel_parent_span_id` for downstream
   consumers that need to reconstruct traces.
2. **Quarantine-before-emit.** Preserved from SP-1. Any partial write
   crashes leave quarantine side-cars as the authoritative record,
   never half-emitted events.
3. **Deterministic order.** All events in a batch sort by
   `(ts ascending, span_id ascending)` before emit. Named tie-breaker
   — no map iteration or insertion order dependency.
4. **Idempotency.** A second ingest of the same payload produces zero
   new events and zero duplicate quarantine files (quarantine
   filenames include `trace_id`+`span_id`; overwrite is byte-identical).

### Schema migration

`model_turn` events written by SP-1 (if any existed) would have
`chain_id = "otel:" + trace_id` and `schema_version = "v2"`. SP-2 bumps
`schema_version` to `"v2.1"` and applies the uniform chain-id rule to
`model_turn`. Because SP-1's dogfood gate deferred on an environmental
blocker (documented at `docs/observations/2026-04-20-sp1-dogfood-gate.md`),
no `v2` `model_turn` events exist in any ledger. The migration is paper
only — no reader code needs dual-version handling.

## Components

### New Go files

| File | Exports | Responsibility |
|---|---|---|
| `openclaw_model_turn.go` | `translateModelUsage(resource, span) (ModelTurn, reason)` + `ModelTurn` struct | Moved from SP-1's `openclaw.go`. Translation logic unchanged; chain-id construction delegates to the shared helper. |
| `openclaw_webhook.go` | `translateWebhookProcessed(...)`, `translateWebhookError(...)` + `WebhookReceived`, `WebhookFailed` structs | Success + error paths live together; they share the `openclaw.webhook.*` attribute vocabulary. |
| `openclaw_session_stuck.go` | `translateSessionStuck(...)` + `SessionStuck` struct | Stuck-threshold observation. |

### Refactored `openclaw.go`

- `ParseOpenClawSpans` becomes a `span.Name` → translator dispatcher;
  no per-span logic inline.
- Shared helpers stay: `getSpanStringAttr`, `getSpanIntAttr`,
  `getResourceStringAttr`, `isAllZero`, `sanitizeFilename`,
  `makeQuarantine`, `WriteQuarantine`.
- **New shared helper:** `buildChainID(traceID, spanID []byte) string`
  — single source of truth for the uniform rule. Every translator uses
  it; grep-verified in the test suite.
- `EmitModelTurns` → renamed `EmitEvents`, takes `[]TranslatedSpan` (a
  polymorphic slice) instead of `[]ModelTurn`.

### Translator polymorphism

```go
type TranslatedSpan interface {
    EventType() string                     // "model_turn" | "webhook_received" | "webhook_failed" | "session_stuck"
    ChainID() string                       // built via buildChainID — uniform across types
    Ts() string                            // RFC3339 from span.StartTimeUnixNano
    Surface() string                       // from resource service.name
    Payload() (json.RawMessage, error)
    Labels() map[string]string             // source=otel, dialect=openclaw, otel_trace_id, otel_span_id, [otel_parent_span_id]
}
```

Each per-span struct implements `TranslatedSpan`. `EmitEvents` loops
over `[]TranslatedSpan` without branching on concrete type.

### TS / contracts changes

- `libs/contracts/src/event.schema.ts` — add three `z.object(...)`
  payload schemas (`WebhookReceivedPayloadSchema`,
  `WebhookFailedPayloadSchema`, `SessionStuckPayloadSchema`) plus
  three entries in the `EventSchema` discriminated union.
- `libs/contracts/src/event.types.ts` — add three TypeScript types.
- `libs/contracts/tests/event.schema.test.ts` — Zod round-trip and
  negative tests for each new payload.
- **Concrete payload fields** come from plugin-source static
  inspection during the plan phase (same pattern SP-0 → SP-1
  established). The spec commits to the shape categories below; the
  plan commits to the final attribute lists.

### Fixtures

- `testdata/openclaw/model_usage/*.pb` (moved from SP-1's location).
- `testdata/openclaw/webhook_processed/*.pb`.
- `testdata/openclaw/webhook_error/*.pb`.
- `testdata/openclaw/session_stuck/*.pb`.
- Each fixture is a synthesized `TracesData` protobuf built by a
  small Go fixture-generator; generator source lives alongside tests
  so synthesis is reviewable. The `.pb` files are build artifacts,
  committed for deterministic test input. Each generator has a
  comment header pointing at the plugin-source line-numbers it
  simulates.

## Data flow

### Envelope construction (identical across all four event types)

| Envelope field | Source | Rule |
|---|---|---|
| `schema_version` | constant | `"v2.1"` |
| `chain_id` | `buildChainID(trace, span)` | `"otel:" + hex(trace_id) + ":" + hex(span_id)` |
| `ts` | `span.StartTimeUnixNano` | RFC3339 UTC |
| `surface` | resource `service.name` | required; missing → quarantine |
| `event_type` | translator | literal per span |
| `labels.source` | constant | `"otel"` |
| `labels.dialect` | constant | `"openclaw"` |
| `labels.otel_trace_id` | `hex(trace_id)` | always set |
| `labels.otel_span_id` | `hex(span_id)` | always set |
| `labels.otel_parent_span_id` | `hex(span.parent_span_id)` if non-zero | **new in SP-2** — preserves intra-trace causality the flat chain-id rule drops |

### Deterministic order

`[]TranslatedSpan` sorts by `(ts ascending, span_id ascending)` before
the emit loop. Named tie-breaker. A sort without a tie-breaker is not
sorted.

### Payload shape commitments

The spec commits to the shape categories below; the plan's first task
is an SP-0-style static inventory of the plugin source for each span
type to produce the concrete required/optional attribute lists.

**`model_turn` (migrated from SP-1, payload unchanged).**
Fields: `model_name`, `provider`, `input_tokens`, `output_tokens`,
`session_id_external?`, `duration_ms?`, `cache_read_tokens?`,
`cache_write_tokens?`. Only the chain-id changes — payload byte-for-
byte identical to SP-1.

**`webhook_received` (from `openclaw.webhook.processed`).**
Required: webhook source identifier (stripe/github/…), endpoint,
`duration_ms` (derived from span end-start). Optional:
`session_id_external?` if the webhook triggered a session,
`request_size_bytes?`.

**`webhook_failed` (from `openclaw.webhook.error`).**
Required: webhook source, endpoint, `error_category` (derived from
span `status.code` + any error-type attribute), `duration_ms`.
Optional: `error_message?`, `session_id_external?`.

**`session_stuck` (from `openclaw.session.stuck`).**
Required: `session_id_external` (the openclaw session that tripped
the threshold), `stuck_age_ms` (the age that tripped the threshold;
SP-0 inventoried `openclaw.session.stuck_age_ms` as a metric
histogram, the equivalent span attribute key is confirmed via
plan-phase source inspection). Optional: `stage?`,
`last_activity_ts?`.

### Quarantine-shape additions

SP-1's quarantine reason set extends with per-span reasons from the
new translators. Concrete reason list derives from plugin-source
inspection in the plan phase.

## Error handling & boundaries

### Failure taxonomy

| Failure | Behavior | Quarantine reason |
|---|---|---|
| OTLP decode fails | Fatal error to caller | — (returned to caller, no partial writes) |
| Span name not in dispatch table | Quarantine | `unmapped_span_name:<name>` |
| Required attr missing | Quarantine | `missing_required_attr:<key>` |
| Required attr has value `"unknown"` (openclaw's in-source fallback) | Quarantine | `unknown_value:<key>` |
| `trace_id` not 16 bytes or all-zero | Quarantine | `invalid_trace_id_length` / `invalid_trace_id_zero` |
| `span_id` not 8 bytes or all-zero | Quarantine | `invalid_span_id_length` / `invalid_span_id_zero` *(new — span_id is now in chain_id)* |
| Negative counter / duration | Quarantine | `invalid_value:<key>` |
| Duplicate `span_id` within one batch | Quarantine (all but first) | `duplicate_span_id` *(new)* |
| `webhook.error` span with underivable `error_category` | Quarantine | `missing_required_attr:error_category` |
| Partial write crash during emit | Side-cars already persisted; next ingest replays via chain-id idempotency | — |

### Boundary cases (Knuth walk — 0, 1, N-equal, malformed)

- **Empty payload** (zero `ResourceSpans`): `(nil, nil, nil)`. No
  events, no quarantines, no error.
- **Single span, well-formed:** one event, zero quarantines.
- **N spans with identical `ts`:** sort tie-breaks by `span_id`
  lexicographic (hex).
- **N spans with identical `(ts, span_id)`:** shouldn't happen per
  OTEL spec. First wins in sort; rest quarantine with
  `duplicate_span_id`.
- **Mixed valid + invalid in one payload:** valid emits; invalid
  quarantines; emit does not fail because siblings quarantined. This
  is the crash-safety invariant a pipeline lives or dies on.
- **Idempotent re-ingest:** same payload twice → same event set, same
  quarantine files (byte-identical overwrite).

### No backward-compat shims for v2

Per the "no defensive code for scenarios that can't happen" rule
(CLAUDE.md), readers do not handle both `v2` and `v2.1` chain-id
shapes. SP-1's dogfood gate deferred; zero `v2` events exist anywhere.
If the dogfood retry produces `v2` events *before* SP-2 lands, that
retry's fixture is already synthesized and migrates with the codebase.

### One explicit non-goal

The dispatcher does not recover from missing required attrs by falling
back to hardcoded values. Missing `service.name` → quarantine; missing
webhook source → quarantine. Recovery heuristics are the exact
"defensive code for scenarios that can't happen" anti-pattern
CLAUDE.md warns against.

## Testing

### Per-translator unit tests (Go)

- `openclaw_model_turn_test.go` — SP-1's existing tests, updated for
  the new chain-id scheme (`want_chain_id` becomes
  `"otel:<trace>:<span>"` throughout).
- `openclaw_webhook_test.go` — table-driven; success path, error path,
  each required-attr failure, each boundary case.
- `openclaw_session_stuck_test.go` — same structure.

### Dispatcher integration tests (`openclaw_integration_test.go`, extended)

1. Mixed-span fixture (all four span types in one batch) → correct
   event-type per span; all chain-ids unique; deterministic order.
2. Empty payload → `(nil, nil, nil)`.
3. Payload of only quarantine-eligible spans → zero events, N
   quarantine files, success.
4. Payload with mix of valid + invalid → valid emits, invalid
   quarantines, single-pass success.
5. Identical `ts` across spans → sort tie-breaks by `span_id`.
6. Duplicate `span_id` in batch → first wins, rest quarantine with
   `duplicate_span_id`.
7. Idempotent replay → event count unchanged, quarantine files
   byte-identical.

### TS Zod schema tests (`libs/contracts/tests/event.schema.test.ts`)

- Round-trip (parse + re-serialize) for each new payload.
- Negative: missing required field rejected; wrong type rejected.

### CLI end-to-end (`cmd/chitin-kernel/main_test.go`)

One added test: feed a mixed-span fixture through `chitin-kernel
ingest-otel --dialect openclaw`, verify the events JSONL matches
golden output, verify the quarantine directory matches golden.

### Verification gates (enforced in the plan's final checkpoint)

- `go test ./...` green.
- `pnpm -F contracts test` green.
- CLI E2E golden matches.
- `grep -n '"otel:"' go/execution-kernel/internal/ingest/*.go`
  returns exactly one match — inside `buildChainID`. (Knuth: one
  source of truth, mechanically verified.)
- Every event emitted by any translator has `schema_version == "v2.1"`
  (per-translator unit-level assertion).

### Out of scope for SP-2 tests

- Performance / throughput benchmarks — no evidence-based reason to
  benchmark; re-address if SP-3's push receiver lands.
- Real-capture fixtures — synthesized-only per the fixture decision.
- Concurrency tests — SP-1's single-process sequential invocation
  assumption carries forward; push-mode concurrency is SP-3's problem.

## Explicitly out of scope (future followups)

- **Metric instrument ingest.** The 18 `openclaw.*` metric instruments
  inventoried in SP-0 are not handled by SP-2. Metrics-disposition
  decision (events / session-end rollup / separate store) gets its own
  mini-spec.
- **Routing `session.stuck` directly to the governance-debt ledger.**
  The brainstorm surfaced this as a cleaner shape — "stuck" is health
  telemetry, not chain-linkable history — but the ledger's ingest API
  for kernel-emitted findings isn't designed yet. Revisit when SP-4 or
  a ledger-ingest mini-spec lands.
- **Governance-debt ledger hook for `unmapped_span_name` quarantines.**
  Proactive drift detection ("openclaw added a new span type chitin
  doesn't know about") would naturally live here, but same blocker as
  above — ledger ingest API for kernel findings. Follow-up.
- **Push receiver (SP-3).** Independent of SP-2; gated on evidence
  that a push endpoint is needed.
- **Cross-surface diffs (SP-4).** Gated on SP-2 shipping plus enough
  real dogfood data across both surfaces.
- **Real-capture dogfood gates for webhook / stuck paths.** Genuine
  operational exercise (webhook sources, long-running sessions);
  candidate Hermes side-quest now that the probe is active.

## Cost estimate

3–4 days, re-estimated after the plan's first task (plugin-source
static inventory for webhook + stuck spans) completes. The dispatch
pattern and envelope construction are proven by SP-1, so most of the
work is per-span translator code + fixture synthesis + Zod schemas;
the model_turn migration is small because SP-1 hasn't accumulated
production data.

## Predecessor work cited

- `docs/superpowers/specs/2026-04-20-otel-genai-ingest-workstream-design.md` — meta-spec, §SP-2.
- `docs/superpowers/specs/2026-04-20-sp1-openclaw-dialect-translator-design.md` — SP-1 predecessor design.
- `docs/observations/2026-04-20-openclaw-otel-capture.md` — SP-0 inventory.
- `docs/observations/2026-04-20-sp1-dogfood-gate.md` — SP-1 dogfood deferral.
- `go/execution-kernel/internal/ingest/openclaw.go` — SP-1 implementation, specifically lines 283–291 for the multi-span-per-trace TODO this spec resolves.
