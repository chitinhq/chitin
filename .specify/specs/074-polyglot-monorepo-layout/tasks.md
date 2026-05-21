# Tasks: Polyglot Monorepo Layout

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Branch**: `074-polyglot-monorepo-layout`
**Companion analysis**: `docs/strategy/chitin-monorepo-audit.md`

## Conventions

- `[P]` = parallelizable (disjoint file set from its siblings).
- Phases are strictly ordered (0 → 1 → 2 → 3); each ships as its own PR(s) and
  delivers standalone value (FR-015).
- INV-005: every change is behavior-preserving — the same target produces the
  same artifact before and after.
- INV-006 / FR-012: never register or relocate code owned by specs 069/070.
- Each new/modified test file carries a `// spec: 074-polyglot-monorepo-layout`
  (or `# spec:`) reference comment.

---

## Phase 0 — Cull drift (FR-016)

Goal: `nx show projects` reflects only real projects before the gap is measured.

- **T001** — `git rm -r apps/chitin-dashboard`: the abandoned React dashboard
  superseded by the Angular `apps/chitin-console`. Removes 12 tracked files
  including a committed `dist/`. Confirm nothing imports it (`grep -r
  chitin/dashboard`); update root `tsconfig.json` references and
  `vite.config.ts` globs if either names it.
- **T002** — Remove the untracked `apps/agentguard-vscode` leftover directory
  (its tracked files were already deleted by PR #837 — only an on-disk husk
  remains). Verify `nx show projects` no longer lists `@chitin/dashboard` or
  `chitin-agentguard`.

---

## Phase 1 — Close the registration gap (US1, P1 — zero file moves)

Goal: every surviving code directory is a registered Nx project. Satisfies
FR-001–003, SC-001/002/003/004.

- **T003** — Finalize the survivor inventory: cross-check the audit §1.2 list
  against the merged state of specs 069 (agent-bus/Octi gone) and 070
  (orchestrator surface). Record the definitive orphan list in `plan.md` or a
  `research.md`. Decide `swarm/` granularity — split along `swarm/tests/`
  boundaries (plan "Decision").
- **T004** [P] — `project.json` for `go/run-sdk`: `nx:run-commands` targets
  (`build`→`go build`, `test`→`go test`, `lint`→`go vet`, `validate`), full
  tag set (`type:lib`, `lang:go`, `scope:*`, `layer:*`).
- **T005** [P] — `project.json` for `python/argus`: targets shelling to
  `pytest`/`compileall`; tags (`type:app` — it has a CLI entry point + systemd
  units, FR-013; `lang:python`); declare the `argus → analysis` dependency.
- **T006** [P] — `project.json` for `bench/` (governance benchmark harness):
  `test`/`validate` targets; tags (`type:app`, `lang:python`).
- **T007** [P] — `project.json` for each surviving `services/*`
  (`services/mini-mcp`, `services/swarm-kanban-mcp` — NOT `agent-bus`, deleted
  by 069): targets + tags.
- **T008** — `project.json` for each surviving `swarm/*` component, split
  along the `swarm/tests/` boundaries decided in T003; skip cron/lobster/
  `swarm/bin` orchestration owned by spec 070.
- **T009** — Verification: `nx show projects` lists every survivor (SC-001);
  a Go-only change in `run-sdk` makes `nx affected -t test` run only it
  (SC-002); a second `nx run-many -t test` is all cache hits (SC-003);
  `nx graph` shows the Go/Python nodes + the `argus → analysis` edge.

---

## Phase 2 — Standardize + route CI through Nx (US2, P2)

Goal: one registration mechanism, uniform targets/tags, CI through Nx.
Satisfies FR-004–006, SC-004/005.

- **T010** — Converge on `project.json` as the single mechanism: migrate the
  `package.json` `nx`-field projects (`apps/cli`,
  `libs/router-plugin-api/typescript`) to `project.json`, OR document the
  field as the one allowed exception with a stated reason (FR-004, INV-004).
- **T011** [P] — Normalize target names to `build`/`test`/`lint`/`validate`
  across all projects (documented build opt-out where no artifact exists).
- **T012** [P] — Ensure every project carries the full
  `type:*`/`scope:*`/`layer:*`/`lang:*` tag set (FR-003).
- **T013** — Extend `tools/lint/layer-tag-coverage.ts` to validate target +
  tag presence for **all** languages; fail the build with a project-named
  message on a gap (FR-005, SC-005).
- **T014** — Rewrite `.github/workflows/ci.yml`: replace the unconditional
  `go test ./...`, `go vet ./...`, and `python -m unittest` steps with
  `nx affected`/`nx run-many` targets (FR-006, SC-004). Keep CI green.
- **T015** — Verification: the convention gate passes workspace-wide and fails
  on a removed tag; CI has zero bespoke per-language steps.

---

## Phase 3 — Converge the layout (US3, P3 — one PR per project)

Goal: type-first layout; no top-level `go/`/`python/`. Satisfies FR-007–011,
SC-006/007. **One isolated, mechanical PR per relocation.**

- **T016** — Relocate `go/execution-kernel` → `apps/execution-kernel`
  (`type:app`, FR-013): rewrite the `go.mod` module path and every internal
  `github.com/chitinhq/chitin/go/...` import in one atomic commit; gate
  `go build ./... && go test ./...` (FR-009, edge case).
- **T017** — Relocate `go/run-sdk` → `libs/run-sdk/go/`, folding it beside the
  TS `run-sdk` as the `router-plugin-api` polyglot pattern (FR-008); module
  path + imports rewritten; same gate.
- **T018** [P] — Relocate `python/analysis` → `libs/analysis` (`type:lib`,
  FR-013); update consumers' import/path references.
- **T019** — Relocate `python/argus` → `apps/argus` (`type:app`); update the
  `argus → analysis` relative path dependency to the new `libs/analysis`
  location (FR-010, edge case). Depends on T018.
- **T020** — Update workspace wiring to the final layout: `pnpm-workspace.yaml`
  globs, root `tsconfig.json` `references`, root `vite.config.ts` test globs,
  and `eslint.config.mjs` ignore paths — no glob may point at a non-existent
  or unregistered path (FR-011).
- **T021** [P] — Write the contributor guide (`docs/`): the final layout, the
  registration convention, and how to add a project in Go, Python, and
  TypeScript (FR-014).
- **T022** — Final verification: `ls` of the repo root shows no `go/` or
  `python/` language folder (SC-006); every project resolves under `apps/` or
  `libs/`; every relocation PR was green on its language gate (SC-007).

---

## Dependencies

- Phase 0 → Phase 1 → Phase 2 → Phase 3 (strict).
- Phase 1: T003 precedes T004–T008; T004–T007 are `[P]`; T008 needs T003's
  `swarm/` split decision; T009 closes the phase.
- Phase 2: T010 precedes T011/T012; T013 needs T011+T012; T014 needs T013;
  T015 closes the phase.
- Phase 3: T016, T017 independent Go moves; T018 precedes T019; T020 after all
  relocations; T021 `[P]`; T022 closes the spec.

## Parallel execution notes

- Phase 1 T004–T007 each add one isolated `project.json` — fully parallel.
- Phase 3 relocations are **not** parallelized with each other beyond the
  marked `[P]` pairs — each is its own PR with its own language gate, so the
  affected-detection baseline stays clean between merges.

## Coordination

- Phase 1 dispatched to the swarm as kanban tickets once spec 074 lands;
  Phase 1's `project.json` additions are ideal `[P]` parallel work for
  clawta/ares. Phase 3 relocations stay operator-supervised (Go module-path
  blast radius).
