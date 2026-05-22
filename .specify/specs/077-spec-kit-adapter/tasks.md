# Tasks: Spec-Kit Adapter

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md)

## Format: `[ID] [P?] [Story] Description`

- **[P]** = parallelizable (different files, no incomplete dependency)
- **[US1/US2/US3]** = the user story a task serves (story phases only)

## Path Conventions

New Go package `go/orchestrator/adapter/` inside the spec-070 orchestrator
module — interface + registry + compile activity + diff at the root,
one sub-package per kit (`speckit/`, `openspec/`, `superpowers/`), fixtures
under `testdata/`. Consumes `go/orchestrator/dag/` (spec 076) and the
spec-075 capability taxonomy; owns neither.

---

## Phase 1: Setup (Shared Infrastructure)

- [ ] T001 Create the `go/orchestrator/adapter/` package skeleton — root files (`adapter.go`, `registry.go`, `compile.go`, `diff.go`, `context.go`, `constitution.go`, `errors.go`), the `speckit/`, `openspec/`, `superpowers/` sub-package directories, and `testdata/` — per plan.md
- [ ] T002 [P] Add the import wiring to `go/orchestrator/adapter/adapter.go` — depend on `go/orchestrator/dag/` (spec 076 schema) and the spec-075 capability taxonomy package; no new third-party dependency

## Phase 2: Foundational (Blocking Prerequisites)

- [ ] T003 Define the `SpecKitAdapter` interface in `go/orchestrator/adapter/adapter.go` — `Detect(repoRoot) (bool, error)` and `Compile(repoRoot, specRef) (dag.DAG, error)`; the scheduler obtains a DAG ONLY through this interface (FR-001, FR-003)
- [ ] T004 Implement the adapter registry + per-repo kit detection in `go/orchestrator/adapter/registry.go` — register concrete adapters, detect the kit by marker presence (`.specify/`, `openspec/`, superpowers skill markers), select one adapter; report "no recognized kit" explicitly when none match (FR-002, FR-008)
- [ ] T005 Implement the spec→DAG compile Temporal activity in `go/orchestrator/adapter/compile.go` — a pure, deterministic, side-effect-free activity that selects the adapter via the registry and returns the normalized 076 DAG; no `time.Now`, no network, no writes (FR-001, FR-003)
- [ ] T006 [P] Implement the DAG-diff function in `go/orchestrator/adapter/diff.go` — given a prior DAG and a freshly compiled DAG, return added / removed / changed nodes; never a silent wholesale replacement of in-flight work (FR-012)
- [ ] T007 [P] Implement compilation failure handling in `go/orchestrator/adapter/errors.go` — malformed/unparseable artifacts fail with the file and location; a dangling dependency reference fails naming the missing target; never emit a partial DAG (FR-010, FR-011)
- [ ] T008 [P] Implement Task Context extraction in `go/orchestrator/adapter/context.go` — pull FR references, file paths, and spec/plan excerpts a node needs so a driver can act without re-reading the kit (FR-005)

**Checkpoint**: an adapter can be registered, a compile activity invoked, and failures + diffs handled — kit adapters can now be built.

## Phase 3: User Story 1 — Compile a spec-kit repo into the DAG (Priority: P1) 🎯 MVP

**Goal**: the GitHub spec-kit adapter compiles `.specify/specs/NNN-name/` into a normalized Work-Unit DAG the 076 scheduler runs unchanged.
**Independent test**: point the adapter at a known `specs/NNN-name/` directory; confirm one node per `tasks.md` task, edges matching the ordering + `[P]` markers, and that the 076 scheduler accepts it.

