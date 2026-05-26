---
spec_id: 124
title: Queue test time-bomb — inject `now` so `conflicting_persistent` doesn't get shadowed by wall-clock drift
status: Draft
owner: chitinhq
created: 2026-05-26
depends_on:
  - 114
related:
  - 118
---

# Spec 124 — Queue test time-bomb fix

## Why

On 2026-05-26 while reviewing PR #1135 (spec 121 impl), a
full `go test ./...` surfaced two failing tests on `main`:

```
--- FAIL: TestRunQueue_HermeticAcrossAllReasonKinds
--- FAIL: TestRunQueue_ReasonFilter_NarrowsToSingleKind/conflicting_persistent
```

The failure shape (from a 2026-05-26 12:21 EDT run):

```
queue set mismatch:
  missing: [{PR:9008 Reason:conflicting_persistent}]
  extra:   [{PR:9008 Reason:stale_no_automation}]
```

CI shows green because the CI `test` job doesn't exercise
the `cmd/chitin-orchestrator` package directly (a separate
issue; see scope-out below).

**Root cause is a time-bomb in the test fixture, not a bug
in the classifier.** Verified by calling `matchLiveRules`
directly with the expected state — the live-rule classifier
correctly returns `conflicting_persistent` for a PR with
`Mergeable: CONFLICTING, UpdatedAt: now - 3h,
LastAutomatedCommitAt: now - 1h`.

The mechanism:

  - `queue_test.go:46` defines a frozen wall-clock
    `queueTestNow = time.Date(2026, 5, 25, 17, 0, 0, 0,
    time.UTC)` and builds every fixture timestamp
    (commits, `updatedAt`, chain events) relative to it.
  - `cmd/chitin-orchestrator/queue.go:66` reads
    `now := time.Now().UTC()` directly — the production
    `runQueue` has no injection seam for time.
  - Once real wall-clock crosses ~24 hours past
    `queueTestNow`, the production code sees `now -
    fixture.UpdatedAt > 24h` for PR #9008, which makes the
    `stale_no_automation` rule's `prItselfIsOld` branch
    true, which makes the rule match a CONFLICTING PR that
    the test author intended to land in
    `conflicting_persistent` instead.
  - The first `liveRuleOrder` match wins (filter.go:96
    contract), so `conflicting_persistent` is shadowed.

The classifier's rule order (dialectic → stale → conflict)
is correct: a dialectic verdict is the most-specific
escalation signal; stale-no-automation is the more general
fallback; conflict is a current-state inference that only
fires when nothing more specific has. The bug is purely
that the test infrastructure doesn't share its clock with
the code it's testing.

**Why this matters beyond a stuck test:**

  - **Test fixtures with frozen `now` values are a class
    of bug.** The same shape exists anywhere production
    code reads `time.Now()` directly and a test builds
    fixtures off a frozen anchor. Fixing this one in
    isolation is fine; codifying the seam pattern makes
    future test fixtures durable.
  - **The classifier's behaviour change ISN'T just a test
    artifact today.** In *production*, a real PR that
    sits CONFLICTING for >24h with at least one
    orchestrator commit would surface as
    `stale_no_automation` (correct — it IS stale) rather
    than `conflicting_persistent` (also correct — it IS
    a persistent conflict). The shadowing is intentional
    per the rule order. But the test was written to
    verify both rules fire on their respective fixtures
    independently — that contract is currently broken.

**Scope is small.** Add a `nowProvider` seam to `runQueue`,
have the test inject `queueTestNow`. ~5 tasks; no behavior
change in production.

## User stories

### US1 (P1) — Production `runQueue` accepts an injectable `now` clock

> As the test author, I can construct a `runQueue`
> invocation that uses a deterministic `now` value
> instead of `time.Now()`. Production callers (the CLI
> subcommand entry point) continue to use real wall-clock
> by default — no behaviour change for operators.

**Independent test:** A hermetic test calls the
`runQueue` core function with an injected `now` set to
`2026-05-25 17:00 UTC` (the existing `queueTestNow`) and
the existing fixture, and asserts the classifier returns
`conflicting_persistent` for PR #9008 — matching the
2026-05-25 17:00 frame the fixture was authored against.

### US2 (P1) — `TestRunQueue_HermeticAcrossAllReasonKinds` passes deterministically

> As a developer running `go test ./...`, the queue tests
> pass reliably regardless of when I run them. The fix
> threads the existing `queueTestNow` constant into the
> production code path the test exercises, so the
> fixture's `2026-05-25 14:00 UTC` `updatedAt` is
> compared against the same `2026-05-25 17:00 UTC` the
> fixture was built against — not against today's
> wall-clock.

**Independent test:** Run `go test
./cmd/chitin-orchestrator/ -run TestRunQueue` after the
fix at any wall-clock time (today, in a year). All
subtests pass. The classifier returns
`conflicting_persistent` for #9008 and `stale_no_automation`
for #9007 deterministically.

### US3 (P2) — Pattern documented in the runbook for future test authors

> As a future developer writing time-sensitive tests in
> chitin, the runbook for this spec captures the
> time-bomb pattern as a class of bug, gives the
> idiomatic fix (inject `now` via a seam), and points at
> the queue tests as the canonical example. Other call
> sites that read `time.Now()` directly and have fixture
> tests with frozen anchors are listed for future
> follow-up.

**Independent test:** None — documentation contract. The
runbook (T005) lists at least three other call sites in
the repo that follow the same pattern, and a checklist
question for new specs to consider.

## Functional requirements

