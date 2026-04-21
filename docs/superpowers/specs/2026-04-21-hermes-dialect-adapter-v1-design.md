# Hermes Dialect Adapter v1 — Design

**Date:** 2026-04-21
**Parent:** `docs/superpowers/specs/2026-04-20-otel-genai-ingest-workstream-design.md` (workstream meta-spec — hermes is the second consumer after openclaw)
**Related:** `docs/superpowers/specs/2026-04-20-hermes-probe-design.md` (probe that designed the adapter-shape decision tree; this spec is the "verdict = yes" follow-up the probe anticipated)
**Informed by:** OTEL surface scan run 2026-04-21 against `~/.hermes/hermes-agent/` — finding: no OTEL export surface in hermes, but a first-class plugin-hook API (`post_api_request`) exposes strictly-superior per-LLM-call data compared to openclaw's OTLP spans (tokens, duration, model, provider, session correlation).
**Status:** Design — ready for user review, then handoff to writing-plans for an execution plan.

## Preamble

Hermes runs on this workstation as a custom Python agent harness (`~/.hermes/hermes-agent/`) driving a local Ollama instance. A week of informal usage (see the as-yet-uncommitted probe observations) showed hermes carries real work — React app scaffolds, architecture diagrams, async teammate patterns — and the decision to graduate it from demo tasks to real work is trivially gated by one missing piece: **hermes emits nothing chitin can ingest**. Without observability, handing hermes real work defeats chitin's core thesis (self-hosted agents, observable by design, per `project_github_native_agent_gap.md`). This spec wires that gap.

The brainstorming decisions (2026-04-21) that shape the design:

1. **Translator placement:** chitin-side, not hermes-side. Hermes emits its native event dialect; all normalization to canonical form lives in `libs/adapters/<source>/` or `go/execution-kernel/internal/ingest/<source>.go` in chitin. Source-harness code has no chitin vocabulary. Rationale: chitin is OSS and meant to be generally useful; others will integrate their own harnesses. Principle captured in `memory/feedback_chitin_minimal_source_side.md`.
2. **Transport:** JSONL file, not HTTP. Hermes plugin appends to a daily-rotated file; chitin reads the file. Decouples hermes from chitin's uptime; keeps the plugin to "dump JSON, done."
3. **Abstraction level:** plugin hooks, not gateway hooks. Gateway hooks expose agent-lifecycle events (`agent:start/step/end`) without token/model data. Plugin hooks expose `post_api_request` — a per-LLM-API-call hook (`run_agent.py:10919`) carrying model, provider, duration, token usage, finish-reason, and session correlation. Strictly superior to what openclaw's OTLP capture provides.
4. **v1 event set (asymmetric):** hermes plugin captures five event types (`post_api_request`, `on_session_start`, `on_session_end`, `pre_tool_call`, `post_tool_call`) — kwargs-dump is cheap, forward-compat is free. Chitin translator v1 parses only `post_api_request` → `ModelTurn`. Other event types land in JSONL and are quarantined with `Reason="v1-scope"`. v2 expands the translator without a hermes-side redeploy.
5. **Chitin-side invocation:** new `chitin-kernel ingest-hermes --file <path>` subcommand. Matches existing pattern (`ingest-otel`, `ingest-transcript` at `cmd/chitin-kernel/main.go:36`). Batch execution, user-invoked or cron'd. No daemon, no tail-follow — v2 concern.

## One-sentence invariant

Every `post_api_request` event emitted by a hermes plugin and written to a daily JSONL file becomes exactly one `ModelTurn` event in the chitin chain when `chitin-kernel ingest-hermes` runs against that file; every other event type in the file is quarantined with `Reason="v1-scope"`.

## Scope

### In scope

