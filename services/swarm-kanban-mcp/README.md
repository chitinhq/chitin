# swarm-kanban-mcp

MCP stdio server exposing kanban board state to Claude Code sessions.

Per `docs/strategy/2026-05-18-swarm-redesign.md` §Week 1 (red's lane work). Lets red interact with any kanban board (chitin / readybench / personal-os / swarm) without subprocess-calling `hermes kanban` over and over. Owner routing contract from Clawta's lane proposal lives here.

## Tools

| Tool | Purpose |
|---|---|
| `list_boards()` | Enumerate all kanban boards |
| `list_tickets(board, status?)` | List tickets, optional status filter |
| `get_ticket(board, ticket_id)` | Full detail + comments |
| `claim_ticket(board, ticket_id, owner)` | Transition to in_progress, attach audit comment |
| `update_status(board, ticket_id, new_status, author, comment?)` | Transition state |
| `create_ticket(board, title, body, assignee, priority?, triage?)` | File new ticket |

## Install for Claude Code

```bash
claude mcp add swarm-kanban -- python3 /home/red/workspace/chitin/services/swarm-kanban-mcp/server.py
```

## Why

Old flow: red runs `hermes kanban --board swarm list` in a Bash tool, parses text. 5+ tool calls to claim + comment + transition a ticket.

New flow: red calls `mcp__swarm-kanban__claim_ticket(board="swarm", ticket_id="t_xxx", owner="red")`. One call. Structured response.

Matches Clawta's "owner routing contract" — board is the bus is the source of truth, agents read+write via typed interfaces.

## Tests

```bash
cd ~/workspace/chitin
python3 -m unittest services.swarm-kanban-mcp.tests.test_server -v
```

Board-tool tests should pass.
