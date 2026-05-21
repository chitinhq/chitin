# Feature Specification: Polyglot Monorepo Layout

**Feature Branch**: `074-polyglot-monorepo-layout`

**Created**: 2026-05-20

**Status**: Draft

**Input**: User description: "Document and analyze the repo. I want to use an
Nx monorepo for the repository. The Python folder and Go folder sit inside the
Nx monorepo but aren't really integrated with it — Go isn't under libraries or
apps. Research and write the spec for whether the libraries should be nested
under the app layers or live in apps, and how to do this correctly."

**Companion analysis**: `docs/strategy/chitin-monorepo-audit.md`

---

## Background

Chitin is an Nx 22.6.5 monorepo. Nx is language-agnostic — it orchestrates
*tasks* (`go build`, `pytest`, `tsc -b`), not JavaScript — and the repo
already proves this: `execution-kernel` (Go), `chitin-analysis` (Python), and
`chitin-router-plugin-api-python` (Python) are registered Nx projects driven
by `nx:run-commands`.

The audit (`docs/strategy/chitin-monorepo-audit.md`) found that the real
problem is **not** that Go and Python "can't" be in Nx — it is that:

1. **Registration gap** — `go/run-sdk`, `python/argus`, `services/*`, the
   surviving `swarm/*` code, and `bench/` have no `project.json`. They are
   invisible to `nx affected`, `nx graph`, caching, and CI affected-detection.
2. **Inconsistency** — three registration mechanisms (`project.json`,
   `package.json` `nx` field, pure inference), inconsistent target names and
   tags, and two contradictory polyglot layouts (`libs/router-plugin-api`
   co-locates languages by domain; `go/` and `python/` separate them by
   language at the top level).
3. **Layout** — Go and Python live in top-level `go/` and `python/` folders,
   not under `apps/`/`libs/`. Since Nx 16.8 this is purely a navigation /
   consistency concern, not a functional one.

This spec closes the gap and converges the layout, in three independently
shippable phases. **Phase 1 is the real integration win and ships first;
the file relocation (Phase 3) is staged last so it can never block it.**

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Every project is visible to Nx (Priority: P1)

A developer changes a Go file in `run-sdk` or a Python file in `argus`. They
run `nx affected -t test` and the affected Go/Python projects run their tests.
They open `nx graph` and see those projects and their dependency edges. CI
computes one affected set across all three languages instead of running
hand-rolled per-language steps unconditionally.

**Why this priority**: This is the actual "integrate Go and Python into the
monorepo" outcome. Without it, affected-detection and caching are blind to
most of the repo's code — the headline cost of the current state. It is also
the smallest provable slice: it needs **zero file moves**, only added
`project.json` files.

**Independent Test**: For each newly registered project, run
`nx show project <name>` and confirm it resolves; touch one source file in it
and confirm `nx affected -t test --base=HEAD~1` includes it.

**Acceptance Scenarios**:

1. **Given** every surviving code directory has a `project.json`, **When** a
   developer runs `nx show projects`, **Then** `go/run-sdk`, `python/argus`,
   each `services/*` survivor, each `swarm/*` survivor, and `bench/` all
   appear.
2. **Given** a Go-only change in `run-sdk`, **When** `nx affected -t test` is
   run, **Then** `run-sdk`'s test target executes and unrelated TS/Python
   projects do not.
3. **Given** a clean working tree, **When** `nx run-many -t test` is run
   twice, **Then** the second run is served from cache for projects whose
   inputs are unchanged.
4. **Given** `nx graph`, **When** it is opened, **Then** the Go and Python
   projects appear as nodes with their cross-project dependency edges
   (e.g. `argus → analysis`).

---

### User Story 2 - One consistent way to register and tag a project (Priority: P2)

A developer adds a new project — in any language — and follows exactly one
documented pattern: a `project.json` with the standard `build` / `test` /
`lint` / `validate` targets and the full `type:* / scope:* / layer:* / lang:*`
tag set. A lint gate fails the build if any project is missing a target or a
tag.

**Why this priority**: Consistency is what keeps the gap from reopening. It is
P2 because the repo is *functional* after US1 — this hardens it. Builds on US1
(can't standardize targets on projects that aren't registered yet).

**Independent Test**: Run the project-convention lint gate against the whole
workspace; confirm it passes, then remove a tag from one `project.json` and
confirm it fails with a specific message.

**Acceptance Scenarios**:

1. **Given** the convention is documented, **When** any project's
   `project.json` is inspected, **Then** it declares `build` (or a documented
   opt-out), `test`, `lint`, and `validate` targets.
