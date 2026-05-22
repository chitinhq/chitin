# Tasks: Sample Feature

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md)

## Format: `[ID] [P?] [Story] Description`

- **[P]** = parallelizable
- **[US1]** = the user story a task serves

## Phase 1: Setup

- [ ] T001 Create the package skeleton — implement the directory layout per plan.md
- [ ] T002 [P] Add the import wiring — define the module dependencies

## Phase 2: Foundational

- [ ] T003 Implement the core type in `core.go` — define the central struct (FR-001)
- [ ] T004 [P] Author tests for the core type in `core_test.go` — add a table-driven test (FR-002)
- [ ] T005 [P] Author tests for the edge cases in `edge_test.go` — add a unit test for boundaries (FR-002)

## Phase 3: User Story 1 — The feature works (Priority: P1)

- [ ] T006 [US1] Implement the feature handler in `handler.go` — wire the request path (FR-003)
- [ ] T007 [US1] Write the documentation in `README.md` — author the runbook for operators (FR-004)
