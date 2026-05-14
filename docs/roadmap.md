# Roadmap

The strategic arc, what's shipped, what's in flight, what's deferred.
Updated 2026-05-13 (substrate-composition rev).

## Strategic arc

```
   1. Aggregate          2. Align policy        3. Compose with
      capture across       to data (debt          orchestration
      all drivers          ledger → rules)        substrates
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

We're firmly in step 3: aggregation is always-on across 6 drivers;
the four-hop swarm pipeline (`hermes → clawta → openclaw/lobster →
frontier-coder CLI`) is shipping; the policy layer exists but rules
are still mostly hand-written rather than ledger-derived.

The 2026-05-13 substrate-composition rev
(`docs/decisions/2026-05-13-swarm-readopted-composing-substrates.md`)
codifies the current shape: chitin = kernel + drivers + chain + the
swarm that composes hermes (kanban) and openclaw (Lobster + acpx).
Supersedes the "no orchestration in chitin" exclusion from the
2026-05-06 narrow, but keeps everything 05-06 said about kernel
authority + single-writer discipline.

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

- **Claude Code hook driver** — `internal/driver/claudecode/`; PR #66
- **Codex CLI hook driver** — `internal/driver/codex/`; same wire shape as Claude Code
- **Gemini CLI hook driver** — `internal/driver/gemini/`; same wire shape as Claude Code
- **Hermes hook driver** — `internal/driver/hermes/`; `scripts/install-hermes-hook.sh`
- **Copilot CLI in-kernel SDK driver** — `chitin-kernel drive copilot`; PR #51
- **OpenClaw plugin driver** — `apps/openclaw-plugin-governance/`. Different shape from the four hook drivers by design — plugin runtime, not native hook. Slice 3 ships default-`enforce` covering all 19 pi-runtime tools.

The four PreToolUse-class drivers (Claude Code, Codex, Gemini, Hermes)
all point at the same compiled Go shim `bin/chitin-router-hook`, which
stamps `CHITIN_DRIVER` and dispatches to the per-vendor normalizer.

The hero sentence names six drivers. Bitrot in any is a hero-sentence bug.

### Layer Contracts v1 (locked 2026-04-29)

Four invariants: kernel authority, driver constraint (`AllowedDrivers` primitive), routing scope, aggregation role. See `docs/architecture/layer-contracts.md`.

### F4 — OTEL emit MVP (merged 2026-05-02)

Kernel projects every chain event onto an OTLP/HTTP JSON span after the canonical write succeeds. One-way bridge: chain authoritative, OTEL non-authoritative. Opt-in via `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`.

### Cost-gov v3 — bounded enforcement (merged 2026-05-04)

`MaxToolCalls`, `MaxInputBytes`, tier classification (T0/T2 audit-log labels), cross-process envelope (`gov.db` WAL). Supersedes v2.

### 2026-05-06 narrowing (kernel single-writer discipline)

Chitin scope cleared of duplicate orchestration: removed `apps/runner`
(Temporal-backed swarm dispatcher), `docs/swarm-backlog.md`, 11
chitin-side dispatch timers. The kernel-single-writer +
no-in-tree-duplicate-runtime parts of this still stand. The "chitin
doesn't know hermes exists" part is superseded by the 2026-05-13
composition rev. Boundary: `docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md`.

### 2026-05-07 → 2026-05-08 escalate-effect cull

Built an in-gate operator-approval escalate flow (PRs #380–#396, ~16 PRs, ~1500 LOC). External recipe survey on 2026-05-08 surfaced that hermes' `tools/approval.py` already provides operator-prompt + reply-parse + persistent-allowlist natively. Culled the entire chitin parallel implementation (PRs #397–#400). Decision: `docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`.

### 2026-05-08 audit-driven cuts

External-survey audit through three lenses (Knuth correctness, da Vinci architecture, Sun Tzu positioning). Findings convergent across lenses:

- Half-finished orchestration cull: `apps/runner/` shells, `apps/slack-app/` shells, `infra/temporal/`, 122 stale `tmp/result-swarm-*.json`, 7 swarm-flavored Python analyzers. Finished in this rev.
- `libs/scheduler/` + `apps/cli/src/commands/scheduler.ts` — orchestration in disguise. Deleted.
- `internal/router/spawn_peer.go` + `cmd/chitin-kernel/router_hook_escalate.go` — out-of-loop peer-spawn weaker than hermes' in-loop `delegate_task`. Deleted (~1055 LOC).
- 4 specs marked `superseded`: predictive-execution, local-worker (+addendum), scheduler-design, escalate-design. Removed in the 2026-05-13 doc purge.
- Knuth correctness fixes: `RecordDenial` error propagation, `LoadWithInheritance` policy validation, empty-entry rejection in `path_under` / `branches` / `action`.

Net result: ~5000+ LOC removed; chitin's surface tightened to the moat.

### Swarm composition (shipping incrementally since 2026-05-11)

Four-hop pipeline from
`docs/superpowers/specs/2026-05-11-hermes-clawta-lobster-finish-design.md`:

```
hermes kanban (substrate)
  → clawta tick (chitin: poller + dispatch wrapper)
    → openclaw kanban-dispatch.lobster (substrate: workflow + acpx)
      → frontier-coder CLI (gov.Gate at the leaf)
