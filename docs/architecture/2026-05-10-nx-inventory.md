# Nx Workspace Inventory - 2026-05-10

Status: restored and revalidated for kanban task `t_7c01b5e4`.

This document records the current Nx and filesystem shape before the
monorepo restructuring spec. It intentionally does not choose a target
layout.

## Commands Run

```bash
pnpm exec nx show projects --json
pnpm exec nx graph --print
pnpm exec nx show project <name> --json
pnpm exec nx run @chitin/tooling-lint:lint:layer-tag-coverage
pnpm install
cat pnpm-workspace.yaml
git ls-files apps libs tools go python
test ! -d apps/mcp-server
test ! -d apps/runner
test ! -d apps/slack-app
test ! -d libs/governance
test ! -d libs/scheduler
```

## Resolved Nx Projects

`pnpm exec nx show projects --json` reports 11 projects:

```json
[
  "@chitin/router-plugin-api",
  "@chitinhq/openclaw-plugin-governance",
  "@chitin/adapter-ollama-local",
  "@chitin/adapter-claude-code",
  "@chitin/adapter-openclaw",
  "execution-kernel",
  "@chitin/generators",
  "@chitin/contracts",
  "@chitin/telemetry",
  "@chitin/tooling-lint",
  "@chitin/cli"
]
```

| Project | Root | Project Type | Source Root | Tags | Targets |
|---|---|---:|---|---|---|
| `@chitin/router-plugin-api` | `libs/router-plugin-api/typescript` | null | null | `npm:private` | none |
| `@chitinhq/openclaw-plugin-governance` | `apps/openclaw-plugin-governance` | null | null | `npm:public`, `layer:app` | `test`, `nx-release-publish` |
| `@chitin/adapter-ollama-local` | `libs/adapters/ollama-local` | `library` | `libs/adapters/ollama-local/src` | `npm:private`, `layer:adapter` | `typecheck` |
| `@chitin/adapter-claude-code` | `libs/adapters/claude-code` | null | null | `npm:private`, `layer:adapter` | `typecheck` |
| `@chitin/adapter-openclaw` | `libs/adapters/openclaw` | `library` | `libs/adapters/openclaw/src` | `npm:private`, `layer:adapter` | `typecheck` |
| `execution-kernel` | `go/execution-kernel` | `application` | `go/execution-kernel` | `npm:private`, `layer:kernel` | `build`, `test`, `lint`, `run` |
| `@chitin/generators` | `tools/generators` | null | null | `npm:private`, `layer:tooling` | none |
| `@chitin/contracts` | `libs/contracts` | null | null | `npm:private`, `layer:contracts` | `typecheck`, `test`, `generate-go-types` |
| `@chitin/telemetry` | `libs/telemetry` | null | null | `npm:private`, `layer:telemetry` | `typecheck`, `test` |
| `@chitin/tooling-lint` | `tools/lint` | null | null | `npm:private`, `layer:tooling` | `typecheck`, `test`, `lint:layer-tag-coverage` |
| `@chitin/cli` | `apps/cli` | null | null | `npm:private`, `layer:cli` | `typecheck`, `test`, `run` |

## Target Commands

The resolved project target commands are:

