# Chitin spec-kit — INDEX

> Last updated 2026-05-19 (spec corpus train: Octi 040-049 + 054, Mini 050-053, SDD platform 060-065).
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
> (050 slice 2 + 051/052/053).

| Spec | Title | Status | What it owns |
|------|-------|--------|--------------|
| 050 | mini-mcp-spec-dispatch | ratified | Mini MCP server + spec-driven dispatch; slice 1 shipped (PR #795), slice 2 in PR #799 |
| 051 | mini-goalid-from-specs | draft | `goal_id` minted from the spec set a Mini session is opened against |
| 052 | agent-worktree-mention-guardrails | draft | Worktree + mention-addressing guardrails for agent sessions |
| 053 | mini-dispatch-via-kanban-driver | draft | Route Mini dispatch through the kanban driver (Option 3) |

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

10 other chitin tickets recommended for **archive** (operator-
attended; tracking epics, research deferred, operator-audit planning
docs, or work superseded by GitHub-archived upstreams). See
`.specify/specs/audit-2026-05-18/INDEX.md` for the per-ticket
triage rationale.

## Workspace-overlay & retro specs

| Spec | Title | Status | Notes |
|------|-------|--------|-------|
| 730 | path-scope-race-guards | shipped | Day-0 readybench portal retro fix (3-layer scope defense) |

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