```

Concretely:

- `bin/chitin-router-hook` — compiled Go shim; stamps `CHITIN_DRIVER`
  from `--agent=<cli>`; deferred to caller-set value if exported
- `chitin.yaml` rule `hermes-no-frontier-spawn` — blocks direct
  frontier-CLI spawning from `driver=hermes`; forces the four-hop path
- `swarm/workflows/kanban-dispatch.lobster` — workflow expressed in
  Lobster; calls clawta for classification, picks driver via
  `_pick_driver.py`, spawns the leaf CLI via `spawn_worker`
- `swarm/bin/clawta-poller` + `swarm/systemd/clawta-poller.timer` —
  dispatch tick reading the hermes kanban
- `swarm/bin/clawta-pr-lifecycle` — PR-state → kanban reflection
- `swarm/data/agent-cards/{claude-code,codex,gemini,copilot}.json` —
  git-tracked agent-card source (symlink-deployed to
  `~/.openclaw/data/agent-cards/`)
- `swarm/roles/{programmer,researcher,reviewer}/SKILL.md` —
  git-tracked per-role prompts
- `scripts/kanban-flow` — bash chokepoint over the hermes kanban DB;
  enforces the audit invariant (every status change pairs a comment +
  `task_events` row)

See `docs/runbooks/swarm-sdlc-status-machine.md` for the state
machine the swarm walks.

## In flight

| ID | Slice | Why it ships now |
|----|-------|------------------|
| **S11-1** | Close the `gov.Gate` enforcement leak | Per 2026-05-11 spec slice 1: a denied tool call (`governance-mutation-authority-required`) logged `allowed:false` but the command still produced output. Observation without enforcement breaks the kernel-authority invariant. Per-CLI conformance tests assert `(chain shows allowed:false) AND (leaf CLI did not execute)`. |
| **S11-2** | `kanban-dispatch.lobster` slices 2-5: clawta classify, step-output interpolation, `spawn_worker`, per-leaf-CLI smoke | Pipeline scaffold exists; the four leaf-CLI legs aren't yet end-to-end-tested. |
| **S12** | `swarm-audit` reads via CLI + emits `swarm.audit.summary` chain events | Today's audit greps raw JSONL and writes a log file Hermes can't ingest. Two small CLI flags + a Python refactor make the chain the canonical observability surface. |
| **D1** | Chain-mined driver conformance gaps | High-leverage work comes from real `default-deny-unknown` rows: extend per-driver normalizers, then redeploy hooks/binaries so the chain proves the gap closed. |
| **AD** | `AllowedDrivers` as active kernel primitive | Layer Contract v1 #2 says the kernel exposes the primitive; today it's a passive schema field on `ExecutionRequest` (the Lobster `pick_driver` step ranks by cost from agent cards without asking the kernel what's allowed). With the swarm in-tree, `_pick_driver.py` and the kernel are one step apart — wire it, or update the contract. |

## Deferred

Real ideas waiting for operator demand or an asymmetric-strength signal:

- **Action vocabulary expansion** — extend `internal/gov/action.go` to cover the predictive-execution spec's `rewrite` / `redirect` / `stage` decisions. Lean into asymmetry without growing surface.
- **Chain summarization + causal slicing** — make the chain useful at higher abstraction than per-event.
- **Driver coverage** — add new drivers as the ecosystem's tool-call vocabulary stabilizes.
- **Policy packs** — per-domain rule bundles (security, cost, compliance) operators install via `chitin-kernel install-pack`. Step 4 of the strategic arc.
- **Semantic indexer over chain events** — vectorize chain payloads for fuzzy retrieval. Built on top of the 2026-05-12 event-emission contract.
- **Web/TUI dashboard** — chitin-dashboard spec; data source is the same CLI surface the 2026-05-12 spec aligns swarm-audit with.

## What chitin still does NOT do

(Narrowed from the 2026-05-06 list, which over-corrected. Hermes and
openclaw are upstream substrates the swarm composes, not external
boundaries.)

- **Own the kanban data.** Hermes ships the SQLite DB + UI; chitin reads and writes via `kanban-flow` (which goes through the hermes CLI). Don't denormalize kanban into chitin.
- **Ship a workflow engine.** Lobster (openclaw-side) executes `kanban-dispatch.lobster`. Chitin authors the workflow; it doesn't ship a runner.
- **Run a TypeScript/Temporal worker.** `apps/runner/` stays deleted. The clawta-poller tick + Lobster workflow replaces it.
- **Speak to LLM backends directly.** Each vendor's CLI talks to its own backend under its own auth. Chitin governs the actions those CLIs take.
- **Run as SaaS.** Local-only: operator's box, operator's data.

## Cut order if scope tightens further

When a future external-substrate survey shows another chitin surface that re-implements a substrate primitive, the cull pattern from 2026-05-08 still applies: invest deeper into asymmetric strengths (canonical vocabulary, chain depth, driver coverage, the four-hop pipeline's unifying contracts); divest from anything that duplicates a substrate's job.
