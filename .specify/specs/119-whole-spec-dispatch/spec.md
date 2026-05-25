---
spec_id: 119
title: Whole-spec single-driver dispatch — flip the default unit of work from "task" to "spec"
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 070
  - 076
  - 097
related:
  - 094
  - 113
  - 114
  - 115
  - 116
  - 117
  - 118
---

# Spec 119 — Whole-spec dispatch

## Why

Chitin's dispatcher (spec 070 + 076 + 097) was designed when its
target drivers were T1-T2 capability — small context windows, narrow
attention, expensive per-token. The natural shape was:

  - Spec author writes a `tasks.md` with N atomic tasks
  - Scheduler compiles a DAG, dispatches each task as a separate work
    unit, each in its own driver invocation, each producing its own PR
  - Multi-agent parallelism is the win; per-task granularity is the
    audit trail

Today's empirical evidence (2026-05-25) demonstrates the shape is
mis-applied for the "implement one spec" use case under T4 drivers:

  - **Cross-task coherence failures.** Spec 114's T005 / T006 / T007
    each independently created `internal/queue/types.go` because no
    task owned the shared `Entry` struct (spec 117 root cause). Spec
    115's T005 / T006 each independently created
    `internal/speclint/violation.go`. Three independent invocations
    each invented its own version of the shared type; merge time
    failed to compile. A single driver invocation holding the whole
    spec produces ONE `types.go` because the same model is making
    every decision.

  - **Silent task drops.** Spec 114 reported 15/15 tasks done in
    scheduler status; only 12 produced PRs (T004 filter, T008 reason,
    T010 delta silently dropped). Spec 115 reported 25/25 done; only
    18 produced PRs (T009/T013/T015/T018/T022/T024/T025 silently
    dropped). The scheduler's "done" doesn't distinguish "task
    completed" from "task produced its deliverable" — spec 118 is the
    diagnostic fix. But the failure class evaporates entirely under
    whole-spec dispatch: there's one work unit, and it either delivers
    or doesn't.

  - **Operator overhead.** Today's spec 114 + 115 re-dispatch
    produced 30 work-unit PRs that the operator had to triage. After
    the gap-fill PRs (#1103, #1104) coalesced 16 of them, the
    operator-touch-time per spec was still measured in hours, not
    minutes.

  - **Wall-clock.** Each spec took ~1.5h fragmented (sequential
    driver invocations + per-PR CI + Copilot review + iteration loop
    + merge). A single-driver invocation against opus-4.7 or
    gpt-5.5-codex would have produced the same coherent PR in 20-30
    minutes.

  - **Token economics.** Per-task dispatch reloads the spec, the
    plan, surrounding code into context on every invocation. One
    big-window invocation pays the context-load cost ONCE. The
    "small-task = cheaper" intuition reverses for repeated context
    reloads.

  - **Small narrow tasks lose invariants.** Drivers hallucinate more
    when they don't see WHY the change is part of a larger feature.
    A task whose description is "implement L02 (cross-spec refs)
    rule" without the surrounding spec context is a strictly worse
    prompt than the same task embedded in the full spec.

The fix: **flip the default unit of work from "task" to "spec".**
`chitin-orchestrator schedule <spec-ref>` becomes a single-driver
invocation of a T4-capable agent (opus-4.7 / gpt-5.5-codex / similar)
that receives the full spec.md + tasks.md + plan.md and produces a
single PR addressing every task. The per-task dispatch mode remains
available behind `--per-task` for the cases that genuinely benefit
(orthogonal multi-agent work, cross-project coordination), but is no
longer the default for spec implementation.

This composes naturally:
  - Spec 094 dialectic review still fires on the resulting PR
  - Spec 113 iteration loop still fires on Copilot review submissions
  - Spec 116 internal re-review still fires on the fixup commit
  - Specs 117 + 118 still address the per-task dispatch failures for
    operators who opt back into that mode
  - The "multi-agent" thesis remains true for review (spec 094 across
    drivers) and for cross-project orchestration (the multi-project
    play); spec 119 just stops abusing it for single-spec implementation

## User stories

### US1 (P1) — `--whole-spec` dispatches one work unit per spec

> As the operator running `chitin-orchestrator schedule
> 117-file-overlap-edge-creates --whole-spec`, the scheduler compiles
> a single-node DAG (one work unit covering the entire spec) and
> dispatches it to one T4-tier driver. The driver receives the full
> spec.md + tasks.md + plan.md as its context and is expected to
> produce ONE PR that closes every task in tasks.md.

**Independent test:** Schedule a representative spec
(e.g. spec 117) with `--whole-spec`. Within the driver's timeout
window (4 hours), exactly one PR appears on the spec's branch with
every `[ ]` task in tasks.md updated to `[x]`. The chain emits one
`work_unit_completed` event for the dispatch (vs N for per-task).

### US2 (P1) — Whole-spec is the default for new dispatches

> As the operator typing `chitin-orchestrator schedule <spec-ref>`
> with no mode flag, the scheduler runs in `--whole-spec` mode. The
> per-task behaviour is preserved behind an explicit `--per-task`
> flag so operators who specifically want the parallel-dispatch
> shape can still opt in (e.g. for cross-cutting refactors where
> tasks really are orthogonal).

**Independent test:** Schedule any spec WITHOUT a mode flag; verify
the chain `scheduler_started` event payload carries
`mode: "whole-spec"`. Schedule the same spec with `--per-task`;
verify `mode: "per-task"`.

### US3 (P2) — Telemetry distinguishes the two modes

> As the operator running the morning telemetry summary, the
> per-mode dispatch counts and outcomes are surfaced separately so I
> can measure the strategic shift: how many specs ran whole-spec
> this week, what their median wall-clock-to-PR was, what their
> coherence-failure rate was vs the per-task baseline.

**Independent test:** The `scheduler_started` chain event carries a
`mode` field per US2. Querying the chain by `mode` returns disjoint
sets. A small Python script (one-off) can compute per-mode medians
over the last 7 days.

## Functional requirements

- **FR-001** `chitin-orchestrator schedule <spec-ref> [--whole-spec |
  --per-task]` accepts a mutually-exclusive mode flag. Default when
  neither is specified: `--whole-spec`. The `--per-task` flag MUST
  preserve the existing pre-spec-119 dispatcher behaviour byte-for-byte
  so operators relying on the old shape see no regression.

- **FR-002** In `--whole-spec` mode, the scheduler MUST compile a
  single-node DAG: ONE work unit whose ID matches the spec ref
  (e.g. `wu-117-file-overlap-edge-creates`), no derived edges, no
  per-task fan-out.

- **FR-003** The whole-spec work unit's context payload MUST include:
  the full `spec.md`, the full `tasks.md`, the full `plan.md` (if
  present), and the closed list of unchecked task IDs from tasks.md.
  No truncation, no summarization — drivers MUST see the full
  authoritative text.

- **FR-004** The whole-spec work unit MUST be routed to a driver
  whose `CapabilityCard` declares a new capability tag
  `code.spec-implement` (added by this spec) AND whose `Tier` is
  T4. The capability tag is the routing signal; drivers that don't
  declare it remain available for per-task work but won't be picked
  for whole-spec dispatch.

- **FR-005** The whole-spec work unit's `Deadline` MUST extend to
  support a full-spec invocation. Initial value: 4 hours. Operators
  can override via `--timeout`. The activity timeout in
  `RunWorkUnitWorkflow` extends to match.

- **FR-006** The whole-spec mode MUST emit one
  `whole_spec_dispatched` chain event at the start (carrying spec
  ref, driver id, task count) and one `whole_spec_completed` event
  on success / `whole_spec_failed` on failure. These are NEW event
  types; the per-task `scheduler_started` / `work_unit_completed`
  events remain unchanged for per-task dispatch.

- **FR-007** Spec 113's iteration loop MUST fire on the resulting
  PR's Copilot reviews unchanged. The webhook handler doesn't care
  whether the PR was produced by whole-spec or per-task dispatch.

- **FR-008** Spec 094 dialectic review and spec 116 internal
  re-review MUST fire on the resulting PR unchanged.

- **FR-009** The `--per-task` mode MUST emit a deprecation note to
  stderr (not an error) reminding the operator of the
  `--whole-spec` default. The note includes a one-line "if you
  meant to do this for X reason, this is the right flag" hint.

- **FR-010** The `scheduler_started` chain event payload MUST add a
  closed-taxonomy `mode` field with values `whole-spec` |
  `per-task`. Existing chain consumers (spec 114 queue scanner, spec
  118 silent-drop detector) MUST tolerate the new field without
  schema changes.

## Success criteria

- **SC-001** A representative spec dispatched in whole-spec mode
  produces exactly ONE PR that addresses every task in tasks.md.
  Measured by re-running spec 117's dispatch in both modes after
  spec 119 ships: whole-spec produces 1 PR; per-task produces 9.

- **SC-002** Median wall-clock from `schedule` to "PR open" drops
  by ≥ 50% in whole-spec mode vs the per-task baseline. Measured
  over 5+ specs dispatched in each mode post-deployment.

- **SC-003** Operator-touch-time per spec drops by ≥ 70%. Proxy
  metric: count of PRs the operator manually closed, merged, or
  commented on during the spec's lifecycle.

- **SC-004** Cross-task coherence failures (the class spec 117
  addresses) drop to zero in whole-spec mode by construction —
  there are no parallel tasks to collide. Measured by replaying
  spec 114 + spec 115 under whole-spec: no `types.go` /
  `violation.go` style collisions.

- **SC-005** Silent task drops (the class spec 118 addresses) drop
  to zero in whole-spec mode by construction — there is one work
  unit, and it either delivers all tasks or fails. Measured by
  comparing the count of completed-but-undelivered tasks
  whole-spec vs per-task.

- **SC-006** Token cost per shipped spec in whole-spec mode is
  comparable or LOWER than the per-task baseline (despite the
  larger per-invocation context). The intuition: per-task pays the
  spec-context cost N times; whole-spec pays it once. Measured by
  driver-invocation token telemetry.

## Scope

In:
  - `cmd/chitin-orchestrator/schedule.go` — `--whole-spec` /
    `--per-task` flag parsing, mode propagation
  - `cmd/chitin-orchestrator/schedule.go` — deprecation note on
    `--per-task` invocation
  - `internal/dag/` (or `adapter/speckit/`) — single-node DAG
    compilation path for whole-spec mode
  - `driver/driver.go` — new `Capability` constant
    `CodeSpecImplement` (or analog)
  - Driver capability cards on claudecode (opus-4.7) + codex
    (gpt-5.5-codex) — declare the new capability tag
  - `workflows/scheduler_workflow.go` — single-node-DAG handling
    path, possibly just a degenerate case of the existing N-node
    path
  - `activities/run_work_unit.go` (or wherever the work-unit
    activity lives) — extended timeout per FR-005
  - Chain emit sites — new event types per FR-006, `mode` field on
    `scheduler_started` per FR-010
  - Tests + documentation

Out:
  - Removing the per-task code path (FR-001 preserves it byte-for-
    byte). Removal can be a follow-up after metrics establish that
    nobody depends on it.
  - Auto-routing between modes based on spec shape (e.g. "if
    tasks.md has >20 tasks, prefer whole-spec"). Heuristic
    selection can be a follow-up; this spec leaves the choice
    explicit.
  - Multi-spec / cross-project dispatch coordination. That's the
    legitimate multi-agent use case; out of scope for this spec
    but enabled by it (the "one driver per spec" shape is the
    building block for "many drivers across many specs").

## Edge cases

  - **`tasks.md` declares no tasks** (degenerate spec): the
    whole-spec work unit still dispatches; the driver is told "the
    spec has no tasks; verify the spec.md and tell me what to
    deliver." The activity reports `Result.Explanation` accordingly
    without producing a PR. Surfaces via spec 118's
    `work_unit_completed_without_deliverable` mechanism.
  - **`spec.md` is malformed** (no FRs, no user stories): the spec
    115 spec-lint subcommand catches this before dispatch. The
    scheduler MAY run spec-lint pre-flight in whole-spec mode and
    refuse to dispatch on lint-error severity (this is a
    follow-up; this spec doesn't require it).
  - **Driver deadline exceeded** (4h): the activity returns a
    timeout result; the chain emits `whole_spec_failed` with reason
    `deadline_exceeded`. The operator decides whether to retry
    with a longer `--timeout` or fall back to `--per-task`.
  - **Driver hallucinates a partial implementation**: the iteration
    loop (spec 113) catches the gaps via Copilot review on the
    resulting PR. The dialectic re-review (spec 094 + 116) provides
    a second pair of eyes before the operator merges.
  - **Operator wants per-task for one spec** (e.g. a genuinely
    cross-cutting refactor where parallel drivers each touch a
    different package): `--per-task` is preserved verbatim. The
    deprecation note nudges but doesn't block.

## Composability

  - **Spec 070 / 076 / 097** (scheduler entry points + workflow
    + CLI) — this spec extends the dispatcher's mode selection;
    the workflow types stay.
  - **Spec 094** (dialectic review) — multi-driver review on the
    resulting PR is unchanged. THIS is where multi-agent earns its
    keep: different drivers reviewing the same artifact.
  - **Spec 113** (PR iteration loop) — fires on Copilot review
    submissions on the resulting PR unchanged.
  - **Spec 116** (internal re-review) — fires on the fixup commit
    unchanged.
  - **Specs 117 + 118** (dispatcher meta-fixes) — remain relevant
    for the `--per-task` mode that stays available; whole-spec
    mode sidesteps the failure classes those specs address.
  - **Multi-project play (future)** — the natural extension. One
    operator coordinating N projects, each with whole-spec dispatch
    against its own driver, dialectic review across them. The
    correct shape for "multi-agent self-improving swarm" — orthogonal
    cross-project work, not fragmented within-spec work.