- **Hermes plugin** at `~/.hermes/plugins/chitin-sink/` (Python package with `__init__.py`). Registers five plugin hooks (`post_api_request`, `on_session_start`, `on_session_end`, `pre_tool_call`, `post_tool_call`). Each handler packs its kwargs into a JSON object `{event_type, ts, kwargs}` and appends one line to today's JSONL file.
- **JSONL file layout:** `~/.hermes/chitin-sink/events-YYYY-MM-DD.jsonl`. Day-rotated. Append-only. Directory auto-created on first write. No size cap, no manual rotation inside a day.
- **Chitin translator** at `go/execution-kernel/internal/ingest/hermes.go`. Mirrors `openclaw.go` shape: `ParseHermesEvents(lines []byte) ([]ModelTurn, []Quarantine, error)` — never errors mid-walk; a returned error is reserved for structural failures (e.g. file not readable).
- **Chitin CLI subcommand** `chitin-kernel ingest-hermes --file <path>` in `cmd/chitin-kernel/main.go`. Parses JSONL, invokes translator, emits each `ModelTurn` into the event chain (reusing the existing `emit` package path used by `ingest-otel`).
- **Chain-id scheme:** `chain_id = "hermes:" + hex(synthetic_trace_id) + ":" + hex(synthetic_span_id)`. Mirrors the tripartite shape SP-2 adopted for OTEL-sourced events (`"otel:" + hex(trace) + ":" + hex(span)`); the prefix is honest about provenance. Synthetic trace/span IDs are derived deterministically from hermes's hook kwargs (`session_id`, `api_call_count`) — hermes emits no real OTEL IDs, so synthesis is the honest choice, and determinism gives free re-ingest idempotency.
- **Tests:** fixture-driven translator unit tests (mixed event types in a synthetic JSONL), ingest-subcommand integration test mirroring `openclaw_integration_test.go`.

### Out of scope

- **v2 event types.** Tool calls (`pre_tool_call`/`post_tool_call`), session lifecycle (`on_session_start`/`on_session_end`), and any future plugin hooks. These land in JSONL and quarantine; parsing them introduces new canonical types (`ToolCall`, session envelopes) that are their own scope.
- **Real-time tail-follow.** v1 is batch. Users or cron drive invocation. Daemonizing the kernel is a separate architectural decision.
- **Governance / policy over hermes.** This spec is observability only. "chitin governs hermes" (block dangerous tools, enforce policies) is the follow-up the probe spec named; it requires the observability loop working first.
- **Token-cost aggregation, dashboards, queries.** Downstream of ingest. Separate brainstorms when `ModelTurn` volume is non-trivial.
- **Multi-source correlation.** If hermes runs Claude Code as a subagent, or vice-versa, the session/trace-linkage across sources is a separate workstream.
- **Backfilling the probe week.** Any hermes activity before the plugin is installed is not captured. No post-hoc reconstruction.
- **Hermes-side schema changes.** If hermes renames hook kwargs in a future version, the plugin and translator are updated — no attempt at version-tolerant reading in v1.
- **Any Readybench/bench-devs content.** Chitin is OSS; the content-boundary rule applies.

## Architecture

```
hermes process                                chitin kernel
────────────────────────────────              ──────────────────────────────
run_agent.py                                  cmd/chitin-kernel/main.go
  run_conversation()
    ...tool loop iteration...                   ingest-hermes
      invoke_hook("post_api_request",            --file <path>
        task_id, session_id, platform,               │
        model, provider, base_url, api_mode,         │
        api_call_count, api_duration,                ▼
        finish_reason, response_model,            internal/ingest/hermes.go
        usage, assistant_content_chars,             ParseHermesEvents(bytes)
        assistant_tool_call_count)                    → []ModelTurn
              │                                       → []Quarantine
              ▼                                         │
    ~/.hermes/plugins/chitin-sink/__init__.py           ▼
      def _on_post_api_request(**kwargs):           internal/emit.Emit(ModelTurn)
        _append({                                     → chain .jsonl
          "event_type": "post_api_request",           → chain_index.sqlite
          "ts": iso_now(),
          "kwargs": kwargs
        })
              │
              ▼
    ~/.hermes/chitin-sink/
      events-YYYY-MM-DD.jsonl   ─────────────────→ (chitin reads this file)
```

## Components

### 1. Hermes plugin — `~/.hermes/plugins/chitin-sink/`

One Python package, ~50 lines. Single public symbol: `register(ctx)`.

**Files:**
- `__init__.py` — registers five hooks; each handler calls `_append(event_type, kwargs)`.
- `README.md` — one paragraph: what it does, where the output goes, how to disable.

**Handler contract:**

