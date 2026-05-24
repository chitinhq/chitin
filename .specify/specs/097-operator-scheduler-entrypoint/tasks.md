---

description: "Task list — 097 operator entrypoint for the spec-DAG scheduler"
---

# Tasks: Operator entrypoint for the spec-DAG scheduler

**Input**: Design documents from `/specs/097-operator-scheduler-entrypoint/`

**Prerequisites**: plan.md (✓), spec.md (✓), research.md (✓), data-model.md (✓), contracts/ (✓), quickstart.md (✓)

**Tests**: included — spec.md's success criteria (SC-001 through SC-005) and the quickstart's verification recipe both require an end-to-end round-trip test plus negative-path tests for the exit-code contract.

**Organization**: Tasks are grouped by user story. The spec has two P1 stories; both are part of the MVP (the CLI is not useful with just one half). Independent-testability within each story is preserved by separating handler code from integration glue.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Different files, no dependencies on incomplete tasks — safe to run in parallel.
- **[Story]**: `[US1]` schedule; `[US2]` status + cancel. No story label = Setup, Foundational, or Polish.
- Paths are absolute-from-repo-root unless otherwise noted.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Land the test fixture and stage the subcommand dispatcher in `main.go` without yet implementing any handlers. After this phase, the binary's existing no-args worker-host behavior is byte-identical to today's; subcommand parsing exists but every subcommand is a stub that exits with `not implemented`.

- [ ] T001 [P] Create fixture spec under `go/orchestrator/cmd/chitin-orchestrator/testdata/097-fixture/` with `spec.md`, `plan.md`, `tasks.md` (3-4 tasks all mapping to `code.implement`), `checklists/requirements.md` — every file lint-clean per `chitin-kernel speckit-lint`
- [ ] T002 Extract the worker-host main into a function `runWorkerHost(ctx context.Context) int` in `go/orchestrator/cmd/chitin-orchestrator/main.go` so the dispatcher can call it as the no-subcommand default; verify by running `chitin-orchestrator` and asserting the existing worker behavior is unchanged
- [ ] T003 Add the subcommand dispatcher to `main.go`: parse `os.Args[1]`, dispatch to `schedule` / `status` / `cancel` (all initially returning `exit 1` with `not yet implemented`), fall through to `runWorkerHost` for no-args or unknown-subcommand argv shapes; preserve the existing systemd-unit invocation behavior

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared helpers all three subcommands depend on. After this phase, the three subcommand handlers can be implemented independently.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [ ] T004 [P] Implement Temporal client helper `dialTemporal(ctx, hostport) (client.Client, error)` in `go/orchestrator/cmd/chitin-orchestrator/client.go`; respects `--temporal-host` flag → `$TEMPORAL_HOSTPORT` env → `client.DefaultHostPort` default per D6; exit-2 on dial error with the spec'd stderr message
- [ ] T005 [P] Implement repo-root resolver `resolveRepoRoot(flag string) (string, error)` in `go/orchestrator/cmd/chitin-orchestrator/repo.go`; respects `--repo-root` flag → `$CHITIN_REPO_ROOT` env → `git rev-parse --show-toplevel` from cwd; exit-2 if none resolve
- [ ] T006 [P] Implement spec-ref resolver `resolveSpecRef(repoRoot, ref string) (SpecRefResolution, error)` in `go/orchestrator/cmd/chitin-orchestrator/specref.go` per D9 (exact → numeric prefix → slug); returns sorted candidate list on ambiguity for the stable stderr error per FR / Entity 2
- [ ] T007 [P] Implement chain-emit helper `emitChainEvent(ctx, eventType string, payload any) error` in `go/orchestrator/cmd/chitin-orchestrator/emit.go`; shells out to `chitin-kernel emit -event-json -` per Entity 6/7; honors `$CHITIN_KERNEL_BIN` env override per R4; fail-soft (log warn, return nil) when binary missing or emit errors
- [ ] T008 [P] Implement exit-code constants `ExitSuccess = 0`, `ExitUserError = 1`, `ExitRuntimeError = 2` in `go/orchestrator/cmd/chitin-orchestrator/exit.go` and a `runMain(args []string) int` pattern so handlers return ints rather than calling `os.Exit` directly (testability)
- [ ] T009 [P] Implement DAG pre-validator `ValidateForDispatch(d dag.DAG, r driver.Registry) []ValidationError` in `go/orchestrator/cmd/chitin-orchestrator/validate.go` per Entity 3; rejects `NeedsClarification` capabilities and capabilities no registered driver declares
- [ ] T010 Driver-registry construction helper `buildRegistry() (driver.Registry, error)` in `go/orchestrator/cmd/chitin-orchestrator/registry.go` — extract the existing registration block from `main.go:52-65` so both the worker host AND the subcommand handlers construct an identical registry (drift prevention per R2)

