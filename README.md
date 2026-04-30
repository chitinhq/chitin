# Chitin

**Execution kernel for AI coding agents.** Every tool call across Claude Code, Copilot CLI, and openclaw is gated by a single policy and recorded in a hash-linked event chain that also emits OTEL spans into your existing observability stack. Nx monorepo (Go + TypeScript). MIT licensed.

> Principle: real execution before policy. Policy before automation. Automation gated by the same kernel.

## Phase 1 — Claude Code capture→replay on a local workstation

```bash
pnpm install
pnpm exec nx run execution-kernel:build
pnpm exec nx run cli:build
./dist/apps/cli/main.js init claude-code
# Run a Claude Code session...
./dist/apps/cli/main.js events list
./dist/apps/cli/main.js replay <run_id>
```

## Architecture

- `apps/cli` — operator CLI (`chitin init | events list | events tail | replay`)
- `libs/contracts` — canonical event schema (zod); Go types generated from this
- `libs/telemetry` — JSONL tailer, SQLite indexer, replay streamer
- `libs/adapters/claude-code` — monitor-only PreToolUse hook receiver (thin TS, exec's the Go kernel)
- `go/execution-kernel` — canon, normalize, emit, hook. Only layer allowed side effects.

## Where chitin writes data

Chitin emits append-only JSONL and a SQLite materialized view to a `.chitin/` directory. The kernel resolves the path in this order: `--chitin-dir` flag → repo-local `.chitin/` (walking up from cwd) → fallback `$HOME/.chitin/`. **The Go kernel is the only writer.** TS adapters are read-only against the filesystem (see [architecture.md](./docs/architecture.md#hard-rule)).

```
$HOME/.chitin/                       # global state — used when no repo-local .chitin/ exists
├── flow_events.jsonl                # governance flow events (gov decisions, swarm ticks)
├── gov-decisions-YYYY-MM-DD.jsonl   # daily gov decision logs (one file per day)
├── gov.db                           # SQLite gov state
├── hook-capture/                    # raw Pre/PostToolUse JSON captures (one file per hook fire)
├── kernel-errors.log                # kernel-side errors (read by `chitin health`)
└── current-envelope                 # active cost envelope

<repo>/.chitin/                      # repo-local capture (created by `chitin-kernel init`)
├── events-<run_id>.jsonl            # canonical event stream — one file per run
└── session_state.json
```

For diagnostics, run `chitin health` — it reports on the resolved dir and exits non-zero on `[FAIL]` rows. The [health runbook](./docs/observations/runbooks/health.md) explains every row and what to do when it's red.

## Toolchain

- **Nx** — orchestrator (project graph, affected, module boundaries)
- **Vite+** (`vp`) — TypeScript test/lint/format/build (Vitest, Oxlint, Oxfmt, Rolldown, tsgo)
- **Go 1.22+** — execution kernel, run via `nx:run-commands`

## Docs

- [`docs/thesis.md`](./docs/thesis.md)
- [`docs/operating-model.md`](./docs/operating-model.md)
- [`docs/architecture.md`](./docs/architecture.md)
- [`docs/event-model.md`](./docs/event-model.md)
- [`docs/toolchain.md`](./docs/toolchain.md)
- [`docs/roadmap.md`](./docs/roadmap.md)
- [`docs/archive-map.md`](./docs/archive-map.md)

## License

[MIT](./LICENSE)
