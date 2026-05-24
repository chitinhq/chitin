# Feature Specification: 097-fixture (round-trip test fixture)

**Feature Branch**: `feat/097-fixture`

**Created**: 2026-05-23

**Status**: Draft

**Input**: User description: "Minimal lint-clean fixture spec consumed by the spec 097 implementation's round-trip integration test. The fixture's tasks.md compiles into a small DAG with 3-4 tasks all mapping to code.implement, so dispatch finds a driver in the default registry and the schedule → status → cancel round-trip completes deterministically against a sandbox Temporal."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — The fixture compiles into a routable DAG (Priority: P1)

A test harness invokes `speckit.New().CompileSpec(repoRoot, "097-fixture")` against this directory and gets back a 3-node `*dag.DAG` whose every node's `Capability` resolves to `code.implement`. The DAG is dispatched against a sandbox Temporal cluster; the SchedulerWorkflow accepts it; the operator entrypoint's `status` query returns a non-empty `NodeStatus` map; the operator entrypoint's `cancel` cleanly winds the workflow down.

**Why this priority**: this fixture exists to verify the end-to-end round-trip. If the fixture itself can't compile, every dependent test breaks.

**Independent Test**: run `chitin-orchestrator schedule 097-fixture --repo-root <fixture-parent>` and assert exit 0, a RunID in stdout, and a `SchedulerWorkflow` in Running state in the Temporal UI within 10 seconds.

**Acceptance Scenarios**:

1. **Given** this fixture directory and a fresh sandbox Temporal, **When** the schedule subcommand runs against it, **Then** a workflow starts with all 3 nodes mapped to `code.implement` capability, exit code 0, and one `scheduler_started` chain event is emitted.

### Edge Cases

- The fixture's `tasks.md` deliberately uses task descriptions whose keywords are mapped to `code.implement` by the spec-077 adapter's `MapCapability` — verifying the adapter's keyword extraction works end-to-end through the schedule subcommand.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The fixture MUST compile to a non-empty DAG via the spec-077 adapter without error.
- **FR-002**: Every compiled node's `Capability` MUST resolve to `code.implement` (a capability declared by claudecode, codex, and openclaw drivers in the default registry).
- **FR-003**: The fixture's `tasks.md` MUST be lint-clean per `chitin-kernel speckit-lint`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: `chitin-orchestrator schedule 097-fixture` exits 0 in under 10 seconds against a local Temporal dev server.
- **SC-002**: The Temporal UI shows the resulting `SchedulerWorkflow` in `Running` or `Completed` state (depending on driver availability in the test harness).

## Assumptions

- A local Temporal dev server is available at `127.0.0.1:7233` (the test harness ensures this).
- The default driver registry built by `cmd/chitin-orchestrator/main.go` is in scope; the test harness reuses the same registry construction.
- This fixture is consumed only by tests in `go/orchestrator/cmd/chitin-orchestrator/`; it is not promoted to a real chitin spec.
