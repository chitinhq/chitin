# 069 — Decommission the agent-bus and the mini wrapper

> Operator directive 2026-05-20: kill the **agent-bus** (unreliable; the
> kanban board is the proven coordination channel) and the **mini** wrapper
> (a remote-control wrapper around the Claude Code CLI — "not necessary
> right now"). Full removal.
>
> **Re-scope note:** an earlier draft of 069 said "and Octi." That was a
> naming collision — the "Octi" specs 040–048 are the **Temporal
> orchestration** design that becomes the Chitin Orchestrator (spec 070)
> and are explicitly **not** killed. Only the `mini` / `mini-mcp` CC-CLI
> wrapper is decommissioned here.

## Ticket refs

- chitin — implementation ticket derived from `tasks.md`

## File-system scope

**Repo (branch 069 → PR):**
- `services/agent-bus/` — delete
- `services/mini-mcp/` — delete (the mini CC-CLI wrapper MCP)
- `swarm/mini/` — delete (mini runtime)
- `services/swarm-kanban-mcp/` — drop `post_swarm_message` (the
  agent-bus hook) from implementation, docs, and tests; the board server
  otherwise untouched
- `swarm/bin/mini*`, `swarm/bin/install-mini*`,
  `swarm/bin/install-agent-bus-cron.sh` — delete stale launchers/installers
- `swarm/tests/test_mini_*.py` — delete tests for removed Mini runtime
- `.specify/specs/INDEX.md` — mark spec 001 + mini specs 050–053 superseded

**System (applied directly; ✓ = already done):**
- `~/.claude.json` + `~/.codex/config.toml` — agent-bus & mini MCP unwired ✓
- `~/.hermes/cron/jobs.json` — `agent-bus-inbound-poll` cron removed ✓
- Octi `redis.service` — stopped + disabled ✓
- `~/.claude/settings.json` — SessionStart agent-bus inbox hook removed
- `~/.chitin/agent-bus/bus.db*` — bus database removed

## Goal

The kanban board is the swarm's sole coordination channel. The agent-bus
and the mini CC-CLI wrapper are fully removed — no process, cron, MCP
wiring, hook, or repo code references either.

## Acceptance criteria

AC1. `services/agent-bus/` no longer exists; nothing in the repo imports it.
AC2. `services/mini-mcp/` and `swarm/mini/` no longer exist.
AC3. `~/.claude.json` and `~/.codex/config.toml` list neither `agent-bus`
nor `mini`; `swarm-kanban` is untouched. ✓
AC4. No `agent-bus-inbound-poll` cron (✓) and no SessionStart bus-inbox hook.
AC5. `swarm-kanban-mcp` imports and serves the board with `post_swarm_message`
removed — zero import of `services/agent-bus`.
AC6. The Octi `redis.service` is stopped and disabled. ✓
AC7. `INDEX.md` records spec 001 and mini specs 050–053 as superseded by
069. Specs 040–048 are **untouched**.
AC8. No in-repo launcher, installer, or test imports the removed
`swarm.mini`, `services/mini-mcp`, or `services/agent-bus` code.

## Invariants

- `swarm-kanban-mcp` is touched only to remove the one bus call — board
  coordination survives intact.
- Specs 040–048 (Temporal orchestration) are NOT marked superseded — they
  are re-homed under spec 070.

## Out of scope

- The Temporal orchestration specs 040–048 → spec 070 (Chitin Orchestrator).
- `swarm/octi/` agent capability profiles → the naming rollout.
- The broader dead-weight cull → spec 071.
- The hermes-native cron `deliver: discord:#x` path — independent of the
  agent-bus; it stays.

## Open questions

- None — operator clarified: full removal, mini included, Octi/Temporal kept.
