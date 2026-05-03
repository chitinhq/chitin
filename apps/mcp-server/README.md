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

The server runs straight from TypeScript via `tsx`. There's no
build/dist target in this slice — invoking the source file directly
is the supported install path. A pre-bundled distribution lands
in a follow-up if/when the standalone-binary use case arrives.

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `CHITIN_KERNEL_BINARY` | `chitin-kernel` | Path to the chitin-kernel binary |
| `CHITIN_HOME` | `~/.chitin` | Chitin state dir; matches the Go kernel's override |

## Development

```bash
# Run directly with tsx
pnpm exec tsx apps/mcp-server/src/main.ts

# Run the (libs/mcp-chitin) tests
pnpm exec nx run @chitin/mcp-server:test
```
