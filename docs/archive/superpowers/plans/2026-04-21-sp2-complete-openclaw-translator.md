# SP-2 — Complete openclaw Translator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend SP-1's openclaw-dialect OTEL translator to cover every span type the plugin emits, introducing three new chitin event types (`webhook_received`, `webhook_failed`, `session_stuck`) and migrating `model_turn` to a uniform chain-id scheme that handles the multi-span-per-trace case SP-1's TODO flagged.

**Architecture:** Per-span-type dispatcher pattern — `openclaw.go` becomes the router + shared helpers; each span's translation lives in its own file (`openclaw_model_turn.go`, `openclaw_webhook.go`, `openclaw_session_stuck.go`). Uniform chain-id rule enforced by a single `buildChainID` helper. Polymorphic emit via a `TranslatedSpan` interface.

**Tech Stack:** Go 1.25, existing `go.opentelemetry.io/proto/otlp` dependency, Zod, TypeScript, existing `emit.Emitter` + `chain.Index`.

**Spec:** `docs/superpowers/specs/2026-04-21-sp2-complete-openclaw-translator-design.md`
**Parent workstream:** `docs/superpowers/specs/2026-04-20-otel-genai-ingest-workstream-design.md` (§SP-2)
**Predecessor implementation:** `go/execution-kernel/internal/ingest/openclaw.go` (SP-1, PR #35, `ce71d10`)
**Plugin-source inventory (done during plan-writing, attributes inlined in tasks):** lines 53637–53798 of `@openclaw/diagnostics-otel@2026.4.15-beta.1`.

**Dependency graph between tasks:**

```
Task 1 (buildChainID) ─┬─▶ Task 2 (migrate model_turn) ─▶ Task 3 (extract) ─▶ Task 4 (TranslatedSpan) ─┐
                       │                                                                                 │
Task 5 (Zod contracts) ┤                                                                                 │
                       │                                                                                 ▼
Task 6 (Go struct + impl) ──▶ Task 7 (webhook.processed) ──▶ Task 8 (webhook.error) ──▶ Task 9 (stuck) ──▶ Task 10 (dispatcher) ──▶ Task 11 (integration) ──▶ Task 12 (CLI E2E) ──▶ Task 13 (ship)
```

Tasks 1 and 5 can run in parallel. Tasks 2–4 reshape the existing model_turn machinery. Tasks 6–9 add the new translators. Task 10 wires the dispatcher. Tasks 11–13 verify + ship.

**Commit / identity reminders (chitin-specific):**
- Git identity: `jpleva91@gmail.com` (the OSS chitin identity — do NOT use readybench.io).
- PR flow: non-draft PR → Copilot review → adversarial review (`/review`) → fix all findings → merge on green.
- Doc-only changes can commit directly to `main`.
- No content from Readybench / bench-devs — chitin is OSS.

---

## Task 1: Introduce `buildChainID` helper

**Files:**
- Modify: `go/execution-kernel/internal/ingest/openclaw.go`
- Test: `go/execution-kernel/internal/ingest/openclaw_test.go` (create if not present)

Goal: a single helper that is the only place the `"otel:..."` chain-id string is assembled. Every translator calls this helper; no ad-hoc string concatenation elsewhere.

- [ ] **Step 1: Write the failing test**

Create or append to `go/execution-kernel/internal/ingest/openclaw_test.go`:

```go
package ingest

import "testing"

func TestBuildChainID_UniformFormat(t *testing.T) {
	trace := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
		0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff,
	}
	span := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	got := buildChainID(trace, span)
	want := "otel:00112233445566778899aabbccddeeff:0102030405060708"
	if got != want {
		t.Fatalf("buildChainID mismatch\n got: %q\nwant: %q", got, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestBuildChainID_UniformFormat
```

Expected: FAIL with `undefined: buildChainID`.

- [ ] **Step 3: Implement `buildChainID` and `buildOtelLabels` in `openclaw.go`**

Add to `go/execution-kernel/internal/ingest/openclaw.go` (place near the top of the file, after the package-level types, before `ParseOpenClawSpans`):

```go
// buildChainID is the single source of truth for OTEL-ingest chain-id
// construction. Every translator calls this helper; no other code
// assembles "otel:..." strings. Uniform across event types so chain-ids
// never collide across spans in a trace.
//
// Invariant: the returned string has exactly one ":" prefix after "otel",
// one full-length (32 hex char) trace portion, and one full-length
// (16 hex char) span portion. Callers must have already validated
// trace/span length; this function does not re-check.
func buildChainID(traceID, spanID []byte) string {
	return "otel:" + hex.EncodeToString(traceID) + ":" + hex.EncodeToString(spanID)
}

// buildOtelLabels constructs the label map every OTEL-ingest event gets.
// Single source of truth for the label vocabulary: source, dialect,
// otel_trace_id, otel_span_id, and (when non-empty) otel_parent_span_id.
// parentSpanIDHex is "" when the span has no parent (root span).
func buildOtelLabels(traceIDHex, spanIDHex, parentSpanIDHex string) map[string]string {
	m := map[string]string{
		"source":        "otel",
		"dialect":       "openclaw",
		"otel_trace_id": traceIDHex,
		"otel_span_id":  spanIDHex,
	}
	if parentSpanIDHex != "" {
		m["otel_parent_span_id"] = parentSpanIDHex
	}
	return m
}
```

And add a unit test for `buildOtelLabels` to `openclaw_test.go`:

```go
func TestBuildOtelLabels_OmitsEmptyParent(t *testing.T) {
	got := buildOtelLabels("abc", "def", "")
	if _, ok := got["otel_parent_span_id"]; ok {
		t.Fatalf("empty parent should be omitted, got %+v", got)
	}
	if got["otel_trace_id"] != "abc" || got["otel_span_id"] != "def" {
		t.Fatalf("trace/span mismatch: %+v", got)
	}
	if got["source"] != "otel" || got["dialect"] != "openclaw" {
		t.Fatalf("constant labels missing: %+v", got)
	}
}

func TestBuildOtelLabels_IncludesParentWhenSet(t *testing.T) {
	got := buildOtelLabels("abc", "def", "parent-hex")
	if got["otel_parent_span_id"] != "parent-hex" {
		t.Fatalf("parent label missing: %+v", got)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestBuildChainID_UniformFormat
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/openclaw.go go/execution-kernel/internal/ingest/openclaw_test.go
rtk git commit -m "SP-2 T1: add buildChainID helper (uniform otel chain-id)"
```

---

## Task 2: Migrate `model_turn` chain-id to the uniform scheme

**Files:**
- Modify: `go/execution-kernel/internal/ingest/openclaw.go`
- Modify: `go/execution-kernel/internal/ingest/openclaw_integration_test.go`

Goal: SP-1's `EmitModelTurns` builds `chain_id = "otel:" + turn.TraceID` inline. Replace that with a call to `buildChainID(trace, span)`, add the span_id byte-slice to the `ModelTurn` struct, update the existing integration test expectations.

- [ ] **Step 1: Update the failing test expectation**

In `go/execution-kernel/internal/ingest/openclaw_integration_test.go`, find the assertion that checks the emitted event's `chain_id` (should be something like `want_chain_id := "otel:" + ...`). Update it to use the new scheme. Example patch:

```go
// was (SP-1):
// wantChainID := "otel:" + traceIDHex
// now:
wantChainID := "otel:" + traceIDHex + ":" + spanIDHex
```

Also ensure `spanIDHex` is derived from the same span_id used to build the fixture. The existing fixture already pins a specific span_id; reuse it.

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestOpenClawIngest
```

Expected: FAIL with a chain-id mismatch in the assertion.

- [ ] **Step 3: Add `SpanIDBytes`, `TraceIDBytes`, and `ParentSpanIDHex` to `ModelTurn`; use `buildChainID`**

In `openclaw.go`:

(a) Widen the struct with the byte fields and the parent-span field:

```go
// was:
// SpanID            string
// now (add these fields alongside the existing ones; do NOT remove the hex SpanID yet):
SpanIDBytes       []byte  // raw 8-byte span_id; hex-encoded on demand
TraceIDBytes      []byte  // raw 16-byte trace_id; hex-encoded on demand
ParentSpanIDHex   string  // hex-encoded parent_span_id; "" when span is a root span
```

(b) Update `translateModelUsage` to populate the new fields:

```go
mt := ModelTurn{
    TraceIDBytes: span.TraceId,
    SpanIDBytes:  span.SpanId,
    TraceID:      traceIDHex,
    SpanID:       hex.EncodeToString(span.SpanId),
    // ... rest unchanged
}
if len(span.ParentSpanId) == 8 && !isAllZero(span.ParentSpanId) {
    mt.ParentSpanIDHex = hex.EncodeToString(span.ParentSpanId)
}
```

(c) In `EmitModelTurns`, replace the inline chain-id assembly:

```go
// was:
// chainID := "otel:" + turn.TraceID
// now:
chainID := buildChainID(turn.TraceIDBytes, turn.SpanIDBytes)
```

(d) In `EmitModelTurns`, replace the inline labels-assembly with a call to `buildOtelLabels` (use the original `turn.SpanID` field name — the rename to `SpanIDHex` happens in Task 3):

```go
// was:
// ev.Labels["source"] = "otel"
// ev.Labels["dialect"] = "openclaw"
// now:
for k, v := range buildOtelLabels(turn.TraceID, turn.SpanID, turn.ParentSpanIDHex) {
    ev.Labels[k] = v
}
```

- [ ] **Step 4: Run the full package tests to verify**

```bash
cd go/execution-kernel && go test ./internal/ingest/
```

Expected: PASS (the integration test now produces the new chain-id format and matches the updated expectation).

- [ ] **Step 5: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/openclaw.go go/execution-kernel/internal/ingest/openclaw_integration_test.go
rtk git commit -m "SP-2 T2: migrate model_turn chain-id to uniform otel:<trace>:<span>"
```

---

## Task 3: Extract `model_turn` translator to its own file

**Files:**
- Create: `go/execution-kernel/internal/ingest/openclaw_model_turn.go`
- Modify: `go/execution-kernel/internal/ingest/openclaw.go`

Goal: pure refactor — move `ModelTurn` struct, `translateModelUsage`, `openclawModelUsageSpanName` constant, and `modelTurnPayload` struct out of `openclaw.go` into `openclaw_model_turn.go`. No behavior change. Tests still pass without modification.

- [ ] **Step 1: Create `openclaw_model_turn.go` with the moved code**

```go
// Package ingest — openclaw_model_turn.go translates openclaw.model.usage spans.
//
// Pinned to @openclaw/diagnostics-otel@2026.4.15-beta.1. Required attributes
// derive from the static source inventory at plugin lines 53644–53695.
package ingest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// openclawModelUsageSpanName is the span name mapped to model_turn events.
const openclawModelUsageSpanName = "openclaw.model.usage"

// ModelTurn is the parse-stage output for one openclaw.model.usage span.
type ModelTurn struct {
	TraceIDBytes      []byte
	SpanIDBytes       []byte
	TraceID           string
	SpanIDHex         string // renamed from SpanID to free the SpanID() interface method
	ParentSpanIDHex   string // "" when root span
	TsStr             string // renamed from Ts to free the Ts() interface method
	SurfaceStr        string // renamed from Surface to free the Surface() interface method
	Provider          string
	ModelName         string
	InputTokens       int64
	OutputTokens      int64
	SessionIDExternal string
	DurationMs        int64
	CacheReadTokens   int64
	CacheWriteTokens  int64
}

// modelTurnPayload is the typed shape of the model_turn event payload.
type modelTurnPayload struct {
	ModelName         string `json:"model_name"`
	Provider          string `json:"provider"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	SessionIDExternal string `json:"session_id_external,omitempty"`
	DurationMs        int64  `json:"duration_ms,omitempty"`
	CacheReadTokens   int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens  int64  `json:"cache_write_tokens,omitempty"`
}

