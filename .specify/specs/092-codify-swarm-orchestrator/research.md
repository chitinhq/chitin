# Phase 0 Research: Codify the swarm-is-orchestrator architecture

**Feature**: 092-codify-swarm-orchestrator
**Date**: 2026-05-22
**Status**: Decisions resolved; alternatives documented; deep research bound for `docs/strategy/`

This document records the decisions that resolved §7's design space. Each decision was settled in conversation across multiple turns with Ares (hermes/GPT-5.5) and Clawta (openclaw/GLM-5.1) reviewing and the operator (Jared) ratifying. Where industry research bears, it's cited; the full reference report lands as a separate PR at `docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md` per Ares's "keep constitutional amendments crisp" guidance.

## D1 — What is "the swarm"?

**Decision**: The swarm IS the Go Temporal orchestrator (spec 070). Not a collection of agents. Not a Discord channel. Not a folder of scripts.

**Rationale**: This matches the orchestrator-as-driver framing already encoded in the chitin-console SDLC diagram (`apps/chitin-console/src/app/pages/sdlc-diagram.page.ts`) and the 2026-05-20 strategy doc (`docs/strategy/chitin-swarm-sdlc-model-2026-05-20.md`). The deep research validates this empirically: Anthropic's production multi-agent research system uses an orchestrator-worker pattern (Claude Opus lead + Claude Sonnet workers) with reported 90.2% improvement over single-agent; Cognition shipped "Devin can now Manage Devins" (March 2026) converging on the same coordinator + isolated workers pattern; practitioner research (Beam.ai, Digital Applied) reports hierarchical wins over flat swarms because the supervisor anchors goal alignment.

**Alternatives considered**:
- **Agent-as-swarm-member** (the framing in earlier docs): rejected. Drivers don't have autonomous dispatch authority; the orchestrator does. Treating agents as peers creates the role-confusion symptom observed in this session (Clawta's inverted self-model, Claude Code's re-discovery of architecture every session).
- **Flat swarm** (no hierarchy): rejected. Practitioner consensus is unambiguous — swarms drift without a supervisor. Chitin's Temporal orchestrator IS the supervisor; flat would dismantle the deterministic-dispatch guarantee.
- **Multi-vendor swarm of swarms**: deferred. Not needed today; door not closed.

## D2 — What are agents, then?

**Decision**: Agents are DRIVERS — capability-tagged executors the orchestrator dispatches to. The canonical driver table:

| Driver | Surface | Purpose |
|---|---|---|
| `claudecode` | terminal, synchronous | Operator-pair / operator-steered coding driver (modes: in-person pair, /gold autonomous, remote) |
| `openclaw` | Discord, async (GLM-5.1) | Operator's async execution surrogate (Clawta); receives orchestrator-dispatched work, returns artifacts |
| `hermes` | Discord, async (GPT-5.5) | Operator's async spec/review/coordination surrogate (Ares); turns intent into contracts, reviews drift |
| `codex` | dispatched | Implementation driver |
| `copilot` | dispatched (cloud) | PR-review / second-opinion driver |
| `local-llm` | dispatched | Local-model driver, capability-matched |
| `gemini` | dispatched | Additional driver, capability-matched |

**Rationale**: Model-conditioned specialization is empirically supported. Anthropic's own pattern is heterogeneous (Opus lead + Sonnet workers); the Heterogeneous Swarms paper (arXiv 2502.04510) reports 18.5% gains over role/weight baselines. The model-tier signal from the operator — "ares uses gpt5.5, clawta uses glm5.1, better for each role" — assigns Ares (broader-context reasoner) to review/spec and Clawta (fast bounded executor) to implementation. This was the resolution to a real role conflict surfaced during the conversation (Clawta's self-model inverted; Ares's matched the operator signal).

**Alternatives considered**:
- **Uniform-pool drivers** (agent-agnostic, capability-only): partially adopted. Capability-tag routing via SelectDriver activity (spec 076) is preserved; uniform pool is rejected because the actual capabilities differ meaningfully across drivers.
- **Single-driver swarm**: rejected. Loses parallelism benefit; 15× token cost penalty without the parallelism payoff.

## D3 — What does "single surface" mean for the implementation gate?

**Decision**: The implementation gate is scoped to *executable swarm work*. Implementation work (code mutations, file edits, infrastructure changes, builds, deploys, anything that produces an executable artifact) MUST flow through the orchestrator as a DAG-resolved or ad-hoc orchestrator-intaked work-unit. Top-of-funnel and mid-funnel work (reports, research, code reviews, spec creation) MAY enter via operator-facing surfaces without a pre-existing DAG node — the kernel still gates every tool call, but the DAG-pre-resolution requirement is relaxed.

