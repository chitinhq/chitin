# Feature Specification: Driver Review-Mode Dispatch

**Feature Branch**: `spec/104-driver-review-mode-dispatch`

**Created**: 2026-05-24

**Status**: Draft

**Input**: User description: "Closes the `DispatchMachineReviewer.Execute` stub that surfaced during the 2026-05-24 dogfood run of spec 094 (PR Review Mechanism). After the 4-PR stack (#953 #954 #957 #958 #959) landed and the worker was redeployed, `chitin-orchestrator pr-review 948` ran the full dialectic infrastructure end-to-end: snapshot captured (863 KB, under the 1.5 MiB cap added in #959), 3 reviewer-tagged drivers selected (claudecode, codex, copilot), 2 primaries + arbiter dispatched in parallel, verdicts aggregated, decision returned. Workflow `3a054a9c-17dd-4ef7-a527-faf04bf57dbb` completed in 5.84s. But every reviewer invocation returned the stub `'TODO: real driver dispatch not wired in this slice (Phase 2 foundational)'` from `go/orchestrator/activities/review/dispatch_machine_reviewer.go:103-114`, because the activity does not actually invoke `d.Invoke(ctx, WorkUnit{...})`. This spec wires the real dispatch path so the dialectic gate produces real verdicts on PRs."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Real review verdicts on a single PR (Priority: P1)

The operator runs `chitin-orchestrator pr-review <PR#>`. The workflow captures the PR snapshot, selects the reviewer slate, and dispatches each primary + (on disagreement) the arbiter via `DispatchMachineReviewer`. Each dispatched driver receives a `WorkUnit` whose `Tool` field is set to the driver's declared `review_mode.tool_name`, whose `Context` is the snapshot JSON per `.specify/specs/094-pr-review-mechanism/contracts/review-mode-driver-contract.md`, and whose deadline is the workflow's remaining time. The driver returns a `Result` whose `Explanation` field is a JSON-encoded `StructuredVerdict`. The activity parses, validates per `verdict.Validate`, and translates to `verdict.Outcome` (success or failure-kind). The workflow aggregates and returns a real decision.

**Why this priority**: Without real dispatch, the dialectic gate produces only stub failures — every PR halts. This is the load-bearing missing leg.

**Independent Test**: `chitin-orchestrator pr-review 948 --policy-class impl --arbiter machine` against a real merged PR. After workflow completion, `temporal workflow describe` shows each invocation's `outcome.verdict` is one of `{approve, approve_with_comments, request_changes, abstain}` and `outcome.failure` is empty. No invocation returns the Phase 2 stub error.

---

### User Story 2 — Malformed-verdict failure is a closed FailureKind (Priority: P1)

A reviewer driver returns a `Result.Explanation` that doesn't parse as `StructuredVerdict`, or parses but violates `verdict.Validate` (e.g., `verdict=approve` with non-empty `Blockers`). The activity does NOT return an activity-level error — it returns a closed `ReviewerInvocation` with `Outcome.Failure.Kind = FailureMalformedShape` and a `Detail` string that quotes the validation error. The workflow's aggregator sees a uniform Outcome and routes per FR-009 (treat as disagreement → arbiter, or halt if pool exhausted).

**Why this priority**: Driver-side bugs must not crash the gate. Closed failure semantics is the spec 094 contract.

**Independent Test**: Mock a driver that returns `{"verdict":"approve","blockers":["foo"]}`. Activity returns Outcome with `Failure.Kind=FailureMalformedShape`. No Go error returned.

---

### User Story 3 — Activity timeout closes as FailureTimeout (Priority: P2)

A reviewer driver takes longer than `DispatchMachineReviewerTimeout` (30 min default, FR-026). The activity's `ctx` deadline fires. The driver's `Invoke` returns `Result{Status: StatusTimeout}` or an error. The activity closes the invocation with `Outcome.Failure.Kind = FailureTimeout` and `ElapsedMS = ctx.Err elapsed`.

**Why this priority**: Production reviewers (especially network-bound ones) will sometimes time out. Without explicit timeout handling, the activity returns a Go error and Temporal retries indefinitely.

**Independent Test**: Inject a driver whose Invoke sleeps past the activity deadline. Activity returns Outcome.Failure.Kind=FailureTimeout within the deadline budget. No Temporal retry storm.

---

### User Story 4 — Driver lacks review_mode declaration (Priority: P2)

