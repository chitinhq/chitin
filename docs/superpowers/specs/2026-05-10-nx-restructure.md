# Nx-Native Monorepo Restructuring Spec

Date: 2026-05-10

Status: Proposed

Task: `t_8bda2f95` / P100 kickoff

## Inputs

This spec is grounded in `AGENTS.md`, `.github/copilot-instructions.md`, current package manifests, current `nx.json`, and the post-cull decision docs named in `AGENTS.md`.

The requested inventory artifact, `docs/architecture/2026-05-10-nx-inventory.md`, is not present in this checkout at spec time. Before executing migration changes, recreate or restore that inventory and compare it against this spec. The validation commands below include a path check for that artifact.

## Goal

Make the repository Nx-native without broadening Chitin's product scope. The restructuring is allowed to change project metadata, tags, target names, and directory placement. It must not add orchestration, approvals, LLM consultation, MCP hosting, model routing, SaaS surfaces, or feature behavior.

## Allowed Buckets as Nx Projects

| Chitin bucket | Nx project type | Required tags | Current examples | Rule |
| --- | --- | --- | --- | --- |
| Kernel | `application` | `type:app`, `scope:kernel`, `layer:kernel`, `lang:go`, `runtime:local` | `execution-kernel` | Canonical write path and gate runtime only. |
| Analytics libs | `library` | `type:lib`, `scope:analytics`, layer tag by role, language tag | `@chitin/contracts`, `@chitin/telemetry`, `chitin-analysis` | Read-side schemas, replay, indexing, analysis, and contracts. |
| Plugins | `library` for reusable adapters/API packages, `application` only for runnable downstream plugin packages | `type:plugin` or `type:adapter`, `scope:plugin`, layer tag by role | `@chitin/adapter-claude-code`, `@chitin/router-plugin-api`, `@chitinhq/openclaw-plugin-governance` | Operator-installed driver adapters and downstream substrate plugins only. |
| Apps built off kernel | `application` | `type:app`, `scope:app`, specific layer tag | `@chitin/cli` | Must non-trivially consume kernel state in a way no substrate already does. |

Projects outside these buckets must be deleted, moved to `scratch/`, or kept outside Nx. They must not be tagged into the production project graph.

## Canonical Top-Level Layout

The canonical layout is:

```text
apps/
  cli/
  openclaw-plugin-governance/
go/
  execution-kernel/
libs/
  adapters/
    claude-code/
    ollama-local/
    openclaw/
  contracts/
  router-plugin-api/
    typescript/
    python/
  telemetry/
python/
  analysis/
tools/
  generators/
  lint/
docs/
  architecture/
  decisions/
  superpowers/
examples/
  router-plugins/
scratch/
scripts/
```

Directory rules:

- `go/execution-kernel` remains the kernel root. Do not split kernel internals into separate Nx projects unless Go module boundaries are also split.
- `python/analysis` remains under `python/` because it is an analytics read-side package, not a TS workspace package.
- `libs/router-plugin-api` may contain language-specific packages. `libs/router-plugin-api/typescript` is an Nx package; `libs/router-plugin-api/python` is either added as a Python Nx project later or left as source-only until Python targets are wired.
- `tools/*` are Nx tooling projects only when they enforce or generate repository structure. They are not Chitin runtime features.
- `examples/*` are non-production examples and should use `type:example` if added to Nx; otherwise keep them outside the project graph.
- `scratch/*` is excluded from production Nx boundaries and validation except basic repository hygiene.

## Project Naming

Nx project names must be stable and command-friendly.

| Area | Directory | Nx project name |
| --- | --- | --- |
| Go kernel | `go/execution-kernel` | `execution-kernel` |
| CLI app | `apps/cli` | `@chitin/cli` |
| OpenClaw governance plugin app | `apps/openclaw-plugin-governance` | `@chitinhq/openclaw-plugin-governance` |
| Contracts | `libs/contracts` | `@chitin/contracts` |
| Telemetry | `libs/telemetry` | `@chitin/telemetry` |
| Claude Code adapter | `libs/adapters/claude-code` | `@chitin/adapter-claude-code` |
| OpenClaw adapter | `libs/adapters/openclaw` | `@chitin/adapter-openclaw` |
| Ollama local adapter | `libs/adapters/ollama-local` | `@chitin/adapter-ollama-local` |
| Router plugin API TS | `libs/router-plugin-api/typescript` | `@chitin/router-plugin-api` |
| Python analysis | `python/analysis` | `analysis` or `chitin-analysis` |
| Generators | `tools/generators` | `@chitin/generators` |
| Lint tooling | `tools/lint` | `@chitin/tooling-lint` |

Rules:

- Published or potentially published TS packages use npm package names as Nx names.
- Non-TS projects use short kebab-case names unless a package manager already owns the name.
- Do not use bucket names alone as project names. `kernel`, `contracts`, `telemetry`, `adapter`, and `plugin` are roles, not project identities.

## Tag Taxonomy

Every production Nx project must have one tag from each applicable axis:

