---
spec_id: 118
title: Closed reason taxonomy for factory_dispatch_failed + work-unit silent-drop visibility
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 098
  - 099
related:
  - 114
  - 117
---

# Spec 118 — Dispatcher failure visibility

## Why

The morning's telemetry summary (2026-05-25) surfaced two related gaps
in dispatcher observability:

  1. **`factory_dispatch_failed` events carry empty `reason` fields.** 15
     such events fired in the last 7 days; every one is opaque. The
     emit site at `factory_listen.go:622` (`emitFailureEvent`) writes
     `{spec_ref, error}` where `error` is the raw `err.Error()` string
     returned by `h.dispatch()`. No closed-taxonomy classification.
     Operators can't grep "show me every dispatch failure caused by X"
     because every failure is a snowflake. The result: cascading dispatch
     failures look like noise; root causes go unsurfaced.

  2. **Work units that complete `done` in scheduler status without
     producing a PR.** The spec 114 + spec 115 re-dispatch on
     2026-05-25 demonstrated this concretely: spec 114 reported 15/15
     tasks `done`, but only 12 produced PRs (T004 filter, T008 reason,
     T010 delta dropped silently). Spec 115 reported 25/25 done, only
     18 produced PRs (T009 L07, T013 classifier, T015 dispatch, T018
     allowlist seed, T022 route test, T024 runbook, T025 measurement
     silently dropped). The scheduler distinguishes "task completed"
     from "task produced its declared deliverable" inadequately — both
     surfaces report `done`, and no chain event differentiates them.

Both gaps are visible only at merge time (gap 1) or during operator
review (gap 2). Closing them makes the dispatcher self-diagnosing
rather than relying on the operator to notice "wait, where's T008's
PR?"

The fix:

  - **For gap 1**: define a closed `FactoryDispatchFailureKind` taxonomy
    enumerating every observed failure mode. `emitFailureEvent`
    classifies the error before emitting and populates a new
    `failure_kind` field. The raw error string stays in a separate
    `detail` field for grepability.
  - **For gap 2**: introduce a new chain event
    `work_unit_completed_without_deliverable` that fires when a
    work-unit-level activity reports success but the expected
    deliverable (PR opened, file created, etc.) is absent at the
    activity's exit. The scheduler's `done` status remains accurate
    in the strict sense ("the activity returned success") but the new
    event surfaces the gap that's invisible from the status alone.

This composes naturally with spec 117 (file-overlap edge inference for
shared support files) — together they close the two failure classes
that hit the spec 114/115 dispatches. Spec 117 prevents the file
collisions; spec 118 makes the remaining drops observable.

## User stories

### US1 (P1) — `factory_dispatch_failed` events carry a classified `failure_kind`

> As the operator investigating a recent dispatch incident, every
> `factory_dispatch_failed` chain event in the last 7 days carries a
> `failure_kind` field whose value is one of the closed taxonomy
> entries. Grepping `failure_kind = X` returns every event of that
> class — I can count "how many spec-load failures vs Temporal-dial
> failures vs ambiguous-ref failures hit this week" without parsing
> 15 different error message strings.

**Independent test:** Trigger a factory dispatch with each failure
mode (synthetic for the rarer ones) and assert each emitted
`factory_dispatch_failed` event carries the expected `failure_kind`
from the closed taxonomy. Re-running the telemetry summary script
(`/tmp/telemetry_summary.py`) over the resulting events should group
them by `failure_kind` cleanly.

### US2 (P1) — Work-unit-level silent drops emit a `work_unit_completed_without_deliverable` event

> As the operator running `chitin-orchestrator status -run-id <id>`,
> when the scheduler reports a task `done` but the activity's
> deliverable contract wasn't actually satisfied (no PR opened, no
> file created), the chain shows a
> `work_unit_completed_without_deliverable` event for that task.
> Grepping the chain for that event_type lists every silent drop —
> no more "wait, where's T008's PR?" surprises during merge review.

**Independent test:** Run a synthetic spec containing one task whose
activity returns success but produces no PR (mock the activity to
short-circuit before the open-PR step). The scheduler status reports
the task `done`; the chain contains exactly one
`work_unit_completed_without_deliverable` event keyed by the
work-unit id.

### US3 (P2) — Operator queue (spec 114) surfaces silent drops

> As the operator running `chitin-orchestrator queue`, the
> `work_unit_completed_without_deliverable` events are added to the
> spec 114 FR-008 reason taxonomy under a new reason kind
> `silent_drop`. The queue surfaces silent-drop work units alongside
> the existing escalation kinds so a daily triage catches them
> without my having to remember to grep the chain.

**Independent test:** Emit a synthetic
`work_unit_completed_without_deliverable` chain event keyed to a
real open PR (or, since silent drops have no PR, keyed to the
spec-ref so the queue surfaces it as a no-PR row). Running
`chitin-orchestrator queue --reason silent_drop` lists it.

## Functional requirements

- **FR-001** Define `FactoryDispatchFailureKind` as a closed string
  taxonomy in `go/orchestrator/cmd/chitin-orchestrator/factory_listen.go`
  (or a sibling file). Initial set:
    - `spec_ref_not_found` — the spec dir does not exist under
      `.specify/specs/`
    - `spec_ref_ambiguous` — the spec ref matches more than one dir
    - `tasks_md_missing` — spec dir exists but `tasks.md` does not
    - `tasks_md_parse_error` — `tasks.md` exists but the speckit
      adapter could not parse it
    - `temporal_dial_failed` — `client.Dial` returned an error
    - `temporal_start_workflow_failed` — Temporal accepted the
      connection but `StartWorkflow` returned an error
    - `capability_mismatch` — at least one task declares a capability
      no registered driver provides
    - `internal` — anything else (caught fallback, keeps the
      taxonomy closed at the boundary)

