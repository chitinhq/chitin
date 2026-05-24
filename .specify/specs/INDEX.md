# Chitin spec-kit — INDEX

> Last updated 2026-05-23 (added spec 092 no-driver-bypass invariant-test draft; note: 087-091 are in-flight on open spec PRs and may not be present on this base branch yet).
> Per chitin spec 024 §1.3: every active repo carries `.specify/specs/INDEX.md`.
>
> Status legend: **shipped** = merged + deployed; **ratified** = spec
> merged, impl ticket open; **draft** = spec PR open; **archived** =
> superseded or rolled back.

## Active specs (governance / dispatch / agent-bus)

| Spec | Title | Status | Bound ticket | Notes |
|------|-------|--------|--------------|-------|
| **001** | agent-bus | shipped | (multiple Phase 1-5) | Phase 5 in flight as `t_18ffbbb3` (typed attachment renderers) |
| **018** | dispatch base-freshness | shipped | (chitin task #64) | Lobster patch + 6 regression tests; PR #738 |
| **020** | SDD+TDD enforcement (L1/L2/L3 + §1.2) | ratified | `t_f06d2a5b` | 3-layer hook + gate + constitution §1.2; PR #740 merged; impl pending operator promotion |
| **021** | agent-bus discord mirror env-refresh | shipped | superseded by 023 | History-only; merged PR #741 |
| **022** | dispatch readiness contract | draft | — | PR #744 open; spec 022 unifies poller + watchdog spec-root resolution + boundary checks |
| **023** | agent-bus bidirectional liveness | shipped | — | PR #746 merged; outbound env-refresh + inbound `agent-bus-inbound-poll` cron + bidirectional e2e |
| **024** | active-repo doc-bundle contract | shipped | (this overnight sprint) | PR #747 merged; the spec defining this INDEX file |

## Spec-kit foundation (mostly shipped)

| Spec | Title | Status | Notes |
|------|-------|--------|-------|
| 002 | scripts-manifest | shipped | Source of truth for tracked operator-box scripts |
| 003 | kanban-isolation | shipped | Per-board kanban DB separation |
| 004 | driver-allowlist | shipped | `_pick_driver` only routes to kernel-approved drivers |
| 005 | drift-guard | shipped | Watchdog detection of spec/code drift |
| 006 | wiki-pipeline | shipped | `/wiki` slash command; LLM-compiled knowledge base |
| 007 | analyzer-cron | shipped | Sentinel-driven analyzer cron |
| 008 | watchdog-spec-aware | shipped | Watchdog respects `.specify/specs/` for promotion decisions |
| 009 | poller-respects-spec-kit | shipped | Same for poller |
| 010 | pr-lifecycle-exact-match | shipped | Strict PR-lifecycle state matching |
| 011 | script-coverage-agent-unlock | shipped | Tests for `chitin agent unlock` |
| 012 | script-coverage-chain-watch | shipped | Tests for chain-watch script |
| 013 | script-coverage-envelope-rotate | shipped | Tests for envelope rotator |
| 014 | script-coverage-install-kernel | shipped | Tests for install-kernel script |
| 015 | diagnostics-mutation-separation | shipped | Diagnostics read-only; mutation gated |
| 016 | watchdog-prompt-durability | shipped | Watchdog prompt stable across cron restarts |
| 017 | poller-dependency-unblock-veto | shipped | Poller honors `Blocked until:` veto in bound specs |

## Octi orchestration plane (Temporal Go) — PR 1/3 (foundation)

> Ratified 2026-05-19 via agent-bus thread 19. Three proposals (Ares
> msgs 7740-7743, Clawta msg 7722, claude-code msg 7726); hybrid
> ratified — Ares 5-role frame + Clawta conflict_sets + claude-code
> derived confidence. Parent: spec 038. Operator override (2026-05-19):
> Ares = research + spec-reviewer + board-groomer; #swarm + #mini +
> #hermes deleted, only #ares + #clawta survive.
>
> Split across 3 PRs to honor chitin bounds:max_lines_changed (2000):
> this PR (foundation) = 040 + 049 + 7 capability profile YAMLs.

| Spec | Title | Status | What it owns |
|------|-------|--------|--------------|
| **040** | octi-scaffolding | draft | Temporal Go module + `workflowcheck` CI gate + hello-world workflow |
| **049** | octi-swarm-role-architecture | draft | 6 roles, capability schema, handoff packet, derived confidence — BEHAVIOR layer above 040-048 |

Capability profiles under `swarm/octi/config/capability_profiles/`:
ares, claude, clawta, mini, copilot, codex, claudecode. Operationalize
spec 049 §R6.

## Octi orchestration plane (Temporal Go) — PR 2/3 (critique closures)

> Ratified 2026-05-19 via agent-bus thread 19. Parent: spec 038.
> Split across 3 PRs for chitin bounds:max_lines_changed (2000):
> PR 1 = 040 + 049 + capability profiles; this PR (2/3) = the three
> Clawta-critique-closure specs; PR 3 = workflow migrations.

| Spec | Title | Status | Closes |
|------|-------|--------|--------|
| **041** | octi-event-mirror-contract | draft | Clawta critique #1 — replay from telemetry alone, no Temporal-visibility dependency |
| **042** | octi-agentbus-identity-contract | draft | Clawta critique #2 — anchor + dedup + multi-audience fan-out (post-#swarm-deletion) |
| **047** | octi-mention-routing-workflow | draft | Clawta critique #3 — listener ownership (narrowed: per-agent channels only) |

## Octi orchestration plane (Temporal Go) — PR 3/3 (workflow migrations)

> Ratified 2026-05-19 via agent-bus thread 19. Parent: spec 038.
> Split across 3 PRs for chitin bounds:max_lines_changed (2000):
> PR 1 = 040 + 049 + capability profiles; PR 2 = critique closures
> (041/042/047); this PR (3/3) = the workflow migrations that port
> today's cron/lobster sprawl onto Octi Temporal workflows.

| Spec | Title | Status | Migration target |
|------|-------|--------|------------------|
| **043** | octi-dispatch-workflow | draft | `kanban-dispatch.lobster` (6-stage pipeline) |
| **044** | octi-poller-workflow | draft | `swarm/bin/clawta-poller` |
| **045** | octi-bridge-workflow | draft | `hermes-clawta-bridge.py` |
| **046** | octi-autonomous-claim-workflow | draft | `autonomous-board-engine.sh` |
| **048** | octi-ha-migration-template | draft (template) | tripwired `start-dev` → HA cluster |

## Octi assembly-line process spec

> Spec 054 is the **process spec** — it sequences the Octi role
> architecture (049) and runtime (040-048) into one end-to-end
> 10-stage, 2-gate deterministic assembly line. On ratification it
> supersedes `workspace/claude/skills/spec-factory.md` as the
> canonical swarm operating procedure. Awaiting Ares + Clawta
> alignment sign-off, then operator ratification.

| Spec | Title | Status | What it owns |
|------|-------|--------|--------------|
| **054** | octi-assembly-line | draft | The canonical 10-stage / 2-gate swarm operating procedure — ties 038 + 040-049 into one process |

## Mini worker plane

> The Mini session primitive — the L4 worker layer the Octi controller
> dispatches into. Specs landed via PR #795 (050 slice 1) and PR #799
> (050 slice 2 + 051/052/053). Superseded by spec 069; retained here
> only as historical index context.

| Spec | Title | Status | What it owns |
|------|-------|--------|--------------|
| 050 | mini-mcp-spec-dispatch | superseded by 069 | Mini MCP server + spec-driven dispatch |
| 051 | mini-goalid-from-specs | superseded by 069 | `goal_id` minted from the spec set a Mini session is opened against |
| 052 | agent-worktree-mention-guardrails | superseded by 069 | Worktree + mention-addressing guardrails for agent sessions |
| 053 | mini-dispatch-via-kanban-driver | superseded by 069 | Route Mini dispatch through the kanban driver (Option 3) |

## SDD platform — charter 060 + roadmap 061-065

> The chitin spec-driven-development platform (PR #803). Charter spec
> 060 ratifies the 7-layer stack; specs 061-065 realize the gaps, built
> bottom-up: 061 → 062 → 063 → 064 → 065. Strategy narrative:
> `docs/strategy/chitin-spec-driven-platform.md`.

| Spec | Layer | Title | Status | Triage ticket |
|------|-------|-------|--------|---------------|
| **060** | — | chitin-sdd-platform-charter | ratified (operator) | — |
| 061 | L1 | unified-spec-model | draft | `t_095e6cf0` |
| 062 | L2/L3 | spec-build-attribution | draft | `t_0291fcfc` |
| 063 | L5 | cross-layer-replay | draft | `t_87eeb464` |
| 064 | L6 | telemetry-spec-feedback | draft (Q1+Q2 resolved) | `t_c2c59167`; PR #805 |
| 065 | L7 | goal-rebuild-engine | draft | `t_aaf68eaa` |

## Grooming observability — spec 066

> Spec 066 adds structured decision records and drift analysis to the
> kanban grooming loop (spec 054 stage 8 → stage 0 flywheel telemetry).

| Spec | Layer | Slug | Status | Bound ticket |
|------|-------|------|--------|---------------|
| 066 | 8→0 | grooming-telemetry | draft | `t_70a085ab` |

## CLAWTA_IMPLEMENTER_LANES — spec 067

> Spec 067 defines the two-path routing split for assignee=clawta
> tickets: routing (→ terminal worker via _pick_driver) vs. implementer
> (→ Clawta directly when Stage 5 handoff present). Parent: specs 049, 054.

| Spec | Layer | Slug | Status | Bound ticket |
|------|-------|------|--------|---------------|
| 067 | dispatch | clawta-implementer-lanes | draft | `t_5bb1151a` |

## Icarus bench loop revival — spec 068

> Spec 068 reverses the retirement of the Icarus terminal-bench loop (PR
> #794) to get bench runs flowing immediately while the v2 Harbor agent
> (specs 036/038) is spec'd but not yet implemented. Review ticket
> `t_1615b319` (done). T001 shipped; T002–T007 pending.

| Spec | Title | Status | Bound ticket | Notes |
|------|-------|--------|--------------|-------|
| 068 | icarus-bench-loop-revival | ratified | `t_1615b319` | Non-stop bench loop; PR #826 |

## Dispatch invariants — spec 025

| Spec | Title | Status | Bound ticket | Notes |
|------|-------|--------|--------------|-------|
| 025 | dispatch-atomicity-invariant | draft | — | Block↔close single-owner invariant |

## Spec stubs from 2026-05-18 chitin spec-kit audit

> Filed during the overnight goal's Ares-lane audit. Cross-lane
> authored by red because Ares is hermes-agent-locked at the time;
> Ares ratifies post-hoc. Each stub is draft-grade and doesn't
> promote its bound ticket to ready until ratification.

| Spec | Title | Bound ticket | Status |
|------|-------|--------------|--------|
| 026 | agent-work-contract-pr-template | t_04f498eb | draft (stub) |
| 027 | kernel-modify-event-block | t_1ba34650 | draft (stub) |
| 028 | clawta-poller-phased-rollout | t_26dc166c | draft (stub) |
| 029 | e2e-multi-board-test | t_3a0d06be | draft (stub) |
| 030 | multi-repo-board-support | t_657f9952 | draft (stub) |
| 031 | hermes-adversarial-pr-review | t_6c53f7ff | draft (stub) |
| 032 | review-burden-metrics | t_99cbcc0f | draft (stub) |
| 033 | typed-egress-mcp-trust-policy | t_c7bb6c64 | draft (stub) |
| 034 | argus-standup-fold | t_da209102 | draft (stub) |
| 035 | copilot-driver-chitin-policy-env | t_6bfe83b7 | draft (stub) |
| 037 | sw-011-heartbeat-proof-tests | — | draft (stub) |
| 039 | mini-discord-inbound | — | draft |

10 other chitin tickets recommended for **archive** (operator-
attended; tracking epics, research deferred, operator-audit planning
docs, or work superseded by GitHub-archived upstreams). See
`.specify/specs/audit-2026-05-18/INDEX.md` for the per-ticket
triage rationale.

## Monorepo platform — spec 074

> Spec 074 closes the Nx polyglot registration gap (Go/Python projects
> invisible to `nx affected`/graph/cache) and converges the folder layout
> to a single type-first `apps/`+`libs/` scheme. Four independently
> shippable phases (Phase 0 culls dead drift); coordinates with deletion
> specs 069/070. Companion analysis: `docs/strategy/chitin-monorepo-audit.md`.

| Spec | Title | Status | Bound ticket | Notes |
|------|-------|--------|--------------|-------|
| 074 | polyglot-monorepo-layout | draft | — | P0 cull drift → P1 registration gap → P2 convention → P3 layout convergence |

## Chitin Orchestrator — specs 070 + 075-081

> The agent-agnostic, Temporal-based orchestration platform, and the
> self-improvement loop built on it. Spec 070 (operator-ratified engine:
> Temporal Go, 2026-05-20) is the durable-execution foundation; 075 the
> agent-driver contract; 076 the spec-DAG scheduler that replaces the
> kanban pull-loop; 077 the kit-agnostic spec adapter; 078-079 the
> self-improvement loop and its information-ingestion front-end. Octi
> specs 040-048 are re-homed here. Implementation: PR #886. Strategy:
> `docs/strategy/chitin-orchestrator-options-2026-05-20.md`.

| Spec | Title | Status | What it owns |
|------|-------|--------|--------------|
| **070** | chitin-orchestrator | draft | Temporal Go durable-execution platform; worktree isolation; migration off cron/script sprawl |
| **075** | agent-driver-contract | draft | The `AgentDriver` interface, driver registry, capability cards — plug in any agent, zero core change |
| **076** | spec-dag-scheduler | draft | Specs → dependency DAG; deterministic scheduler; agent vs deterministic nodes; replaces the kanban pull-loop (070 FR-015) |
| **077** | spec-kit-adapter | draft | Kit-agnostic compile (spec-kit / OpenSpec / superpowers) → the normalized 076 DAG |
| **078** | self-improvement-loop | draft | Telemetry → analysis → spec proposals → [human gate] → implementation; generalizes Sentinel (064) |
| **079** | information-ingestion-pipeline | draft | External-knowledge front-end: broad-net gathering + signal/noise filter feeding 078's proposals |
| **080** | orchestrator-ops-completion | draft | Gemini + Copilot agent drivers (roster 5→7); write-only Discord notification surface; chitin-console as a first-class systemd service |
| **081** | cron-migration-board-retirement | draft | Phase 3–5: migrate the ~15 swarm crons/watchdogs to Temporal scheduled workflows; retire the kanban-era board read-model |
| **092** | no-driver-bypass-invariant | draft | Executable invariant test: implementation-producing driver invocations must carry orchestrator work-unit attribution; direct driver bypasses fail closed |

## Merge orchestration + review — specs 093 + 094

> The PR-merge workload itself becomes a first-class orchestrator
> workflow. Direct dogfood of constitution §7 ("the swarm is the
> orchestrator"): no more ad-hoc `gh pr merge` calls. Multi-repo,
> queue-aware, policy-gated. Lives in the same Go worker as 070-081.
> Spec 094 adds the dialectic PR-review mechanism (two primary
> reviewer drivers + class-routed arbiter) that 093's policy table
> subscribes to for governance, spec-only, impl, and research-docs
> classes via a v1.1.0 amendment after 094 ratifies.

| Spec | Title | Status | What it owns |
|------|-------|--------|--------------|
| **093** | merge-queue-orchestrator | draft | `MergeQueueWorkflow` parent + `PRMergeWorkflow` child; 6-class policy table (governance / live-fix / spec-only / research-docs / impl / bookkeeping); pointer-file auto-resolve invariant; lease-protected force-push; signal-blocked governance gate honoring spec 092's no-bypass invariant. v1.0 ships standalone; v1.1.0 amendment adds `review_required` + `arbiter_type` columns once 094 ratifies. |
| **094** | pr-review-mechanism | draft | `reviewer` capability tag on the spec 075 driver registry; `SelectDriver` extended with capability-filter + no-self-review exclusion; `PRReviewWorkflow` child spawned by 093's PRMergeWorkflow; parallel two-primary dispatch; dialectic short-circuit on agreement; class-routed arbiter (operator via structured GitHub PR comment for governance/spec-only; third machine driver for impl/research-docs once a 3rd reviewer driver lands); 4-value `StructuredVerdict` (approve / approve-with-comments / request-changes / abstain) with FR-014 invariants; `re-review` + `override-review` signals (governance non-overridable); OTLP per-invocation audit trail with content hashes. |

## Workspace-overlay & retro specs

| Spec | Title | Status | Notes |
|------|-------|--------|-------|
| 728 | dispatch-default-branch-fix | shipped | Respect board default_branch in worker commit gate |
| 730 | path-scope-race-guards | shipped | Day-0 readybench portal retro fix (3-layer scope defense) |

## ⚠️ Spec-number collisions — operator resolution needed

These spec-dir prefixes are claimed by more than one directory on disk.
Each needs an operator ruling (renumber or suffix) before the registry
can show a single canonical row:

- **036** — `036-dispatch-fault-tolerance-invariants`,
  `036-ic-001-icarus-local-llm-driver`, `036-icarus-harbor-agent-adapter`
- **038** — `038-icarus-harbor-agent-adapter`,
  `038-octi-persistent-claude-session`
- **067** — `067-clawta-implementer-lanes`, `067-tasks-to-tickets`
  (the section above lists `clawta-implementer-lanes`; the other is unlisted)
- **071** — `071-chitin-coach`, `071-kanban-block-invariant-fix`
  (both unlisted pending the ruling)

## How this file is maintained

- Maintainer: chitin AGENTS.md owner (Ares lane in the overnight roadmap).
- Updated whenever a new spec lands or a status changes. Spec status
  transitions: `draft → ratified → shipped → archived`.
- The active-specs section ordering: newest at top (high-leverage
  recent work surfaces first).
- Each row links to its spec.md once the spec PR lands; for in-flight
  specs the PR# is referenced.

## Spec-kit conventions (per chitin spec 020 + 024)

- Every spec carries: `## File-system scope`, `## Test coverage`
  (e2e-default per §1.2), `## Acceptance Criteria`, `## Invariants`,
  `## Out of scope`.
- Every test file (under recognized globs) carries
  `// spec: NNN-<slug>` (or `# spec:`) reference comment in first 20
  lines.
- Workers MUST NOT promote a ticket to `ready` without a `Spec:
  NNN-<slug>` reference in the body that resolves to an existing
  spec.md here.

## Decommissioned (spec 069 — 2026-05-20)

Operator directive: the kanban board is the swarm's sole coordination
channel. The following specs are **superseded** — their subsystems are
removed by spec `069-decommission-agent-bus-mini`:

- **001** (agent-bus) — the agent-bus is decommissioned (unreliable; the
  board replaced it).
- **050–053** (Mini worker plane) — the `mini` Claude-Code-CLI wrapper is
  decommissioned ("not necessary right now").

Specs **040–048** ("Octi" Temporal orchestration) are **NOT** superseded —
they are re-homed under spec `070-chitin-orchestrator` (Chitin Orchestrator).
