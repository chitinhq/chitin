# SP-1 — openclaw-dialect OTEL Translator (First Live Ingest)

**Date:** 2026-04-20
**Parent:** `docs/superpowers/specs/2026-04-20-otel-genai-ingest-workstream-design.md` (workstream meta-spec, post-SP-0 amendment)
**Informed by:** `docs/observations/2026-04-20-openclaw-otel-capture.md` (SP-0 empirical findings — 0 `gen_ai.*` vs 75 `openclaw.*`, Framing B selected)
**Status:** Design — ready for implementation-plan cycle.

## Preamble

SP-0 established that openclaw's `diagnostics-otel` plugin emits valid
OTLP over the standard OpenTelemetry SDK but uses the `openclaw.*`
vendor attribute namespace instead of the OpenTelemetry GenAI
semantic conventions. The meta-spec's Framing B (widen the translator
to accept openclaw's vendor schema as the v1 dialect) was selected.
SP-1 implements that dialect.

The brainstorming decisions (2026-04-20) that shape this design:

1. **Fixture posture:** synthesize an SP-1 fixture from the SP-0
   source inventory; capture a real successful turn as SP-1's
   dogfood gate (hybrid). No synthetic-against-semconv payloads;
   the synthesized fixture is a byte-level reproduction of what the
   characterized emit-path writes.
2. **Event type:** introduce a new chitin event type `model_turn`.
   openclaw's `openclaw.model.usage` span has no text body; existing
   event types (`assistant_turn`, `session_start`) cannot honestly
   hold its payload.
3. **Decoder:** depend on `go.opentelemetry.io/proto/otlp` — the
   canonical OTEL Go proto bindings. No hand-rolled decoder.
4. **CLI / package layout:** flat — files in `internal/ingest/`,
   sibling command `ingest-otel` in `cmd/chitin-kernel/main.go`.
   Matches the existing `ingest-transcript` pattern exactly.
5. **Translator strictness:** Approach 1 (strict / full-fidelity) —
   all SP-0-guaranteed attrs are required; any missing required attr
   on an `openclaw.model.usage` span quarantines that span.

## One-sentence invariant

One openclaw OTLP/protobuf payload produced with a successful
`openclaw.model.usage` span, when passed to `chitin-kernel
ingest-otel --from <file> --dialect openclaw --envelope-template
<template>`, produces exactly one `model_turn` event in
`.chitin/events-<run_id>.jsonl` with (a) valid `prev_hash`/`this_hash`
chain linkage, (b) `labels.source = "otel"` and
`labels.dialect = "openclaw"`, (c) `chain_id = "otel:" + trace_id`,
and (d) every required attribute faithfully populated — with every
other span in the same payload either also mapped or quarantined to
`.chitin/otel-quarantine/<span_name>-<trace_id>-<span_id>.json` with
a typed reason, and never silently dropped.

## Architecture

The meta-spec's four-station pipeline, instantiated for openclaw.

```
[openclaw.pb file]
      │
      ▼
[intake: file read + OTLP/protobuf decode]
   package: internal/ingest/otel.go
   dependency: go.opentelemetry.io/proto/otlp/trace/v1
   output: []*tracepb.ResourceSpans
      │
      ▼
[translator: openclaw dialect]
   package: internal/ingest/openclaw.go
   input: []*tracepb.ResourceSpans + envelope template
   behavior: walk every span, if span.Name == "openclaw.model.usage"
             extract required attrs, build model_turn payload; else
             quarantine
   output: []event.Event (EventType="model_turn"), []Quarantine
      │
      ▼
[chain writer: existing emit.Emitter]
   package: internal/emit + internal/chain  (unchanged)
   behavior: fills seq, prev_hash, this_hash; appends to events JSONL
      │
      ▼
[store: .chitin/events-<run_id>.jsonl]
```

### Station boundaries (invariants)

- The intake station knows OTLP; it does not know "openclaw." It is
  reusable by any future OTEL dialect.
- The translator station knows `openclaw.model.usage` and
  `openclaw.*` attributes; it does not know OTLP wire format and
  does not know chain state.
- The chain writer (existing `emit.Emitter`) is unmodified. It sees
  envelope events and does not care about their source.
- The store (`.chitin/events-<run_id>.jsonl`) is unmodified.

