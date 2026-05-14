# Cost-Governance Kernel — Implementation Plan v3

**Supersedes:** `2026-04-28-cost-governance-kernel.md` (talk-driven plan, scrapped after design pivot 2026-04-29).

**Status:** Plan, not yet executed. Branch: `feat/cost-governance-kernel` off `main`. Worktree: `~/workspace/chitin-cost-governance/`.

**Scope:** Cross-process envelope coordination, two driver surfaces (Claude Code hook, Copilot ACP shim), permission-gate + audit-log integration. No execution by chitin. No session-spawn routing. No cross-session memory. No talk runbook.

**Parent spec:** `2026-04-28-cost-governance-kernel-design.md` (v2). Spec needs revision to match this plan; divergences listed at the end.

**Design pivot record:** This plan is the output of a `/grill-me` walk on 2026-04-29 that resolved 14 design-tree branches. Key reversals from v2 spec: chitin is permission-gate-only (not executor); T0 is an audit-log tag (not an execution branch); envelope state is sqlite (not flat-file); real-time cap is on tool calls + input bytes (not $USD); Claude Code hook driver is in scope and ships first.

---

## Architectural shape

Chitin's kernel is a permission-gate plus envelope enforcement, with audit logging. Two integration surfaces share one set of state:

- **Claude Code hook** (`chitin-kernel gate evaluate --hook-stdin --agent=claude-code`): cold-start subprocess per Claude Code tool call. Reads JSON from stdin, calls `gov.Gate.Evaluate()` + `envelope.Spend()`, writes JSON, exits.
- **Copilot ACP shim** (`chitin-kernel drive copilot --acp --stdio`): long-running per-session subprocess between openclaw's acpx plugin and a child `copilot --acp --stdio`. Same governance per `tool_call_request` frame.

Shared state in `~/.chitin/gov.db` (sqlite — escalation counter from v1, envelope tables added here) and `~/.chitin/gov-decisions-<date>.jsonl` (append-only audit log). Cross-driver-shared by design.

**Envelope discovery precedence** per driver invocation:
1. `--envelope=<id>` flag if explicitly passed
2. `CHITIN_BUDGET_ENVELOPE` env var if set
3. `~/.chitin/current-envelope` file if present (kubectl-style; set via `envelope use <id>`)
4. None → gate + audit only, no spend enforcement, Decision logged with `EnvelopeID: ""`

**Real-time enforcement:** `MaxToolCalls`, `MaxInputBytes` per envelope. `BudgetUSD` field is informational only — no published per-token rate maps cleanly to Copilot CLI's flat-rate model, so the $USD cap would be fictional. Real $USD reconciliation deferred to OTEL ingest (separate roadmap item).

**Tier classification** is computed per Decision and stamped on the audit row. Pure routing label; does not influence cost calculation or execution.

---

## Milestones

### Milestone A — Kernel primitives

**Goal:** Data structures, classifier, and persistence layer for governance + envelope semantics.

- [ ] **Sqlite migration** adding `envelopes` and `envelope_grants` tables to `~/.chitin/gov.db`:
  ```sql
  CREATE TABLE envelopes (
      id TEXT PRIMARY KEY,                  -- ULID
      created_at TEXT NOT NULL,
      closed_at TEXT,
      max_tool_calls INTEGER,
      max_input_bytes INTEGER,
      budget_usd REAL,                       -- informational
      spent_calls INTEGER NOT NULL DEFAULT 0,
      spent_bytes INTEGER NOT NULL DEFAULT 0,
      spent_usd REAL NOT NULL DEFAULT 0,     -- informational
      last_spend_at TEXT
  );
  CREATE TABLE envelope_grants (
      envelope_id TEXT NOT NULL REFERENCES envelopes(id),
      granted_at TEXT NOT NULL,
      delta_calls INTEGER,
      delta_bytes INTEGER,
      delta_usd REAL,
      reason TEXT
  );
  ```
