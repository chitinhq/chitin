# Nx workspace conventions

This note captures the Nx conventions chitin should follow when culling
old code and reshaping the repo. It is source-derived from the official
Nx v22 docs and adapted to chitin's current scope: execution governance
kernel, analytics/read-side libraries, driver plugins, and thin apps.

## Sources

- Nx project dependency rules:
  https://nx.dev/docs/concepts/decisions/project-dependency-rules
- Nx folder structure guidance:
  https://nx.dev/docs/concepts/decisions/folder-structure
- Nx project size guidance:
  https://nx.dev/docs/concepts/decisions/project-size
- Nx mental model:
  https://nx.dev/docs/concepts/mental-model
- Nx module boundary enforcement:
  https://nx.dev/docs/features/enforce-module-boundaries
- Nx project configuration:
  https://nx.dev/docs/reference/project-configuration
- Nx dependency management:
  https://nx.dev/docs/concepts/decisions/dependency-management
- Nx buildable/publishable libraries:
  https://nx.dev/docs/concepts/buildable-and-publishable-libraries

## What Nx means by convention

Nx does not require one global folder shape, but it does expect an
intentional one. Its docs recommend grouping projects by scope: usually
the application or larger product area the code belongs to. Shared code
lives under shared scopes, and nested grouping folders are normal.

Nx also treats project boundaries as architecture, not just packaging.
A project can exist purely for organization and constraint enforcement;
it does not need to be a generally reusable package. Split a new project
when it improves affected-task precision, graph clarity, or boundary
enforcement. Keep related code in an existing project when the split
would mostly add config and navigation cost.

Nx detects projects from `package.json` or `project.json`, and plugins
can add more detection. For dependencies Nx can infer, prefer normal
imports/package relationships. For relationships it cannot infer,
especially across non-TypeScript code, declare `implicitDependencies`.

## Type vocabulary

Nx's documented baseline uses a small type vocabulary:

- `type:feature`: business use case or application flow. It can depend
  on any lower-level project type.
- `type:ui`: presentational UI. It depends on `type:ui` and `type:util`.
  Chitin is currently non-UI, so this should usually be absent.
- `type:data-access`: persistence, state, external-system delegates, and
  read/write access layers. It depends on `type:data-access` and
  `type:util`.
- `type:util`: low-level utilities and pure functions. It depends only
  on `type:util`.

Nx explicitly says each repository may need its own types, but also
recommends keeping the type set small. For chitin, add custom types only
when the four baseline types cannot describe an actual boundary.

Chitin-specific additions that are justified:

- `type:app`: runnable entrypoints only. Apps should be thin.
- `type:contract`: canonical schemas, generated types, and public
  cross-language contracts. Contracts are not generic utilities; they
  define repo-wide API boundaries.
- `type:adapter`: driver or substrate integration shims.
- `type:tooling`: repo maintenance tools, generators, lint helpers.

Avoid vague layer names such as `layer:app` or `layer:cli` as the only
classification. They mix role, product scope, and runtime. Use separate
dimensions instead.

## Tag dimensions

Every Nx project should carry at least these dimensions:

- `type:*` - architectural role.
- `scope:*` - product/domain scope.
- `lang:*` - implementation language.

Optional dimensions:

- `runtime:*` - only when execution environment matters for boundaries
  or task selection.
- `deploy:*` - only for independently distributed artifacts.

Recommended chitin scopes:

- `scope:kernel` - Go execution kernel and kernel-owned contracts.
- `scope:cli` - operator CLI surfaces.
- `scope:contracts` - canonical event/request schemas.
- `scope:telemetry` - read-side chain indexes, replay, analytics APIs.
- `scope:analysis` - chain-derived Python analysis libraries.
- `scope:driver` - driver/substrate adapters and plugins.
- `scope:tooling` - repo generators, lint, and maintenance tools.

Recommended language tags:

- `lang:go`
- `lang:ts`
- `lang:py`

## Folder shape for chitin

Target shape:

```text
apps/
  cli/                         # thin app: command parsing and composition

libs/
  contracts/                   # type:contract, scope:contracts, lang:ts
  telemetry/                   # type:data-access, scope:telemetry, lang:ts
  analysis/                    # type:data-access, scope:analysis, lang:py
  adapters/
    claude-code/               # type:adapter, scope:driver, lang:ts
    openclaw/                  # type:adapter, scope:driver, lang:ts
    ollama-local/              # type:adapter, scope:driver, lang:ts
  tooling/
    generators/                # type:tooling, scope:tooling, lang:ts
    lint/                      # type:tooling, scope:tooling, lang:ts

go/
  execution-kernel/            # type:app, scope:kernel, lang:go
```