### What is new vs existing

New Go files:
- `go/execution-kernel/internal/ingest/otel.go` (+ `_test.go`)
- `go/execution-kernel/internal/ingest/openclaw.go` (+ `_test.go`)
- `go/execution-kernel/internal/ingest/openclaw_integration_test.go`
- `go/execution-kernel/testdata/gen_synthesized_fixture.go` (one-shot
  helper that writes the canonical synthesized fixture; committed for
  reproducibility)

New CLI command:
- `cmdIngestOTEL` in `go/execution-kernel/cmd/chitin-kernel/main.go`
  as a sibling to `cmdIngestTranscript`.

New module dependency:
- `go.opentelemetry.io/proto/otlp`
- Pulls in `google.golang.org/protobuf` as transitive dep.

New contract:
- Add `ModelTurnPayloadSchema` to `libs/contracts/src/payloads.schema.ts`.
- Add the `model_turn` variant to `EventSchema` in
  `libs/contracts/src/event.schema.ts`.
- Export the payload type from `libs/contracts/src/event.types.ts`
  and `libs/contracts/src/index.ts`.

New fixtures:
- `docs/observations/fixtures/2026-04-20-openclaw-otel-capture/sp1/synthesized-model-usage.pb`
  (committed at implementation time — generated by the helper above)
- `docs/observations/fixtures/2026-04-20-openclaw-otel-capture/sp1/real-model-usage.pb`
  (committed at dogfood-gate time, IF the real capture succeeds during
  SP-1 execution)

Not modified:
- `emit.Emitter`, `chain.Index`, `event.Event` (Go struct), the envelope
  schema, or any existing ingest path. SP-1 is additive.

## Components

### C1 — OTLP/protobuf decoder (`internal/ingest/otel.go`)

```go
// Package ingest — otel.go decodes OTLP/protobuf payloads into span slices
// for downstream dialect translators. Dialect-agnostic.
package ingest

// DecodeTraces reads one binary OTLP/protobuf ExportTraceServiceRequest
// body and returns its ResourceSpans slice. Malformed input is fatal.
func DecodeTraces(data []byte) ([]*tracepb.ResourceSpans, error)

// IterSpans walks every span across all ResourceSpans / ScopeSpans
// yielding each with its Resource context. Hides protobuf nesting from
// dialect callers.
func IterSpans(rs []*tracepb.ResourceSpans, fn func(r *resourcepb.Resource, s *tracepb.Span))
```

Reusable: any future OTEL dialect consumes the same decoder output.

### C2 — openclaw-dialect translator (`internal/ingest/openclaw.go`)

```go
// ModelTurn is the parse-stage output for one openclaw.model.usage span.
type ModelTurn struct {
    TraceID           string // 32 hex chars — used for chain_id
    SpanID            string // 16 hex chars — used for tie-breaker ordering
    Ts                string // RFC3339, from span start_time_unix_nano
    Surface           string // from resource service.name
    Provider          string // from openclaw.provider (required, ≠ "unknown")
    ModelName         string // from openclaw.model (required, ≠ "unknown")
    InputTokens       int64  // from openclaw.tokens.input (required, ≥ 0)
    OutputTokens      int64  // from openclaw.tokens.output (required, ≥ 0)
    SessionIDExternal string // optional, from openclaw.sessionId
    DurationMs        int64  // optional, derived from span end−start
    CacheReadTokens   int64  // optional, from openclaw.tokens.cache_read
    CacheWriteTokens  int64  // optional, from openclaw.tokens.cache_write
}

// Quarantine records an unmappable span for audit.
type Quarantine struct {
    Reason   string // e.g. "missing_required_attr:openclaw.model"
    SpanName string
    TraceID  string
    SpanID   string
    SpanRaw  json.RawMessage // full span, JSON-serialized
}

// ParseOpenClawSpans classifies every span into either turns or
// quarantined. Never errors mid-walk; a returned error is for
// structural decode failures, not per-span faults.
func ParseOpenClawSpans(rs []*tracepb.ResourceSpans) (turns []ModelTurn, quarantined []Quarantine, err error)

// EmitModelTurns mirrors EmitTurns: validate template, clone per turn,
// fill event_type = "model_turn", marshal payload, delegate to em.Emit.
// Writes quarantine side-cars before emitting (crash-safety: quarantine
// complete, events partial is preferable to the inverse).
func EmitModelTurns(em *emit.Emitter, dir string, tmpl *event.Event, turns []ModelTurn, quarantined []Quarantine) (int, error)
```

