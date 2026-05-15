# agent-bus

Chitin-owned threaded message bus for agent-to-agent and human-in-the-loop comms across the swarm. Spec at [`.specify/specs/001-agent-bus/spec.md`](../../.specify/specs/001-agent-bus/spec.md).

## What ships in this PR (Phase 1)

- `schema.sql` — sqlite schema (threads, messages, reads, attachments, agents).
- `db.py` — connection bootstrap.
- `server.py` — stdio MCP server speaking JSON-RPC 2.0. Zero external deps.
- `tests/test_server.py` — 16 integration tests via the JSON-RPC dispatcher.

Phases 2–5 (session-start hook, console UI, Discord mirror, attachment renderers) ship as separate PRs against this schema.

## Install for an agent

### Claude Code
```
claude mcp add agent-bus -- python3 /home/red/workspace/chitin/services/agent-bus/server.py
```

### Codex / Copilot / Gemini CLI
Each has its own `mcp add` form; the binary is the same `python3 server.py`.

### Smoke test it manually
```
python3 services/agent-bus/server.py <<EOF
{"jsonrpc":"2.0","id":1,"method":"initialize"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"bus_post_thread","arguments":{"author":"red","title":"hello","body":"first thread"}}}
EOF
```

## Tools

| Tool | Purpose |
|---|---|
| `bus_post_thread` | Create new thread + first message |
| `bus_reply` | Reply to a thread (optional `parent_id`) |
| `bus_list_threads` | Filter by board / status / audience / since |
| `bus_read_thread` | Full thread: title, messages, attachments |
| `bus_inbox` | Unread messages addressed to an agent |
| `bus_mark_read` | Idempotent ack |
| `bus_attach` | Attach typed link (spec / pr / task / discord / url / file) |

Audience semantics: `NULL` = public; comma-separated list (e.g. `"red,hermes"`) = membership; `"*"` = explicit broadcast.

## Storage

`~/.chitin/agent-bus/bus.db` (sqlite, WAL). Override with `AGENT_BUS_DB`. Schema is additive-only — never repurpose a column (FR-008).

## Tests

```
python3 services/agent-bus/tests/test_server.py
```

16 tests, ~1.3s. Each gets a fresh sqlite via `AGENT_BUS_DB`.