| Project | Target | Executor | Command / Notes |
|---|---|---|---|
| `@chitinhq/openclaw-plugin-governance` | `test` | `nx:run-commands` | `pnpm exec vitest run apps/openclaw-plugin-governance/test` |
| `@chitinhq/openclaw-plugin-governance` | `nx-release-publish` | `@nx/js:release-publish` | Nx release publish target |
| `@chitin/adapter-ollama-local` | `typecheck` | `nx:run-commands` | `tsc --build tsconfig.json --emitDeclarationOnly` in `libs/adapters/ollama-local` |
| `@chitin/adapter-claude-code` | `typecheck` | `nx:run-commands` | `tsc --build tsconfig.json --emitDeclarationOnly` in `libs/adapters/claude-code` |
| `@chitin/adapter-openclaw` | `typecheck` | `nx:run-commands` | `tsc --build tsconfig.json --emitDeclarationOnly` in `libs/adapters/openclaw` |
| `execution-kernel` | `build` | `nx:run-commands` | `go build -o ../../dist/go/execution-kernel/chitin-kernel ./cmd/chitin-kernel` in `go/execution-kernel` |
| `execution-kernel` | `test` | `nx:run-commands` | `go test ./...` in `go/execution-kernel` |
| `execution-kernel` | `lint` | `nx:run-commands` | `go vet ./...` in `go/execution-kernel` |
| `execution-kernel` | `run` | `nx:run-commands` | `go run ./cmd/chitin-kernel` in `go/execution-kernel` |
| `@chitin/contracts` | `typecheck` | `nx:run-commands` | `tsc --build tsconfig.json --emitDeclarationOnly` in `libs/contracts` |
| `@chitin/contracts` | `test` | `nx:run-commands` | `pnpm exec vitest run libs/contracts/tests` |
| `@chitin/contracts` | `generate-go-types` | `nx:run-commands` | `pnpm exec tsx libs/contracts/tools/generate-go-types.ts`; output `go/execution-kernel/internal/event/event.go` |
| `@chitin/telemetry` | `typecheck` | `nx:run-commands` | `tsc --build tsconfig.json --emitDeclarationOnly` in `libs/telemetry` |
| `@chitin/telemetry` | `test` | `nx:run-commands` | `pnpm exec vitest run libs/telemetry/tests` |
| `@chitin/tooling-lint` | `typecheck` | `nx:run-commands` | disabled echo target because project references set `noEmit: true` |
| `@chitin/tooling-lint` | `test` | `nx:run-commands` | `pnpm exec vitest run --config tools/lint/vitest.config.ts` |
| `@chitin/tooling-lint` | `lint:layer-tag-coverage` | `nx:run-commands` | `pnpm exec tsx tools/lint/layer-tag-coverage.ts` |
| `@chitin/cli` | `typecheck` | `nx:run-commands` | disabled echo target because project references set `noEmit: true` |
| `@chitin/cli` | `test` | `nx:run-commands` | `pnpm exec vitest run apps/cli/tests` |
| `@chitin/cli` | `run` | `nx:run-commands` | `pnpm exec tsx apps/cli/src/main.ts` |

`@chitin/router-plugin-api` and `@chitin/generators` currently resolve
with no targets.

## Project Graph Edges

`pnpm exec nx graph --print` reports these project-to-project edges:

| Source | Target | Type |
|---|---|---|
| `@chitin/adapter-ollama-local` | `@chitin/contracts` | static |
| `@chitin/adapter-claude-code` | `@chitin/contracts` | static |
| `@chitin/telemetry` | `@chitin/contracts` | static |
| `@chitin/tooling-lint` | `@chitin/contracts` | static |
| `@chitin/cli` | `@chitin/contracts` | static |
| `@chitin/cli` | `@chitin/telemetry` | static |
| `@chitin/cli` | `@chitin/telemetry` | dynamic |

The following resolved projects currently have no graph dependencies:

- `@chitin/router-plugin-api`
- `@chitinhq/openclaw-plugin-governance`
- `@chitin/adapter-openclaw`
- `execution-kernel`
- `@chitin/generators`
- `@chitin/contracts`

## Workspace Package Globs

`pnpm-workspace.yaml` currently declares:

```yaml
packages:
  - 'libs/*'
  - 'libs/adapters/*'
  - 'libs/router-plugin-api/typescript'
  - 'apps/*'
  - 'go/*'
  - 'tools/*'
```

Notably, `python/analysis` is not included in the pnpm workspace globs.

## Module Boundary Constraints

`eslint.config.mjs` contains the active `@nx/enforce-module-boundaries`
rule. Current `layer:*` depConstraints are:

