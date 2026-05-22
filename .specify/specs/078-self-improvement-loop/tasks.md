# Tasks: Self-Improvement Loop

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md)

## Format: `[ID] [P?] [Story] Description`

- **[P]** = parallelizable (different files, no incomplete dependency)
- **[US1/US2/US3]** = the user story a task serves (story phases only)

## Path Conventions

Packages within the spec-070 orchestrator module — the loop package
`go/orchestrator/loop/`, the loop workflow `go/orchestrator/workflows/`,
the proposal-queue projection activity `go/orchestrator/activities/`.
Depends on spec 076's scheduler/DAG (`deterministic` nodes), spec 075's
driver registry (including the local-LLM driver), and the Chitin
Telemetry layer as the read surface.

---

## Phase 1: Setup (Shared Infrastructure)

- [ ] T001 Create the `go/orchestrator/loop/` package skeleton — `window.go`, `ingest.go`, `analysis.go`, `finding.go`, `proposal.go`, `category.go` with package doc and exported stubs (plan.md Project Structure)
- [ ] T002 Create the loop workflow file skeleton at `go/orchestrator/workflows/improvement_loop.go` — package, imports, workflow registration stub (FR-001)
- [ ] T003 [P] Wire `workflowcheck` against `go/orchestrator/workflows/improvement_loop.go` in the orchestrator CI determinism gate (FR-001, plan.md Constraints)

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ The window/finding/proposal types and the Gate-able Category set are pure (no Temporal import). They block every user story.**

- [ ] T004 Implement the Telemetry Window type in `go/orchestrator/loop/window.go` — checkpoint-bounded slice (previous checkpoint → cycle start) over the cross-layer source set: governance/chitin-chain decision log, orchestrator run history, CI outcomes, bench results, PR outcomes, agent run telemetry (FR-002, Key Entities: Telemetry Window)
- [ ] T005 Implement the Finding type in `go/orchestrator/loop/finding.go` — an analyzed observation (recurring failure / missed opportunity / regression) carrying the specific telemetry records that evidence it; a stable finding identity for duplicate and regression matching (FR-004, Key Entities: Finding)
- [ ] T006 Implement the Spec Proposal type in `go/orchestrator/loop/proposal.go` — a concrete, reviewable change against a *named* chitin spec, carrying its finding and evidence; never a vague suggestion (FR-003, FR-004, Key Entities: Spec Proposal)
- [ ] T007 Implement the closed Gate-able Category set in `go/orchestrator/loop/category.go` — code generation, PR review, review-against-deterministic-specs, review-against-deterministic-code, e2e test authoring, peer review of tests/code; a membership check synthesis calls to refuse out-of-set proposals (FR-007, Key Entities: Gate-able Category)
- [ ] T008 [P] Unit-test `go/orchestrator/loop/` types in `finding_test.go`, `proposal_test.go`, `category_test.go` — boundaries: empty window, two telemetry records of the identical finding (one finding, not two), a regression finding vs a fresh failure, a category-set membership reject (FR-002, FR-004, FR-007)

**Checkpoint**: the loop's pure core compiles and is tested — the workflow can now build on it.

## Phase 3: User Story 1 — Telemetry becomes a reviewable spec proposal (Priority: P1) 🎯 MVP

**Goal**: a single on-demand loop workflow runs the telemetry → analysis → finding → spec-proposal arc and emits exactly one evidence-backed proposal, queued for the operator and never applied — the loop's irreducible core.

**Independent test**: feed the workflow a fixed telemetry window containing a known recurring failure; confirm it emits exactly one spec proposal that names the failure, cites the grounding telemetry, is a concrete diff against a real spec, and is queued for the operator — never applied.

