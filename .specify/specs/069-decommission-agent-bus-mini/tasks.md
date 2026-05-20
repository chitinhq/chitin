# 069 — Tasks

> Canonical task list for spec 069. Ordered: stop → unwire → delete.

- [x] T001 Stop + disable the Octi `redis.service`; remove the `redis-octi`
  docker container. Satisfies AC6.
- [x] T002 Unwire `agent-bus` + `mini` MCP from `~/.claude.json`; unwire
  `agent-bus` from `~/.codex/config.toml`. Keep `swarm-kanban`. Satisfies AC3.
- [x] T003 Remove the `agent-bus-inbound-poll` hermes cron. Satisfies AC4 (cron).
- [x] T004 Remove the SessionStart agent-bus inbox hook from
  `~/.claude/settings.json`. Satisfies AC4 (hook).
- [x] T005 Excise `post_swarm_message` from
  `services/swarm-kanban-mcp/server.py` (function, `BUS_DB`/`SWARM_THREAD_ID`
  constants, tool-handler entry, tool schema entry, doc mentions) plus the
  MCP README/test expectations; module parses, 0 bus refs remain. Satisfies
  AC5.
- [x] T006 `git rm -r services/agent-bus services/mini-mcp swarm/mini`.
  Satisfies AC1, AC2.
- [x] T007 Marked spec 001 + mini specs 050–053 superseded in `INDEX.md`;
  specs 040–048 left untouched. Satisfies AC7.
- [x] T008 Moved the bus DB to `~/.chitin/agent-bus.decommissioned-20260520`.
- [x] T009 Verified: dirs gone; no py/go importer of the deleted code
  (4 incidental string mentions in test files, not imports); swarm-kanban-mcp
  parses; MCP unwired.
- [x] T010 Delete stale Mini/agent-bus launchers, installers, and tests:
  `swarm/bin/mini*`, `swarm/bin/install-mini*`,
  `swarm/bin/install-agent-bus-cron.sh`, and `swarm/tests/test_mini_*.py`.
  Satisfies AC8.
- [x] T011 Rebase onto spec 070 after `specs -> .specify/specs`; place this
  spec at `.specify/specs/069-decommission-agent-bus-mini/`. Satisfies
  spec-kit path correctness.
- [ ] T012 Commit on branch `069-decommission-agent-bus-mini`, open PR.