- **FR-001** `runQueue` at
  `cmd/chitin-orchestrator/queue.go:66` MUST accept an
  injectable `now time.Time` value through a
  package-internal seam. The seam SHOULD take the shape
  of a function-level parameter on an internal
  `runQueueWithNow` helper, with the public `runQueue`
  calling it with `time.Now().UTC()` — preserving the
  existing CLI behaviour byte-for-byte.

- **FR-002** The seam MUST propagate `now` all the way
  through to `queue.Build(chainEvents, live, now)` and
  any downstream formatter that reads time (per
  `queue.go:80` + `queue.go:97-99`). No call inside the
  request path may read `time.Now()` independently after
  the seam is in place — otherwise the determinism
  guarantee fails partway through.

- **FR-003** `TestRunQueue_HermeticAcrossAllReasonKinds`
  and `TestRunQueue_ReasonFilter_NarrowsToSingleKind`
  MUST be updated to invoke `runQueue` via the seam with
  `queueTestNow` as the injected value. The existing
  fixture data is unchanged; only the invocation path
  changes.

- **FR-004** Pre-existing production behaviour at every
  external caller MUST be byte-identical: invoking
  `chitin-orchestrator queue` from a shell with no new
  flags produces output identical to today (modulo
  wall-clock-dependent age columns, which already vary
  per run). The change MUST NOT introduce a new CLI flag
  — the seam is internal to the package.

- **FR-005** A hermetic test MUST exist that pins the
  classifier's contract: given the canonical fixture
  state for PR #9008 (`Mergeable: CONFLICTING,
  UpdatedAt: now-3h, LastAutomatedCommitAt: now-1h`),
  `matchLiveRules` returns `conflicting_persistent`.
  This is a guard against future rule-order changes that
  could regress the contract independent of the
  time-bomb fix.

## Success criteria

- **SC-001** `go test ./cmd/chitin-orchestrator/ -run
  TestRunQueue` passes deterministically on
  2026-05-26 AND on any wall-clock time at least 365
  days in the future. Measured by running the test on
  a system whose clock is artificially advanced.

- **SC-002** Operator-invoked `chitin-orchestrator
  queue` output for the operator's real PR set is
  byte-identical pre- and post-fix (modulo PR list
  changes between the two runs). Measured by snapshot
  comparison on a quiet PR backlog.

- **SC-003** A `grep -rn 'time.Now()' --include='*.go'
  cmd/chitin-orchestrator/ internal/queue/`-style scan
  shows ZERO occurrences in the request path after the
  fix, except in the single canonical seam (the
  default-value provider).

## Scope

In:
  - `cmd/chitin-orchestrator/queue.go` — `runQueue`
    seam per FR-001
  - `cmd/chitin-orchestrator/queue_test.go` — fixture
    invocation update per FR-003
  - One new hermetic test pinning `matchLiveRules` for
    PR #9008's fixture state per FR-005
  - Runbook documenting the pattern per US3
  - Tests + documentation

Out:
  - Fixing CI to actually run the
    `cmd/chitin-orchestrator` test package. The CI green
    despite a failing test indicates the `test` job
    doesn't exercise this package directly OR something
    is masking the failure at the workflow level. That's
    a separate spec (worth filing as a follow-up — see
    Composability).
  - Reordering `liveRuleOrder` to put
    `conflicting_persistent` ahead of
    `stale_no_automation`. The current order is
    intentional per spec 114 FR-003; the bug is in test
    infrastructure, not rule priority. If the operator
    later decides conflict should win, that's a separate
    semantic decision with its own spec.
  - Auditing every `time.Now()` call site in the repo
    for time-bomb potential. The runbook lists candidates
    for future follow-up but this spec doesn't refactor
    them.
  - Behavioral changes for operators (no new flags, no
    new output, no new errors).

## Edge cases

  - **Operator runs `chitin-orchestrator queue` with no
    PRs.** The seam doesn't change the empty-queue path;
    `runQueue` still returns an empty result, exit 0.
  - **CI catches a future test that uses `time.Now()`
    directly in a different test.** This spec doesn't
    add CI plumbing; the documentation (US3) captures
    the pattern but doesn't enforce it. A future spec
    can add a lint rule.
  - **A real production PR ages past 24h with
    `CONFLICTING` mergeable.** Per the spec 114 FR-003
    rule order, it surfaces as `stale_no_automation`
    (the more general rule wins on first match). That's
    the documented behaviour — not changed by this
    spec.
  - **The seam helper signature collides with a future
    refactor.** The seam is package-private; if a future
    spec restructures `runQueue`, the seam moves with
    it. No external API surface to protect.

## Composability

  - **Spec 114** (operator escalation surface) — owns
    the queue subcommand and the `Build` function. This
    spec touches both at the seam level without changing
    behaviour. The `matchLiveRules` rule order from
    spec 114 FR-003 is unchanged.
  - **Spec 118** (failure-reason taxonomy) — parallel
    structure. Both specs operate over the same
    `reason` enum but at different stages
    (118 produces, 114 consumes for the queue).
    Unaffected by this spec.
  - **Future spec — CI coverage gap.** A follow-up
    SHOULD investigate why CI showed green on `main`
    despite the `cmd/chitin-orchestrator` package
    failing local `go test`. This spec deliberately
    doesn't fix that; first establish that the test is
    deterministic, then close the CI gap.
  - **Future spec — time-bomb pattern audit.** A repo
    sweep for `time.Now()` reads in code that has
    test fixtures with frozen anchors. Likely
    candidates: anywhere in `internal/queue/`,
    `loop/`, `schedules/`, `activities/` that uses
    threshold-based time math.
