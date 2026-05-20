# 069 — Implementation plan

## Approach

Ordered teardown — stop running things, unwire, then delete code. System
changes are applied directly (not version-controlled); repo changes land on
branch `069-decommission-agent-bus-mini` → PR → merge.

The system-side teardown (MCP unwiring, the cron, redis) is already done.
What remains is the repo-side deletion plus the SessionStart hook and the
bus DB.

## Order

1. ✓ Stop + disable the Octi `redis.service`.
2. ✓ Unwire `agent-bus` + `mini` MCP from `~/.claude.json` and `~/.codex/config.toml`.
3. ✓ Remove the `agent-bus-inbound-poll` hermes cron.
4. Remove the SessionStart agent-bus inbox hook from `~/.claude/settings.json`.
5. Excise `post_swarm_message` from `services/swarm-kanban-mcp/server.py` —
   the function, the `BUS_DB`/`SWARM_THREAD_ID` constants, the tool-handler
   dict entry, the tool schema entry, and the doc-comment mentions — then
   confirm the module still parses and lists its board tools.
6. `git rm -r services/agent-bus services/mini-mcp swarm/mini`.
7. Mark spec 001 + mini specs 050–053 superseded in `INDEX.md`.
8. Delete `~/.chitin/agent-bus/bus.db*`.
9. Verify: repo grep clean; `swarm-kanban-mcp` parses; no bus/mini process.

## Risk

`swarm-kanban-mcp` is the board server — the channel we keep. The only
change is removing the bus hook (AC5 gates it: the module must still parse
and expose its board tools). If the excision is entangled, stub
`post_swarm_message` to a no-op rather than risk the board server.

## Validation

- `python3 -c "import ast; ast.parse(open('services/swarm-kanban-mcp/server.py').read())"` parses.
- `grep -rIl 'agent-bus\|agent_bus\|mini-mcp' --include=*.py .` → empty (excl. specs).
- No `services/agent-bus`, `services/mini-mcp`, `swarm/mini` directories.
