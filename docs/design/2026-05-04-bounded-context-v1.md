---
date: 2026-05-04
status: design — pending operator answers on §10 open questions

> **Post-cull note (2026-05-08):** This design proposes a kernel extension (content-addressed tool-output storage). The concept is valid but implementation is deferred per the scope narrowing (`docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md`). References to `apps/runner` are stale (deleted in the cull).

audience: operator + future agents picking up bounded-context work
purpose: Crystallize the shape of chitin's bounded-context layer —
  event schema, storage model, policy logic, and MVP slicing — so
  implementation work can land in clean PRs against a stable design.
supersedes: nothing — first pass.
related:
  - project memory `project_bounded_context_roadmap.md` (the strategic
    framing this doc concretizes)
  - project memory `project_bounded_context_v1_schema.md` (the
    canonical reference; this doc is its long form)
---

# Bounded context v1

## 1. Why this doc exists

Chitin sits at the gate. Today it sees PreToolUse hook fires and
makes deterministic policy decisions; tool *outputs* return directly
to the agent without chitin ever seeing them. That works fine until
sessions hit the 47-min context wall — long-running agents drown in
their own transcript and either compact lossily (Anthropic's Auto
Compact) or hit hard caps. The proxy-hack ecosystem (RTK, Caveman
Claude, Token Optimizer MCP) papers over this at the wrong layer.

Chitin is the right layer. The kernel can intercept tool outputs at
the same boundary it gates inputs, store them content-addressed,
return summary+ref to the agent, and emit OTEL projections — all
while keeping the hash-linked event chain canonical and replay-safe.

## 2. Core invariant

> Full raw tool output is never sent directly back to the agent
> unless policy allows it.

Pipeline:

```
raw tool output
  → persisted blob (content-addressed; sha256-keyed)
  → hash-linked chain event (manifest of refs, not bytes)
  → bounded summary/ref returned to agent
  → OTEL span emitted asynchronously
```

The event chain is canonical. OTEL is projection only.

## 3. Event schema

All four event types share the v2 chain header
(`chain_id`/`event_id`/`prev_hash`/`this_hash`) and join the existing
event union in `libs/contracts/src/event.schema.ts`.

### 3.1 `tool_output_captured`

```ts
{
  event_type: "tool_output_captured",
  chain_id: string,
  event_id: string,
  prev_hash: string,
  this_hash: string,
  session_id: string,
  tool_call_id: string,
  tool_name: string,
  output_ref: string,          // chitin://blob/sha256:...
  output_sha256: string,
  output_bytes: number,
  estimated_tokens: number,
  captured_at: string          // ISO-8601 UTC
}
```

**Purpose:** immutable proof that chitin saw the full output. Always
emitted, even when the policy decision is `pass_full`.

### 3.2 `tool_output_policy_decision`

```ts
{
  event_type: "tool_output_policy_decision",
  chain_id: string,
  event_id: string,
  prev_hash: string,
  this_hash: string,
  tool_call_id: string,
  policy_id: "bounded_context_v1",
  decision: "pass_full" | "summarize" | "truncate" | "deny",
  reason: string,
  input_tokens_estimate: number,
  max_allowed_tokens: number
}
```

**Purpose:** deterministic governance decision. The reason field is
free-form text; the `decision` enum is the audit-stable signal.

### 3.3 `tool_output_summary_created`

```ts
{
  event_type: "tool_output_summary_created",
  chain_id: string,
  event_id: string,
  prev_hash: string,
  this_hash: string,
  tool_call_id: string,
  output_ref: string,
  summary_ref: string,         // chitin://summary/sha256:...
  summary_sha256: string,
  summary_tokens: number,
  summarizer: {
    mode: "heuristic" | "llm" | "extractive",
    model?: string,            // when mode=llm
    prompt_hash?: string       // when mode=llm — stable hash of the
                               // summarizer prompt template
  }
}
```

**Replay-safety split** (load-bearing):

