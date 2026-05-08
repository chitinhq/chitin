# Cull `apps/mcp-server` + `libs/mcp-chitin`; expose tools via existing chitin-kernel subcommands

**Date:** 2026-05-08
**Status:** Accepted
**Audit lens:** Sun Tzu (substrate symmetry — don't host what the substrate already hosts)

## Context

`apps/mcp-server/` ran a Model Context Protocol (MCP) server over stdio
that exposed eight chitin governance tools (envelope list/grant/close,
gate status/reset, chain info/verify, decisions recent). `libs/mcp-chitin/`
held the per-tool definitions, all of which were thin `spawnSync`
wrappers around `chitin-kernel <subcommand>`.

The 2026-05-08 substrate-symmetry audit flagged this as **server-plumbing
duplication**: both substrates we run on (Hermes and OpenClaw) ship
production-grade MCP servers natively (`hermes mcp`, `openclaw mcp set`)
with full lifecycle, transport, schema validation, and tool-registry
machinery. Chitin's stdio MCP server was reproducing that wire layer for
the exact same client surface — Claude Code, Cursor, mobile Claude — that
already gets MCP transport from the substrates.

The eight chitin **tools** themselves remain asymmetric: they wrap chitin-
specific state (envelope, gate, chain, decision log) that no substrate
exposes. The tools are kept; the server hosting them is dropped.

## What's gone

- `apps/mcp-server/` — server entry point (`main.ts`), zod schemas,
  MCP transport wiring, README, `package.json`, `tsconfig.json`.
  Single tracked entry: `apps/mcp-server/src/main.ts` (~135 LOC).
- `libs/mcp-chitin/` — tool definitions, `spawnSync` kernel wrapper,
  `KernelError` type, vitest suite. 8 tool wrappers in `src/tools/`,
  `kernel.ts`, `index.ts`, `tests/tools.test.ts`. ~600 LOC including
  tests.
- Workspace plumbing: `tsconfig.json` references for both, pnpm-lockfile
  workspace entries (settled by `pnpm install`).

**Total: ~700 LOC removed** from the TS workspace; 0 lines deleted from
the Go kernel.

## What's kept

The eight tools' **behavior** is preserved at the chitin-kernel CLI
surface, where it lives natively for seven of the eight tools and was
added (single new subcommand) for the last one.

| Tool                      | Backed by chitin-kernel CLI surface           | Pre-existing? |
|---------------------------|------------------------------------------------|---------------|
| `envelope_list`           | `chitin-kernel envelope list`                  | yes           |
| `envelope_grant`          | `chitin-kernel envelope grant <id>`            | yes           |
| `envelope_close`          | `chitin-kernel envelope close <id>`            | yes           |
| `gate_status`             | `chitin-kernel gate status`                    | yes           |
| `gate_reset`              | `chitin-kernel gate reset`                     | yes           |
| `chain_info`              | `chitin-kernel chain-info`                     | yes           |
| `chain_verify`            | `chitin-kernel chain-verify`                   | yes           |
| `decisions_recent`        | `chitin-kernel decisions recent`               | **new**       |

Seven of eight were already callable as kernel subcommands — the deleted
TS wrappers were literal `spawnSync` calls into them. The eighth
(`decisions_recent`) was the only TS code with non-trivial logic: it
read `gov-decisions-*.jsonl` daily files directly. That logic is now
in `internal/gov/decision_read.go` (`ReadRecent`) with the boundary
cases pinned by tests (empty dir, nonexistent dir, multiple days, stop-
early when older file pre-cutoff, malformed-line skip, zero-window/zero-
limit rejection).

### Why no `chitin-kernel mcp <tool>` namespace

The original cull spec offered an `mcp` namespace as one option:

```
chitin-kernel mcp envelope-list
chitin-kernel mcp envelope-grant ...
```

Rejected as gold-plating. Aliases-only namespaces are friction without
function. Substrates' MCP servers can register chitin tools by invoking
the **existing** subcommand directly:

```
hermes mcp register chitin.envelope_list \
  --command chitin-kernel --args '["envelope","list"]'

openclaw mcp set chitin-tools \
  '{"command":"chitin-kernel","args":["gate","status"]}'
```

This keeps the kernel surface smaller (one new subcommand vs eight new
subcommand aliases), and the existing CLI surface — which operators
already use directly from the shell — stays the documented MCP-callable
contract too.

## How operators wire chitin into their substrate's MCP

OpenClaw (per `openclaw mcp set` schema):

```json
{
  "chitin-envelope-list": {
    "command": "chitin-kernel",
    "args": ["envelope", "list"]
  },
  "chitin-decisions-recent": {
    "command": "chitin-kernel",
    "args": ["decisions", "recent", "--window-hours", "24", "--limit", "100"]
  }
}
```

Hermes (per `hermes mcp` registration):

```yaml
mcp_servers:
  chitin-tools:
    command: chitin-kernel
    args: [gate, status]
```

The substrate's MCP server hosts the wire protocol, schema, and client
discovery; chitin owns only the data-side semantics (`gov.db`, daily
jsonl decision logs, chain index). This matches the post-#397/398/399
cull pattern: chitin keeps what's unique (universal cross-driver gate,
canonical actions, audit chain) and defers operational machinery to the
substrate that already ships it.

## Verification

- `go build ./...` clean
- `go vet ./...` clean
- `go test ./...` — 982 tests pass across 26 packages
- `pnpm install` settles the lockfile
- `pnpm exec tsc --build` clean (no stale references)
- `pnpm exec vitest run` — 326 tests pass (one pre-existing unrelated
  EPIPE in `libs/router-plugin-api/typescript/index.test.ts`)
- New CLI command smoke-tested:
  `chitin-kernel decisions recent --window-hours 24 --limit 10` → JSON array

## Counterfactual

If chitin shipped its own MCP server long-term, every substrate
upgrade — new MCP transport, new schema spec, new client discovery
mechanism — would need a parallel chitin upgrade. Hosting the wire
layer that the substrate already hosts is a maintenance debt with no
unique value: the same Claude Code session that talks to Hermes' MCP
server can talk to chitin's tools through Hermes' MCP server, with
chitin contributing only the data semantics (which is the part chitin
is uniquely qualified to provide).

## Followups

- Document the substrate-side wire snippets in chitin's operator guide
  (`docs/governance-setup.md` or a new MCP-integration section) when
  the operator needs to register the tools end-to-end.
- If a future agent runtime appears that does **not** ship a built-in
  MCP server, revisit hosting one — but only as a thin transport that
  shells out to `chitin-kernel`, not a re-implementation of the tools.