- [ ] T009 [P] [US1] Implement the per-layer telemetry-ingest activities in `go/orchestrator/loop/ingest.go` — one activity per source (governance decision log, run history, CI, bench, PR, agent telemetry) reading the Chitin Telemetry layer into a Telemetry Window; a missing or unreachable layer yields an empty contribution, never an error (FR-002, edge case: unreachable telemetry layer)
- [ ] T010 [P] [US1] Implement the analysis passes in `go/orchestrator/loop/analysis.go` — over a Telemetry Window, detect recurring failures and missed opportunities and emit Findings with their evidence records (FR-001, US1 acceptance scenario 1)
- [ ] T011 [US1] Implement the on-demand loop workflow in `go/orchestrator/workflows/improvement_loop.go` — telemetry-ingest activities → analysis → finding → proposal synthesis → enqueue; a durable, individually-inspectable workflow run (FR-001, FR-003)
- [ ] T012 [US1] Implement proposal-prose synthesis in the loop workflow — a frontier-agent step that turns a Finding into a concrete spec-proposal diff against a named spec, carrying the finding's evidence; refuse synthesis for any out-of-category target (FR-003, FR-007, FR-009; US1 acceptance scenario 2)
- [ ] T013 [US1] Implement the proposal-queue projection activity in `go/orchestrator/activities/proposal_queue.go` — enqueue each cycle's proposals for operator review, attributable to their cycle and finding; the queue is written, never read back to decide the next cycle (FR-013, 070 FR-016; US1 acceptance scenario 3)
- [ ] T014 [US1] Enforce the human gate in the loop workflow — the cycle ends at *queued*; nothing in code, policy, or configuration changes without explicit operator approval; an approved proposal is implemented only through the orchestrator + the spec-076 scheduler, never a side channel (FR-005, FR-006; US1 acceptance scenario 3; SC-002)
- [ ] T015 [US1] Implement duplicate suppression in `go/orchestrator/loop/finding.go` — a still-pending finding that recurs attaches new evidence to the existing pending proposal; the loop never re-queues a duplicate (FR-014; edge case: finding recurs while proposal un-reviewed; SC-006)
- [ ] T016 [US1] Implement the rejection record and the stale-spec rule — an operator rejection is recorded and the loop does not re-propose the identical change without new evidence; a proposal touching a superseded or missing spec is marked stale rather than emitted against a dead spec (FR-015; US1 acceptance scenario 4; edge cases: rejected proposal, dead spec)
- [ ] T017 [US1] Replay/determinism test for the loop workflow in `go/orchestrator/workflows/improvement_loop_test.go` — Temporal `testsuite`; a fixed window with a known recurring failure yields exactly one grounded proposal, queued and not applied (FR-001; SC-001, SC-002; US1 Independent Test)

**Checkpoint**: the loop closes once — telemetry becomes a single evidence-backed, operator-queued spec proposal. The MVP.

## Phase 4: User Story 2 — Review steps run deterministic, not frontier (Priority: P2)

**Goal**: every loop step with a mappable decision tree runs as a spec-076 `deterministic` node or a spec-075 small-model invocation; a frontier agent is invoked only for proposal-prose synthesis; the cycle's frontier-token cost is bounded to that one step.

**Independent test**: run a loop cycle whose work includes a PR-against-spec review and a telemetry anomaly scan; confirm each ran as a deterministic node or small-model invocation, no frontier agent was invoked for them, and frontier-token cost is the synthesis step alone.

- [ ] T018 [US2] Classify each loop step in `go/orchestrator/loop/analysis.go` — mappable review steps (PR-against-acceptance-criteria, code-against-deterministic-spec, telemetry anomaly scan, e2e test run) flagged for `deterministic`-node / small-model execution; only proposal-prose synthesis flagged frontier (FR-008, FR-009; US2 acceptance scenario 4)
- [ ] T019 [US2] Dispatch mappable review steps as spec-076 `deterministic` nodes from the loop workflow — a fully-mappable step MUST NOT be implemented as a frontier-agent invocation (FR-008; 076 FR-017; US2 acceptance scenarios 1–2)
- [ ] T020 [P] [US2] Route high-volume classification/review steps through the spec-075 local-LLM driver — the small-model tier for deterministic-ish work (FR-010; US2 acceptance scenario 1)
- [ ] T021 [US2] Implement the unmapped-input escalation path — a deterministic review step that meets an input outside its mapped cases escalates that single case to a frontier agent, never guesses, never fails the whole cycle; if the small model is unavailable, fall back to a deterministic activity or defer to the next cycle, never silently route to a frontier agent (edge cases: unmapped input, small model unavailable)
- [ ] T022 [P] [US2] Frontier-cost-accounting test in `go/orchestrator/workflows/improvement_loop_test.go` — a cycle with a PR-against-spec review and a telemetry anomaly scan: each ran deterministic/small-model, zero frontier invocations for them, frontier-token cost is the synthesis step alone (FR-008, FR-009; SC-003, SC-004; US2 Independent Test)

**Checkpoint**: the loop is affordable — review and analysis cost zero frontier tokens; a one-shot becomes a sustainable loop.

## Phase 5: User Story 3 — The loop runs continuously on a schedule (Priority: P3)

**Goal**: a scheduled loop workflow fires a cycle on a cadence; each cycle ingests only telemetry since the prior checkpoint, advances the checkpoint on completion, never overlaps the prior cycle, and detects regressions in previously-approved-and-implemented proposals.