Follows the pattern of `ingest/transcript.go` + `ingest/emit.go` exactly.

### C3 — CLI entry (`cmd/chitin-kernel/main.go`)

New sibling command `ingest-otel`.

```
chitin-kernel ingest-otel \
  --from <path/to/*.pb> \
  --dialect openclaw \
  --envelope-template <path/to/template.json> \
  [--dir .chitin]
```

Two modes mirroring `ingest-transcript`:

- **Parse-only** (no `--envelope-template`): decode, translate,
  print `{"ok":true,"turns":[...],"quarantined":[...]}`. No side
  effects on the chain; no quarantine files written.
- **Emit mode** (with `--envelope-template`): decode → translate →
  write quarantine side-cars first → emit events via `emit.Emitter`.
  Exit code:
  - `0` — all mappable spans emitted, zero quarantined.
  - `2` — some spans quarantined; non-quarantined spans were emitted.
  - non-zero-other — fatal station failure (I/O, template-invalid,
    chain-index mismatch) — no events emitted (or partial, with the
    chain remaining consistent because `emit.Emitter` is
    append-only-transactional).

Unsupported dialect (`--dialect gen_ai` etc.) → loud exit with
typed error. Only `--dialect openclaw` is valid in v1.

### C4 — new `model_turn` event type

`libs/contracts/src/payloads.schema.ts` — add:

```ts
export const ModelTurnPayloadSchema = z.object({
  model_name: z.string().min(1),
  provider: z.string().min(1),
  input_tokens: z.number().int().nonnegative(),
  output_tokens: z.number().int().nonnegative(),
  session_id_external: z.string().optional(),
  duration_ms: z.number().int().nonnegative().optional(),
  cache_read_tokens: z.number().int().nonnegative().optional(),
  cache_write_tokens: z.number().int().nonnegative().optional(),
});
export type ModelTurnPayload = z.infer<typeof ModelTurnPayloadSchema>;
```

`libs/contracts/src/event.schema.ts` — add to the discriminated union:

```ts
z.object({ ...envShape, event_type: z.literal('model_turn'), payload: ModelTurnPayloadSchema }),
```

Mirror in Go as a payload struct colocated in
`internal/ingest/openclaw.go`. `event.Event` in
`internal/event/event.go` is unchanged — it stays the envelope-only
canonical form; per-payload structs live at their consumer sites
(same pattern as `assistantTurnPayload` in `ingest/emit.go`).

`session_id_external` naming intentionally distinct from the envelope
`session_id` (UUIDv4, chitin-owned) — openclaw's vendor sessionId
must not collide with chitin's identity.

### C5 — quarantine writer (in `openclaw.go`)

```go
// WriteQuarantine persists one unmappable span to
// <dir>/otel-quarantine/<span_name>-<trace_id>-<span_id>.json.
// Idempotent: overwriting the same path on replay produces identical
// content.
func WriteQuarantine(dir string, q Quarantine) error
```

Directory created under `<.chitin>/otel-quarantine/` via `os.MkdirAll`
(mirrors `kstate.Init` idiom).

## Data flow

Specific field-level mapping rules. Every `openclaw.model.usage` span
follows these rules deterministically.

### Step 1 — span selection

Walk every span. A span is *selected* if and only if
`span.Name == "openclaw.model.usage"`. Any other span name is
quarantined with
`reason = "unmapped_span_name:<actual_name>"`. SP-2 extends the select
set.

### Step 2 — required-attr extraction (fatal-to-span if missing)