- `type:app`, `type:lib`, `type:adapter`, `type:plugin`, `type:tooling`, or `type:example`
- `scope:kernel`, `scope:analytics`, `scope:plugin`, `scope:app`, `scope:tooling`, or `scope:example`
- `layer:kernel`, `layer:contracts`, `layer:telemetry`, `layer:analysis`, `layer:adapter`, `layer:plugin-api`, `layer:plugin`, `layer:cli`, or `layer:tooling`
- `lang:go`, `lang:ts`, `lang:python`, or `lang:mixed`
- Optional runtime tags: `runtime:local`, `runtime:driver`, `runtime:openclaw`, `runtime:cli`, `runtime:tooling`

Legacy culled layer tags must be removed from enforceable constraints: `layer:scheduler`, `layer:slack`, and `layer:mcp`.

### Dep Constraints

The target `@nx/enforce-module-boundaries` constraints should converge to:

```js
[
  { sourceTag: 'layer:contracts', onlyDependOnLibsWithTags: [] },
  { sourceTag: 'layer:telemetry', onlyDependOnLibsWithTags: ['layer:contracts'] },
  { sourceTag: 'layer:analysis', onlyDependOnLibsWithTags: ['layer:contracts', 'layer:telemetry'] },
  { sourceTag: 'layer:plugin-api', onlyDependOnLibsWithTags: ['layer:contracts'] },
  { sourceTag: 'layer:adapter', onlyDependOnLibsWithTags: ['layer:contracts', 'layer:telemetry', 'layer:plugin-api'] },
  { sourceTag: 'layer:plugin', onlyDependOnLibsWithTags: ['layer:contracts', 'layer:telemetry', 'layer:plugin-api', 'layer:adapter'] },
  { sourceTag: 'layer:cli', onlyDependOnLibsWithTags: ['layer:contracts', 'layer:telemetry', 'layer:adapter'] },
  { sourceTag: 'layer:tooling', onlyDependOnLibsWithTags: ['layer:contracts'] },
  { sourceTag: 'layer:kernel', onlyDependOnLibsWithTags: [] },
]
```

Kernel code is Go and must remain dependency-isolated from TS libraries at module-boundary level. Cross-language coupling happens through generated files, command invocation, schemas, and documented contracts, not imports.

## Target Naming

Use one shared target vocabulary. Avoid target names that encode implementation details unless they are intentionally narrow maintenance operations.

### Common Targets

- `build`: create distributable artifact or compiled binary.
- `test`: run project tests.
- `lint`: run static linting or vetting for the project.
- `typecheck`: run TS/Python type checks where configured.
- `validate`: project-local aggregate that depends on `lint`, `typecheck`, and `test` when present.
- `run`: execute a local application entrypoint.
- `package`: create a publishable/archive artifact.
- `generate`: generate code or scaffolding for the project.

### Go Kernel

`execution-kernel` targets:

- `build`: `go build -o ../../dist/go/execution-kernel/chitin-kernel ./cmd/chitin-kernel`
- `test`: `go test ./...`
- `lint`: `go vet ./...`
- `run`: `go run ./cmd/chitin-kernel`
- Optional later: `validate` depends on `lint`, `test`, and `build`.

### TypeScript Libraries

TS lib targets:

- `test`: Vitest scoped to the package test directory.
- `typecheck`: inferred by `@nx/js/typescript` where `tsconfig.lib.json` exists.
- `build`: inferred by `@nx/js/typescript` only for buildable libraries.
- Custom generation uses explicit names, for example `generate-go-types`.

### Python Analytics

Python project targets:

- `test`: `python -m pytest`
- `lint`: Python linter once selected by the repo.
- `typecheck`: Python type checker once selected by the repo.
- `validate`: aggregate target after all three exist.

Do not invent a Python app surface. `python/analysis` is an analytics library/read-side package.

### Adapters

Adapter targets:

- `test`: adapter behavior tests.
- `typecheck`: TS typecheck when configured.
- `build`: only when an adapter is packaged or published.
- No `serve`, `daemon`, scheduler, approval, or orchestration targets.

### Plugins

Plugin targets:

- `test`: plugin tests against substrate-facing behavior.
- `package`: create installable plugin package if publishing is configured.
- `validate`: aggregate.

Plugins may call `chitin-kernel` commands, but they must not embed kernel policy decisions in parallel implementations.

### Apps

App targets:

- `run`: local command invocation.
- `test`: app tests.
- `build`: only if the app has a distributable artifact.
- `validate`: aggregate.

`@chitin/cli` keeps `run` and `test`; add `typecheck` via Nx inference where possible. It must not grow hermes-style orchestration features.

### Tooling

Tooling targets:

- `test`: tests for repository tooling.
- `lint:*`: narrow repository policy checks, such as `lint:layer-tag-coverage`.
- `generate`: Nx generators.

Tooling projects may depend on `@chitin/contracts` for schema-aware checks. They must not become runtime libraries.

## Required Project Outcomes

### `go/execution-kernel`

Keep in place as `execution-kernel`, `projectType: application`, with `layer:kernel`. It remains the only canonical gate/write-path implementation. Remove duplicate Nx declarations if necessary so `nx show project execution-kernel --json` has one resolved source of truth.