The driver registry includes a driver that declares `CapCodeReview` but does NOT declare a `review_mode.tool_name`. The activity returns Outcome.Failure.Kind=FailureError with detail `"driver <id> declares CapCodeReview but has no review_mode block"`. SelectReviewers does NOT pre-filter on review_mode presence — that's a runtime concern, surfaced by the activity at dispatch time so the operator can debug.

**Why this priority**: Catches the misconfigured-driver case explicitly rather than producing a confused stub.

**Independent Test**: Mock a driver with `Capabilities=[CapCodeReview]` and no ReviewMode block. Dispatch activity returns Outcome.Failure.Kind=FailureError with the named detail string.

### Edge Cases

- **Driver returns empty Result.Explanation**: Outcome.Failure.Kind=FailureMalformedShape with detail "empty Explanation field".
- **Driver returns non-JSON Result.Explanation**: same FailureMalformedShape; detail names the json parse error.
- **Driver returns Result.Status=StatusError**: Outcome.Failure.Kind=FailureError, Detail = Result.ErrorMessage.
- **Driver's review-mode tool isn't registered with chitin-kernel**: kernel gate rejects the tool call; driver returns Result.Status=StatusError with kernel-rejection message; Outcome.Failure.Kind=FailureError.
- **Snapshot size exceeds driver's review_mode.max_bytes_in**: The activity truncates SpecArtifacts (keeping diff intact) and notes the truncation in the WorkUnit input as `snapshot_truncated_to_bytes`. Driver receives a snapshot smaller than declared cap.
- **Two primaries return DIFFERENT verdicts in the same dialectic**: workflow aggregator routes to arbiter per FR-009. This spec doesn't change that — only ensures the verdicts are real.
- **All three reviewers (P1, P2, arbiter) return FailureMalformedShape**: workflow halts with reason "no reviewer produced a structured verdict; check driver review-mode contract conformance".

## Requirements *(mandatory)*

### Functional Requirements

#### Driver review-mode declaration (US4)

- **FR-001** A driver's `driver.CapabilityCard` MUST expose a `ReviewMode` field of type `*ReviewModeConfig` (pointer; nil means "no review mode declared"). The struct:
  ```go
  type ReviewModeConfig struct {
      ToolName        string `json:"tool_name"`         // the tool the driver exposes for review
      PromptTemplate  string `json:"prompt_template"`    // driver-internal prompt; opaque to orchestrator
      MaxBytesIn      int    `json:"max_bytes_in"`       // driver-self-declared cap on snapshot bytes
  }
  ```
- **FR-002** A driver that declares `CapCodeReview` SHOULD also populate `ReviewMode`. A driver that declares `CapCodeReview` with a nil `ReviewMode` is valid (passes Card validation) but will fail-soft at dispatch time per US4.
- **FR-003** `ReviewModeConfig.ToolName` MUST be non-empty when ReviewMode is set. Empty ToolName is a Card-validation error at registry registration.
- **FR-004** `ReviewModeConfig.MaxBytesIn` MUST be ≥ 64 KB. Zero or smaller values are Card-validation errors. Recommended default: 1.5 MiB to match the snapshot cap from #959.

#### Dispatch activity wiring (US1, US3, US4)

- **FR-005** `DispatchMachineReviewer.Execute` MUST replace the `_ = d` stub at `dispatch_machine_reviewer.go:102` with the real dispatch path. The activity is bound to the driver registry (already done) and looks up the driver by `in.DriverID`.
- **FR-006** Before calling Invoke, the activity inspects `d.Card().ReviewMode`. If nil → return closed Outcome with `Failure.Kind=FailureError`, `Detail = "driver <id> declares CapCodeReview but has no review_mode block"`. No Invoke call.
- **FR-007** The activity builds a `WorkUnit` shaped per `contracts/review-mode-driver-contract.md` "Tool input schema". Field-by-field translation from the input `PRReviewInput` + `PRSnapshot`:
  ```go
  wu := driver.WorkUnit{
      SpecID: "094",                  // dialectic gate
      Tool:   reviewMode.ToolName,    // driver-declared
      Deadline: workflow.GetInfo(ctx).WorkflowExecutionTimeout.Sub(time.Since(startedAt)), // remaining
      Context: marshalReviewContext(in.Snapshot, in.PolicyClass, reviewMode.MaxBytesIn),
  }
  ```