2. **Given** the `layer-tag-coverage` gate is extended to all languages,
   **When** a project is missing a `lang:*`, `type:*`, `scope:*`, or
   `layer:*` tag, **Then** the gate fails naming the project and the missing
   tag.
3. **Given** the registration convention is type-first, **When** a project
   uses the legacy `package.json` `nx` field, **Then** it is migrated to a
   `project.json` (or the field is documented as the single allowed
   mechanism — one mechanism, not three).
4. **Given** CI, **When** it runs, **Then** Go and Python are exercised via
   `nx affected`/`nx run-many` targets, not bespoke `go test` / `unittest`
   steps.

---

### User Story 3 - Apps and libs mean what they say (Priority: P3)

A new contributor opens the repo. Every application — Angular, Node, Go binary,
Python CLI — is under `apps/`. Every library is under `libs/`. They never have
to know a project's language to find it. A multi-language domain (like
`router-plugin-api`) groups its languages in subfolders under one project
directory, the same way everywhere.

**Why this priority**: This is the cosmetic / navigation fix the goal
describes ("Go isn't under apps/libs… weird"). It is genuinely valuable but
P3 because the repo is fully integrated and consistent after US1+US2 — this is
the last, mechanical step, and it carries the only non-trivial risk (Go module
path rewrites), so it ships last and per-project.

**Independent Test**: After each relocation PR, run
`go build ./... && go test ./...` (Go) or the project's `nx test` (Python),
and `nx graph` — confirm the project resolves at its new path with no broken
imports.

**Acceptance Scenarios**:

1. **Given** the target layout, **When** the top level is listed, **Then**
   there is no top-level `go/` or `python/` folder — every project is under
   `apps/` or `libs/`.
2. **Given** `go/execution-kernel` is relocated to `apps/execution-kernel`,
   **When** `go build ./... && go test ./...` runs, **Then** it passes — the
   `go.mod` module path and every internal import have been updated in one
   atomic change.
3. **Given** `python/analysis` and `python/argus` are relocated, **When**
   `argus` runs its tests, **Then** the `analysis` editable path dependency
   resolves at its new location.
4. **Given** a multi-language project, **When** it is inspected, **Then** it
   follows the `router-plugin-api` pattern: one project directory with
   `python/` / `typescript/` / `go/` subfolders, each its own Nx project.

---

### Edge Cases

- **Go module path on relocation**: moving a Go module changes its module
  path; every internal import (`github.com/chitinhq/chitin/go/...`) must be
  rewritten in the same commit. Gate: `go build ./... && go test ./...`.
- **Python relative dependency**: `argus` depends on `analysis` via
  `path = "../analysis"`; relocating either updates that path.
- **`pnpm-workspace.yaml`**: the `go/*` glob currently matches a directory
  with no `package.json`. After relocation/registration the workspace globs
  must be corrected so they describe reality.
- **Code being deleted by specs 069/070**: `services/agent-bus` and
  `swarm/octi` are slated for removal. They MUST be skipped — not registered,
  not relocated.
- **Root `vite.config.ts` globs**: hard-coded `apps/**` / `libs/**` test
  globs must still match after relocation, or be replaced by Nx-driven test
  discovery.
- **`tsconfig.json` project references**: relocating any TS project updates
  the root `references` array.
- **CI base-ref**: `nx affected` needs correct git history depth in CI
  (already configured) — verify it still holds after registration.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Every code-bearing directory that survives specs 069 and 070
  MUST be a registered Nx project — specifically `go/run-sdk`,
  `python/argus`, each surviving `services/*`, each surviving `swarm/*`
  component, and `bench/`.
- **FR-002**: Each registered project MUST declare standard targets: `test`,
  `lint`, `validate`, and `build` where a build artifact exists (a documented
  opt-out is allowed for projects with no build step).
- **FR-003**: Each registered project MUST carry the full tag set:
  `type:{app|lib}`, `scope:*`, `layer:*`, and `lang:{go|python|ts}`.
- **FR-004**: The repo MUST converge on a **single** registration mechanism.
  `project.json` is the chosen mechanism; projects using the `package.json`
  `nx` field MUST be migrated, OR the field MUST be documented as the one
  allowed exception with a stated reason.
- **FR-005**: The `tools/lint/layer-tag-coverage` gate MUST be extended to
  validate target and tag presence for **all** projects, Go and Python
  included, and MUST fail the build on a violation.