- `mode: "heuristic"` / `"extractive"` → byte-deterministic. Replay
  produces the same summary.
- `mode: "llm"` → not byte-deterministic. The `prompt_hash` + `model`
  fields make the result *explainable*: a future operator can
  re-summarize with the same prompt + model and verify that the
  current output is in the same family. This is weaker than strict
  determinism but stronger than "we summarized, ¯\\_(ツ)_/¯".

### 3.4 `agent_visible_output_emitted`

```ts
{
  event_type: "agent_visible_output_emitted",
  chain_id: string,
  event_id: string,
  prev_hash: string,
  this_hash: string,
  tool_call_id: string,
  visible_payload: {
    kind: "full" | "summary_ref" | "truncated" | "denied",
    text: string,
    refs: string[]
  },
  visible_tokens: number
}
```

**Purpose: forensic anchor.** This event records *exactly* what the
agent saw. Without it, a replay can only know what the agent *could*
have seen given the policy — not what was actually returned. Critical
for debugging "why did the agent do X" weeks after the fact.

## 4. Storage model

SQLite first. Postgres compatible.

```sql
CREATE TABLE blobs (
  ref TEXT PRIMARY KEY,        -- chitin://blob/sha256:...
  sha256 TEXT NOT NULL,
  bytes INTEGER NOT NULL,
  content_type TEXT NOT NULL,  -- text/plain, application/json, etc.
  created_at TEXT NOT NULL,
  payload BLOB NOT NULL
);

CREATE TABLE summaries (
  ref TEXT PRIMARY KEY,        -- chitin://summary/sha256:...
  source_ref TEXT NOT NULL,    -- the blob it summarizes
  sha256 TEXT NOT NULL,
  tokens INTEGER NOT NULL,
  mode TEXT NOT NULL,          -- heuristic | llm | extractive
  payload TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE token_usage (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  tool_call_id TEXT,
  phase TEXT NOT NULL,         -- prompt | tool_input | tool_output |
                               -- model_output | cumulative
  input_tokens INTEGER,
  output_tokens INTEGER,
  cumulative_tokens INTEGER,
  created_at TEXT NOT NULL
);
```

The chain is the manifest of refs. The blob/summary stores are
source-of-truth for bytes. Bytes are content-addressed (sha256-
keyed), so a tampered blob fails verification at retrieval —
replay-safety is preserved without putting bytes inside the chain.

## 5. Policy logic

```typescript
function handleToolOutput(output, ctx) {
  const outputRef = blobStore.write(output);
  emit("tool_output_captured", metadata(outputRef, output));

  const tokens = estimateTokens(output);
  if (tokens <= ctx.policy.maxToolOutputTokens) {
    emit("tool_output_policy_decision", { decision: "pass_full" });
    return emitVisible("full", output);
  }

  if (ctx.policy.allowSummarization) {
    const summary = summarize(output);
    const summaryRef = summaryStore.write(summary);
    emit("tool_output_policy_decision", { decision: "summarize" });
    emit("tool_output_summary_created", { outputRef, summaryRef });
    return emitVisible("summary_ref", {
      text: summary,
      refs: [outputRef, summaryRef],
    });
  }

  const truncated = truncate(output, ctx.policy.maxToolOutputTokens);
  emit("tool_output_policy_decision", { decision: "truncate" });
  return emitVisible("truncated", truncated);
}
```

## 6. Default policy

```yaml
# bounded_context block in chitin.yaml (proposed; see §10 open Q4)
bounded_context:
  policy_id: bounded_context_v1
  max_tool_output_tokens: 1200
  max_stdout_tokens: 800
  max_stderr_tokens: 1200
  max_diff_tokens: 2000
  always_store_raw_output: true
  allow_summarization: true
  allow_llm_summary: false       # heuristic-only by default
  pass_full_for:
    - test_failure_summary
    - compiler_error_short
    - git_status
    - file_read_small
  summarize_for:
    - test_logs
    - build_logs
    - github_issue_list
    - search_results
    - large_file_read
  deny_or_require_human_for:
    - secret_like_output
    - credential_dump
    - large_env_print
```

