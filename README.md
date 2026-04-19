# Chitin

**Observability-first substrate for AI coding agents.** Captures, normalizes, and replays agent tool calls locally. Nx monorepo (Go + TypeScript). MIT licensed.

> Principle: Claude Code, OpenClaw, and local/cloud Ollama-backed agent execution all require observability before governance or automation.

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