// translateModelUsage is the per-span required+optional extraction for
// openclaw.model.usage. Returns (ModelTurn, "") on success, or
// (zero-ModelTurn, reason) with a typed reason on any required-attr failure.
func translateModelUsage(resource *resourcepb.Resource, span *tracepb.Span) (ModelTurn, string) {
	if len(span.TraceId) != 16 {
		return ModelTurn{}, "invalid_trace_id_length"
	}
	if isAllZero(span.TraceId) {
		return ModelTurn{}, "invalid_trace_id_zero"
	}
	if len(span.SpanId) != 8 {
		return ModelTurn{}, "invalid_span_id_length"
	}
	if isAllZero(span.SpanId) {
		return ModelTurn{}, "invalid_span_id_zero"
	}

	surface := getResourceStringAttr(resource, "service.name")
	if surface == "" {
		return ModelTurn{}, "missing_required_attr:service.name"
	}

	if span.StartTimeUnixNano == 0 {
		return ModelTurn{}, "missing_required_attr:start_time_unix_nano"
	}
	ts := time.Unix(0, int64(span.StartTimeUnixNano)).UTC().Format(time.RFC3339)

	provider := getSpanStringAttr(span, "openclaw.provider")
	if provider == "" {
		return ModelTurn{}, "missing_required_attr:openclaw.provider"
	}
	if provider == "unknown" {
		return ModelTurn{}, "unknown_value:openclaw.provider"
	}

	modelName := getSpanStringAttr(span, "openclaw.model")
	if modelName == "" {
		return ModelTurn{}, "missing_required_attr:openclaw.model"
	}
	if modelName == "unknown" {
		return ModelTurn{}, "unknown_value:openclaw.model"
	}

	inputTokens, inputPresent := getSpanIntAttr(span, "openclaw.tokens.input")
	if !inputPresent {
		return ModelTurn{}, "missing_required_attr:openclaw.tokens.input"
	}
	if inputTokens < 0 {
		return ModelTurn{}, "invalid_value:openclaw.tokens.input"
	}
	outputTokens, outputPresent := getSpanIntAttr(span, "openclaw.tokens.output")
	if !outputPresent {
		return ModelTurn{}, "missing_required_attr:openclaw.tokens.output"
	}
	if outputTokens < 0 {
		return ModelTurn{}, "invalid_value:openclaw.tokens.output"
	}

	mt := ModelTurn{
		TraceIDBytes: span.TraceId,
		SpanIDBytes:  span.SpanId,
		TraceID:      hex.EncodeToString(span.TraceId),
		SpanIDHex:    hex.EncodeToString(span.SpanId),
		TsStr:        ts,
		SurfaceStr:   surface,
		Provider:     provider,
		ModelName:    modelName,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
	if len(span.ParentSpanId) == 8 && !isAllZero(span.ParentSpanId) {
		mt.ParentSpanIDHex = hex.EncodeToString(span.ParentSpanId)
	}
	if sid := getSpanStringAttr(span, "openclaw.sessionId"); sid != "" {
		mt.SessionIDExternal = sid
	}
	if span.EndTimeUnixNano != 0 && span.EndTimeUnixNano >= span.StartTimeUnixNano {
		mt.DurationMs = int64((span.EndTimeUnixNano - span.StartTimeUnixNano) / 1_000_000)
	}
	if cr, ok := getSpanIntAttr(span, "openclaw.tokens.cache_read"); ok {
		if cr < 0 {
			return ModelTurn{}, "invalid_value:openclaw.tokens.cache_read"
		}
		mt.CacheReadTokens = cr
	}
	if cw, ok := getSpanIntAttr(span, "openclaw.tokens.cache_write"); ok {
		if cw < 0 {
			return ModelTurn{}, "invalid_value:openclaw.tokens.cache_write"
		}
		mt.CacheWriteTokens = cw
	}
	return mt, ""
}

// buildModelTurnPayload marshals the typed payload struct for the
// event envelope. Returns an error only on JSON encoding failure.
func buildModelTurnPayload(mt ModelTurn) (json.RawMessage, error) {
	p := modelTurnPayload{
		ModelName:         mt.ModelName,
		Provider:          mt.Provider,
		InputTokens:       mt.InputTokens,
		OutputTokens:      mt.OutputTokens,
		SessionIDExternal: mt.SessionIDExternal,
		DurationMs:        mt.DurationMs,
		CacheReadTokens:   mt.CacheReadTokens,
		CacheWriteTokens:  mt.CacheWriteTokens,
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal model_turn payload: %w", err)
	}
	return b, nil
}
```

- [ ] **Step 2: Remove the moved code from `openclaw.go` and update field references**

Delete from `openclaw.go`:
- The `openclawModelUsageSpanName` constant.
- The `ModelTurn` struct.
- The `translateModelUsage` function.
- The `modelTurnPayload` struct.

Keep in `openclaw.go` for now (they stay shared and get restructured further in Task 4):
- `ParseOpenClawSpans` (dispatcher; references `openclawModelUsageSpanName` from the new file — still works because same package).
- `EmitModelTurns` (inline payload assembly stays here for this task; Task 4 refactors it).
- `Quarantine`, `makeQuarantine`, `WriteQuarantine`, `sanitizeFilename`.
- All `getSpanStringAttr`, `getSpanIntAttr`, `getResourceStringAttr`, `isAllZero` helpers.
- `buildChainID` and `buildOtelLabels` helpers (from Task 1).

Update `EmitModelTurns` to call `buildModelTurnPayload` instead of inlining, AND to use the renamed struct fields (`TsStr`, `SurfaceStr`, `SpanIDHex` replacing the old `Ts`, `Surface`, `SpanID`):

```go
// was:
// ev.Ts = turn.Ts
// ev.Surface = turn.Surface
// ev.ChainID = "otel:" + turn.TraceID
// now:
ev.Ts = turn.TsStr
ev.Surface = turn.SurfaceStr
ev.ChainID = buildChainID(turn.TraceIDBytes, turn.SpanIDBytes)

// Labels — replace inline assignments with the shared helper:
// was:
// ev.Labels["source"] = "otel"
// ev.Labels["dialect"] = "openclaw"
// now:
for k, v := range buildOtelLabels(turn.TraceID, turn.SpanIDHex, turn.ParentSpanIDHex) {
    ev.Labels[k] = v
}

// Payload — replace inline struct construction with the helper:
// was:
// payload := modelTurnPayload{ ... }
// raw, err := json.Marshal(payload)
// now:
raw, err := buildModelTurnPayload(turn)
if err != nil {
    return emitted, fmt.Errorf("marshal payload for turn %d: %w", i, err)
}
ev.Payload = raw

// Also update the pre-sort in ParseOpenClawSpans (if still there; Task 4 removes it)
// to use the new field names:
// sort.SliceStable(turns, func(i, j int) bool {
//     if turns[i].TsStr != turns[j].TsStr {
//         return turns[i].TsStr < turns[j].TsStr
//     }
//     return turns[i].SpanIDHex < turns[j].SpanIDHex
// })
```

**Also update Task 2's earlier edits** — if Step 2 of Task 2 left `SpanID`, `Ts`, `Surface` populated in the struct literal (from before the rename), update those assignments too. The rename applies everywhere the struct is referenced.

- [ ] **Step 3: Verify build + tests still pass (no new test needed — refactor has no behavior change)**

```bash
cd go/execution-kernel && go build ./... && go test ./internal/ingest/
```

Expected: build succeeds, all existing tests PASS (including the integration test from Task 2).

- [ ] **Step 4: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/openclaw.go go/execution-kernel/internal/ingest/openclaw_model_turn.go
rtk git commit -m "SP-2 T3: extract model_turn translator to its own file"
```

---

## Task 4: Define `TranslatedSpan` interface and refactor `EmitEvents`

**Files:**
- Modify: `go/execution-kernel/internal/ingest/openclaw.go`
- Modify: `go/execution-kernel/internal/ingest/openclaw_model_turn.go`
- Modify: `go/execution-kernel/internal/ingest/openclaw_integration_test.go`

Goal: replace the `ModelTurn`-specific emit with a polymorphic emit that accepts `[]TranslatedSpan`. `ModelTurn` implements the interface. No new event types yet — this task is the shape change.

- [ ] **Step 1: Write the failing interface-impl test**

In `go/execution-kernel/internal/ingest/openclaw_test.go`, append:

```go
func TestModelTurn_ImplementsTranslatedSpan(t *testing.T) {
	var _ TranslatedSpan = ModelTurn{}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestModelTurn_ImplementsTranslatedSpan
```

Expected: FAIL with `undefined: TranslatedSpan` or `ModelTurn does not implement TranslatedSpan`.

- [ ] **Step 3: Add the `TranslatedSpan` interface to `openclaw.go`**

Add above `ParseOpenClawSpans`:

```go
// TranslatedSpan is the polymorphic output of every per-span translator.
// EmitEvents walks []TranslatedSpan without branching on concrete type.
// The interface is deliberately narrow: envelope fields + payload bytes
// + labels. Chain-id is always built via buildChainID; the interface
// hides the trace_id/span_id bytes behind ChainID().
type TranslatedSpan interface {
	EventType() string // "model_turn" | "webhook_received" | "webhook_failed" | "session_stuck"
	ChainID() string
	Ts() string
	Surface() string
	SpanID() string // hex, used for the deterministic sort tie-breaker
	Payload() (json.RawMessage, error)
	Labels() map[string]string
}
```

- [ ] **Step 4: Implement `TranslatedSpan` on `ModelTurn` in `openclaw_model_turn.go`**

Append to `openclaw_model_turn.go`:

```go
func (m ModelTurn) EventType() string { return "model_turn" }

func (m ModelTurn) ChainID() string {
	return buildChainID(m.TraceIDBytes, m.SpanIDBytes)
}

func (m ModelTurn) Ts() string      { return m.TsStr }
func (m ModelTurn) Surface() string { return m.SurfaceStr }
func (m ModelTurn) SpanID() string  { return m.SpanIDHex }

func (m ModelTurn) Payload() (json.RawMessage, error) {
	return buildModelTurnPayload(m)
}

func (m ModelTurn) Labels() map[string]string {
	return buildOtelLabels(m.TraceID, m.SpanIDHex, m.ParentSpanIDHex)
}

var _ TranslatedSpan = ModelTurn{}
```

**Field-name note:** `ModelTurn`'s fields were renamed in Task 3 (`Ts` → `TsStr`, `Surface` → `SurfaceStr`, `SpanID` → `SpanIDHex`) precisely so the interface methods `Ts()`, `Surface()`, `SpanID()` can be defined here. If the executor sees a compile error like `field and method with the same name`, they skipped the rename in Task 3 — go back and apply it.

- [ ] **Step 5: Run the interface test**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestModelTurn_ImplementsTranslatedSpan
```

Expected: PASS.

- [ ] **Step 6: Refactor `EmitModelTurns` → `EmitEvents`**

In `openclaw.go`, replace `EmitModelTurns` with `EmitEvents`:

```go
// EmitEvents validates the envelope template, writes every quarantine
// side-car first (crash-safety), then emits one chitin event per
// TranslatedSpan in deterministic order.
//
// Invariants: ValidateEnvelopeTemplate called first; quarantine written
// before the first Emit call; events sorted by (ts asc, span_id asc)
// before emit; a chain_id already present in the index is skipped
// (idempotent replay).
//
// Not safe for concurrent invocation: the em.Index.Get / em.Emit pair
// is not atomic. SP-2 preserves SP-1's single-process sequential
// assumption; push-mode concurrency is SP-3's problem.
func EmitEvents(em *emit.Emitter, dir string, tmpl *event.Event, spans []TranslatedSpan, quarantined []Quarantine) (int, error) {
	if err := ValidateEnvelopeTemplate(tmpl); err != nil {
		return 0, fmt.Errorf("invalid_envelope_template: %w", err)
	}

	for _, q := range quarantined {
		if err := WriteQuarantine(dir, q); err != nil {
			return 0, fmt.Errorf("write_quarantine: %w", err)
		}
	}

	// Deterministic order: ts ascending, span_id (hex) ascending tie-break.
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].Ts() != spans[j].Ts() {
			return spans[i].Ts() < spans[j].Ts()
		}
		return spans[i].SpanID() < spans[j].SpanID()
	})

	emitted := 0
	for i, span := range spans {
		chainID := span.ChainID()

		existing, err := em.Index.Get(chainID)
		if err != nil {
			return emitted, fmt.Errorf("index lookup for span %d: %w", i, err)
		}
		if existing != nil {
			continue
		}

		ev := cloneTemplate(tmpl)
		ev.EventType = span.EventType()
		ev.Ts = span.Ts()
		ev.Surface = span.Surface()
		ev.ChainID = chainID
		if ev.Labels == nil {
			ev.Labels = map[string]string{}
		}
		for k, v := range span.Labels() {
			ev.Labels[k] = v
		}

		raw, err := span.Payload()
		if err != nil {
			return emitted, fmt.Errorf("payload for span %d: %w", i, err)
		}
		ev.Payload = raw

		if err := em.Emit(&ev); err != nil {
			return emitted, fmt.Errorf("emit span %d: %w", i, err)
		}
		emitted++
	}
	return emitted, nil
}
```

Remove the old `EmitModelTurns` function entirely. The previous sort-inside-ParseOpenClawSpans call also needs to go — sorting now happens inside `EmitEvents`. Search `openclaw.go` for `sort.SliceStable` and remove the pre-sort block in `ParseOpenClawSpans`.

- [ ] **Step 7: Update `ParseOpenClawSpans` signature + callers**

`ParseOpenClawSpans` currently returns `([]ModelTurn, []Quarantine, error)`. Widen to `([]TranslatedSpan, []Quarantine, error)`:

```go
func ParseOpenClawSpans(rs []*tracepb.ResourceSpans) ([]TranslatedSpan, []Quarantine, error) {
	var translated []TranslatedSpan
	var quarantined []Quarantine

	IterSpans(rs, func(resource *resourcepb.Resource, span *tracepb.Span) {
		if span.Name != openclawModelUsageSpanName {
			quarantined = append(quarantined, makeQuarantine(
				fmt.Sprintf("unmapped_span_name:%s", span.Name), span,
			))
			return
		}
		mt, reason := translateModelUsage(resource, span)
		if reason != "" {
			quarantined = append(quarantined, makeQuarantine(reason, span))
			return
		}
		translated = append(translated, mt)
	})

	return translated, quarantined, nil
}
```

Update `openclaw_integration_test.go` and `cmd/chitin-kernel/main.go` (search for `EmitModelTurns`) to call `EmitEvents` with the widened type.

- [ ] **Step 8: Run all ingest tests to verify nothing regressed**

```bash
cd go/execution-kernel && go test ./...
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
rtk git add go/execution-kernel/
rtk git commit -m "SP-2 T4: introduce TranslatedSpan interface, rename EmitModelTurns->EmitEvents"
```

---

## Task 5: Add Zod schemas for the three new event types

**Files:**
- Modify: `libs/contracts/src/payloads.schema.ts`
- Modify: `libs/contracts/src/payloads.types.ts` (if the repo has split types from schemas; otherwise all in one file)
- Modify: `libs/contracts/src/event.schema.ts`
- Modify: `libs/contracts/tests/event.schema.test.ts`

- [ ] **Step 1: Write failing Zod round-trip tests**

Append to `libs/contracts/tests/event.schema.test.ts`:

```typescript
import { describe, it, expect } from 'vitest';
import {
  WebhookReceivedPayloadSchema,
  WebhookFailedPayloadSchema,
  SessionStuckPayloadSchema,
} from '../src/payloads.schema';

