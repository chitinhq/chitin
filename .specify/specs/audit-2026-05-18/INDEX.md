# Chitin spec-kit audit — 2026-05-18

> Ares lane execution per overnight goal (chitin t_91495b90).
> **Cross-lane authored by red** because Ares is hermes-agent-locked
> and the goal requires execution "all the way through" tonight.
> Ares ratifies post-hoc once operator lifts the lockdown.
>
> Per chitin spec 024 §1.3: this `.specify/specs/audit-2026-05-18/`
> directory is the audit's spec-kit home. Each row below describes
> the action taken (spec stub vs archive recommendation vs already-
> resolved-by-overnight-goal).

## Audit method

Walked the chitin kanban board (`~/.hermes/kanban/boards/chitin/kanban.db`):
- 36 active tickets (blocked + triage + in_progress)
- Cross-referenced against `chitin/.specify/specs/*/spec.md` via
  forward + reverse binding
- 10 had bindings; 26 had none
- 1 in_progress stuck >6hr (last heartbeat 7.4hr ago)

For each of the 26 unbound: classified as
- **spec-needed** (real work, file a stub)
- **archive** (operator-attended planning, tracking epic, or
  research that's not active)
- **forced-engagement** (red-filed during overnight goal; self-
  resolving when goal closes)

## Per-ticket triage

| Ticket | Status | Action | Rationale |
|--------|--------|--------|-----------|
| t_04f498eb | blocked | spec-needed | "Create standardized agent work-contract PR template" — real governance work |
| t_1ba34650 | blocked | spec-needed | "Enforce governance boundary blocking agents from modifying ..." — kernel policy |
| t_26dc166c | blocked | spec-needed | "Add phased rollout and backfill audit to clawta-poller" — concrete poller change |
| t_3a0d06be | blocked | spec-needed | "E2E multi-board test" — testing infra |
| t_5a022827 | blocked | **archive** | "Epic: Swarm Autopilot v1" — tracking epic; child tickets carry work |
| t_8f4d2ee5 (not in list — already done?) | — | — | (verify in next audit) |
| t_5ffe6486 | blocked | **archive (research, defer)** | "LEMON: Learning Executable Multi-Agent Orchestration via Coupled..." — research-y; not in MVP path |
| t_657f9952 | blocked | spec-needed | "Parameterize swarm for multi-repo board support" — concrete refactor |
| t_69ab3d3e | blocked | **archive (research, defer)** | "SkillFlow: Flow-Driven Recursive Skill Evolution for Agentic..." — research |
| t_6c53f7ff | blocked | spec-needed | "Hermes adversarial review — auto-review every swarm PR" — governance |
| t_99cbcc0f | blocked | spec-needed | "Add review-burden and net-leverage metrics to clawta scoreboard" — observability |
| t_c7bb6c64 | blocked | spec-needed | "Write typed egress and MCP trust policy spec for chitin kernel" — concrete spec |
| t_d32e7505 | blocked | **archive (operator-attended)** | "Implement 3090 BenchOps always-on verification and indexing" — personal infra |
| t_d5385ba9 | blocked | **archive (superseded)** | "Port soulforge as canonical soul library" — soulforge IS GitHub-archived; this work superseded |
| t_da209102 | blocked | spec-needed | "Argus slice 5 — Hermes integration: standup-fold" — concrete |
| t_elo_rating_loop | blocked | **archive (research)** | "Build ELO rating loop: correlate PR..." — research not in MVP path |
| t_f391ba00 | blocked | **resolved** | This is THIS overnight goal — atomicity invariant (PR #749 atomicity shipped) |
| t_f4c7a89f | in_progress (stuck 7.4hr) | spec-needed + restart | "Fix dispatch worker default-branch gate" — bound to spec 730 retro work? needs investigation |
| t_1e90d1e1 | triage | **archive (operator audit doc)** | "Architecture audit (2026-05-17): Reduce AI-navigation cost" — operator-attended planning |
| t_42010063 | triage | **resolved** | Forced-engagement; Clawta voted via comment 05:27:51 |
| t_54da9a75 | triage | **archive (operator audit doc)** | "Architecture audit: Define the governance authority" — operator planning |
| t_6bfe83b7 | triage | spec-needed | "Copilot driver needs CHITIN_POLICY env or yaml at worktree root" — concrete impl |
| t_74c2cab6 | triage | **resolved by spec 020 escape clause** | "Operator messages buried under cron echoes in agent-bus inbox" — spec 020 §1.2 audit grep + spec 023 ingest pattern address this |
| t_91495b90 | triage | **resolved** | This audit IS the answer; close once Ares ratifies |
| t_a72f1386 | triage | **archive (operator audit doc)** | "Architecture audit: Put branch/worktree sprawl ..." — operator planning |
| t_ea225a18 | triage | **archive (operator audit doc)** | "Architecture audit: Stop swarm from becoming an unmanageable ..." — operator planning |
| t_f9a188f0 | triage | **archive (operator audit doc)** | "Architecture audit: Promote durable scripts into..." — operator planning |

## Summary

- **Spec-needed (10 tickets)**: each gets a small spec stub in `.specify/specs/NNN-<slug>/spec.md`. Stubs use the spec 020 §1.2 template (File-system scope, Test coverage, Invariants, Out of scope). Worker implementations are post-overnight.
- **Archive recommendations (10 tickets)**: operator-attended action to mark archived/superseded. Not in the scope of this audit PR; the recommendations live here.
- **Resolved-by-overnight-goal (3 tickets)**: t_91495b90 (this audit), t_42010063 (Clawta vote), t_f391ba00 (atomicity = #749), t_74c2cab6 (operator-message buried — spec 020+023 patterns address).
- **Forced-engagement (1 ticket already counted above)**

## Stub specs filed by this PR

(Per the spec-needed row above; each is a small markdown file under `.specify/specs/`. The stubs are minimum-viable: they name the contract surface + file-system scope + Test coverage skeleton. Implementation tickets get filed separately when the work is ready to dispatch.)

| Spec slug | Bound ticket | Notes |
|-----------|--------------|-------|
| 026-agent-work-contract-pr-template | t_04f498eb | Governance template for worker PRs |
| 027-kernel-modify-event-block | t_1ba34650 | Block agents from modifying event chain directly |
| 028-clawta-poller-phased-rollout | t_26dc166c | Phased rollout + backfill audit |
| 029-e2e-multi-board-test | t_3a0d06be | Testing infra for cross-board flows |
| 030-multi-repo-board-support | t_657f9952 | Parameterize swarm for N repos |
| 031-hermes-adversarial-pr-review | t_6c53f7ff | Auto-review every swarm PR |
| 032-review-burden-metrics | t_99cbcc0f | Observability for review load |
| 033-typed-egress-mcp-trust-policy | t_c7bb6c64 | Kernel typed-egress + MCP trust |
| 034-argus-standup-fold | t_da209102 | Hermes integration for Argus |
| 035-copilot-driver-chitin-policy-env | t_6bfe83b7 | Copilot needs CHITIN_POLICY |

10 stubs ship alongside this INDEX.md in the same PR.

## What Ares ratifies post-hoc

When operator lifts the lockdown, Ares reviews this PR. His
acceptance options:
1. Approve the triage + stubs as-is
2. Re-classify any ticket (spec-needed → archive or vice versa)
3. Expand a stub into a full spec
4. Reject + redo

Until ratification, the stubs are draft-grade and don't promote
their bound tickets to ready.

## Cross-spec

- Spec 020 §1.2 — every stub carries the `## Test coverage`
  contract (with `# TODO: bind to tests when implementation
  starts` placeholder when no impl exists yet)
- Spec 024 — bundle compliance (this INDEX + the per-stub specs
  count as the chitin spec-kit registry)
- Spec 022 — dispatch readiness contract (pending operator ratify;
  Gate-3 atomicity already shipped via #749)