| Source                                            | Requirement                         | `ModelTurn` field | Target field                           |
| ------------------------------------------------- | ----------------------------------- | ----------------- | -------------------------------------- |
| `span.trace_id`                                   | 16 non-zero bytes                   | `TraceID`         | `chain_id = "otel:" + trace_id` (envelope) |
| `resource.attributes["service.name"]`             | non-empty string                    | `Surface`         | envelope `surface`                     |
| `span.start_time_unix_nano`                       | non-zero                            | `Ts`              | envelope `ts` (RFC3339)                |
| `span.attributes["openclaw.provider"]`            | non-empty string, not `"unknown"`   | `Provider`        | payload `provider`                     |
| `span.attributes["openclaw.model"]`               | non-empty string, not `"unknown"`   | `ModelName`       | payload `model_name`                   |
| `span.attributes["openclaw.tokens.input"]`        | int ≥ 0                             | `InputTokens`     | payload `input_tokens`                 |
| `span.attributes["openclaw.tokens.output"]`       | int ≥ 0                             | `OutputTokens`    | payload `output_tokens`                |

Missing any of the above → quarantine with
`reason = "missing_required_attr:<key>"`. The explicit
`"unknown"` rejection on provider / model comes from the SP-0 source
inventory (both fields default to `"unknown"` via `?? "unknown"`
when the diagnostic event lacks them — line 53646–53648 in the
plugin source — and a `"unknown"` value is semantic noise, not a
real attribution).

### Step 3 — optional-attr extraction (no-error if missing)

| Source                                                    | `ModelTurn` field   | Output behavior                                                  |
| --------------------------------------------------------- | ------------------- | ---------------------------------------------------------------- |
| `span.attributes["openclaw.sessionId"]`                   | `SessionIDExternal` | set payload `session_id_external` if non-empty                   |
| `span.end_time_unix_nano` − `span.start_time_unix_nano`   | `DurationMs`        | set payload `duration_ms` if end time is non-zero                |
| `span.attributes["openclaw.tokens.cache_read"]`           | `CacheReadTokens`   | set payload `cache_read_tokens` if non-zero                      |
| `span.attributes["openclaw.tokens.cache_write"]`          | `CacheWriteTokens`  | set payload `cache_write_tokens` if non-zero                     |

Optional attrs absent from the output payload means the corresponding
Zod field is omitted (not set to 0 or null).

### Step 4 — envelope assembly

Envelope template is provided externally (same pattern as
`ingest-transcript`). Required template fields:

- `schema_version = "2"`
- `run_id` (UUIDv4, non-empty)
- `session_id` (UUIDv4, non-empty) — envelope-level, not
  `session_id_external`
- `chain_type = "session"`
- `driver_identity` (user / machine_id / machine_fingerprint)
- `agent_instance_id` / `agent_fingerprint`

Fields the translator overrides per-event (from the span / resource):

- `surface` ← resource `service.name` (the translator prefers real
  data over template — warn on mismatch but take the resource value;
  rationale: the template is a hint, the producer is truth)
- `chain_id` ← `"otel:" + span.trace_id`
- `ts` ← `start_time_unix_nano` converted to RFC3339
- `event_type` ← `"model_turn"`
- `labels.source` ← `"otel"`
- `labels.dialect` ← `"openclaw"`

Fields passed through from template unchanged:
`run_id`, `session_id`, `driver_identity`, `agent_instance_id`,
`parent_agent_id`, `agent_fingerprint`.

Fields filled by `emit.Emitter` (unchanged from existing path):
`seq`, `prev_hash`, `this_hash`.

### Step 5 — chain and persist

`em.Emit(&ev)` writes to `.chitin/events-<run_id>.jsonl` with full
chain linkage. Re-ingest is idempotent by the chain writer's existing
dedup (chain_id + seq collision). No SP-1-owned checkpoint file.

### Tie-breakers

- **Multiple `openclaw.model.usage` spans with same `trace_id`:**
  both become `model_turn` events in the same chain, ordered by
  `start_time_unix_nano` ascending. Ties on equal timestamps break on
  `span_id` lexicographic (hex). `seq` assigned in translator output
  order by the emitter.
- **All-zero `trace_id`:** per OTEL spec, this is invalid. Quarantine
  with `reason = "invalid_trace_id_zero"`. Do not substitute a
  generated ID — that breaks the chain_id determinism invariant.
- **Attribute name collision (`openclaw.tokens.input` present twice in
  same span):** OTEL permits duplicate attribute keys on wire. Use
  last-writes-win (protobuf default). Do not error.

## Error handling

### Classification invariant

For every span in the input file, exactly one of three things
happens. Never zero, never two.

