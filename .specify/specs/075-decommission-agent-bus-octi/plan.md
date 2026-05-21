# 075 — Implementation plan

## Approach

An ordered teardown. **Stop running things first, then unwire, then
delete code** — so nothing is mid-call when its code vanishes.

System-side changes (configs, crons, services, DB) are applied directly
— they are not version-controlled. Repo-side changes (code deletion, doc
updates) go on branch `075-decommission-agent-bus-octi` → PR → merge.

## Order

1. **Stop.** Stop + disable the Octi `redis.service`. The agent-bus and
   `mini` MCP servers are stdio — they die with their client; there is
   no long-lived service to stop, only MCP wiring to remove.
2. **Unwire MCP.** Remove `agent-bus` + `mini` from `~/.claude.json`;
   remove `agent-bus` from `~/.codex/config.toml`. Keep `swarm-kanban`.
   Takes effect next agent session — current sessions hold harmless
   stale handles.
3. **Cron.** `hermes cron remove` the `agent-bus-inbound-poll` job.
4. **Hook.** Drop the SessionStart agent-bus inbox hook from
   `~/.claude/settings.json`.
5. **swarm-kanban-mcp.** Excise `post_swarm_message` — the function, its
   MCP tool registration, and any callers — then confirm the server
   still imports and lists its board tools.
6. **Delete repo code.** `git rm -r services/agent-bus services/mini-mcp
   swarm/mini`.
7. **Worktree.** `git worktree remove` the `octi-spec-corpus` worktree.
8. **Docs.** ROLE.md + operating-model doc → board-only wording;
   `INDEX.md` supersede notes for spec 001 and the octi specs.
9. **DB.** Delete `~/.chitin/agent-bus/bus.db*`.
10. **Verify.** Repo grep clean; `swarm-kanban-mcp` smoke test; no bus /
    octi process or cron remains.

## Risk

`swarm-kanban-mcp` *is* the board — the channel we are keeping. The only
change to it is removing the bus hook. AC5 gates the work: the server
must still import and serve before the spec is done. If excising
`post_swarm_message` is entangled, stub it to a no-op rather than risk
the board server.

## Validation

- `python3 -c "import ast; ast.parse(open('services/swarm-kanban-mcp/server.py').read())"` parses.
- `grep -rIl 'agent-bus\|agent_bus\|mini-mcp' --include=*.py --include=*.go .` → empty.
- `systemctl --user is-enabled redis.service` → `disabled`.
- `hermes cron list` no longer lists `agent-bus-inbound-poll`.