- **FR-008** `marshalReviewContext` MUST produce a JSON object matching the contract's "Tool input schema" exactly: keys `pr`, `diff`, `spec_artifacts`, `policy_class_hint`, `snapshot_captured_at`, `max_bytes_in`. Field-name compatibility is load-bearing — the driver-side prompt is opaque to chitin (FR-003 of spec 094), so any rename breaks every driver's prompt.
- **FR-009** If `len(marshalled_context) > reviewMode.MaxBytesIn`, the activity progressively truncates `spec_artifacts` (drop the largest first), then truncates each diff to a per-file budget, until the marshalled size fits OR the diff section is reduced to path-only. The final input MUST include a `snapshot_truncated_to_bytes` field naming the final byte size, so the driver knows it received a partial view.
- **FR-010** The activity calls `d.Invoke(ctx, wu)`. Three terminal outcomes:
  - `Result.Status == StatusSuccess`: parse `Result.Explanation` as `verdict.StructuredVerdict`. Run `verdict.Validate`. If pass → `Outcome.Verdict = &parsed`, `Failure = nil`. If validate fails → `Outcome.Failure.Kind = FailureMalformedShape`, detail = err.Error().
  - `Result.Status == StatusTimeout`: `Outcome.Failure.Kind = FailureTimeout`, detail = `Result.ErrorMessage` (or "deadline exceeded" if empty).
  - `Result.Status == StatusError`: `Outcome.Failure.Kind = FailureError`, detail = `Result.ErrorMessage`.
- **FR-011** `ctx.Err()` after Invoke → if `context.DeadlineExceeded` AND Result.Status was not already terminal: `Outcome.Failure.Kind = FailureTimeout`.
- **FR-012** Activity-level Go error returned ONLY for configuration faults: nil registry (already covered), driver not found, malformed input. All driver-side failures become closed Outcomes.

#### Verdict-shape parser (US1, US2)

- **FR-013** New helper `verdict.ParseStructured(jsonBytes []byte) (StructuredVerdict, error)`. Wraps `json.Unmarshal` + `Validate`. Used by the activity at FR-010; available to tests + future analytics.
- **FR-014** Parse failures produce error messages that name the offending field — `"verdict: unknown enum value 'maybe-approve'"`, `"concerns: must be []string, got string"`. Operator-debuggable.

#### Telemetry (US1)

- **FR-015** When a real Outcome.Verdict lands (not Failure), the existing `EmitReviewTelemetry` activity's hashes (`ConcernsHash`, `RecommendationsHash`, `BlockersHash`) now carry real content rather than empty-list hashes. No code changes here — the hash computation is already correct; this FR documents that telemetry becomes meaningful only after FR-010 is wired.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001** Dispatching `chitin-orchestrator pr-review` against PR #948 (or any merged PR with two ready CapCodeReview drivers that declare ReviewMode) produces a workflow whose Outcome.Verdict is set on at least the primary slots. No invocation returns the Phase 2 stub error.
- **SC-002** ≥ 90% of dispatched invocations across 50 sequential real-PR reviews return a parseable StructuredVerdict (i.e., FailureMalformedShape rate < 10%). Sub-criterion: the rate is operator-reviewable via `swarm-summary` chain aggregation.
- **SC-003** No reviewer invocation hangs past its activity deadline. `FailureTimeout` rate < 1% across the 50-PR sample.
- **SC-004** A driver that declares CapCodeReview without ReviewMode produces a clear `FailureError` with detail naming the gap; operator can debug from the workflow result alone, no log diving required.
- **SC-005** Driver prompt templates remain opaque to the orchestrator — chitin code never reads, validates, or modifies `ReviewModeConfig.PromptTemplate`. Verified by grep of orchestrator source.

## Assumptions

- The driver registry (spec 075) exposes a CapabilityCard with a settable ReviewMode field. If not currently, FR-001 adds it.
- The kernel-gated tool registry (chitin-kernel) supports per-tool naming so the orchestrator can dispatch by tool_name. Already the case per spec 002 (scripts-manifest) and 075 (driver contract).
- Reviewer drivers (claudecode, codex, copilot at minimum) will ship review-mode tools in follow-up driver-side PRs. This spec covers the ORCHESTRATOR side only; driver-side prompts are owned by each driver.

### Scope

#### In scope

- ReviewModeConfig type + Card validation
- `DispatchMachineReviewer.Execute` real-dispatch wiring (replaces the stub)
- `marshalReviewContext` helper producing the contract-shaped input JSON
- `verdict.ParseStructured` helper + tests
- Progressive truncation of marshalled context to fit MaxBytesIn
- All four FailureKind translation paths

