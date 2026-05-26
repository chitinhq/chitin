---
spec_id: 127
title: Workflow input-validation guard — reject placeholder repo inputs at start (fail-fast over retry-forever)
status: Draft
owner: chitinhq
created: 2026-05-26
depends_on:
  - 099
related:
  - 070
  - 094
  - 098
---

# Spec 127 — Workflow input-validation guard

## Why

On 2026-05-26 at 14:00 EDT, operator triage of the production
orchestrator found **198 PRReviewWorkflow runs stuck in
`CapturePRSnapshot` retry loops**, the oldest started 2026-05-24
15:19 UTC (≈ 47 hours of zombie state). All 198 carried
fixture inputs:

  - `{"repo":"owner/name","pr_number":100,"pr_author":"copilot",...}`
  - `{"repo":"o/r","pr_number":200,...}`
  - `{"repo":"o/r","pr_number":400,...}`

Per-workflow retry counts ranged from 23 to 797 attempts. The
combined retry traffic exhausted the operator's GitHub GraphQL
rate limit (5000/hr/user) during routine work, forcing a
20-minute pause mid-merge and locking out legitimate
`gh pr merge` calls until the rate-limit window reset.

**The leak path:** `factoryHandler{}` is constructed in
`cmd/chitin-orchestrator/factory_listen_pr_test.go` without
setting `temporalHost`. The test calls `h.handlePR(w, req)`
with a synthetic webhook body carrying
`repository.full_name: "owner/name"`,
`number: 100`, and the
`chitin-dispatch` + `driver:copilot` labels (the eligibility
contract). `handlePR` calls `dispatchPRReview` with
`TemporalHost: h.temporalHost` (empty). `dispatchPRReview`
defaults its dialer to `dialTemporalAsStarter`, which falls
back to `$TEMPORAL_HOSTPORT` when `host` is empty. On the
operator's dev box (where `$TEMPORAL_HOSTPORT` is set to the
production worker), `go test ./cmd/chitin-orchestrator/`
silently starts real PRReviewWorkflows in production
Temporal with placeholder repos. The workflows then retry
forever because `gh pr view "owner/name"` always 404s.

**Why a guard, not just "fix the test":**

  - The test fix (inject a stub dialer, or set
    `temporalHost` to an unroutable value) closes ONE leak
    path. A future test, a misconfigured operator script, a
    bad webhook payload, or a malformed CLI invocation can
    open new ones. The workflow itself shouldn't trust its
    own input.
  - 47 hours of silent failure is the bigger lesson. A
    workflow whose first activity 404s 800 times should
    self-terminate, not retry-forever — but absent a
    semantic "this input is invalid" signal, Temporal's
    retry policy can't distinguish "API rate limit, retry
    later" from "this input will NEVER succeed."
  - Input validation at workflow start is a deterministic
    rejection point. It produces ONE failure event, ONE
    chain entry, and NO retries — the right shape for "the
    caller gave us garbage."

**Composability:**

  - **Spec 099** (GitHub-native dispatch) — defined
    `PRReviewWorkflow` and `prDispatchInput`. This spec
    adds the validator at the workflow entry point.
  - **Spec 094** (PR review workflow) — owns
    `PRReviewWorkflow`'s activity DAG; the validator
    runs before the first activity dispatches.
  - **Spec 098** (factory listen) — owns the upstream
    webhook handler that constructs the inputs; this
    spec is downstream-defensive (the validator catches
    inputs even when the upstream caller is wrong).
  - **Spec 070** (work-unit primitives) — the validator
    pattern is generic; this spec scopes it to
    `PRReviewWorkflow` first but FR-006 declares the
    contract that other dispatched workflows MUST adopt
    in follow-up specs (`SchedulerWorkflow`,
    `PRIterationWorkflow`, `SiblingRebaseWorkflow`).

## User stories

### US1 (P1) — Placeholder inputs fail-fast, not retry-forever

> As the orchestrator, when a `PRReviewWorkflow` is started
> with `repo` matching a known placeholder pattern (the
> closed set in FR-002), the workflow MUST reject the input
> at the first deterministic step (before any activity
> dispatches), emit ONE
> `workflow_input_rejected` chain event with the rejection
> reason, and complete with `WorkflowExecutionFailed`. No
> retries, no API calls, no log spam.

