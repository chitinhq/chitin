---

description: "Tasks — 097-fixture (round-trip test fixture)"
---

# Tasks: 097-fixture

**Input**: Design documents from `/specs/097-fixture/`

**Prerequisites**: spec.md (required), plan.md (required)

**Note**: This is a TEST FIXTURE consumed by spec 097's integration tests. The task descriptions are deliberately simple "implement" keywords so spec-077's MapCapability resolves every one to `code.implement` — a capability declared by claudecode, codex, and openclaw drivers in the default registry. No actual work is dispatched against this fixture; the round-trip test schedules + queries + cancels before any driver picks up a work unit.

## Phase 1: Implementation

- [ ] T001 [P] [US1] Implement the placeholder handler in fixture/handler.go
- [ ] T002 [P] [US1] Implement the placeholder validator in fixture/validator.go
- [ ] T003 [US1] Implement the placeholder wiring in fixture/main.go
