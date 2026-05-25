---
spec_id: 116
title: Multi-driver iteration re-review — Copilot for round 1, drivers for round 2+
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 094
  - 113
related:
  - 098
  - 114
---

# Spec 116 — Multi-driver iteration re-review

## Why

Spec 113 ships the PR-comment-respond loop, but it depends on Copilot
posting a fresh review after each fixup commit so the next round can
fire. Empirical evidence across this session's PRs (#1038, #1041, #1042,
#1043, #1044, #1048, #1049, #1052, #1066) shows that **Copilot rarely
re-reviews fixup commits**. Pattern observed:

  - PR opens → Copilot posts review with N comments (always)
  - Driver pushes fixup commit addressing comments
  - Copilot... usually doesn't re-review. PR sits MERGEABLE with the
    original comments still nominally "unaddressed" until the operator
    eyeballs the fixup and merges.

This makes the spec 113 loop's "cap of 3 rounds" mostly theoretical —
round 2 almost never fires because the trigger (Copilot review) doesn't
appear. The loop reduces to "one fixup attempt, then the operator decides."

The fix: **after each fixup commit, dispatch an internal re-review with
a DIFFERENT driver** (codex or claudecode-haiku) that explicitly checks
whether the fixup addressed the original Copilot comments. The internal
review posts a structured verdict as a PR review, which itself triggers
the spec 113 webhook → another iteration round if the verdict is
RequestChanges, or a `ready-to-merge` label if Approve.

This composes naturally:
  - Spec 094 already has the multi-driver dialectic-review substrate
    (StructuredVerdict, DispatchMachineReviewer, ParseStructured)
  - Spec 113 already has the iteration workflow and the fixup-commit
    push path
  - Spec 094's R-AUTHORID rule already enforces no-self-review (driver
    that authored cannot review)

What's missing is just the GLUE: after spec 113's fixup commit lands,
fire a spec-094-shaped re-review with a fixup-focused reviewer pool,
post the verdict as a PR review, let the resulting webhook drive the
next round.

## User stories

### US1 (P1) — Internal re-review fires after every fixup commit

> As the operator, when spec 113's loop pushes a fixup commit on a
> chitin-authored PR, the factory automatically re-reviews the fixup
> with a non-Copilot driver (codex or claudecode-haiku, picked to
> exclude the fixup's author). The re-review's structured verdict is
> posted as a PR review via `gh pr review` — which fires the
> pull_request_review webhook and drives the next iteration round
> through the spec 113 dispatcher.

**Independent test:** Spec 113's `pr_iteration_completed` event fires
with `pushed_fixup: true`. Within 1 minute, a non-Copilot review
appears on the PR (`gh pr view --json reviews`) authored by the
selected re-review driver. If the verdict is RequestChanges, spec 113
fires another round; if Approve, the PR gets the `ready-to-merge`
label.

### US2 (P2) — Approval lands a label, not an auto-merge

> As the operator, an Approve verdict from the internal re-review
> applies a `ready-to-merge` PR label rather than calling `gh pr
> merge` directly. The final merge click stays with me — the loop's
> job is to land me at "this PR is provably good" not at "this PR is
> merged without my consent."

**Independent test:** PR with all-Approve verdicts shows the
`ready-to-merge` label. PR is NOT merged automatically. Operator
`gh pr merge` works as usual.

### US3 (P2) — Confidence signal in StructuredVerdict distinguishes "actually approved" from "gave up"

> As the operator, the re-review's StructuredVerdict carries a
> `confidence: high | medium | low` field. A `low` confidence Approve
> applies the `ready-to-merge` label BUT also escalates to my queue
> (spec 114) so I know to look. A `high` confidence Approve labels and
> stays quiet — that's the autopilot case.

**Independent test:** A re-review verdict with `confidence: low`
applies BOTH `ready-to-merge` and `chitin-escalated/low-confidence`
labels. Spec 114's queue surfaces it under the low-confidence reason.

## Functional requirements

### Trigger (US1)

- **FR-001** Spec 113's `IteratePRReview` activity, on the path where
  `PushedFixup: true`, fires a follow-on `DispatchInternalReview`
  activity. The follow-on is sequential within the same workflow round
  (not a separate workflow) so the operator sees one workflow per
  iteration round.
- **FR-002** Reviewer pool is configured via operator-host env:
  `CHITIN_INTERNAL_REREVIEW_POOL=codex,claudecode-haiku` (comma-
  separated driver ids). Spec 094's R-AUTHORID rule excludes the fixup
  author from the pool.

### Re-review invocation (US1)

- **FR-003** `DispatchInternalReview` activity invokes one driver from
  the pool with a spec-094-style review prompt — but the prompt
  context includes:
  - The fixup commit's diff (`git show <sha>`)
  - The ORIGINAL Copilot review body + line comments
  - The original PR description
  - A required `StructuredVerdict` output envelope per spec 094 FR-003
- **FR-004** Re-review driver MUST NOT have the same id as the fixup
  author (spec 094 R-AUTHORID). On empty eligible pool (e.g.,
  single-driver operator-host configuration), the activity emits
  `internal_rereview_skipped { reason: "empty_pool" }` and the loop
  continues with the existing "operator decides" fallback.

### Verdict propagation (US1)

- **FR-005** Re-review driver's verdict is posted as a GitHub PR
  review via `gh pr review <PRNumber> --comment --body <verdict-body>`
  (using `--comment`, NOT `--approve` — even an Approve verdict from
  an internal driver does not click GitHub's approve button; only the
  operator does). Body format: human-readable summary + the canonical
  StructuredVerdict JSON in a fenced block (so subsequent tools can
  parse it).
- **FR-006** The posted review fires `pull_request_review.submitted`
  → spec 113's webhook handler → next iteration round if
  `RequestChanges`. The driver's login (e.g. `chitin-orchestrator`)
  needs to be added to spec 113's allowlist
  (`copilotReviewerLogins`) so the webhook handler accepts it.

### Approval signal (US2)

- **FR-007** On Approve verdict (or ApproveWithComments where blockers
  is empty), `DispatchInternalReview` applies the `ready-to-merge`
  label via `gh pr edit --add-label`. The label is operator-visible in
  spec 114's queue (with a positive marker — not an escalation).
- **FR-008** Auto-merge is OUT OF SCOPE. The label is informational
  only; the operator's `gh pr merge` click is the merge authority.

### Confidence signal (US3)

- **FR-009** Extend the StructuredVerdict schema (spec 094 contract)
  with `confidence: "high" | "medium" | "low"`. Backward-compatible:
  existing reviewers that don't populate the field default to
  `medium`.
- **FR-010** Spec 114 FR-008 reason taxonomy extends with
  `internal_rereview_low_confidence` — a `low` confidence Approve
  applies BOTH `ready-to-merge` and `chitin-escalated/low-confidence`
  labels.

### Telemetry

- **FR-011** Chain events:
  - `internal_rereview_started { pr_number, round, reviewer_driver, fixup_sha }`
  - `internal_rereview_completed { pr_number, round, reviewer_driver, verdict, confidence, blockers_count }`
  - `internal_rereview_skipped { pr_number, reason }`

## Success criteria

- **SC-001** Across 10 chitin-authored PRs that receive Copilot review
  + fixup, ≥ 8 advance to a definitive state (Approve label OR
  iteration_cap_hit escalation) within 30 min of first fixup —
  without operator action. Today's baseline: ~0/10 (Copilot doesn't
  re-review, PRs sit).
