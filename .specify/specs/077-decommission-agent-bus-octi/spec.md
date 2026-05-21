# 077 — Decommission the agent-bus and Octi

> Operator directive 2026-05-20: kill the agent-bus — it is unreliable
> and the kanban board is the proven coordination channel. The spec-068
> handoff ticket `t_1615b319` drew full decisions from both Ares and
> Clawta within 13 minutes; the parallel agent-bus thread drew nothing.
> **Full removal.** Octi is killed in the same pass — Octi is bus-native
> (its event mirror, identity contract, and mention routing all run on
> the agent-bus: specs 041/042/047), so it cannot outlive the bus.

## Ticket refs

- chitin — implementation ticket derived from `tasks.md`

## File-system scope

**Repo (branch 069 → PR):**
- `services/agent-bus/` — delete (server, db, inbox, Discord bridge, tests)
- `services/mini-mcp/` — delete (Octi MCP server)
- `swarm/mini/` — delete (Octi runtime code)
- `services/swarm-kanban-mcp/server.py` — drop the `post_swarm_message`
  agent-bus hook; the board server otherwise untouched
- `.specify/specs/INDEX.md` — mark spec 001 + the octi specs superseded
- `.claude/worktrees/octi-spec-corpus` — remove the git worktree
- `docs/strategy/` operating-model doc + Ares `ROLE.md` — board-only
- `.specify/specs/077-decommission-agent-bus-octi/` (this spec)

**System (done directly — not version-controlled):**
- `~/.claude.json` — drop the `agent-bus` and `mini` MCP entries (keep `swarm-kanban`)
- `~/.codex/config.toml` — drop the agent-bus MCP entry
- `~/.claude/settings.json` — drop the SessionStart agent-bus inbox hook
- `~/.hermes/cron/jobs.json` — remove the `agent-bus-inbound-poll` cron
- `redis.service` (user unit) — stop + disable ("Redis for Octi Pulpo")
- `~/.chitin/agent-bus/bus.db*` — delete the bus database

## Goal

The kanban board (`swarm-kanban` MCP + `hermes kanban`) is the swarm's
**sole** agent-coordination channel. The agent-bus and Octi subsystems
are fully removed — no running process, cron, MCP wiring, hook, or
repo code references either.

## Background

The agent-bus (spec 001) is a threaded message bus plus a bidirectional
Discord bridge. In practice it is unreliable: Discord delivery 404s, the
inbox is noisy, and `hermes cron list` crashes on a bus job's malformed
`deliver`. Agents already coordinate on the board — the spec-068 handoff
proved it end to end. Octi (specs 038, 040–049, 054) is an experimental
agent layered entirely on the bus; it has no active crons and is dormant.
Operator: remove both, one clean sweep.

## Acceptance criteria

AC1. **Bus code gone.** `services/agent-bus/` no longer exists; nothing
in the repo imports it.

AC2. **Octi code gone.** `services/mini-mcp/` and `swarm/mini/` no longer
exist; the `octi-spec-corpus` worktree is removed.

AC3. **MCP unwired.** `~/.claude.json` and `~/.codex/config.toml` no
longer list `agent-bus` or `mini`. `swarm-kanban` is untouched.

AC4. **No bus cron / hook.** The `agent-bus-inbound-poll` cron is gone;
the SessionStart bus-inbox hook is removed from `~/.claude/settings.json`.

AC5. **swarm-kanban still works.** `swarm-kanban-mcp` imports and serves
the board with `post_swarm_message` removed — zero import of
`services/agent-bus`.

AC6. **Octi services stopped.** The Octi `redis.service` is stopped and
disabled; no `mini-mcp` process runs.

AC7. **Specs marked.** `INDEX.md` records spec 001 and the octi specs as
superseded by 069.

## Invariants

- The kanban board and `swarm-kanban` MCP are touched **only** to remove
  the one bus call — board coordination survives intact.
- After 069, a repo grep for `agent-bus` / `agent_bus` / `mini-mcp`
  returns only this spec and historical (superseded) spec files.

## Out of scope

- The hermes-native cron `deliver: discord:#x` path — that is
  hermes-agent's own delivery, independent of the agent-bus; it stays.
- The kanban board itself — it is the channel we keep.
- Rewriting historical bus/octi spec bodies — they are marked superseded
  in `INDEX.md`, not deleted.

## Open questions

- None — operator clarified: full removal, Octi included.
