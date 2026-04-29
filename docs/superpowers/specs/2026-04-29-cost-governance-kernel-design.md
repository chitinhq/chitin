# Cost-Governance Kernel — Design

**Date:** 2026-04-29
**Status:** Design v3 (revised 2026-04-29 after `/grill-me` design pivot. Talk pressure removed; chitin's role narrowed to permission-gate; envelope state moves to sqlite; T0 collapses to audit-log tag; hook driver promoted in scope).
**Forcing function:** None — strategic. The 2026-05-07 talk is covered by existing artifacts (PR #51 single Copilot through chitin SDK driver, plus openclaw acpx smoke-test). This kernel slice builds chitin-as-real-system; it is not shaped to a demo deadline.
**Supersedes:** `2026-04-28-cost-governance-kernel-design.md` (v2). v2's flat-file envelope, T0-as-execution, $USD-as-primary-cap, watch TUI, and swarm fallback are all reversed here. v2 stays in `docs/` as historical record of the talk-driven path.
**Parent decisions:**
- Original "compute-amortized intelligence" memo (2026-04-28): hard runtime budgets, compiler-style model tiering, LLM-as-exception-handler, deterministic-first pipelines, aggressive caching, preflight pruning, observability cost feedback. **Most of the memo's principles defer to post-kernel work.** This slice ships the irreducible kernel: per-action permission-gate, cross-process envelope, audit log.
- Substrate spike findings (2026-04-28): Lobster BORROW (post-kernel), memU BORROW (post-kernel), Composio SKIP. Unchanged from v2.
- Openclaw 2026.4.25 acpx-override smoke test (2026-04-28): config schema accepts per-agent spawn-command override at `plugins.entries.acpx.config.agents.<id>.command`. Override fires; live e2e is the first task of Milestone B in the implementation plan. Unchanged from v2.
- `gov.Gate` (PR #45 + #51, merged 2026-04-28): closed-enum action vocabulary, policy evaluator, audit log, escalation counter, `Agent` field on Decision. This spec extends them; does not replace.
- `chitin-kernel drive copilot` (PR #51): the v1 SDK driver. v3 adds an `--acp --stdio` mode for the openclaw acpx path. The SDK mode is preserved unchanged.
- **`memory/project_two_driver_pattern.md`:** open-vendor (in-process) vs closed-vendor (wrapping orchestrator) — same `gov.Gate` API. Spec v3 honors this: chitin is a governance surface, not a tool harness. The open-vendor path here is Claude Code's PreToolUse hook (in-process via cold-start subprocess); the closed-vendor path is the Copilot ACP shim wrapping `copilot --acp --stdio`.
- **`memory/project_architectural_rules.md`:** Go kernel owns all side effects. Interpreted as: side effects route *through* the kernel for governance — not "the kernel executes them itself." Permission-gate is the right interpretation; tool harness is not.
- **`memory/feedback_forcing_functions_are_exceptions.md`:** snap back to aggregate-first when forcing function passes. v2 was talk-driven; v3 reverts to strategic shape.
- **14 design-tree decisions** from the 2026-04-29 `/grill-me` walk, recorded in the implementation plan and reflected throughout this spec.

## Preamble

`gov.Gate.Evaluate(action)` decides whether each tool call is allowed. That is necessary but not sufficient. It does not enforce a *cost ceiling*, does not coordinate across multiple agents, and does not mark *which class of action* this was for downstream cost-feedback work. The kernel slice in this spec adds three invariants on top of the existing `gov.Gate`:

1. **Bounded enforcement.** No agent run can exceed its envelope's `MaxToolCalls` or `MaxInputBytes` without explicit `envelope grant`; the gate denies the next call when any cap is exhausted. `BudgetUSD` is tracked informationally (real per-token rates are partially fictional for Copilot CLI's flat-rate model; honest accounting requires post-hoc OTEL ingest, which is out of this slice).
2. **Compiler-tier classification.** Every action is tagged T0 or T2 by a deterministic rule table. The Tier is metadata on the Decision row; **chitin does not execute differently based on Tier.** The label informs future routing tools (out of scope here) that "this action class is cheap" — it does not mean chitin ran it. Within an openclaw-orchestrated Copilot session, openclaw's harness executes; within a Claude Code session, Claude Code executes. Chitin gates and audits.
3. **Cross-process envelope under one state.** Multiple agents (across drivers) spawned under a shared budget envelope and a shared audit log. State lives in `~/.chitin/gov.db` (sqlite, alongside the existing `gov.Counter`). Cross-process atomicity via sqlite WAL.

Two driver surfaces in this slice. Both share `gov.Gate`, the audit log, the envelope, and the operator setup pattern:

- **Claude Code hook** (RTK-shape PreToolUse hook) — every Claude Code tool call routes through `chitin-kernel gate evaluate --hook-stdin --agent=claude-code` before execution. Cold-start subprocess; latency budget measured before deciding on daemon mode.
- **Copilot ACP shim** (openclaw acpx orchestrator path) — every Copilot ACP session openclaw spawns is intercepted by `chitin-kernel drive copilot --acp --stdio` via a one-line acpx config override. Long-running per-session subprocess; sub-millisecond per-call latency.

Per-tool-call routing happens inside each driver — not at session-spawn time. Session-spawn routing (where real cost savings live, since chitin doesn't execute) is **deferred to ecosystem phase** per `memory/project_strategic_roadmap.md` (aggregate → policy → ecosystem → cloud).

## One-sentence invariant

Once an envelope is current via `~/.chitin/current-envelope` (or `CHITIN_BUDGET_ENVELOPE` env var), every Claude Code tool call routes through `gov.Gate.Evaluate` + `envelope.Spend` via the PreToolUse hook, and every Copilot CLI session openclaw spawns is intercepted by `chitin-kernel drive copilot --acp --stdio` for the same governance per tool-call frame, with every decision (Tier-tagged, byte-counted, dollar-estimated-but-informational) landing in the shared `~/.chitin/gov-decisions-<date>.jsonl` and envelope state coordinated cross-process via sqlite at `~/.chitin/gov.db`.

## Scope

### In scope

- **`gov.BudgetEnvelope`** — sqlite-backed, cross-process. State in `~/.chitin/gov.db` (alongside existing `gov.Counter` schema). Per-envelope row in `envelopes` table; grants in `envelope_grants`. `Spend(CostDelta) error` returns `ErrEnvelopeExhausted` on cap breach. WAL mode; sub-ms common case under N concurrent shim writers.
- **`gov.CostDelta`** — `{USD float64, InputBytes, OutputBytes, ToolCalls int64}`. The unit of debit. `USD` is informational/best-effort; the real-time caps are on calls + bytes.
- **`gov.Tier` enum** — `TierUnset`, `T0Local`, `T1Cheap` (reserved), `T2Expensive`. Pure label on Decision; does not influence cost calculation or execution.
- **`gov.Decision` extensions** — `EnvelopeID`, `Tier`, `CostUSD`, `InputBytes`, `OutputBytes`, `ToolCalls` fields. JSONL writer extended to emit them. All `,omitempty` for backward compat.
- **`internal/cost/`** — new package. `Estimate(action gov.Action, executor string, rates RateTable) CostDelta`. **Tier-blind.** Per-executor rate lookup keyed on the agent that's actually running the tool call (`copilot-cli`, `claude-code-anthropic`, `claude-code-local`, future-others). Local-Ollama executor returns `{0,0}`. Rate table loaded from `chitin.yaml`; Anthropic and Copilot rates are pinned snapshots with an "informational, approximate" disclosure.
- **`internal/tier/`** — new package. `Route(action gov.Action) gov.Tier` rule table. Deterministic queries → T0 (file.read, git.{diff,log,status,worktree.list}, github.{pr,issue}.{view,list}, http.request to allowlisted hosts). Side-effect/judgment → T2 (file.write, git.commit, github.pr.create, etc.). Default T2.
- **`gov.Gate.Evaluate` extension** — accept `*BudgetEnvelope` parameter (nil = no envelope enforcement; preserves v1 behavior). Before returning Allow, calls `envelope.Spend(estimate)`. Returns Decision with new fields populated.
- **`chitin-kernel drive copilot --acp --stdio` mode** — long-running per-session shim spawned by openclaw's acpx plugin via the canonical override:
  ```
  plugins.entries.acpx.config.agents.copilot.command =
    "chitin-kernel drive copilot --acp --stdio --envelope=$CHITIN_BUDGET_ENVELOPE"
  ```
  Speaks ACP over stdio in both directions (parent = openclaw, child = `copilot --acp --stdio`). Every `tool_call_request` frame from the child is normalized to `gov.Action`, run through `gov.Gate.Evaluate(action, "copilot-cli", envelope)`, and either forwarded to the parent (allow) or refused with a synthesized refusal frame (deny). Refusal text encodes Reason + Suggestion + CorrectedCommand for model visibility — exact frame shape resolved by spike (see Open Questions).
- **`chitin-kernel gate evaluate --hook-stdin --agent=claude-code` mode** — cold-start subprocess invoked per Claude Code tool call via `~/.claude/settings.json` `PreToolUse` hook. Reads PreToolUse JSON from stdin, normalizes to `gov.Action`, evaluates, writes Claude Code's expected response (exit 0 + empty for allow; exit 2 + `{"decision":"block","reason":"..."}` for deny). Reason field is model-visible (UX win over v1 SDK driver).
- **`internal/driver/claudecode/`** — new package. Tool-name normalizer (Bash → terminal-rerouted shell.exec; Edit/Write/NotebookEdit → file.write; Read → file.read; WebFetch/WebSearch → http.request; Task → delegate.task; Glob/Grep/LS/TodoWrite mapping resolved at impl time). Response formatter.
- **`chitin-kernel envelope` subcommand group** — `create --calls=N --bytes=N [--usd=N]`, `use <id>` (writes `~/.chitin/current-envelope`), `inspect <id>`, `list`, `grant <id> --calls=+N --bytes=+N`, `close <id>`. Operator-facing.
- **`chitin-kernel envelope tail [<id>] [--stats]`** — line-formatter on the audit log JSONL stream. inotify on Linux; poll fallback. Replaces v2's TUI watch dashboard. ~50 lines of Go.
- **`chitin-kernel install acpx-override [--profile=<name>] [--dry-run]`** — idempotent JSON merge into `~/.openclaw[-<profile>]/openclaw.json`. Backup-on-write. Mirrored `uninstall acpx-override`.
- **`chitin-kernel install claude-code-hook [--global|--project] [--dry-run]`** — idempotent JSON merge into `~/.claude/settings.json` or `.claude/settings.json`. Backup-on-write. Mirrored `uninstall claude-code-hook`.
- **Cold-start benchmark** — measure p50/p95/p99 of `chitin-kernel gate evaluate --hook-stdin` invocations on the operator's box. If p95 > 100ms, design daemon mode (`gate daemon` listening on `~/.chitin/gate.sock`) before shipping. Otherwise, ship cold-start.
- **Multi-agent live integration test** — N parallel Copilot ACP shims under one envelope; sibling-deny on exhaustion; audit-log integrity under concurrent writers.

### Out of scope

- **T0 execution by chitin.** Chitin does not execute tool calls. Within-session execution stays with whoever owns the session (openclaw harness for ACP path, Claude Code for hook path). T0 is an audit-log label informing future routing tools, not an execution branch.
- **Session-spawn routing.** "Choose which agent (cheap or expensive) to spawn for a given task" is the cost-saving lever, but it lives at openclaw's `sessions_spawn` boundary, not in the kernel. Deferred to ecosystem phase per `project_strategic_roadmap.md`.
- **`BudgetUSD` as a primary cap.** Per-token rates are partially fictional for Copilot CLI's flat-rate subscription model. The `BudgetUSD` field is informational; real-time enforcement is on calls + bytes. Real $USD reconciliation deferred to OTEL ingest.
- **`chitin-kernel watch` TUI dashboard.** Replaced by `envelope tail` line formatter. The TUI was a stage prop for the talk; without that pressure it doesn't earn its build cost.
- **`chitin-kernel swarm` subcommand.** v2's fallback for "if openclaw misbehaves on stage." Dropped entirely. Openclaw is the orchestrator — no fallback path needed in the kernel.
- **T1 (Haiku-class cheap cloud).** Reserved enum value; no implementation. Post-kernel work.
- **Cross-session memory (claude-mem-shape, memU).** Deferred to substrate roadmap. The talk's demo beats are designed to be self-contained per session.
- **AST-deterministic pipelines, prompt/tool/artifact cache, preflight task pruning, cost-feedback auto-tuner.** All explicitly post-kernel per memo.
- **Confidence-based T0→T2 escalation.** v2's `meta.confidence < threshold` path is moot now that T0 is a label. Tier classification is rule-based only.
- **OTEL ingest from openclaw / Claude Code.** Stays on the SP-1 ingest roadmap. The audit log is the kernel's source of truth.
- **Modifying openclaw, acpx, Claude Code, or any plugin code.** Everything we need from upstream is reachable via documented config schemas.
- **Talk runbook.** Talk is covered by existing artifacts.
- **Readybench / bench-devs content.** Chitin is OSS (`memory/feedback_chitin_oss_boundary.md`).

## Architecture

```
operator: chitin-kernel envelope create --calls=500 --bytes=5MB  →  ULID 01J...
operator: chitin-kernel envelope use 01J...                       →  ~/.chitin/current-envelope
operator: chitin-kernel install claude-code-hook --global         →  ~/.claude/settings.json
operator: chitin-kernel install acpx-override                     →  ~/.openclaw/openclaw.json
operator: chitin-kernel envelope tail &                           →  tails ~/.chitin/gov-decisions-<today>.jsonl

──────────────────────────────────────────────────────────────────────────────
Path A — Claude Code session (interactive)
──────────────────────────────────────────────────────────────────────────────

operator: claude
   │
   └─ Claude Code agent loop. On every tool use:
        │
        └─ PreToolUse hook fires:
              chitin-kernel gate evaluate --hook-stdin --agent=claude-code
                │
                ├─ resolve envelope: --envelope flag → env var → ~/.chitin/current-envelope
                ├─ read PreToolUse JSON from stdin
                ├─ normalize to gov.Action
                ├─ tier.Route(action) → T0 or T2
                ├─ cost.Estimate(action, executor, rates) → CostDelta
                ├─ envelope.Spend(estimate) → ok or ErrEnvelopeExhausted
                ├─ gov.Gate.Evaluate(action, "claude-code", envelope) → Decision
                ├─ append Decision to ~/.chitin/gov-decisions-<today>.jsonl
                └─ exit 0 (allow) or exit 2 + JSON{decision:"block",reason:"..."} (deny)

──────────────────────────────────────────────────────────────────────────────
Path B — Copilot ACP via openclaw orchestrator
──────────────────────────────────────────────────────────────────────────────

operator: openclaw agent --message "burn down issues #101 #102 #103"
   │
   └─ openclaw runs an agent turn that calls sessions_spawn 3x with runtime: "acp", agentId: "copilot"
        │
        └─ acpx plugin reads override from config and spawns N parallel:
             ┌────────────────────────────────────────────────────────────────────┐
             │ chitin-kernel drive copilot --acp --stdio --envelope=$CHITIN_…    │
             │                                                                    │
             │   ── speaks ACP to openclaw over stdio (parent)                    │
             │   ── speaks ACP to copilot --acp --stdio over stdio (child)        │
             │                                                                    │
             │   on each tool-call request from child:                            │
             │     1. normalize ACP frame → gov.Action                            │
             │     2. tier.Route(action) → T0 or T2 (label)                       │
             │     3. cost.Estimate(action, "copilot-cli", rates) → CostDelta     │
             │     4. envelope.Spend(estimate) → ok or ErrEnvelopeExhausted       │
             │     5. gov.Gate.Evaluate(action, "copilot-cli", envelope)          │
             │        ── allow → forward request to parent (openclaw harness)     │
             │        ── deny → synthesize ACP refusal frame back to child        │
             │     6. append Decision to gov-decisions-<today>.jsonl              │
             └────────────────────────────────────────────────────────────────────┘

  N parallel chitin shims, all sharing ~/.chitin/gov.db envelope row
  + ~/.chitin/gov-decisions-<today>.jsonl

──────────────────────────────────────────────────────────────────────────────
Tail (separate process; read-only consumer)
──────────────────────────────────────────────────────────────────────────────

chitin-kernel envelope tail
    └─ tails the JSONL, prints decoded decision lines + periodic envelope-stats
```

The chitin shim/hook is a permission gate. It does not execute tool calls. The audit log is the source of truth; `envelope tail` is a strict consumer of the JSONL and never gates anything.

### Why permission-gate, not tool harness

When openclaw orchestrates a Copilot ACP session, openclaw IS the harness — it executes file reads, git commands, shell, etc. The Copilot model decides "I want to read this file"; openclaw's harness does the read; file contents flow back as input tokens for Copilot's next turn. The token cost is what Copilot pays to ingest the contents — not the read itself. **Moving the read from openclaw's harness to chitin's harness is a wash.** No cost saving exists at the within-session execution layer.

The same logic applies to Claude Code: Claude Code itself executes. Chitin gates.

The real cost levers are: (a) hard cap on calls + bytes (real-time enforceable in the gate); (b) per-session agent selection at spawn time (openclaw's territory; deferred); (c) post-hoc reconciliation via OTEL ingest (deferred). Tier on the Decision row tells future tools which action classes are cheap so spawn-time routing can be informed by audit history — but the kernel itself does not act on Tier.

### Why sqlite, not flat-file JSON

`gov.Counter` (the existing escalation counter) already lives in `~/.chitin/gov.db`. Adding envelope tables to the same db is consistent: one storage mechanism, one set of operator backup/inspect/migrate concerns. Sqlite WAL handles cross-process concurrent write semantically with no flock dance. The query patterns operators want (`envelope inspect`, `envelope list`, post-hoc analysis) are SQL-shaped; building them on JSON-per-envelope means re-implementing query logic in Go. v2's flat-file rationale was talk-pressure-driven; with that pressure off, sqlite is the right call from the start.

### Why calls + bytes, not $USD

Copilot CLI doesn't bill per-token in user-visible terms — it's a flat-rate subscription with monthly "premium request" caps. Anthropic API does have per-token rates, but `PreToolUse` fires before any model response, so the hook driver can only see what's about to be sent, not what comes back. Local Ollama is $0. The data we can count cleanly in real time is: tool calls (one per Decision) and input bytes (action.Target length + any payload). The data we can't count cleanly is the actual model token usage and the actual $ paid. So the cap is on what we can count; the $ is informational. Real $USD reconciliation needs OTEL ingest of model.usage data — when that lands, the schema is already shaped to accept it.

### Why current-envelope file, not env var

Setting an env var per shell session is friction. Operators want kubectl-style "current context" — set once, picked up by every tool below. `chitin-kernel envelope use 01J…` writes the file; the hook command, the ACP shim, and any future driver all resolve the same way. Env var still works as override (and is what the openclaw acpx config substitutes per spawn), but the file is the day-to-day surface.

## Components

### `gov/budget.go` (new)

```go
type BudgetEnvelope struct {
    ID      string  // ULID
    DB      *sql.DB // shared *sql.DB pointing at ~/.chitin/gov.db
    Limits  BudgetLimits
}

type BudgetLimits struct {
    MaxToolCalls   int64
    MaxInputBytes  int64
    BudgetUSD      float64  // informational
}

// LoadEnvelope opens or creates the envelope by ID.
func LoadEnvelope(db *sql.DB, id string) (*BudgetEnvelope, error)

// Spend debits. Returns ErrEnvelopeExhausted on cap breach (calls or bytes).
// Sticky-closed: once closed_at is set, subsequent Spends fail with
// ErrEnvelopeClosed regardless of remaining caps.
//
// Sqlite WAL handles cross-process atomicity. The Spend is a single
// UPDATE ... WHERE inside a transaction; on cap breach we set closed_at
// in the same transaction.
func (e *BudgetEnvelope) Spend(d CostDelta) error

// Inspect returns a snapshot. Read-only.
func (e *BudgetEnvelope) Inspect() (EnvelopeState, error)

// Grant raises caps. Logs to gov-decisions-*.jsonl as rule_id:operator-grant.
func (e *BudgetEnvelope) Grant(deltaCalls, deltaBytes int64, deltaUSD float64, reason string) error
```

ULID for envelope IDs so they sort by creation time in audit-log queries. Sqlite WAL means no flock at the application level. ENOSPC on disk → error returned (fail closed; never silently lose a debit).

### `gov/decision.go` (extended)

Existing `Decision` struct gains:

```go
EnvelopeID    string  `json:"envelope_id,omitempty"`
Tier          Tier    `json:"tier,omitempty"`
CostUSD       float64 `json:"cost_usd,omitempty"`     // informational
InputBytes    int64   `json:"input_bytes,omitempty"`
OutputBytes   int64   `json:"output_bytes,omitempty"`
ToolCalls     int64   `json:"tool_calls,omitempty"`
```

All `,omitempty` so existing audit-log readers tolerate. `WriteLog` marshalled-struct extended in lockstep.

### `gov/tier.go` (new)

```go
type Tier string
const (
    TierUnset    Tier = ""
    T0Local      Tier = "T0"
    T1Cheap      Tier = "T1"  // reserved; unused in this slice
    T2Expensive  Tier = "T2"
)
```

### `internal/cost/cost.go` (new)

```go
type CostDelta struct {
    USD          float64
    InputBytes   int64
    OutputBytes  int64
    ToolCalls    int64
}

type RateTable map[string]ExecutorRate  // keyed by executor, e.g. "copilot-cli"

type ExecutorRate struct {
    USDPerInputKtok   float64  // approximate; informational
    USDPerOutputKtok  float64  // approximate; informational
    BytesPerToken     float64  // default 4 (rough English)
}

// Estimate is tier-blind. Returns a CostDelta keyed to the executor's
// rate. Used for envelope.Spend and for the Decision's informational
// CostUSD field. The cap fires on InputBytes and ToolCalls, not USD.
func Estimate(action gov.Action, executor string, rates RateTable) CostDelta
```

Local-Ollama executor (e.g. `claude-code-local` when `ANTHROPIC_BASE_URL` points at localhost) has rate `{0, 0, 4}` — counts bytes for the cap, USD stays zero, honest.

### `internal/tier/tier.go` (new)

`Route(action gov.Action) gov.Tier` — switch on `action.Type` with one secondary check on `action.Params["sub_action"]` for `shell.exec`. Pure function. ~50 lines.

The rules live in code, not YAML, because they are kernel-level invariants (a YAML author should not be able to override "git.commit must classify T2" by setting `tier_hint: T0`). Future ecosystem-phase work may layer YAML hints on top, but the floor is in code.

### `internal/driver/copilot/acp/` (new)

Files:
- `shim.go` — top-level entry. `--acp --stdio` mode. Spawns child `copilot --acp --stdio`; bidirectional ACP frame proxy.
- `acp_decode.go` — minimal frame parser. Decoded shapes: `tool_call_request`, `tool_call_response`, `cancel`, `prompt`, `session_set_mode`. Unknown frames pass through opaque.
- `intercept.go` — per-frame `func(Frame) Frame` interceptor. Default impl is identity; governance hook composed on top.
- `intercept_governance.go` — on each `tool_call_request`: normalize → tier.Route → cost.Estimate → envelope.Spend → gov.Gate.Evaluate → forward or refuse. Refusal frame shape resolved by spike (Open Questions).

Invoked as `chitin-kernel drive copilot --acp --stdio --envelope=<id>`. Envelope ID accepted via `--envelope` flag, `CHITIN_BUDGET_ENVELOPE` env var, or `~/.chitin/current-envelope` file (in that precedence order).

### `internal/driver/claudecode/` (new)

Files:
- `normalize.go` — `Normalize(HookInput) → gov.Action`. Tool-name mapping per the hook driver spec. Bash → `gov.Normalize("terminal", ...)` for full shell re-tagging. Edit/Write/NotebookEdit → file.write. Read → file.read. WebFetch/WebSearch → http.request. Task → delegate.task. Glob/Grep/LS/TodoWrite → resolved at impl time.
- `format.go` — `Decision → (stdout JSON, exit code)` for Claude Code's hook protocol contract.

`gate evaluate --hook-stdin --agent=claude-code` extends the existing `gate evaluate` subcommand with a stdin-reading mode. Cold-start subprocess per call.

### `cmd/chitin-kernel/envelope.go` (new)

Cobra subcommand group: `envelope`. Subcommands:
- `create --calls=N --bytes=N [--usd=N]` — emits ULID to stdout for shell capture.
- `use <id>` — atomic write of `~/.chitin/current-envelope` (write-tmp + rename).
- `inspect <id>` — JSON dump of envelope state.
- `list` — recent envelopes, formatted table.
- `grant <id> --calls=+N --bytes=+N [--usd=+N]` — raise caps. Logs `rule_id: operator-grant` to audit.
- `close <id>` — operator-close. Subsequent Spends fail with `ErrEnvelopeClosed`.

### `cmd/chitin-kernel/envelope_tail.go` (new)

`envelope tail [<id>] [--stats]`. Tails `~/.chitin/gov-decisions-<today>.jsonl` (inotify on Linux; poll fallback). Per-Decision line:
```
2026-04-29T15:01:02Z  claude-code  T0  $0.000   file.read /path/...     ALLOW
```
With `--stats`, periodic envelope-summary line every N decisions:
```
[stats] envelope 01J-X: calls 47/500, bytes 2.3MB/5.0MB, $0.32 (informational), denials 0
```

Replaces v2's TUI watch. ~50 lines. Composes with shell tools (grep/jq/awk) the way TUIs don't.

### `cmd/chitin-kernel/install_acpx.go` (new)

Cobra: `install acpx-override [--profile=<name>] [--dry-run]`. Reads `~/.openclaw[-<profile>]/openclaw.json`. Writes the canonical override (`plugins.entries.acpx.config.agents.copilot.command`). Backup `<path>.chitin-backup-<ts>`. Idempotent. Refuses to overwrite a non-chitin override on the same agent.

### `cmd/chitin-kernel/install_claude_code.go` (new)

Cobra: `install claude-code-hook [--global|--project] [--dry-run]`. Reads `~/.claude/settings.json` or `.claude/settings.json`. Idempotent JSON merge of the chitin hook block:
```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "Bash|Edit|Write|NotebookEdit|Read|WebFetch|WebSearch|Task",
      "hooks": [{
        "type": "command",
        "command": "chitin-kernel gate evaluate --hook-stdin --agent=claude-code"
      }]
    }]
  }
}
```
Backup-on-write. Idempotent. Refuses to overwrite a non-chitin matcher on the same trigger.

### Audit log extensions

`gov-decisions-<date>.jsonl` schema gains six optional fields (already named in `gov/decision.go` extensions): `envelope_id`, `tier`, `cost_usd`, `input_bytes`, `output_bytes`, `tool_calls`. Existing readers tolerate via `,omitempty`. Concurrent multi-process write is OK on Linux for ≤PIPE_BUF (4 KiB) lines via O_APPEND atomicity; one Decision line is well under that.

## Data flow

All flows assume the operator has run setup once: `envelope create`, `envelope use <id>`, `install claude-code-hook --global`, `install acpx-override`.

### Flow A — Claude Code allow under cap

1. Operator runs `claude` and asks it to read a file.
2. Claude Code emits a `PreToolUse` event with `tool_name: "Read", tool_input: {file_path: "..."}`.
3. The hook fires `chitin-kernel gate evaluate --hook-stdin --agent=claude-code`.
4. Hook command resolves envelope: precedence chain finds `~/.chitin/current-envelope`.
5. Normalize input → `gov.Action{Type: file.read, Target: "..."}`.
6. `tier.Route → T0Local`.
7. `cost.Estimate → CostDelta{InputBytes: 0, ToolCalls: 1, USD: 0}`.
8. `envelope.Spend(estimate)` — sqlite UPDATE inside transaction, ~ms.
9. `gov.Gate.Evaluate → Decision{Allowed: true, Tier: T0, EnvelopeID: "01J…", InputBytes: 0, ToolCalls: 1}`.
10. `WriteLog(decision)`. Hook exits 0; Claude Code reads the file.
11. `envelope tail` updates with the decision line; `--stats` increments `calls`.

### Flow B — Claude Code deny on calls cap

1. Envelope is at 499/500 calls. Claude Code emits another tool call.
2. Hook fires; resolves envelope; normalizes; routes; estimates `{ToolCalls: 1}`.
3. `envelope.Spend` → debits to 500/500 → returns `ErrEnvelopeExhausted` and sets `closed_at` in the same transaction (sticky).
4. Hook exits 2 with `{"decision":"block","reason":"envelope 01J… exhausted (500/500 calls)"}`.
5. Claude Code displays the reason to the model. Model can ask the operator to grant more.
6. Operator runs `chitin-kernel envelope grant 01J… --calls=+100`. Envelope reopens (closed_at cleared, max_tool_calls raised); next call allowed.

### Flow C — Copilot ACP allow under cap

1. Operator runs `openclaw agent --message "..."`. Openclaw runs a turn that calls `sessions_spawn({runtime: "acp", agentId: "copilot"})`.
2. acpx reads the override and spawns `chitin-kernel drive copilot --acp --stdio --envelope=01J…` with `CHITIN_BUDGET_ENVELOPE` substituted.
3. Chitin shim spawns child `copilot --acp --stdio` and proxies frames.
4. Child Copilot's first tool-call request: `Action{Type: file.read, Target: "..."}`.
5. Tier route → T0; cost.Estimate; envelope.Spend; gov.Gate.Evaluate → Decision allow.
6. Frame forwarded to parent (openclaw); openclaw's harness reads the file; response forwarded back to child.
7. WriteLog appended to `gov-decisions-<today>.jsonl`.

### Flow D — Copilot deny on cap with sibling propagation

1. Three Copilot agents are running in parallel under one envelope. Envelope at 498/500 calls.
2. Agent A's child requests another tool call.
3. Agent A's shim does `envelope.Spend` — debits to 499/500. Allow.
4. Agent B's child requests a tool call.
5. Agent B's shim `envelope.Spend` debits to 500/500, sets closed_at, returns `ErrEnvelopeExhausted`.
6. Agent B's shim synthesizes ACP refusal frame back to child Copilot.
7. Agent C's next `envelope.Spend` reads `closed_at != null`, immediately fails with `ErrEnvelopeClosed`. Same refusal flow.
8. Audit log shows three agents' decisions all stamped with same `envelope_id`. Mix of allows and one budget-exhausted Decision; ≥1 propagated denial.
9. Operator can `envelope grant` to resume.

### Flow E — Operator cancels mid-session

1. Operator hits Ctrl+C in openclaw (or runs `/acp cancel`).
2. Openclaw sends ACP `cancel` frames to each child session. Each chitin shim forwards the cancel frame to its underlying child Copilot.
3. In-flight tool calls complete and log; subsequent requests are answered with cancellation refusal.
4. Each shim exits with code 130 (SIGINT convention) when its child exits.
5. Envelope is left open (not closed) so the operator can resume by spawning new agents against the same envelope ID.

### Flow F — Spawn fails preflight

1. Operator misconfigured: `CHITIN_BUDGET_ENVELOPE` set to an envelope ID that doesn't exist on disk.
2. acpx spawns the chitin shim; shim's `LoadEnvelope("01J-bad")` returns ENOENT.
3. Shim emits an ACP error frame to openclaw with `Reason: "envelope not found"` and exits 1.
4. Openclaw marks the spawn as failed; reports to the operator.
5. No partial state. No silent fallback.

(Same applies to the Claude Code hook: if the resolved envelope ID doesn't exist, hook exits 2 with reason "envelope not found"; Claude Code surfaces to model.)

## Substrate decisions

### Openclaw (ADOPT, primary orchestrator)

Unchanged from v2. Production-grade, OSS, native parallel execution on `subagent` lane (default cap 8). Configurable acpx agent-command override. Smoke-tested 2026-04-28; live e2e is the first task of Milestone B. `npm install -g openclaw@2026.4.25`. Zero upstream PRs needed.

### Lobster (BORROW, post-kernel)

v2 borrowed JSON-Schema-validated agent outputs and `name + steps` shape into the swarm subcommand. Swarm subcommand is dropped in v3, so the borrow becomes lat. 90-day revisit if Lobster ships pre-step middleware/hook API.

### memU (BORROW, post-kernel)

Unchanged. Borrowing the resource → item → category hierarchy as the post-kernel index over `gov-decisions-*.jsonl`. Not borrowing storage layer or SDK.

### Composio (SKIP)

Unchanged. Skipping. Default-path credential custody in their cloud violates the OSS boundary; hosted execution downgrades `gov.Gate` to a pre-flight intent check.

### claude-mem (acknowledge, post-kernel)

Surfaced 2026-04-29 as a candidate for cross-session memory continuity. Functionally analogous to memU's role; same post-kernel deferral.

## Self-review

### Placeholder scan

No TBD / TODO. `<placeholder>` tokens used only for documented field substitutions (`<id>`, `<date>`, `<today>`, `<name>`, `<profile>`).

### Internal consistency

- Permission-gate (no execution by chitin) is consistent across §Preamble, §Architecture, §Components, §Data flow, §Why permission-gate.
- Tier-as-label is consistent across §Preamble, §Components/tier, §Components/cost, §Data flow.
- Calls + bytes as primary cap is consistent across §Scope, §Components/budget, §Why calls + bytes, §Data flow.
- Sqlite-backed envelope is consistent across §Scope, §Components/budget, §Why sqlite.
- Two driver surfaces under one set of state is consistent across §Preamble, §Architecture, §Components/{copilot/acp,claudecode}, §Data flow.

### Scope check

- Single coherent kernel slice. Reuses `gov.Gate`, `gov.Counter` sqlite layer, `gov-decisions-*.jsonl` shape, escalation counter — unchanged.
- Two new packages (`internal/cost/`, `internal/tier/`) plus driver-specific packages (`internal/driver/copilot/acp/`, `internal/driver/claudecode/`) plus six new fields on Decision plus new sqlite tables. Bounded.
- All execution-side work (T0 transport, session-spawn routing, cost-feedback auto-tuner, prompt cache, AST pipelines, preflight pruning) explicitly out of scope.

### Ambiguity check

- "Cancel propagation across N shims" — when envelope's `closed_at` is set, sibling shims discover this on their next `envelope.Spend` (which reads the row). No out-of-band signal. Fine because every shim does at least one Spend per second under realistic loads. For long-idle sessions, post-kernel: add a sqlite NOTIFY-shaped trigger (or polling watcher) so shims react within a frame.
- "Bytes vs tokens" — `InputBytes` is what we count; tokens are bytes/4 for English approximation. The cap fires on bytes. The informational `CostUSD` uses bytes-per-token from the rate table. Honest about the approximation.
- "ACP frame parsing scope" — the chitin shim is a *minimal* ACP proxy. It only parses frame shapes that touch tool calls. Other frame types are forwarded as opaque bytes. Confirm against openclaw's `docs/cli/acp.md` compatibility matrix at impl time.
- "Hook latency" — cold-start of `chitin-kernel gate evaluate --hook-stdin` may be 50-200ms depending on box. Bench task in Milestone C decides daemon mode. Acceptable to ship cold-start if p95 < 100ms.

### Out-of-scope leak check

- No changes to closed-enum action vocabulary. Fields added to Decision; Action.Type is unchanged.
- No new gov rules in `chitin.yaml` for this slice. Envelope is enforced in `gov.Gate.Evaluate` plumbing, not via the policy DSL.
- v1 SDK driver is unchanged. v3 adds a new `--acp --stdio` mode and a new `--envelope` flag; SDK mode preserved.
- No openclaw, acpx, Claude Code, Lobster, memU, or Composio code lands in this slice. Only chitin code.
- Hook driver spec stays as-is; this spec consumes its design and adds envelope plumbing. Spec `2026-04-28-claude-code-hook-driver-design.md` may need a small clarifying patch to mention envelope integration.

## Open questions for plan phase

1. **ACP refusal-frame visibility.** Does the child Copilot model see the chitin Reason text when the shim returns a refusal frame? Or do we need to inject a synthetic tool *response* with the Reason embedded? Resolve via 30-min spike against captured ACP transcript before any Milestone B intercept code is written. Document in `docs/observations/acp-refusal-shape.md`.
2. **Cold-start latency for Claude Code hook.** Bench `chitin-kernel gate evaluate --hook-stdin` p50/p95/p99 on the operator's box. If p95 > 100ms, design daemon mode (`gate daemon` listening on `~/.chitin/gate.sock`). If p95 ≤ 100ms, ship cold-start.
3. **Glob/Grep/LS/TodoWrite mapping.** Default-allow as browse tools, default-deny as fail-closed unknowns, or new action types? Resolve in Milestone C when normalize.go is being written. Recommendation: default-allow with explicit baseline rules, since they're read-shaped.
4. **Sqlite migration story.** `gov.db` already has the counter table. Adding envelope tables is additive; need a versioned migration runner. Define in Milestone A. May reuse whatever `gov.Counter` already does, if anything.
5. **Tier rule table.** With T0 as pure label, do we keep the full rule list (every action type tagged) or trim to a minimum set? Recommendation: keep full — cheap to compute, produces metadata for future routing analytics.
6. **Audit log rotation/retention.** Daily JSONL is the convention. Multi-process concurrent write is OK on Linux for ≤PIPE_BUF lines. Rotation/compress/archive policy — define in Milestone D.
7. **Default envelope on `envelope create` without explicit caps.** Recommend `--calls=500 --bytes=5MB` as a safe default. Operator can raise. Lower than typical demo-scale to avoid surprises.
8. **Envelope file flock contention under N agents.** Sqlite WAL handles N readers + 1 writer. With 8 sibling shims doing Spend on the same row under WAL, contention is per-row; tested in Milestone D stress test (8 parallel processes).

## Branch + worktree

Per `memory/feedback_always_work_in_worktree.md`:
- Spec branch: this file lands on `main` (consistent with v1/v2 spec commits).
- Implementation branch when planned: `feat/cost-governance-kernel` off `main`.
- Worktree: `~/workspace/chitin-cost-governance/`.
- v2 spec stays in `docs/superpowers/specs/` as historical record; do not delete.