#### Out of scope

- **Per-driver review-mode tool implementations.** Each driver's prompt + tool registration is its own PR.
- **Driver-side prompt templates.** Opaque to the orchestrator per FR-003 of spec 094.
- **Auto-discovery of review_mode**. The CapabilityCard is the single source of truth.
- **Operator-arbiter surface (R-OPSURF).** Spec 094 Phase 4 follow-up; orthogonal.
- **PR comment delivery of the verdict.** Spec 093 (merge queue) consumer concern; spec 094 emits the decision.
- **Multi-PR review batching.** One PR per workflow; out of scope.

### Dependencies

- Spec 094 — PR Review Mechanism (contracts/review-mode-driver-contract.md is the canonical schema)
- Spec 075 — driver contract (the CapabilityCard surface)
- Spec 102 — PR Review workflow wiring completion (most gaps already closed by this session's #954/#957/#958/#959; SC-001 of this spec consumes that work)

## Risks

### Driver-side prompts are not in chitin's control

Each driver owns its prompt template. A driver whose prompt produces malformed StructuredVerdict output silently degrades to FailureMalformedShape. **Mitigation**: SC-002 monitors the rate; spec 094's invariant test suite checks the shape. A high FailureMalformedShape rate on a single driver is an operational signal to the driver's maintainer, surfaced via swarm-summary.

### MaxBytesIn truncation may starve reviewers of context

A driver with low MaxBytesIn (e.g., 200 KB) on a large PR sees only partial diff. Verdict quality drops. **Mitigation**: drivers should declare MaxBytesIn aligned with their model's context budget. Operator can detect by checking `outcome.detail.snapshot_truncated_to_bytes < snapshot.size`.

### Activity timeout vs driver timeout asymmetry

`DispatchMachineReviewerTimeout` is 30 min (FR-026 of spec 094); a driver's own internal timeout may be shorter (e.g., codex with a 60s tool timeout). The activity respects the driver's faster timeout via `Result.Status=StatusTimeout`. No risk if drivers are honest about their own timeouts.

### StructuredVerdict schema evolution

If spec 094 amends StructuredVerdict (adds fields, deprecates enum values), every driver's prompt template needs updating. **Mitigation**: schema lives in `contracts/structured-verdict-schema.md`; spec 094 v2 amendments include a deprecation window.

## Notes for Implementation Phase

**Implementation deferred** — design-only. Recommended sequence as 2 follow-up PRs:

### PR-A (orchestrator-side dispatch, no driver changes)

1. Add `ReviewModeConfig` type to `driver/card.go`; add `ReviewMode *ReviewModeConfig` field to `CapabilityCard`. Card-validation tests for FR-003, FR-004.
2. Add `verdict.ParseStructured` helper + tests (FR-013, FR-014).
3. Add `marshalReviewContext` helper to `activities/review/` (FR-007, FR-008, FR-009). Tests cover truncation paths.
4. Replace `DispatchMachineReviewer.Execute` stub with real dispatch (FR-005 through FR-012). Tests cover all four FailureKind paths via fake drivers.
5. Integration test: end-to-end workflow with a fake driver that returns a valid StructuredVerdict — assert verdict propagates to Outcome.

### PR-B (per-driver review-mode declarations)

6. For each of {claudecode, codex, copilot, openclaw}: declare `ReviewMode` in the driver's Card. Each driver's PR includes its own prompt template + the review tool implementation.
7. Driver-side: each driver's prompt + tool registers a `review` capability that handles the input schema from `contracts/review-mode-driver-contract.md` and returns a StructuredVerdict.
8. Driver-side tests: golden-fixture verdicts on canned PR snapshots.
9. Validation: re-dispatch `chitin-orchestrator pr-review 948`; assert verdicts land per SC-001.

After PR-A + PR-B: the dialectic gate produces real verdicts on every PR run. SC-001 closes. The loop's spec-094 leg is fully operational — operators get real `approve / approve_with_comments / request_changes / abstain` decisions on each PR, with arbiter routing on disagreement. The dogfood-driven discovery from session 2026-05-24 closes.

## Metadata

- **spec_id**: 104
- **owner**: chitinhq
- **depends_on**: 094, 075, 102
- **related**: 091, 093, 099, 101