**Independent test:** Start `PRReviewWorkflow` with
`PRReviewInput{repo: "owner/name", pr_number: 100, ...}`.
Assert the workflow completes within 1 second with status
`Failed`, that exactly ONE `workflow_input_rejected` event
fires with payload `{repo: "owner/name", pr_number: 100,
reason: "placeholder_repo", workflow_type:
"PRReviewWorkflow"}`, and that ZERO
`CapturePRSnapshot` activity tasks were scheduled.

### US2 (P1) — Real repos still dispatch normally

> As the autonomous loop, a `PRReviewWorkflow` started with
> a real `owner/repo` (matching `^[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$`
> and NOT in the FR-002 placeholder set) MUST dispatch
> exactly as it does today — no behaviour change. The
> validator is a filter, not a gate that legitimate inputs
> must pass through.

**Independent test:** Start `PRReviewWorkflow` with
`PRReviewInput{repo: "chitinhq/chitin", pr_number: 1141,
...}`. Assert the workflow proceeds to dispatch
`CapturePRSnapshot` (asserting via Temporal testsuite that
the activity was scheduled), and that NO
`workflow_input_rejected` event fires.

### US3 (P2) — Operator runbook can identify + clean up future leaks

> As the operator triaging a "why is GitHub rate-limiting
> me?" incident, `chitin-orchestrator workflows list-zombies`
> reports any currently-running workflow whose input matches
> a placeholder pattern OR whose retry count exceeds a
> threshold (default 50). Output: a fixed-column table of
> WorkflowID + RunID + repo + retry-count + age. Exit codes:
> 0 if none, 2 if any found.

**Independent test:** Stage two running workflows in a test
namespace — one legitimate, one placeholder. Assert
`workflows list-zombies` lists only the placeholder one,
exit code 2.

### US4 (P2) — Cleanup subcommand respects the safety belt

> As the operator wanting to terminate leaked workflows,
> `chitin-orchestrator workflows terminate-zombies` walks
> the same set US3 lists and terminates each with reason
> `placeholder_repo_input_2026-05-26` (or a flag-overridable
> value). The command MUST refuse to terminate any
> workflow whose input is NOT a placeholder (the FR-002
> closed set) — i.e., the operator can't accidentally
> nuke legitimate runs even with a typo.

**Independent test:** Stage two running workflows as in US3.
Run `workflows terminate-zombies --dry-run`. Assert stdout
lists only the placeholder one. Run without `--dry-run`;
assert only the placeholder one is terminated; assert
the legitimate one is still running.

### US5 (P3) — Test-time leak path is closed at the source

> As the platform owner, the
> `cmd/chitin-orchestrator/factory_listen_pr_test.go` tests
> MUST construct `factoryHandler{}` with a non-empty
> `temporalHost` set to an unroutable sentinel
> (e.g., `unroutable.invalid:0`) OR set the
> `CHITIN_FACTORY_LISTEN_NO_DISPATCH=1` env var (a new
> shutoff this spec adds) so that `go test` against this
> package NEVER reaches `dialTemporalAsStarter`. This
> sec-belt is independent of FR-001-2 (the workflow
> validator); it removes the leak source so the validator
> is defense-in-depth, not first-line.

**Independent test:** Run the factory-listen-pr test suite
with `$TEMPORAL_HOSTPORT` set to the production worker host.
Assert ZERO new workflows appear in production Temporal's
`PRReviewWorkflow` count delta over the test run.

## Functional requirements

- **FR-001** A new `ValidatePRReviewInput` deterministic
  helper MUST be added at
  `go/orchestrator/internal/wfvalidate/pr_review.go`.
  Signature: `Validate(in PRReviewInput) error`. Returns
  a typed `*RejectError{Reason, Detail}` on rejection,
  `nil` on accept. The function is pure (no I/O, no
  globals); the regex set is built once at init.

- **FR-002** Closed placeholder-pattern set (the rejection
  taxonomy):
  ```
  placeholder_repos = {
    "owner/name",   // matches go's idiomatic example value
    "o/r",          // matches the abbreviated test fixture
    "test/repo",    // common docs example
    "example/example",
    "foo/bar",      // common throwaway
  }
  placeholder_pr_numbers = {0}    // unset/zero pr_number is invalid
  ```
  The set is intentionally small and exact-match. Future
  additions require a spec amendment (the closed-set
  posture matches spec 114's reason taxonomy and spec 121's
  ENV taxonomy). Pattern-match (vs exact-match) is
  explicitly OUT — a real repo named `test-corp/repo-1`
  shouldn't false-match.

