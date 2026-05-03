# chitin MCP server

Exposes chitin governance state to any MCP client (Claude Code, Cursor, mobile Claude) via the
Model Context Protocol over stdio.

## Tools

| Tool | Description |
|---|---|
| `chitin_envelope_list` | List budget envelopes |
| `chitin_envelope_grant` | Grant additional budget to an envelope |
| `chitin_envelope_close` | Close an envelope |
| `chitin_gate_status` | Per-agent escalation level and active policy |
| `chitin_gate_reset` | Reset lockdown state for an agent |
| `chitin_chain_info` | Chain state (last seq + hash) for a session |
| `chitin_chain_verify` | Chain linkage integrity check (Phase-1.5 stub) |
| `chitin_decisions_recent` | Windowed governance decision log |

## Install

### Claude Code

```bash
claude mcp add chitin -- npx tsx /path/to/chitin/apps/mcp-server/src/main.ts
```

Or point at a built dist:

```bash
claude mcp add chitin /path/to/chitin/dist/apps/mcp-server/main.js
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `CHITIN_KERNEL_BINARY` | `chitin-kernel` | Path to the chitin-kernel binary |
| `CHITIN_BUDGET_DIR` | `~/.chitin` | Directory used by envelope and decisions tools |

## Development

```bash
# Run directly with tsx
pnpm exec tsx apps/mcp-server/src/main.ts

# Build
pnpm nx build mcp-server
```
