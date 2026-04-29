# Cost-Governance Kernel — Design

**Date:** 2026-04-28
**Status:** Design v2 (revised 2026-04-28 evening after openclaw 2026.4.25 smoke test). Not yet planned; not yet implemented.
**Forcing function:** 2026-05-07 tech talk. ~9 days. The talk reframes from "Copilot CLI Without Fear" to "Compute-amortized intelligence: how chitin makes AI dev work bounded and predictable." This spec is the kernel slice that backs the new framing; without it the talk has no demo.
**Parent decisions:**
- The "compute-amortized intelligence" memo (this session, 2026-04-28): hard runtime budgets, compiler-style model tiering (T0 local → T2 expensive), LLM-as-exception-handler, deterministic-first pipelines, aggressive caching, preflight pruning, observability cost feedback.
- Substrate spike findings (this session, 2026-04-28): Lobster BORROW (YAML+JSON-Schema pipeline shape; no governance hook, no OTEL, TS-only), memU BORROW (hierarchical memory; wrong language, wrong cache problem), Composio SKIP (cloud credential custody, opaque execution). Build chitin-native; revisit substrates 90 days post-talk.
- **v2 reversal: openclaw is back as the orchestrator.** Re-evaluation against `openclaw@2026.4.25` (released 2026-04-25, three days ago) showed:
  - Blocker 1 (hardcoded agent launcher) is **resolved upstream**. The bundled `acpx` plugin's config schema exposes `plugins.entries.acpx.config.agents.<id>.command` for per-agent spawn-command overrides. Verified by smoke test in an isolated profile this session: schema accepts the override, `openclaw config validate` passes, value reads back unchanged. Config path documented in `dist/extensions/acpx/openclaw.plugin.json` and `docs/cli/acp.md`.
  - Blocker 2 (broken OTEL diagnostic-event) is **materially narrowed but not on critical path**. The 2026.4.25 changelog records "OpenTelemetry coverage expands across model calls, token usage, tool loops, harness runs, exec processes..." The remaining gap on non-auto-reply paths persists per upstream PR #21290's own description, but chitin does not need openclaw's OTEL pipe — chitin's own `gov-decisions-*.jsonl` audit log already carries every decision and is extended in this spec to also carry tier + cost.
  - **Upgrade required:** `npm install -g openclaw@2026.4.25` (bumped from 2026.4.15 this session).