describe('WebhookReceivedPayloadSchema', () => {
  it('accepts valid minimal payload', () => {
    const p = { channel: 'telegram', webhook_type: 'message', duration_ms: 42 };
    expect(WebhookReceivedPayloadSchema.parse(p)).toEqual(p);
  });
  it('accepts optional chat_id', () => {
    const p = { channel: 'telegram', webhook_type: 'message', duration_ms: 42, chat_id: 'abc' };
    expect(WebhookReceivedPayloadSchema.parse(p)).toEqual(p);
  });
  it('rejects missing channel', () => {
    const p = { webhook_type: 'message', duration_ms: 42 };
    expect(() => WebhookReceivedPayloadSchema.parse(p)).toThrow();
  });
  it('rejects negative duration_ms', () => {
    const p = { channel: 'telegram', webhook_type: 'message', duration_ms: -1 };
    expect(() => WebhookReceivedPayloadSchema.parse(p)).toThrow();
  });
});

describe('WebhookFailedPayloadSchema', () => {
  it('accepts valid minimal payload', () => {
    const p = { channel: 'telegram', webhook_type: 'message', error_message: 'boom' };
    expect(WebhookFailedPayloadSchema.parse(p)).toEqual(p);
  });
  it('accepts optional chat_id', () => {
    const p = { channel: 'telegram', webhook_type: 'message', error_message: 'boom', chat_id: 'abc' };
    expect(WebhookFailedPayloadSchema.parse(p)).toEqual(p);
  });
  it('rejects missing error_message', () => {
    const p = { channel: 'telegram', webhook_type: 'message' };
    expect(() => WebhookFailedPayloadSchema.parse(p)).toThrow();
  });
});

