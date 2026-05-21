# 069 — Tasks

> Canonical task list for spec 069. Ordered: stop → unwire → delete.

- [x] T001 Stop + disable the Octi `redis.service` ("Redis for Octi
  Pulpo"). Satisfies AC6. — DONE: redis.service inactive+disabled;
  redis-octi docker container removed.

- [ ] T002 Unwire MCP: drop `agent-bus` + `mini` from `~/.claude.json`;
  drop `agent-bus` from `~/.codex/config.toml`. Keep `swarm-kanban`.
  Depends: none. Satisfies AC3.

- [x] T003 Remove the `agent-bus-inbound-poll` hermes cron
  (`hermes cron remove`). Satisfies AC4 (cron half). — DONE: job
  `agbus-inb-poll` removed.

- [ ] T004 Remove the SessionStart agent-bus inbox hook from
  `~/.claude/settings.json`. Satisfies AC4 (hook half).

- [ ] T005 Excise `post_swarm_message` (the agent-bus hook) from
  `services/swarm-kanban-mcp/server.py` — function, MCP tool
  registration, callers — and verify the server still imports + lists
  board tools. Depends: T002. Satisfies AC5.

- [ ] T006 `git rm -r services/agent-bus services/mini-mcp swarm/mini`.
  Depends: T005. Satisfies AC1, AC2 (code half).

- [ ] T007 Remove the `.claude/worktrees/octi-spec-corpus` git worktree.
  Satisfies AC2 (worktree half).

- [ ] T008 Docs: ROLE.md + the operating-model doc → board-only;
  `INDEX.md` supersede notes for spec 001 + the octi specs
  (038, 040–049, 054). Satisfies AC7.

- [ ] T009 Delete the bus DB `~/.chitin/agent-bus/bus.db*`.

- [ ] T010 Verify: repo grep for `agent-bus`/`agent_bus`/`mini-mcp`
  returns only spec files; `swarm-kanban-mcp` smoke test passes; no
  bus/octi process or cron remains. Depends: T001–T009.

- [ ] T011 Commit on branch `077-decommission-agent-bus-octi`, open PR,
  merge. Depends: T010.
