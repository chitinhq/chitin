# `apps/cli` — `chitin` CLI

Operator-facing CLI for chitin: wire surfaces, inspect events,
replay runs, install the kernel, run health checks, audit the debt
ledger, run review passes.

```bash
pnpm install
pnpm exec tsx apps/cli/src/main.ts <command>
# Or via the bin once installed:
chitin <command>
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
| `install` | Build + symlink the chitin-kernel binary into `~/.local/bin`. |
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

`@chitin/cli` is the operator surface. It's NOT used by the swarm
worker (that's `apps/runner`). Both compose against the
same kernel + libs.

## Test suite

```bash
pnpm exec vitest run apps/cli/tests
```

## Related

- `apps/runner/README.md` — the autonomous swarm runtime
- `go/execution-kernel/cmd/chitin-kernel/` — the Go kernel binary
  the CLI wraps
- `libs/contracts/README.md` — the schemas the CLI consumes
