# Roadmap

The strategic arc, what's shipped, what's in flight, and what's deferred. Updated 2026-05-01.

> Companion: [`swarm-backlog.md`](./swarm-backlog.md) — tier-tagged execution backlog
> the local 24/7 swarm chews through. Roadmap = *strategy*. Backlog = *execution*.

## Strategic arc

```
   1. Aggregate          2. Align policy        3. Ecosystem
      capture across       to data (debt          distribution
      all drivers          ledger → rules)        (openclaw, …)
          │                    │                      │
          └────────►───────────┴────────►─────────────┤
                                                      │
   4. Policy packs      5. Cloud offering      6. North star
      reusable             centralized            autonomous swarm +
      governance           trace/policy           self-building product
      bundles              SaaS                   governed by chitin
          │                    │                      │
          └────────►───────────┴────────►─────────────┘
```

Each step funds and de-risks the next. We're between 1 and 2 today: aggregation is always-on; the policy layer is shipped but the rules are still mostly hand-written rather than ledger-derived.

## Shipped

### Phase 1 — Claude Code capture → replay (single-surface)

Nx + Vite+ scaffold, Go execution kernel (canon, normalize, emit, hook), `libs/contracts` (zod schema + generated Go types), `libs/telemetry` (JSONL tailer, SQLite indexer, replay), `libs/adapters/claude-code` (monitor-only PreToolUse hook), `apps/cli`. Local-only, fully offline.

### Phase 1.5 — surface-neutral event chain (merged 2026-04-19)

- v2 envelope: `chain_id` / `chain_type` / `parent_chain_id` / `seq` / `prev_hash` / `this_hash` / `schema_version: "2"`
- Go kernel: `init`, `emit`, `chain-info`, `ingest-transcript`, `install-hook`, `uninstall-hook`; transactional emit (`BEGIN IMMEDIATE`); JSONL→SQLite reconcile; canonical-JSON SHA-256 parity with TS
- Claude Code adapter: 7-hook forwarder (SessionStart / UserPromptSubmit / PreToolUse / PostToolUse / PreCompact / SubagentStop / SessionEnd)
- Ollama-local adapter: wrapper-mode session-chain
- Souls library: `souls/canonical/` (8 promoted) + `souls/experimental/` (7 provisional); `soul_id` + `soul_hash` populate `session_start.payload`

PR #1 (chain contract) + PR #2 (souls) merged into main.

### Governance v1 (merged 2026-04-28)

`gov.Gate.Evaluate(action, agent) → Decision` with closed-enum action vocabulary, policy evaluator, audit log to `gov-decisions-YYYY-MM-DD.jsonl`, escalation counter sticky in `~/.chitin/gov.db`. Three modes: monitor / enforce / guide. Kill switches: soft (`mode: monitor`) and hard (`gate lockdown`).

### Drivers (all three live)

- **Claude Code hook driver** — PR #66, cost-gov milestone C
- **Copilot CLI in-kernel SDK driver** — PR #51 (`chitin-kernel drive copilot`)
- **openclaw acpx config-override** — one-line install, no chitin-side wrapper code

The hero sentence names these three drivers. Bitrot in any is a hero-sentence bug.

### Layer Contracts v1 (locked 2026-04-29)

Four invariants documented at [`architecture/layer-contracts.md`](./architecture/layer-contracts.md): kernel authority, driver constraint (`AllowedDrivers` primitive), routing scope, aggregation role.

### `AllowedDrivers` primitive + Temporal swarm (merged 2026-05-01)

Three coordinated PRs ship the local 24/7 swarm:

- **PR #81 — slice 1+2** — Temporal worker (`apps/temporal-worker/`) + chitin-governance openclaw plugin (`apps/openclaw-plugin-governance/`). `ExecutionRequest` is the `AllowedDrivers` primitive in concrete form (`libs/contracts/src/execution-request.schema.ts`). End-to-end verified: Temporal → openclaw → plugin's `before_tool_call` hook → kernel deny.
- **PR #83 — slice 3a (core normalizer)** — openclaw pi-runtime tool names (`exec`, `process`, `read`, `write`, `edit`) mapped to canonical action types in `gov.Normalize()`.
- **PR #84 — slice 3 (chat-domain + routing + default-enforce)** — 14 chat-domain tools mapped (memory, sessions, image, ollama_web, cron, subagents) plus per-driver agent routing in `activity.ts` (`local-qwen → qwen-agent`, etc.) and default mode flipped from `observe` to `enforce`. End-to-end verified with qwen3-coder:30b dispatching the `read` tool.