- [ ] **`internal/gov/budget.go`** — `BudgetEnvelope` handle, `BudgetLimits{MaxToolCalls, MaxInputBytes, BudgetUSD}`, `Spend(CostDelta) error` returning `ErrEnvelopeExhausted`. Backed by sqlite WAL; no flock. Sticky-closed: once `closed_at` set, subsequent Spends fail.
- [ ] **`budget_test.go`** — load-creates, spend-debits, exhaust-and-stays, grant-raises-cap, **concurrent-spend-is-exact** (100 separate subprocesses × 1 spend each against bounded envelope; assert exactly N successes, 0 over-spend).
- [ ] **`gov/decision.go` extensions** — add `EnvelopeID`, `Tier`, `CostUSD`, `InputBytes`, `OutputBytes`, `ToolCalls` fields. `,omitempty` JSON tags so existing audit-log readers tolerate.
- [ ] **`gov/tier.go`** — `Tier` enum: `TierUnset`, `T0Local`, `T1Cheap` (reserved), `T2Expensive`.
- [ ] **`internal/cost/cost.go`** — `CostDelta{USD, InputBytes, OutputBytes, ToolCalls}`, `Estimate(action gov.Action, executor string, rates RateTable) CostDelta`. Tier-blind. Per-executor rate lookup. `ANTHROPIC_BASE_URL=localhost:*` → executor=`claude-code-local`, rate `{0,0}`.
- [ ] **`internal/cost/rates.go`** — rate table loaded from `chitin.yaml` under `cost.rates.<executor>`. Document Anthropic and Copilot rate sources in code comment with an "approximate, informational" disclaimer.
- [ ] **`internal/tier/tier.go`** — `Route(action gov.Action) gov.Tier`. Deterministic queries → T0 (file.read, git.{diff,log,status,worktree.list}, github.{pr,issue}.{view,list}, http.request to allowlist). Side-effect/judgment → T2 (file.write, git.commit, github.pr.create, etc.). Default T2.
- [ ] **`gov.Gate.Evaluate` signature** — `Evaluate(action Action, agent string, envelope *BudgetEnvelope) Decision`. Existing v1 SDK driver call sites pass nil; preserve current behavior for nil envelope.

**Acceptance criteria:**
- All A-level unit tests green.
- Existing v1 SDK driver tests still pass (nil-envelope backward compat).
- Cross-process concurrent envelope test passes.

---

### Milestone C — Claude Code hook driver

**Goal:** Operator installs the Claude Code hook globally, runs interactive Claude Code sessions under chitin governance, sees decisions in audit log with envelope spend reflected.

- [ ] **`gate evaluate --hook-stdin` mode** — extend existing subcommand. Reads Claude Code's PreToolUse JSON from stdin, resolves envelope via the precedence chain, returns Claude Code's expected response: exit 0 + empty stdout for allow, exit 2 + `{"decision":"block","reason":"..."}` for deny.
- [ ] **`internal/driver/claudecode/normalize.go`** — tool-name mapping per the hook driver spec:
  - `Bash` → `gov.Normalize("terminal", {"command": <cmd>})` (inherits shell re-tagging)
  - `Edit`, `Write`, `NotebookEdit` → `file.write`
  - `Read` → `file.read`
  - `WebFetch`, `WebSearch` → `http.request`
  - `Task` → `delegate.task`
  - `Glob`, `Grep`, `LS`, `TodoWrite` → resolve at impl time (default-allow as browse tools is the leaning recommendation)
- [ ] **`normalize_test.go`** — every documented Claude Code tool name produces non-empty Action.Type.
- [ ] **`internal/driver/claudecode/format.go`** — `Decision` → hook response JSON + exit code. Reason string includes Suggestion + CorrectedCommand if present (model-visible).
- [ ] **`cmd/chitin-kernel/install_claude_code.go`** — `install claude-code-hook [--global|--project] [--dry-run]`. Idempotent JSON merge into `~/.claude/settings.json` or `.claude/settings.json`. Backup `<path>.chitin-backup-<ts>` on every change. Refuse to overwrite a non-chitin matcher; emit merge instruction instead.
- [ ] **`uninstall_claude_code.go`** — reverse operation.
- [ ] **Cold-start benchmark** — `bench_coldstart_test.go`. 100 invocations of `chitin-kernel gate evaluate --hook-stdin` against a fixture JSON. Report p50/p95/p99 on operator's box. **Decision gate:** if p95 > 100ms, design and build daemon mode (`gate daemon` subcommand listening on `~/.chitin/gate.sock`); if p95 ≤ 100ms, ship cold-start.
- [ ] **Live integration test** — install hook globally, run a real Claude Code session that does Read + Bash + Edit, confirm decisions appear in `gov-decisions-<today>.jsonl` with `agent: "claude-code"`.

**Acceptance criteria:**
- Cold-start benchmark recorded; daemon-mode decision made.
- Operator runs `claude` and chitin governance is in the path of every tool call.
- Audit log has Claude Code decisions with envelope spend reflected when an envelope is current.

---

### Milestone E (partial) — Operator surface

**Goal:** Operator can manage envelopes and observe live activity.