describe('SessionStuckPayloadSchema', () => {
  it('accepts valid minimal payload', () => {
    const p = { state: 'awaiting_model', age_ms: 120000 };
    expect(SessionStuckPayloadSchema.parse(p)).toEqual(p);
  });
  it('accepts all optional fields', () => {
    const p = {
      state: 'awaiting_model',
      age_ms: 120000,
      session_id_external: 'sess-123',
      session_key: 'key-abc',
      queue_depth: 5,
    };
    expect(SessionStuckPayloadSchema.parse(p)).toEqual(p);
  });
  it('rejects missing state', () => {
    const p = { age_ms: 120000 };
    expect(() => SessionStuckPayloadSchema.parse(p)).toThrow();
  });
  it('rejects negative age_ms', () => {
    const p = { state: 'awaiting_model', age_ms: -1 };
    expect(() => SessionStuckPayloadSchema.parse(p)).toThrow();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
pnpm -F contracts test
```

Expected: FAIL with `module has no exported member 'WebhookReceivedPayloadSchema'` (etc.).

- [ ] **Step 3: Add the three payload schemas**

Append to `libs/contracts/src/payloads.schema.ts`:

```typescript
export const WebhookReceivedPayloadSchema = z.object({
  channel: z.string().min(1),
  webhook_type: z.string().min(1),
  duration_ms: z.number().int().nonnegative(),
  chat_id: z.string().optional(),
});
export type WebhookReceivedPayload = z.infer<typeof WebhookReceivedPayloadSchema>;

export const WebhookFailedPayloadSchema = z.object({
  channel: z.string().min(1),
  webhook_type: z.string().min(1),
  error_message: z.string().min(1),
  chat_id: z.string().optional(),
});
export type WebhookFailedPayload = z.infer<typeof WebhookFailedPayloadSchema>;

export const SessionStuckPayloadSchema = z.object({
  state: z.string().min(1),
  age_ms: z.number().int().nonnegative(),
  session_id_external: z.string().optional(),
  session_key: z.string().optional(),
  queue_depth: z.number().int().nonnegative().optional(),
});
export type SessionStuckPayload = z.infer<typeof SessionStuckPayloadSchema>;
```

- [ ] **Step 4: Add the three entries to the `EventSchema` discriminated union**

In `libs/contracts/src/event.schema.ts`, add imports:

```typescript
import {
  SessionStartPayloadSchema,
  UserPromptPayloadSchema,
  AssistantTurnPayloadSchema,
  CompactionPayloadSchema,
  SessionEndPayloadSchema,
  IntendedPayloadSchema,
  ExecutedPayloadSchema,
  FailedPayloadSchema,
  ModelTurnPayloadSchema,
  WebhookReceivedPayloadSchema,  // new
  WebhookFailedPayloadSchema,    // new
  SessionStuckPayloadSchema,     // new
} from './payloads.schema';
```

Extend the discriminated union (last three entries):

```typescript
export const EventSchema = z.discriminatedUnion('event_type', [
  z.object({ ...envShape, event_type: z.literal('session_start'), payload: SessionStartPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('user_prompt'), payload: UserPromptPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('assistant_turn'), payload: AssistantTurnPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('compaction'), payload: CompactionPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('session_end'), payload: SessionEndPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('intended'), payload: IntendedPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('executed'), payload: ExecutedPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('failed'), payload: FailedPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('model_turn'), payload: ModelTurnPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('webhook_received'), payload: WebhookReceivedPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('webhook_failed'), payload: WebhookFailedPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('session_stuck'), payload: SessionStuckPayloadSchema }),
]);
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
pnpm -F contracts test
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add libs/contracts/src/payloads.schema.ts libs/contracts/src/event.schema.ts libs/contracts/tests/event.schema.test.ts
rtk git commit -m "SP-2 T5: add Zod schemas for webhook_received, webhook_failed, session_stuck"
```

---

## Task 6: Go payload structs + `TranslatedSpan` implementations for the three new types

**Files:**
- Create: `go/execution-kernel/internal/ingest/openclaw_webhook.go`
- Create: `go/execution-kernel/internal/ingest/openclaw_session_stuck.go`

Goal: define the Go data model for the three new event types. Each struct implements `TranslatedSpan`. No translator functions yet — those are Tasks 7, 8, 9.

- [ ] **Step 1: Write failing interface-impl tests**

Append to `go/execution-kernel/internal/ingest/openclaw_test.go`:

```go
func TestWebhookReceived_ImplementsTranslatedSpan(t *testing.T) {
	var _ TranslatedSpan = WebhookReceived{}
}
func TestWebhookFailed_ImplementsTranslatedSpan(t *testing.T) {
	var _ TranslatedSpan = WebhookFailed{}
}
func TestSessionStuck_ImplementsTranslatedSpan(t *testing.T) {
	var _ TranslatedSpan = SessionStuck{}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run "TestWebhook|TestSessionStuck"
```

Expected: FAIL with `undefined: WebhookReceived` etc.

- [ ] **Step 3: Create `openclaw_webhook.go`**

```go
// Package ingest — openclaw_webhook.go translates openclaw.webhook.processed
// and openclaw.webhook.error spans.
//
// Pinned to @openclaw/diagnostics-otel@2026.4.15-beta.1. Attributes verified
// at plugin lines 53697–53734. openclaw.webhook.processed carries a duration;
// openclaw.webhook.error is an instant span with status=ERROR and an
// openclaw.error attribute carrying the redacted error message.
package ingest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// WebhookReceived is the translated form of openclaw.webhook.processed.
type WebhookReceived struct {
	TraceIDBytes    []byte
	SpanIDBytes     []byte
	TraceID         string
	SpanIDHex       string
	ParentSpanIDHex string // "" when root span
	TsStr           string
	SurfaceStr      string
	Channel         string
	WebhookType     string
	DurationMs      int64
	ChatID          string // optional; "" when absent
}

// WebhookFailed is the translated form of openclaw.webhook.error.
type WebhookFailed struct {
	TraceIDBytes    []byte
	SpanIDBytes     []byte
	TraceID         string
	SpanIDHex       string
	ParentSpanIDHex string // "" when root span
	TsStr           string
	SurfaceStr      string
	Channel         string
	WebhookType     string
	ErrorMessage    string
	ChatID          string // optional
}

type webhookReceivedPayload struct {
	Channel     string `json:"channel"`
	WebhookType string `json:"webhook_type"`
	DurationMs  int64  `json:"duration_ms"`
	ChatID      string `json:"chat_id,omitempty"`
}

type webhookFailedPayload struct {
	Channel      string `json:"channel"`
	WebhookType  string `json:"webhook_type"`
	ErrorMessage string `json:"error_message"`
	ChatID       string `json:"chat_id,omitempty"`
}

// --- TranslatedSpan impl: WebhookReceived ---

func (w WebhookReceived) EventType() string { return "webhook_received" }
func (w WebhookReceived) ChainID() string   { return buildChainID(w.TraceIDBytes, w.SpanIDBytes) }
func (w WebhookReceived) Ts() string        { return w.TsStr }
func (w WebhookReceived) Surface() string   { return w.SurfaceStr }
func (w WebhookReceived) SpanID() string    { return w.SpanIDHex }

func (w WebhookReceived) Payload() (json.RawMessage, error) {
	p := webhookReceivedPayload{
		Channel:     w.Channel,
		WebhookType: w.WebhookType,
		DurationMs:  w.DurationMs,
		ChatID:      w.ChatID,
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook_received payload: %w", err)
	}
	return b, nil
}

func (w WebhookReceived) Labels() map[string]string {
	return buildOtelLabels(w.TraceID, w.SpanIDHex, w.ParentSpanIDHex)
}

// --- TranslatedSpan impl: WebhookFailed ---

func (w WebhookFailed) EventType() string { return "webhook_failed" }
func (w WebhookFailed) ChainID() string   { return buildChainID(w.TraceIDBytes, w.SpanIDBytes) }
func (w WebhookFailed) Ts() string        { return w.TsStr }
func (w WebhookFailed) Surface() string   { return w.SurfaceStr }
func (w WebhookFailed) SpanID() string    { return w.SpanIDHex }

func (w WebhookFailed) Payload() (json.RawMessage, error) {
	p := webhookFailedPayload{
		Channel:      w.Channel,
		WebhookType:  w.WebhookType,
		ErrorMessage: w.ErrorMessage,
		ChatID:       w.ChatID,
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook_failed payload: %w", err)
	}
	return b, nil
}

func (w WebhookFailed) Labels() map[string]string {
	return buildOtelLabels(w.TraceID, w.SpanIDHex, w.ParentSpanIDHex)
}

var _ TranslatedSpan = WebhookReceived{}
var _ TranslatedSpan = WebhookFailed{}
```

Note: `hex` is imported but not used in this file until Tasks 7 and 8 (where `translateWebhookProcessed` and `translateWebhookError` call `hex.EncodeToString`). Go will flag the unused import in Task 6 — either comment out the `encoding/hex` import here and add it in Task 7, OR add an `_ = hex.EncodeToString` sink at package level to suppress the lint. Easiest path: delete the `"encoding/hex"` import line in Task 6 and re-add in Task 7 Step 4's implementation block.

- [ ] **Step 4: Create `openclaw_session_stuck.go`**

```go
// Package ingest — openclaw_session_stuck.go translates openclaw.session.stuck
// spans. Pinned to @openclaw/diagnostics-otel@2026.4.15-beta.1. Attributes
// verified at plugin lines 53783–53797. Instant span with status=ERROR and
// message="session stuck".
package ingest

import (
	"encoding/json"
	"fmt"
)

// SessionStuck is the translated form of openclaw.session.stuck.
type SessionStuck struct {
	TraceIDBytes      []byte
	SpanIDBytes       []byte
	TraceID           string
	SpanIDHex         string
	ParentSpanIDHex   string // "" when root span
	TsStr             string
	SurfaceStr        string
	State             string
	AgeMs             int64
	SessionIDExternal string // optional
	SessionKey        string // optional
	QueueDepth        int64  // paired with QueueDepthPresent; "" semantics via the bool
	QueueDepthPresent bool
}

type sessionStuckPayload struct {
	State             string `json:"state"`
	AgeMs             int64  `json:"age_ms"`
	SessionIDExternal string `json:"session_id_external,omitempty"`
	SessionKey        string `json:"session_key,omitempty"`
	QueueDepth        *int64 `json:"queue_depth,omitempty"`
}

func (s SessionStuck) EventType() string { return "session_stuck" }
func (s SessionStuck) ChainID() string   { return buildChainID(s.TraceIDBytes, s.SpanIDBytes) }
func (s SessionStuck) Ts() string        { return s.TsStr }
func (s SessionStuck) Surface() string   { return s.SurfaceStr }
func (s SessionStuck) SpanID() string    { return s.SpanIDHex }

func (s SessionStuck) Payload() (json.RawMessage, error) {
	p := sessionStuckPayload{
		State:             s.State,
		AgeMs:             s.AgeMs,
		SessionIDExternal: s.SessionIDExternal,
		SessionKey:        s.SessionKey,
	}
	if s.QueueDepthPresent {
		qd := s.QueueDepth
		p.QueueDepth = &qd
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal session_stuck payload: %w", err)
	}
	return b, nil
}

func (s SessionStuck) Labels() map[string]string {
	return buildOtelLabels(s.TraceID, s.SpanIDHex, s.ParentSpanIDHex)
}

var _ TranslatedSpan = SessionStuck{}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run "TestWebhook|TestSessionStuck|TestModelTurn_Implements"
```

Expected: PASS.

Also run the full suite to make sure the `SpanID` rename didn't break the integration test:

```bash
cd go/execution-kernel && go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/
rtk git commit -m "SP-2 T6: add Go structs + TranslatedSpan impls for 3 new event types"
```

---

## Task 7: Translate `openclaw.webhook.processed`

**Files:**
- Modify: `go/execution-kernel/internal/ingest/openclaw_webhook.go`
- Create: `go/execution-kernel/internal/ingest/openclaw_webhook_test.go`
- Create: `go/execution-kernel/internal/ingest/testdata/openclaw/webhook_processed/synthesized.pb`
- Create: `go/execution-kernel/internal/ingest/fixtures_test.go` (shared fixture-builder helper)

Attributes from plugin inventory (line 53697–53713):
- Required: `openclaw.channel` (string, "unknown" sentinel quarantines), `openclaw.webhook` (string, same sentinel rule).
- Optional: `openclaw.chatId` (string).
- Duration: derived from span end-start.

- [ ] **Step 1: Create the shared fixture + test-helper file**

Create `go/execution-kernel/internal/ingest/fixtures_test.go`. Contains two helpers used by Tasks 7, 8, 9:

- `buildFixture` — assembles a `TracesData` protobuf from attribute maps.
- `translateSingle[T]` — calls a per-span translator on every span in a decoded payload, returning `([]T, []Quarantine)`. Used before the dispatcher is wired in Task 10.

```go
// fixtures_test.go — test-only helper for building synthesized OTLP protobuf
// payloads for openclaw span translators. One source of truth for span
// assembly; individual test files pass attribute maps and get back a
// ready-to-decode []byte.
package ingest

import (
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type fixtureSpan struct {
	name           string
	traceID        []byte  // must be 16 bytes
	spanID         []byte  // must be 8 bytes
	parentSpanID   []byte  // 0 or 8 bytes
	startNanos     uint64
	endNanos       uint64
	stringAttrs    map[string]string
	intAttrs       map[string]int64
	statusCode     tracepb.Status_StatusCode
	statusMessage  string
}

func buildFixture(t *testing.T, serviceName string, spans []fixtureSpan) []byte {
	t.Helper()

	toAttrs := func(s map[string]string, i map[string]int64) []*commonpb.KeyValue {
		var out []*commonpb.KeyValue
		for k, v := range s {
			out = append(out, &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}})
		}
		for k, v := range i {
			out = append(out, &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: v}}})
		}
		return out
	}

	pbSpans := make([]*tracepb.Span, 0, len(spans))
	for _, s := range spans {
		sp := &tracepb.Span{
			Name:              s.name,
			TraceId:           s.traceID,
			SpanId:            s.spanID,
			ParentSpanId:      s.parentSpanID,
			StartTimeUnixNano: s.startNanos,
			EndTimeUnixNano:   s.endNanos,
			Attributes:        toAttrs(s.stringAttrs, s.intAttrs),
		}
		if s.statusCode != tracepb.Status_STATUS_CODE_UNSET {
			sp.Status = &tracepb.Status{Code: s.statusCode, Message: s.statusMessage}
		}
		pbSpans = append(pbSpans, sp)
	}

	td := &tracepb.TracesData{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: serviceName}}},
			}},
			ScopeSpans: []*tracepb.ScopeSpans{{Spans: pbSpans}},
		}},
	}

	data, err := proto.Marshal(td)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return data
}

