---
spec_id: 125
title: Factory-listen Added-only dispatch — stop spurious re-dispatch on impl-PR merge
status: Draft
owner: chitinhq
created: 2026-05-26
depends_on:
  - 098
related:
  - 099
  - 112
  - 119
  - 121
---

# Spec 125 — Factory-listen Added-only dispatch

## Why

On 2026-05-26 the autonomous loop produced TWO observed instances
of a phantom-PR bug — and three more were imminent if not fixed:

  - **2026-05-26 01:29Z** — spec 118 + spec 120 had each just
    merged via their whole-spec impl PRs (#1130 + #1131). Within
    9 minutes, factory-listen had re-dispatched both specs,
    opening duplicate impl PRs **#1132 + #1133**. The operator
    closed them manually with a comment naming the bug.
  - **2026-05-26 16:17Z** — same pattern. Spec 121 impl PR
    #1135 had merged at 16:06Z. By 16:17Z, factory-listen had
    dispatched a SECOND time, producing draft PR **#1138** on
    branch `chitin/wu/wu-121-driver-output-blob-store-whole-f1cd2f7c`
    (different suffix from #1135's `f97fb83c`). State: `dirty`
    (conflict against the already-merged content). Operator
    closed it.
  - **2026-05-26 16:30Z forward** — three open spec PRs (#1136
    spec 122, #1137 spec 123, #1139 spec 124) are queued to
    repeat the same pattern on merge.

**The mechanism** is at `go/orchestrator/cmd/chitin-orchestrator/factory_listen.go:34`:

```go
var specPathPattern = regexp.MustCompile(`(?:^|/)\.specify/specs/(\d+-[a-z0-9._-]+)/tasks\.md$`)
```

and the dispatch loop that consumes it:

```go
for _, path := range append(append([]string{}, c.Added...), c.Modified...) {
    if m := specPathPattern.FindStringSubmatch(path); m != nil {
        seen[m[1]] = struct{}{}
    }
}
```

The pattern matches `.specify/specs/NNN-name/tasks.md`. The
dispatch loop iterates the **union of `c.Added` and
`c.Modified`**, so any commit that *modifies* a spec's
`tasks.md` triggers a new dispatch — regardless of who made
the change or why.

The empirical Modify cases are:

  - **The impl driver itself**, when it finishes a spec, ticks
    `[ ] → [x]` on the spec's tasks.md as part of its work
    output. That tasks.md edit appears as `Modified` in the
    impl PR's merge commit, which re-triggers dispatch.
  - **A documentation tidy** that the operator (or the impl
    driver in fixup) does to clean up tasks.md (whitespace,
    wording). Modified → re-dispatch.

The legitimate Added case is the one this spec preserves:

  - **A new spec lands.** Spec PR merges → `.specify/specs/<new>/tasks.md`
    is in `c.Added`. Factory dispatches the impl. This is the
    autonomous-loop's central function and MUST be preserved
    byte-for-byte.

**The fix is one line:** iterate only `c.Added`, not the union.
Tasks.md modifications no longer re-trigger dispatch. Operators
who want to re-dispatch an existing spec use the manual
`chitin-orchestrator schedule <spec-ref>` flow (already
exists; the operator used it on 2026-05-25 night to dispatch
spec 121 after a race condition skipped its initial trigger).

**Why this is a bug-stop, not an architecture change:**

  - The current dispatch model (whole-spec on Added) is
    working. Spec 119 shipped, spec 121 shipped, spec 122-124
    queued — all via Added. No architectural redesign needed.
  - The Modify-on-impl-merge trigger is purely a leak:
    every successful impl creates a phantom duplicate impl
    attempt. The duplicate is always wrong by construction
    (the spec is already implemented; the new dispatch
    re-implements against stale state and produces a
    conflicting PR).
  - There is no observed legitimate Modify trigger today.
    If a future use case appears ("operator added a task to
    a shipped spec and wants re-dispatch"), the manual
    `schedule` subcommand exists.

**Why this is urgent (must ship tonight):**

  - Three spec PRs (#1136, #1137, #1139) are queued behind
    the spec review cycle. Each will produce a phantom
    duplicate on merge — three more operator-touch events.
  - Spec 123 (auto-merge) would, if shipped before this fix,
    auto-merge a labeled duplicate phantom into main. Spec
    123 specifically depends on the dispatch signal being
    reliable; #125 makes it so.

## User stories

### US1 (P1) — Modify-only pushes to tasks.md do NOT trigger dispatch

> As the autonomous loop, when an impl PR merges to main and
> the resulting push contains `.specify/specs/NNN-name/tasks.md`
> in `c.Modified` (because the impl driver ticked the task
> boxes) but NOT in `c.Added`, factory-listen MUST NOT
> dispatch a new whole-spec workflow for that spec. The
> existing scheduler for that spec (which produced the impl)
> has already concluded; re-dispatching against the now-merged
> state produces a duplicate PR.

**Independent test:** Synthesize a push payload mirroring the
2026-05-26 #1138 trigger (commit on `main`, tasks.md path in
`Modified` only). Assert factory-listen's `handlePush` returns
`dispatched: false, spec_refs: []` and does NOT call
`runSchedule`. A `factory_dispatch_filtered` chain event
fires per FR-003.

### US2 (P1) — Added pushes to tasks.md continue to dispatch

> As the operator landing a brand-new spec PR, when the merge
> commit contains `.specify/specs/125-foo/tasks.md` in
> `c.Added` (the file did not exist in the prior tree),
> factory-listen MUST dispatch as it does today — no
> behaviour change for the legitimate case.

**Independent test:** Synthesize a push payload where
`tasks.md` is in `c.Added`. Assert factory-listen dispatches
exactly one workflow with the matching spec ref. This is the
spec-119 happy path; the test pins it as a regression guard.

### US3 (P1) — Added + Modified for the same path → Added wins

> As an edge case, if a single commit contains the SAME
> `tasks.md` path in BOTH `c.Added` and `c.Modified` (unusual
> but the GitHub schema permits it — for instance a file
> renamed in a way that the diff parser categorizes both
> ways), dispatch MUST fire exactly once. Added is the
> signal; the Modified entry is redundant data.

**Independent test:** Synthesize a payload with the same
path in both arrays. Assert exactly one workflow starts,
not two.

### US4 (P2) — Filtered events are auditable via chain

> As the operator debugging "why didn't my spec re-dispatch
> when I updated tasks.md?", the chain MUST carry a
> `factory_dispatch_filtered` event per filtered Modify-only
> match, so an operator querying the chain can see the
> mechanism. The event payload identifies the spec ref, the
> commit SHA, and the reason.

**Independent test:** Run US1's test fixture; assert exactly
ONE `factory_dispatch_filtered` event fires with payload
`{spec_ref, commit_sha, reason: "modify_only", path}`.

## Functional requirements

- **FR-001** The dispatch loop at
  `cmd/chitin-orchestrator/factory_listen.go:handlePush`
  MUST iterate **only** `c.Added` when collecting spec refs
  to dispatch. The `c.Modified` array MUST NOT contribute
  to the dispatch set.

- **FR-002** When `specPathPattern` matches a path in
  `c.Modified` AND the same path is NOT in `c.Added`,
  factory-listen MUST emit one `factory_dispatch_filtered`
  chain event before continuing. Payload schema:
  `{spec_ref: string, commit_sha: string, path: string,
  reason: "modify_only"}`. The event is per-path, not
  per-commit — a commit modifying three tasks.md paths
  emits three events.

- **FR-003** When `specPathPattern` matches a path in
  BOTH `c.Added` and `c.Modified` (the deduplication edge
  per US3), dispatch fires exactly once via the Added path.
  No `factory_dispatch_filtered` event fires for the
  Modified duplicate — Added is the signal, the Modified
  redundancy is silent.

- **FR-004** No operator-visible behaviour change on the
  legitimate Added path. The Added handling code in
  `handlePush` is unchanged at the byte level (only the loop
  iterator that drives it changes).

- **FR-005** The fix MUST work without operator config:
  no new flag, no new env var, no config file changes. The
  semantic change is across-the-board for the deployment.

- **FR-006** The manual override path
  (`chitin-orchestrator schedule <spec-ref>`) remains the
  documented way to re-dispatch an already-shipped spec —
  unchanged by this spec, called out in the runbook.

- **FR-007** Canonical reason taxonomy for the
  `factory_dispatch_filtered` event's payload. The
  `reason` field MUST be a closed-set value drawn from:
    - `modify_only` — the tasks.md path appeared in
      `c.Modified` but not in `c.Added` (the dominant
      filter cause; covers the impl-PR-merge case that
      motivates this spec)
  The set is intentionally a single value today; if a
  future filter cause is introduced (e.g. "spec on a
  closed-revert branch"), it requires extending this
  closed enum via a spec amendment.

## Success criteria

- **SC-001** Within 24 hours of deployment, zero phantom
  duplicate impl PRs land in the chitinhq/chitin repo.
  Measured by `gh pr list --search 'is:closed reason:"duplicate dispatch"'`
  count: today's running total is 3 (#1132, #1133, #1138);
  post-deploy, no new entries.

- **SC-002** Spec PRs #1136 (122), #1137 (123), #1139
  (124), when they merge, each produce exactly ONE impl
  PR (not two). Measured by counting `chitin/wu/*` branches
  per spec-ref over the next 7 days.

- **SC-003** The legitimate Added dispatch path's behaviour
  is byte-identical to today. Measured by re-running the
  factory-listen integration tests pre- and post-fix: the
  Added-trigger tests pass unchanged.

- **SC-004** The chain carries auditable evidence of the
  filtering: every Modify-on-tasks.md push to main produces
  at least one `factory_dispatch_filtered` event. Measured
  by a chain query 7 days post-deploy: the event count is
  > 0 and correlates 1:1 with impl-PR merges to main.

## Scope

In:
  - `cmd/chitin-orchestrator/factory_listen.go` — dispatch
    loop change per FR-001 + FR-002 + FR-003
  - Chain emit site for `factory_dispatch_filtered` per
    FR-002, following the existing kernel-emit pattern
  - HTTP-route tests in `factory_listen_test.go` covering
    US1 + US2 + US3 + US4
  - Operator runbook update

Out:
  - Per-spec-ref dedup against running workflows. The
    Modify trigger is currently 100% noise; a more complex
    "dedup against Temporal" approach buys nothing the
    Added-only filter doesn't.
  - Heuristic "is this commit from the impl driver?"
    detection (commit author, message regex, etc.). Brittle
    and unnecessary — the Added/Modified distinction is
    structural.
  - Renaming/migrating the manual `chitin-orchestrator
    schedule` flow. It's the documented override path
    today and stays that way.
  - Changing what the impl driver writes to tasks.md.
    Driver-side behaviour is unchanged.
  - Architectural changes to the dispatch model
    (whole-spec vs per-task, feature-branch coordination,
    issue-driven dispatch). Those are separate strategic
    decisions; this spec only stops the leak.

## Edge cases

  - **Pure rename: `tasks.md` removed + added under a new
    path.** Both `c.Added` and `c.Removed` populated. The
    Added handling fires for the new path — correct, this
    IS a new spec dispatch (renamed spec slugs are treated
    as new specs by convention).
  - **Spec PR merge that also tweaks an UNRELATED spec's
    tasks.md** (e.g. fixing a typo as a drive-by). The
    drive-by appears as Modified → no dispatch for that
    unrelated spec. The PR author's intent is clearly the
    primary spec; the typo fix doesn't need to re-dispatch
    the typo'd spec.
  - **Commit that adds a new spec AND modifies an existing
    spec's tasks.md.** New spec dispatches via Added; the
    existing spec's Modify is silently filtered (per US4,
    a `factory_dispatch_filtered` event fires for it).
  - **Force-push that rewrites history.** GitHub still
    fires a push webhook with the new tip's diff; the
    Added/Modified distinction is computed against the
    PREVIOUS tip. If the force-push happens to make
    `tasks.md` appear as Added (e.g. the file didn't exist
    before the force-push), dispatch fires. This is
    extremely rare on `main` (branch protection) and the
    behaviour is correct in the unlikely case.
  - **Multiple webhook deliveries for the same commit.**
    GitHub retries on non-2xx. The filter is deterministic
    per push, so retries with the same payload produce the
    same outcome. Idempotent.
  - **The CHITIN_DISABLE_CHAIN_EMIT=1 environment.** The
    `factory_dispatch_filtered` emit honors the existing
    convention; chain emit is suppressed in tests/sandbox.

## Composability

  - **Spec 098** (factory webhook) — this spec is a one-line
    surgical change to the spec-098 dispatch loop. No new
    routes, no new structs.
  - **Spec 099** (webhook PR handler) — unaffected;
    operates on a different route.
  - **Spec 112** (auto-rebase) — orthogonal; auto-rebase
    keys on PR merge, not on the dispatch trigger.
  - **Spec 119** (whole-spec dispatch) — the dispatch
    target this spec protects. Whole-spec works fine; the
    only thing wrong was the trigger firing too often.
  - **Spec 121** (blob store) — orthogonal; blob events
    are not the dispatch signal.
  - **Spec 122** (report freshness canary), **spec 123**
    (auto-merge), **spec 124** (queue time-bomb fix) — each
    will benefit from this fix when they merge, because
    none of them will produce a phantom duplicate impl PR.
  - **Spec 126** (Shape C, spec issues for visibility,
    drafted alongside) — orthogonal; both can ship
    independently. Spec 126 adds a parallel surface; spec
    125 fixes the existing one.
