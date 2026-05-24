# Chitin Repo Constitution Overlay

<!--
Amendment 2026-05-22 — §7 added: "The swarm is the orchestrator."
Codifies the post-2026-05-20 SDLC refocus as gate-level truth. Driver
table enumerated (claudecode, openclaw=Clawta+GLM-5.1, hermes=Ares+
GPT-5.5, codex, copilot, local-llm, gemini). Implementation gate (MUST):
no implementation PR may be opened, no implementation file mutated, no
destructive implementation action may execute unless the work has first
entered the orchestrator as either a DAG-resolved work-unit or an
orchestrator-intaked ad-hoc work-unit. Ad-hoc top-of-funnel work
(reports, reviews, spec creation) remains kernel-gated but DAG-free.
§5 (board-aware scripts) and §6 (swarm/ as transitional housing) are
superseded where they describe kanban or swarm/ as live driving surfaces.
Ratified through multi-agent review with Ares (hermes) + Clawta
(openclaw) + operator (Jared); deep research grounding the architecture
in industry consensus lands separately at
docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md.
-->

<!--
Amendment 2026-05-20 — §2 strengthened: "the platform flow always uses
workers + worktrees" is now a hard MUST (was a soft convention). Rationale:
shared-checkout churn. Aligns with spec 070 (Chitin Orchestrator) FR-013/014.
No spec-kit template references §2's worktree rule — no template changes
required. Applied to both .specify/memory/constitution.md and
.specify/constitution.md.
-->

> Extends `~/workspace/.specify/constitution.md` with kernel- and swarm-specific
> rules. Never weakens a workspace-level invariant.

## 1. Side-effect boundary

The kernel (`chitin-kernel`) is the only component that gates tool calls and
writes to the event chain. Everything else in this repo — swarm scripts,
dispatch workflows, pollers, watchdog — reads kanban state and produces
side effects (PRs, comments, dispatch calls) through hermes or openclaw, never
through the kernel directly.

**Rule:** if a new swarm script needs to gate a tool call, it goes through
`chitin-kernel gate evaluate`. If it needs to dispatch work or mutate kanban
state, it goes through hermes or the lobster workflow. Never bypass the
kernel to write chain events; never bypass hermes to write kanban state.

## 2. Branch and worktree conventions

