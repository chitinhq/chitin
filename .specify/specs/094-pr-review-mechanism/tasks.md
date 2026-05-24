# Tasks: PR Review Mechanism (Spec 094)

**Input**: spec.md + plan.md + research.md + data-model.md + contracts/* + quickstart.md in `.specify/specs/094-pr-review-mechanism/`

**Prerequisites**: spec.md (✅ ratified), plan.md (✅ this directory), research.md (✅), data-model.md (✅), contracts/ (✅ five files), quickstart.md (✅)

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Maps to spec User Story (`US1`..`US5`) or to a foundational/setup/polish task (no story label)
- Paths shown are relative to the repo root and follow `plan.md` Project Structure.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Branch + worktree creation, package skeleton, no business logic yet.

- [ ] T001 Create `feat/094-pr-review-mechanism` worktree at `/tmp/chitin-094-review` from `origin/main` (constitution §2)
- [ ] T002 [P] Create empty package directory `go/orchestrator/activities/review/` with a package declaration in `go/orchestrator/activities/review/doc.go`
- [ ] T003 [P] Create empty package directory `go/orchestrator/activities/review/verdict/` with a package declaration in `go/orchestrator/activities/review/verdict/doc.go`
- [ ] T004 [P] Confirm Temporal SDK + `gh` shell-out helpers from spec 093 are importable from the new packages (sanity-import test in `go/orchestrator/activities/review/doc_test.go`)

**Checkpoint**: Worktree exists, packages compile empty, ready for type definitions.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Pure-data types, validators, aggregators, and substrate extensions (spec 075 / 076). MUST complete before any user-story workflow task.

**⚠️ CRITICAL**: No user story work begins until this phase is complete.

### Verdict types + invariants (FR-013, FR-014)

- [ ] T005 [P] Define `StructuredVerdict` struct + `VerdictEnum` constants in `go/orchestrator/activities/review/verdict/verdict.go`
- [ ] T006 [P] Implement `Validate(v StructuredVerdict) error` enforcing FR-014 invariants 1–4 in `go/orchestrator/activities/review/verdict/invariants.go`
- [ ] T007 [P] Table-driven tests for `Validate()` covering all four enum values × valid + each FR-014 invariant violation in `go/orchestrator/activities/review/verdict/invariants_test.go`

### Outcome + aggregation (R-AGG, FR-009 → FR-012)

- [ ] T008 [P] Define `ReviewerOutcome`, `FailureReason`, `FailureKind`, `Role` in `go/orchestrator/activities/review/verdict/outcome.go` with `IsApproveShaped()` / `IsRequestChanges()` / `IsFailure()` predicates
- [ ] T009 Implement `Aggregate(p1, p2 ReviewerOutcome, arbiter *ReviewerOutcome) ReviewGateDecision` in `go/orchestrator/activities/review/verdict/aggregate.go` (depends on T005, T008)
- [ ] T010 Table-driven tests for `Aggregate()` covering the full cartesian product (4 enum × 4 enum × {nil, 4 arbiter outcomes, failed arbiter}) in `go/orchestrator/activities/review/verdict/aggregate_test.go`

### Workflow + activity I/O types (data-model.md)

- [ ] T011 [P] Define `PRReviewInput`, `ReviewGateDecision`, `GateState`, `ArbiterType` in `go/orchestrator/workflows/pr_review_types.go`
- [ ] T012 [P] Define `PRSnapshot`, `PRFile`, `SpecArtifact` (+ content-hash helpers) in `go/orchestrator/workflows/pr_review_snapshot.go`
- [ ] T013 [P] Define `ReviewerInvocation`, `ReviewerSlate`, `DriverID` in `go/orchestrator/activities/review/types.go`

### Driver-registry extension (spec 075 additive — R-AUTHORID supporting)

- [ ] T014 [P] Add `CapabilityReviewer` constant to `go/orchestrator/registry/capability.go`
- [ ] T015 [P] Add `ReviewMode` struct + `DriverEntry.ReviewMode *ReviewMode` field to `go/orchestrator/registry/driver.go` (additive — existing callers unchanged)
- [ ] T016 Add `Registry.LookupByGitIdentity(gitIdentity string) (DriverID, bool)` method in `go/orchestrator/registry/registry.go` (depends on existing `git_identity` field; if absent, add `git_identity` as a sibling additive change)
- [ ] T017 Tests for registry extensions: capability tag enumerable, ReviewMode marshalling, LookupByGitIdentity hit + miss cases in `go/orchestrator/registry/registry_test.go`

### SelectDriver extension (spec 076 additive — R-SEL)

- [ ] T018 Add `RequireCapability Capability` and `ExcludeIdentities []DriverID` fields to `SelectDriverInput` in `go/orchestrator/selectdriver/select_driver.go`
- [ ] T019 Implement the capability filter and identity exclusion in the existing `SelectDriver` activity body; preserve current behaviour when both fields are zero-valued
- [ ] T020 Tests for extended `SelectDriver`: capability filter narrows pool, exclude list removes the right ID, both compose correctly, existing callers' behaviour unchanged in `go/orchestrator/selectdriver/select_driver_test.go`

### PRReviewWorkflow skeleton (no logic yet)

- [ ] T021 Create `PRReviewWorkflow` function skeleton in `go/orchestrator/workflows/pr_review.go` with the input/output signature from T011 but a body that returns `Halted, "skeleton"` for now
- [ ] T022 Register `PRReviewWorkflow` in `go/orchestrator/cmd/chitin-orchestrator/main.go` worker registration
- [ ] T023 Testsuite-bootstrapped workflow test in `go/orchestrator/workflows/pr_review_test.go` that just confirms the skeleton runs end-to-end and returns the placeholder decision (smoke test for worker registration)

**Checkpoint**: Verdict math, registry/SelectDriver extensions, and the workflow skeleton are all in place. User-story phases can now begin in parallel.

---

## Phase 3: User Story 1 — Happy path, both primaries approve (Priority: P1) 🎯 MVP

**Goal**: Implement the dialectic happy path — two primaries dispatched in parallel; if both return any approve-shaped verdict, the gate passes without arbiter and the merge proceeds.

**Independent Test**: Recipe SC-001 in quickstart.md — submit a small clean spec-only PR through the merge queue, observe two reviewer invocations both approve-shaped, no arbiter, merge happens.

### Tests for User Story 1 (TDD — write first, ensure failing)

- [ ] T024 [P] [US1] Testsuite test `TestHappyPath_BothApprove` in `go/orchestrator/workflows/pr_review_test.go` — fake activities return two `approve` verdicts; assert gate=passed, arbiter not engaged, exactly 2 invocations.
- [ ] T025 [P] [US1] Testsuite test `TestHappyPath_ApproveAndApproveWithComments` — one returns `approve`, one returns `approve-with-comments`; assert gate=passed, arbiter not engaged.
- [ ] T026 [P] [US1] Testsuite test `TestHappyPath_ParallelDispatch` — assert the two `dispatch_machine_reviewer` invocations have their `started_at` within 1s of each other (SC-009 invariant) using mock-recorded timestamps.
- [ ] T027 [P] [US1] Activity-level test for `emit_review_telemetry` — given an outcome with content, assert the emitted OTLP event has correct driver_id, role, content hashes, elapsed in `go/orchestrator/activities/review/emit_review_telemetry_test.go`.

### Implementation for User Story 1

- [ ] T028 [P] [US1] Implement `select_reviewers.go` activity in `go/orchestrator/activities/review/select_reviewers.go` — calls `SelectDriver` with `RequireCapability=reviewer` and exclusion list; constructs `ReviewerSlate`; halts on shortfall with named-counts reason.
- [ ] T029 [P] [US1] Implement `dispatch_machine_reviewer.go` activity in `go/orchestrator/activities/review/dispatch_machine_reviewer.go` — invokes driver's `review` tool with PRSnapshot input per `contracts/review-mode-driver-contract.md`; parses JSON; calls `verdict.Validate()`; returns `ReviewerOutcome` (verdict-or-failure shape).
- [ ] T030 [P] [US1] Implement `emit_review_telemetry.go` activity in `go/orchestrator/activities/review/emit_review_telemetry.go` — emits the FR-032 OTLP event with content hashes (not raw text).
- [ ] T031 [US1] Wire happy-path logic into `PRReviewWorkflow` in `go/orchestrator/workflows/pr_review.go` (depends on T028, T029, T030): capture snapshot, select reviewers, dispatch both primaries via `workflow.Go` for parallelism, wait both, aggregate via `verdict.Aggregate()`, emit telemetry per invocation, return decision.
- [ ] T032 [US1] Run T024–T026 against the live workflow; iterate until all three pass. Confirm SC-009 parallel-dispatch claim holds in the test recorder.

**Checkpoint**: SC-001 passes end-to-end on stub-driver fixtures. MVP merge gate works for the dominant case.

---

## Phase 4: User Story 2 — Class-routed arbiter on disagreement (Priority: P1)

**Goal**: When primaries return mixed verdicts (approve-shaped + request-changes, or any combination requiring arbitration), dispatch the arbiter per `arbiter_type`: operator (governance + spec-only) or third machine driver (impl/live-fix/bookkeeping/research-docs).

**Independent Test**: Recipe SC-002 in quickstart.md — submit a spec-only PR with fixture-injected mixed primaries, observe operator-arbiter prompt comment + Discord notify, reply with valid YAML verdict, observe arbiter outcome drives gate.

### Tests for User Story 2

- [ ] T033 [P] [US2] Testsuite test `TestDisagreement_OperatorArbiter_Approves` — primaries mixed, class=spec-only → activity dispatches operator arbiter (fake activity returns approve); assert gate=passed via arbiter, 3 invocations recorded.
- [ ] T034 [P] [US2] Testsuite test `TestDisagreement_OperatorArbiter_RequestChanges` — primaries mixed, class=governance, operator arbiter returns request-changes → assert gate=blocked with reason naming arbiter blockers.
- [ ] T035 [P] [US2] Testsuite test `TestDisagreement_MachineArbiter_DriverNotPrimaryNotAuthor` — primaries mixed, class=impl with arbiter_type=machine, simulated third driver in pool → assert dispatched arbiter ID is neither primary nor author.
- [ ] T036 [P] [US2] Testsuite test `TestDisagreement_MachineArbiter_PoolExhausted` — same as T035 but pool has no third driver → assert workflow halts with reason "arbiter pool exhausted".
- [ ] T037 [P] [US2] Testsuite test `TestApproveAndApproveWithComments_NotDisagreement` — explicit assertion that the two approve-shaped variants do NOT count as disagreement (spec.md Edge Case).
- [ ] T038 [P] [US2] Activity-level test for `dispatch_operator_arbiter.go` — mock GitHub comment APIs; assert prompt comment is posted with the template from `contracts/operator-arbiter-surface.md`; assert YAML parser accepts valid replies and rejects invalid ones with follow-up.

### Implementation for User Story 2

- [ ] T039 [P] [US2] Implement `dispatch_operator_arbiter.go` activity in `go/orchestrator/activities/review/dispatch_operator_arbiter.go` — posts prompt comment, polls for operator reply, parses + validates YAML, returns `StructuredVerdict`. Honors FR-027 (no time bound) and uses `activity.RecordHeartbeat` every 30s.
- [ ] T040 [P] [US2] Implement `notify_operator.go` activity in `go/orchestrator/activities/review/notify_operator.go` — Discord notify via spec 080 with notification.kind in `{operator-arbiter.dispatch, operator-arbiter.re-notify}`.
- [ ] T041 [US2] Extend `PRReviewWorkflow` arbiter branch (depends on T039, T040): if `arbiter_type=operator`, dispatch operator arbiter + notify; if `arbiter_type=machine`, dispatch a third reviewer driver. On (mixed primaries OR any failure on either primary OR any abstain), engage arbiter.
- [ ] T042 [US2] Wire 2-hour workflow-level heartbeat timer per R-HEARTBEAT: `workflow.NewTimer(2h)` loops; on fire, calls `notify_operator` with `operator-arbiter.re-notify` kind when the workflow is waiting on operator arbiter.
- [ ] T043 [US2] Run T033–T038 against the live workflow; iterate until green.

**Checkpoint**: SC-002 + SC-003 (machine variant operationally degenerate per Assumptions) pass on fixtures. Disagreement → arbiter routing is correct per class.

---

## Phase 5: User Story 3 — Both reject (halt without arbiter) + signals (Priority: P2)

**Goal**: When both primaries return `request-changes`, halt immediately without arbiter. Implement `re-review` and `override-review` signals on the parent workflow with the class-gated rejection invariant.

**Independent Test**: Recipe SC-004 + SC-011 + SC-012 + SC-006 in quickstart.md.

### Tests for User Story 3

- [ ] T044 [P] [US3] Testsuite test `TestBothReject_NoArbiter` — both primaries return request-changes; assert gate=blocked with reason "both primaries request-changes", arbiter NOT dispatched, exactly 2 invocations.
- [ ] T045 [P] [US3] Testsuite test `TestReReviewSignal_SpawnsFreshChild` — workflow blocked → parent receives `re-review` signal → in-flight child (none) skipped → fresh `PRReviewWorkflow` spawned with new snapshot; both old and new invocations preserved in history.
- [ ] T046 [P] [US3] Testsuite test `TestReReviewSignal_DebouncedDuringInFlight` — workflow in-flight on a fresh review → second `re-review` signal arrives → asserted dropped (logged in telemetry, no new child).
- [ ] T047 [P] [US3] Testsuite test `TestOverrideReview_NonGovernance_Accepts` — spec-only PR blocked → `override-review` signal with reason → assert gate marked `passed (override)`, telemetry records both original blocking verdicts AND the override-review.accepted event.
- [ ] T048 [P] [US3] Testsuite test `TestOverrideReview_Governance_Rejects` — governance PR blocked → `override-review` signal → assert structured error returned to sender, gate state unchanged, override-review.rejected emitted.
- [ ] T049 [P] [US3] Testsuite test `TestOverrideReview_MissingReason_Rejects` — non-governance PR blocked → `override-review` signal with empty reason → assert rejected.

### Implementation for User Story 3

- [ ] T050 [US3] Extend `PRReviewWorkflow` aggregator path to short-circuit (request-changes, request-changes) to `GateBlocked` without arbiter dispatch (verify `verdict.Aggregate()` from T009 covers this; if so, this is integration-only).
- [ ] T051 [US3] In spec 093's `go/orchestrator/workflows/pr_merge.go`, add `re-review` signal handler per `contracts/workflow-signal-schemas.md` (depends on spec 093 v1.0.0 existing or being co-developed): cancel in-flight child, spawn fresh `PRReviewWorkflow` with fresh snapshot; debounce duplicate signals received in same workflow tick.
- [ ] T052 [US3] In spec 093's `pr_merge.go`, add `override-review` signal handler with class-gated rejection (governance), required-reason check, and `passed (override)` gate marker.
- [ ] T053 [US3] Emit `re-review.accepted`, `re-review.dropped`, `override-review.accepted`, `override-review.rejected` telemetry events via `emit_review_telemetry` (extend the activity with a `signal_event` mode or add a sibling `emit_signal_telemetry` activity).
- [ ] T054 [US3] Run T044–T049 against the live workflow.

**Checkpoint**: SC-004 + SC-006 + SC-011 + SC-012 all pass. Halt and recovery paths are correct.

---

## Phase 6: User Story 4 — No-self-review + pool shortfall (Priority: P2)

**Goal**: Wire the author exclusion through the selection activity; halt at selection on pool shortfall with named counts.

**Independent Test**: Recipe SC-005 + Acceptance Scenarios 4.1–4.4.

### Tests for User Story 4

- [ ] T055 [P] [US4] Testsuite test `TestAuthorExcluded_DriverAuthor` — PR author maps to a reviewer-tagged driver → assert that driver appears in zero reviewer invocations.
- [ ] T056 [P] [US4] Testsuite test `TestPoolShortfall_HaltsAtSelection` — v1 pool of 2 reviewer drivers + author is one of them → eligible pool is 1 < required 2 → assert workflow halts at selection with reason naming `(need 2, have 1 after exclusions)`.
- [ ] T057 [P] [US4] Testsuite test `TestUnmappedAuthor_NoExclusion` — PR author identifier maps to no driver (e.g., operator-authored) → assert full pool eligible, no exclusion applied.
- [ ] T058 [P] [US4] Testsuite test `TestArbiterPoolExhausted_NamedShortfall` — when arbiter required and only 2 reviewer-tagged drivers post-exclusion → assert halt with reason "arbiter pool exhausted" (US2's T036 covered the machine arbiter case; this is the broader shortfall variant).

### Implementation for User Story 4

- [ ] T059 [US4] In `select_reviewers.go` (from T028), call `Registry.LookupByGitIdentity(pr.Author)`; thread result into `excludeIdentities` passed to `SelectDriver`; record `ExcludedAuthor` in `ReviewerSlate` for telemetry attribution.
- [ ] T060 [US4] Add the named-counts shortfall error from `select_reviewers.go` when `len(EligibleAfterExclusion) < required` for the current role being filled. Workflow caller maps the error to `GateHalted` with the named reason.
- [ ] T061 [US4] Run T055–T058 against the live workflow.

**Checkpoint**: SC-005 + the no-self-review invariant are end-to-end verified. Pool shortfalls fail loudly with clear telemetry.

---

## Phase 7: User Story 5 — Single reviewer failure (Priority: P3)

**Goal**: A single primary failure (timeout, error, malformed verdict) does not block the gate — it triggers arbiter dispatch. Both primaries failing halts at arbiter dispatch.

**Independent Test**: Recipe SC-007 in quickstart.md.

### Tests for User Story 5

- [ ] T062 [P] [US5] Testsuite test `TestPrimaryTimeout_DispatchesArbiter` — one primary returns `FailureTimeout`, surviving primary returns `approve` → assert arbiter dispatched (treating missing as undecidable); arbiter verdict drives gate.
- [ ] T063 [P] [US5] Testsuite test `TestPrimaryMalformedVerdict_DispatchesArbiter` — one primary returns malformed JSON → asserts `FailureMalformedJSON` recorded; arbiter dispatched.
- [ ] T064 [P] [US5] Testsuite test `TestPrimaryMalformedShape_DispatchesArbiter` — one primary returns valid JSON that fails FR-014 invariants → asserts `FailureMalformedShape`; arbiter dispatched.
- [ ] T065 [P] [US5] Testsuite test `TestBothPrimariesFail_HaltsAtArbiterBoundary` — both primaries timeout → assert workflow halts at arbiter dispatch with reason "all primaries failed", arbiter NOT dispatched, operator escalation surfaced.
- [ ] T066 [P] [US5] Testsuite test `TestAggregator_FailedTreatedAsUndecidable` — pure aggregator test (no workflow) that (`approve`, `FailureTimeout`) and (`request-changes`, `FailureTimeout`) both trigger arbiter case (extension of T010).

### Implementation for User Story 5

- [ ] T067 [US5] In `dispatch_machine_reviewer.go` (from T029), ensure all five failure kinds (timeout, error, malformed_json, malformed_shape, cancelled) are returned as `ReviewerOutcome{Failure: ...}` without panicking the activity.
- [ ] T068 [US5] In `PRReviewWorkflow`, treat a failed `ReviewerOutcome` from a primary as "needs arbitration" per FR-009's "any other combination" clause. Both-failed halts at arbiter dispatch boundary with the named reason.
- [ ] T069 [US5] Run T062–T066 against the live workflow.

**Checkpoint**: SC-007 passes. Driver flakiness is no longer a single point of failure.

---

## Phase 8: Spec 093 v1.1.0 amendment integration

**Purpose**: Apply the policy-table amendment per `contracts/policy-table-amendment.md`. This is a separate PR scoped to spec 093 v1.1.0 — listed here because it's required for spec 094's workflow to be wired into production. May be implemented in parallel with US1–US5 by a different developer.

- [ ] T070 [P] In `.specify/specs/093-merge-queue-orchestrator/contracts/policy-table.md`, add `review_required` (bool) and `arbiter_type` (enum) columns per amendment doc; set per-class values to the v1.1.0 defaults.
- [ ] T071 [P] In `go/orchestrator/activities/merge/policy/policy_table.go`, add `ReviewRequired bool` and `ArbiterType ArbiterType` struct fields and the load-time `Validate()` invariant check (governance + spec-only MUST be operator arbiter).
- [ ] T072 In `go/orchestrator/workflows/pr_merge.go`, after classification, branch on `class.ReviewRequired`: if true, spawn `PRReviewWorkflow` as a child with `class.ArbiterType` in the input; await `ReviewGateDecision`; halt/proceed accordingly. Run concurrently with the existing wait-for-checks step (FR-036).
- [ ] T073 Tests in `policy_table_test.go` covering the new columns: governance + spec-only constraints, default migration values for v1.0.0 tables, malformed configurations rejected at load.
- [ ] T074 Tests in `pr_merge_test.go` (spec 093's existing test file, extended) covering: `ReviewRequired=true` spawns the child and waits; `ReviewRequired=false` skips review; child's `ReviewGateDecision` shape consumed correctly.

**Checkpoint**: Spec 093 v1.1.0 amendment is testable in isolation. When merged, spec 094's workflow is wired into the merge orchestrator end-to-end.

---

## Phase 9: Polish & Cross-cutting

- [ ] T075 Register `dispatch_machine_reviewer`, `dispatch_operator_arbiter`, `select_reviewers`, `emit_review_telemetry`, `notify_operator` activities in `cmd/chitin-orchestrator/main.go` worker initialization (extension of T022).
- [ ] T076 [P] Add `hermes` driver registry entry under `.specify/registry/drivers.yaml` (or equivalent) declaring `capabilities: [reviewer]` and a `review_mode` block; `prompt_template` content can be a TODO stub for the driver-team to populate (per FR-003).
- [ ] T077 [P] Add `openclaw` driver registry entry — same shape as T076.
- [ ] T078 Run speckit-lint (`chitin-kernel speckit-lint .specify/specs/094-pr-review-mechanism`) and confirm 0 findings. Re-lint after every artifact change.
- [ ] T079 Run quickstart smoke (Recipes SC-001 + SC-006) against the production worker on a real test PR. Capture telemetry trace as evidence.
- [ ] T080 [P] Update `.specify/specs/INDEX.md` to mark spec 094 as `Status: Ratified` and bump the registry's "current active feature" pointer past 094.
- [ ] T081 Open PR `feat/094-pr-review-mechanism` on the worktree branch with all spec artifacts + Go implementation. Body cites the 12 SC verifications. Bootstrap merge uses the constitution §7 ad-hoc carve-out (operator-driven multi-agent ratification, same pattern as spec 092).

---

## Dependencies

```
Phase 1 (Setup, T001-T004)
   └─ Phase 2 (Foundational, T005-T023)
         ├─ Phase 3 (US1, T024-T032)   [P1 — MVP]
         ├─ Phase 4 (US2, T033-T043)   [P1]   (depends on US1's dispatch + workflow skeleton)
         ├─ Phase 5 (US3, T044-T054)   [P2]   (depends on US1 + US2)
         ├─ Phase 6 (US4, T055-T061)   [P2]   (depends on US1; can parallel with US3)
         ├─ Phase 7 (US5, T062-T069)   [P3]   (depends on US1; can parallel with US3 + US4)
         └─ Phase 8 (Spec 093 v1.1.0 integration, T070-T074)   (depends on Phase 2; can begin in parallel with US1–US5)
               └─ Phase 9 (Polish, T075-T081)
```

### Parallelism notes

- **Within Phase 2**: T005–T013 are all parallel ([P] tasks) — they touch distinct files in distinct packages. T009 and T010 depend on T005 + T008. T014–T020 are independent of T005–T013 — different sub-package, can be done by a separate developer.
- **Phases 3–7**: US1 is the MVP; ship it before any of US2–US5. Once US1's workflow skeleton is in place, US2–US5 can be developed in parallel by different developers (each touches different aggregator branches, signal handlers, or fault paths).
- **Phase 8 (spec 093 amendment)**: Can begin as soon as Phase 2 completes — does NOT depend on US1–US5 because the spec 093 changes are policy-table + workflow-spawn glue, not review-workflow internals.

---

## Implementation Strategy

### MVP First (US1 only)

1. Phase 1 (worktree + packages) — half a day.
2. Phase 2 (foundational types, validators, registry/SelectDriver extensions) — 1–2 days.
3. Phase 3 (US1 happy path) — 1 day.
4. **STOP**: validate Recipe SC-001 end-to-end on a fixture-driven PR. The dialectic happy path is the ~80% case.
5. If shipping just US1 as a slice, the workflow is operational for clean PRs and blocks loudly on anything else — acceptable as v1.0-slice if needed.

### Incremental delivery

- US1 (MVP) → US2 (arbiter routing, formalizes spec 092's operator-arbiter pattern) → US3 (signals) → US4 (no-self-review) → US5 (failure isolation) → Phase 8 (spec 093 integration) → Phase 9 (polish + PR).
- Each user story is independently testable via its own testsuite tests; the workflow's parent-side wiring shifts as each phase adds branches.

### Parallel team strategy

With multiple developers:

- Dev A: Phase 2 (T005–T013), then US1 (Phase 3).
- Dev B: Phase 2 (T014–T020), then US2 (Phase 4).
- Dev C: Phase 8 (spec 093 amendment) in parallel with US1.
- After US1 ships: Dev D picks US3, Dev A continues to US4, Dev B continues to US5.

---

## Format check

All 81 tasks above follow `- [ ] [TID] [P?] [Story?] description with file path`. Setup/Foundational/Polish phases carry no `[Story]` label; user-story phases all carry `[US1]`..`[US5]`. Parallel-capable tasks are marked `[P]`.

## Total counts

- Total tasks: **81** (T001–T081)
- Setup (Phase 1): 4
- Foundational (Phase 2): 19 (T005–T023)
- US1 (Phase 3, P1 MVP): 9 (T024–T032)
- US2 (Phase 4, P1): 11 (T033–T043)
- US3 (Phase 5, P2): 11 (T044–T054)
- US4 (Phase 6, P2): 7 (T055–T061)
- US5 (Phase 7, P3): 8 (T062–T069)
- Spec 093 v1.1.0 (Phase 8): 5 (T070–T074)
- Polish (Phase 9): 7 (T075–T081)

## Independent test criteria

| Story | Independent test | Quickstart recipe |
|---|---|---|
| US1 | Submit small clean spec-only PR → both primaries approve → merge happens without arbiter | SC-001 |
| US2 | Submit PR with fixture-injected mixed primary verdicts → operator/machine arbiter dispatched per class | SC-002, SC-003 |
| US3 | Submit PR yielding two request-changes → workflow halts without arbiter; re-review respawns; override on non-governance proceeds; override on governance rejected | SC-004, SC-006, SC-011, SC-012 |
| US4 | Submit PR authored by `hermes` → `hermes` excluded from selection; with 2-driver v1 pool the workflow halts at selection with named-counts shortfall | SC-005 |
| US5 | Configure one driver to timeout → surviving primary + arbiter drive the gate; both-timeout halts at arbiter dispatch | SC-007 |

## Suggested MVP scope

US1 only (Phase 1 + Phase 2 + Phase 3, 32 tasks). Yields a workflow that lands clean PRs end-to-end with full audit + telemetry; everything else falls back to operator escalation. Acceptable v1.0-slice if spec 094 needs to land before US2–US5 are ready.
