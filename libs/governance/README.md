# @chitin/governance

The policy SDK + decision substrate for chitin's tool-call adjudication
layer.

**Status:** Slice 1 — substrate only. No ingress wiring yet (no Claude
Code hook, no openclaw plugin extension). Those land in Slice 1.5.

**Spec:** `docs/superpowers/specs/2026-05-03-predictive-execution-policy-design.md`

## What's here

```
src/
├── tool-call-request.schema.ts    # ToolCallRequest — the canonical
│                                  # tool-call adjudication contract
├── semantic-envelope.schema.ts    # SemanticEnvelope — derived from raw call
├── blast-vector.schema.ts         # BlastVector — 4-axis blast description
├── decision.schema.ts             # Decision — the 7-decision space
├── classifier.ts                  # classify() — C1 deterministic table
├── decide.ts                      # decide() — synchronous policy eval
└── index.ts                       # barrel
```

## What this is not (yet)

Slice 1 is the **substrate**, not the product. To prove the abstraction
is real, two more pieces must land:

- A Claude Code `PreToolUse` hook script that wraps `decide()`.
- An extension to `apps/openclaw-plugin-governance` that calls
  `decide()` from its `before_tool_call` hook.

Both share *one* decision path. That's the load-bearing claim of
Slice 1: one canonical contract, one synchronous decision call, two
ingress paths sharing one adjudicator. The substrate has to be solid
before either ingress lands.

Subsequent slices add: `BlastVector` calibration loop (Slice 2),
`allow_with_auto_undo` (Slice 3), tiered advisor (Slice 4), intent
layer + drift (Slice 5), trust calibration (Slice 6), human escalation
(Slice 7), counterfactual replay (Slice 8). See spec §12 for the full
slice ordering.

## Naming note: ToolCallRequest vs ExecutionRequest

`libs/contracts/src/execution-request.schema.ts` already defines
`ExecutionRequest` for the **workflow dispatch** layer (what task should
this agent run?). `@chitin/governance`'s `ToolCallRequest` is at a
different layer: **tool-call-level adjudication** (should this specific
tool call be allowed?). Different concerns, deliberately distinct names.

## Usage (planned, when ingress wires up in Slice 1.5)

`classify()` and `decide()` are deliberately separate so the chain
records both the classifier output and the decision. Slice-1's API is
two steps; the SDK shape in Slice 1.5 will likely fold them into a
single `adjudicate()` for ergonomics, while keeping the underlying
two-step pipeline available for callers that want the intermediate
envelope (e.g., to emit a separate chain event for the classification).

```ts
import { classify, decide, type ToolCallRequest } from '@chitin/governance'

// At a Claude Code PreToolUse hook handler:
const cls = classify({
  ingress: 'claude_code_pretooluse',
  tool_name: hookInput.tool_name,             // e.g., 'Bash'
  tool_args: hookInput.tool_input,            // e.g., { command: '...' }
})

const request: ToolCallRequest = {
  schema_version: '1',
  request_id:    crypto.randomUUID(),         // ULID also acceptable
  session_id:    hookInput.session_id,
  agent_id:      'claude-code',
  ingress:       'claude_code_pretooluse',
  tool_name:     hookInput.tool_name,
  tool_args:     hookInput.tool_input,
  semantic_envelope:     cls.envelope,
  blast_vector:          cls.blast_vector,
  classifier_confidence: cls.confidence,
  classifier_version:    cls.classifier_version,
}

const decision = await decide(request)
// decision.kind: 'allow' | 'deny' | 'rewrite' | 'redirect' | ...
```