- **FR-006**: CI (`.github/workflows/ci.yml`) MUST exercise Go and Python
  through Nx targets (`nx affected` / `nx run-many`), replacing the bespoke
  unconditional `go test`, `go vet`, and `python -m unittest` steps.
- **FR-007**: The target folder layout MUST be type-first: every application
  under `apps/`, every library under `libs/`, with no top-level `go/` or
  `python/` language folder remaining.
- **FR-008**: A multi-language project MUST follow the `router-plugin-api`
  pattern — one project directory with per-language subfolders, each its own
  Nx project — and this MUST be the single documented polyglot pattern.
- **FR-009**: Relocating a Go module MUST update its `go.mod` module path and
  every internal import in one atomic change, gated by
  `go build ./... && go test ./...`.
- **FR-010**: Relocating a Python package MUST update every relative path
  dependency (e.g. `argus`'s editable dependency on `analysis`).
- **FR-011**: `pnpm-workspace.yaml`, root `tsconfig.json` `references`, and
  root `vite.config.ts` test globs MUST be updated to match the final layout;
  no glob may point at a non-existent or unregistered path.
- **FR-012**: Code slated for deletion by specs 069 (`services/agent-bus`,
  `swarm/octi`) and 070 (cron/lobster/`swarm/bin` orchestration) MUST NOT be
  registered or relocated by this spec.
- **FR-013**: The classification of each relocated project MUST be recorded:
  a project with a runnable entry point / binary / served process is an
  `app`; a project consumed only as a dependency is a `lib`. Specifically
  `execution-kernel` and `argus` are apps; `run-sdk` and `analysis` are libs.
- **FR-014**: A contributor-facing document MUST describe the final layout,
  the registration convention, and how to add a project in each language
  (Go, Python, TypeScript).
- **FR-015**: Each phase MUST be independently shippable: Phase 1 (FR-001–003)
  ships and delivers value with zero file moves; Phase 2 (FR-004–006) ships
  on top of it; Phase 3 (FR-007–011) ships last, one project per PR.
- **FR-016**: Dead / drift projects — code that survives no feature and is
  superseded — MUST be **removed**, not registered. The audit identified
  `apps/chitin-dashboard` (an abandoned React dashboard superseded by the
  Angular `apps/chitin-console`; it even commits its `dist/` build output)
  and the untracked `apps/agentguard-vscode` leftover (its tracked files were
  already removed by PR #837). A code-bearing directory that is neither a
  living project nor deletion-bound by another spec is drift; this spec culls
  it. This is **Phase 0** — it runs before registration so the gap analysis
  and `nx show projects` count reflect only real projects.

### Key Entities

- **Nx project**: a named, registered unit of work — `project.json` (name,
  `projectType`, `tags`, `targets`) — that Nx can build, test, cache, and
  place in the dependency graph.
- **Target**: a named task on a project (`build`, `test`, `lint`,
  `validate`), here implemented via `nx:run-commands` for Go/Python.
- **Tag**: a label (`type:*`, `scope:*`, `layer:*`, `lang:*`) driving Nx
  boundary rules and the `layer-tag-coverage` gate.
- **Registration gap**: the set of code directories with no `project.json` —
  invisible to Nx affected/graph/cache.
- **Polyglot project**: one project directory holding multiple
  per-language Nx projects in subfolders (the `router-plugin-api` pattern).

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: `nx show projects` lists 100% of surviving code-bearing
  directories — zero registration gap.
- **SC-002**: `nx affected -t test` triggered by a Go-only or Python-only
  change runs exactly that language's affected projects and nothing
  unrelated.
- **SC-003**: A second `nx run-many -t test` with no input changes is 100%
  cache hits.
- **SC-004**: CI has zero bespoke per-language test/lint steps — every
  language runs through `nx affected` / `nx run-many`.
- **SC-005**: The project-convention lint gate passes for every project and
  fails (with a project-named message) when a target or tag is removed.
- **SC-006**: After Phase 3, `ls` of the repo root shows no `go/` or
  `python/` language folder; every project resolves under `apps/` or `libs/`.
- **SC-007**: Every relocation PR is green on `go build ./... && go test
  ./...` (Go) or `nx test <project>` (Python/TS) before merge.

---

## File-system scope

Files this spec is expected to add or modify:

- `**/project.json` — new files for each orphan project (Phase 1).
- `tools/lint/layer-tag-coverage.ts` — extended coverage (Phase 2).
- `.github/workflows/ci.yml` — Nx-driven Go/Python steps (Phase 2).
- `apps/`, `libs/` — relocated project directories (Phase 3).
- `go/execution-kernel/go.mod`, `go/run-sdk/go.mod` and all `*.go` imports —
  module-path rewrite on relocation (Phase 3).
- `python/argus/pyproject.toml`, `python/analysis/pyproject.toml` — relative
  path updates on relocation (Phase 3).
- `pnpm-workspace.yaml`, root `tsconfig.json`, root `vite.config.ts` —
  workspace/reference/glob updates (Phase 3).
- `docs/` — the contributor layout/registration guide (FR-014).
- `eslint.config.mjs` — boundary rules currently ignore `go/**` / `python/**`;
  update ignore paths after relocation.

Out-of-scope paths: `services/agent-bus`, `swarm/octi`, and any
cron/lobster/`swarm/bin` orchestration code owned by specs 069/070.

## Test coverage

- **Phase 1/2**: a workspace test asserting `nx show projects` includes every
  expected project; the extended `layer-tag-coverage` gate as an executable
  check.
- **Phase 3**: per-relocation, the project's own `nx test` plus
  `go build ./... && go test ./...` for Go modules — run in the relocation PR.
- Each new/modified test file carries a `// spec: 074-polyglot-monorepo-layout`
  (or `# spec:`) reference comment per the spec-kit convention.
- e2e-default per constitution §1.2: the affected-detection behavior
  (SC-002) is verified end-to-end against a real git diff, not mocked.

## Invariants

- **INV-001**: A code-bearing directory is either a registered Nx project or
  explicitly listed as out-of-scope (deletion-bound). There is no third
  state.
- **INV-002**: Registration is independent of folder location — Phase 1 never
  moves a file; Phase 3 never changes a `project.json` target's behavior.
- **INV-003**: Project `type` is determined by runtime shape, not language: a
  runnable binary/CLI/service is an `app`; a dependency-only package is a
  `lib`.
- **INV-004**: There is exactly one registration mechanism and exactly one
  polyglot layout pattern in the repo at the end of this spec.
- **INV-005**: Every relocation is mechanical and behavior-preserving — the
  same targets produce the same artifacts before and after the move.
- **INV-006**: This spec never registers or relocates code owned by an
  in-flight deletion spec (069, 070).

## Assumptions

- Specs 069 and 070 land their deletions before (or alongside) Phase 1, so
  the "surviving code" set is known. If they slip, Phase 1 proceeds on the
  unambiguous survivors (`go/run-sdk`, `python/argus`, `bench/`,
  `services/mini-mcp`, `services/swarm-kanban-mcp`) and defers the rest.
- The Go modules are internal — not `go get`-ed by external consumers — so a
  module-path rewrite has no external blast radius.
- `uv` (or `pip`) and the Go toolchain are available in CI and dev
  environments, as they are today.
- The existing `type:* / scope:* / layer:* / lang:*` tag taxonomy and the
  `eslint.config.mjs` boundary rules are kept; this spec extends their
  coverage, it does not redesign them.
- Nx 22.6.5's treatment of `apps/`/`libs/` as convention (not hard-coded
  layout) holds — folder location does not affect project resolution.

## Out of Scope

- Decommissioning the agent-bus or Octi (spec 069).
- Replacing cron/lobster/`swarm/bin` orchestration with Temporal (spec 070).
- Publishing any package to npm or PyPI (the `router-plugin-api` publish
  pipeline remains its own future work).
- Introducing Nx remote/cloud caching, a new Go Nx plugin, or a new Python
  Nx plugin — `nx:run-commands` is sufficient and already proven here.
- Rewriting build *logic* — this spec changes registration and location, not
  how anything compiles or tests.
- Option C (domain-first `packages/`) from the audit — rejected as
  disproportionate churn.

## Dependencies

- **Spec 069** (decommission agent-bus + Octi) — defines deletion-bound code.
- **Spec 070** (Chitin Orchestrator) — defines surviving swarm orchestration.
- Companion analysis: `docs/strategy/chitin-monorepo-audit.md`.

## Open questions

- Should `swarm/` be registered as one coarse Nx project or split into
  several (`swarm-mini`, `swarm-workflows`, …)? Recommendation: split by
  the natural test boundaries already in `swarm/tests/`, decided at Phase 1
  planning once 070's surviving surface is final.
- For `go/run-sdk`: does it stay a standalone lib, or fold into the
  `run-sdk` TS lib as `libs/run-sdk/go/` (the `router-plugin-api` pattern)?
  Recommendation: fold it, since both implement the same SDK contract —
  confirm at Phase 3 planning.
