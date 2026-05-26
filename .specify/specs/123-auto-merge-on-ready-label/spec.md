---
spec_id: 123
title: Auto-merge on chitin/ready-to-merge label — close the loop after internal re-review
status: Draft
owner: chitinhq
created: 2026-05-26
depends_on:
  - 099
  - 114
  - 116
related:
  - 094
  - 112
  - 113
  - 119
  - 121
---

# Spec 123 — Auto-merge on ready-to-merge label

## Why

Spec 116 (internal re-review) lands the `chitin/ready-to-merge`
label on PRs that pass the multi-driver verdict. The label
exists as the explicit machine-readable signal that "this PR
has cleared every quality gate the factory knows how to
apply." The comment at
`activities/internal_rereview_post.go:16` calls it out
verbatim: the label exists "for any future auto-merge worker."

**Today, nothing acts on the label.**

The concrete consequence — observed on 2026-05-26 with PR
#1135 (spec 121's whole-spec impl run):

  - Codex T4 driver shipped 1,237 lines of implementation
    covering 20 of 21 tasks (T021 deferred for sound reason)
  - Spec 113 PR-iteration loop fired at 03:12:54 UTC,
    addressed all 4 Copilot review comments via fixup commit
    `b058c05d1`
  - All 7 CI checks green
  - The operator (Jared, mid-day on 2026-05-26) had to read,
    judge, and merge the PR manually — every step before that
    was autonomous

The loop ended one merge action short of closing. That one
action is exactly the kind of mechanical signal-driven
operation that doesn't need operator judgement when the
upstream gates have already done their job. Spec 116 is the
judgement. Spec 123 is the lever.

**Why now, not earlier.** Two operational reasons:

  1. **The label-applying path didn't exist until spec 116
     shipped** (2026-05-25). Auto-merge without a trustworthy
     label-applier would be auto-merging on a fake signal —
     dangerous. Spec 116 closed the trust gap; spec 123 cashes
     it in.
  2. **The PR-iteration loop didn't exist until spec 113
     shipped** (2026-05-25). Auto-merge without a fixup loop
     would just thrash PRs through reviewer cycles indefinitely
     when a real issue exists. Spec 113 gives the loop a
     terminating condition; spec 123 acts on the terminal
     state.

The substrate is now in place. The missing piece is mechanical.

**Why this is small, not big.** A naive read of "auto-merge"
suggests a complex bot. The actual scope is one Temporal
workflow + one webhook event subscription + one new chain
event family + a small spec 114 escalation extension. The
"complex part" — deciding when a PR is mergeable — is already
done by spec 116. This spec just **moves bytes**: when a label
fires, read CI status, if green, `gh pr merge --squash`.

## User stories

### US1 (P1) — Labeled + CI green → auto-merge within minutes

> As the autonomous loop, when spec 116's
> `ApplyReadyToMergeLabel` lands the
> `chitin/ready-to-merge` label on a PR whose CI checks
> are already green and which is otherwise mergeable
> (no conflicts, not draft), the auto-merge workflow
> squash-merges the PR with `--delete-branch` within
> 60 seconds of the label-applied webhook landing. The
> PR sequence concludes with no operator action.

**Independent test:** Stage a fixture PR with green CI,
non-draft, mergeable, and apply the `chitin/ready-to-merge`
label. Within 60 seconds, the PR's `state` becomes `MERGED`,
the head branch is deleted, and the chain carries a
`auto_merge_succeeded` event with the PR number and the
merge SHA.

### US2 (P1) — Labeled but CI pending → wait, then merge

> As the autonomous loop, when the label is applied before
> CI completes (a normal race: the spec 116 re-review and the
> CI may finish in either order), the workflow waits for CI
> to complete and merges once it goes green, up to a
> configurable timeout (default 1 hour). The wait uses
> Temporal timers — no busy-poll, no wasted compute.

**Independent test:** Stage a fixture PR with CI pending,
apply the label. The workflow records `auto_merge_waiting`;
when CI completes green within the timeout window, the
workflow proceeds and emits `auto_merge_succeeded`. The
timer fires deterministically in the Temporal testsuite env.

### US3 (P1) — CI failed → unlabel + comment + escalate

