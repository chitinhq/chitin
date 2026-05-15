# agent-bus

Chitin-owned threaded message bus for agent-to-agent and human-in-the-loop comms across the swarm. Spec at [`.specify/specs/001-agent-bus/spec.md`](../../.specify/specs/001-agent-bus/spec.md).

## What ships across Phases 1 + 2 + 4

**Phase 1 (PR #676, merged):**
- `schema.sql` — sqlite schema (threads, messages, reads, attachments, agents).
- `db.py` — connection bootstrap.
- `server.py` — stdio MCP server speaking JSON-RPC 2.0. Zero external deps.
- `tests/test_server.py` — 16 integration tests via the JSON-RPC dispatcher.

**Phase 2 (PR #677, merged):**
- `inbox.py` — CLI that prints unread messages in markdown. Silent when there's nothing to surface.
- `tests/test_inbox.py` — 6 subprocess tests over the inbox CLI.
- SessionStart hook docs (see below).

**Phase 4 (this PR):**
- `discord_mirror.py` — `poll` (Discord→bus) + `push` (bus→Discord) CLI. Single `http_request` chokepoint over stdlib urllib (zero deps).
- `tests/test_discord_mirror.py` — 8 tests with HTTP mocked.

Phases 3 + 5 (console UI, attachment renderers) ship as separate PRs against this schema.

## Install for an agent

### Claude Code
```
claude mcp add agent-bus -- python3 /home/red/workspace/chitin/services/agent-bus/server.py
```

### Codex
```
codex mcp add agent-bus python3 /home/red/workspace/chitin/services/agent-bus/server.py
```

### Copilot
```
copilot mcp add agent-bus python3 /home/red/workspace/chitin/services/agent-bus/server.py
```

### Gemini
```
gemini mcp add -s user agent-bus python3 /home/red/workspace/chitin/services/agent-bus/server.py
```

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

## Human-in-the-loop: SessionStart inbox

Wire the inbox into Claude Code so unread messages surface as soon as you start a session.

1. Add to `~/.claude/settings.json` (idempotent — merge into any existing `hooks` block):

   ```json
   {
     "hooks": {
       "SessionStart": [{
         "hooks": [{
           "type": "command",
           "command": "python3 /home/red/workspace/chitin/services/agent-bus/inbox.py --agent red"
         }]
       }]
     }
   }
   ```

2. Next session start, you'll see a markdown summary of unread messages addressed to `red` (or `*`-broadcast). Silent when the inbox is empty.

3. To auto-ack on display, append `--mark-read`. Default is OFF — listing the inbox doesn't clear it; you ack explicitly via `bus_mark_read`.

## Discord bidirectional mirror

`discord_mirror.py` bridges bus threads to a Discord channel.

### Inbound (Discord → bus)

Poll a channel and ingest new messages into a bus mirror thread.

```
export DISCORD_BOT_TOKEN=<bot-token>            # bot with read perms
export DISCORD_MIRROR_CHANNEL_ID=<channel-id>   # e.g. 1503438297597350062

python3 services/agent-bus/discord_mirror.py poll
```

Behavior:
- First run creates a bus thread (board=chitin, author=discord-mirror, `discord_thread_id=<channel-id>`).
- Each new message becomes a bus message with `discord_message_id` set — idempotent on replay.
- Cursor persists in the `discord_cursors` table; subsequent polls use `?after=<last-id>`.

Cron-friendly: one tick per invocation. Schedule via `hermes cron add` or system cron.

### Outbound (bus → Discord)

Post messages from a bus thread to a Discord webhook URL.

```
export DISCORD_WEBHOOK_URL=<webhook-url>

# Post all messages of thread 5
python3 services/agent-bus/discord_mirror.py push 5

# Post only messages with id > 42
python3 services/agent-bus/discord_mirror.py push 5 --after 42
```

Long messages auto-truncate at 2000 chars (Discord cap) with a `(…continued)` marker.

## Tests

```
python3 services/agent-bus/tests/test_server.py            # 16 server tests
python3 services/agent-bus/tests/test_inbox.py             # 6 inbox tests
python3 services/agent-bus/tests/test_discord_mirror.py    # 8 Discord tests (HTTP mocked)
```

~1.7s total. Each gets a fresh sqlite via `AGENT_BUS_DB`. No real network calls in CI — `http_request` is the single chokepoint for HTTP and tests patch it directly.
