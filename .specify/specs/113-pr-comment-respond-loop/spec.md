---
spec_id: 113
title: PR comment-respond loop — factory iterates Copilot reviews automatically
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 094
  - 098
  - 099
  - 112
related:
  - 114
---

# Spec 113 — PR comment-respond loop

## Why

As of 2026-05-25 the autonomous loop ships PRs (spec 098 + 112) and Copilot
reviews them, but **nothing in the factory reads the reviews or acts on
them.** The operator has to either read every PR thread themselves or merge
blind. Empirical evidence from the 2026-05-25 spec-109 + spec-110 cleanup:

  - 8 chitin-authored PRs (#1041-#1048) — every one received a Copilot
    review with 1-3 line comments
  - 16 unaddressed line comments across 7 PRs at the start of the
    cleanup session
  - 0 fixup commits from the factory — every comment required Jared (or
    Jared's main-conversation Claude) to read, evaluate, apply a fix,
    push the commit, and post a reply

Of the 16 comments, ~14 were valid (real bugs, valid concerns, doc
mismatches) and ~2 were noise / sibling-PR-deferred. Each fixup cycle
took ~10-15 minutes including review reading + code edit + test + push
+ reply. Total session cost: ~2 hours of operator equivalent attention
on routine review iteration that should be mechanical.

The implementation chain is complete. The review-iteration chain
doesn't exist. This spec closes it.

## User stories

### US1 (P1) — Factory iterates Copilot review comments to zero or escalation

> As the operator, when Copilot leaves comments on a chitin-authored PR,
> the factory automatically re-invokes the driver that authored the PR
> with the comment context. The driver either pushes a fixup commit
> resolving each comment OR replies on the comment thread explaining why
> no change is needed. The loop repeats up to N rounds; if any comment
> remains unresolved at the cap, the PR is escalated to the operator
> queue with a single "needs you" flag.

**Independent test:** Open a chitin-authored PR with a known-fixable
issue. Trigger a Copilot review. Within 5 minutes, observe a fixup
commit on the PR branch addressing the comment, and a reply on the
review thread. Verify the chain emits `pr_iteration_round_started`
followed by `pr_iteration_completed` with `action_counts.fix=1`.

### US2 (P2) — Operator can configure iteration cap per spec

> As a spec author, I can set `iteration_cap: N` in the spec frontmatter
> so iteration-heavy specs (e.g. design polish) get more rounds while
> simple specs (e.g. one-line fixes) stay at the default cap.

**Independent test:** Spec frontmatter sets `iteration_cap: 1`.
Dispatch produces a PR with comments requiring 2 rounds to resolve.
First round runs; second round skipped; PR escalates after the first
round.

### US3 (P3) — Human-reviewer comments don't escape iteration

> When a human reviewer (not Copilot) leaves a comment on a chitin-
> authored PR, the factory recognises this is a higher-trust review
> and escalates immediately rather than auto-iterating against it.

**Independent test:** Operator leaves a comment on a chitin-authored
PR. Factory does NOT spawn a `PRIterationWorkflow`; instead it
immediately emits `pr_iteration_escalated` with
`reason: "human_reviewer_present"`.

## Functional requirements

### Trigger (US1)

- **FR-001** Extend `factory-listen` `/webhook/pr` to recognise
  `pull_request_review.submitted` events on PRs whose head branch
  matches `chitin/wu/*` (factory-authored). Existing copilot-dispatch
  detection logic is unchanged.
- **FR-002** Eligibility: review state is `COMMENTED` or
  `CHANGES_REQUESTED`; reviewer is in the configured allowlist
  (default: `copilot-pull-request-reviewer`). Non-allowlisted human
  reviewers route to US3.
- **FR-003** Dispatch a new `PRIterationWorkflow` per eligible review,
  with deterministic WorkflowID `iteration-pr-<N>-review-<M>` so
  duplicate webhook deliveries dedup via Temporal
  `REJECT_DUPLICATE` (same pattern as spec 112 US2's WorkflowID).

### Iteration (US1)

- **FR-004** `PRIterationWorkflow` invokes the same driver that
  authored the original PR (looked up by the `sched/run/<id>` label
  combined with the work-unit branch slug). Uses
  `worktree.Manager.Checkout` (spec 112 US2) to mint a worktree on
  the PR branch.
- **FR-005** Prompt template includes: PR diff, every unaddressed
  comment with file+line+body, the original task description, and a
  required output format (one JSON envelope per comment with
  `{id, action: "fix"|"reply", reply_body?: ...}`).
- **FR-006** Driver output is split: code changes become a fixup
  commit (`git add -A && git commit -m "review fix: ..."`) pushed
  with `--force-with-lease`; reply bodies become PR review thread
  replies via the GitHub review-comment reply API — keyed by the
  comment id and NOT the PR number, so the exact `gh api` invocation
  is `gh api -X POST repos/<owner>/<repo>/pulls/comments/<comment_id>/replies -f body=...`.
  Implementers MUST use `repos/<owner>/<repo>/...` for every gh-api
  call in this spec; bare `/pulls/...` paths are rejected by gh as
  invalid.
- **FR-007** Iteration round count is carried in the workflow state.
  Cap default is **3 rounds**; spec frontmatter `iteration_cap`
  overrides.

### Escalation (US1, US3)

- **FR-008** When rounds reach cap and ≥1 comment is still unaddressed
  (no fixup committed AND no reply posted), emit
  `pr_iteration_escalated` chain event and apply PR label
  `chitin-escalated/comment-cap`.
- **FR-009** When a human reviewer comment is detected (FR-002 mismatch),
  emit `pr_iteration_escalated` immediately with
  `reason: "human_reviewer_present"` and apply label
  `chitin-escalated/human-reviewer`.

### Telemetry

- **FR-010** Chain events (closed taxonomy — implementers MUST NOT invent
  additional event types; each terminal state in the Edge cases below
  maps to exactly one of these):
  - `pr_iteration_round_started { pr_number, round, reviewer, comment_count }`
  - `pr_iteration_completed { pr_number, round, fixup_sha, replies_posted, action_counts: {fix, reply, skip} }`
  - `pr_iteration_failed { pr_number, round, failure_kind, detail }` (driver fault / push fault)
  - `pr_iteration_escalated { pr_number, rounds_attempted, last_review_id, reason }`
  - `pr_iteration_skipped { pr_number, reason }` (PR terminal mid-iteration,
    duplicate-webhook no-op, or other early-exit no-ops — see edge cases)
- **FR-011** Canonical `reason` strings used in `pr_iteration_escalated`
  events. The set is closed; spec 114's queue-filter taxonomy (FR-008)
  MUST be the same vocabulary string-for-string:
  - `iteration_cap_hit` — FR-008 cap reached with ≥1 unaddressed comment
  - `human_reviewer_present` — FR-009 non-allowlisted reviewer detected
  - `lease_lost` — force-push lost its lease (driver-side fault that
    promotes to escalation immediately, NOT a separate event type)
  - `iteration_completed_with_skips` — round completed cleanly but
    `action_counts.skip > 0` (driver ducked one or more comments);
    surfaces in spec 114's queue under this reason kind

## Success criteria

- **SC-001** Re-running the 2026-05-25 scenario (specs 109/110 dispatch
  with their 8 PRs and 16 Copilot comments) produces zero operator
  attention required until ALL PRs either auto-merge or escalate.
- **SC-002** Median time from `pull_request_review.submitted` to
  `pr_iteration_completed`: ≤ 5 minutes.
- **SC-003** Iteration is idempotent: a duplicate webhook delivery
  results in zero additional fixup commits and zero additional
  workflow runs (Temporal `REJECT_DUPLICATE`).

## Scope

### In scope

- Factory-listen extension for `pull_request_review.submitted`
- `PRIterationWorkflow` + `IterateReviewComments` activity
- Driver re-invocation against the PR branch
- Reply-thread posting via `gh api`
- Iteration-cap enforcement + escalation labels
- Chain events for full observability

### Out of scope

- Resolving disagreements between Copilot and the driver (the driver
  may "reply" with a "won't fix" — Copilot may not be convinced; that's
  fine, the operator escalation surface (spec 114) handles those)
- Multi-reviewer aggregation (one workflow per review)
- Cross-PR review propagation (one PR at a time)
- Human-comment iteration (US3 explicitly does NOT iterate against
  humans — escalates instead)

## Edge cases

- **Duplicate webhook delivery for the same review:** Temporal
  `REJECT_DUPLICATE` policy on `iteration-pr-<N>-review-<M>` rejects
  the second `ExecuteWorkflow` call; dispatcher catches the
  `WorkflowExecutionAlreadyStarted` (same pattern as spec 112 US2)
  and emits `pr_iteration_skipped { reason: "duplicate_delivery" }`.
- **Driver produces no output / "I see nothing to fix":** record as
  `pr_iteration_completed { action_counts: {fix: 0, reply: 0, skip:
  N} }` (where N is the number of comments the driver skipped). Once
  the round completes, if `action_counts.skip > 0` AND the round was
  the FIRST round (so this isn't a normal cap-hit), additionally emit
  `pr_iteration_escalated { reason: "iteration_completed_with_skips" }`
  so spec 114's queue surfaces the PR for operator eyeballs under that
  reason kind (114 FR-008 must include this kind).
- **Force-push fails (lease lost to a concurrent operator push):**
  emit `pr_iteration_failed { failure_kind: "lease_lost" }` AND
  `pr_iteration_escalated { reason: "lease_lost" }` (the
  failure-event is the diagnostic record; the escalation event is
  what spec 114 keys off). Operator now has divergent state to
  resolve.
- **PR is closed / merged mid-iteration:** workflow no-ops on next
  activity, emits `pr_iteration_skipped { reason: "pr_terminal" }`.
- **Comment thread has already-replied entries:** activity skips
  threads with an existing chitin reply (identified by author
  `chitin-orchestrator`); these skips do NOT count toward
  `action_counts.skip` since they are by-design no-ops, not driver
  ducks.

## Composability with spec 114

113 produces the "factory handled it" bucket implicitly: PRs that have
`pr_iteration_completed` events with `action_counts.fix > 0` and no
escalation event. 114's queue filter is the inverse — surface only PRs
where the factory didn't handle it. Together they collapse the operator's
PR review queue to "what needs you" instead of "all PRs."