// sampleTraceID returns a deterministic 16-byte trace_id.
func sampleTraceID(n byte) []byte {
	out := make([]byte, 16)
	for i := range out {
		out[i] = n
	}
	return out
}

// sampleSpanID returns a deterministic 8-byte span_id.
func sampleSpanID(n byte) []byte {
	out := make([]byte, 8)
	for i := range out {
		out[i] = n
	}
	return out
}

// sampleTs returns a deterministic start timestamp (2026-04-21T12:00:00Z).
func sampleTs(offsetSeconds int64) (startNanos, endNanos uint64) {
	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC).UnixNano()
	startNanos = uint64(base + offsetSeconds*int64(time.Second))
	endNanos = startNanos + uint64(time.Second) // 1s default duration
	return
}

// translateSingle bypasses the dispatcher and calls a per-span translator
// on every span in a decoded OTLP/protobuf payload. Used by Tasks 7, 8, 9
// before Task 10 wires ParseOpenClawSpans to the new translators.
// T is the per-span translated-struct type (WebhookReceived, WebhookFailed,
// SessionStuck, or ModelTurn).
func translateSingle[T any](t *testing.T, payload []byte, fn func(r *resourcepb.Resource, s *tracepb.Span) (T, string)) ([]T, []Quarantine) {
	t.Helper()
	rs, err := DecodeTraces(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var out []T
	var quarantined []Quarantine
	IterSpans(rs, func(r *resourcepb.Resource, s *tracepb.Span) {
		result, reason := fn(r, s)
		if reason != "" {
			quarantined = append(quarantined, makeQuarantine(reason, s))
			return
		}
		out = append(out, result)
	})
	return out, quarantined
}
```

- [ ] **Step 2: Write failing per-span unit tests for `translateWebhookProcessed`**

Create `go/execution-kernel/internal/ingest/openclaw_webhook_test.go`. Tests call `translateSingle(t, payload, translateWebhookProcessed)` directly, bypassing the dispatcher (which doesn't route to the new translator until Task 10):

```go
package ingest

import "testing"

func TestTranslateWebhookProcessed_ValidMinimal(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.processed",
		traceID:    sampleTraceID(0x11),
		spanID:     sampleSpanID(0x22),
		startNanos: start,
		endNanos:   end,
		stringAttrs: map[string]string{
			"openclaw.channel": "telegram",
			"openclaw.webhook": "message",
		},
	}})

	got, quarantined := translateSingle(t, payload, translateWebhookProcessed)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined=%v, want none", quarantined)
	}
	if len(got) != 1 {
		t.Fatalf("got=%d, want 1", len(got))
	}
	if got[0].Channel != "telegram" || got[0].WebhookType != "message" {
		t.Fatalf("channel/webhook mismatch: %+v", got[0])
	}
	if got[0].DurationMs != 1000 {
		t.Fatalf("duration_ms=%d, want 1000", got[0].DurationMs)
	}
	if got[0].EventType() != "webhook_received" {
		t.Fatalf("event_type=%q, want webhook_received", got[0].EventType())
	}
}

func TestTranslateWebhookProcessed_QuarantinesUnknownChannel(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.processed",
		traceID:    sampleTraceID(0x11),
		spanID:     sampleSpanID(0x22),
		startNanos: start,
		endNanos:   end,
		stringAttrs: map[string]string{
			"openclaw.channel": "unknown",
			"openclaw.webhook": "message",
		},
	}})

	got, quarantined := translateSingle(t, payload, translateWebhookProcessed)
	if len(got) != 0 {
		t.Fatalf("got=%d, want 0", len(got))
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined=%d, want 1", len(quarantined))
	}
	if quarantined[0].Reason != "unknown_value:openclaw.channel" {
		t.Fatalf("reason=%q", quarantined[0].Reason)
	}
}

func TestTranslateWebhookProcessed_OptionalChatID(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.processed",
		traceID:    sampleTraceID(0x11),
		spanID:     sampleSpanID(0x22),
		startNanos: start,
		endNanos:   end,
		stringAttrs: map[string]string{
			"openclaw.channel": "telegram",
			"openclaw.webhook": "message",
			"openclaw.chatId":  "chat-42",
		},
	}})

	got, _ := translateSingle(t, payload, translateWebhookProcessed)
	if got[0].ChatID != "chat-42" {
		t.Fatalf("chat_id=%q, want chat-42", got[0].ChatID)
	}
}