**Checkpoint**: Foundation ready — schedule, status, and cancel handlers can now be implemented in parallel.

---

## Phase 3: User Story 1 — Schedule a spec for implementation (Priority: P1) 🎯 MVP-half-1

**Goal**: An operator runs `chitin-orchestrator schedule 097-fixture` and the orchestrator starts a real Temporal workflow against the fixture's DAG. Per spec §US1 acceptance scenarios 1-6.

**Independent Test**: from a clean state, run `chitin-orchestrator schedule 097-fixture --repo-root <fixture-parent>` against the fixture spec; assert exit 0, stdout contains the success line with a RunID, a `scheduler_started` chain event exists, and a `SchedulerWorkflow` is visible in the Temporal UI.

### Tests for User Story 1

- [ ] T011 [P] [US1] Argv parsing test in `go/orchestrator/cmd/chitin-orchestrator/schedule_argv_test.go`: assert known shapes (`schedule 096`, `schedule 096 --temporal-host x:y`, `schedule 096 --repo-root /abs/path`) parse correctly; unknown flags exit 1 with usage to stderr
- [ ] T012 [P] [US1] Spec-ref resolution unit tests in `go/orchestrator/cmd/chitin-orchestrator/specref_test.go`: cover exact match, numeric prefix unique, numeric prefix ambiguous (lists candidates sorted), slug-only match, no match (lists available)
- [ ] T013 [P] [US1] DAG validation unit tests in `go/orchestrator/cmd/chitin-orchestrator/validate_test.go`: cover all-valid DAG returns empty `ValidationResult`; `NeedsClarification` capability surfaces with `Kind: "needs_clarification"`; unroutable capability (registry has no declarer) surfaces with `Kind: "unroutable"`; empty DAG (zero tasks) returns valid (legitimate per Edge Cases)
- [ ] T014 [P] [US1] Chain-event emission test in `go/orchestrator/cmd/chitin-orchestrator/emit_test.go`: with a fake kernel binary on `$CHITIN_KERNEL_BIN`, assert `scheduler_started` event JSON written to its stdin has the spec'd shape (Entity 6); with the binary missing, assert `emitChainEvent` logs warn and returns nil
- [ ] T015 [US1] Integration test for schedule round-trip in `go/orchestrator/cmd/chitin-orchestrator/schedule_integration_test.go`: spin up Temporal dev server in a test container OR connect to a running local one (gated by `TEST_TEMPORAL_HOSTPORT` env), invoke `runMain([]string{"schedule", "097-fixture", "--repo-root", "../testdata"})`, assert exit 0, parse stdout for RunID, query Temporal `DescribeWorkflowExecution` and assert the workflow exists in Running state

### Implementation for User Story 1

- [ ] T016 [US1] Implement schedule handler `cmdSchedule(args []string) int` in `go/orchestrator/cmd/chitin-orchestrator/schedule.go`: parse argv → resolve repo root (T005) → resolve spec ref (T006) → compile DAG via `speckit.New().CompileSpec(repoRoot, specRef)` → validate (T009) → dial Temporal (T004) → `client.ExecuteWorkflow(ctx, StartWorkflowOptions{ID: uuid, TaskQueue}, "SchedulerWorkflow", SchedulerInput{...})` → emit `scheduler_started` (T007) → print success line → return ExitSuccess. Surface compile errors as ExitUserError; surface Temporal errors as ExitRuntimeError; chain emit failure logs warn but does not change exit code per D8.
- [ ] T017 [US1] Wire schedule's error rendering to the stable stderr messages from `contracts/schedule-subcommand.md` so operators can grep on them; cover: missing/ambiguous ref, missing tasks.md, malformed tasks.md, validation failures with the full per-node listing, Temporal unreachable
- [ ] T018 [US1] Wire the dispatcher in `main.go` (T003) to route `schedule` argv to `cmdSchedule` instead of the stub; remove the `not yet implemented` placeholder

