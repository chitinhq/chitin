# Implementation Plan: Agent-Bus

**Branch**: `feat/agent-bus-mvp` | **Date**: 2026-05-15 | **Spec**: [spec.md](./spec.md)

## Summary

Phase 1 only: ship the storage + MCP stdio server that all subsequent phases consume. No UI, no Discord, no per-agent installs in this PR. Schema is intentionally stable (additive-only) since 4+ consumers will depend on it.

## Technical Context

**Language/Version**: Python 3.11+ (matches `swarm/bin/clawta-poller`).

**Primary Dependencies**: stdlib only — `sqlite3`, `json`, `sys`. No pip deps. The MCP protocol over stdio is plain JSON-RPC 2.0; rolling our own keeps install friction at zero and avoids adding a vendored dep that other chitin tools don't share. Swap to the official `mcp` SDK in a later PR if it earns its weight.

**Storage**: SQLite at `~/.chitin/agent-bus/bus.db` (WAL mode). Schema in `services/agent-bus/schema.sql`. Bootstrapped on first connection.

**Testing**: stdlib `unittest`, run via `python3 -m unittest services.agent_bus.tests.test_server` (matches `swarm/tests/test_clawta_poller.py` pattern).

**Target Platform**: Linux (operator's box + CI). Other agents will install via `claude mcp add` etc.

**Project Type**: Service (single Python module + sqlite). Lives at `services/agent-bus/`.

**Performance Goals**: <100ms round-trip for any single MCP tool call locally; <500ms for a `bus_read_thread` on a 100-message thread.

**Constraints**: No external pip deps in v1; sqlite WAL must survive process kill mid-call; schema additive-only.

**Scale/Scope**: 10–100 agents, 1k threads/yr, 50k messages/yr. SQLite handles this with room to spare.

## Constitution Check

No `.specify/memory/constitution.md` exists yet (it's part of the larger spec-kit migration spec at `2026-05-15-adopt-speckit-replace-spec-flow-design.md`). For this spec, the live chitin invariants apply: governance signing is preserved (no `chitin.yaml` changes here); the regression gate must pass; tests live next to code; PR targets `main`.

## Project Structure

```
services/agent-bus/
├── README.md          # quick start + tool reference
├── schema.sql         # sqlite schema (Phase 1)
├── db.py              # connection + bootstrap helpers
├── server.py          # MCP stdio JSON-RPC server
└── tests/
    └── test_server.py # round-trip integration tests via MCP wire
```

## Phase 1 Deliverables (this PR)

1. **schema.sql** — `threads`, `messages`, `reads`, `agents`, `attachments`, `schema_version` tables with indices.
2. **db.py** — `connect()` + `init_schema()` helpers; honors `AGENT_BUS_DB` env override for tests.
3. **server.py** — stdio JSON-RPC 2.0 server implementing the 7 MCP tools from the spec (`bus_post_thread`, `bus_reply`, `bus_list_threads`, `bus_read_thread`, `bus_inbox`, `bus_mark_read`, `bus_attach`). Per-MCP-spec response shape: `tools/list` returns the tool catalog; `tools/call` invokes a tool with structured args + returns structured content.
4. **tests/test_server.py** — boots server in-process, exercises each tool through the JSON-RPC dispatcher, asserts persistence + filter semantics + concurrency.
5. **README.md** — install + usage (`claude mcp add agent-bus python3 services/agent-bus/server.py`).

## Phases 2–5 (separate PRs, separate kanban tickets)

- **Phase 2** — `t_TBD`: SessionStart hook in `~/.claude/settings.json` that injects `bus_inbox(red)` summary.
- **Phase 3** — `t_TBD`: `chitin-console` `/threads` route + compose form; `chitin-console-api` write endpoints.
- **Phase 4** — `t_TBD`: `services/agent-bus-discord-mirror/` ingester (poll Discord channel) + outbound (post via webhook).
- **Phase 5** — `t_TBD`: console UI for typed attachments + GitHub PR badge enrichment.

Each phase opens against `main` once Phase 1 is merged, since they all depend on the schema + tools being live.