```python
def _append(event_type: str, kwargs: dict) -> None:
    # Best-effort write; plugin hook contract already catches exceptions.
    path = Path.home() / ".hermes" / "chitin-sink" / f"events-{today_iso()}.jsonl"
    path.parent.mkdir(parents=True, exist_ok=True)
    line = json.dumps({
        "event_type": event_type,
        "ts": datetime.now(timezone.utc).isoformat(),
        "kwargs": _scrub(kwargs),
    }, default=str)
    with path.open("a") as f:
        f.write(line + "\n")
```

`_scrub` drops kwargs that aren't JSON-serializable by default (e.g., the `conversation_history` list from `pre_llm_call` is large and recursive; the plugin drops it at source rather than serializing a novel). Exact scrub list documented in plugin README: `conversation_history` dropped; everything else kept as-is.

**Hooks registered:**

| Hook | Why captured | v1 translator behavior |
|---|---|---|
| `post_api_request` | Primary. Per-LLM-call data with tokens, duration, model. | Parsed → `ModelTurn`. |
| `on_session_start` | Session boundary with `session_id, model, platform`. | Quarantined (Reason="v1-scope"). |
| `on_session_end` | Session boundary with `completed, interrupted`. | Quarantined (Reason="v1-scope"). |
| `pre_tool_call` | Tool-call visibility: `tool_name, args, task_id`. | Quarantined (Reason="v1-scope"). |
| `post_tool_call` | Tool-call result: `tool_name, args, result, task_id`. | Quarantined (Reason="v1-scope"). |

### 2. JSONL file contract

**Path:** `~/.hermes/chitin-sink/events-YYYY-MM-DD.jsonl`. Local time zone is system-local; the per-line `ts` is ISO UTC regardless.

**Line format:** one JSON object per line:

```json
{"event_type": "post_api_request",
 "ts": "2026-04-21T19:42:01.234567+00:00",
 "kwargs": {"task_id": "...", "session_id": "...", "model": "qwen3-coder:30b", "provider": "ollama-launch", "api_call_count": 1, "api_duration": 2.34, "finish_reason": "stop", "usage": {"prompt_tokens": 1024, "completion_tokens": 256, "total_tokens": 1280}, "assistant_content_chars": 1720, "assistant_tool_call_count": 2, ...}}
```

No line order guarantee across handlers (async hooks can interleave). Within a single hook's handler, writes are sequential.

### 3. Chitin translator — `go/execution-kernel/internal/ingest/hermes.go`

Mirrors `openclaw.go`:

```go
// ParseHermesEvents classifies every line in a hermes-sink JSONL stream.
// Returns turns (parseable post_api_request events) and quarantined (all
// other event types + malformed lines). Never errors mid-walk; a returned
// error is reserved for structural failures (I/O, non-JSONL input).
func ParseHermesEvents(raw []byte) ([]ModelTurn, []Quarantine, error)
```

**`post_api_request` → `ModelTurn` mapping:**

| ModelTurn field | Hermes kwargs source | Notes |
|---|---|---|
| `TraceID` | synthetic: first 32 hex chars of sha256(`session_id`) | Hermes has no OTEL trace ID. Deterministic from session_id; all API calls in one session share a trace (consistent with OTEL semantics). |
| `SpanID` | synthetic: first 16 hex chars of sha256(`session_id` + ":" + `api_call_count`) | Deterministic, unique per (session, call). |
| `Ts` | line-level `ts` (ISO UTC) | Recorded when hook fired, not when ingested. |
| `Surface` | `"hermes"` | Constant for this adapter. |
| `Provider` | `kwargs.provider` | e.g. `"ollama-launch"`. |
| `ModelName` | `kwargs.response_model` if non-nil, else `kwargs.model` | `response_model` is what the LLM reported; `model` is what hermes requested. |
| `InputTokens` | `kwargs.usage.prompt_tokens` | 0 if `usage` nil. |
| `OutputTokens` | `kwargs.usage.completion_tokens` | 0 if `usage` nil. |
| `SessionIDExternal` | `kwargs.session_id` | |
| `DurationMs` | round(`kwargs.api_duration` * 1000) | |
| `CacheReadTokens` | `kwargs.usage.prompt_tokens_details.cached_tokens` if present | 0 otherwise. |
| `CacheWriteTokens` | 0 | Hermes / Ollama don't expose cache-write breakdown today. Explicitly 0, not "unknown." |