**Checkpoint**: After this phase, `chitin-orchestrator schedule <ref>` works end-to-end against a real Temporal. The complementary `status` / `cancel` subcommands still return their `not yet implemented` stubs — a scheduled run can only be inspected via the Temporal UI until US2 lands.

---

## Phase 4: User Story 2 — Query and control a running scheduler run (Priority: P1) — MVP-half-2

**Goal**: An operator runs `chitin-orchestrator status` to see what's in flight, `status -run-id <id>` to inspect a single run, and `cancel -run-id <id>` to stop one cleanly. Per spec §US2 acceptance scenarios 1-5.

**Independent Test**: with a known scheduler run in flight (from US1's `schedule`), `status` lists it, `status -run-id <id>` returns matching `SchedulerStatus` JSON, `cancel -run-id <id> -reason "test"` cancels within one tick and emits a `scheduler_canceled` chain event with the reason.

### Tests for User Story 2

- [ ] T019 [P] [US2] Status list-mode test in `go/orchestrator/cmd/chitin-orchestrator/status_list_test.go`: stub the Temporal `ListWorkflow` response with two running workflows, assert JSON output is a sorted array of two entries with the spec'd shape (Entity 5); `--text` produces the fixed-column table; empty result produces `[]` and exit 0
- [ ] T020 [P] [US2] Status single-run test in `go/orchestrator/cmd/chitin-orchestrator/status_inspect_test.go`: stub `QueryWorkflow` to return a `SchedulerStatus`; assert JSON output matches verbatim; unknown run_id surfaces exit 1 with the spec'd stderr message
- [ ] T021 [P] [US2] Cancel happy-path test in `go/orchestrator/cmd/chitin-orchestrator/cancel_test.go`: stub `DescribeWorkflowExecution` to return Running, stub `CancelWorkflow` to succeed; assert exit 0, stdout contains the canceled line with reason, a `scheduler_canceled` chain event is emitted
- [ ] T022 [P] [US2] Cancel idempotency tests in `go/orchestrator/cmd/chitin-orchestrator/cancel_idempotency_test.go`: stub `DescribeWorkflowExecution` to return Completed → cancel exits 1 with `already in terminal state "Completed"` and no chain event; same for Canceled, Failed, Terminated, TimedOut
- [ ] T023 [US2] Integration test extending T015: in the schedule round-trip, after scheduling, call `cmdStatus(["status"])` and assert the new run is listed; call `cmdStatus(["status", "-run-id", runID])` and assert the SchedulerStatus shape; call `cmdCancel(["cancel", "-run-id", runID, "-reason", "test"])` and assert exit 0 + the workflow transitions to Canceled within 30s

### Implementation for User Story 2

- [ ] T024 [US2] Implement status handler `cmdStatus(args []string) int` in `go/orchestrator/cmd/chitin-orchestrator/status.go`: parse argv → dial Temporal (T004) → branch on `-run-id`: absent → `ListWorkflow` + per-workflow `Query(status)` for live tick/frontier → sort by started_at desc → render JSON (default) or table (`--text`); present → `QueryWorkflow(runID, "", "status")` and render unchanged. Unknown run_id → ExitUserError.
- [ ] T025 [US2] Implement cancel handler `cmdCancel(args []string) int` in `go/orchestrator/cmd/chitin-orchestrator/cancel.go`: parse argv → dial Temporal → `DescribeWorkflowExecution` to probe state → idempotent reject if terminal → `CancelWorkflow` → emit `scheduler_canceled` (T007) → print canceled line → return ExitSuccess. Chain emit failure logs warn but does not change exit code (D8).
- [ ] T026 [US2] Wire dispatcher routing for `status` and `cancel` in `main.go` (T003); remove the remaining `not yet implemented` stubs

**Checkpoint**: After this phase, all three subcommands work end-to-end. The full operator round-trip (`schedule` → `status` → `cancel`) succeeds against a real Temporal. The MVP is complete.

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: Operator-facing documentation, end-to-end verification against the live quickstart recipe, and one cross-cutting concern: validating spec 097 itself stays speckit-lint clean as the artifacts settle.

- [ ] T027 [P] Operator runbook at `docs/operator/scheduling.md` documenting the three subcommands, JSON/text output shapes, exit-code convention, chain event types; cite spec 097 and link to the three contract docs
- [ ] T028 [P] CHANGELOG entry for the next `chitin-orchestrator` release mentioning the three new subcommands and the two new chain event types; under `## Unreleased` or the equivalent release section
- [ ] T029 [P] Speckit-lint clean check on spec 097 — `chitin-kernel speckit-lint specs/097-operator-scheduler-entrypoint` from repo root; assert 0 findings; fail the polish phase if any new findings appeared during implementation
- [ ] T030 Run the quickstart.md verification recipe end-to-end against a real `chitin-orchestrator` build and a real Temporal dev server; record the wall-clock for schedule (assert <10s for SC-001), the cancel-honored latency (assert <30s for SC-004), and the status freshness (assert <5s for SC-003); attach the recorded numbers to the implementation PR body

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: starts immediately; no dependencies
- **Foundational (Phase 2)**: depends on Setup completion — blocks all user stories
- **User Story 1 (Phase 3)**: depends on Foundational; can start immediately once Phase 2 checkpoint is met
- **User Story 2 (Phase 4)**: depends on Foundational; CAN run in parallel with US1 (different files) BUT US2's integration test T023 depends on US1's `schedule` working, so the test order is US1 impl → US2 impl → US2 integration test
- **Polish (Phase 5)**: depends on all user stories being complete

### User Story Dependencies

- **US1 (P1)** has no dependency on US2; the schedule handler stands alone (operators inspect via Temporal UI if needed)
- **US2 (P1)** has no implementation dependency on US1 — but its integration test naturally needs a scheduled run to inspect/cancel, so it leverages US1's smoke test fixture

### Within Each User Story

- Tests in `*_test.go` should be added alongside their implementation files; the spec is not requesting strict TDD red-green-refactor but the implementation tasks reference the test files explicitly so they land together
- Argv parsing tests → spec-ref + DAG validation tests → handler integration test
- Implementation handlers depend on Foundational helpers (T004-T010); implementing them in parallel within a story is fine (different files)

### Parallel Opportunities

- **All Foundational tasks (T004-T009)** marked `[P]` can run concurrently — different files, no shared state
- **All US1 tests (T011-T014)** marked `[P]` can run concurrently
- **All US2 tests (T019-T022)** marked `[P]` can run concurrently
- **US1 implementation (T016) and US2 implementation (T024, T025)** are in different files and can be developed concurrently after Foundational completes
- **Polish docs and CHANGELOG (T027, T028, T029)** all `[P]` can be authored concurrently

---

## Parallel Example: User Story 1

```bash
# After Foundational completes, run all US1 tests in parallel:
go test -run TestSchedule_ArgvParsing  ./cmd/chitin-orchestrator/... &
go test -run TestSpecRef               ./cmd/chitin-orchestrator/... &
go test -run TestValidate              ./cmd/chitin-orchestrator/... &
go test -run TestEmitChainEvent        ./cmd/chitin-orchestrator/... &
wait

# Then implement the handler against those tests:
# T016 — schedule.go cmdSchedule
# T017 — stderr rendering
# T018 — dispatcher wiring
```

---

## Implementation Strategy

### MVP First (US1 + US2 together)

Both stories are P1; the CLI is incomplete with only one. The MVP is:

1. Complete Phase 1: Setup (testdata fixture, dispatcher scaffold)
2. Complete Phase 2: Foundational (all 7 helpers) — CRITICAL — blocks everything
3. Complete Phase 3: US1 (schedule subcommand works end-to-end)
4. Complete Phase 4: US2 (status + cancel subcommands work end-to-end)
5. **STOP and VALIDATE**: run the quickstart.md round-trip
6. Ship the MVP — spec 097's CLI is operator-usable

### Incremental Delivery (single-developer flow)

1. Setup + Foundational → no shippable artifact yet
2. + US1 → operators can schedule but inspect via Temporal UI; demo-able
3. + US2 → full round-trip; ship the MVP
4. + Polish → documentation + verified-recipe ship

### Parallel Team Strategy

If two developers staffed:

1. Both complete Setup + Foundational together (or split T004-T010 across developers)
2. Once Foundational is done:
   - Developer A: US1 (schedule)
   - Developer B: US2 (status + cancel)
3. Stories converge in the integration test (T023) which needs US1's schedule working

---

## Notes

- `[P]` tasks = different files, no dependencies
- `[Story]` label maps task to its user story for traceability
- Each user story is independently completable; both are P1 because the CLI surface is incomplete with only one
- Verify tests pass before declaring a task complete
- Commit after each task or logical group
- Stop at any checkpoint to validate independently
- Avoid: vague tasks, same-file conflicts, cross-story file dependencies