> As the operator who comes back to a PR that auto-merge
> tried to handle, if CI failed (or any required check
> reported a non-success conclusion), the workflow MUST
> remove the `chitin/ready-to-merge` label, post a single
> structured PR comment naming the failed check(s) and the
> reason auto-merge stepped back, and escalate via spec 114
> with reason `auto_merge_ci_failed`. Auto-merge MUST NOT
> retry on its own — re-applying the label is the
> operator's affirmative signal to try again.

**Independent test:** Stage a fixture PR with one failing
required check, apply the label. The workflow removes the
label within 30 seconds, posts exactly one PR comment whose
body matches the FR-006 template, and emits
`auto_merge_ci_failed` to the chain + `Notify` activity.
Re-applying the label triggers a fresh workflow (idempotent
on workflow id per FR-009).

### US4 (P1) — Merge conflict → unlabel + escalate

> As the operator, when the PR is labeled with green CI but
> a merge conflict exists against `main`, auto-merge MUST
> NOT attempt to resolve the conflict or rebase. It removes
> the label, posts a PR comment naming the conflict, and
> escalates via spec 114 with reason `auto_merge_conflict`.
> Conflict resolution is operator territory; spec 112's
> sibling-rebase mechanism handles the orthogonal case of
> "another PR merged ahead of you cleanly."

**Independent test:** Stage a fixture PR with a synthetic
merge conflict against `main`, apply the label. The
workflow detects the unmergeable state, removes the label,
posts the conflict comment, emits `auto_merge_conflict`. No
attempt is made to resolve, rebase, or close the PR.

### US5 (P1) — CI hangs past timeout → escalate, don't merge

> As the operator, when CI never reaches a terminal state
> within the configured timeout (default 1 hour), the
> workflow MUST give up cleanly: remove the label, post a
> comment, escalate via spec 114 with reason
> `auto_merge_ci_timeout`. A hanging CI is never auto-
> merged on assumption.

**Independent test:** Stage a fixture PR with CI in a
permanent `pending` state, apply the label, advance the
Temporal testsuite clock past the timeout. The workflow
gives up exactly once, no merge happens, the chain emits
`auto_merge_ci_timeout`.

### US6 (P2) — Label removed mid-flow → workflow exits cleanly

> As the operator, if I remove the `chitin/ready-to-merge`
> label while the workflow is waiting (for CI, for the
> next poll, for anything), the workflow MUST observe the
> removal and exit cleanly without merging — the label is
> the consent signal, removing it is consent withdrawn.
> No comment, no escalation: this is a routine operator
> action.

**Independent test:** Stage a fixture PR labeled while CI
is pending. Mid-flow, remove the label via a synthetic
`unlabeled` webhook. The workflow exits with
`auto_merge_canceled` (chain event), no merge, no escalation
notify.

### US7 (P2) — Duplicate `labeled` events coalesce

> As the autonomous loop, when GitHub fires multiple
> `labeled` events for the same PR (a retry, a flapping
> label, etc.), the workflow MUST NOT double-merge. The
> WorkflowID is deterministic per PR, the reuse policy
> rejects concurrent duplicates, and a duplicate event
> after a successful merge becomes a no-op with a single
> `auto_merge_already_settled` chain event.

**Independent test:** Fire two `labeled` events for the
same PR within 1 second. Assert exactly ONE merge attempt
(by chain event count), one `auto_merge_succeeded`, and
one `auto_merge_already_settled` for the duplicate.

## Functional requirements

- **FR-001** A new Temporal workflow `AutoMergeWorkflow`
  MUST be added at `go/orchestrator/workflows/auto_merge.go`.
  Input: `AutoMergeInput{Repo, PRNumber, LabelName,
  TriggerEventID}`. Output: `AutoMergeResult{Outcome,
  MergeSHA, FailureReason}`. The workflow is durable —
  on worker restart, in-flight `AutoMergeWorkflow` runs
  resume from history without re-triggering side effects.

- **FR-002** The webhook handler at
  `cmd/chitin-orchestrator/factory_listen.go` (spec 099's
  `/webhook/pr` route) MUST extend its event dispatch to
  start `AutoMergeWorkflow` when the incoming event is
  `pull_request.labeled` AND `label.name ==
  "chitin/ready-to-merge"` (the constant from spec 116
  `activities/internal_rereview_post.go:19`). Existing
  routes (`pull_request_review`, `issue_comment`) MUST be
  unaffected.