**Rationale**: Ares's "Single-surface dispatch rule" split was the key insight that resolved the original draft's contradiction (the early MUST said "no PR may be opened…" but the ad-hoc-allowed examples included "Ares writes a spec → opens a PR"). Tightening to "no *implementation* PR may be opened…" resolves the contradiction without sacrificing the gate's load-bearing intent.

**Alternatives considered**:
- **All driver work must be a DAG work-unit**: rejected. Makes spec creation a chicken-and-egg problem (you'd need a spec to write a spec) and over-constrains operator-pair conversation.
- **No gate at all on driver work**: rejected. Then the orchestrator isn't actually a supervisor; agents free-fire on operator request.

## D4 — How does reactive work fit?

**Decision**: Reactive work (Discord mentions, operator escalations, cron triggers) that produces implementation MUST enter the orchestrator as an ad-hoc work-unit (not a pre-existing DAG node, but a work-unit nonetheless). The gate is the same: the orchestrator is in the path. The source of the work-unit doesn't exempt it from the rule.

**Rationale**: Clawta's edit on the prior draft. Prevents the loophole where someone could DM a driver "go change this code" and bypass the orchestrator because "it's reactive, not DAG." Closing this loophole was important for the gate to be actually load-bearing.

**Alternatives considered**: exempting reactive work from the gate — rejected outright (would defeat the whole gate).

## D5 — Is MCP 2026-07-28's Tasks extension the implementation substrate?

**Decision**: NOT in the constitution. §7 names it as "a candidate worth its own spec." A separate spec evaluates MCP Tasks against alternatives (Claude-Code skills, Hermes tools, a new internal MCP server, CLI commands) before any commitment.

**Rationale**: Both Ares and Clawta argued (correctly) that the constitution states what MUST be true, not which implementation surface achieves it. The deep research showed MCP Tasks is the architecturally cleanest candidate (stateless, async, task-handle-based, exactly the shape Chitin needs) — but it's still a release candidate (final 2026-07-28) and the spec-vs-substrate decision deserves its own focused review.

**Alternatives considered**:
- **MCP Tasks adopted in §7**: rejected per above.
- **Claude-Code skills as the single surface**: deferred to the same future spec.
- **A new internal MCP server**: deferred to the same future spec.

## D6 — What about Anthropic citations in the constitution body?

**Decision**: No vendor names in §7's prose. Use "industry-standard safety layering" for the 4-layer reference. Citations to Anthropic, Cognition, etc. live in `docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md`, not in the constitution.

**Rationale**: Ares's catch. The constitution is gate-level — it states truths, not who said them. Vendor citations create maintenance debt and tie the constitution to external publication state.

**Alternatives considered**:
- **Cite Anthropic directly in §7**: rejected per above.
- **No 4-layer reference at all**: weaker. The 4-layer framing is genuinely useful as a mental model; just don't name who codified it.

## D7 — Should the deep research report bundle with §7?

**Decision**: Separate PR. Research report at `docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md`; §7 amendment alone in the constitution PR.

**Rationale**: Ares's call. Constitutional amendments need crisp, reviewable diffs. The research is valuable but is reference material, not part of the gate. Bundling them obscures what's being ratified.

**Alternatives considered**:
- **Bundle research with §7 PR** (Clawta's preference): rejected per Ares's argument. The reviewer of the §7 amendment shouldn't have to read 800+ lines of research to evaluate the amendment.

## D8 — Where does Clawta's inverted self-model get fixed?

**Decision**: Separate operator action. The §7 driver table is canonical for the constitution; Clawta's startup context patch (so its self-description matches the constitution) is filed as a small follow-up operation, possibly outside this repo.

**Rationale**: Constitutional amendments shouldn't be bundled with per-driver prompt patches. The constitution names the truth; downstream startup configs are patched separately.

**Alternatives considered**:
- **Patch Clawta context in this PR**: rejected — Clawta's context likely doesn't live in this repo.
- **Wait to ship §7 until Clawta is patched**: rejected — they're independent.

## Cross-references for the implementation phase

- Canonical §7 text: `contracts/canonical-section-7.md` — the implementation contract.
- Visual sources of truth: `apps/chitin-console/src/app/pages/sdlc-diagram.page.ts`, `apps/chitin-console/src/app/pages/orchestrator-diagram.page.ts`.
- Strategy doc this codifies: `docs/strategy/chitin-swarm-sdlc-model-2026-05-20.md`.
- Predecessor specs: 070 (Chitin Orchestrator), 076 (SchedulerWorkflow + SelectDriver), 077 (Spec-Kit adapter), 081 (Temporal Schedules), 087 (kanban substrate retirement, in flight).
- Companion PR (separate): `docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md` — the deep research report.
- Follow-up specs (queued, ordered): spec 091 AC amendments → no-driver-bypass invariant test → plan-level review checkpoint → fan-out cap → verifier-driver formalization → MCP Tasks ingress evaluation.