- [ ] T009 [US1] Implement the spec-kit `tasks.md` parser in `go/orchestrator/adapter/speckit/parse.go` — parse the `[ID] [P?] [Story]` task list; emit exactly one DAG node per task (FR-004)
- [ ] T010 [US1] Implement spec-kit edge derivation in `go/orchestrator/adapter/speckit/edges.go` — sequential `depends_on` edges between non-`[P]` tasks; `[P]` tasks within a phase as parallel siblings (FR-004)
- [ ] T011 [US1] Implement spec-kit metadata mapping in `go/orchestrator/adapter/speckit/metadata.go` — carry capability tag (from the spec-075 taxonomy) and priority from task metadata onto each node (FR-004, FR-014)
- [ ] T012 [US1] Implement spec-kit task-context extraction in `go/orchestrator/adapter/speckit/context.go` — wire `tasks.md` FR references and file paths plus `spec.md`/`plan.md` excerpts into the node via `adapter/context.go` (FR-005)
- [ ] T013 [US1] Implement the `speckit.Adapter` in `go/orchestrator/adapter/speckit/adapter.go` — `Detect` on `.specify/` presence, `Compile` orchestrating parse → edges → metadata → context; register it with the registry (FR-001, FR-003, FR-004)
- [ ] T014 [US1] Add the spec-kit fixture tree at `go/orchestrator/adapter/testdata/speckit/` and a table-driven compile test in `go/orchestrator/adapter/speckit/adapter_test.go` — assert one node per task, correct edges, and that the 076 scheduler accepts the DAG (FR-003, FR-004, SC-002)

**Checkpoint**: spec-kit repos — including chitin's own `specs/` — compile to a DAG the scheduler runs. MVP delivered.

## Phase 4: User Story 2 — A second kit, zero scheduler change (Priority: P2)

**Goal**: an OpenSpec adapter compiles `openspec/changes/<name>/` into the same normalized DAG, preserving brownfield change-kind — with zero scheduler diff.
**Independent test**: compile an OpenSpec repo; confirm a structurally valid DAG, ADDED/MODIFIED/REMOVED preserved as node metadata, and the scheduler runs it with no scheduler-code change.

- [ ] T015 [US2] Implement the OpenSpec parser in `go/orchestrator/adapter/openspec/parse.go` — parse `openspec/changes/<name>/` (proposal/apply/archive); emit one node per change delta (FR-006)
- [ ] T016 [US2] Implement OpenSpec change-kind metadata in `go/orchestrator/adapter/openspec/metadata.go` — preserve each delta's ADDED / MODIFIED / REMOVED change-kind as node metadata; map capability from the spec-075 taxonomy (FR-007, FR-014)
- [ ] T017 [US2] Implement OpenSpec edge derivation + task-context extraction in `go/orchestrator/adapter/openspec/edges.go` — edges from OpenSpec phase ordering; node context via `adapter/context.go` (FR-003, FR-005)
- [ ] T018 [US2] Implement the `openspec.Adapter` in `go/orchestrator/adapter/openspec/adapter.go` — `Detect` on `openspec/` presence, `Compile` orchestrating the above; register it with the registry (FR-001, FR-002, FR-006)
- [ ] T019 [US2] Add the OpenSpec fixture tree at `go/orchestrator/adapter/testdata/openspec/` and a table-driven compile test in `go/orchestrator/adapter/openspec/adapter_test.go` — assert a valid normalized DAG with change-kind preserved (FR-006, FR-007, SC-003)
- [ ] T020 [US2] Confirm zero scheduler diff — assert no source change outside `go/orchestrator/adapter/` was needed to add the second kit (FR-002, SC-001)

**Checkpoint**: two kits compile through one interface; the kit-agnostic thesis is proven.

## Phase 5: User Story 3 — Detect the kit; surface ambiguity honestly (Priority: P3)

**Goal**: detection selects the adapter automatically, requires an explicit choice when a repo uses two kits, and emits `NEEDS CLARIFICATION` rather than guessing an ambiguous edge or capability.
**Independent test**: run detection over repos of each kit; feed a spec with a known ambiguous dependency; confirm the node carries `NEEDS CLARIFICATION` and is not auto-edged.