func TestTranslateWebhookProcessed_MissingChannel(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.processed",
		traceID:    sampleTraceID(0x11),
		spanID:     sampleSpanID(0x22),
		startNanos: start,
		endNanos:   end,
		stringAttrs: map[string]string{
			"openclaw.webhook": "message",
		},
	}})

	_, quarantined := translateSingle(t, payload, translateWebhookProcessed)
	if len(quarantined) != 1 || quarantined[0].Reason != "missing_required_attr:openclaw.channel" {
		t.Fatalf("want missing_required_attr:openclaw.channel, got %+v", quarantined)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestTranslateWebhookProcessed
```

Expected: FAIL — `undefined: translateWebhookProcessed`.

- [ ] **Step 4: Implement `translateWebhookProcessed` in `openclaw_webhook.go`**

Append:

```go
const openclawWebhookProcessedSpanName = "openclaw.webhook.processed"
const openclawWebhookErrorSpanName = "openclaw.webhook.error"

func translateWebhookProcessed(resource *resourcepb.Resource, span *tracepb.Span) (WebhookReceived, string) {
	if len(span.TraceId) != 16 {
		return WebhookReceived{}, "invalid_trace_id_length"
	}
	if isAllZero(span.TraceId) {
		return WebhookReceived{}, "invalid_trace_id_zero"
	}
	if len(span.SpanId) != 8 {
		return WebhookReceived{}, "invalid_span_id_length"
	}
	if isAllZero(span.SpanId) {
		return WebhookReceived{}, "invalid_span_id_zero"
	}

	surface := getResourceStringAttr(resource, "service.name")
	if surface == "" {
		return WebhookReceived{}, "missing_required_attr:service.name"
	}

	if span.StartTimeUnixNano == 0 {
		return WebhookReceived{}, "missing_required_attr:start_time_unix_nano"
	}
	ts := time.Unix(0, int64(span.StartTimeUnixNano)).UTC().Format(time.RFC3339)

	channel := getSpanStringAttr(span, "openclaw.channel")
	if channel == "" {
		return WebhookReceived{}, "missing_required_attr:openclaw.channel"
	}
	if channel == "unknown" {
		return WebhookReceived{}, "unknown_value:openclaw.channel"
	}

	webhookType := getSpanStringAttr(span, "openclaw.webhook")
	if webhookType == "" {
		return WebhookReceived{}, "missing_required_attr:openclaw.webhook"
	}
	if webhookType == "unknown" {
		return WebhookReceived{}, "unknown_value:openclaw.webhook"
	}

	var durationMs int64
	if span.EndTimeUnixNano != 0 && span.EndTimeUnixNano >= span.StartTimeUnixNano {
		durationMs = int64((span.EndTimeUnixNano - span.StartTimeUnixNano) / 1_000_000)
	}

	w := WebhookReceived{
		TraceIDBytes: span.TraceId,
		SpanIDBytes:  span.SpanId,
		TraceID:      hex.EncodeToString(span.TraceId),
		SpanIDHex:    hex.EncodeToString(span.SpanId),
		TsStr:        ts,
		SurfaceStr:   surface,
		Channel:      channel,
		WebhookType:  webhookType,
		DurationMs:   durationMs,
	}
	if len(span.ParentSpanId) == 8 && !isAllZero(span.ParentSpanId) {
		w.ParentSpanIDHex = hex.EncodeToString(span.ParentSpanId)
	}
	if chatID := getSpanStringAttr(span, "openclaw.chatId"); chatID != "" {
		w.ChatID = chatID
	}
	return w, ""
}
```

Make sure `openclaw_webhook.go` imports `encoding/hex`, `time`, `resourcepb`, `tracepb`.

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestTranslateWebhookProcessed
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/
rtk git commit -m "SP-2 T7: translate openclaw.webhook.processed -> webhook_received"
```

---

## Task 8: Translate `openclaw.webhook.error`

**Files:**
- Modify: `go/execution-kernel/internal/ingest/openclaw_webhook.go`
- Modify: `go/execution-kernel/internal/ingest/openclaw_webhook_test.go`

Attributes from plugin inventory (line 53715–53733):
- Required: `openclaw.channel` ("unknown" quarantines), `openclaw.webhook` (same), `openclaw.error` (the redacted error message, required).
- Optional: `openclaw.chatId`.
- Span status code: `ERROR`, message: the redacted error.
- Instant span (start≈end; do not record duration).

- [ ] **Step 1: Write failing unit tests**

Append to `openclaw_webhook_test.go`:

```go
func TestTranslateWebhookError_ValidMinimal(t *testing.T) {
	start, end := sampleTs(0)
	// Instant span: end == start.
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:          "openclaw.webhook.error",
		traceID:       sampleTraceID(0x33),
		spanID:        sampleSpanID(0x44),
		startNanos:    start,
		endNanos:      start, // instant
		stringAttrs: map[string]string{
			"openclaw.channel": "telegram",
			"openclaw.webhook": "message",
			"openclaw.error":   "transport failed: timeout",
		},
		statusCode:    tracepb.Status_STATUS_CODE_ERROR,
		statusMessage: "transport failed: timeout",
	}})
	_ = end

	got, quarantined := translateSingle(t, payload, translateWebhookError)
	if len(quarantined) != 0 {
		t.Fatalf("quarantine=%v", quarantined)
	}
	if len(got) != 1 {
		t.Fatalf("got=%d, want 1", len(got))
	}
	if got[0].Channel != "telegram" || got[0].WebhookType != "message" {
		t.Fatalf("mismatch: %+v", got[0])
	}
	if got[0].ErrorMessage != "transport failed: timeout" {
		t.Fatalf("error_message=%q", got[0].ErrorMessage)
	}
}

func TestTranslateWebhookError_MissingError(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.error",
		traceID:    sampleTraceID(0x33),
		spanID:     sampleSpanID(0x44),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{
			"openclaw.channel": "telegram",
			"openclaw.webhook": "message",
		},
	}})
	_, quarantined := translateSingle(t, payload, translateWebhookError)
	if len(quarantined) != 1 || quarantined[0].Reason != "missing_required_attr:openclaw.error" {
		t.Fatalf("reason=%+v", quarantined)
	}
}

func TestTranslateWebhookError_UnknownChannel(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.webhook.error",
		traceID:    sampleTraceID(0x33),
		spanID:     sampleSpanID(0x44),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{
			"openclaw.channel": "unknown",
			"openclaw.webhook": "message",
			"openclaw.error":   "boom",
		},
	}})
	_, quarantined := translateSingle(t, payload, translateWebhookError)
	if len(quarantined) != 1 || quarantined[0].Reason != "unknown_value:openclaw.channel" {
		t.Fatalf("reason=%+v", quarantined)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestTranslateWebhookError
```

Expected: FAIL with `undefined: translateWebhookError`.

- [ ] **Step 3: Implement `translateWebhookError` in `openclaw_webhook.go`**

Append:

```go
func translateWebhookError(resource *resourcepb.Resource, span *tracepb.Span) (WebhookFailed, string) {
	if len(span.TraceId) != 16 {
		return WebhookFailed{}, "invalid_trace_id_length"
	}
	if isAllZero(span.TraceId) {
		return WebhookFailed{}, "invalid_trace_id_zero"
	}
	if len(span.SpanId) != 8 {
		return WebhookFailed{}, "invalid_span_id_length"
	}
	if isAllZero(span.SpanId) {
		return WebhookFailed{}, "invalid_span_id_zero"
	}

	surface := getResourceStringAttr(resource, "service.name")
	if surface == "" {
		return WebhookFailed{}, "missing_required_attr:service.name"
	}

	if span.StartTimeUnixNano == 0 {
		return WebhookFailed{}, "missing_required_attr:start_time_unix_nano"
	}
	ts := time.Unix(0, int64(span.StartTimeUnixNano)).UTC().Format(time.RFC3339)

	channel := getSpanStringAttr(span, "openclaw.channel")
	if channel == "" {
		return WebhookFailed{}, "missing_required_attr:openclaw.channel"
	}
	if channel == "unknown" {
		return WebhookFailed{}, "unknown_value:openclaw.channel"
	}

	webhookType := getSpanStringAttr(span, "openclaw.webhook")
	if webhookType == "" {
		return WebhookFailed{}, "missing_required_attr:openclaw.webhook"
	}
	if webhookType == "unknown" {
		return WebhookFailed{}, "unknown_value:openclaw.webhook"
	}

	errMsg := getSpanStringAttr(span, "openclaw.error")
	if errMsg == "" {
		return WebhookFailed{}, "missing_required_attr:openclaw.error"
	}

	w := WebhookFailed{
		TraceIDBytes: span.TraceId,
		SpanIDBytes:  span.SpanId,
		TraceID:      hex.EncodeToString(span.TraceId),
		SpanIDHex:    hex.EncodeToString(span.SpanId),
		TsStr:        ts,
		SurfaceStr:   surface,
		Channel:      channel,
		WebhookType:  webhookType,
		ErrorMessage: errMsg,
	}
	if len(span.ParentSpanId) == 8 && !isAllZero(span.ParentSpanId) {
		w.ParentSpanIDHex = hex.EncodeToString(span.ParentSpanId)
	}
	if chatID := getSpanStringAttr(span, "openclaw.chatId"); chatID != "" {
		w.ChatID = chatID
	}
	return w, ""
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestTranslateWebhookError
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/
rtk git commit -m "SP-2 T8: translate openclaw.webhook.error -> webhook_failed"
```

---

## Task 9: Translate `openclaw.session.stuck`

**Files:**
- Modify: `go/execution-kernel/internal/ingest/openclaw_session_stuck.go`
- Create: `go/execution-kernel/internal/ingest/openclaw_session_stuck_test.go`

Attributes from plugin inventory (line 53783–53797):
- Required: `openclaw.state` (string), `openclaw.ageMs` (number).
- Optional: `openclaw.sessionId`, `openclaw.sessionKey`, `openclaw.queueDepth`.
- Span status code: `ERROR`, message: "session stuck".
- Instant span.

**Numeric attribute encoding gotcha:** `openclaw.ageMs` and `openclaw.queueDepth` are JS numbers; OTEL SDK emits them as `IntValue` if the number has no fractional part, else `DoubleValue`. The existing `getSpanIntAttr` only reads `IntValue`. Add a sibling `getSpanIntOrDoubleAsIntAttr` helper that accepts both.

- [ ] **Step 1: Write the failing helper test**

Append to `openclaw_test.go`:

```go
func TestGetSpanIntOrDoubleAsIntAttr(t *testing.T) {
	span := &tracepb.Span{
		Attributes: []*commonpb.KeyValue{
			{Key: "int_attr", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 42}}},
			{Key: "double_attr", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: 42.0}}},
			{Key: "fractional", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: 42.5}}},
		},
	}
	if v, ok := getSpanIntOrDoubleAsIntAttr(span, "int_attr"); !ok || v != 42 {
		t.Fatalf("int_attr: got (%d,%v)", v, ok)
	}
	if v, ok := getSpanIntOrDoubleAsIntAttr(span, "double_attr"); !ok || v != 42 {
		t.Fatalf("double_attr: got (%d,%v)", v, ok)
	}
	if _, ok := getSpanIntOrDoubleAsIntAttr(span, "fractional"); ok {
		t.Fatalf("fractional: want !ok (fractional values rejected)")
	}
	if _, ok := getSpanIntOrDoubleAsIntAttr(span, "missing"); ok {
		t.Fatalf("missing: want !ok")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestGetSpanIntOrDouble
```

Expected: FAIL with `undefined: getSpanIntOrDoubleAsIntAttr`.

- [ ] **Step 3: Implement `getSpanIntOrDoubleAsIntAttr` in `openclaw.go`**

Append to `openclaw.go` (in the attribute-helpers section):

```go
// getSpanIntOrDoubleAsIntAttr returns the attribute value as int64.
// Accepts IntValue or DoubleValue (if the double has no fractional part).
// Returns (0, false) for missing, wrong-type, or fractional doubles.
// Duplicate-key handling: last write wins (matches getSpanIntAttr).
func getSpanIntOrDoubleAsIntAttr(s *tracepb.Span, key string) (int64, bool) {
	var found bool
	var last int64
	for _, kv := range s.Attributes {
		if kv.Key != key || kv.Value == nil {
			continue
		}
		switch v := kv.Value.GetValue().(type) {
		case *commonpb.AnyValue_IntValue:
			last = v.IntValue
			found = true
		case *commonpb.AnyValue_DoubleValue:
			if v.DoubleValue == float64(int64(v.DoubleValue)) {
				last = int64(v.DoubleValue)
				found = true
			} else {
				found = false
			}
		}
	}
	return last, found
}
```

- [ ] **Step 4: Run the helper test to verify it passes**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestGetSpanIntOrDouble
```

Expected: PASS.

- [ ] **Step 5: Write failing `translateSessionStuck` tests**

Create `go/execution-kernel/internal/ingest/openclaw_session_stuck_test.go`:

```go
package ingest

import "testing"

func TestTranslateSessionStuck_ValidMinimal(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.session.stuck",
		traceID:    sampleTraceID(0x55),
		spanID:     sampleSpanID(0x66),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{
			"openclaw.state": "awaiting_model",
		},
		intAttrs: map[string]int64{
			"openclaw.ageMs": 120000,
		},
		statusCode:    tracepb.Status_STATUS_CODE_ERROR,
		statusMessage: "session stuck",
	}})

	got, quarantined := translateSingle(t, payload, translateSessionStuck)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined=%+v", quarantined)
	}
	if len(got) != 1 {
		t.Fatalf("got=%d, want 1", len(got))
	}
	if got[0].State != "awaiting_model" || got[0].AgeMs != 120000 {
		t.Fatalf("mismatch: %+v", got[0])
	}
	if got[0].QueueDepthPresent {
		t.Fatalf("queue_depth_present=true, want false")
	}
}

func TestTranslateSessionStuck_AllOptional(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.session.stuck",
		traceID:    sampleTraceID(0x55),
		spanID:     sampleSpanID(0x66),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{
			"openclaw.state":      "awaiting_model",
			"openclaw.sessionId":  "sess-abc",
			"openclaw.sessionKey": "key-xyz",
		},
		intAttrs: map[string]int64{
			"openclaw.ageMs":      120000,
			"openclaw.queueDepth": 5,
		},
	}})

	got, _ := translateSingle(t, payload, translateSessionStuck)
	s := got[0]
	if s.SessionIDExternal != "sess-abc" {
		t.Fatalf("session_id_external=%q", s.SessionIDExternal)
	}
	if s.SessionKey != "key-xyz" {
		t.Fatalf("session_key=%q", s.SessionKey)
	}
	if !s.QueueDepthPresent || s.QueueDepth != 5 {
		t.Fatalf("queue_depth: %+v", s)
	}
}

func TestTranslateSessionStuck_MissingState(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.session.stuck",
		traceID:    sampleTraceID(0x55),
		spanID:     sampleSpanID(0x66),
		startNanos: start,
		endNanos:   start,
		intAttrs:   map[string]int64{"openclaw.ageMs": 120000},
	}})

	_, quarantined := translateSingle(t, payload, translateSessionStuck)
	if len(quarantined) != 1 || quarantined[0].Reason != "missing_required_attr:openclaw.state" {
		t.Fatalf("reason=%+v", quarantined)
	}
}

func TestTranslateSessionStuck_MissingAgeMs(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.session.stuck",
		traceID:    sampleTraceID(0x55),
		spanID:     sampleSpanID(0x66),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{"openclaw.state": "awaiting_model"},
	}})

	_, quarantined := translateSingle(t, payload, translateSessionStuck)
	if len(quarantined) != 1 || quarantined[0].Reason != "missing_required_attr:openclaw.ageMs" {
		t.Fatalf("reason=%+v", quarantined)
	}
}

func TestTranslateSessionStuck_NegativeAgeMs(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.session.stuck",
		traceID:    sampleTraceID(0x55),
		spanID:     sampleSpanID(0x66),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{"openclaw.state": "awaiting_model"},
		intAttrs:   map[string]int64{"openclaw.ageMs": -1},
	}})

	_, quarantined := translateSingle(t, payload, translateSessionStuck)
	if len(quarantined) != 1 || quarantined[0].Reason != "invalid_value:openclaw.ageMs" {
		t.Fatalf("reason=%+v", quarantined)
	}
}
```

- [ ] **Step 6: Implement `translateSessionStuck` in `openclaw_session_stuck.go`**

Add imports and function:

```go
import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

const openclawSessionStuckSpanName = "openclaw.session.stuck"