| Input condition                                                    | Action                                                               |
| ------------------------------------------------------------------ | -------------------------------------------------------------------- |
| `span.Name == "openclaw.model.usage"` + all required attrs OK       | Emit one `model_turn` event.                                         |
| `span.Name == "openclaw.model.usage"` + any required attr missing   | Quarantine with `reason = "missing_required_attr:<key>"`.            |
| `span.Name != "openclaw.model.usage"`                                | Quarantine with `reason = "unmapped_span_name:<actual_name>"`.       |

### Per-station behavior

- **Intake (file read + OTLP decode).** I/O failure or malformed
  protobuf → fatal for the ingest command. Exit non-zero with a typed
  JSON error (`{"error":"otlp_decode_failed","byte_offset":N,...}`).
  No events, no quarantine side-effects.
- **Translator.** Per-span classification per the table above. Never
  fatal to the command. Quarantine files are written *before* any
  event is emitted (crash-safety: quarantine-complete-events-partial
  is preferable to the inverse).
- **Envelope-validation.** If `ValidateEnvelopeTemplate` fails, fatal
  before any span is processed. No side-effects.
- **Chain-writer.** Unchanged from today. Hash-mismatch on chain-index
  load is fatal; same handler as the stdin-hook path.

### Exit codes

- `0` — all mappable spans emitted cleanly, zero quarantined.
- `2` — some spans quarantined; non-quarantined spans were emitted
  durably. Downstream tooling (ledger-lint) treats this as a gap
  signal.
- Other non-zero — fatal station failure per above.

### Re-ingest idempotency

- Chain writer dedups by `chain_id + seq` collision (existing
  behavior) — same spans produce the same events, no duplicates.
- Quarantine files overwrite themselves (same path for the same
  `<span_name>-<trace_id>-<span_id>`; content is deterministic for
  the same input span).
- No SP-1 checkpoint file. OTLP files are small snapshots, not
  append-only transcripts — offset-based checkpointing is not
  applicable.

### What happens on a real-time capture with no `openclaw.model.usage` span

This is the SP-0-reproduction case (empty-batch heartbeats, or a
capture where all spans are `openclaw.webhook.*`). All spans
quarantined; zero events emitted; exit code `2` if any span was
present, `0` if the file was literally empty. Correct behavior —
the pipeline prove zero-events-from-zero-real-turns.

## Testing

### Fixture strategy

Fixtures live under
`docs/observations/fixtures/2026-04-20-openclaw-otel-capture/sp1/`.

1. **`synthesized-model-usage.pb`** — committed at implementation
   time. A minimal valid OTLP/protobuf payload containing exactly one
   `openclaw.model.usage` span with every required + optional
   attribute from §Data flow populated with representative values.
   Generated by a one-shot Go helper
   (`go/execution-kernel/testdata/gen_synthesized_fixture.go`) that
   imports `go.opentelemetry.io/proto/otlp` and writes the bytes.
   The helper is committed alongside for reproducibility; it is not
   run by `go test`.
2. **`real-model-usage.pb`** — committed during SP-1's dogfood gate
   *if* a real successful openclaw agent turn captures an
   `openclaw.model.usage` span. If the dogfood gate defers (see
   below), this fixture does not land and the SP-1 plan documents
   the deferral.

The synthesized fixture is a byte-level reproduction of the SP-0
source-characterized emit path, not a guess at semconv. This
satisfies the meta-spec's "no synthetic OTEL" rule applied
correctly: synthesized-against-observed-code is not
synthesized-against-assumed-spec.

### Unit tests — `internal/ingest/openclaw_test.go`

One function per mapping rule. Every test reads
`synthesized-model-usage.pb`, mutates one field in-memory (not on
disk), runs `ParseOpenClawSpans`, and asserts.

- `TestParseOpenClawSpans_HappyPath` — synthesized fixture unchanged
  → one `ModelTurn` with all fields populated, zero quarantined.
- `TestParseOpenClawSpans_MissingModelName`
- `TestParseOpenClawSpans_MissingProvider`
- `TestParseOpenClawSpans_MissingInputTokens`
- `TestParseOpenClawSpans_MissingOutputTokens`
- `TestParseOpenClawSpans_MissingServiceName`
- `TestParseOpenClawSpans_MissingStartTime`
- `TestParseOpenClawSpans_UnknownModelRejected` — model =
  `"unknown"` → quarantined; rationale in §Data flow Step 2.
