# Chitin Repo Constitution Overlay

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