func translateSessionStuck(resource *resourcepb.Resource, span *tracepb.Span) (SessionStuck, string) {
	if len(span.TraceId) != 16 {
		return SessionStuck{}, "invalid_trace_id_length"
	}
	if isAllZero(span.TraceId) {
		return SessionStuck{}, "invalid_trace_id_zero"
	}
	if len(span.SpanId) != 8 {
		return SessionStuck{}, "invalid_span_id_length"
	}
	if isAllZero(span.SpanId) {
		return SessionStuck{}, "invalid_span_id_zero"
	}

	surface := getResourceStringAttr(resource, "service.name")
	if surface == "" {
		return SessionStuck{}, "missing_required_attr:service.name"
	}

	if span.StartTimeUnixNano == 0 {
		return SessionStuck{}, "missing_required_attr:start_time_unix_nano"
	}
	ts := time.Unix(0, int64(span.StartTimeUnixNano)).UTC().Format(time.RFC3339)

	state := getSpanStringAttr(span, "openclaw.state")
	if state == "" {
		return SessionStuck{}, "missing_required_attr:openclaw.state"
	}

	ageMs, agePresent := getSpanIntOrDoubleAsIntAttr(span, "openclaw.ageMs")
	if !agePresent {
		return SessionStuck{}, "missing_required_attr:openclaw.ageMs"
	}
	if ageMs < 0 {
		return SessionStuck{}, "invalid_value:openclaw.ageMs"
	}

	s := SessionStuck{
		TraceIDBytes: span.TraceId,
		SpanIDBytes:  span.SpanId,
		TraceID:      hex.EncodeToString(span.TraceId),
		SpanIDHex:    hex.EncodeToString(span.SpanId),
		TsStr:        ts,
		SurfaceStr:   surface,
		State:        state,
		AgeMs:        ageMs,
	}
	if len(span.ParentSpanId) == 8 && !isAllZero(span.ParentSpanId) {
		s.ParentSpanIDHex = hex.EncodeToString(span.ParentSpanId)
	}
	if sid := getSpanStringAttr(span, "openclaw.sessionId"); sid != "" {
		s.SessionIDExternal = sid
	}
	if sk := getSpanStringAttr(span, "openclaw.sessionKey"); sk != "" {
		s.SessionKey = sk
	}
	if qd, ok := getSpanIntOrDoubleAsIntAttr(span, "openclaw.queueDepth"); ok {
		if qd < 0 {
			return SessionStuck{}, "invalid_value:openclaw.queueDepth"
		}
		s.QueueDepth = qd
		s.QueueDepthPresent = true
	}

	// Suppress unused-import lint; json + fmt used in Payload().
	_ = json.Marshal
	_ = fmt.Errorf

	return s, ""
}
```

Remove the `_ = json.Marshal` / `_ = fmt.Errorf` lines if the imports are already used elsewhere in the file.

- [ ] **Step 7: Run the tests to verify they pass**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestTranslateSessionStuck
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/
rtk git commit -m "SP-2 T9: translate openclaw.session.stuck -> session_stuck"
```

---

## Task 10: Wire the dispatcher to route all four span types

**Files:**
- Modify: `go/execution-kernel/internal/ingest/openclaw.go`

- [ ] **Step 1: Write the failing dispatcher-routing test**

Append to `openclaw_test.go`:

```go
func TestParseOpenClawSpans_RoutesAllFourSpanTypes(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{
		{
			name:       "openclaw.model.usage",
			traceID:    sampleTraceID(0x01),
			spanID:     sampleSpanID(0x01),
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{
				"openclaw.provider": "ollama",
				"openclaw.model":    "qwen2.5:0.5b",
			},
			intAttrs: map[string]int64{
				"openclaw.tokens.input":  10,
				"openclaw.tokens.output": 20,
			},
		},
		{
			name:       "openclaw.webhook.processed",
			traceID:    sampleTraceID(0x02),
			spanID:     sampleSpanID(0x02),
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{
				"openclaw.channel": "telegram",
				"openclaw.webhook": "message",
			},
		},
		{
			name:       "openclaw.webhook.error",
			traceID:    sampleTraceID(0x03),
			spanID:     sampleSpanID(0x03),
			startNanos: start, endNanos: start,
			stringAttrs: map[string]string{
				"openclaw.channel": "telegram",
				"openclaw.webhook": "message",
				"openclaw.error":   "boom",
			},
		},
		{
			name:       "openclaw.session.stuck",
			traceID:    sampleTraceID(0x04),
			spanID:     sampleSpanID(0x04),
			startNanos: start, endNanos: start,
			stringAttrs: map[string]string{"openclaw.state": "awaiting_model"},
			intAttrs:   map[string]int64{"openclaw.ageMs": 120000},
		},
		{
			name:       "openclaw.message.processed", // unmapped — should quarantine
			traceID:    sampleTraceID(0x05),
			spanID:     sampleSpanID(0x05),
			startNanos: start, endNanos: end,
		},
	})

	rs, _ := DecodeTraces(payload)
	spans, quarantined, err := ParseOpenClawSpans(rs)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(spans) != 4 {
		t.Fatalf("spans=%d, want 4", len(spans))
	}
	if len(quarantined) != 1 || quarantined[0].Reason != "unmapped_span_name:openclaw.message.processed" {
		t.Fatalf("quarantined=%+v", quarantined)
	}

	// Verify each span type present.
	types := map[string]int{}
	for _, s := range spans {
		types[s.EventType()]++
	}
	want := map[string]int{"model_turn": 1, "webhook_received": 1, "webhook_failed": 1, "session_stuck": 1}
	for k, v := range want {
		if types[k] != v {
			t.Fatalf("event_type %q count=%d, want %d (all counts: %+v)", k, types[k], v, types)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestParseOpenClawSpans_RoutesAllFour
```

Expected: FAIL — only `openclaw.model.usage` routes; the other three land in quarantine with `unmapped_span_name`.

- [ ] **Step 3: Widen the dispatcher in `openclaw.go` with duplicate-span detection**

Replace `ParseOpenClawSpans` with (note the `seen` map implementing the spec's `duplicate_span_id` quarantine rule):

```go
// seenKey is the (trace_id, span_id) pair used to detect duplicates within
// a single ingest batch. The pair — not just span_id — preserves the
// possibility that two distinct traces share a span_id by coincidence
// (OTEL only guarantees span_id uniqueness within a trace).
type seenKey struct {
	trace [16]byte
	span  [8]byte
}

func ParseOpenClawSpans(rs []*tracepb.ResourceSpans) ([]TranslatedSpan, []Quarantine, error) {
	var translated []TranslatedSpan
	var quarantined []Quarantine
	seen := map[seenKey]bool{}

	IterSpans(rs, func(resource *resourcepb.Resource, span *tracepb.Span) {
		// Duplicate detection runs BEFORE translation so malformed spans
		// (wrong trace/span length) quarantine with the length reason
		// rather than the duplicate reason. Only well-formed pairs enter
		// the seen set.
		if len(span.TraceId) == 16 && len(span.SpanId) == 8 {
			var key seenKey
			copy(key.trace[:], span.TraceId)
			copy(key.span[:], span.SpanId)
			if seen[key] {
				quarantined = append(quarantined, makeQuarantine("duplicate_span_id", span))
				return
			}
			seen[key] = true
		}

		switch span.Name {
		case openclawModelUsageSpanName:
			mt, reason := translateModelUsage(resource, span)
			if reason != "" {
				quarantined = append(quarantined, makeQuarantine(reason, span))
				return
			}
			translated = append(translated, mt)
		case openclawWebhookProcessedSpanName:
			w, reason := translateWebhookProcessed(resource, span)
			if reason != "" {
				quarantined = append(quarantined, makeQuarantine(reason, span))
				return
			}
			translated = append(translated, w)
		case openclawWebhookErrorSpanName:
			w, reason := translateWebhookError(resource, span)
			if reason != "" {
				quarantined = append(quarantined, makeQuarantine(reason, span))
				return
			}
			translated = append(translated, w)
		case openclawSessionStuckSpanName:
			s, reason := translateSessionStuck(resource, span)
			if reason != "" {
				quarantined = append(quarantined, makeQuarantine(reason, span))
				return
			}
			translated = append(translated, s)
		default:
			quarantined = append(quarantined, makeQuarantine(
				fmt.Sprintf("unmapped_span_name:%s", span.Name), span,
			))
		}
	})

	return translated, quarantined, nil
}
```

- [ ] **Step 4: Run the dispatcher test to verify it passes**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestParseOpenClawSpans_RoutesAllFour
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/openclaw.go
rtk git commit -m "SP-2 T10: wire dispatcher for all four openclaw span types"
```

---

## Task 11: Integration + boundary tests

**Files:**
- Modify: `go/execution-kernel/internal/ingest/openclaw_integration_test.go`

Add six integration tests covering the boundaries the spec promises (§Error handling & boundaries).

- [ ] **Step 1: Write the failing integration tests**

Append to `openclaw_integration_test.go`:

```go
func TestOpenClawIngest_EmptyPayload(t *testing.T) {
	td := &tracepb.TracesData{}
	data, _ := proto.Marshal(td)

	rs, err := DecodeTraces(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	spans, quarantined, err := ParseOpenClawSpans(rs)
	if err != nil || spans != nil || quarantined != nil {
		t.Fatalf("empty: spans=%v quarantined=%v err=%v", spans, quarantined, err)
	}
}

func TestOpenClawIngest_AllQuarantine(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{
		{name: "something.else", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0x01), startNanos: start, endNanos: end},
		{name: "another.span",   traceID: sampleTraceID(0x02), spanID: sampleSpanID(0x02), startNanos: start, endNanos: end},
	})

	rs, _ := DecodeTraces(payload)
	spans, quarantined, _ := ParseOpenClawSpans(rs)
	if len(spans) != 0 {
		t.Fatalf("spans=%d, want 0", len(spans))
	}
	if len(quarantined) != 2 {
		t.Fatalf("quarantined=%d, want 2", len(quarantined))
	}
}

func TestOpenClawIngest_MixedValidInvalid(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{
		// Valid model_turn
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0x01),
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
		},
		// Invalid (missing provider)
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x02), spanID: sampleSpanID(0x02),
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
		},
	})

	rs, _ := DecodeTraces(payload)
	spans, quarantined, _ := ParseOpenClawSpans(rs)
	if len(spans) != 1 {
		t.Fatalf("spans=%d, want 1", len(spans))
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined=%d, want 1", len(quarantined))
	}
}

func TestOpenClawIngest_IdenticalTsSortsBySpanID(t *testing.T) {
	start, end := sampleTs(0)
	// Build two valid model_turn spans with identical ts but different span_ids.
	payload := buildFixture(t, "openclaw", []fixtureSpan{
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0xbb), // lexicographically LATER
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
		},
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x02), spanID: sampleSpanID(0xaa), // lexicographically EARLIER
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
		},
	})

	rs, _ := DecodeTraces(payload)
	// Route through EmitEvents to exercise the sort.
	em, dir, tmpl := setupTestEmitter(t) // existing helper in openclaw_integration_test.go
	spans, quarantined, _ := ParseOpenClawSpans(rs)
	n, err := EmitEvents(em, dir, tmpl, spans, quarantined)
	if err != nil || n != 2 {
		t.Fatalf("emit: n=%d err=%v", n, err)
	}

	// Read back the jsonl; first event should be the 0xaa span.
	events := readEmittedEvents(t, dir) // existing helper
	if len(events) != 2 {
		t.Fatalf("events=%d", len(events))
	}
	first := events[0]
	if !strings.HasSuffix(first.ChainID, ":"+hex.EncodeToString(sampleSpanID(0xaa))) {
		t.Fatalf("first chain_id=%q, want suffix 0xaa", first.ChainID)
	}
}