- `TestParseOpenClawSpans_UnknownProviderRejected`
- `TestParseOpenClawSpans_ZeroTraceID`
- `TestParseOpenClawSpans_UnmappedSpanName` — span renamed to
  `"openclaw.webhook.processed"` → quarantined.
- `TestParseOpenClawSpans_OptionalPresent` / `_OptionalAbsent`
- `TestParseOpenClawSpans_MultipleSpansOrderedByTime`
- `TestParseOpenClawSpans_TieBreakerSpanID` — equal timestamps.
- `TestParseOpenClawSpans_DuplicateAttrKeyLastWins`

### Integration tests — `internal/ingest/openclaw_integration_test.go`

Full pipeline against the fixture, driven through `EmitModelTurns`.

- `TestEmitModelTurns_SynthesizedFixtureEndToEnd` — parse + emit to
  an ephemeral `.chitin/` (`t.TempDir()`) → JSONL contains exactly
  one `model_turn` event with valid `prev_hash`/`this_hash` chain,
  `labels.source=="otel"`, `labels.dialect=="openclaw"`, `chain_id`
  matches `"otel:" + <synth_trace_id>`.
- `TestEmitModelTurns_Idempotent` — call twice with same fixture and
  same emitter → second call is a no-op on the JSONL (chain-writer
  dedup); quarantine files overwritten in-place, content identical.
- `TestEmitModelTurns_BadEnvelopeTemplate_MissingRunID` —
  `ValidateEnvelopeTemplate` fails before any span is processed;
  zero events, zero quarantine files written.
- `TestEmitModelTurns_MixedBatch` — fixture with one happy-path span
  + one unmapped-name span → one event emitted, one quarantine file
  written, returned counts (emitted, quarantined) match (1, 1).

### CLI tests — `cmd/chitin-kernel/main_test.go` (extension)

Drive `chitin-kernel ingest-otel` as a subprocess in `go test`
(mirrors the existing test pattern for other commands).

- `TestCLI_IngestOTEL_EmitMode` — synthesized fixture + valid
  envelope template → exit 0, one event in the JSONL, zero
  quarantine files.
- `TestCLI_IngestOTEL_ParseOnly` — no `--envelope-template` →
  prints JSON with turn + quarantined counts; no events, no
  quarantine files.
- `TestCLI_IngestOTEL_UnsupportedDialect` — `--dialect gen_ai` →
  exit non-zero with typed error.
- `TestCLI_IngestOTEL_QuarantineExit2` — fixture with one
  unmapped-name span → exit code 2; events-JSONL contains the
  mappable event; quarantine file exists.
- `TestCLI_IngestOTEL_MalformedProtobuf` — garbage file → exit
  non-zero with `otlp_decode_failed`.

### Contract tests — `libs/contracts/tests/event.schema.test.ts`
(extension)

- `model_turn — valid payload round-trips` — emit-then-parse a
  representative envelope+payload.
- `model_turn — missing required payload field rejected` — per field.
- `model_turn — optional fields absent allowed`.

### Dogfood gate (SP-1 ship gate, live not CI)

Two-part gate, the second gracefully degraded if the first fails.

1. **Capture.** Fix the openclaw agent profile so
   `openclaw agent --agent main -m "say hi"` runs against a pulled
   ollama model (leverage `qwen2.5:0.5b` which SP-0 left pulled).
   Enable `diagnostics.otel.enabled` and start the receiver on
   `:4318`. Run one turn. Capture the resulting POST body with a
   non-empty `openclaw.model.usage` span. Commit as
   `fixtures/.../sp1/real-model-usage.pb`.

2. **Ingest.** `chitin-kernel ingest-otel --from real-model-usage.pb
   --dialect openclaw --envelope-template <template> --dir <tempdir>`
   → exit 0, the envelope event is valid via `chitin-kernel chain
   info`, and `chitin events list --event-type model_turn` shows it.

If part 1 fails (profile surgery too invasive, provider config broken,
openclaw version quirk), SP-1 ships with only the synthesized
fixture; the plan amendment documents the deferral and names the
follow-up checkpoint. The translator is fully tested at unit +
integration level regardless; the dogfood gate is about end-to-end
confidence, not schema correctness.