- **FR-002** `emitFailureEvent` MUST accept a `failure_kind` parameter
  and write it into the payload as a top-level `failure_kind` field.
  The existing `error` field stays as-is (renamed to `detail` is
  acceptable but not required) so existing grep-based triage doesn't
  break.

- **FR-003** Every call site that invokes `emitFailureEvent` MUST
  classify the error against the taxonomy before emission. The
  classifier is a pure helper `classifyDispatchError(err error)
  FactoryDispatchFailureKind` that wraps `errors.As` / string-match
  checks. Unrecognised errors classify as `internal`.

- **FR-004** A new chain event_type
  `work_unit_completed_without_deliverable` MUST be emitted when a
  work-unit-level activity (currently `DeliverWorkProduct` in
  `go/orchestrator/activities/deliver.go` plus any future
  deliverable-producing activity) returns success but the deliverable
  contract was not satisfied. Payload shape:
    - `work_unit_id` (the orchestration-id of the activity invocation)
    - `task_id` (the spec task id, e.g. "T008")
    - `spec_ref` (the spec dir id)
    - `deliverable_kind` (closed set: `pr`, `file`, `chain_event`)
    - `reason` (closed set: `no_changes_to_commit`,
      `git_push_failed`, `gh_pr_create_failed`,
      `activity_declined_without_failure`)

- **FR-005** The work-unit-level emit MUST happen INSIDE the activity
  body — not in the workflow that calls it — so the chain event
  survives even when the workflow reports the task `done` via the
  activity's nominal success. The activity's `Result.Explanation`
  field MUST also name the missing deliverable so Temporal-history
  readers see the gap without dropping to the chain.

- **FR-006** The spec 114 reason taxonomy (`internal/queue/reason.go`,
  shipped in #1103) MUST be extended with a new kind `silent_drop`.
  The spec 114 filter's chain-event scanner (FR-008) recognises
  `work_unit_completed_without_deliverable` events and surfaces them
  in the queue.

- **FR-007** The taxonomy growth contract: adding a new
  `FactoryDispatchFailureKind` or work-unit reason value is a
  source-level change requiring a follow-up spec. Drivers MUST NOT
  invent reasons at runtime (FR-010 mirror of spec 113).

## Success criteria

- **SC-001** Replay the chain over the last 7 days through a small
  reclassification script that pretends the new taxonomy was in
  place: every recorded `factory_dispatch_failed` event maps to one
  of the FR-001 taxonomy values via the new classifier (or
  `internal` for anything truly opaque). Target: ≤ 10% land in
  `internal` after one pass; the rest in named kinds.

- **SC-002** Re-dispatch a spec known to have silent drops (e.g. the
  prior spec 114 dispatch reproduced in a test fixture). The chain
  contains one `work_unit_completed_without_deliverable` event per
  dropped task.

- **SC-003** `chitin-orchestrator queue --reason silent_drop` lists
  exactly the silent-dropped tasks after the test fixture replays.

## Scope

In:
  - `factory_listen.go` — `FactoryDispatchFailureKind` taxonomy,
    `classifyDispatchError`, `emitFailureEvent` payload extension
  - `activities/deliver.go` — `work_unit_completed_without_deliverable`
    emit on the "no changes to commit" / "push failed" / "open PR
    failed" branches
  - `internal/queue/reason.go` (already exists from #1103) — extend
    with `silent_drop`
  - `internal/queue/scan.go` (already exists from #1103) — recognise
    the new event_type in the chain scanner
  - `internal/queue/filter.go` (already exists from #1103) — map
    silent-drop events to queue entries
  - Tests for each of the above

Out:
  - Other emit sites that DON'T relate to dispatch failure (those
    keep their existing payloads). This spec is tightly scoped to
    the two surfaces above.
  - Retroactive backfill of the 15 already-recorded
    `factory_dispatch_failed` events — those stay as historical
    artifacts with their original empty `failure_kind`. SC-001 uses
    a one-shot replay script, not a chain-rewrite.

## Edge cases

  - **Multi-fault one dispatch**: A single dispatch attempt that
    fails for compound reasons (e.g. spec_ref exists but tasks.md
    parses AND a Temporal dial error). The classifier returns the
    FIRST matching kind from FR-001's listed order. Operators see
    the symptom that fired first; downstream investigation surfaces
    the secondary.
  - **Activity reports success, deliverable missing, AND error
    follow-on**: The `work_unit_completed_without_deliverable` event
    fires regardless. The activity's `Result.Explanation` records
    both the nominal-success-reason and the missing-deliverable-
    reason side by side.
  - **`internal` classification proliferates**: If SC-001 measures
    >50% landing in `internal` after a week, that's a signal the
    taxonomy needs a follow-up extension spec — not that the spec
    failed. The taxonomy is meant to grow as new failure modes
    surface.

## Composability

  - **Spec 098 / 099** (factory_listen webhook receiver) — this spec
    extends the existing emit path; the receiver's wire-up is
    unchanged.
  - **Spec 114** (operator queue) — `silent_drop` becomes the ninth
    FR-008 reason kind. The queue's existing closed-taxonomy
    enforcement (`ValidateReasonKind` in `internal/queue/reason.go`)
    accepts the new value.
  - **Spec 117** (file-overlap edge inference) — sibling spec for
    the file-collision failure class. Together they close the two
    dispatcher-level failure modes observed during the spec 114/115
    re-dispatch on 2026-05-25.
  - **Spec 113** (PR comment-respond loop) — the iteration loop's
    `pr_iteration_escalated` event already carries a closed `reason`
    field. This spec adopts the same pattern for the dispatch path.
