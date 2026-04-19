# Design — Phase 1.5: Observability Chain Contract

**Date:** 2026-04-19
**Status:** Approved for implementation
**Phase:** 1.5 (observability surface expansion)
**Supersedes:** v1 event contract (broken, no migration)

## Problem Statement

Phase 1 shipped a Claude Code capture→replay loop built around a flat "one event per tool call" model, captured only via `PreToolUse`. Two gaps surfaced in review that collapse the planned Phase-1.5 work (second surface: OpenClaw + Ollama-local) and a smaller Phase-1 patch (close the PreToolUse-only gap by adding PostToolUse) into one project:

1. **Pre/Post is a Claude-Code-specific artifact.** Other surfaces (OpenClaw, Ollama-local, Copilot, GitHub Actions, CI/CD) expose different points in the tool-call lifecycle. The schema cannot hardcode "Pre" and "Post" if it wants to be surface-neutral.

2. **Governance (Phase 2) needs a chain, not a pair.** When a future policy layer blocks or rewrites a dangerous call, the log has to prove: *this is what the agent asked for → this is what policy decided → this is what actually ran*. That's 2–4 linked events, not one enriched event.

Further, reasoning-layer observability (user prompts, assistant turns, thinking, token usage) is achievable today on all three target surfaces — Claude Code via hooks + transcript tail, Ollama-local via its HTTP API, Anthropic SDK via `thinking` blocks — and should land as part of the same contract rather than as a follow-up.

## Goals

- **Surface-neutral event contract.** Same envelope across Claude Code, OpenClaw, Ollama-local, and any future surface. Per-surface detail lives in payload + `surface` + `driver_identity`, never in the envelope shape.
- **Full observability at both tool-call and reasoning layers.** Capture every user prompt, every assistant turn (text + thinking + token usage), every tool call (intention + execution + duration + result), every compaction, every subagent spawn, every session boundary.
- **Tamper-evident replay.** Per-chain hash linkage now; global chain and signed credentials reserved for Phase 2.
- **Sidecar-ready substrate.** `.chitin/events-<run_id>.jsonl` declared the stable public interface; SQLite indexer is the reference consumer pattern. Cloud / dashboard / alerting sidecars enabled (Phase 3) without further schema work.
- **Uniform instrumented-launch UX.** `chitin run <surface>` wraps every agent surface with one command; the session lifecycle is owned end-to-end by the wrapper.

## Non-Goals (explicitly deferred)

- Cloud / Postgres / Neon shipper — Phase 3.
- Web dashboard server — Phase 3.
- Alerting rules engine — Phase 3.
- Cross-machine aggregation — Phase 3.
- Policy engine + governance enforcement + `chain verify` implementation — Phase 2 (fields land now; verification tool later).
- Ed25519 signatures / W3C DIDs / Verifiable Credentials — Phase 2/3 (fingerprint field is hash-only; upgrade path preserved).
- Live reasoning streaming (Approach 2) — whenever a real consumer needs it; drop-in upgrade from Approach 1.
- Global (cross-chain) hash chain — Phase 2 if governance needs end-to-end integrity.
- Tool-call chains on OpenClaw and Ollama-local — Phase 2 (Phase 1.5 captures session chain only for these surfaces).
- In-process resource sampling (CPU, RSS, GPU) — Phase 2+.

## Architecture Overview

```
chitin run <surface>                             user-facing CLI (TS)
   │
   ├─ chitin-kernel init / sweep-transcripts     idempotent setup
   ├─ chitin-kernel install-hook                 session-scoped overlay (Claude Code)
   ├─ spawn <surface>, wait for exit             TS orchestration
   │     │
   │     ├─ [surface-specific hooks / IO]
   │     │     └─ libs/adapters/<surface> (TS, thin forwarder)
   │     │           └─ chitin-kernel hook / emit  (Go, owns all side effects)
   │     │                 ├─ canon / normalize / classify
   │     │                 ├─ compute seq + prev_hash + this_hash
   │     │                 └─ append to .chitin/events-<run_id>.jsonl
   │     │                       └─ chain_index.sqlite keeps (chain_id → last_seq, last_hash)
   │     │
   │     └─ (on SessionEnd) chitin-kernel ingest-transcript → emit assistant_turn events
   │
   ├─ chitin-kernel uninstall-hook               cleanup
   └─ done

                        ┌─────────────────────────────────┐
                        │ .chitin/events-<run_id>.jsonl   │  ◄── stable public interface
                        └─────────────────────────────────┘
                                     │
                            (tail + project; read-only)
                                     │
              ┌──────────────────────┼──────────────────────────┐
              │                      │                          │
         SQLite indexer       [future] Neon shipper    [future] alerting sidecar
         (reference sidecar, ships in 1.5)        (Phase 3)        (Phase 3)
```