### Cross-cutting

- No mocked `emit.Emitter`, no mocked chain index, no mocked
  protobuf parser. All tests run against real code on real disk
  (`t.TempDir()` for ephemeral `.chitin/`).
- Test runtime target: unit tests < 1 s; integration + CLI tests
  < 5 s combined. Fixture is a single span, ~300 bytes of protobuf.
- Every sub-project's CI already runs the full `go test ./...`;
  SP-1 does not introduce a new CI lane.

## Open risks

1. **The real-capture dogfood gate may defer.** If the openclaw agent
   profile surgery proves harder than expected during SP-1
   implementation, the dogfood gate defers to a follow-up checkpoint.
   Mitigation: the translator is fully exercised at unit + integration
   level against the source-derived synthesized fixture; the real
   capture adds end-to-end confidence but does not add schema
   coverage. The SP-1 plan names the deferral conditions.
2. **openclaw version drift.** Openclaw publishes `YYYY.M.patch`,
   monthly-ish. A future version may add attrs to the
   `openclaw.model.usage` span or rename `openclaw.tokens.*`. The
   translator pins to v2026.4.15-beta.1 of `@openclaw/diagnostics-otel`
   via the SP-0 source inventory. SP-2 will re-verify against the
   current version when it runs. SP-1 documents the pinned version in
   the translator's package comment.
3. **`chain_id = "otel:" + trace_id` collision across ingests.** If two
   distinct openclaw installations happen to produce the same
   `trace_id` (astronomically unlikely with a 128-bit randomly-sampled
   ID), they collide into the same chain. This is the same
   "collision would corrupt chain" risk any deterministic chain
   derivation has; mitigation is the 128-bit entropy of `trace_id`.
   Do not design around a surface that does not exist.
4. **`ValidateEnvelopeTemplate` mismatch with Zod envelope.** If the
   Go and TypeScript envelope contracts drift, templates validated by
   Go pass through but fail contract-test consumers. Mitigation: the
   existing contract-test suite catches this already; SP-1's
   `model_turn` additions go in both schemas in the same commit.

## Self-review

### Placeholder scan

- No `TBD` or `TODO` literals.
- `real-model-usage.pb` is explicitly conditional on the dogfood
  gate succeeding during SP-1 execution; that is a deliverable-shape
  flag, not a spec placeholder.

### Internal consistency

- The required-attr list in §Data flow Step 2 exactly matches the
  unit-test coverage in §Testing (one test per required attr, plus
  two for the `"unknown"` rejection rule).
- `chain_id = "otel:" + trace_id` is stated once in §Data flow and
  consistently referenced (never redefined) in §Components and
  §Open risks.
- `labels.source = "otel"` and `labels.dialect = "openclaw"` appear
  consistently in §Architecture, §Components, §Data flow, and
  §Testing without contradiction.
- The fail-loud + quarantine posture is stated once in §Error
  handling and referenced, not re-specified, from §Components.

### Scope check

SP-1 is sized for one brainstorm → spec → plan cycle. It produces
exactly one new event type, one new CLI command, one new OTLP
decoder, one dialect translator — all reusable by or extended by
SP-2 without rework. No station is done twice.

### Ambiguity check

- "representative values" in the synthesized fixture (§Testing
  Fixture strategy): deliberately loose here; the SP-1 plan specifies
  the exact bytes in its step-1 task.
- "fix the openclaw agent profile" in the dogfood gate: deliberately
  loose — the plan's dogfood-gate step will include specific surgery
  commands based on what's needed at execution time.
- "representative values" and "fix the profile" are implementation-
  detail placeholders that belong in the plan, not the spec.

## Execution handoff

**Next action:** invoke the writing-plans skill to produce a detailed
implementation plan for SP-1. The plan decomposes the five components
(C1–C5) into steps, interleaves TDD (tests first for the translator
and decoder), names the Copilot + adversarial review cycle, and
specifies the dogfood-gate deferral condition explicitly.

**Parallelism opportunity:** C1 (decoder) and C4 (new event type in
contracts) have no inter-dependencies; the plan can recommend
executing them in parallel or as two independent commits before the
translator (C2) depends on both.
