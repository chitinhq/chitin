# Chitin repository instructions

## Build, test, and lint commands

- Install dependencies with `pnpm install`. CI uses Node 22 + pnpm 10. If `better-sqlite3` did not build during install, run `pnpm rebuild better-sqlite3`.
- Build the Go kernel with `pnpm exec nx run execution-kernel:build`.
- Run the CLI from source with `pnpm exec nx run @chitin/cli:run -- --help` (or `pnpm exec tsx apps/cli/src/main.ts`).
- Run all TypeScript tests with `pnpm exec vitest run`.
- Run project-scoped TypeScript tests with `pnpm exec nx run @chitin/cli:test`, `pnpm exec nx run @chitin/contracts:test`, or `pnpm exec nx run @chitin/telemetry:test`.
- Run a single Vitest file with `pnpm exec vitest run apps/cli/tests/health.test.ts`.
- Run a single Vitest test with `pnpm exec vitest run apps/cli/tests/health.test.ts -t "returns 0 when everything is zero"`.
- Run all Go tests with `(cd go/execution-kernel && go test ./...)`.
- Run a single Go test with `(cd go/execution-kernel && go test ./internal/gov -run TestGate_DeniesRmRfAndLogs)`.
- Lint Go with `pnpm exec nx run execution-kernel:lint` (wraps `go vet ./...`).
- Lint TypeScript/JS with `pnpm exec oxlint .` and `pnpm exec eslint .`.
- Type-check individual TS projects with Nx, e.g. `pnpm exec nx run @chitin/contracts:typecheck` or `pnpm exec nx run @chitin/telemetry:typecheck`.

## High-level architecture

- This is an Nx monorepo split into tagged layers: `libs/contracts`, `libs/telemetry`, `libs/adapters/*`, `apps/cli`, and `go/execution-kernel`.
- `libs/contracts` owns the v2 event envelope/payload schemas plus shared `.chitin` directory resolution logic.
- `apps/cli` is mostly an orchestration surface built with Commander. It shells into `chitin-kernel` for install/uninstall, health, ingest, and event-emission workflows.
- `libs/adapters/claude-code` turns Claude hook payloads into v2 event stubs and invokes `chitin-kernel emit`. `chitin run <surface>` also wraps Claude/OpenClaw/Ollama launches and emits session lifecycle events around them.
- The Go kernel is the canonical write path. It handles emit, hook install/uninstall, ingest, health, and governance/gate commands, and appends immutable `.chitin/events-<run_id>.jsonl` files.
- `libs/telemetry` is the read/query side. `ensureIndexed()` materializes `.chitin/events.db` from canonical JSONL on demand, and CLI commands such as `events list`, `events tree`, and `replay` read from that derived SQLite view.

## Key conventions

- Treat `.chitin/events-<run_id>.jsonl` as the source of truth. `events.db` is a derived cache and may not exist until a DB-backed command calls `ensureIndexed()`.
- Keep the TS and Go `.chitin` resolvers behaviorally aligned: `libs/contracts/src/chitindir-resolve.ts` mirrors `go/execution-kernel/internal/chitindir/resolve.go`.
- When changing the event model, update all of these together: `libs/contracts/src/*.schema.ts`, `go/execution-kernel/internal/event/event.go`, adapter payload builders, and telemetry's `V2Event`/indexing logic. The `generate-go-types` script exists, but the current v2 Go event struct is still hand-maintained.
- Respect the Nx layer tags enforced in `eslint.config.mjs`: `contracts` has no internal deps, `telemetry` can depend only on `contracts`, `adapter` and `cli` can depend only on `contracts` + `telemetry`, and the kernel stays separate from the TS packages.
- TypeScript is strict `nodenext` with `isolatedModules` and `noUnusedLocals`; unused imports/locals and CommonJS assumptions tend to fail quickly.
- When current workspace targets disagree with older phase docs, trust `pnpm exec nx show project <project> --json` and CI over README-era commands. For example, the kernel has the active explicit build target; the CLI currently exposes `run`, `test`, and `typecheck` instead of a dedicated build target.
- Nx TypeScript typecheck targets are wired through `@nx/js:typescript-sync`, so interactive runs may prompt for `nx sync` before the task executes.
- Other assistant configs in this repo already assume Nx-aware tooling (`.claude/settings.json` enables the Nx plugin and `.codex/config.toml` registers `nx-mcp`). If the current client supports Nx MCP or Nx workspace introspection, use that instead of guessing targets.
- Claude global install verification depends on wrapper entries in `~/.claude/settings.json` shaped like Chitin's `_tag: "chitin"` hook wrappers with an empty `matcher` and nested `hooks` command entries.