| Source Tag | May Depend On |
|---|---|
| `layer:contracts` | none |
| `layer:telemetry` | `layer:contracts` |
| `layer:scheduler` | `layer:contracts`, `layer:telemetry` |
| `layer:slack` | `layer:contracts`, `layer:telemetry` |
| `layer:adapter` | `layer:contracts`, `layer:telemetry` |
| `layer:mcp` | `layer:contracts`, `layer:telemetry` |
| `layer:cli` | `layer:contracts`, `layer:telemetry`, `layer:scheduler` |
| `layer:app` | `layer:contracts`, `layer:telemetry` |
| `layer:tooling` | `layer:contracts` |
| `layer:kernel` | none |

`pnpm exec nx run @chitin/tooling-lint:lint:layer-tag-coverage`
completed successfully with 0 errors and 3 orphan warnings:

```text
layer-tag-coverage: warning - depConstraints with no matching package:
  layer:mcp (defined but no package carries this tag yet)
  layer:scheduler (defined but no package carries this tag yet)
  layer:slack (defined but no package carries this tag yet)
layer-tag-coverage: ok (0 errors, 3 orphan warnings)

NX   Successfully ran target lint:layer-tag-coverage for project @chitin/tooling-lint
```

Nx also printed:

```text
Your AI agent configuration is outdated. Run "nx configure-ai-agents" to update.
```

## Tracked Paths By Surface

Tracked source paths under the workspace roots reduce to:

```text
apps/cli
apps/openclaw-plugin-governance
go/execution-kernel
libs/adapters/claude-code
libs/adapters/ollama-local
libs/adapters/openclaw
libs/contracts
libs/router-plugin-api
libs/telemetry
python/analysis
tools/generators
tools/lint
```

## Removed Historical Ghost Directories

The following historical ghost directories do not exist in the current
worktree:

- `apps/mcp-server`
- `apps/runner`
- `apps/slack-app`
- `libs/governance`
- `libs/scheduler`

Scope guards with `test ! -d` passed for all five paths during
revalidation.

## Tracked Code Outside Resolved Nx Projects

Two tracked surfaces are outside, or only partially inside, resolved Nx
project coverage.

### `python/analysis`

`python/analysis` has 47 tracked files and is not represented in the Nx
project list. It is also not included in `pnpm-workspace.yaml`.

Tracked examples include:

```text
python/analysis/__init__.py
python/analysis/__main__.py
python/analysis/codex_mine.py
python/analysis/debt.py
python/analysis/decisions.py
python/analysis/detect.py
python/analysis/draft.py
python/analysis/floundering_calibration.py
python/analysis/llm_draft.py
python/analysis/loaders.py
python/analysis/models.py
python/analysis/predict.py
```

Ignored local Python artifacts also exist under `python/analysis`, such as
`.venv`, `.pytest_cache`, `__pycache__`, `*.egg-info`, and `out/`.

### `libs/router-plugin-api/python`

`libs/router-plugin-api/python` has 2 tracked files:

```text
libs/router-plugin-api/python/chitin_governance.py
libs/router-plugin-api/python/setup.py
```

The resolved Nx project for router plugin API is only
`libs/router-plugin-api/typescript`.

## Observed Metadata Gaps

These are factual gaps in the current resolved workspace metadata, not
target-architecture decisions:

- 7 of 11 resolved projects have `projectType: null`.
- 8 of 11 resolved projects have `sourceRoot: null`.
- `@chitin/router-plugin-api` has no `layer:*` tag.
- `@chitin/router-plugin-api` and `@chitin/generators` have no targets.
- `@chitin/tooling-lint:typecheck` and `@chitin/cli:typecheck` are disabled
  echo targets because project references set `noEmit: true`.
- Boundary constraints still include orphaned historical layers:
  `layer:mcp`, `layer:scheduler`, and `layer:slack`.
- `@chitin/contracts:generate-go-types` writes into
  `go/execution-kernel/internal/event/event.go`, which is outside the
  contracts project root.

## Verification Status

- `pnpm install`: passed; lockfile was up to date.
- `pnpm exec nx show projects --json`: passed.
- `pnpm exec nx graph --print`: passed.
- `pnpm exec nx show project <name> --json`: passed for all 11 projects.
- Scope guards for removed ghost directories: passed.
- `pnpm exec nx run @chitin/tooling-lint:lint:layer-tag-coverage`: passed
  with 3 orphan warnings.
