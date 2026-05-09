# Roadmap

The strategic arc, what's shipped, what's in flight, what's deferred. Updated 2026-05-08 (post-cull rewrite).

## Strategic arc

```
   1. Aggregate          2. Align policy        3. Compose with
      capture across       to data (debt          orchestrators
      all drivers          ledger → rules)        (hermes, openclaw)
          │                    │                      │
          └────────►───────────┴────────►─────────────┤
                                                      │
   4. Policy packs      5. Local operator      6. North star
      reusable             leverage               execution governance
      governance           replay + analytics     as the auditable layer
      bundles              on operator data       under whatever the
                                                  orchestration ecosystem
                                                  picks next
          │                    │                      │
          └────────►───────────┴────────►─────────────┘
```

Each step funds and de-risks the next. We're between 1 and 2 today: aggregation is always-on across 6 drivers; the policy layer is shipped but the rules are still mostly hand-written rather than ledger-derived.

The 2026-05-06 narrowing (`docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md`) and the 2026-05-08 cull (`docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`) are the load-bearing positioning moves: chitin = kernel + plugins + data; orchestration / approvals / scheduling all live in the substrate (hermes today; whatever tomorrow).

## Shipped

### Phase 1 — Claude Code capture → replay (single-surface)

Go execution kernel (canon, normalize, emit, hook), `libs/contracts` (zod schema + generated Go types), `libs/telemetry` (JSONL tailer, SQLite indexer, replay), `libs/adapters/claude-code` (PreToolUse hook), `apps/cli`. Local-only.

### Phase 1.5 — surface-neutral event chain (merged 2026-04-19)

- v2 envelope: `chain_id` / `chain_type` / `parent_chain_id` / `seq` / `prev_hash` / `this_hash`
- Go kernel: `init`, `emit`, `chain-info`, `ingest-transcript`, `install-hook`; transactional emit (`BEGIN IMMEDIATE`); JSONL→SQLite reconcile; canonical-JSON SHA-256 parity with TS
- Claude Code adapter: 7-hook forwarder (SessionStart / UserPromptSubmit / PreToolUse / PostToolUse / PreCompact / SubagentStop / SessionEnd)

### Governance v1 (merged 2026-04-28)

`gov.Gate.Evaluate(action, agent) → Decision` with closed-enum action vocabulary, policy evaluator, audit log to `gov-decisions-YYYY-MM-DD.jsonl`, escalation counter sticky in `~/.chitin/gov.db`. Three modes: monitor / enforce / guide. Kill switches: soft (`mode: monitor`) and hard (`gate lockdown`).

### Drivers (six live)

- **Claude Code hook driver** — PR #66
- **Codex CLI hook driver** — `--agent=codex` PreToolUse handler
- **Gemini CLI hook driver** — `--agent=gemini` PreToolUse handler
- **Copilot CLI in-kernel SDK driver** — PR #51 (`chitin-kernel drive copilot`)
- **OpenClaw before_tool_call plugin** — `~/.openclaw/plugins/chitin-governance/`
- **Hermes pre_tool_call hook** — `scripts/install-hermes-hook.sh` wires `~/.hermes/config.yaml`

The hero sentence names six drivers. Bitrot in any is a hero-sentence bug.

### Layer Contracts v1 (locked 2026-04-29)

Four invariants: kernel authority, driver constraint (`AllowedDrivers` primitive), routing scope, aggregation role. See `docs/architecture/layer-contracts.md`.

### F4 — OTEL emit MVP (merged 2026-05-02)

Kernel projects every chain event onto an OTLP/HTTP JSON span after the canonical write succeeds. One-way bridge: chain authoritative, OTEL non-authoritative. Opt-in via `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`.

### Cost-gov v3 — bounded enforcement (merged 2026-05-04)

`MaxToolCalls`, `MaxInputBytes`, tier classification (T0/T2 audit-log labels), cross-process envelope (`gov.db` WAL). Supersedes v2.

