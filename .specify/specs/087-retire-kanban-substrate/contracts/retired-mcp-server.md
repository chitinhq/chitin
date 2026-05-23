# Retired Contract: swarm-kanban MCP server

**Surface**: `services/swarm-kanban-mcp/server.py`
**Status**: RETIRED by spec 087
**FR**: FR-002

## What this contract was

A Model Context Protocol (MCP) stdio server that exposed kanban-board operations as MCP
tools to agents (clawta, ares, hermes) running with MCP access. Agents listed the server
in their MCP configuration and called tools by name to query or mutate the kanban DB.

## Tools exposed (now retired)

| Tool name | Operation |
|---|---|
| `mcp__swarm-kanban__list_boards` | enumerate board slugs |
| `mcp__swarm-kanban__list_tickets` | list tickets for a board, optionally filtered by lane / assignee |
| `mcp__swarm-kanban__get_ticket` | fetch a single ticket by id |
| `mcp__swarm-kanban__claim_ticket` | atomic claim with assignee + state transition |
| `mcp__swarm-kanban__create_ticket` | create a ticket in a lane |
| `mcp__swarm-kanban__update_status` | move a ticket between lanes |

(Verified against the tool list shown in the speckit-tasks system reminder
`mcp__swarm-kanban__*` entries; the deferred-tools manifest in this session listed
exactly these 6.)

## What replaces it

**Nothing inside chitin replaces these tools** — the dispatch substrate is the Temporal
orchestrator (spec 070), which agents do not query through MCP. The orchestrator's UI
surfaces (sessions, orchestrator-diagram) cover the operator-visibility use case the
list_*/get_* tools used to serve.

Agents that previously called claim_ticket / update_status are dispatched by the
orchestrator now (workflow signals, not agent-initiated claims). The `mcp__server__tool`
action-name recognition in the kernel's driver normalizers (`go/execution-kernel/
internal/driver/*/normalize.go`) remains — it is destination-agnostic and applies to any
MCP server an operator wires up in the future. FR-009 / Assumption.

## What external callers see after the retirement

- An agent whose MCP config still lists `swarm-kanban` will fail to start the server
  process (binary not found / Python module missing). The MCP framework reports the
  startup failure; the agent continues without that tool surface.
- Any code that constructs MCP action names of the shape `mcp__swarm-kanban__*` (e.g.,
  for governance lookups) finds no destination — the kernel still RECOGNIZES the shape
  via the normalizers, but no in-repo policy targets this server.

External MCP callers are explicitly OUT OF SCOPE per the spec Assumptions; cleaning up
agent-side configs is operator action, not a chitin-repo deliverable.

## Verification at merge

```
test -e services/swarm-kanban-mcp/ && echo FAIL || echo PASS
grep -rln 'swarm-kanban-mcp\|mcp__swarm.?kanban' apps/ go/ libs/ services/ swarm/ | wc -l   # expect 0
```