- [ ] **`cmd/chitin-kernel/envelope.go`** — subcommand group:
  - `envelope create --calls=N --bytes=N [--usd=N]` — emits ULID to stdout
  - `envelope use <id>` — atomic write of `~/.chitin/current-envelope` (write-tmp + rename)
  - `envelope inspect <id>` — JSON dump of envelope state
  - `envelope list` — recent envelopes table
  - `envelope grant <id> --calls=+N --bytes=+N` — raise caps; logs `rule_id: operator-grant` to audit
  - `envelope close <id>` — mark closed
- [ ] **`envelope_tail.go`** — `envelope tail [<id>] [--stats]`. Streams JSONL through a per-line formatter:
  ```
  2026-04-29T15:01:02Z  claude-code  T0  $0.000   file.read /path/...     ALLOW
  ```
  `--stats` prints periodic envelope summary lines:
  ```
  [stats] envelope 01J-X: calls 47/500, bytes 2.3MB/5.0MB, $0.32 (informational), denials 0
  ```
  inotify on Linux; poll fallback elsewhere.
- [ ] Atomic `envelope use` race test (two processes calling `use` concurrently, final state is one of them, never corrupted).

**Acceptance criteria:**
- Operator runs `envelope create`, `envelope use <id>`, then a Claude Code session, then `envelope tail` in another terminal sees real-time decision lines.

---

### Milestone B — Copilot ACP shim

**Goal:** `chitin-kernel drive copilot --acp --stdio` mode that intercepts ACP frames between openclaw's acpx plugin and a child `copilot --acp --stdio`, applies gov.Gate per tool-call request, surfaces denials as ACP refusal frames the model sees.