- **FR-003** The workflow MUST re-validate the label is
  STILL applied at the start of execution (the label
  could have been removed in the seconds between webhook
  receipt and workflow start). If not present, emit
  `auto_merge_canceled` and exit.

- **FR-004** The workflow MUST check CI status via `gh pr
  view <N> --json statusCheckRollup,mergeStateStatus,
  mergeable,isDraft`. CI is `green` iff every entry in
  `statusCheckRollup` has `conclusion == "SUCCESS"` and
  none has `status == "PENDING"`. CI is `failed` iff any
  entry has `conclusion ∈ {"FAILURE", "TIMED_OUT",
  "CANCELLED", "ACTION_REQUIRED"}`. CI is `pending`
  otherwise (including the case where the rollup is empty,
  which means checks haven't started yet).

- **FR-005** When CI is `pending`, the workflow MUST sleep
  via `workflow.Sleep` (Temporal timer) and re-check.
  Backoff: 60s, 120s, 240s, 480s, cap at 480s thereafter.
  Total wall-clock cap: `merge_timeout_seconds` (default
  3600 = 1 hour). On timeout: emit `auto_merge_ci_timeout`,
  remove label, comment, escalate.

- **FR-006** PR comments emitted by the workflow MUST use
  a fixed template per failure mode. The set of templates
  is closed and named: `ci_failed`, `merge_conflict`,
  `ci_timeout`. Each template renders the matched check
  names (for `ci_failed`), the conflict file count (for
  `merge_conflict`), or the elapsed wait time (for
  `ci_timeout`). All comments include a one-line "auto-
  merge stepped back; re-apply the label to retry"
  footer.

- **FR-007** The label-removal action MUST use `gh pr edit
  <N> --remove-label "chitin/ready-to-merge"`. The
  workflow MUST tolerate the case where the label is
  already gone (concurrent operator action) — no error,
  log only.

- **FR-008** The merge action MUST use `gh pr merge <N>
  --squash --delete-branch`. The squash convention matches
  chitin's existing merge style (see recent commits on
  `main`). The `--delete-branch` matches the existing
  cleanup posture. The merge MUST emit
  `auto_merge_succeeded` with the merge commit SHA in
  the payload; on `gh` failure, emit `auto_merge_failed`
  with the stderr tail (capped at 1 KiB).

- **FR-009** The `WorkflowID` MUST be deterministic per
  `(PR, GitHub delivery_id)`:
  `auto-merge-pr-<N>-<TriggerEventID>` where `TriggerEventID`
  is the `X-GitHub-Delivery` header from the `labeled`
  webhook (already carried on `AutoMergeInput` per FR-001).
  Rationale: GitHub redelivers the SAME `labeled` event with
  the SAME `delivery_id` on at-least-once retries — these
  must collide on WorkflowID. A genuinely new label event
  (e.g., the operator re-labels after a prior failure)
  carries a NEW `delivery_id` → distinct WorkflowID →
  fresh workflow, sidestepping spec 099's R3 concern about
  bare `auto-merge-pr-<N>` not handling post-completion
  redelivery cleanly. The `WorkflowIDReusePolicy` MUST be
  `RejectDuplicate` so a true redelivery during an in-flight
  workflow rejects cleanly (Temporal error; the webhook
  handler swallows it and emits `auto_merge_already_running`).
  Post-completion redeliveries (same `delivery_id`) are
  additionally guarded by a chain-event dedup lookup for
  `auto_merge_triggered` with the same `trigger_event_id`,
  emitting `auto_merge_already_settled` and returning
  without starting a new workflow.

- **FR-010** All failure paths (`ci_failed`, `conflict`,
  `timeout`, generic `gh pr merge` failure such as missing
  required review) MUST route through the existing
  `DiscordNotify` activity (spec 080, reused by spec 114
  FR-009 — see `go/orchestrator/activities/notify.go`).
  The chain reason kind is drawn from a closed set of FOUR
  new values added by this spec to the FR-008 reason
  taxonomy in `go/orchestrator/internal/queue/reason.go`:
  `auto_merge_ci_failed`, `auto_merge_conflict`,
  `auto_merge_ci_timeout`, `auto_merge_failed` (the
  catch-all for non-CI `gh pr merge` failures per the
  Edge cases section).