### 2026-05-06 narrowing

Chitin scope narrowed from "execution governance + autonomous swarm runtime" to "execution governance only." Removed: `apps/runner` (Temporal-backed swarm dispatcher), `docs/swarm-backlog.md`, 11 orchestration systemd timers. Boundary: `docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md`.

### 2026-05-07 → 2026-05-08 escalate-effect cull

Built an in-gate operator-approval escalate flow (PRs #380–#396, ~16 PRs, ~1500 LOC). External recipe survey on 2026-05-08 surfaced that hermes' `tools/approval.py` already provides operator-prompt + reply-parse + persistent-allowlist natively. Culled the entire chitin parallel implementation (PRs #397–#400). Decision: `docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`.

### 2026-05-08 audit-driven cuts (this rev)

External-survey audit through three lenses (Knuth correctness, da Vinci architecture, Sun Tzu positioning). Findings convergent across lenses:

- Half-finished orchestration cull: `apps/runner/` shells, `apps/slack-app/` shells, `infra/temporal/`, 122 stale `tmp/result-swarm-*.json`, 7 swarm-flavored Python analyzers. Finished in this rev.
- `libs/scheduler/` + `apps/cli/src/commands/scheduler.ts` — orchestration in disguise. Deleted.
- `internal/router/spawn_peer.go` + `cmd/chitin-kernel/router_hook_escalate.go` — out-of-loop peer-spawn weaker than hermes' in-loop `delegate_task`. Deleted (~1055 LOC).
- 4 specs marked `superseded`: predictive-execution, local-worker (+addendum), scheduler-design, escalate-design. Moved to `docs/superpowers/superseded/`.
- Knuth correctness fixes: `RecordDenial` error propagation, `LoadWithInheritance` policy validation, empty-entry rejection in `path_under` / `branches` / `action`.

Net result: ~5000+ LOC removed; chitin's surface tightened to the moat.

## In flight

| ID | Slice | Why it ships now |
|----|-------|------------------|
| **D1** | Chain-mined driver conformance gaps | The next high-leverage work comes from real `default-deny-unknown` rows: extend per-driver normalizers, then redeploy hooks/binaries so the chain proves the gap closed. |

## Deferred

These are real ideas that wait for either operator demand or an asymmetric-strength signal:

- **Action vocabulary expansion** — extend `internal/gov/action.go` to cover the predictive-execution spec's `rewrite` / `redirect` / `stage` decisions. Lean into asymmetry without growing surface.
- **Chain summarization + causal slicing** — surface in the positioning doc's "replay hydration" note. Make the chain useful at higher abstraction than per-event.
- **Driver coverage** — add new drivers as the ecosystem's tool-call vocabulary stabilizes.
- **Policy packs** — per-domain rule bundles (security, cost, compliance) operators install via `chitin-kernel install-pack`. Step 4 of the strategic arc.

## What chitin will NOT do

Per the 2026-05-06 boundary doc:

- Work tracking, kanban, board state — **hermes**
- Dispatch, spawning runners — **hermes**
- Scheduling (cron, intervals) — **hermes**
- Workflow definitions, retries, durable state — **hermes**
- PR-merge → status-flip pipelines — **hermes**
- Operator approvals — **hermes' tools/approval.py**
- Model routing — **driver-side**
- Mirroring between work-tracking surfaces — **operator's choice**

Whatever orchestrator the operator runs is *agnostic* — chitin doesn't know or care. The orchestrator dispatches, agents do work, every tool call passes through chitin's gate, every decision lands in chitin's chain.

## Cut order if scope tightens further

When the next external-substrate survey shows another chitin surface that re-implements a substrate primitive, the answer is the same as 2026-05-08: cull it. The pattern: invest deeper into asymmetric strengths (canonical vocabulary, chain depth, driver coverage); divest from anything symmetric with the substrate (orchestration, approvals, scheduling, model routing).
