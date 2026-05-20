# Chitin spec-kit — INDEX

> Last updated 2026-05-19 (Octi orchestration corpus 040-048 drafted).
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

## Octi orchestration plane (Temporal Go)

> Ratified 2026-05-19 via agent-bus thread 17 msg 7702. Three
> proposals received (Ares, Clawta, claude-code); hybrid ratified —
> Temporal Go + Clawta's three critiques baked in. Parent: spec 038
> ("Octi — Deterministic Workflow Governance"), slice 4 ("Octi
> worker + Temporal integration").
>
> Ares' chitin-native counter-proposal absorbed as constraints, not
> rejected: Temporal MUST NOT become a second source of truth;
> chitin-kernel gate remains the floor; CI-enforced `workflowcheck`
> is non-negotiable; every workflow step is replayable bit-for-bit
> from Octi/chitin telemetry alone.

| Spec | Title | Status | Maps to | Bake target |
|------|-------|--------|---------|-------------|
| **040** | octi-scaffolding | draft | Temporal Go module + `workflowcheck` CI gate + hello-world workflow | n/a — foundation |
| **041** | octi-event-mirror-contract | draft | Clawta critique #1 — replay from telemetry alone | spec 040 §R8 stub |
| **042** | octi-agentbus-identity-contract | draft | Clawta critique #2 + thread-1-vs-thread-17 routing failure | spec 023 mirror |
| **043** | octi-dispatch-workflow | draft | Port `kanban-dispatch.lobster` (6-stage pipeline) | `~/.openclaw/workflows/kanban-dispatch.lobster` |
| **044** | octi-poller-workflow | draft | Replace `clawta-poller` cron | `swarm/bin/clawta-poller` |
| **045** | octi-bridge-workflow | draft | Replace `hermes-clawta-bridge.py` | `~/.hermes/scripts/hermes-clawta-bridge.py` |
| **046** | octi-autonomous-claim-workflow | draft | Replace `autonomous-board-engine.sh` | `~/.hermes/scripts/autonomous-board-engine.sh` |
| **047** | octi-mention-routing-workflow | draft | Clawta critique #3 — listener ownership preserved | `swarm/bin/{clawta,mini}-mention-listener` |
| **048** | octi-ha-migration-template | draft (template) | Tripwired `start-dev` → HA cluster | template only — not actionable until tripwire fires |
| **049** | octi-swarm-role-architecture | draft | 6 roles, capability schema, handoff packet, derived confidence — the BEHAVIOR layer above 040-048 | ratified thread 19 + operator override 2026-05-19 (Ares = research + spec-review + board-groom + pr-reviewer-signal); reconciles `spec-factory.md` |

Sequencing: 040 ships first (foundation). 041 + 042 close the
critique gaps before any production migration. 043 is the
highest-value single migration (kanban-dispatch.lobster). 044-047
follow per-surface. 048 is a template, dormant until a measurable
tripwire fires.

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