- **FR-011** Closed event taxonomy for this spec
  (FR-014's data model lists payload schemas):
  `auto_merge_triggered`, `auto_merge_waiting`,
  `auto_merge_canceled`, `auto_merge_succeeded`,
  `auto_merge_failed`, `auto_merge_ci_failed`,
  `auto_merge_conflict`, `auto_merge_ci_timeout`,
  `auto_merge_already_running`, `auto_merge_already_settled`.
  All payloads carry `repo` + `pr_number` at minimum;
  per-event additions are listed below.

- **FR-012** The workflow MUST be registered on the
  orchestrator worker at startup. The registration MUST
  produce a one-line entry in the worker boot log
  (analogous to spec 121's `blob_inline_threshold` boot
  log line) confirming `AutoMergeWorkflow` is loaded.
  Operators reading `journalctl -u chitin-orchestrator |
  head` can confirm "the auto-merge workflow is loaded"
  without reading source code.

- **FR-013** A `chitin-orchestrator auto-merge status
  <PR>` CLI subcommand is introduced by this spec.
  Output: a compact table reporting the most recent
  `auto_merge_*` event chain for the PR (timestamps,
  outcomes, reasons). Exit codes: 0 if last terminal
  event was `auto_merge_succeeded`, 2 if it was a failure
  reason, 3 if no auto-merge events exist for this PR.

- **FR-014** Auto-merge MUST be disablable via env
  var `CHITIN_AUTO_MERGE_DISABLED=1`. When disabled, the
  webhook handler logs the receipt of a `labeled` event
  but does NOT start the workflow. This is the
  break-glass — auto-merge gone wrong should be killable
  without a redeploy.

- **FR-015** Canonical reason taxonomy for the
  `auto_merge_canceled` event's payload. The `reason`
  field MUST be a closed-set value drawn from:
    - `label_removed_pre_flight` — the label was already
      gone by the time the workflow checked at start
    - `label_removed_mid_wait` — the label was removed
      via webhook signal while the workflow was waiting
      on CI
    - `pr_closed_pre_flight` — the PR was closed before
      the workflow's first mergeability check
    - `pr_is_draft` — the PR is in draft state (spec
      116 shouldn't label drafts; a manual operator
      label might)
  The set is intentionally small (four ways auto-merge
  bows out gracefully without escalating); extending it
  requires a spec amendment.

## Success criteria

- **SC-001** Within 7 days of deployment, ≥ 5 PRs reach
  the merged state via auto-merge with no operator
  action between spec 116's label and the merge. Measured
  by chain query: count of `auto_merge_succeeded` events
  whose preceding `auto_merge_triggered` event's
  `manual_label: false` (per FR payload).

- **SC-002** Zero double-merges across 30 days. A
  double-merge is defined as two `auto_merge_succeeded`
  events for the same `pr_number` within any window.
  Measured by chain aggregation.

- **SC-003** Operator-touch-time per shipped PR drops
  to zero on the happy path. Measured by absence of
  `gh pr merge` commands in the operator's shell history
  for chitin/ repos within the 7-day measurement window.

- **SC-004** Every escalation surface (CI failed,
  conflict, timeout) lands in Discord within 60 seconds
  of the detection. Measured by chain `ts` delta between
  the failure event and the corresponding `Notify`
  activity success log.

- **SC-005** Re-labeling after a prior failure starts
  a fresh workflow successfully — the reuse policy
  doesn't accidentally block legitimate retries.
  Measured by a manual retry test in week one and a
  chain query for re-labeled PRs across 30 days.

## Scope

In:
  - `workflows/auto_merge.go` — the durable workflow
  - `activities/auto_merge_check.go` — `CheckPRMergeability`
    deterministic activity (gh pr view JSON parse + logic)
  - `activities/auto_merge_act.go` — `MergePR`,
    `UnlabelPR`, `CommentPR` deterministic activities
  - `cmd/chitin-orchestrator/factory_listen.go` — extend
    the `pull_request` event dispatch per FR-002
  - `cmd/chitin-orchestrator/auto_merge.go` — new
    `auto-merge status <PR>` subcommand per FR-013
  - Spec 114 reason taxonomy extension — four new
    closed values per FR-010 (`auto_merge_ci_failed`,
    `auto_merge_conflict`, `auto_merge_ci_timeout`,
    `auto_merge_failed`)
  - Chain emit sites — 10 event types per FR-011
  - Comment template constants — three named templates
    per FR-006
  - Tests + documentation + runbook

Out:
  - Merge methods other than squash. Future operators
    who want `--merge` or `--rebase` can extend; the
    default reflects chitin's existing style.
  - Auto-resolving merge conflicts. Spec 112 handles the
    sibling-rebase case (clean rebase onto fresh main);
    semantic conflicts remain operator territory.
  - Auto-merge of non-chitin-authored PRs. The label is
    applied by spec 116 only on chitin-authored PRs (the
    re-review pipeline doesn't run on operator-authored
    PRs today). If/when the re-review pipeline extends,
    auto-merge inherits the new coverage without code
    changes — the label is the contract.
  - Auto-merge of spec PRs (vs impl PRs). Both kinds get
    the label from spec 116 when they pass — auto-merge
    treats them identically.
  - Bypass branch protection. The merge MUST go through
    GitHub's normal branch-protection rules (required
    checks, required reviews); if a rule blocks the
    merge, `gh pr merge` fails and the workflow escalates.
    Auto-merge is NOT a privilege escalation surface.
  - Manual operator override flag ("auto-merge this PR
    without label"). The label is the consent signal.
    Operators who want a one-off override use `gh pr
    merge` directly.

## Data model

Chain event payloads (closed schema per FR-011):

```
auto_merge_triggered: {
  repo: string,           // "chitinhq/chitin"
  pr_number: int,
  workflow_id: string,    // "auto-merge-pr-<N>-<TriggerEventID>" per FR-009
  trigger_event_id: string, // GitHub X-GitHub-Delivery header value
  manual_label: bool      // true iff the label was applied by a human,
                          // false if by spec 116's activity (heuristic:
                          // label-event actor's login matches a known
                          // bot/orchestrator identity)
}

auto_merge_waiting: {
  repo, pr_number,
  elapsed_seconds: int,
  ci_state: "pending"     // FR-004 closed CI taxonomy is green|failed|pending;
                          // an empty rollup IS pending per FR-004, so no
                          // separate "absent" value is needed
}

auto_merge_canceled: {
  repo, pr_number,
  reason: "label_removed_pre_flight"   // FR-015 canonical set
        | "label_removed_mid_wait"
        | "pr_closed_pre_flight"
        | "pr_is_draft"
}

auto_merge_succeeded: {
  repo, pr_number,
  merge_sha: string,      // the squash commit oid
  head_branch_deleted: bool
}

auto_merge_failed: {
  repo, pr_number,
  stderr_tail: string     // ≤ 1 KiB, the `gh pr merge` stderr tail
}

auto_merge_ci_failed: {
  repo, pr_number,
  failed_checks: [string] // names of checks with non-SUCCESS conclusion
}

auto_merge_conflict: {
  repo, pr_number,
  base_ref: string,
  conflict_file_count: int  // best-effort, 0 if not parseable
}

auto_merge_ci_timeout: {
  repo, pr_number,
  waited_seconds: int,
  timeout_seconds: int,
  last_ci_state: "pending"   // same closed CI taxonomy as auto_merge_waiting;
                             // empty rollup IS pending per FR-004
}

auto_merge_already_running: {
  repo, pr_number,
  conflicting_workflow_id: string
}

auto_merge_already_settled: {
  repo, pr_number,
  prior_outcome: "succeeded" | "failed" | "ci_failed" | "conflict" | "ci_timeout" | "canceled"
}
```

## Edge cases

  - **Label applied AND CI green at the same instant** —
    both webhooks land within 100ms. The `labeled` event
    starts the workflow; the workflow's FR-004 check
    sees green CI immediately and merges. The
    `check_suite.completed` event sees the workflow
    already running and is a no-op (no separate dispatch
    needed; this spec subscribes to `labeled` only).
  - **PR closed between label and merge.** FR-003 also
    validates the PR is `OPEN` — closed PRs emit
    `auto_merge_canceled` with reason `pr_closed_pre_flight`
    (part of the FR-015 canonical four-reason set; payload
    matches the `auto_merge_canceled` schema in the data
    model).
  - **Label is applied to a draft PR.** FR-003 includes
    a draft check; emit `auto_merge_canceled` with reason
    `pr_is_draft`. Spec 116 shouldn't label drafts, but
    a manual operator label might.
  - **Required reviewer hasn't approved.** GitHub's
    branch protection refuses the merge; `gh pr merge`
    returns non-zero. The stderr will mention "review
    required". The workflow emits `auto_merge_failed`
    with the stderr tail. The escalation reason is
    `auto_merge_failed` (not a specific
    `auto_merge_review_missing` — the stderr already
    names the cause; not worth a separate enum value).
  - **Merge succeeds but `--delete-branch` fails.** The
    PR is merged; the branch dangles. Emit
    `auto_merge_succeeded` with `head_branch_deleted:
    false`. Spec 112's sibling-rebase will not be
    affected. A periodic branch-sweeper can clean up
    (future spec, out of scope here).
  - **Webhook arrives before the workflow worker is up.**
    GitHub retries `pull_request.labeled` deliveries on
    non-2xx; the receiver returns 503 if the workflow
    starter is unavailable. The webhook handler MUST log
    the delivery_id so operators can correlate retries.
  - **Workflow worker crashes mid-execution.** Temporal
    resumes from history; the activity that was in
    flight may re-run. Activities MUST be safely
    re-runnable: `gh pr view` is idempotent; `gh pr
    merge` is idempotent on already-merged PRs (returns
    a clean exit); `gh pr edit --remove-label` is
    idempotent (the workflow already tolerates absent
    labels per FR-007); `gh pr comment` is NOT
    idempotent — repeated activity runs would post
    duplicate comments. The activity MUST guard with a
    chain-lookup check ("did this workflow_id already
    post this template?") to dedup. This is the only
    activity needing dedup logic.
  - **CHITIN_AUTO_MERGE_DISABLED=1.** FR-014 break-glass.
    Receiving the webhook produces a log line + no
    workflow start. No chain event (otherwise the kill
    switch would generate noise).
  - **PR labeled by something OTHER than spec 116
    (e.g. manual operator application).** Treat
    identically. `manual_label: true` in the triggered
    event's payload (FR-011) is a hint for analytics,
    not a behavior gate. The label is the contract.

## Composability

  - **Spec 099** (webhook handler) — this spec extends
    the existing `/webhook/pr` route to dispatch on
    `pull_request.labeled`. No new endpoint.
  - **Spec 116** (internal re-review) — produces the
    label this spec consumes. The two specs together
    are the producer/consumer pair for the
    `chitin/ready-to-merge` contract.
  - **Spec 113** (PR iteration loop) — orthogonal but
    composes: 113 produces fixup commits that change
    CI status. Auto-merge's FR-004 re-checks CI on
    every cycle, so a 113 fixup that lands while
    auto-merge is waiting flows naturally into the
    merge.
  - **Spec 094** (dialectic review) — also a producer
    of the label (when the dialectic produces an
    all-approve verdict, 116's
    `ApplyReadyToMergeLabel` activity fires). Same
    consumer relationship.
  - **Spec 112** (sibling-rebase) — fires AFTER a
    chitin merge, so auto-merge becomes a new trigger
    for sibling-rebase fan-out. No interface change;
    spec 112 already keys on any chitin-authored
    merge to main.
  - **Spec 114** (operator escalation surface) — three
    new closed-taxonomy reason values
    (`auto_merge_ci_failed`, `auto_merge_conflict`,
    `auto_merge_ci_timeout`); all failure paths land
    in the existing operator queue.
  - **Spec 119** (whole-spec dispatch) — orthogonal;
    auto-merge doesn't care whether the PR came from
    whole-spec or per-task dispatch.
  - **Spec 121** (blob store) — orthogonal; the events
    this spec emits are tiny, never need externalization.
  - **Spec 122** (report freshness canary) — orthogonal;
    parallel "deterministic activity + chain emit +
    escalation via 114" pattern. The two specs
    reinforce each other's design idioms.