func TestOpenClawIngest_DuplicateSpanIDQuarantinesLaterSpans(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0x01),
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
		},
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0x01), // same trace+span
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 9, "openclaw.tokens.output": 9},
		},
	})

	rs, _ := DecodeTraces(payload)
	spans, quarantined, _ := ParseOpenClawSpans(rs)
	if len(spans) != 1 {
		t.Fatalf("spans=%d, want 1 (first wins)", len(spans))
	}
	if len(quarantined) != 1 || quarantined[0].Reason != "duplicate_span_id" {
		t.Fatalf("quarantined=%+v, want 1 with reason=duplicate_span_id", quarantined)
	}
}

func TestOpenClawIngest_IdempotentReplay(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name: "openclaw.model.usage", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0x01),
		startNanos: start, endNanos: end,
		stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
		intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
	}})

	em, dir, tmpl := setupTestEmitter(t)

	rs1, _ := DecodeTraces(payload)
	spans1, q1, _ := ParseOpenClawSpans(rs1)
	n1, _ := EmitEvents(em, dir, tmpl, spans1, q1)

	rs2, _ := DecodeTraces(payload)
	spans2, q2, _ := ParseOpenClawSpans(rs2)
	n2, _ := EmitEvents(em, dir, tmpl, spans2, q2)

	if n1 != 1 || n2 != 0 {
		t.Fatalf("n1=%d n2=%d, want 1 and 0", n1, n2)
	}
}
```

- [ ] **Step 2: Run the tests — some will fail because helpers don't exist yet**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestOpenClawIngest
```

Expected: FAIL — `setupTestEmitter`, `readEmittedEvents` may need to be factored out from the existing integration test. If those helpers don't exist as named, look at the existing `TestOpenClawIngest_*` test in the file, factor the setup + read-back logic into the two helpers, then re-run.

- [ ] **Step 3: Factor the helpers from the existing integration test**

Look at the existing integration test body in `openclaw_integration_test.go` — specifically the code that sets up the `emit.Emitter`, tmp dir, and envelope template, and the code that reads back the JSONL output. Extract into two package-level helpers:

```go
func setupTestEmitter(t *testing.T) (*emit.Emitter, string, *event.Event) {
	t.Helper()
	// Replicate the existing setup — create tmp dir, SQLite chain.Index,
	// emit.Emitter, and a minimal valid envelope template with
	// SchemaVersion="2".
	// ...
	return em, dir, tmpl
}

func readEmittedEvents(t *testing.T, dir string) []*event.Event {
	t.Helper()
	// Replicate the JSONL-read logic from the existing integration test.
	// ...
	return events
}
```

Copy the exact setup code from the pre-existing SP-1 test; do not re-derive it — the intent is to share, not to rewrite.

- [ ] **Step 4: Run all tests in the package**

```bash
cd go/execution-kernel && go test ./internal/ingest/
```

Expected: all PASS, including new boundary tests.

- [ ] **Step 5: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/openclaw_integration_test.go
rtk git commit -m "SP-2 T11: integration + boundary tests (empty, mixed, sort, dedup, replay)"
```

---

## Task 12: CLI end-to-end golden test

**Files:**
- Modify: `go/execution-kernel/cmd/chitin-kernel/main_test.go`
- Create: `go/execution-kernel/cmd/chitin-kernel/testdata/sp2-mixed-fixture.pb`
- Create: `go/execution-kernel/cmd/chitin-kernel/testdata/sp2-golden-events.jsonl`
- Create: `go/execution-kernel/cmd/chitin-kernel/testdata/sp2-golden-quarantine-manifest.txt`

- [ ] **Step 1: Write the failing golden test**

Append to `go/execution-kernel/cmd/chitin-kernel/main_test.go`:

```go
func TestChitinKernelIngestOtel_SP2MixedFixture(t *testing.T) {
	fixture := loadTestdata(t, "sp2-mixed-fixture.pb")

	tmp := t.TempDir()
	fixturePath := filepath.Join(tmp, "fixture.pb")
	if err := os.WriteFile(fixturePath, fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	env := buildTestEnvelopeTemplate(t, tmp) // existing helper
	envPath := filepath.Join(tmp, "envelope.json")
	writeEnvelopeTemplate(t, envPath, env)

	cmd := exec.Command(chitinKernelBinary(t),
		"ingest-otel",
		"--from", fixturePath,
		"--dialect", "openclaw",
		"--envelope-template", envPath,
		"--events-dir", tmp,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ingest-otel: %v\n%s", err, out)
	}

	// Compare emitted events against golden.
	wantEvents := string(loadTestdata(t, "sp2-golden-events.jsonl"))
	gotEvents := readJSONL(t, filepath.Join(tmp, "events-"+env.RunID+".jsonl"))
	assertEventsEqual(t, wantEvents, gotEvents) // existing helper; ignores volatile fields like this_hash/prev_hash, asserts stable envelope+payload

	// Compare quarantine manifest.
	wantManifest := string(loadTestdata(t, "sp2-golden-quarantine-manifest.txt"))
	gotManifest := listQuarantineFiles(t, filepath.Join(tmp, "otel-quarantine"))
	if wantManifest != gotManifest {
		t.Fatalf("quarantine manifest mismatch:\nwant:\n%s\ngot:\n%s", wantManifest, gotManifest)
	}
}
```

If helpers like `loadTestdata`, `buildTestEnvelopeTemplate`, `assertEventsEqual`, `chitinKernelBinary`, `readJSONL`, `listQuarantineFiles` don't exist, check the existing SP-1 CLI test in the same file for the ones already defined and reuse them. Add missing ones with the minimum implementation needed.

- [ ] **Step 2: Generate the fixture**

Write a one-shot Go program at `go/execution-kernel/cmd/chitin-kernel/testdata/gen_sp2_fixture.go` that uses `buildFixture` (or copies its body — this is `//go:build ignore` code, so it may duplicate) to emit the same mixed fixture from Task 10 (one of each span type, plus one unmapped span for quarantine). Run it once to produce `sp2-mixed-fixture.pb`:

```bash
cd go/execution-kernel/cmd/chitin-kernel/testdata && go run gen_sp2_fixture.go sp2-mixed-fixture.pb
```

Commit both the generator and the `.pb` artifact.

- [ ] **Step 3: Run the test once to capture golden output**

Run the test; it will fail because the golden files don't exist yet. Capture the actual output and save as golden (review before committing):

```bash
cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestChitinKernelIngestOtel_SP2 -v
```

When it fails with a "golden missing" message, read the actual emitted events + quarantine manifest, inspect them for correctness (verify chain-id format, event-type, expected labels), and save as:
- `testdata/sp2-golden-events.jsonl` — 4 event lines (one per mapped span type).
- `testdata/sp2-golden-quarantine-manifest.txt` — one line per quarantine file in deterministic order.

- [ ] **Step 4: Re-run the test — it should now pass**

```bash
cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestChitinKernelIngestOtel_SP2
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add go/execution-kernel/cmd/chitin-kernel/main_test.go go/execution-kernel/cmd/chitin-kernel/testdata/
rtk git commit -m "SP-2 T12: CLI E2E golden test for mixed four-span-type fixture"
```

---

## Task 13: Verification gates + PR

**Files:**
- None (check + PR only)

- [ ] **Step 1: Run the full test suites**

```bash
cd go/execution-kernel && rtk go test ./...
pnpm -F contracts test
```

Expected: both PASS.

- [ ] **Step 2: Verify `buildChainID` is the single source of truth**

```bash
rtk grep '"otel:"' go/execution-kernel/internal/ingest/ --glob="*.go"
```

Expected: exactly one match, inside `buildChainID` in `openclaw.go`. If additional matches appear (stale inline assembly, tests except for the fixture-builder helper), fix them.

- [ ] **Step 3: Verify schema_version unchanged**

```bash
rtk grep -n 'SchemaVersion.*"2"\|schema_version.*"2"' go/execution-kernel/internal/ingest/ --glob="*.go"
```

Expected: every match uses `"2"` (no `"2.1"`, no `"v2"`, no `"v2.1"`).

- [ ] **Step 4: Verify every translator implements `TranslatedSpan`**

Every per-span file should have a `var _ TranslatedSpan = ...{}` compile-time assertion. Spot-check:

```bash
rtk grep -n 'var _ TranslatedSpan' go/execution-kernel/internal/ingest/
```

Expected: four matches — `ModelTurn`, `WebhookReceived`, `WebhookFailed`, `SessionStuck`.

- [ ] **Step 5: Create the PR**

```bash
rtk git push -u origin <your-branch-name>
rtk gh pr create --title "SP-2: complete openclaw translator (spans-only v1)" --body "$(cat <<'EOF'
## Summary
- Adds three chitin event types: `webhook_received`, `webhook_failed`, `session_stuck`.
- Extends the openclaw-dialect translator to cover every span type the plugin emits (`openclaw.model.usage`, `openclaw.webhook.processed`, `openclaw.webhook.error`, `openclaw.session.stuck`).
- Migrates `model_turn` chain-id to the uniform `otel:<trace>:<span>` scheme (resolves SP-1's multi-span-per-trace TODO at `openclaw.go:283-291`).
- Refactors the translator into per-span-type files with a polymorphic `TranslatedSpan` interface.
- Out of scope: metric instrument ingest (separate mini-spec), routing `session.stuck` directly to the governance ledger (future — needs ledger ingest API), push receiver (SP-3), cross-surface diffs (SP-4).

## Spec
`docs/superpowers/specs/2026-04-21-sp2-complete-openclaw-translator-design.md`

## Plan
`docs/superpowers/plans/2026-04-21-sp2-complete-openclaw-translator.md`

## Test plan
- [x] `go test ./...` green
- [x] `pnpm -F contracts test` green
- [x] CLI E2E golden test for mixed four-span-type fixture green
- [x] `grep "otel:"` returns exactly one match (single source of truth)
- [x] Every translator has `var _ TranslatedSpan = ...{}` compile-time assertion
- [x] Schema version stays at `"2"` across all emitted events

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 6: Request reviews and iterate**

Per chitin's PR flow:

1. Wait for Copilot review to finish.
2. Run adversarial review: `/review`.
3. Address every finding with follow-up commits.
4. Merge on green (all checks + both reviews clean).

Remember per memory notes: **Copilot review is a heuristic scanner — treat as noise-level signal; `/review` is the real review gate.**

- [ ] **Step 7: Commit any review-fix follow-ups as separate commits, then merge**

Do NOT amend commits after the PR is opened — always add new commits so the review trail stays readable.

---

## Final verification summary

When Task 13 finishes, these should all be true:

- 12 commits in the feature branch, one per task (T1–T12) plus Task 13's potential review-fix commits.
- 1 new directory of per-span test fixtures.
- 3 new chitin event types in Zod + Go.
- 1 migrated event type (`model_turn` chain-id).
- 1 new shared helper (`buildChainID`) used by every translator.
- 4 compile-time `TranslatedSpan` interface assertions.
- 1 CLI E2E golden test covering the full four-span-type pipeline.
- 0 production data migrations required (paper change — SP-1 dogfood gate deferred).
- 0 schema_version bumps.