**Quarantine reasons (v1):**

- `"v1-scope"` — event_type is one of the four non-primary captured types, or a future hermes hook we don't parse yet.
- `"parse_error"` — line is not valid JSON, or missing `event_type`.
- `"missing_fields:<list>"` — `post_api_request` is missing `session_id`, `api_call_count`, or `usage`.

### 4. Chitin CLI — `chitin-kernel ingest-hermes`

Added to `cmd/chitin-kernel/main.go`:

```go
case "ingest-hermes":
    cmdIngestHermes(args)
```

`cmdIngestHermes` signature mirrors `cmdIngestOTEL`: takes `--file <path>` and `--dir <state-dir>`, reads the JSONL file, calls `ParseHermesEvents`, emits each `ModelTurn` into the chain via the existing `emit` package. Writes quarantine summary to stderr, emits count of turns/quarantined to stdout as JSON.

## Data flow

1. Hermes runs. User sends a message, agent enters tool-calling loop.
2. For each API call: `run_agent.py:10919` fires `post_api_request` with kwargs.
3. Hermes's plugin dispatch calls `chitin-sink`'s handler; handler appends one line to today's JSONL file.
4. Out-of-band (user-invoked or cron'd): `chitin-kernel ingest-hermes --file ~/.hermes/chitin-sink/events-YYYY-MM-DD.jsonl`.
5. Translator walks the file, produces `[]ModelTurn` and `[]Quarantine`.
6. For each `ModelTurn`: `emit.Emit` writes to chain JSONL and updates `chain_index.sqlite`.
7. CLI prints `{"turns": N, "quarantined": M, "quarantined_by_reason": {...}}` on success.

Re-ingestion of the same file is idempotent: each `ModelTurn`'s `chain_id = "hermes:" + hex(TraceID) + ":" + hex(SpanID)` is deterministic from `session_id` + `api_call_count`; the emit path already de-dupes on chain_id (per openclaw's implementation).

## Error handling

- **Plugin handler exception:** caught by hermes's hook dispatcher (`hermes_cli/plugins.py` wraps every invocation in try/except). Exception logged to hermes's errors.log; agent keeps running; that event is lost. Acceptable for observability v1.
- **JSONL file unwritable (disk full, permission denied):** plugin's `_append` catches the IOError, logs to hermes's errors.log, drops the event. Same rationale.
- **Malformed JSONL line on ingest:** quarantined with `Reason="parse_error"`. Translator continues past.
- **Ingest CLI called on missing file:** `cmdIngestHermes` exits with `exitErr("missing_file", ...)`. Matches existing kernel CLI error shape.
- **`post_api_request` with `usage=nil`:** kept as a ModelTurn with 0 tokens AND `assistant_content_chars` preserved (proof the call happened). Not quarantined — some ollama endpoints don't emit usage for streaming responses; these are still honest data.
- **`post_api_request` missing `session_id` or `api_call_count`:** quarantined with `Reason="missing_fields:<list>"`. Can't correlate without them.

## Testing

### Translator unit tests (`hermes_test.go`)

Fixture-driven. One JSONL file per test case in `internal/ingest/testdata/hermes/`:

- `post_api_request_happy.jsonl` — one clean event → one ModelTurn.
- `post_api_request_multi_call_session.jsonl` — three `post_api_request` events with same session_id, `api_call_count` = 1/2/3 → three ModelTurns with distinct synthetic span IDs.
- `missing_usage.jsonl` — `post_api_request` with `usage=null` → ModelTurn with 0 tokens, assistant_content_chars preserved.
- `missing_session_id.jsonl` → quarantined, Reason="missing_fields:session_id".
- `mixed_event_types.jsonl` — one of each hook type → one ModelTurn + four quarantined (Reason="v1-scope").
- `malformed_line.jsonl` — one line is `{not json` → quarantined, Reason="parse_error". Other lines unaffected.

### Integration test (`hermes_integration_test.go`)

