# Tasks: 066 Grooming Telemetry

**Input**: Design documents from `.specify/specs/066-grooming-telemetry/`

**Prerequisites**: `spec.md`, `plan.md`

**Tests**: Required. This is board-health tooling that will drive automated
operator decisions, so fixture-backed CLI tests are part of the acceptance
surface.

## Phase 1: Structured Grooming Decision Records

**Goal**: Grooming actions can emit one stable, parseable JSON comment through
the sanctioned Hermes write path.

**Independent Test**: Run the emitter in `--dry-run` mode and parse stdout with
`jq .grooming_decision`; run unit tests that verify required fields and invalid
inputs.

- [ ] T001 [P] [US1] Add `swarm/tests/test_board_groom_emit.py` covering valid record generation, invalid confidence, invalid stage, and JSON parseability.
- [ ] T002 [US1] Implement `swarm/bin/board-groom-emit` with `--board`, `--ticket`, `--action`, `--rationale`, `--confidence`, `--stage`, `--pipeline-position`, `--actor`, `--session-id`, and `--dry-run`.
- [ ] T003 [US1] Make `swarm/bin/board-groom-emit` submit comments via `hermes kanban --board <board> comment <ticket> --author <actor> <json>` when not in dry-run mode.
- [ ] T004 [US1] Ensure the emitted JSON includes `schema: chitin.grooming_decision.v1` and the AC1 fields without Markdown wrapping.

**Checkpoint**: AC1 and AC6 are testable without changing grooming cron callers.

---

## Phase 2: Drift Analysis Report

**Goal**: Operators can run one CLI to inspect board drift across the six
required dimensions.

**Independent Test**: Build a temporary Hermes board fixture containing tasks,
events, comments, archives, unblocks, reblocks, reassignments, and default
assignees; assert the report includes each expected section and ticket ID.

- [ ] T005 [P] [US2] Add `swarm/tests/test_board_drift.py` with SQLite fixtures for `tasks`, `task_events`, and `task_comments`.
- [ ] T006 [US2] Implement read-only board discovery in `swarm/bin/board-drift` for `~/.hermes/kanban/boards/*/kanban.db` plus an override flag for tests.
- [ ] T007 [US2] Implement time-in-status histograms for created-to-ready, ready-to-claimed, and claimed-to-done durations.
- [ ] T008 [US2] Implement bounce detection for repeated status cycling on a ticket.
- [ ] T009 [US2] Implement assignment stability metrics by ticket and assignee.
- [ ] T010 [US2] Implement ready-stall detection for tickets ready longer than two hours with no claim event.
- [ ] T011 [US2] Implement grooming accuracy metrics for unblock-then-reblock and archive-then-restore sequences.
- [ ] T012 [US2] Implement `assignee=default` frequency reporting with ticket IDs and board names.
- [ ] T013 [US2] Add per-session grooming accuracy grouping from structured grooming comments.
- [ ] T014 [US2] Render a stable Markdown report with the six AC2 section headings.

**Checkpoint**: AC2 and AC3 pass against fixture data.

---

## Phase 3: Integration and Governance

**Goal**: The tooling is discoverable, executable, and compliant with Chitin
kanban/governance constraints.

**Independent Test**: Run the new test files and the existing kanban isolation
checker.

- [ ] T015 [P] [US3] Add `spec: 066-grooming-telemetry` references to new executable and test files.
- [ ] T016 [US3] Run `pytest swarm/tests/test_board_groom_emit.py swarm/tests/test_board_drift.py`.
- [ ] T017 [US3] Run `bash scripts/check-swarm-kanban-isolation.sh`.
- [ ] T018 [US3] Run `swarm/bin/board-groom-emit --dry-run ... | jq .grooming_decision`.
- [ ] T019 [US3] Run `swarm/bin/board-drift --boards-root <fixture-root>` and verify the Markdown sections render.

**Checkpoint**: AC4, AC5, and AC6 are satisfied for review.

---

## Dependencies and Execution Order

- Phase 1 can be implemented independently of Phase 2.
- Phase 2 can begin after T005 defines the fixture shape.
- Phase 3 depends on Phases 1 and 2.
- T001 and T005 can be worked in parallel because they touch separate tests.
- T002-T004 must land before wiring any grooming cron caller to the emitter.
- T006-T014 should remain in `board-drift` until an in-repo Hermes CLI command
  surface exists.

## Slice Tickets

If this work is decomposed further, derive child tickets only from these slices:

- Slice A: T001-T004, structured grooming decision emitter.
- Slice B: T005-T014, board drift analysis report.
- Slice C: T015-T019, governance and integration verification.
