---
spec_id: 109
title: claudecode driver — review-mode StructuredVerdict JSON contract
status: Draft
owner: chitinhq
created: 2026-05-24
depends_on:
  - 075
  - 094
related:
  - 108
  - 110
---

# Spec 109 — claudecode review-mode JSON verdict contract

## Why

The 2026-05-24 autonomous-loop dogfood reached the spec 094 dialectic review stage (after spec 108 + the operator-pinned 2-driver pool widened review eligibility). The first primary's invocation looked like:

```
driver_id: claudecode
elapsed_ms: 220442
failure:
  kind: malformed_json
  detail: "json.Unmarshal: invalid character 'd' looking for beginning of value"
```

claudecode took 3m40s to run, then returned **prose** instead of the structured `StructuredVerdict` JSON spec 094's `DispatchMachineReviewer` activity expects per `contracts/review-mode-driver-contract.md`. The activity classified it as `FailureMalformedShape`, blocking the verdict aggregation.

Spec 094 already documents what the contract looks like — `StructuredVerdict` is one of `{verdict: approve|approve_with_comments|request_changes|abstain, blockers: [], comments: [], summary: ""}`. The gap is that claudecode's review-mode invocation doesn't enforce this shape on output.

## User stories

### US1 (P1) — claudecode emits StructuredVerdict JSON in review mode

> As spec 094's `DispatchMachineReviewer` activity invoking claudecode for a PR review, the driver's `Result.Explanation` field is a valid `StructuredVerdict` JSON document. Parsing succeeds; verdict aggregation proceeds.

**Independent test:** Mock a PR snapshot with a known diff. Invoke claudecode driver in review mode with that snapshot. Assert `Result.Explanation` parses as `StructuredVerdict` and the `verdict` field is one of the closed enum values.

### US2 (P2) — Schema-violation defense

> If claudecode's underlying CLI emits something un-JSON (network glitch, model hiccup), the driver wrapper detects the parse failure and emits a `FailureMalformedShape` outcome from the driver side — not from the activity. Failure detail surfaces what the model actually produced, capped at 1 KiB.

**Independent test:** Inject a fake `claude` binary that prints "this is prose, not JSON" on stdout. Invoke claudecode in review mode. Assert `Result.Status = StatusFailed`, `Result.Explanation` contains the raw output truncated to 1 KiB.

## Functional requirements

### Prompt template

- **FR-001** claudecode driver, when invoked with `Tool=review` (or whatever the review-mode discriminator is on `WorkUnit`), constructs a prompt that includes an explicit JSON schema and an example `StructuredVerdict` so the model knows the output format. The prompt MUST end with: "Return ONLY a JSON document matching this schema. No markdown, no prose, no commentary outside the JSON."
- **FR-002** The driver's review-mode prompt cites the spec 094 contract URL (`docs/`) so the model can self-reference.

### Output post-processing

- **FR-003** Driver wrapper extracts the JSON document from the model's raw output. Strategies in order: (a) strip surrounding markdown fences (` ```json ... ``` `), (b) extract the largest balanced `{...}` block, (c) return raw if no JSON-shaped substring exists.
- **FR-004** If extracted JSON fails to parse OR doesn't satisfy `verdict.Validate` (spec 094 contract), driver emits `Result{Status: StatusFailed, Explanation: "malformed_verdict: <parse error>; raw: <first 1KiB of model output>"}`.
- **FR-005** If extracted JSON parses + validates, driver emits `Result{Status: StatusSucceeded, Explanation: <the validated StructuredVerdict JSON, re-serialized canonically>}`.

### Tests

- **FR-006** Unit test in `go/orchestrator/driver/claudecode/review_mode_test.go` covering: (a) clean JSON-only response, (b) markdown-fenced JSON, (c) prose response (malformed), (d) JSON that violates `verdict.Validate` (e.g., `verdict=approve` with non-empty `blockers`).
- **FR-007** Update the driver Card to declare `review_mode.tool_name` per spec 094's review-mode driver contract (if not already).

## Success criteria

- **SC-001** Re-running the 2026-05-24 dialectic review on PR #1007 with this fix in place: claudecode primary returns `Result.Status = StatusSucceeded` and a parseable `StructuredVerdict`.
- **SC-002** Spec 094 `DispatchMachineReviewer` activity classifies the outcome as a real verdict, not `FailureMalformedShape`.
- **SC-003** No regression in claudecode's existing (non-review) implementation path.

## Scope

### In scope

- Prompt template extension for review-mode invocations
- Output post-processor that extracts + validates JSON
- Driver-level malformed-verdict handling (don't bubble raw output to the activity)
- Test coverage for the four primary cases

### Out of scope

- Changes to spec 094's `DispatchMachineReviewer` activity itself
- Changes to other drivers' review modes (spec 110 handles codex)
- Multi-turn review (driver asks clarifying questions before verdict) — closed: single-shot per invocation

## Edge cases

- **Model returns markdown table or prose review without JSON:** driver returns `StatusFailed` with raw output snippet. Activity classifies as `FailureMalformedShape`. Operator can amend prompt for next iteration.
- **Model returns valid JSON but `verdict.Validate` rejects (e.g. blockers non-empty under approve):** driver returns `StatusFailed`; activity treats as malformed.
- **Model emits two JSON blocks separated by prose:** post-processor takes the LARGEST balanced `{...}` block. Documented behavior; spec authors should prompt for single-block output.
- **Empty stdout from the CLI (model timed out before responding):** driver returns `StatusFailed` with detail "empty output from claudecode review-mode invocation".