- **FR-003** `PRReviewWorkflow` (at
  `go/orchestrator/workflows/pr_review.go`) MUST call
  `wfvalidate.Validate(input)` as its FIRST statement, before
  any `workflow.ExecuteActivity`. On `*RejectError`, the
  workflow MUST:
  1. Emit one `workflow_input_rejected` chain event with
     payload `{workflow_type: "PRReviewWorkflow",
     repo: input.Repo, pr_number: input.PRNumber,
     reason: err.Reason, detail: err.Detail,
     workflow_id: workflow_id}`.
  2. Return `temporal.NewNonRetryableApplicationError(
     err.Error(), "InputRejected", err)` — Temporal treats
     this as a terminal failure (no retries).

- **FR-004** The closed chain-event taxonomy for this spec
  is one event: `workflow_input_rejected`. Payload schema:
  ```
  {
    workflow_type: string,    // "PRReviewWorkflow" (per-workflow scoping)
    workflow_id: string,
    repo: string,
    pr_number: int,
    reason: string,           // FR-002 placeholder set member, OR
                              //   "missing_repo" | "missing_pr_number" |
                              //   "malformed_repo_slug"
    detail: string            // human-readable amplification
  }
  ```

- **FR-005** A `chitin-orchestrator workflows list-zombies`
  subcommand is introduced by this spec (US3). Output: a
  fixed-column table of `workflow_id`, `run_id`, `workflow_type`,
  `repo`, `pr_number`, `attempt_count`, `started_at`. Filter:
  `ExecutionStatus="Running"` AND (input matches FR-002
  placeholder set OR any pending activity has
  `attempt > --retry-threshold`, default 50). Exit codes:
  0 if zero rows, 2 if any rows.

- **FR-006** A `chitin-orchestrator workflows terminate-zombies`
  subcommand is introduced by this spec (US4). Walks the
  same set as FR-005, terminates each via Temporal client
  `TerminateWorkflow(ctx, workflowID, runID, reason, details)`
  with `reason` from `--reason` (default
  `placeholder_repo_input_<YYYY-MM-DD>`). The command MUST
  re-check each workflow's input before terminating; if the
  input is NOT in the FR-002 placeholder set (i.e., the
  caller is asking the command to terminate a legitimate
  workflow because of retry count alone), the command MUST
  refuse and require `--force-real-repo` to proceed. The
  `--force-real-repo` flag is documented as
  "DANGER: only use after operator triage".

- **FR-007** A new env shutoff
  `CHITIN_FACTORY_LISTEN_NO_DISPATCH=1` MUST cause
  `dispatchPRReview` to return early with `prDispatchResult{
  ReviewStarted: false, FailureKind:
  "dispatch_disabled_via_env"}` and emit NO chain event. This
  is the test-time guard (US5) — tests opt in via
  `t.Setenv`, ensuring no test run can accidentally start a
  real workflow even if the test forgets to inject a
  dialer. Production never sets this env var.