- Worker branches: `agent/<driver>-<hash>` (current), `swarm/<driver>-<hash>` (legacy)
- Integration branch: `main` (this is the chitin board's default branch)
- Workers PR against `main`, not against feature branches

**Rule — the platform flow ALWAYS uses workers + worktrees.** Every unit of
work — every worker dispatch, every agent action, every operator task — MUST
run as a worker process in its own dedicated git worktree. The primary
(shared) repository checkout is NEVER a work surface: nothing edits files,
runs a build, or commits branch work in it. Worktrees are created fresh per
work unit and torn down on completion; an orphaned worktree is reclaimable,
never silently reused.

**Rationale:** concurrent work in a shared checkout clobbers branches and
loses commits — observed repeatedly (a branch's commit landing on `main`, the
working tree switched mid-edit). Per-worktree isolation makes parallel work
deterministic. The Chitin Orchestrator (spec 070, FR-013/014) is the
mechanism that enforces this rule.

## 3. Spec-kit promotion gate

Before the `has_spec_kit_entry()` PR lands, the existing
`has_invariants_and_boundaries()` check in
`swarm/workflows/hermes-clawta-bridge.py` serves as the spec gate. Once
`has_spec_kit_entry()` ships, any ticket promoted `triage → ready` MUST have
a matching `.specify/specs/NNN-<slug>/spec.md` in this repo.

One-shot hotfixes and P0 escape hatches follow the workspace constitution §1.

## 4. Tracked installers

Every script that runs on the operator's box ships with an idempotent
installer under `swarm/bin/install-*.sh` that symlinks from the repo source
to the runtime location. Current installers:

| Script | Source | Installer |
|--------|--------|-----------|
| hermes-clawta-bridge | `swarm/workflows/hermes-clawta-bridge.py` | `swarm/bin/install-hermes-clawta-bridge.sh` |
| kanban-dispatch + deps | `swarm/workflows/*` | `swarm/bin/install-swarm-workflow.sh` |
| clawta-poller + guards | `swarm/bin/*` | `swarm/bin/install-clawta-poller.sh` |

New tooling MUST include its installer in the same PR.

## 5. Board-aware scripts

Scripts in `swarm/` that touch the kanban MUST accept a `--board` flag or read
`KANBAN_BOARD` from the environment. Hardcoding `chitin` is only acceptable as
a default, never as the only path. Board config is read via
`chitin-kernel board-config <slug>`.

## 6. Swarm tooling is the exception, not the pattern

`swarm/` is transitional housing for cross-repo operator tooling (constitution
§5). New tooling that is purely chitin-kernel-local (gate logic, chain
readers, driver adapters) belongs under `cmd/`, `internal/`, or `libs/` —
not in `swarm/`.

## 7. The swarm is the orchestrator

The chitin swarm IS the Go Temporal orchestrator (spec 070). Not a collection
of agents. Not a Discord channel. Not a folder of scripts. The orchestrator
drives; every unit of executable swarm work flows through it.

Three load-bearing parts:

- **Deterministic orchestration.** The spec drives. Every implementation work
  unit derives from a spec-kit task DAG (spec → plan → tasks → DAG); the
  orchestrator walks the DAG mathematically, not from heuristics, ambient
  chat, or board state.
- **Telemetry at every observable layer.** Every tool call, prompt boundary,
  driver-visible decision, result, and orchestrator transition is recorded in
  the chain. Hidden model reasoning is not captured (and not required).
  Telemetry observes; it never drives.
- **Kernel gates every tool call.** No tool call escapes `chitin-kernel gate
  evaluate`. No driver bypass. This applies to ALL driver activity —
  including conversational invocations of Ares/Clawta in Discord.

Agents are DRIVERS — not members, not peers. The recognised drivers:

| Driver       | Surface                  | Purpose                                                                                              |
|--------------|--------------------------|------------------------------------------------------------------------------------------------------|
| `claudecode` | terminal, synchronous    | Operator-pair / operator-steered coding driver (modes: in-person pair, /gold autonomous, remote)     |
| `openclaw`   | Discord, async (GLM-5.1) | Operator's async execution surrogate (Clawta); receives orchestrator-dispatched work, returns artifacts |
| `hermes`     | Discord, async (GPT-5.5) | Operator's async spec/review/coordination surrogate (Ares); turns intent into contracts, reviews drift |
| `codex`      | dispatched               | Implementation driver                                                                                |
| `copilot`    | dispatched (cloud)       | PR-review / second-opinion driver                                                                    |
| `local-llm`  | dispatched               | Local-model driver, capability-matched                                                               |
| `gemini`     | dispatched               | Additional driver, capability-matched                                                                |

Driver selection is by capability tag via the SelectDriver activity (spec 076),
not by the agent's own choice.

**The implementation gate (MUST).** No implementation PR may be opened, no
implementation file mutated, and no destructive implementation action may
execute unless the work has first entered the orchestrator as either a
DAG-resolved work-unit or an orchestrator-intaked ad-hoc work-unit.

This is intentionally **hierarchical**, not a flat swarm. The orchestrator is
the supervisor; drivers are capability-scoped executors. Parallelism is a
workflow decision made by the orchestrator from the spec DAG, not an emergent
property of agents talking to each other.

This partition matches the industry-standard safety layering for agent
systems: model / harness / tools / environment. Chitin does not own the
model layer; each driver brings its own model. Chitin owns the harness layer
through the orchestrator, the tools layer through `chitin-kernel gate
evaluate` and `chitin.yaml`, and the environment layer through worktree
isolation and auditable execution boundaries.

**Ad-hoc work allowed (kernel-gated, no DAG required).** Top-of-funnel and
mid-funnel work MAY enter via operator-facing surfaces (Discord, terminal,
MCP, CLI) without being a pre-existing DAG node. The kernel still gates every
tool call; only the DAG-pre-resolution requirement is relaxed. Examples:

- ✅ Operator: "Ares, write a spec for X." → Ares produces `spec.md`, opens a PR with it.
- ✅ Operator: "Clawta, review PR #42." → Clawta posts a review comment.
- ✅ Operator: "Claude Code, research multi-agent best practices." → produces a report committed to `docs/`.
- ✅ Cron / scheduled job: emit a daily report → commits to `docs/reports/`.
- ❌ Operator: "Ares, go change a bunch of code." → BLOCKED. Implementation requires a spec → DAG → work-unit → dispatched driver.
- ❌ Cron / poller: directly invoke a driver to make code changes → BLOCKED.

Reactive work (Discord mentions, operator escalations, cron triggers) that
produces implementation MUST enter the orchestrator as an ad-hoc work-unit
(not a pre-existing DAG node, but a work-unit nonetheless). The gate is the
same: the orchestrator is in the path. The source of the work-unit doesn't
exempt it from the rule.

Direct automation paths that bypass the orchestrator for implementation
work — Discord → gateway → driver → mutation; cron → driver → mutation;
script → driver → mutation; board/poller → driver → mutation — are drift
to be eliminated.

The implementation substrate (a new MCP server, Claude-Code skills, Hermes
tools, CLI commands, or another adapter — including the MCP 2026-07-28 Tasks
extension as a candidate worth its own spec) is a feature decision. The
constitutional gate is: **no implementation work reaches a driver except via
the orchestrator's dispatch from a work-unit (DAG-resolved or ad-hoc). All
other driver work is kernel-gated but not DAG-gated.**

**Supersedes.** This section supersedes:

- The "agent-as-swarm-member" framing in any earlier documentation.
- §5 (Board-aware scripts) and §6 (Swarm tooling is the exception) where they
  describe kanban or `swarm/` as a live driving surface — kanban-as-runtime
  retires under spec 087; `swarm/` retains its meaning only as a folder of
  operator-side glue, not as the swarm itself.
- Specifically retired as legacy implementation control paths (unless and
  until re-expressed as orchestrator work-units): agent-bus mention listeners
  that triggered code mutations, Discord-native agent-to-agent implementation
  dispatch, kanban pull loops, watchdog/poller actuation of mutations,
  cron-driven worker dispatch of code changes.

The console-side diagrams at `apps/chitin-console/src/app/pages/{sdlc,
orchestrator}-diagram.page.ts` and the strategy doc at
`docs/strategy/chitin-swarm-sdlc-model-2026-05-20.md` are the visual sources
of truth this section codifies.