Heuristic-only first. LLM summaries land later behind
`allow_llm_summary: true`.

## 7. Agent-visible format

When the policy returns `summary_ref`, the agent sees:

```
Tool output exceeded context budget.
Summary:
- Build failed in packages/kernel.
- Primary error: missing field `OutputRef` in ToolOutputCapturedEvent.
- Affected files:
  - internal/events/tool_output.go
  - internal/policy/bounded_context.go
- Full output stored at: chitin://blob/sha256:abc123...
- Summary stored at: chitin://summary/sha256:def456...
Use `chitin inspect chitin://blob/sha256:abc123` only if full logs
are required.
```

Preserves utility (the agent can still call `chitin inspect` to fetch
the full bytes when it genuinely needs them) without flooding context
on the default path.

## 8. Session compaction

Compaction is a separate event type, not a tool-output flow. Triggered
by token threshold, step count, or manual hook (see §10 open Q6 on
trigger semantics).

```ts
{
  event_type: "session_compacted",
  chain_id: string,
  event_id: string,
  prev_hash: string,
  this_hash: string,
  session_id: string,
  compacted_range: {
    from_event_id: string,
    to_event_id: string
  },
  retained_state: {
    goal: string,
    decisions: string[],
    open_tasks: string[],
    files_touched: string[],
    known_failures: string[],
    next_actions: string[]
  },
  raw_events_preserved: true,   // invariant — never false in v1
  summary_ref: string
}
```

**Compaction does NOT delete history.** It creates a replayable
checkpoint that points at the range it summarizes. Raw events stay
in the chain; the agent's working context shifts to consume the
summary while replay continues to have access to the full timeline.

## 9. OTEL projection

```
span.name = "chitin.tool_output"
span.attributes = {
  "chitin.session_id": session_id,
  "chitin.tool_name": tool_name,
  "chitin.decision": "summarize",
  "chitin.raw_tokens": 18420,
  "chitin.visible_tokens": 612,
  "chitin.reduction_ratio": 0.967,
  "chitin.output_ref": output_ref
}
```

Asynchronous; never source of truth. Same projection model as F4's
existing chain → OTEL bridge.

## 10. Open questions (operator answers needed before Slice 1 ships)

The schema above is the contract. These six questions affect the
implementation slicing — none invalidate the design, but each
narrows code shape.

**Q1 — PostToolUse interception is net-new.** Today chitin only sees
PreToolUse hook fires. Bounded-context requires a PostToolUse handler
in every driver:
- Claude Code: `PostToolUse` hooks (settings.json hooks union)
- Codex: PostToolUse equivalent in 0.128.0+ (we verified PreToolUse
  byte-compat earlier)
- Gemini: `AfterTool` hook
- openclaw: `after_tool_call` plugin path

**Should the MVP ship Claude-Code-only first, then add codex/gemini/
openclaw in sequence?** That's how PreToolUse landed.

**Q2 — `tool_call_id` threading.** Each driver exposes a tool-call
identifier in its hook payload (Claude's `tool_use_id`, codex's
`tool_call_id`, gemini's `id`). They need to be stable across the
Pre→Post pair so we can correlate `tool_output_captured` to the
matching gate decision. **Add a normalizer test that asserts the id
round-trips per driver?**

**Q3 — Token estimator.** Without per-vendor tokenizers (which would
require shipping `tiktoken`/etc), we'd use byte-count÷4 or word-count
×1.3. That's good enough for *policy gating* (which needs ordinal
correctness, not precision) but **dangerously wrong for cost
attribution** if anyone treats `estimated_tokens` as ground truth.
Tag the field as `_estimate` and keep it distinct from
ground-truth tokens that come back from each provider's response
(claude's `usage`, codex similar)?

**Q4 — Policy YAML location.** Goes as a sibling of `bounds:` in
`chitin.yaml`, or as a sub-block? Existing `bounds:` is action-blast-
radius (max_files_changed, max_lines_changed); bounded-context is
action-output-size — same family, different verb. Lean toward sibling,
top-level `bounded_context:` block. **Confirm?**

**Q5 — `policy_id: "bounded_context_v1"` versioning.** When v2 ships,
do v1 chains stay readable? omitempty + hash-stable schema +
content-addressed refs *should* make this safe — old chains read
fine, new readers handle both versions — but make explicit in the
design. **Add a `chitin chain verify --policy-version-tolerant` flag
for migration story?**

**Q6 — `session_id` vs `chain_id`.** Today's v2 events use `chain_id`
as the hash-link key. The new events have both. **Are they the same
thing for tool-output events, or is `session_id` the agent-session
(one Claude Code run) and `chain_id` the chitin-chain (currently 1:1)?**
Worth pinning before code so semantics don't drift.

## 11. MVP slicing

7 distinct PRs, each independently shippable. Build order:

| Slice | Scope | Estimated LOC |
|---|---|---|
| **1** | Token estimator + blob store + `tool_output_captured` event + Claude Code PostToolUse handler + `chitin inspect <ref>` CLI | ~600 |
| 2 | Apply max-output policy (no summarization yet — pass-full or truncate) + `tool_output_policy_decision` event | ~250 |
| 3 | Heuristic summarizer + `tool_output_summary_created` event | ~400 |
| 4 | `agent_visible_output_emitted` event + agent-visible format renderer | ~200 |
| 5 | Session compaction event + trigger logic | ~500 |
| 6 | OTEL projection of tool_output spans | ~150 |
| 7 | Codex + Gemini + openclaw PostToolUse integration | ~400 |

**Slice 1 is the critical-path shipping unit.** Lands the storage,
event type, and capture path. Subsequent slices add bounded I/O on
top of the same plumbing.

## 12. Slice 1 acceptance

- [ ] `libs/contracts/src/event.schema.ts` adds `tool_output_captured`
      to the v2 union with the documented field set
- [ ] `go/execution-kernel/internal/blobs/` package: `Write(payload)
      → ref` and `Read(ref) → bytes` against SQLite blobs table
- [ ] `apps/runner/...` Claude Code PostToolUse handler
      fires `tool_output_captured` for every tool return
- [ ] `chitin-kernel inspect <ref>` retrieves bytes from the blob
      store; exits non-zero on missing/corrupted ref
- [ ] sha256 verification on retrieval: tampered blob fails the read
      with a clear error
- [ ] No bounded I/O yet — tool outputs still pass-through to the
      agent unmodified. Capture-only.
- [ ] Operator can grep the chain for `tool_output_captured` events
      after a Claude Code session and see one per tool call
- [ ] go test ./... + pnpm exec nx run-many --target=test all green

## 13. Product framing

> Chitin turns agent context from an unbounded transcript into a
> governed execution record.

Token optimizers save money. Chitin preserves control, replay, and
long-running agent viability. That's the line.

The closest comparable in the ecosystem is RTK (proxy-hack
truncation). RTK works at the wrong layer — the agent already saw
the bytes; RTK strips them post-hoc from the model context. Chitin
intercepts at the gate boundary before the agent ever sees the
output, with full audit trail and operator-tunable policy. Different
property set; different correctness story.

## 14. Anti-scope

What this design explicitly does NOT include:

- **LLM-judge summarizers in Slice 1–4.** Heuristic only until v2.
- **Tool-result caching by hash(input).** Filed earlier as a Phase 4
  follow-up; out of scope here.
- **Compression / dictionary encoding** of stored blobs. SQLite's
  default page compression (or no compression) is fine for v1.
- **Cross-session memory pooling.** Blobs are session-scoped; no
  global content store.
- **Auto-summarizer training loops.** The summarizer is fixed code
  in v1; learning lands on the routing-as-learning-system rails
  (P3 outcomes table can include summarizer success metrics later).