**Hard rule:** only the Go execution kernel writes to the **canonical event log** (`.chitin/events-*.jsonl`) and kernel-owned indexes (`chain_index.sqlite`, `transcript_checkpoint.json`, session overlays). Sidecars (Section 7) — including the Phase-1 SQLite indexer in `libs/telemetry` — may write their own derived state (a local DB, a remote API, an alert store) as long as they only *read* the canonical JSONL. TS adapters (input side) never write to the filesystem directly; they route through kernel subcommands.

---

## Section 1 — Event schema v2 (the contract)

Every event carries an identical envelope plus a typed payload. The envelope is what makes events surface-neutral, chain-linkable, and hash-verifiable.

### Envelope (every event)

| Field | Type | Purpose |
|---|---|---|
| `schema_version` | string | `"2"` today. Sidecars MAY refuse unknown versions. |
| `run_id` | UUID | Per capture run (one per process invocation of `chitin run` or per hook-mode session start). |
| `session_id` | UUID | Per agent session. Shared across all agent instances (main + subagents) in that session. |
| `surface` | enum | `claude-code \| openclaw \| ollama-local \| copilot \| github-actions \| ...` (open vocabulary). |
| `driver_identity` | object | `{ user: string, machine_id: string, machine_fingerprint: sha256-hex }` — who is operating the agent. Supersedes v1's vague `driver` string. |
| `agent_instance_id` | UUID | Unique per agent instance within a session. Main agent gets one; each subagent gets its own. Supersedes v1's loose `agent_id`. |
| `parent_agent_id` | UUID \| null | Set on subagent events; null for the main agent. |
| `agent_fingerprint` | sha256-hex | Composite hash naming this agent *configuration* (see below). Reproducibility identity. |
| `event_type` | string (open vocab) | See [Event Type Vocabulary](#event-type-vocabulary). |
| `chain_id` | string (opaque) | Groups events belonging to one chain. UUID for chains we mint; Claude Code's `tool_use_id` for tool-call chains (which looks like `toolu_01ABC…`, not a UUID). Stable and unique; format is surface-defined. |
| `chain_type` | enum | `session \| tool_call`. |
| `parent_chain_id` | string (opaque) \| null | Chain nesting: tool-call chain → parent session chain; subagent session chain → parent tool-call chain; root session chain → null. Same format as `chain_id`. |
| `seq` | integer | Ordinal within the chain. **Emission order**, not wall-clock order. See [Section 3 invariant](#seq-vs-ts). |
| `prev_hash` | sha256-hex \| null | Hash of prior event in the same `chain_id`. null if `seq == 0` (chain head). |
| `this_hash` | sha256-hex | Hash of this event record (canonical JSON, `this_hash` itself excluded from input). |
| `ts` | ISO-8601 UTC | Wall-clock timestamp of the event (original — for hook-fired events, when the hook fired; for transcript-ingested events, the transcript message's timestamp). |
| `labels` | `map<string, string>` | Freeform user + adapter metadata. Adapter defaults: `cwd`, `git_branch`, `git_sha_at_session_start`, CLI version. User-set via `--label k=v` or `CHITIN_LABELS` or project-local `.chitin/labels.json`. Stored as JSON column; indexed on demand. |
| `payload` | object | Shape depends on `event_type`. See [Payload Shapes](#payload-shapes). |

### Agent Fingerprint

`agent_fingerprint` is a single SHA-256 hex that uniquely names an agent **configuration** (not instance). Composite hash over:

- `model` — `{ name, provider, version }`
- `system_prompt_hash` — sha256 of the active system prompt
- `tool_allowlist_hash` — sha256 of the enabled-tool set (sorted + canonicalized)
- `soul_hash` — sha256 of the active soul/archetype config (if any); omitted from hash input if none
- `agent_version` — surface's own version (e.g., Claude Code CLI version)

Two events with matching `agent_fingerprint` are reproducibly comparable. Two with differing fingerprints are not — forensically important when comparing traces across surfaces or config changes.

The fingerprint's *components* land in the `session_start` payload so a fingerprint value can be decoded back to its inputs without re-running the agent.

### Hash Mechanics

- **Canonical JSON:** keys sorted lexicographically, no whitespace, UTF-8, ISO-8601 string timestamps.
- **`this_hash`** = SHA-256 hex of canonical JSON serialization of the event record with `this_hash` field itself excluded from input.
- **`prev_hash`** = the previous event's `this_hash` within the same `chain_id`, or null if `seq == 0`.
- **Per-chain hashing, not global.** `prev_hash` only references events in the same chain. Global cross-chain hash deferred to Phase 2.
- **Verification (`chitin chain verify`):** walks each chain ordered by `seq`; recomputes each `this_hash`; confirms `prev_hash` linkage. **Reserved for Phase 2**; fields land in v2 so verification can light up without backfill.

### Event Type Vocabulary

**Session chain** (`chain_type == "session"`):
- `session_start` — head of the session chain.
- `user_prompt` — a user turn (captured live from UserPromptSubmit hook on Claude Code; from stdin capture on Ollama-local).
- `assistant_turn` — an assistant turn (text + optional thinking + usage). Captured from transcript on Claude Code; from stdout + API on Ollama-local.
- `compaction` — context compaction boundary (Claude Code `PreCompact`).
- `session_end` — tail of the session chain. Always the last event in its chain by `seq`.

**Tool-call chain** (`chain_type == "tool_call"`):
- `intended` — head. What the agent asked for (Claude Code `PreToolUse`).
- `executed` — tail on success. Duration, result, output preview.
- `failed` — tail on error. Duration, error kind, error message.

**Reserved for Phase 2** (documented in schema, not emitted yet):
- `policy_decided` — policy engine weighed in.
- `rewritten` — tool call was modified before execution.
- `denied` — tool call blocked.

**Subagent sessions** use `session_start` / `session_end` with `parent_agent_id != null` and `payload.spawning_tool_call_id` set — no dedicated event types needed.

### Payload Shapes

#### `session_start`
```
{
  cwd: string,
  client_info: { name: "claude-code" | "ollama" | ..., version: string },
  model: { name: string, provider: string, version?: string, context_window?: int },
  system_prompt_hash: sha256-hex,
  tool_allowlist_hash: sha256-hex,
  soul_id?: string,                    // short name like "jokic"; present if a soul is active
  soul_hash?: sha256-hex,              // verifying hash of the soul config
  agent_version: string,               // surface version (e.g., Claude Code CLI version)
  spawning_tool_call_id?: string,      // present iff this is a subagent session; links to parent tool_call chain (same format as chain_id)
  task_description?: string            // present for subagents: the prompt from parent
}
```

#### `user_prompt`
```
{ text: string, attachments?: [{ kind, path | data }] }
```

#### `assistant_turn`
```
{
  text: string,
  thinking?: string,
  model_used: { name: string, provider: string, version?: string },
  usage: {
    input_tokens: int,
    output_tokens: int,
    cache_creation_input_tokens?: int,
    cache_read_input_tokens?: int,
    thinking_tokens?: int
  },
  ts_start: ISO-8601,
  ts_end: ISO-8601
}
```

#### `compaction`
```
{
  reason: string,
  pre_token_count?: int,
  post_token_count?: int,
  summary?: string
}
```

#### `session_end`
```
{
  reason: "clean" | "subagent_stop" | "orphaned_sweep" | "transcript_read_error" | string,
  totals: {
    turn_count: int,
    tool_call_count: int,
    total_input_tokens: int,
    total_output_tokens: int,
    total_duration_ms: int
  }
}
```

#### `intended`
```
{
  tool_name: string,
  raw_input: object,                   // verbatim tool input from the agent
  canonical_form?: object,             // produced by Go canon package (shell commands)
  action_type: "read" | "write" | "exec" | "git" | "net" | "dangerous"
}
```

#### `executed`
```
{
  duration_ms: int,
  output_preview?: string,              // first N chars of stdout or similar
  output_bytes_total?: int
}
```
(No `result` field: `executed` *is* the success case by event_type. Errors use `failed`. Phase 2's denials get their own event type.)

#### `failed`
```
{
  duration_ms: int,
  error_kind: string,                   // "timeout" | "non_zero_exit" | "internal" | ...
  error: string,
  output_preview?: string
}
```

---

## Section 2 — Chain composition and linkage

Every event belongs to exactly one chain (identified by `chain_id`). Chains nest through `parent_chain_id`. Events within a chain are totally ordered by `seq` and hash-linked by `prev_hash`.

### Chain types and nesting

- **Root session chain** — `chain_type=session`, `parent_chain_id=null`. One per top-level agent instance. Head: `session_start`. Tail: `session_end`.
- **Tool-call chain** — `chain_type=tool_call`, `parent_chain_id` = the session chain it was initiated in. Head: `intended`. Tail: `executed` or `failed`. `chain_id` = Claude Code's `tool_use_id` for that invocation.
- **Subagent session chain** — structurally identical to a root session chain, but `parent_chain_id` = the tool-call chain that spawned it (the parent's `Task` invocation), and `parent_agent_id` = the spawning agent's id. Distinguished by `parent_agent_id != null` + `payload.spawning_tool_call_id` set.

### Example trace — main agent spawns a subagent that reads a file

```
[chain S1 / session / parent=null / agent=A1]
  seq=0  session_start            (prev=null)
  seq=1  user_prompt "do X"
  seq=2  assistant_turn (calls Task)
  seq=3  session_end               ← final event of main session

[chain T1 / tool_call / parent=S1 / agent=A1]
  seq=0  intended (Task)           (prev=null)
  seq=1  executed                  (duration_ms=12500)

[chain S2 / session / parent=T1 / agent=A2 / parent_agent_id=A1]
  seq=0  session_start
         payload.spawning_tool_call_id = T1
  seq=1  user_prompt (task desc from main)
  seq=2  assistant_turn
  seq=3  session_end

[chain T2 / tool_call / parent=S2 / agent=A2]
  seq=0  intended (Read)
  seq=1  executed                  (duration_ms=5)
```

### Replay tree reconstruction (three queries)

```sql
-- Root sessions
SELECT chain_id FROM events WHERE chain_type='session' AND parent_chain_id IS NULL;

-- Children of a chain (tool-call chains in a session; subagent sessions under a tool call)
SELECT DISTINCT chain_id FROM events WHERE parent_chain_id = :id;

-- Events in a chain, ordered
SELECT * FROM events WHERE chain_id = :id ORDER BY seq ASC;
```

### Parallel tool calls

Claude Code may issue multiple tool calls within one assistant turn (parallel tool use). Each gets its own `chain_id`, all with the same `parent_chain_id` (the session), potentially overlapping in wall-clock time. The chain model handles this natively — chains are self-contained, ordering within each is well-defined.

### Design decisions flagged

1. **Per-chain hash, not global.** Simpler; sufficient for Phase 1.5. Phase 2 can add an envelope-level global chain if governance needs end-to-end integrity.
2. **No forward references.** A parent's `executed` event does not denormalize the list of spawned subagent chains. Consumers query on `payload.spawning_tool_call_id`.
3. **`session_start` / `session_end` reused for subagents.** `parent_agent_id` + `spawning_tool_call_id` carry the disambiguation. Fewer event types.

---

## Section 3 — Claude Code adapter: wrapper + hook modes

### Adapter contract (applies to every surface)

Thin TS layer in `libs/adapters/<surface>`. Receives surface-native events, normalizes them, invokes `chitin-kernel hook` or `chitin-kernel emit` with the normalized payload. No filesystem writes, no process spawning — the kernel owns both.

### Claude Code hook → `event_type` mapping

| Hook | Emits | Notes |
|---|---|---|
| `SessionStart` | `session_start` | envelope filled with model, soul, fingerprint, labels |
| `UserPromptSubmit` | `user_prompt` | |
| `PreToolUse` | `intended` | opens a new tool_call chain keyed by Claude's `tool_use_id` |
| `PostToolUse` | `executed` or `failed` | closes the tool_call chain with same `tool_use_id` |
| `PreCompact` | `compaction` | |
| `SubagentStop` | `session_end` (for the subagent) | closes the subagent's session chain |
| `SessionEnd` | triggers transcript ingest, **then** emits `session_end` for the main session | see `seq` invariant below |

The `Stop` hook is **not** subscribed — it fires per turn-complete but that data comes from the transcript.

`assistant_turn` events come from transcript ingest (no hook exposes them).

### Wrapper mode — `chitin run claude-code [args]`

```
1. TS CLI generates a fresh session_id.
2. chitin-kernel init                           # idempotent
3. chitin-kernel sweep-transcripts              # cheap orphan recovery; rarely does anything
4. chitin-kernel install-hook --session-id <id>
   → writes a session-scoped settings overlay under .chitin/sessions/<id>/
     registering our adapter binary for each of the 7 subscribed hooks.
5. TS CLI spawns `claude [args]` with CLAUDE_CODE_SETTINGS=<overlay path>; inherits stdio; waits.
6. SessionStart hook fires → adapter → kernel emits session_start.
7. [normal session: user_prompt / intended / executed / compaction / SubagentStop hooks fire;
    adapter → kernel emits corresponding events]
8. SessionEnd hook fires → adapter → kernel ingest-transcript → kernel emits session_end.
9. claude process exits.
10. chitin-kernel uninstall-hook --session-id <id>  # cleans up overlay
```

### Hook mode (fallback)

For users launching Claude Code externally (IDE, hotkey, `claude` from a random shell). `chitin init` writes the adapter into the user's global Claude Code settings. Same event flow; no session-scoped overlay; each session naturally gets its own `session_id` via `SessionStart`.

### Chain correlation and bookkeeping

- Claude Code passes `tool_use_id` in every tool-hook payload. The adapter forwards it; the kernel uses it as the tool-call `chain_id`. Pre and Post of the same tool call hit the same chain.
- Kernel maintains `.chitin/chain_index.sqlite` mapping `chain_id → (last_seq, last_hash)`. On each emit: look up, compute `seq = last_seq + 1` and `prev_hash = last_hash`, append to JSONL, update index.
- Chain index is fully rebuildable from JSONL on startup if missing or stale (crash recovery). Still kernel-owned writes.

### `seq` vs `ts`

**Invariant: `session_end` is always the last event in its session chain by `seq`.** Transcript ingest runs *before* `session_end` is emitted; transcript-sourced `assistant_turn` events slot in at their emit-time `seq` values (after all hook-fired events, before `session_end`).

Consequence: **within a session chain, `seq` is emission order, not wall-clock order.**

- For time-ordered replay: consumers sort by `ts`.
- For tamper-evidence: `prev_hash` follows `seq`.

Both orders are stable. Both are useful. Dashboards and replay tools must document which they use.

---

## Section 4 — Transcript ingest (Approach 1: batch at SessionEnd)

### Location and format

Claude Code transcripts live at `~/.claude/projects/<project-slug>/<session_id>.jsonl`. Each line is a JSON message. We care about `type: "assistant"` messages; each has a timestamp, a model id, a `usage` block, and a `content` array of typed blocks (`text`, `thinking`, `tool_use`, etc.).

### Extraction — one assistant message → one `assistant_turn` event

- `payload.text` — concatenated `text` blocks.
- `payload.thinking` — concatenated `thinking` blocks (optional; only present if extended thinking is active).
- `payload.usage` — the message's usage block, verbatim.
- `payload.model_used` — `{ name, provider, version }` inferred from the `model` id.
- `ts` — the message's original transcript timestamp (not ingest time).
- `tool_use` blocks in content are **ignored** — already captured by PreToolUse / PostToolUse hooks; no double-emit.

Emit in transcript order, appending to the session chain after all hook-fired events and before `session_end` (per Section 3's invariant).

### Kernel subcommand

`chitin-kernel ingest-transcript --session-id <id> --transcript-path <path>`. Reads the file, emits `assistant_turn` events, updates the chain index. Idempotent: tracks last-ingested byte offset per transcript in `.chitin/transcript_checkpoint.json`, resumes rather than double-emits.

### Subagent transcripts (open verify-at-implementation item)

Claude Code stores subagent conversations somewhere — TBD at implementation time whether in separate `.jsonl` files (discoverable by subagent `session_id`) or inline in the parent transcript with a parent_message_id reference. Kernel subcommand stays `ingest-transcript` either way — called once per session chain (main + each subagent).

### Resilience — orphaned sessions

If a session dies unclean (kill -9, machine crash), `SessionEnd` never fires and the transcript never ingests.

- Kernel maintains `.chitin/transcript_checkpoint.json` keyed by `session_id → { transcript_path, last_ingest_offset, status: "complete" | "partial" }`.
- `chitin-kernel sweep-transcripts` scans the project dir for transcripts without `status == "complete"`, ingests them, emits a synthetic `session_end` with `payload.reason = "orphaned_sweep"` so replay stays well-formed.
- Sweep is run opportunistically: on `chitin init`, on `chitin events list/tail`, and at the start of every `chitin run`. Cheap; rarely does anything.

---

## Section 5 — OpenClaw and Ollama-local adapters (Phase 1.5 scope)

Both ship **wrapper-mode only**. No hook-mode fallback — neither surface has a hook system.

### Phase 1.5 scope (both)

**Session chain only**: `session_start`, `user_prompt`, `assistant_turn`, `session_end`. **No tool-call chains for these surfaces in 1.5.**

Rationale: Phase 1.5's real test is whether the *session chain* contract is surface-neutral. Reasoning tasks are comparable across all three surfaces. Tool-call chain parity for OpenClaw / Ollama gets its own design pass in Phase 2.

### Ollama-local adapter (concrete)

- Wraps `ollama run <model> [prompt]` (one-shot or interactive).
- `chitin run ollama -- run llama3 "explain X"` — TS CLI generates `session_id`, spawns ollama, captures stdin/stdout/stderr.
- Emits:
  - `session_start` before spawn: `surface=ollama-local`, payload with model name, local ollama version, machine_fingerprint.
  - `user_prompt` per user turn (stdin capture in one-shot or interactive modes).
  - `assistant_turn` per model turn (stdout capture; `payload.usage` from Ollama's JSON response — `prompt_eval_count`, `eval_count`, timing fields).
  - `session_end` on process exit.
- Prefers HTTP API reads on `localhost:11434` for structured usage over scraping terminal output. Any HTTP reads happen in the Go kernel (respects hard rule).

### OpenClaw adapter (spike)

Not designed in this doc — marked as a **Phase 1.5 implementation spike**:

- Investigate OpenClaw's launch model (CLI args, subprocess lifecycle, log format, HTTP API, config files).
- Output: ~1-page design note appended to this spec.
- Decision: is wrapper-mode cleanly implementable, or does OpenClaw need a different integration shape (log tailing, library plugin)?
- If blocked: flag to user, defer OpenClaw to a follow-up phase. Ship Phase 1.5 with Claude Code + Ollama-local — two surfaces is enough to prove surface-neutrality.

### Shared adapter responsibilities

- Correct envelope: `surface`, `driver_identity`, `agent_fingerprint`, `labels`.
- Forward events via `chitin-kernel emit` (no direct FS writes).

### Phase 1.5 acceptance criterion (updated)

Run the same reasoning task on Claude Code + Ollama-local (and OpenClaw if the spike ships). Verify all surfaces produce session chains with identical envelope structure, differing only in `surface`, `model_used`, `usage`, and content. Surface-neutrality proven.

---

## Section 6 — CLI surface and kernel subcommands

Two tiers: **TS CLI** (`chitin`, what users type, in `apps/cli`) drives UX and orchestration. **Go kernel** (`chitin-kernel`) handles every side-effecting operation.

### TS CLI (`chitin`)

| Command | Purpose |
|---|---|
| `chitin init [--label k=v ...]` | Create `.chitin/` state (idempotent). Labels written to `.chitin/labels.json`. |
| `chitin run <surface> [args...]` | **Primary instrumented-launch UX.** `chitin run claude-code`, `chitin run ollama -- run llama3 "..."`, `chitin run openclaw [...]`. Accepts `--label k=v` (merged with project defaults). |
| `chitin events list [--session <id>] [--surface <s>] [--event-type <t>] [--label k=v] [--since <ts>]` | Extended with chain-aware filters. |
| `chitin events tail` | Unchanged — live stream of JSONL. |
| `chitin events tree <session_id>` | Render the session as a nested tree: session → tool-call chains → subagent sessions → their tool-call chains. |
| `chitin replay <session_id>` | Walks the tree (Section 2 queries). |
| `chitin chain verify <session_id>` | Stub in 1.5: prints "verification lands in Phase 2." Fields exist; tool ships later. |

### Kernel subcommands (`chitin-kernel`)

| Subcommand | Purpose |
|---|---|
| `init [--force]` | Create `.chitin/`, `events-<run_id>.jsonl`, `events.db`, `chain_index.sqlite`, `transcript_checkpoint.json`. Idempotent. |
| `install-hook --session-id <id>` | Write a session-scoped settings overlay under `.chitin/sessions/<id>/` (Claude Code). |
| `uninstall-hook --session-id <id>` | Remove overlay. |
| `emit --event-type <t> --session-id <id> --chain-id <id> --chain-type <t> [--parent-chain-id <id>] --payload-file <path>` | Emit one event. Computes `seq`, `prev_hash`, `this_hash`; appends to JSONL; updates chain_index. |
| `ingest-transcript --session-id <id> --transcript-path <path>` | Batch-ingest assistant_turn events. Idempotent via offset checkpoint. |
| `sweep-transcripts [--project-dir <path>]` | Orphan recovery. |
| `chain-info --chain-id <id>` | Returns `(last_seq, last_hash, event_count)` JSON on stdout. Used by adapters when bookkeeping needs verification. |
| `hook` | **Existing.** Reads a Claude Code hook payload on stdin, routes to the right `emit` internally. Single entry point from adapters. |

### Error shape (kernel subcommands)

Exit 0 on success. On error: exit non-zero, emit one JSON line to stderr — `{"error": "<kind>", "message": "<msg>", "chain_id": "<id>" | null}`. TS CLI surfaces with readable formatting.

### Kernel-as-library note

The Go kernel should factor `canon`, `normalize`, `emit`, `chain`, `hash` as library packages (`go/execution-kernel/pkg/...`) with thin CLI wrappers in `cmd/`. Phase 3 Postgres shippers, analytics workers, or policy engines may link the library directly without subshelling. Non-goal for 1.5 implementation beyond maintaining this packaging discipline.

---

## Section 7 — Sidecar contract and Phase 3 prep

No code ships in Phase 1.5 for this section; the contract is *declared* so Phase 3 sidecars land cleanly.

### Stable public interface

`.chitin/events-<run_id>.jsonl` is the append-only, zod-governed contract. Format lives in `libs/contracts`. Any change to envelope or event vocabulary is a `schema_version` bump. Consumers read JSONL (or the SQLite projection); consumers never write to the JSONL.

### Reference sidecar

`libs/telemetry`'s SQLite indexer *is* the reference. It's the tail-and-project pattern every future sidecar follows. Architecture doc labels it as such so Phase 3 sidecar authors have a template.

### Sidecar contract (rules)

1. **Read-only against JSONL.** Only the Go kernel writes to `.chitin/events-*.jsonl`. Sidecars may write their own derived state — a local DB, a remote API, an alert store — but not the canonical JSONL.
2. **Malformed-line tolerance.** Already in `libs/telemetry`; every sidecar inherits that discipline.
3. **Restartable from checkpoint.** Byte offset into the JSONL OR `(run_id, chain_id, seq)` cursor. Sidecars must not assume they were running since session start.
4. **Idempotent on replay.** Seeing the same event twice must not double-write derived state. Envelope's `this_hash` is a natural dedup key.

### Upgrade path Approach 1 → Approach 2

Same JSONL shape, same sidecar contract. Only change: *when* `assistant_turn` events land (SessionEnd batch → live per-session tailer). No consumer breaks.

---

## Section 8 — Schema cutover, testing, error handling

### Schema cutover (break v1)

- `chitin-kernel init --force` wipes `.chitin/events-*.jsonl`, `events.db`, `session_state.json`, `chain_index.sqlite`, `transcript_checkpoint.json`, `sessions/`, `labels.json`. Fresh start on v2.
- Update zod schema in `libs/contracts` to v2 envelope + per-event-type payloads.
- Regenerate Go types via `nx run contracts:generate-go-types`.
- `schema_version: "2"` required on every envelope.
- Delete v1-specific code; no migration helpers. Git history is the archive.

### Testing plan (layered, bottom-up)

**1. Contracts**
- Zod schema round-trip tests for every envelope combination.
- Payload schema tests for every event_type.
- Go-type-generator snapshot test (types regenerate byte-stable across runs).

**2. Kernel**
- `canon` + `normalize`: existing Phase 1 tests hold.
- **Hash determinism:** table-driven test with golden hashes — fixed event record → identical canonical-JSON serialization → identical SHA-256 across Go versions and platforms.
- **Chain bookkeeping:** emit N events into a chain; verify `seq` monotonic + `prev_hash` linkage recomputes correctly.
- **Chain index rebuild:** delete `chain_index.sqlite`, invoke any kernel subcommand, verify it rebuilds from JSONL and subsequent emits use correct `seq` / `prev_hash`.
- **Transcript ingest:** fixture Claude Code transcript → expected `assistant_turn` events (text, thinking, usage, model_used, timestamps).
- **Orphan sweep:** fixture transcript with no `session_end` → sweep emits synthetic `session_end` with `reason: "orphaned_sweep"`.
- **Idempotent ingest:** re-invoke ingest-transcript twice; second invocation emits zero new events (checkpoint respects offset).

**3. Claude Code adapter**
- Hook → event_type mapping for each of the 7 subscribed hooks.
- Pre/Post correlation: two hook payloads with same `tool_use_id` produce `intended` + `executed` on the same `chain_id`.
- Wrapper mode: install-hook + mock-spawn + uninstall-hook cleanup sequence.

**4. Ollama-local adapter**
- Wrapper-mode fixture with a mocked `ollama` executable: verify session chain emission + usage extraction.

**5. CLI**
- `chitin run` orchestration.
- `chitin events tree` rendering (golden fixture: 2-chain session with one subagent).
- `chitin replay` walks the tree correctly.

**6. End-to-end smoke**
- `chitin run claude-code` on a trivial task (`"echo hi"`).
- Confirm resulting JSONL has expected chain structure and hash integrity.

### Error handling

- **Kernel subcommand errors:** exit non-zero, JSON to stderr (`{error, message, chain_id}`).
- **Malformed hook payload:** kernel logs, skips, exits 0 — never crash the user's session.
- **Transcript read failures** (file missing, truncated, non-JSON lines): log, mark checkpoint `"partial"`, emit synthetic `session_end` with `payload.reason = "transcript_read_error"`. Replay produces a well-formed (annotated) tree.
- **Chain index corruption detected:** rebuild from JSONL on the next kernel invocation (self-healing).
- **Hash integrity violation on emit:** refuse to append, exit non-zero. Defensive — should be unreachable under single-writer discipline.

---

## Open decisions / implementation-time verifications

Not design gaps — resolvable during implementation:

1. **Subagent transcript storage shape** (Section 4). Inspect real transcripts; choose demux strategy.
2. **OpenClaw invocation model** (Section 5). Formal spike with a ~1-page note appended to this spec.

---

## Implementation rollout order

The writing-plans skill will refine this into discrete plan steps. Expected sequence:

1. **`libs/contracts` v2** — zod schema, Go type regeneration, `schema_version` field, payload unions, hash helpers.
2. **`go/execution-kernel` v2** — new subcommands (`init --force`, `install-hook`, `uninstall-hook`, `emit`, `ingest-transcript`, `sweep-transcripts`, `chain-info`); chain bookkeeping; hash computation; chain_index.sqlite; transcript_checkpoint.json; transcript parser.
3. **`libs/adapters/claude-code` v2** — 7 hook subscriptions; wrapper-mode + hook-mode adapter behavior; `tool_use_id` → `chain_id` correlation.
4. **`apps/cli` v2** — `chitin run` orchestration; `events tree`; `replay` walks tree; `chain verify` stub.
5. **`libs/adapters/ollama-local`** — new package; wrapper-mode only; HTTP API usage reads.
6. **`libs/adapters/openclaw`** — spike + design note + minimal wrapper-mode implementation (or deferred with explicit flag).
7. **End-to-end smoke test** — `chitin run claude-code` capturing a trivial task; chain integrity verified.
8. **`libs/telemetry` v2** — SQLite indexer schema updated for new envelope; existing tailer + replay streamer updated; update reference-sidecar documentation.

Each step testable in isolation; each step committable without breaking the previous.

---

## Appendix A — Envelope field reference (concise)

```ts
type Envelope = {
  schema_version: "2";
  run_id: UUID;
  session_id: UUID;
  surface: string;                        // open vocabulary
  driver_identity: {
    user: string;
    machine_id: string;
    machine_fingerprint: Sha256Hex;
  };
  agent_instance_id: UUID;
  parent_agent_id: UUID | null;
  agent_fingerprint: Sha256Hex;
  event_type: string;                     // open vocabulary
  chain_id: string;                       // opaque; UUID for minted chains, surface-native id for tool-call chains (e.g. Claude Code tool_use_id)
  chain_type: "session" | "tool_call";
  parent_chain_id: string | null;         // same format as chain_id
  seq: number;
  prev_hash: Sha256Hex | null;
  this_hash: Sha256Hex;
  ts: string;                             // ISO-8601 UTC
  labels: Record<string, string>;
  payload: Payload;                       // union keyed by event_type
};
```

## Appendix B — Kernel FS writes (exhaustive)

Every filesystem write in chitin Phase 1.5 lives behind one of these kernel subcommand invocations:

| Path | Writer | Purpose |
|---|---|---|
| `.chitin/events-<run_id>.jsonl` | `emit`, `ingest-transcript`, `sweep-transcripts` | Canonical event log |
| `.chitin/events.db` | `libs/telemetry` indexer (read-only on JSONL, writes derived SQLite) | **Not kernel-owned** — sidecar writes its own derived state; this is allowed by the sidecar contract |
| `.chitin/chain_index.sqlite` | `emit`, `ingest-transcript`, `sweep-transcripts` | chain_id → (last_seq, last_hash) |
| `.chitin/transcript_checkpoint.json` | `ingest-transcript`, `sweep-transcripts` | Offset tracking per transcript |
| `.chitin/sessions/<id>/` | `install-hook`, `uninstall-hook` | Session-scoped Claude Code settings overlay |
| `.chitin/labels.json` | `init --label k=v ...` | Project-default labels |

The SQLite indexer (`events.db`) is a sidecar, not kernel-owned — it respects the hard rule because it reads the kernel-owned JSONL and writes its own derived state.
