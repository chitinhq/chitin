# `apps/cli` — `chitin` CLI

Operator-facing CLI for chitin: acquire the kernel, wire governed
surfaces, inspect events, replay runs, run health checks, audit the
debt ledger, run review passes.

```bash
pnpm install
npx @chitin/cli guard
chitin status
chitin replay <session-id>
```

## Subcommands

| Command | What it does |
|---------|--------------|
| `init claude-code [--workspace <dir>]` | Wire the PreToolUse hook for Claude Code in the given workspace. One-shot install. |
| `events list [--surface <s>] [--run <id>] [--limit N]` | Print rows from the captured event chain. |
| `events tail [--surface <s>] [--run <id>]` | Stream events as they're appended (like `tail -f`). |
| `events tree [--run <id>]` | Render the event chain as a parent-child tree. |
| `replay <run-id>` | Re-run a captured run from the chain. |
| `run …` | Submit a one-off agent turn (programmer-shape). See `--help`. |
| `guard` | Resolve a platform-matched kernel binary and install governed adapters for Claude Code, Codex, Gemini, and a Copilot wrapper. |
| `status` | Report kernel availability plus per-surface hook/wrapper status. |
| `install` | Low-level surface installer wrapper around kernel `install` for legacy flows. |
| `health` | Run the kernel's health check (chain integrity, marker counts, etc). |
| `ledger` | Audit `docs/debt-ledger.md` — counts open/claimed/shipped + filters. |
| `review <pr-number>` | Manually run the review-graph workflow against a PR (operator-facing equivalent of the auto-enqueued path in `dispatcher.ts`). |

Each subcommand is one file under `src/commands/`. Add a new
subcommand by:

1. Writing `src/commands/<name>.ts` with a `register<Name>` (or
   `<name>Command`) export.
2. Wiring it into `src/main.ts`.
3. Adding a vitest case under `tests/`.

## Layer

`@chitin/cli` is the operator distribution surface. `guard` mirrors the
old `@red-codes/agentguard` one-command pattern: npm installs the JS
wrapper, the CLI resolves a per-platform kernel binary, then the target
surface gets wired without a manual binary step.

## Test suite

```bash
pnpm exec vitest run apps/cli/tests
```

## Related

- `go/execution-kernel/cmd/chitin-kernel/` — the Go kernel binary
  the CLI wraps
- `libs/contracts/README.md` — the schemas the CLI consumes