Three planes locked: **Temporal** (control), **OpenClaw** (execution), **Chitin** (enforcement). Per the post-talk plan, this is the load-bearing piece for autonomous-swarm ops; the talk demos it live.

## In flight

Forcing function: **2026-05-07 talk** ("Copilot CLI Without Fear"). Eight days from this update.

| ID | Slice | Why it ships now |
|----|-------|------------------|
| **F4** | Thin OTEL emit MVP | Demo beat in talk. 4 event types only (`session_start`, `pre_tool_use`, `decision`, `post_tool_use`), OTLP HTTP JSON, async. Kernel-write-survives-OTEL-failure invariant. Parallel slice — does not bloat cost-gov v3. |
| **G2** | Memory rewrites + Layer Contracts doc + supersede headers + README first paragraph | Talk-readiness cleanup. **This roadmap rewrite is part of G2.** |
| **H** | Plan-enforcement design (committed f127b4f) | Spec-only pre-talk; implementation post-talk |
| **Cost-gov v3** | Bounded enforcement (`MaxToolCalls`, `MaxInputBytes`), tier classification (T0/T2 audit-log labels), cross-process envelope (`gov.db` WAL) | Supersedes v2. Spec/plan committed 2026-04-29 (c1ecbf9) |

Cut order if F4 slips: README first, then strategic-roadmap polish. Never cut Layer Contracts doc, supersede headers, or memory rewrites.

## Next slice (post slice-3 swarm merge)

The `AllowedDrivers` primitive shipped with slices 1–3 above. The swarm now runs but eats its own backlog (`swarm-backlog.md`); these are the strategic slices that aren't groomable to a tier yet:

1. **Pre-activity policy gate** — `chitin-kernel task validate` subcommand. The spec addendum committed to it; submit.ts currently bypasses (zod parse only). Tracked as `task-validate-command-pre-activity-gate` in the swarm backlog.
2. **Wall-timeout SIGKILL propagation** — current activity hangs to Temporal's 15-min `startToCloseTimeout` because openclaw's grandchildren keep stdout pipes open after parent dies. Blocking the swarm from running on slow models without retry pollution. Tracked in swarm backlog.
3. **Slice 4 scope decision** — open. Candidates on the deferred list below; needs a Jared+Claude Code interactive call to pick.

When (1) and (2) ship, terrain B (compute fabric / placement-as-policy) becomes a real public roadmap item with the swarm dogfooding it.

## Deferred (post-talk)

- Full `gen_ai.*` semconv compliance, OTLP-grpc, batching, multi-exporter, retries
- octi v2 spec edits to consume `AllowedDrivers` (pre-plan-handoff)
- A2 (platform/infra) and A4 (security/compliance) audience expansion — gated on A1 traction signals
- Terrain B formal milestones (compute-fabric public roadmap)
- Copilot CLI v2 spike — open-vendor in-process extension via `joinSession({tools, hooks})`. Earliest start: 2026-05-08.

## Audience sequencing (locked)

A1 (agent framework builders) → A2 (platform/infra) → A4 (security/compliance). A3 (solo operators) is a side channel. A1 messaging is not diluted with platform/dashboard/cost narratives.

## Phase 0 — archive predecessors (complete)

Renamed `chitinhq/chitin → chitinhq/chitin-archive` at `v1.0.0`; archived every other v1 repo in the org; created the new public MIT `chitinhq/chitin` monorepo. Hermes (the predecessor driver) was killed as a chitin component on 2026-04-23 — chitin is governance around openclaw + Claude Code, not a tick loop. See [`archive-map.md`](./archive-map.md) for what was extracted vs left behind.
