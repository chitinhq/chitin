# Implementation Plan: Nx Workspace Convention Restructure

Status: in progress.

Date: 2026-05-09

Inputs:

- `AGENTS.md`
- `docs/architecture/nx-workspace-conventions.md`
- `.agents/skills/nx-conventions-audit/SKILL.md`
- `node .agents/skills/nx-conventions-audit/scripts/audit-nx-conventions.mjs`
- `pnpm exec nx graph --print`

## Objective

Bring Chitin's Nx project metadata and folder layout into alignment with
the source-derived Nx conventions while preserving the post-cull product
boundary: Go execution kernel, analytics/read-side libraries, driver
plugins, and thin apps only.

Success means the Nx graph describes the architecture accurately, stale
`layer:*` tags no longer drive module boundaries, and remaining cull/move
work is split into small verified slices.

## Current Graph Summary

Nx currently detects 11 projects:

- `execution-kernel`
- `@chitin/cli`
- `@chitin/contracts`
- `@chitin/telemetry`
- `@chitin/adapter-claude-code`
- `@chitin/adapter-ollama-local`
- `@chitin/adapter-openclaw`
- `@chitin/generators`
- `@chitin/tooling-lint`
- `@chitin/router-plugin-api`
- `@chitinhq/openclaw-plugin-governance`

Inferred dependencies:

- `@chitin/cli` -> `@chitin/contracts`, `@chitin/telemetry`
- `@chitin/telemetry` -> `@chitin/contracts`
- `@chitin/adapter-claude-code` -> `@chitin/contracts`
- `@chitin/adapter-ollama-local` -> `@chitin/contracts`
- `@chitin/tooling-lint` -> `@chitin/contracts`

## Issue List

### Cull

- `libs/router-plugin-api/**` and `examples/router-plugins/**` may be
  stale after the MCP/router-plugin cull. Verify against decision docs
  before deletion.
- `scratch/copilot-spike/**` is historical spike code and not part of the
  target Nx product shape.

### Move

- `apps/openclaw-plugin-governance` is a driver/substrate plugin, not a
  thin app. It should move under `libs/adapters` or a dedicated plugin
  library scope after package/install paths are checked.
- `python/analysis` should be modeled as `libs/analysis` with
  `type:data-access`, `scope:analysis`, `lang:py`.
- `tools/generators` and `tools/lint` should move under
  `libs/tooling/*` or be explicitly retained as tooling exceptions.

### Tag

- All current Nx projects are missing the new required dimensions:
  `type:*`, `scope:*`, and `lang:*`.
- Most current projects still use stale `layer:*` tags.

### Boundary

- `eslint.config.mjs` still enforces `layer:*` constraints and references
  culled tags: `layer:scheduler`, `layer:slack`, and `layer:mcp`.
- `pnpm-workspace.yaml` still includes
  `libs/router-plugin-api/typescript` and `tools/*`.
- Cross-language relationships are not yet represented with
  `implicitDependencies`.

### Docs

- Historical swarm/openclaw-plugin documents should be reviewed and either
  moved to superseded context or clearly marked as historical.

## Commands

- Audit: `node .agents/skills/nx-conventions-audit/scripts/audit-nx-conventions.mjs`
- Graph: `pnpm exec nx graph --print`
- Project list: `pnpm exec nx show projects --json`
- ESLint boundary check: `pnpm exec eslint .`
- TS tests: `pnpm exec vitest run`
- Go tests: `(cd go/execution-kernel && go test ./...)`

## Project Structure Target

```text
apps/
  cli/

libs/
  contracts/
  telemetry/
  analysis/
  adapters/
    claude-code/
    openclaw/
    ollama-local/
  tooling/
    generators/
    lint/

go/
  execution-kernel/
```

## Boundaries

- Always: preserve the four allowed buckets in `AGENTS.md`.
- Always: use `type:*`, `scope:*`, and `lang:*` tags for real Nx
  projects.
- Always: verify `nx graph --print` after metadata or path changes.
- Ask first: deleting packages that may still be operator-installed.
- Ask first: adding new Nx plugins or changing package manager behavior.
- Never: move Go `internal/` packages into `libs/` only for visual
  symmetry.
- Never: reintroduce orchestration, approvals, MCP hosting, or in-kernel
  LLM behavior while restructuring.

## Implementation Phases

### Phase 1: Metadata and Boundary Rules

Acceptance criteria:

- All retained Nx projects carry `type:*`, `scope:*`, and `lang:*` tags.
- Stale `layer:*` tags are removed from project metadata.
- `eslint.config.mjs` enforces the new tag vocabulary and no longer
  references culled layers.
- `nx graph --print` still succeeds.
- The audit script no longer reports tag findings for retained projects.

Verification:

- `pnpm exec nx show projects --json`
- `pnpm exec nx graph --print`
- `node .agents/skills/nx-conventions-audit/scripts/audit-nx-conventions.mjs`
- `pnpm exec eslint .`

### Phase 2: Cull Dead Router/Scratch Surfaces

Acceptance criteria:

- Router plugin API/examples are either deleted or explicitly justified as
  live plugin support.
- Scratch Copilot spike code is removed or moved to historical docs only.
- Workspace globs no longer include culled packages.

Verification:

- `pnpm exec nx show projects --json`
- `pnpm exec nx graph --print`
- `rg "router-plugin|copilot-spike" docs AGENTS.md README.md`

### Phase 3: Move Valid Libraries into Nx Shape

Acceptance criteria:

- OpenClaw governance plugin is no longer under `apps/` unless it becomes
  a thin runnable app.
- Python analysis is modeled as a library project under `libs/analysis`.
- Tooling projects are under `libs/tooling/*` or documented as retained
  exceptions.
- Import paths, package workspace globs, and Nx project roots are updated.

Verification:

- `pnpm exec nx graph --print`
- `pnpm exec nx run-many -t test typecheck --parallel=3`
- Python analysis tests if available.

### Phase 4: Cross-Language Graph Edges

Acceptance criteria:

- Non-inferred Go/Python/TS relationships have explicit
  `implicitDependencies` where useful.
- Contract parity between TS schemas and Go event types is visible in Nx
  metadata or targets.

Verification:

- `pnpm exec nx graph --print`
- `pnpm exec nx affected -t test --files=libs/contracts/src/index.ts`

## Initial Task

Start with Phase 1 because it changes only metadata and boundary rules,
does not move code, and makes the next audit output sharper.