The Go module can remain under `go/execution-kernel` while still being
an Nx project. Go's `internal/` directory is a language-level boundary;
do not move kernel internals into `libs/` just to make the tree look
uniform. Instead, make the kernel's Nx metadata explicit and keep
`cmd/chitin-kernel` thin.

If Go packages need graph-level visibility beyond one kernel project,
use project-level metadata or a Go-aware Nx plugin later. Do not split
Go internals into fake Nx projects unless the graph, cache, and boundary
benefit is real.

## Dependency rules

Baseline tag rules for TypeScript/JavaScript enforcement:

```js
depConstraints: [
  {
    sourceTag: 'type:app',
    onlyDependOnLibsWithTags: [
      'type:feature',
      'type:data-access',
      'type:contract',
      'type:adapter',
      'type:util',
    ],
  },
  {
    sourceTag: 'type:feature',
    onlyDependOnLibsWithTags: [
      'type:feature',
      'type:data-access',
      'type:contract',
      'type:adapter',
      'type:ui',
      'type:util',
    ],
  },
  {
    sourceTag: 'type:adapter',
    onlyDependOnLibsWithTags: ['type:contract', 'type:util'],
  },
  {
    sourceTag: 'type:data-access',
    onlyDependOnLibsWithTags: ['type:data-access', 'type:contract', 'type:util'],
  },
  {
    sourceTag: 'type:contract',
    onlyDependOnLibsWithTags: ['type:util'],
  },
  {
    sourceTag: 'type:ui',
    onlyDependOnLibsWithTags: ['type:ui', 'type:util'],
  },
  {
    sourceTag: 'type:util',
    onlyDependOnLibsWithTags: ['type:util'],
  },
  {
    sourceTag: 'type:tooling',
    onlyDependOnLibsWithTags: [
      'type:contract',
      'type:data-access',
      'type:util',
      'type:tooling',
    ],
  },
]
```

Scope rules should encode chitin's product boundary:

- `scope:contracts` should not depend on product scopes except small
  utilities.
- `scope:telemetry` may depend on `scope:contracts`, not on apps or
  adapters.
- `scope:driver` may depend on `scope:contracts` and small utilities,
  not telemetry unless the adapter is explicitly read-side only.
- `scope:cli` may compose contracts, telemetry, and adapters, but should
  not own kernel logic.
- `scope:kernel` should be isolated from TS libraries at import level;
  contract parity is through generated files and tests, not runtime
  imports.

Nx's open-source ESLint rule enforces JS/TS imports and package
dependencies. Nx's language-agnostic Conformance rule enforces project
boundaries over the full graph, but it requires Nx Enterprise. Until
Conformance or a local graph check is adopted, represent Go/Python
relationships with project metadata, `implicitDependencies`, and CI
targets, then enforce what can be enforced in open-source Nx.

## Apps stay thin

In Nx terms, apps are runnable composition points. For chitin:

- `apps/cli` owns argument parsing, command wiring, process invocation,
  and user-facing output.
- Reusable command behavior belongs in `libs/*` when it has a real API
  boundary.
- Kernel side effects remain in `go/execution-kernel`.
- A new app is justified only when it non-trivially consumes kernel state
  in a way the CLI cannot express and no substrate already owns.

## Buildable and publishable libraries

Most chitin libraries should be workspace libraries: directly consumed
inside the monorepo with test/typecheck targets. Use buildable or
publishable Nx libraries only when an artifact must be independently
built, cached, or distributed outside the repo. Driver plugins that are
operator-installed may need build targets; internal schema and telemetry
libraries usually do not need publishable packaging.

## Restructure checklist

Before moving code:

1. Confirm the code still fits one of the allowed chitin buckets in
   `AGENTS.md`.
2. Decide the project's `type:*`, `scope:*`, and `lang:*` tags.
3. Use Nx move/remove generators when they can preserve project metadata.
4. Update package/workspace metadata so Nx still sees the project.
5. Add `implicitDependencies` for non-inferred cross-language edges.
6. Run `pnpm exec nx graph --print` and verify the graph shape.
7. Run affected or project-scoped `test`, `typecheck`, `lint`, and Go
   targets as appropriate.

When culling code:

1. Prefer `@nx/workspace:remove` for real Nx projects.
2. Delete non-project scratch/examples only after checking whether docs
   reference them as current behavior.
3. Move historical specs or observations to `docs/superpowers/superseded`
   when the historical context is useful; otherwise remove dead
   implementation references from active docs.