- [ ] T021 [P] [US3] Implement multi-kit ambiguity handling in `go/orchestrator/adapter/registry.go` — when more than one kit marker is present (chitin itself has `.specify/` and `docs/superpowers/`), require an explicit kit choice; never pick silently (FR-008)
- [ ] T022 [P] [US3] Implement the superpowers adapter in `go/orchestrator/adapter/superpowers/adapter.go` (+ `parse.go`) — `Detect` on superpowers skill markers, `Compile` skill-driven plans into the normalized DAG; register it (FR-002, FR-006)
- [ ] T023 [US3] Implement `NEEDS CLARIFICATION` marking in `go/orchestrator/adapter/compile.go` and `context.go` — a node whose dependency or required capability is left ambiguous by the kit's artifacts is marked `NEEDS CLARIFICATION`; never invent an edge (FR-009)
- [ ] T024 [US3] Enforce the closed capability vocabulary in `go/orchestrator/adapter/context.go` — a task that maps to no spec-075 taxonomy tag is marked `NEEDS CLARIFICATION`, never given an invented tag (FR-014)
- [ ] T025 [US3] Implement the canonical constitution projection in `go/orchestrator/adapter/constitution.go` — write the canonical project constitution into each kit's expected location (e.g. `.specify/memory/constitution.md` for spec-kit) (FR-013)
- [ ] T026 [US3] Add the ambiguous-dependency fixture at `go/orchestrator/adapter/testdata/ambiguous/` and a test in `go/orchestrator/adapter/compile_test.go` — assert the node carries `NEEDS CLARIFICATION` and is not auto-edged; assert correct adapter selection per kit and explicit-choice on two-kit repos (FR-008, FR-009, FR-014, SC-004)

**Checkpoint**: detection is automatic, ambiguity is honest — all three user stories functional.

## Phase 6: Polish & Cross-Cutting

- [ ] T027 [P] Add the before/after spec pair at `go/orchestrator/adapter/testdata/diff/` and a DAG-diff test in `go/orchestrator/adapter/diff_test.go` — recompiling the changed spec yields a correct added/removed/changed diff (FR-012, SC-006)
- [ ] T028 [P] Complete the fixture-based test suite — table-driven compile tests cover every kit fixture tree under `testdata/` and the malformed-artifact / dangling-reference failure paths with precise locations (FR-010, FR-011, SC-005)
- [ ] T029 Re-run the Constitution Check — confirm all six principles still hold post-implementation (§1/§2/§3/§6 PASS, §4/§5 N/A)

---

## Dependencies

- **Phase 1 → Phase 2 → Phases 3+**: Setup and Foundational block all kit adapters.
- **US1 (P1)** is the MVP — the spec-kit adapter; independently shippable once Phase 1+2 are done.
- **US2 (P2)** depends on Phase 2 (the interface + registry + compile activity); independent of US1 — the OpenSpec adapter is its own sub-package.
- **US3 (P3)** depends on Phase 2; best done after US1/US2 prove the model, since detection and ambiguity-marking are convenience + correctness over working compilers.
- Within a kit story: parse → edges → metadata → context → adapter wire-up → fixture test.

## Parallel Execution Examples

- Phase 2: T006, T007, T008 in parallel (distinct root files — `diff.go`, `errors.go`, `context.go`).
- Phase 5: T021, T022 in parallel (registry change vs. the superpowers sub-package).
- Phase 6: T027, T028 in parallel (distinct test files / fixtures).

## Implementation Strategy

**MVP = US1 (the spec-kit adapter).** Phase 1 + Phase 2 + Phase 3 deliver a
single working adapter that compiles chitin's own `specs/` into a DAG the
076 scheduler runs — that alone proves the normalized-DAG production
contract and lets the platform dogfood itself. US2 proves kit-agnosticism
(two kits, zero scheduler diff); US3 adds detection and honest
ambiguity-marking. Each kit is an isolated sub-package, so a fourth kit
later is a new directory and a registry line — nothing in the scheduler or
orchestrator core moves.
