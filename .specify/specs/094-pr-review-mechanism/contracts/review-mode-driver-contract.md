# Contract: review-mode driver interface

**Spec reference**: FR-001, FR-002, FR-003 | **Research**: R-VTRANSPORT

## Driver-side responsibilities

A driver that declares `capabilities: [reviewer]` in its registry entry MUST also provide a `review_mode` entry in the registry shaped as:

```yaml
review_mode:
  tool_name: review              # the tool name the driver exposes for this contract
  prompt_template: "...prompt-content..."   # the driver's own prompt — opaque to the orchestrator (FR-003)
  max_bytes_in: 200000           # driver-self-declared limit on PRSnapshot bytes
```

The driver MUST expose a tool whose name matches `review_mode.tool_name`. That tool MUST be registered in the driver's `chitin-kernel`-gated tool registry — the gate evaluates every `review` invocation just like any other tool call. There is no review-specific bypass.

## Tool input schema

The orchestrator invokes the tool with one JSON object as input:

```json
{
  "pr": {
    "repo": "chitinhq/chitin",
    "number": 928,
    "title": "feat(speckit): deterministic spec-kit format linter",
    "body": "...PR body markdown...",
    "author": "jpleva91",
    "head_oid": "037d466260f9c1cc73eedb108ba41bd43dfc1702",
    "base_ref": "main"
  },
  "diff": [
    {
      "path": "go/execution-kernel/internal/speckit/lint.go",
      "additions": 287,
      "deletions": 0,
      "diff": "--- /dev/null\n+++ b/...\n@@ ...\n+package speckit\n..."
    }
  ],
  "spec_artifacts": [
    { "path": "specs/094-pr-review-mechanism/spec.md", "content": "..." },
    { "path": "specs/094-pr-review-mechanism/plan.md", "content": "..." }
  ],
  "policy_class_hint": "spec-only",
  "snapshot_captured_at": "2026-05-23T15:30:00Z",
  "max_bytes_in": 200000
}
```

**Field semantics**:

- `pr` — Identity and metadata; reviewer uses this to attribute discussion.
- `diff` — File-level diff hunks. Order is deterministic (alphabetical by `path`).
- `spec_artifacts` — Spec-kit files the PR is bound to. Empty list for non-spec PRs. Selection is the same set that informs spec 093's classification.
- `policy_class_hint` — The class the merge orchestrator assigned. Reviewer MAY consult it (e.g., to scope the rigor of review) but MUST NOT trust it as ground truth.
- `snapshot_captured_at` — Timestamp the orchestrator captured the snapshot.
- `max_bytes_in` — Echoes the driver's own `review_mode.max_bytes_in` — informational. If the orchestrator-computed input exceeds the limit, the orchestrator may truncate (and records a `truncated_input` flag in the activity result; the driver may treat truncation as cause for abstain).

## Tool output schema

The tool MUST return a JSON object conforming to [structured-verdict-schema.md](./structured-verdict-schema.md). No other top-level keys are permitted.

```json
{
  "verdict": "approve-with-comments",
  "concerns": ["The new lint rule may false-positive on legacy specs"],
  "recommendations": ["Add an opt-out marker for grandfathered specs"],
  "blockers": []
}
```

## Failure modes

| Driver behaviour | Orchestrator-side treatment |
|---|---|
| Tool returns a JSON document that fails JSON parse | `FailureMalformedJSON` |
| Tool returns valid JSON that fails schema validation (FR-014 invariants) | `FailureMalformedShape` |
| Tool exits with a non-zero error | `FailureError` |
| Tool does not return within the activity time bound (FR-026: 30 min default) | `FailureTimeout` |
| Workflow cancels the dispatch mid-flight (e.g., parent received re-review signal) | `FailureCancelled` |

All five failure modes are recorded as `ReviewerOutcome.Failure` and feed FR-009's arbiter-dispatch path identically to a verdict that doesn't agree with the other primary.

## Kernel gating

Every tool call the driver makes *while authoring* the verdict (reading the diff, opening files, fetching spec artifacts the driver wants to cross-reference, running compiler/test if a driver chooses) flows through `chitin-kernel gate evaluate` per the driver's normal configuration. The review-mode tool is registered in the driver's tool registry like any other tool; the gate-evaluation hook is not bypassed.

This is the §7 mandate restated for this contract: the orchestrator dispatches the reviewer; the reviewer's drive is itself kernel-gated. The review-mode surface is not a kernel-bypass channel.

## Prompt template — orchestrator non-prescription

Per FR-003, the orchestrator does NOT prescribe the `prompt_template` content. The driver author owns the prompt and can iterate on it without coordinating with the orchestrator team. The only orchestrator requirement is that the input/output contract above is honored.

In practice, a well-formed `prompt_template` will:

- Instruct the model to read the PR carefully (no shortcut to verdict).
- Anchor on the spec_artifacts as the authority for "what does this PR claim to do."
- Explicitly call out the four-verdict vocabulary and the FR-014 invariants.
- Provide a deterministic output template the model can fill in (e.g., a fenced YAML or JSON block).

Concrete prompt template choices for `hermes` and `openclaw` are tracked outside this spec (in those drivers' own repos / registry entries).