**Independent test**: schedule the loop on a short cadence; let it run several cycles over a moving window; confirm each ingests only telemetry since the prior checkpoint, queues its proposals, and that cycles neither overlap nor skip a window.

- [ ] T023 [US3] Implement the Cycle Checkpoint in `go/orchestrator/loop/window.go` and the loop workflow — each cycle ingests exactly the telemetry since the previous cycle's checkpoint and advances the checkpoint on completion, including for an empty cycle (FR-011; US3 acceptance scenarios 1, 4; edge case: empty window advances checkpoint)
- [ ] T024 [US3] Make the loop workflow schedulable on a cadence and add Continue-As-New — bound the always-on workflow's history, carry forward the checkpoint and pending state (FR-011; plan.md Constraints)
- [ ] T025 [US3] Implement the no-overlap guard — a cadence firing while the prior cycle still runs waits for it to complete rather than double-ingesting a window (FR-012; US3 acceptance scenario 3)
- [ ] T026 [US3] Implement regression detection in `go/orchestrator/loop/analysis.go` — detect when a previously-approved-and-implemented proposal's intent later fails in new telemetry and emit a follow-up proposal; the loop closes on its own prior output (FR-016; SC-008; edge case: approved fix regresses)
- [ ] T027 [P] [US3] Continuous-operation test in `go/orchestrator/workflows/improvement_loop_test.go` — several scheduled cycles over a moving window: no overlap, no skipped window, an empty cycle records and advances its checkpoint, a queued regression follows a regressing telemetry record within one cycle (FR-011, FR-012, FR-016; SC-005, SC-008; US3 Independent Test)

**Checkpoint**: the loop runs itself — a continuous cadence, no overlap, no skipped window, closing on its own regressions.

## Phase 6: Polish & Cross-Cutting

- [ ] T028 [US1] Re-express Sentinel (spec 064) as one configuration of the loop — Sentinel's ingest → analyze → mine-governance-policy-proposals arc becomes a configured instance, not a parallel system; retire the Sentinel-only pipeline once the configured instance is proven (FR-018; SC-007)
- [ ] T029 [P] Emit per-cycle self-telemetry from the loop workflow to the Chitin Telemetry layer — window ingested, findings, proposals, deterministic-vs-frontier step accounting — so the loop is itself observable and itself an input to a later cycle (FR-017; 070 FR-008)
- [ ] T030 [P] Confirm `workflowcheck` passes on `go/orchestrator/workflows/improvement_loop.go` — the determinism gate is green (plan.md Constraints)
- [ ] T031 Re-run the Constitution Check — all six principles still PASS post-implementation

---

## Dependencies

- **Phase 1 → Phase 2 → Phase 3**: Setup and the pure window/finding/proposal core block all stories.
- **Phase 2 (the pure core)** is the hard prerequisite — the Finding/Proposal types and the Gate-able Category set must exist and be tested before any workflow builds on them.
- **US1 (P1)** is the MVP — independently shippable once Phases 1+2 are done; depends on the Chitin Telemetry layer as a read surface and on the orchestrator's worker host (spec 070).
- **US2 (P2)** depends on Phase 3 (the running loop workflow); it changes *how* loop steps run, not *whether* the loop closes — it needs spec 076's `deterministic` nodes and spec 075's local-LLM driver.
- **US3 (P3)** depends on Phase 3 (the loop workflow); the schedule, checkpoint, and Continue-As-New extend it.
- Within a story: types/library before workflow; workflow before its replay test; the configured-Sentinel cutover after the loop is proven.

## Parallel Execution Examples

- Phase 1: T003 in parallel with T001/T002 (distinct concern — CI wiring).
- Phase 2: T008 follows T004–T007 but runs alongside no incomplete dependency once they land.
- Phase 3: T009 and T010 in parallel — distinct files (`ingest.go`, `analysis.go`).
- Phase 4: T020 and T022 in parallel — distinct concerns/files.
- Phase 6: T029 and T030 in parallel — distinct concerns/files.

## Implementation Strategy

**MVP = US1 (telemetry becomes a reviewable spec proposal).** Phase 1 +
Phase 2 + Phase 3 deliver the loop's irreducible core — one on-demand
cycle that turns a telemetry window into a single evidence-backed,
operator-queued spec proposal, never applied. That alone closes the loop
and proves the thesis. US2 (the deterministic tier) makes the loop
affordable enough to run continuously; US3 makes it always-on; Phase 6
folds Sentinel into the loop as one configured instance — each increment
adds value without breaking the prior one.