- `gov.Gate` (PR #45 + #51, merged 2026-04-28): the closed-enum action vocabulary, policy evaluator, audit log, escalation counter, and `Agent` field on `Decision` already exist. This spec extends them; it does not replace them.
- `chitin-kernel drive copilot` (PR #51): the per-agent governance shim. v2 adds an `--acp --stdio` mode so that openclaw's acpx plugin can spawn `chitin-kernel drive copilot --acp --stdio` in place of raw `copilot --acp --stdio`. Inside that shim, governance + tier routing + budget enforcement run before each tool call.
- `libs/adapters/ollama-local/`: the T0 transport. Already scaffolded; this spec defines the calling shape but does not reimplement the adapter.
- **Two-driver pattern memory (`memory/project_two_driver_pattern.md`)** is upheld and extended. Open-vendor (Copilot) is in-process via SDK in v1; v2 adds the orchestrator shim — same `gov.Gate` semantics, new spawn-time interception surface.

## Preamble

Today, an agent run inside chitin is governed at the *action* boundary — `gov.Gate.Evaluate(action)` decides whether each tool call is allowed. That is necessary but not sufficient. It does not decide *which model should serve the action*, it does not enforce a *cost ceiling*, and it does not detect *runaway loops* before they exhaust a budget. The compute-amortized intelligence memo is explicit: the kernel must own these decisions at runtime, not the agent and not the YAML author.

This spec defines the kernel slice that adds three invariants on top of the existing `gov.Gate`:

1. **Bounded cost.** No agent run can exceed its declared `budget_usd` / `max_tokens` / `max_tool_calls` without explicit escalation; the gate denies the next call when any cap is exhausted.
2. **Compiler-tier routing.** Every action is routed to T0 (local Ollama) or T2 (Copilot CLI via the existing driver) by a deterministic rule table; T2 is the escalation path, not the default.
3. **Swarm under one envelope.** Multiple agents spawned under a shared budget and a shared audit log; the dashboard makes burn rate visible on stage.

**v2: openclaw orchestrates the swarm.** Openclaw 2026.4.25's acpx plugin spawns N parallel Copilot ACP sessions on its `subagent` lane (default cap 8, configurable). Each spawn is intercepted by `chitin-kernel drive copilot --acp --stdio` via a one-line acpx config override. Inside the chitin shim, gov.Gate + tier router + budget envelope all run before each tool call. The shared budget envelope lives in `~/.chitin/budget-<id>.json` so cross-process children debit the same pool. The audit log stays the same JSONL it already is, now extended with tier + cost fields.

The chitin-kernel does not own orchestration shape (workflow DSL, fan-out semantics, session lifecycle) — openclaw owns that. The chitin-kernel owns the governance and cost invariants per spawn. This is the right separation: openclaw is the production-grade orchestrator, chitin is the kernel-layer differentiator.

A `chitin-kernel swarm` subcommand exists as a fallback for the demo if openclaw misbehaves on stage; it is not the primary path.

This is the irreducible slice for the talk. T1 (cheap-cloud Haiku-tier), AST-deterministic pipelines (memo §3 in full), the prompt/tool/artifact cache (memo §5), preflight task pruning (memo §6), and the cost-feedback auto-tuner (memo §9) are all explicitly post-talk work. They are not blockers for proving the principle.

## One-sentence invariant

Once openclaw is configured with the chitin acpx override and a `gov.BudgetEnvelope` is active in `~/.chitin/budget-<id>.json`, every Copilot CLI session openclaw spawns is intercepted by `chitin-kernel drive copilot --acp --stdio`, every tool call inside that session is routed to T0 or T2 by a deterministic table, every action is gated through `gov.Gate.Evaluate` *and* a budget check before execution, every decision and tier-routing event lands in the shared `gov-decisions-<date>.jsonl` audit log with a `tier` + `cost_usd` field, and the next spawn or tool call denies within one round-trip of the shared envelope being exhausted.

## Scope

### In scope

- **`gov.BudgetEnvelope`** — new type carrying `ID`, `BudgetUSD`, `MaxTokens`, `MaxToolCalls`, with a `Spend(cost CostDelta) error` method that returns `ErrBudgetExhausted` when any cap is breached. **Cross-process by design** (openclaw spawns each ACP session as a separate subprocess). Authoritative state lives in `~/.chitin/budget-<id>.json`, written atomically (write-tmp + rename). Every `Spend` call re-reads, debits, re-writes under an `flock(2)` advisory lock on the same path. SQLite-backed alternative deferred to post-talk; flat JSON + flock is sufficient for the demo and avoids a new schema migration.
- **`gov.CostDelta`** — `{USD float64, InputTokens, OutputTokens, ToolCalls int}`, the unit of debit against an envelope.
- **`gov.Tier` enum** — `T0Local`, `T2Expensive`. T1 reserved as a constant but unused for the talk.
- **`gov.Decision` extension** — add `EnvelopeID`, `Tier`, `CostUSD`, `InputTokens`, `OutputTokens` fields. JSONL writer extended to emit them. Existing readers ignore unknown fields (already documented contract).
- **`internal/cost/`** — new package. Holds the per-model rate table (T0 = 0 USD, T2 = Copilot CLI's published rate), the `Estimate(action, tier) → CostDelta` function (pre-call), and `Reconcile(action, actual usage) → CostDelta` (post-call from gov-decisions stream).
- **`internal/tier/`** — new package. `Route(action, opts) → Tier` decision function. Rule table:
  - `file.read`, `git.diff`, `git.log`, `git.status` → T0 (cheap, deterministic, local).
  - `shell.exec` with sub-action `parse|format|grep|find` → T0.
  - `delegate.task` with confidence < threshold → escalate T0 → T2.
  - `shell.exec` with sub-action `git.commit|gh.pr.create` → T2 (judgment).
  - Anything else → T2 default for talk; tighten post-talk.
  Confidence comes from the T0 model's emitted `meta.confidence` field; threshold is configurable (`tier.escalate_below_confidence`, default 0.6).
- **`gov.Gate.Evaluate` extension** — accept a `*BudgetEnvelope` parameter (nil = no budget enforcement, current behavior). Before returning `Allow`, calls `envelope.Spend(estimate)`. Returns the existing `Decision` with new `EnvelopeID` + `Tier` + `CostUSD` populated.
- **`chitin-kernel drive copilot --acp --stdio` mode (new flag).** This is the spawn target openclaw's acpx plugin invokes when the override is configured. Behavior: speak ACP over stdio (forward to the underlying real `copilot --acp --stdio` subprocess), but every tool-call request crossing the chitin shim is intercepted, run through `gov.Gate.Evaluate(action, "copilot-cli", envelope)` with the envelope loaded from `--envelope=<id>` flag (or env `CHITIN_BUDGET_ENVELOPE`), tier-routed, cost-estimated, decision-logged. Denials map to the ACP refusal shape; allow-with-correction maps to a corrected action. The model sees the chitin `Reason` + `Suggestion` text — same pattern as v1 SDK driver.
- **`chitin-kernel envelope` subcommand** — `create --usd=10`, `inspect <id>`, `list`, `grant <id> +5` (raise budget mid-run), `close <id>` (mark done). Operator-facing for setup + recovery. The `grant` operation is the "raise budget mid-demo" recovery path called out in §Open questions.
- **`chitin-kernel watch` subcommand** — TUI tailing `gov-decisions-*.jsonl`. Per-agent panes: tier breakdown, USD burned, tool calls used, denials, escalations. Top header: total burn, time elapsed, predicted finish at current rate. Bubble Tea or plain ANSI — implementation choice in plan phase, not spec phase.
- **`chitin-kernel swarm` fallback subcommand** — reads a YAML swarm plan (Lobster-shaped: typed steps, JSON-Schema-validated outputs), spawns N children as goroutines, each invoking the existing `chitin-kernel drive copilot` with `--envelope=<id>`. Used **only** as a demo fallback if openclaw misbehaves on stage. Implementation order: ship `drive copilot --acp --stdio` first; if time permits, the swarm subcommand is a 1-day add. If time short, drop it from v1; the openclaw path is the talk demo.
- **`acpx config installer`** — new `chitin-kernel install acpx-override [--profile=<name>]` subcommand that idempotently writes the canonical override into the operator's openclaw config:
  ```
  plugins.entries.acpx.config.agents.copilot.command = "chitin-kernel drive copilot --acp --stdio --envelope=$CHITIN_BUDGET_ENVELOPE"
  ```
  Backs up the original. Shows diff before applying. `--profile=<name>` writes to `~/.openclaw-<name>/openclaw.json`. Mirror `uninstall` for reversibility. This is the operator-facing "wire chitin to openclaw" command.
- **Live-tag integration test** — full e2e against a real openclaw 2026.4.25 install: spawn 3 parallel Copilot ACP sessions via openclaw `sessions_spawn` on the subagent lane, all sharing one envelope, one is forced to budget-exhaust, the other two next-tool-calls deny within one round-trip, audit log contains all decisions tier-tagged + cost-tagged.
- **Talk runbook** — `docs/superpowers/runbooks/2026-05-07-talk-runbook.md`. Three demo beats: openclaw fan-outs 3 Copilot agents on stage (real chitin issues); chitin watch shows live cost burn + tier decisions per agent; one agent's runaway hits the envelope cap and the other two next-action denies — total spent shown on stage.

### Out of scope

- **T1 tier (Haiku-class cheap cloud).** Wire as a constant for forward-compat; do not implement routing. Post-talk.
- **AST-deterministic pipelines.** "AST parse → diff → LLM fills missing fragment → re-validate" (memo §3) is a substantial body of work and not needed to demonstrate tier routing. Post-talk.
- **Prompt/tool-call/artifact cache.** memU-shape resource→item→category index over `gov-decisions-*.jsonl`. Post-talk.
- **Preflight task pruning** (memo §6). Reject low-value tasks before they spawn. Post-talk.
- **Cost-feedback auto-tuner** (memo §9). Auto-routing tuned by historical cost-per-success. Post-talk.
- **Confidence scoring beyond a model-emitted `meta.confidence` field.** No classifier, no calibration, no learned router. Threshold is a single configurable constant.
- **OTEL ingestion from openclaw.** Per v2 parent-decisions block: openclaw 2026.4.25's OTEL coverage is materially better than 2026.4.15 but not the source of truth for the talk. Chitin's audit log is. SP-1 ingest of openclaw's OTEL stays on the post-talk roadmap.
- **Modifying openclaw, acpx, or any plugin code.** Everything we need from openclaw is reachable via the documented config schema. Zero upstream PRs in this slice.
- **Claude Code hook driver** (separate untracked spec). Defer to post-talk; the openclaw-orchestrated swarm of Copilot is sufficient for the talk and Claude Code is a parallel third surface.
- **Lobster / memU / Composio adoption.** Confirmed BORROW / BORROW / SKIP this session. 90-day revisit cycle scheduled post-talk, not now.
- **Multi-host budget envelopes.** The `~/.chitin/budget-<id>.json` + flock approach is single-host. Cross-host coordination (one envelope governing agents on multiple boxes) is post-talk.
- **Readybench / bench-devs content.** Chitin is OSS (`memory/feedback_chitin_oss_boundary.md`).

## Architecture

```
operator: chitin-kernel envelope create --usd=10  →  $CHITIN_BUDGET_ENVELOPE=01J...
operator: chitin-kernel install acpx-override     →  writes plugins.entries.acpx.config.agents.copilot.command
operator: chitin-kernel watch &                   →  tails ~/.chitin/gov-decisions-<date>.jsonl

operator: openclaw agent --message "burn down issues #101 #102 #103"
   │
   └─ openclaw runs an agent turn that calls sessions_spawn 3x with runtime: "acp", agentId: "copilot"
        │
        └─ acpx plugin (bundled in openclaw) reads override from config and spawns:
             ┌────────────────────────────────────────────────────────────────────┐
             │ chitin-kernel drive copilot --acp --stdio --envelope=$CHITIN_…    │
             │                                                                    │
             │   ┌── speaks ACP to openclaw over stdio (parent)                   │
             │   └── speaks ACP to copilot --acp --stdio over stdio (child)       │
             │                                                                    │
             │   on each tool-call request from child:                            │
             │     1. tier.Route(action) → T0 or T2                               │
             │     2. cost.Estimate(action, tier) → CostDelta                     │
             │     3. envelope.Spend(estimate)  // load+flock+debit+save          │
             │          ├─ exhausted → return ACP refusal with envelope-exhausted │
             │          └─ ok → continue                                          │
             │     4. gov.Gate.Evaluate(action, "copilot-cli", envelope)          │
             │          ├─ deny → return ACP refusal with reason+suggestion       │
             │          └─ allow → forward request to child                       │
             │     5. on response: cost.Reconcile(estimate, actual)               │
             │     6. envelope.Spend(reconciled - estimate)                       │
             │     7. append Decision{envelope_id, tier, cost_usd, …} to JSONL    │
             └────────────────────────────────────────────────────────────────────┘

  N parallel chitin shims, one per Copilot ACP session, all sharing
  ~/.chitin/budget-<id>.json + ~/.chitin/gov-decisions-<date>.jsonl

chitin-kernel watch (separate process; read-only tailer)
    └─ tails the JSONL, renders per-agent panes + envelope header on stage
```

The orchestrator is openclaw. It owns the workflow shape, the fan-out (default cap 8 on the subagent lane, configurable), session lifecycle, and ACP transport. The chitin shim does NOT own orchestration — it owns governance and cost on a per-spawn basis. The watch subcommand is a strict consumer of the JSONL stream; it has no privileges and never gates anything.

### Why openclaw orchestrates, not chitin

Three reasons. First, openclaw already has a production-grade fan-out primitive (`sessions_spawn` on the subagent lane). Re-implementing this in chitin is wasted code. Second, the Lobster/memU/Composio spike showed that the orchestration layer is *not* chitin's differentiator — the kernel layer (governance + cost + audit) is. Third, dogfooding: the user's actual production workflow uses openclaw; the talk demos the same setup the user runs every day, not a synthetic chitin-only swarm.

### Why a single envelope, not per-agent budgets

Per-agent budgets are the obvious shape but they hide the real failure mode: one runaway child consumes its share, the others stay under budget, and the swarm "succeeds" while burning $X for no work. The envelope is the unit the operator authorized; debits are pooled. Per-agent `budget_share` exists as a *soft* allocation hint that lets the cost-feedback loop (post-talk) detect "agent A burns 80% of envelope on 20% of tasks," but the *hard* invariant is the envelope, not the share. This matches how a real CFO budgets: the team has one number; individuals have soft targets that roll up.

### Why T0 default with T2 escalation, not T2 default

Per memo §4 and §8: LLMs are exception handlers, not the main path. The router table reflects this — anything deterministic, parse-shaped, or grep-shaped lands at T0. T2 is reserved for ambiguity, judgment, and synthesis. A child agent that wants T2 has to either (a) hit a rule that escalates by type (`git.commit`, `gh.pr.create`) or (b) emit a `meta.confidence < threshold` from T0 and trigger an explicit escalation. The default is local; the cloud is the exception.

### Why a flat-file envelope, not SQLite

The cross-process envelope state needs to be: shared by N subprocesses, mutated atomically, fast (sub-ms common-case), and durable enough for one-day demos. SQLite would be the production-grade answer. For this slice the operations are simple — `Spend` is one read-modify-write — and `flock(2)` + `os.Rename` on a small JSON blob is sufficient, well-understood, and adds no migration burden. SQLite-backed envelopes are a post-talk swap if real concurrency or query patterns warrant.

### Why the audit log is the single source of truth

Three reasons. First, it already exists (PR #45) and chitin's existing tools already read it. Second, it is append-only JSONL — concurrent writers from sibling subprocesses are safe with O_APPEND on Linux for writes ≤PIPE_BUF (4 KiB), well above one Decision line. Third, the watch dashboard, the post-talk cost-feedback loop, the memU-shape index, and the OTEL ingest path all want to read the same stream. If we add a parallel telemetry pipe just for cost we will own two streams forever; one stream stays one stream.

## Components

### `gov/budget.go` (new)

```go
// BudgetEnvelope is a cross-process budget cap. Authoritative state
// lives in ~/.chitin/budget-<id>.json; this struct is a handle, not
// the truth. Every Spend reads, locks via flock(2), debits, writes,
// unlocks. Sub-millisecond common-case.
type BudgetEnvelope struct {
    ID           string  // ULID — sortable by creation time
    Path         string  // ~/.chitin/budget-<id>.json
    Limits       BudgetLimits
}

type BudgetLimits struct {
    BudgetUSD     float64
    MaxTokens     int64
    MaxToolCalls  int64
}

// envelopeState is what's serialized at Path.
type envelopeState struct {
    ID           string
    Limits       BudgetLimits
    SpentUSD     float64
    SpentTokens  int64
    SpentCalls   int64
    Closed       bool   // true after operator close or post-success
    CreatedAt    time.Time
    LastSpendAt  time.Time
}

// LoadEnvelope opens (and creates if missing) the envelope by ID.
func LoadEnvelope(id string) (*BudgetEnvelope, error)

// Spend debits the envelope. Returns ErrBudgetExhausted on any cap
// breach. Once Closed=true (operator closed, or one breach was
// observed), subsequent calls continue to fail — the breach is sticky.
// Acquires flock(LOCK_EX) for the read-modify-write window.
func (e *BudgetEnvelope) Spend(d CostDelta) error

// Inspect returns a snapshot for the watch dashboard. Read-only;
// uses flock(LOCK_SH).
func (e *BudgetEnvelope) Inspect() (envelopeState, error)

// Grant raises BudgetUSD by delta (operator recovery path). Logs
// the grant to gov-decisions-*.jsonl with rule_id "operator-grant"
// for audit.
func (e *BudgetEnvelope) Grant(deltaUSD float64) error
```

ULID instead of UUID so envelope IDs sort by creation time in the audit log. The lock file is the envelope file itself; `flock` on the same fd we read/write is portable across Linux and macOS. ENOSPC on write returns the error — better to fail closed than to silently lose a debit.

### `cost/cost.go` (new)

```go
type CostDelta struct {
    USD          float64
    InputTokens  int64
    OutputTokens int64
    ToolCalls    int64
}

type RateTable map[string]ModelRate // keyed by model id, e.g. "copilot-gpt-4.1"

type ModelRate struct {
    USDPerInputKtok  float64
    USDPerOutputKtok float64
}

// Estimate returns a pre-call cost delta. For T0 it returns
// CostDelta{ToolCalls: 1} only — local compute is treated as zero
// USD. For T2 it estimates input tokens from action.Target length and
// max output tokens from the action's max_output_tokens hint.
func Estimate(action gov.Action, tier gov.Tier, rates RateTable) CostDelta

// Reconcile takes actual usage from the model response and returns a
// delta to apply on top of the prior Estimate. Sign convention: a
// positive delta debits more, a negative delta refunds.
func Reconcile(estimated, actual CostDelta) CostDelta
```

The rate table lives in `chitin.yaml` under `cost.rates.<model_id>`. Keep it simple — a flat map. Source for the talk: GitHub Copilot's published rate sheet at the time of the talk; pinned in the runbook so we can answer "what numbers did you use?" on stage.

### `tier/tier.go` (new)

```go
type RouteOpts struct {
    EscalateBelowConfidence float64 // default 0.6
    OverrideTier            *gov.Tier // YAML-level pin, optional
}

func Route(action gov.Action, opts RouteOpts) gov.Tier
```

Implementation is a switch on `action.Type` with one secondary check on `action.Params["sub_action"]` for `shell.exec`. ~50 lines including comments. The rules live in code, not YAML, because they are policy invariants — the YAML author should not be able to override "git.commit must escalate to T2" by setting `tier_hint: T0`.

### `internal/driver/copilot/acp/` (new) — the ACP shim

This is the load-bearing v2 component. Files:

- `shim.go` — top-level entry: read `--acp --stdio` ACP frames from openclaw on this process's stdin, forward to a child `copilot --acp --stdio` subprocess. Bidirectional ACP frame proxying.
- `intercept.go` — for each ACP `request` frame from the child that proposes a tool call, normalize to `gov.Action`, run tier router + cost estimate + envelope.Spend + gov.Gate.Evaluate. On allow, forward unchanged. On deny (gate or budget), synthesize an ACP refusal frame back to the child agent with the chitin Reason + Suggestion + CorrectedCommand encoded in the refusal text. The child sees a model-visible refusal, not a transport error — this is the same UX the v1 SDK driver already does, ported to ACP frame shape.
- `acp_decode.go` — minimal ACP frame parser for the shapes we care about: `tool_call_request`, `tool_call_response`, `cancel`, `session_set_mode`, `prompt`. Out-of-scope: `terminal/*`, `fs/*`, `mcpServers` (already documented as bridge-mode unsupported).
- `tests/` — unit tests on the frame parser; integration test that pipes a real ACP transcript fixture through the shim and asserts the right denials/allows + audit log lines.

The shim is invoked as `chitin-kernel drive copilot --acp --stdio --envelope=<id>`. Envelope ID also accepted via `CHITIN_BUDGET_ENVELOPE` env var (preferred for the acpx config command, since openclaw substitutes env vars at spawn time).

### `cmd/chitin-kernel/install_acpx.go` (new)

Cobra subcommand: `chitin-kernel install acpx-override [--profile=<name>] [--dry-run]`. Reads the operator's openclaw config (default `~/.openclaw/openclaw.json`; with `--profile=foo`, reads `~/.openclaw-foo/openclaw.json`). Writes the canonical override block:

```json
{
  "plugins": {
    "entries": {
      "acpx": {
        "enabled": true,
        "config": {
          "agents": {
            "copilot": {
              "command": "chitin-kernel drive copilot --acp --stdio --envelope=$CHITIN_BUDGET_ENVELOPE"
            }
          }
        }
      }
    }
  }
}
```

Idempotent: re-running detects an existing identical block and no-ops. Backs up the original to `<path>.chitin-backup-<ts>` on every change. Mirror `chitin-kernel uninstall acpx-override` reverses the change. `--dry-run` emits the diff without applying. Refuses to overwrite a non-chitin override on the same agent (don't trample operator customizations).

### `cmd/chitin-kernel/envelope.go` (new)

Cobra subcommand group: `chitin-kernel envelope`. Subcommands:
- `create --usd=10 [--max-tokens=N] [--max-tool-calls=N]` — emits the envelope ID to stdout (for shell capture into `$CHITIN_BUDGET_ENVELOPE`).
- `inspect <id>` — JSON dump of `envelopeState`.
- `list` — recent envelopes with status.
- `grant <id> +5` — raise BudgetUSD by delta. Logs as `rule_id: operator-grant` to the audit log.
- `close <id>` — operator-close. Subsequent Spends fail with ErrBudgetClosed.

Operator-facing utility commands. Used in the talk runbook for setup + recovery.

### `cmd/chitin-kernel/swarm.go` (new, fallback only)

Cobra subcommand. Reads `<plan>.yaml`, validates against the JSON Schema (Lobster-borrow shape), creates a `gov.BudgetEnvelope`, opens the shared log writer, spawns `len(plan.Agents)` goroutines bounded by `max_parallel`. Each goroutine invokes `chitin-kernel drive copilot --acp --stdio --envelope=<id>` against a piped ACP transcript built from the agent's `prompt` field. Returns non-zero on budget exhaustion, zero on full completion.

**Status:** demo fallback only. Implementation order: ship after `drive copilot --acp --stdio` and `install acpx-override` are working. If time short, drop entirely from v1; the openclaw path is the talk demo and this is recovery insurance.

The cancel propagation uses a single `context.WithCancel` rooted at the envelope. `BudgetEnvelope.Spend` returning `ErrBudgetExhausted` triggers `cancel()`; sibling goroutines see `ctx.Err()` on their next `select` and exit. Deadline ≤2s is enforced by a `time.AfterFunc(2*time.Second, hardKill)` that SIGKILLs any subprocess that hasn't drained.

### `cmd/chitin-kernel/watch.go` (new)

Separate cobra subcommand. Opens `gov-decisions-<today>.jsonl` for tailing (inotify on Linux; poll fallback). Renders one pane per `agent` field seen in the stream. Header shows: `Spent: $X.XX / $Y.YY  | Tokens: A/B  | Calls: C/D  | Elapsed: T  | ETA: …`. Per-pane: agent name, last action type, tier breakdown bar, denial count, escalation count.

Bubble Tea is the right tool but adds a dependency. Plain ANSI cursor moves are sufficient for one screen and zero deps. Pick in plan phase based on whether we already have Bubble Tea elsewhere; if not, plain ANSI.

### Swarm YAML schema (Lobster-borrow, fallback only)

Used only by the fallback `chitin-kernel swarm` subcommand. The primary path uses openclaw's native workflow definitions.

```yaml
name: burn-down-chitin-issues
budget_usd: 10.00
max_tokens: 200000
max_tool_calls: 500
max_parallel: 3

agents:
  - name: agent-issue-101
    prompt: |
      Read issue #101, propose a fix, open a PR.
    tier_hint: T2          # optional; router decides anyway
    budget_share: 0.33     # soft allocation; envelope is hard
    output_schema:
      type: object
      required: [pr_url, issue_url]
      properties:
        pr_url:    { type: string, format: uri }
        issue_url: { type: string, format: uri }
      additionalProperties: false
  - name: agent-issue-102
    prompt: …
  - name: agent-issue-103
    prompt: …
```

Closed-enum schema. Unknown fields reject at parse. Borrowing two specific things from Lobster: (a) JSON Schema on agent outputs (typed pipes, not text), (b) the YAML+name+steps shape that's read like a Dockerfile rather than imperative JS. Not borrowing: Lobster's `approval`/`resumeToken` pause-resume because we already have escalation in `gov.Counter`.

### Openclaw-side configuration (the primary path)

The talk runbook drives the swarm via openclaw, not the chitin YAML. Two pieces of operator setup needed:

1. **acpx override** (one-time, via `chitin-kernel install acpx-override`):
   ```json
   "plugins": { "entries": { "acpx": { "enabled": true, "config": {
     "agents": {
       "copilot": {
         "command": "chitin-kernel drive copilot --acp --stdio --envelope=$CHITIN_BUDGET_ENVELOPE"
       }
     }
   }}}}
   ```
2. **Subagent lane cap** (already default 8; explicit in config for the demo so we can show it):
   ```json
   "agents": { "defaults": { "subagents": { "maxConcurrent": 8 } } }
   ```

Demo trigger from chat: `/acp spawn copilot --bind here` for one agent, or an openclaw agent run that calls `sessions_spawn` 3x with `runtime: "acp"`, `agentId: "copilot"`, three different prompts. All three Copilot processes get launched via the chitin shim, all share the envelope from the env var.

### Audit log extensions

The existing `gov-decisions-<date>.jsonl` schema gains four optional fields: `envelope_id`, `tier`, `cost_usd`, `tokens_in`, `tokens_out`. All optional; existing readers tolerate. The `Decision` struct in `gov/decision.go` gains the corresponding Go fields. The `WriteLog` marshalled struct gains them with `,omitempty` JSON tags so old-shape decisions still round-trip.

## Data flow

All flows assume the operator has run setup once: `chitin-kernel envelope create --usd=10` (captures `$CHITIN_BUDGET_ENVELOPE=01J…`), `chitin-kernel install acpx-override`, and `chitin-kernel watch &` in another terminal.

### Flow A — Allow under budget (the boring path, 99% of calls)

1. Operator runs an openclaw turn that calls `sessions_spawn({ runtime: "acp", agentId: "copilot" })` for issue #101.
2. acpx plugin reads the override, spawns `chitin-kernel drive copilot --acp --stdio --envelope=01J…` with `CHITIN_BUDGET_ENVELOPE` substituted.
3. The chitin shim spawns a child `copilot --acp --stdio` and proxies frames.
4. Child agent's first tool-call request: `Action{Type: file.read, Target: "go/execution-kernel/internal/gov/decision.go"}`.
5. `tier.Route(action) → T0Local`.
6. `cost.Estimate(action, T0Local) → CostDelta{ToolCalls: 1}`.
7. `envelope.Spend(estimate)` — flock, read-modify-write, success in ~1ms.
8. `gov.Gate.Evaluate(action, "copilot-cli", envelope) → Decision{Allowed: true, Tier: T0Local, CostUSD: 0, EnvelopeID: "01J…"}`.
9. Frame forwarded to child copilot; T0 transport executes; response forwarded back.
10. `cost.Reconcile(estimate, actual) → CostDelta{}` (zero delta — T0 is fixed cost).
11. `WriteLog(decision)` — JSONL line written; `chitin-kernel watch` updates the agent's pane.
12. Total elapsed in the chitin shim: well under 200ms target.

### Flow B — Deny on budget (closing demo beat)

1. Three Copilot agents are running in parallel (openclaw subagent lane). One has been hitting T2 hard; envelope at $9.95 of $10.
2. That agent's child requests another tool call: `Action{Type: shell.exec, Target: "long-prompt-to-claude"}`.
3. `tier.Route → T2Expensive`.
4. `cost.Estimate → CostDelta{USD: 0.15}`.
5. `envelope.Spend(estimate)` returns `ErrBudgetExhausted` and persists `Closed: true` to the envelope file.
6. The chitin shim returns an ACP refusal frame to the child with `Reason: "envelope $10 spent; this action would push us to $10.10"`.
7. The chitin shim ALSO writes `Decision{Allowed: false, Mode: "budget", RuleID: "envelope-exhausted", EnvelopeID: "01J…"}` to JSONL.
8. Sibling agents' next `envelope.Spend` (any tool call from any sibling) sees `Closed: true` and refuses with the same shape — natural fan-out without explicit cancel propagation.
9. Watch dashboard shows the spike and the per-agent denials cascading.
10. Operator can either let openclaw close the sessions (each Copilot agent will exit cleanly when its frames stop being answered) or run `chitin-kernel envelope grant 01J… +5` to raise the cap and resume.

### Flow C — T0→T2 escalation

1. Child copilot requests `Action{Type: delegate.task, Target: "design a migration script"}`.
2. `tier.Route → T0Local` (default for delegate.task).
3. Chitin shim forwards the request to T0 transport (libs/adapters/ollama-local). T0 model returns response with `meta.confidence: 0.4`.
4. Shim returns the response to the child with the low-confidence flag visible.
5. Child agent re-issues the same action with `Params["force_tier"] = "T2"`.
6. Re-route: `T2Expensive`.
7. `cost.Estimate → CostDelta{USD: 0.05}`.
8. `envelope.Spend` ok; chitin shim forwards to the underlying real `copilot --acp --stdio` for T2 execution; reconciliation; logged.
9. Audit log shows both attempts; the watch dashboard shows the escalation arrow.

### Flow D — Swarm completion under budget

1. All three Copilot agents emit final responses to openclaw; openclaw closes each ACP session normally.
2. Each chitin shim sees its child exit, writes a final summary line to JSONL, exits 0.
3. Total envelope spend: $4.32 of $10.
4. Watch dashboard freezes the final pane state until operator SIGINT.

### Flow E — Operator cancels mid-swarm

1. Operator hits Ctrl+C in the openclaw chat (or runs `/acp cancel` / `/acp close`).
2. Openclaw sends ACP `cancel` frames to each child session. Each chitin shim forwards the cancel frame to its underlying child copilot.
3. In-flight tool calls complete and log; subsequent requests are answered with cancellation refusal.
4. Each shim exits with code 130 (SIGINT convention) when its child exits.
5. The envelope is left open (not closed) so the operator can resume by spawning new agents against the same envelope ID.

### Flow F — Spawn fails (preflight semantics)

1. Operator misconfigured: `CHITIN_BUDGET_ENVELOPE` set to an envelope ID that doesn't exist on disk.
2. acpx spawns the chitin shim; shim's `LoadEnvelope("01J-bad")` returns ENOENT.
3. Shim emits an ACP error frame to openclaw with `Reason: "envelope not found"` and exits 1.
4. Openclaw marks the spawn as failed; reports to the operator.
5. No partial state. No silent fallback.

## Substrate decisions

### Openclaw (ADOPT, primary orchestrator)

v2 reversal — openclaw IS the swarm orchestrator. Production-grade, OSS, with native parallel execution on the `subagent` lane (default cap 8) and a configurable ACP agent-command override (`plugins.entries.acpx.config.agents.<id>.command`). Smoke-tested 2026-04-28: schema accepts the override, validate passes, value reads back unchanged. Upgrade required: `npm install -g openclaw@2026.4.25`. Zero upstream PRs needed.

### Lobster (BORROW)

Borrowing two things into the (fallback) `chitin-kernel swarm` YAML schema: JSON-Schema-validated agent outputs (typed pipes), and the declarative `name + steps` shape. Not borrowing: Lobster's runtime, its approval/resume model, or its `.lobster` file extension (we use `.yaml`). 90-day revisit if Lobster ships a pre-step middleware/hook API.

### memU (BORROW, post-talk)

Borrowing the resource → item → category hierarchy as the post-talk index over `gov-decisions-*.jsonl`. Not borrowing: memU's Postgres+pgvector storage layer, its Python SDK, or its prompt-injection retrieval shape. 90-day revisit if memU ships a Go SDK or a flat-file storage mode.

### Composio (SKIP)

Skipping. Default-path credential custody in their cloud violates the OSS boundary; hosted execution downgrades `gov.Gate` to a pre-flight intent check. Native adapters in `libs/adapters/<source>/` for the 5–10 tools we actually need are 1–2 days each and preserve local custody, OTEL emission, and gate-on-the-wire. Revisit if Composio publishes a VPC tier price *and* a Go SDK.

## Self-review

### Placeholder scan

No TBD / TODO. `<placeholder>` tokens used only for documented field substitutions (`<plan>`, `<date>`, `<today>`, `<source>`).

### Internal consistency

- Single envelope claim is consistent across §Components/budget, §Data flow, §Why a single envelope.
- "T0 default with T2 escalation" claim is consistent across §Preamble, §Architecture, §Components/tier, §Data flow C.
- The rate table source (GitHub Copilot rate sheet, pinned in runbook) is named in §Components/cost and reinforced in §Talk runbook.
- "Openclaw is the orchestrator, chitin governs each spawn" is consistent across §Preamble, §Architecture, §One-sentence invariant, §Substrate decisions/Openclaw, §Data flow.
- The fallback role of `chitin-kernel swarm` is reinforced in §Components/swarm and §Substrate decisions/Lobster.

### Scope check

- Single coherent kernel slice. Reuses `gov.Gate`, `gov-decisions-*.jsonl`, the existing escalation counter unchanged.
- Two new packages (`internal/cost/`, `internal/tier/`) plus two new subcommands (`swarm`, `watch`) plus four new fields on `Decision`. Bounded.
- All four memo principles excluded from scope (T1, AST pipelines, full caching, preflight pruning, auto-tuner) are explicit in §Out of scope.

### Ambiguity check

- "Cancel propagation across N shims" — when the envelope is exhausted, sibling shims discover this on their next `Spend` (which re-reads the envelope file). There is no out-of-band signal to wake idle shims. For the talk demo this is fine because every shim does at least one Spend per second under realistic agent loads. For long-idle sessions, post-talk: add an inotify watcher on the envelope file so shims react within a frame.
- "Single envelope shared by N children across processes" — `~/.chitin/budget-<id>.json` + flock(2) is the v1 path. Read, lock, debit, write, unlock. ~1ms common case. Resolved as load-bearing this session, not deferred.
- "T0 confidence" — the T0 model has to emit `meta.confidence`. Open question for the plan phase: which Ollama-served models do this natively, and what does our `libs/adapters/ollama-local/` need to surface to make this work? If none, we ship escalation rules (by action type) only, and the confidence-threshold path becomes inert until post-talk.
- "ACP frame parsing scope" — the chitin shim is a *minimal* ACP proxy. It only parses frame shapes that touch tool calls. Other frame types (terminal, fs, mcpServers) are forwarded as opaque bytes. If openclaw's acp.md compatibility matrix lists a shape as Unsupported in bridge mode, we treat it as opaque too — no extra work.

### Out-of-scope leak check

- No changes to the closed-enum action vocabulary. `gov.Action.Type` is unchanged; `Tier` and cost fields are on the *Decision*, not the action.
- No new gov rules in `chitin.yaml` for the talk slice. The envelope is enforced in `gov.Gate.Evaluate` plumbing, not via the policy DSL.
- `chitin-kernel drive copilot` gets a new mode (`--acp --stdio`) and a new flag (`--envelope=<id>`), but the v1 SDK driver path remains unchanged — both modes coexist.
- No openclaw, acpx, Lobster runtime, memU, or Composio code lands in this slice. Only chitin code; openclaw is consumed via its public config schema.
- Claude Code hook driver is NOT extended in this slice. It remains the separate untracked spec deferred to post-talk.

## Open questions for plan phase

1. **ACP frame shape for tool-call refusals.** The spec assumes refusals can be returned to the child agent in a model-visible way. Need to confirm by reading the openclaw acp.md docs section "Tool streaming" + the @zed-industries/agent-client-protocol spec at https://agentclientprotocol.com/. If the child copilot doesn't surface the refusal text to its model, the chitin shim will need to inject the refusal as a synthetic tool *response* with the chitin Reason embedded — slightly hackier but UX-equivalent. Resolve via 30-min spike before writing `intercept.go`.
2. **Live e2e of acpx override.** Smoke test confirmed the schema + validate path. Live e2e (does the override actually fire when openclaw spawns a Copilot ACP session?) is Task 1 of the implementation plan. Use a no-op wrapper (`/tmp/chitin-acpx-wrapper.sh` already in place) to prove the override is invoked before writing the real shim.
3. **Bubble Tea vs plain ANSI for the watch dashboard.** Adds a dep; we don't need anything fancy. Default to plain ANSI; revisit if the demo needs a feature plain ANSI can't do.
4. **T0 confidence emission.** Which Ollama models emit `meta.confidence` natively? If none, the escalation path is rule-based-only for the talk; the confidence-threshold parameter is wired but inert. Acceptable for v1.
5. **Rate table maintenance.** GitHub Copilot rate sheet is the source of truth. Pinned in the runbook as a rendered snapshot at talk time. Open question: does Copilot CLI expose actual usage in its ACP frames, or do we have to estimate from prompt+response token counts? If yes, reconciliation is exact; if not, reconciliation uses tiktoken-style local counting and is approximate. Resolve in plan phase by a 30-minute spike against a running session.
6. **Default budget for `chitin-kernel envelope create` without an explicit `--usd`.** Recommend $5 as a safe default. Operator can raise. Lower than the talk demo to avoid surprises.
7. **Audit log rotation under concurrent writers.** O_APPEND on Linux is atomic for writes ≤PIPE_BUF (4 KiB). A single Decision JSONL line is well under that. No external locking needed. Document in the plan as the assumption being verified — particularly important now that writers are separate processes (the chitin shim spawns), not goroutines.
8. **Envelope file flock contention.** With 3-8 sibling shims doing read-modify-write on the same JSON file under flock, contention is bounded by the per-call critical-section duration (~1ms). Should be fine for talk demo (3 agents). Stress-test at 8 agents in plan phase to confirm no thundering-herd issue.
9. **Openclaw OTEL secondary signal.** 2026.4.25 changelog claims expanded OTEL coverage. 30-min validation in plan phase: enable `diagnostics.otel.enabled = true` in test profile, run a Copilot ACP spawn, see if real spans flow. If yes, optionally pipe to chitin's existing ingest path as a secondary signal. If no, no impact — chitin's audit log remains the source of truth.
10. **What if the operator's day-of demo blows the budget?** The `chitin-kernel envelope grant <id> +5` recovery path is in scope. Talk runbook documents the muscle memory.

## Branch + worktree

Per `memory/feedback_always_work_in_worktree.md`:

- Spec branch: lands on `main` (consistent with the v1 + claude-code-hook spec commits).
- Implementation branch when planned: `feat/cost-governance-kernel` off `main`.
- Worktree: `~/workspace/chitin-cost-governance/`.
- Talk runbook: `docs/superpowers/runbooks/2026-05-07-talk-runbook.md`. Lands on the implementation branch alongside the code that backs it.
