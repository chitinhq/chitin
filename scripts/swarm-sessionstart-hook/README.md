# swarm-sessionstart-hook

Per sw-007 Lane C (red lane): SessionStart hook that surfaces ready/in_progress/blocked tickets assigned to the current agent at the start of every Claude Code session. The wake-up mechanism for Claude Code in the new 3-agent swarm.

## What it does

When a Claude Code session starts, the hook scans every kanban board under `~/.hermes/kanban/boards/` and emits a markdown summary of tickets assigned to the agent (default: `red`). Silent when nothing matches (per agent-bus inbox convention).

## Files

- `swarm-tickets-for.sh` — the actual hook script. Reads kanban DBs directly via sqlite3.
- `install.sh` — idempotent installer per Constitution §6. Symlinks the hook into `~/.claude/hooks/` and registers the SessionStart entry in `~/.claude/settings.json`.
- `tests/test_swarm_tickets_for.sh` — 14 assertions covering silent-empty, single-ticket, status-grouping, board-filter, and silent-no-match cases.

## Install

```bash
bash scripts/swarm-sessionstart-hook/install.sh
```

Idempotent — safe to re-run after `git pull`.

## Test

```bash
bash scripts/swarm-sessionstart-hook/tests/test_swarm_tickets_for.sh
```

Exits non-zero on first failure with a `FAIL` line. All-pass output ends with `all assertions passed.`

## Manual smoke

```bash
bash scripts/swarm-sessionstart-hook/swarm-tickets-for.sh red
bash scripts/swarm-sessionstart-hook/swarm-tickets-for.sh red swarm    # only one board
bash scripts/swarm-sessionstart-hook/swarm-tickets-for.sh clawta       # different agent
```

## Why this matters

Per the failed sw-006 Haiku Test, the new 3-agent swarm has no live wake-up on the swarm kanban board. Ares's controller skeleton needs `--loop` mode + cron; Clawta needs an OpenClaw subscription. This hook is red's lane: every time a Claude Code session starts (which is the only ambient wake-up red has), it inspects the swarm board first.

Combined with the other two lanes (sw-007 A/B/C), no ticket assigned to one of us should sit in `ready` longer than the slowest wake-up cycle.