- **FR-008** `factory_listen_pr_test.go` MUST be updated
  (US5) so every test in the file either
  (a) sets `CHITIN_FACTORY_LISTEN_NO_DISPATCH=1` via
  `t.Setenv`, OR
  (b) injects a stub dialer through the existing
  `factoryHandler.dispatcher` injection seam (per spec
  098's slice 4 work).
  This is the only test-file change in scope; future tests
  for the same package MUST follow the same pattern (called
  out in the runbook).

- **FR-009** The closed FR-002 placeholder set is the
  CONTRACT for downstream specs. Follow-up specs adding
  validators to other dispatched workflows
  (`SchedulerWorkflow`, `PRIterationWorkflow`,
  `SiblingRebaseWorkflow`, `AutoMergeWorkflow` per spec 123)
  MUST reuse the same `wfvalidate` package and the same
  FR-002 set. Cross-workflow validators MUST NOT diverge —
  one taxonomy, one rejection vocabulary.

- **FR-010** The validator MUST also reject these structural
  cases (independent of the closed placeholder set):
  - `input.Repo == ""` → reason `missing_repo`
  - `input.PRNumber == 0` → reason `missing_pr_number`
  - `input.Repo` does NOT match
    `^[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$` → reason
    `malformed_repo_slug`
  These three cases capture the "obviously broken" inputs
  that aren't in the FR-002 closed placeholder set but
  still can't possibly succeed.

- **FR-011** Canonical reason taxonomy declared by this spec —
  the closed set of `reason` values that MAY appear in
  `workflow_input_rejected` event payloads (FR-004) and in
  `*RejectError` values returned by the validator (FR-001).
  Adding a new value requires a spec amendment.
  - `placeholder_repo` — input's `repo` matched a member of the
    FR-002 closed placeholder set (the original 2026-05-26
    leak shape)
  - `missing_repo` — input's `repo` was the empty string per
    FR-010
  - `missing_pr_number` — input's `pr_number` was zero per
    FR-010
  - `malformed_repo_slug` — input's `repo` did not match the
    GitHub slug regex per FR-010

## Success criteria

- **SC-001** Within 24 hours of deployment, a placeholder
  workflow start (e.g., from a CI rerun of the legacy
  test) produces ONE `workflow_input_rejected` event and
  ZERO `CapturePRSnapshot` retries. Measured by chain
  query for the event type + Temporal query for
  `ActivityTaskScheduled` count.

- **SC-002** Zero new zombie workflows (defined as
  `Running` PRReviewWorkflow with attempt count > 10) in
  the production namespace across 7 days post-deployment.
  Measured by `workflows list-zombies` returning empty.

- **SC-003** Operator GitHub GraphQL rate-limit
  utilization on the orchestrator's identity drops by
  ≥ 50% (the 198 zombies were each polling every minute).
  Measured by the daily 09:00 rate-limit snapshot from
  spec 114's digest.

- **SC-004** `workflows terminate-zombies --dry-run` is
  the documented runbook for any future leak — measured
  by referencing it in the cleanup section of the
  operator runbook (FR docs).

- **SC-005** No regression in legitimate
  `PRReviewWorkflow` starts — measured by counting the
  PRReviewWorkflow starts attributable to real `chitinhq/*`
  repos in the 7-day post-deployment window vs the
  7-day pre-deployment window, accepting natural variance.

## Scope

In:
  - `go/orchestrator/internal/wfvalidate/pr_review.go` — the validator
    + `RejectError` type + closed placeholder set (FR-001 + FR-002 + FR-010)
  - `go/orchestrator/workflows/pr_review.go` — call site
    that invokes the validator and emits the rejection event (FR-003 + FR-004)
  - `go/orchestrator/cmd/chitin-orchestrator/workflows.go` (new) —
    `list-zombies` + `terminate-zombies` subcommands (FR-005 + FR-006)
  - `go/orchestrator/cmd/chitin-orchestrator/pr_review_dispatch.go` —
    the `CHITIN_FACTORY_LISTEN_NO_DISPATCH` short-circuit (FR-007)
  - `go/orchestrator/cmd/chitin-orchestrator/factory_listen_pr_test.go`
    update (FR-008)
  - Operator runbook + tests + chain-event taxonomy doc update

Out:
  - Validators on other dispatched workflows (SchedulerWorkflow,
    PRIterationWorkflow, SiblingRebaseWorkflow, AutoMergeWorkflow).
    FR-009 declares the contract for follow-up specs; this spec
    scopes only `PRReviewWorkflow` to keep the diff bounded.
  - Detection of "stuck legitimate workflows" (real repo, high
    retry). The FR-005 retry-threshold flag covers it, but the
    primary motivation is the placeholder set; investigating
    high-retry-on-real-repo is its own spec.
  - Auto-restart / replacement of terminated workflows. The
    operator is responsible for re-dispatching after cleanup
    (the `schedule` subcommand already exists; the runbook
    cross-links it).
  - Re-architecture of `dialTemporalAsStarter`'s default
    fallback behaviour. FR-007 + FR-008 mitigate the
    test-time leak without changing production semantics.

## Data model

Chain event payload (the closed FR-004 schema):

```
workflow_input_rejected: {
  workflow_type: string,    // e.g., "PRReviewWorkflow"
  workflow_id: string,
  repo: string,
  pr_number: int,
  reason: string,           // closed set:
                            //   "placeholder_repo" (FR-002 match)
                            //   "missing_repo" (FR-010)
                            //   "missing_pr_number" (FR-010)
                            //   "malformed_repo_slug" (FR-010)
  detail: string            // human-readable amplification
                            //   (e.g., "repo='owner/name' is a
                            //   known placeholder")
}
```

`RejectError` type (returned by `wfvalidate.Validate`):

```go
type RejectError struct {
    Reason string  // FR-004 closed reason set
    Detail string  // human-readable amplification
}

func (e *RejectError) Error() string { return e.Detail }
```

## Edge cases

  - **Real repo whose name coincidentally is in the
    placeholder set.** Vanishingly unlikely (the set is
    `owner/name`, `o/r`, `test/repo`, `example/example`,
    `foo/bar` — none of which would be a real GitHub repo
    in chitin's universe). If it ever happens, the spec
    amendment process removes the offending entry from
    the FR-002 closed set; until then, the affected real
    repo can dispatch via the manual `schedule` subcommand
    which bypasses this validator (the validator lives
    inside `PRReviewWorkflow`, which is dispatched only
    from `dispatchPRReview` via the webhook path; manual
    `schedule` invokes `SchedulerWorkflow`, a different
    workflow type).
  - **Validator regex matches a real repo
    (`malformed_repo_slug` false positive).** The regex
    `^[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$` matches GitHub's
    documented slug format. A real repo can NOT have a
    name outside this character set per GitHub's
    constraints — false positive is impossible by
    construction.
  - **`workflows list-zombies` against a 10,000-workflow
    namespace.** Pagination via Temporal client's standard
    page-by-page iteration; the closed FR-002 set lookup
    is O(1) per workflow. Anticipated runtime: ≤ 10s for
    a healthy namespace.
  - **`terminate-zombies` race: a workflow completes
    between list and terminate.** Temporal returns a
    typed `NotFound` / `Already Completed` error; the
    command logs it as "completed naturally before
    termination" and counts toward the success total.
  - **`CHITIN_FACTORY_LISTEN_NO_DISPATCH=1` accidentally
    set in production.** The orchestrator log line at
    boot includes the env var's value (analogous to
    spec 121's `blob_inline_threshold=...` boot log
    line); operators can audit at-a-glance. The
    deployment runbook documents the expected production
    value as `unset`.
  - **A legitimate webhook arrives with
    `repository.full_name: ""`.** The validator rejects
    with `reason: "missing_repo"`. This is the right
    answer — GitHub shouldn't send an empty repo name;
    if it does, the webhook handler logged the
    delivery_id (per spec 099's logging contract) and
    the operator can correlate.

## Composability

  - **Spec 099** (GitHub-native dispatch) — provides
    `PRReviewWorkflow` + `prDispatchInput`. The validator
    runs at workflow start; the dispatch path is
    unchanged.
  - **Spec 094** (PR review workflow) — owns the
    `PRReviewWorkflow` activity DAG. The validator is
    the new first step before the DAG begins.
  - **Spec 098** (factory listen) — the upstream webhook
    handler. This spec is downstream-defensive.
  - **Spec 114** (operator escalation surface) —
    `workflow_input_rejected` events are NOT escalated
    by default (the validator is a fail-fast for
    obviously-broken inputs, not an operator-action
    signal). A separate digest/analytics consumer can
    aggregate them if patterns emerge (e.g., "the same
    placeholder fired 50× this week → root-cause the
    test leak").
  - **Spec 121** (blob store) — orthogonal; the
    rejection event payload is tiny (≤ 200 bytes), no
    externalization needed.
  - **Spec 123** (auto-merge on label) — its
    `AutoMergeWorkflow` is the FIRST follow-up workflow
    that MUST adopt FR-009's contract: invoke
    `wfvalidate.ValidateAutoMergeInput` at start,
    rejecting placeholder repo names symmetrically.
    Cross-link in spec 123's task list.
  - **Future per-workflow validators** —
    `SchedulerWorkflow`, `PRIterationWorkflow`,
    `SiblingRebaseWorkflow` each get their own
    `wfvalidate.Validate<Foo>Input` in follow-up specs;
    same package, same closed FR-002 set, same
    `workflow_input_rejected` event taxonomy.