### `python/analysis`

Keep in place and add an explicit Nx project only when Python targets are ready. It should be tagged `type:lib`, `scope:analytics`, `layer:analysis`, `lang:python`. It reads chain-derived data; it does not write governance decisions.

### `libs/router-plugin-api`

Keep as a plugin API bucket. `libs/router-plugin-api/typescript` should be tagged `type:lib`, `scope:plugin`, `layer:plugin-api`, `lang:ts`. `libs/router-plugin-api/python` should either become a sibling Python project with the same logical layer or remain source-only until Python project support lands.

### `apps/openclaw-plugin-governance`

Keep, but classify as `type:plugin`, `scope:plugin`, `layer:plugin`, `lang:ts`, `runtime:openclaw`. It is a downstream-substrate plugin, not a generic Chitin app. If it becomes publishable, add `package`; otherwise keep `test` and `validate`.

### `tools/*`

Keep `tools/generators` and `tools/lint` as `type:tooling`, `scope:tooling`, `layer:tooling`, `lang:ts`. Tooling can enforce this spec, generate project metadata, and validate tags. It must not be used to ship runtime behavior.

### Ghost Directories

These directories are culled scope and must remain absent from the production graph:

- `apps/mcp-server`: do not restore. Chitin does not host MCP servers; substrates expose kernel subcommands.
- `apps/runner`: do not restore. Chitin is not an orchestrator or agent runner.
- `apps/slack-app`: do not restore. Slack workflow surfaces belong outside the kernel repo unless a future app clears the allowed-app bar.
- `libs/governance`: do not restore. The Go kernel is canonical; no parallel TS adjudicator.
- `libs/scheduler`: do not restore. Scheduling belongs to hermes.

If any of these names reappear, validation must fail unless the directory is under `docs/`, `scratch/`, or a migration archive explicitly excluded from Nx.

## Migration Order

1. Restore or regenerate `docs/architecture/2026-05-10-nx-inventory.md` and record current Nx project metadata, package manager workspaces, and ghost directory state.
2. Normalize tags in existing package manifests and `project.json` files without moving code.
3. Update `eslint.config.mjs` depConstraints to remove culled layers and add `layer:analysis`, `layer:plugin-api`, and `layer:plugin`.
4. Add missing Nx project metadata for packages already in `pnpm-workspace.yaml`, starting with adapters and router plugin API.
5. Add Python Nx project metadata for `python/analysis` only after choosing exact Python executors or stable `nx:run-commands` targets.
6. Add `validate` targets and `targetDefaults` after individual targets are green.
7. Consider directory moves only after tag and target validation is green. No move is required for `go/execution-kernel` or `python/analysis`.
8. Add tooling checks for ghost directories and required tag coverage.

Each step should be a separate commit or a small group of commits with passing validation.

## Rollback Plan

Rollback must preserve runtime code and operator data:

- For metadata-only changes, revert the commit that changed package `nx` blocks, `project.json`, `nx.json`, or `eslint.config.mjs`.
- For directory moves, use `git mv` in the forward migration and revert the move commit if Nx resolution or imports break.
- For Python Nx onboarding, remove only the Nx metadata if Python task wiring fails; leave `python/analysis` source in place.
- For depConstraint changes, restore the previous `eslint.config.mjs` constraint block while keeping any independent package manifest fixes that were already validated.
- Never roll back by deleting `go/execution-kernel`, `.chitin` data, generated chain files, or user-local state.

## Exact Validation Commands

Run from the repository root.

Preflight:

```bash
test -f docs/architecture/2026-05-10-nx-inventory.md
pnpm install
pnpm exec nx show projects --json
pnpm exec nx graph --print
```

Scope guards:

```bash
test ! -e apps/mcp-server
test ! -e apps/runner
test ! -e apps/slack-app
test ! -e libs/governance
test ! -e libs/scheduler
pnpm exec nx run @chitin/tooling-lint:lint:layer-tag-coverage
```

Core build and tests:

```bash
pnpm exec nx run execution-kernel:build
pnpm exec nx run execution-kernel:lint
pnpm exec nx run execution-kernel:test
pnpm exec nx run @chitin/contracts:test
pnpm exec nx run @chitin/telemetry:test
pnpm exec nx run @chitin/cli:test
pnpm exec nx run @chitin/cli:typecheck
pnpm exec nx run @chitin/telemetry:typecheck
pnpm exec nx run @chitin/contracts:typecheck
pnpm exec vitest run
(cd go/execution-kernel && go test ./...)
```

Repository lint:

```bash
pnpm exec oxlint .
pnpm exec eslint .
```

Python analytics, once Nx targets exist:

```bash
pnpm exec nx run analysis:test
```

Full affected check after the first migration commit:

```bash
pnpm exec nx affected -t lint,test,typecheck,build
```

Validation is complete only when the commands that correspond to configured targets pass. If a target does not exist yet, either add it in the migration step that claims it or remove it from that step's acceptance criteria.