Mirrors `openclaw_integration_test.go`:

1. Set up a temp `--dir`.
2. Write a JSONL file with three `post_api_request` events + one `on_session_start`.
3. Run `chitin-kernel ingest-hermes` as a subprocess.
4. Assert stdout JSON: `{"turns": 3, "quarantined": 1, ...}`.
5. Assert the chain JSONL contains three ModelTurn events with expected chain_ids.
6. Re-run the same command; assert stdout reports zero *new* turns (idempotency).

### Manual verification (not in CI)

- Install the hermes plugin (`pip install -e ~/.hermes/plugins/chitin-sink/` or `cp -r chitin-sink ~/.hermes/plugins/`).
- Restart hermes gateway; send a Telegram message.
- Verify `~/.hermes/chitin-sink/events-YYYY-MM-DD.jsonl` has at least one `post_api_request` line.
- Run `chitin-kernel ingest-hermes --file <path> --dir <chitin-state-dir>`.
- Verify chain JSONL has a ModelTurn with `Surface="hermes"`.

## Self-review

### Placeholder scan

No `TBD`, `TODO`, or `<fill-in>` literals. `YYYY-MM-DD` inside filepaths and fixture names is a format-literal, not a placeholder — it denotes the runtime-computed date. Scrub-list and fixture names are explicit.

### Internal consistency

- The five-hook capture list in § "Hermes plugin" matches the five rows of the v1-behavior table, matches the five event types named in § "Scope / In scope", matches the testdata fixture `mixed_event_types.jsonl`.
- The `ModelTurn` field mapping in § "Component 3" matches the openclaw `ModelTurn` struct defined at `go/execution-kernel/internal/ingest/openclaw.go:27-40`. No field additions.
- Chain-id scheme in § "Scope / In scope" (`"hermes:" + session_id + ":" + api_call_count_padded`) matches the idempotency claim in § "Data flow" (deterministic → de-dupe on re-ingest).
- Quarantine reason names in § "Component 3" (`"v1-scope"`, `"parse_error"`, `"missing_fields:<list>"`) match the fixture-test expectations in § "Testing".

### Scope check

Single brainstorm → single execution cycle. The spec covers one adapter (hermes), one direction (ingest, not govern), one event type parsed (`post_api_request`), one canonical type produced (`ModelTurn`). Everything richer — tool calls, sessions, governance, tail-follow, dashboards — is explicitly in "Out of scope" with its own future scope-statement.

### Ambiguity check

- **"Synthetic trace/span IDs"** — explicitly defined: sha256(session_id)[:32] for trace (32 hex = 128 bits, matches OTEL trace-id width), sha256(session_id + ":" + api_call_count)[:16] for span (16 hex = 64 bits, matches OTEL span-id width). No iteration-order dependence; deterministic across re-ingests.
- **"Best-effort write"** in the plugin — explicitly means "log the IOError to hermes's errors.log and drop the event." Not silently ignored.
- **"Kwargs dropped at source"** for `conversation_history` — explicit scrub list in plugin README. Other kwargs pass through as-is.
- **"Usage=null means ModelTurn with 0 tokens, not quarantined"** — distinguished from "missing session_id = quarantined." Rule: correlation fields missing → quarantine; data-richness fields missing → keep, mark zero.

## Execution handoff

Next action: write an execution plan via the `superpowers:writing-plans` skill. The plan will break this spec into ordered tasks:

1. Scaffold the hermes plugin (Python package, register function, append helper, scrub list, README).
2. Wire the plugin into hermes (symlink or copy into `~/.hermes/plugins/`, verify it loads).
3. Capture a real JSONL sample by sending one Telegram message through hermes.
4. Author fixture JSONL files from the real sample + synthesized edge cases.
5. Implement `ParseHermesEvents` in `go/execution-kernel/internal/ingest/hermes.go`.
6. Add `cmdIngestHermes` to `cmd/chitin-kernel/main.go` and the subcommand switch.
7. Write translator unit tests.
8. Write integration test.
9. Run the full flow manually (plugin → JSONL → ingest → chain JSONL) and commit a capture artifact to `docs/observations/`.
10. Open PR, run through the standard review cycle (Copilot → /review → adversarial → merge).