- **SC-002** False-positive Approve rate (Approve verdict that
  operator overrides) ≤ 10% on a 20-PR sample. Higher means the
  re-review driver is rubber-stamping; tighten the prompt or raise the
  confidence requirement for label application.
- **SC-003** Median latency from fixup-commit push to
  `internal_rereview_completed` chain event ≤ 90 seconds.

## Scope

### In scope

- Sequential `DispatchInternalReview` activity called by spec 113's
  iteration workflow on PushedFixup paths
- Reviewer pool config + spec 094 R-AUTHORID enforcement
- Structured verdict → PR review via gh
- Confidence signal extension to StructuredVerdict
- `ready-to-merge` label + spec 114 queue integration

### Out of scope

- Auto-merge (`gh pr merge` on Approve) — the operator clicks
- Cross-PR review (one review per fixup, one fixup per round)
- Re-review of NON-fixup commits (only fires after spec 113 pushed
  one)
- Multi-reviewer aggregation per round (one driver per round; spec
  094's pool-aggregation is for the INITIAL review, not iteration)

## Edge cases

- **Empty reviewer pool after R-AUTHORID exclusion:** `internal_rereview_skipped
  { reason: "empty_pool" }`. The PR remains at "fixup pushed, no
  internal verdict" — same end state as today's gap. Operator queue
  (spec 114) can surface it.
- **Re-review driver itself produces malformed verdict JSON:** same
  failure path as spec 094 FR-005 (`FailureMalformedJSON`). The
  posted PR review carries the failure marker; spec 113 dispatcher
  treats it as RequestChanges (safe default — keep iterating).
- **Confidence not in {high, medium, low}:** treat as medium (lenient
  default that still labels). Log warning.
- **Same review id collision:** the posted PR review gets a fresh
  GitHub review id. Spec 113's WorkflowID derivation
  (`iteration-pr-N-review-M`) uses the new id so the next round
  dedups correctly.
- **`ready-to-merge` label already present:** `gh pr edit --add-label`
  is idempotent. Re-running the activity is safe.

## Composability

- With **spec 094 (dialectic review):** reuses StructuredVerdict +
  R-AUTHORID. Future work could share the same `DispatchMachineReviewer`
  activity between initial-review and iteration-re-review (currently
  the iteration call is simpler — one driver, not a pool).
- With **spec 113 (iteration loop):** lives inside spec 113's workflow
  as a sequential follow-on to the fixup push. Closes the round-2
  trigger gap (Copilot's silence after fixup) by generating our own
  trigger event.
- With **spec 114 (operator queue):** new `ready-to-merge` label is a
  POSITIVE signal (PR is provably good); new
  `internal_rereview_low_confidence` reason is an ESCALATION signal
  (verdict approved but with uncertainty). Both render in the queue.