**Block on this milestone:** ACP refusal-frame visibility spike (open spec Q #1) — confirm whether refusal frames are model-visible, or whether the shim must inject a synthetic tool response with the chitin Reason embedded. ~30 min spike against a captured ACP transcript. Document resolution in `docs/observations/acp-refusal-shape.md`. **No B-tasks start until this is resolved.**

- [ ] **`internal/driver/copilot/acp/shim.go`** — top-level entry. ACP-over-stdio with parent (openclaw); spawns child `copilot --acp --stdio`; frame-aware bidirectional proxy.
- [ ] **`internal/driver/copilot/acp/acp_decode.go`** — minimal frame parser for `tool_call_request`, `tool_call_response`, `cancel`, `prompt`, `session_set_mode`. Unknown frames pass through opaque; key invariant: every byte stream that comes in goes back out byte-equivalent unless it's a recognized tool-call frame.
- [ ] **`acp_decode_test.go`** — fixture-based decode tests + unknown-frame round-trip tests. Fixtures captured from a real `copilot --acp --stdio` session.
- [ ] **`intercept.go`** — per-frame `func(Frame) Frame` interceptor. Initial impl: identity. Test: spawn shim with a fake-copilot child that emits one tool-call-request, assert intercept reads it without breaking the proxy.
- [ ] **`intercept_governance.go`** — on each `tool_call_request`, normalize to `gov.Action`, run tier router → cost.Estimate → envelope.Spend → gov.Gate.Evaluate. Allow → forward. Deny (gate or budget) → synthesize ACP refusal/synthetic-response per spike resolution. Reason text = `decision.Reason + "\n\nSuggestion: " + decision.Suggestion + "\n\nCorrected: " + decision.CorrectedCommand` (omit blanks).
- [ ] **`cmd/chitin-kernel/copilot.go`** — extend `drive copilot` subcommand. When `--acp --stdio` flags both set, take shim path; else preserve v1 SDK path. Accept `--envelope=<id>` flag with env-var and current-envelope-file fallbacks.
- [ ] **`cmd/chitin-kernel/install_acpx.go`** — `install acpx-override [--profile=<name>] [--dry-run]`. Idempotent JSON merge into `~/.openclaw[-<profile>]/openclaw.json` writing the canonical override:
  ```
  plugins.entries.acpx.config.agents.copilot.command =
    "chitin-kernel drive copilot --acp --stdio --envelope=$CHITIN_BUDGET_ENVELOPE"
  ```
  Backup `<path>.chitin-backup-<ts>`. Mirror `uninstall acpx-override`.
- [ ] **Live e2e** — install override on `chitin-test` profile, set envelope env, openclaw spawns Copilot ACP, chitin shim governs, decisions appear in audit log with `agent: "copilot-cli"`. Cross-driver consistency: Copilot decisions match Claude Code decisions in shape (same fields, same JSON layout).

**Acceptance criteria:**
- Single Copilot session through openclaw under chitin governance.
- Refusal frames visible to the Copilot model (per spike resolution).
- Audit log entries identical in shape to Claude Code's.

---

### Milestone D — Multi-agent envelope coordination

**Goal:** N parallel Copilot ACP sessions under one envelope; sibling-deny on exhaustion; audit-log integrity.

- [ ] **Stress test** — 8 parallel `chitin-kernel drive copilot --acp` shim subprocesses (default openclaw subagent lane cap), all sharing one envelope ID, concurrent envelope.Spend across processes. Verify sqlite WAL handles contention without lost spends or deadlock.
- [ ] **Live e2e** — `envelope create --calls=20 --bytes=100KB` (intentionally low). Trigger 3-agent Copilot fan-out via openclaw. Confirm one agent hits the cap, sibling agents next-call deny with envelope-exhausted reason. All decisions stamped with same `envelope_id`.
- [ ] **Audit log integrity test** — decisions from N concurrent shim writers land cleanly. O_APPEND atomic for ≤PIPE_BUF (4 KiB) writes; verify single Decision lines stay under that. No interleaved or torn JSONL lines under sustained load.

**Acceptance criteria:**
- 8-shim concurrent stress test passes.
- 3-agent live test shows cap enforcement and sibling propagation.
- Audit log integrity verified under multi-process write.

---

## Plan-phase open items (resolve at milestone start)

- **Tier rule table content.** With T0 = pure label, do we keep the full rule list or trim? Recommendation: keep — cheap to compute, produces metadata for future routing analytics. Resolve in Milestone A.
- **Glob/Grep/LS/TodoWrite mapping.** Default-allow as browse tools, default-deny as fail-closed unknown, or new action types? Resolve in Milestone C when normalize.go is being written.
- **Audit log rotation/retention.** Daily JSONL is the spec convention. Multi-driver concurrent write is OK on Linux for ≤PIPE_BUF lines. Rotation/compress/archive policy — define in Milestone D.
- **ACP refusal-frame visibility spike.** Block before Milestone B.
- **Sqlite migration runner.** `gov.db` already has the counter table. Adding envelope tables is additive; need a versioned migration runner. Define in Milestone A.

---

## Spec divergences to address

The v2 spec needs a revision pass to match this plan. Divergences:

| v2 spec says | Plan/decision says |
|---|---|
| Flat-file JSON envelope with flock(2) | Sqlite envelope tables in `gov.db` |
| T0 = local execution, T2 = Copilot | T0 = audit-log tag only; chitin doesn't execute |
| `cost.Estimate(action, tier, rates)` returns 0 USD for T0 | `cost.Estimate(action, executor, rates)` is tier-blind, real-executor rates |
| `BudgetUSD` is the primary cap | `BudgetUSD` is informational; calls + bytes are the real-time caps |
| `chitin-kernel watch` TUI dashboard | `chitin-kernel envelope tail` line formatter |
| `chitin-kernel swarm` fallback subcommand | Dropped entirely |
| Talk runbook at `docs/superpowers/runbooks/2026-05-07-talk-runbook.md` | Out of scope for this plan |
| "Claude Code hook driver — defer post-talk" | Promoted; Milestone C ships first |
| `~/.chitin/budget-<id>.json` per envelope | Single sqlite db; per-envelope row |
| Day-by-day schedule with `[COPILOT]/[CLAUDE]/[HUMAN]` dispatch tags | Milestone-based with acceptance criteria |

Spec revision can land alongside Milestone A as v3 spec, or as a clarifying patch on v2. Plan author preference: v3 spec on `main` referencing v2 in the parent-decisions block; same convention as v2 → v1.

---

## Branch + worktree

Per `memory/feedback_always_work_in_worktree.md`:
- Implementation branch: `feat/cost-governance-kernel` off `main`.
- Worktree: `~/workspace/chitin-cost-governance/` (created at first milestone start).
- Spec/plan revision branch: this plan and any v3 spec land directly on `main` (consistent with prior spec/plan commits).

---

## Reference: existing artifacts

| Artifact | Path | Status |
|---|---|---|
| v2 spec | `docs/superpowers/specs/2026-04-28-cost-governance-kernel-design.md` | Needs v3 revision |
| Hook driver spec | `docs/superpowers/specs/2026-04-28-claude-code-hook-driver-design.md` | Mostly aligned; minor envelope-integration update needed |
| v1 SDK driver | `go/execution-kernel/internal/driver/copilot/` | PR #51, merged. Surface preserved as `drive copilot` (non-ACP mode). |
| Smoke-test profile | `~/.openclaw-chitin-test/openclaw.json` | acpx override pre-configured |
| Bash wrapper (smoke artifact) | `/tmp/chitin-acpx-wrapper.sh` | Replaced by real binary in Milestone B |
| openclaw 2026.4.25 | system-installed | Already upgraded |
| Local Ollama | `qwen3-coder:30b`, `gemma4`, others | Available for future local-T0 work, not used in this slice |
