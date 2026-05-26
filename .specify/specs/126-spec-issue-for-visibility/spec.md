---
spec_id: 126
title: One GitHub issue per spec — operator visibility surface without changing dispatch
status: Draft
owner: chitinhq
created: 2026-05-26
depends_on:
  - 098
  - 114
related:
  - 094
  - 113
  - 116
  - 119
---

# Spec 126 — Spec issue for visibility

## Why

The 2026-05-26 architecture conversation surfaced a real
operator-experience gap: today's autonomous loop produces
events the operator only sees via Discord notifications,
chain queries, or PR browsing. The lifecycle of a single
spec — from "spec PR opens" to "impl ships" — has no
durable GitHub-native landing page.

A spec passes through five operator-relevant phases:

  1. Spec PR opens (operator can review)
  2. Spec PR merges (factory will dispatch)
  3. Impl PR opens (factory's whole-spec dispatch produced it)
  4. Impl PR review cycle (Copilot + dialectic + iteration)
  5. Impl PR merges (the spec has shipped)

Today each phase fires its own chain event(s) + Discord
notification(s) + GitHub PR(s). Nothing collates the spec
lifecycle in one place. An operator who wants to answer
"is spec 121 fully done?" walks five surfaces:

  - The spec PR (`#1134`) — merged
  - The chain for `factory_triggered` events
  - The factory-listen log
  - The impl PR (`#1135`) — merged
  - Spec 116's re-review chain (or absence thereof)

This spec adds **one GitHub issue per spec** as the
canonical operator-facing lifecycle surface. The issue is
created on spec PR merge, comments accumulate at each
phase, and the issue closes on impl PR merge. The
dispatcher is unchanged; the issue is **read-mostly UI**
for humans, not a state machine that gates anything.

**Why NOT per-task issues** (the `speckit-taskstoissues`
shape): spec 119 architecturally rejected per-task
fragmentation because of cross-task coherence failures,
silent task drops, operator overhead, wall-clock cost,
and token economics. Per-task issues would reintroduce
those costs purely for a UI gain. **One issue per spec**
preserves spec 119's whole-spec dispatch model entirely —
issues collate the lifecycle WITHOUT becoming dispatch
units.

**Why this is cheap:** the work is event handlers that
fire on existing chain events (`factory_triggered`,
`scheduler_started`, PR `opened`/`closed`/`merged` from
spec 099's webhook handler). Each handler is one
`gh issue create` or `gh issue comment` call. No new
state machine; no new dispatch path; no telemetry
convention changes. The chain stays the source of
truth; the issue is a *projection* of the chain into
GitHub UI.

**Why "may" not "must":** if `gh issue` API calls fail,
the autonomous loop continues. The issue is a
convenience, not a contract. Failures are logged and
emit a chain event for audit; nothing blocks dispatch.

## User stories

### US1 (P1) — Spec PR merge opens a GitHub issue for the spec

> As the operator landing a spec PR, when the PR merges,
> a GitHub issue MUST be created with title `[NNN] <spec
> title>`, label `chitin/spec`, and body containing:
> link to the spec PR, link to the spec.md + tasks.md
> on `main`, the dispatch status (pending → triggered
> → in-flight → impl-PR-open → merged), and a
> placeholder for the impl PR link (filled by US3).

**Independent test:** Synthesize a `pull_request.closed`
webhook with `merged: true` for a spec-only PR. Within
60s, assert a new issue exists in the repo with the
expected title shape and the `chitin/spec` label. The
issue body contains the spec PR URL.

### US2 (P1) — Dispatch start comments on the spec issue

> As the operator watching the autonomous loop, when
> factory-listen dispatches a whole-spec workflow for
> the spec, the spec issue MUST receive a comment
> naming the dispatch run-ID, the driver ID, and the
> capability that was required. The comment uses a fixed
> template so a chain consumer can detect it
> idempotently.

**Independent test:** Synthesize a `factory_triggered`
chain event for the spec; assert the issue receives
exactly one comment matching the template (`### Dispatch
triggered ... run_id=... driver=... capability=...`).
Firing the same chain event twice MUST NOT produce
duplicate comments (idempotency via chain lookup, per
FR-007).

### US3 (P1) — Impl PR open updates the spec issue with a link

> As the operator wanting one place to find the impl PR,
> when the impl driver opens its PR, the spec issue MUST
> receive a comment linking the impl PR (`Impl PR
> opened: #NNNN`). The issue body's "impl PR" placeholder
> is also patched in-place to carry the link (so future
> readers of the issue see the link without scrolling
> through comments).

**Independent test:** Synthesize a `pull_request.opened`
webhook for a `chitin/wu/wu-<spec-ref>-...` branch.
Assert the spec issue receives the impl-PR-opened
comment AND the issue body's placeholder is updated to
the PR's URL.

### US4 (P1) — Impl PR merge closes the spec issue

> As the operator surveying which specs are done, when
> the impl PR merges, the spec issue MUST close with a
> final comment summarising the lifecycle (spec PR link,
> impl PR link, merge SHA, total wall-clock from spec-
> merge to impl-merge). A closed issue with the
> `chitin/spec` label is the canonical "this spec
> shipped" marker.

**Independent test:** Synthesize the impl PR's
`pull_request.closed` webhook with `merged: true`.
Assert the spec issue's state is now `closed`, and the
final comment matches the FR-006 template.

### US5 (P2) — Failed dispatch escalates via the spec issue

> As the operator alerted to a failed dispatch, when
> spec 118's `factory_dispatch_failed` chain event
> fires, the spec issue MUST receive a comment naming
> the failure reason from spec 118's closed taxonomy.
> The issue remains OPEN — failed dispatch doesn't
> close the lifecycle, it pauses it.

**Independent test:** Synthesize a
`factory_dispatch_failed` event with reason
`capability_mismatch`. Assert the spec issue gets a
comment quoting the reason; the issue stays open.

### US6 (P2) — API failures are graceful and audited

> As the operator running the autonomous loop, if any
> `gh issue *` call fails (GraphQL rate limit, network
> hiccup, GitHub outage), the issue update is silently
> skipped — but a chain event MUST record the failure
> so the operator can reconcile manually later. The
> rest of the autonomous loop (dispatch, review,
> merge) MUST NOT be blocked by issue-API failures.

**Independent test:** Inject a failing `gh` shell-out
in the issue-handler activity. Assert (a) the
chain emits one `spec_issue_update_failed` event with
the relevant context; (b) the calling workflow (e.g.
PR-iteration) continues to completion.

## Functional requirements

- **FR-001** A new deterministic activity
  `EnsureSpecIssue` MUST be added at
  `go/orchestrator/activities/spec_issue.go`. Input:
  `{repo, spec_ref, spec_title, spec_pr_url,
  spec_md_url, tasks_md_url}`. Behaviour: if a
  `chitin/spec` issue for this `spec_ref` already
  exists, return its number; otherwise create it via
  `gh issue create --label chitin/spec --title "[NNN]
  <title>" --body <template>` and return the new
  number. Idempotent on duplicate calls.

- **FR-002** A new activity `CommentSpecIssue` MUST
  be added at the same file. Input: `{repo, spec_ref,
  template_id, params}`. Template ids are a closed set:
  `dispatch_triggered`, `impl_pr_opened`,
  `impl_pr_merged`, `dispatch_failed`. The activity
  resolves the issue number for `spec_ref` (via
  `gh issue list --label chitin/spec --search
  "[NNN]"`), renders the template, posts the comment
  via `gh issue comment`. Idempotent per FR-007.

- **FR-003** A new activity `UpdateSpecIssueBody` MUST
  be added. Input: `{repo, spec_ref, patches map[string]string}`.
  Resolves the issue, edits the body in-place via
  `gh issue edit --body` (re-rendering the placeholder
  fields with the patched values). The body is a
  Markdown template with named anchor blocks
  (`<!-- chitin:impl_pr -->`) that the patcher targets;
  unknown anchors are no-ops, NOT errors.

- **FR-004** A new activity `CloseSpecIssue` MUST be
  added. Input: `{repo, spec_ref, final_comment_params}`.
  Posts the final summary comment per FR-006, then
  closes the issue via `gh issue close`.

- **FR-005** The spec issue MUST be created from
  factory-listen's `handlePR` route at
  `cmd/chitin-orchestrator/factory_listen.go` when the
  incoming event is `pull_request.closed` AND
  `merged: true` AND the spec-PR discriminator
  (`isSpecPR` from spec 115 T001) returns true. The
  call is fire-and-forget: a failure to create the
  issue MUST NOT fail the webhook (per US6 + FR-008).

- **FR-006** Comment templates are a closed set, named
  in FR-002. The set:
    - `dispatch_triggered` — body: `### Dispatch
      triggered\nrun_id: <id>\ndriver: <id>\ncapability:
      <name>\nat: <ts>`
    - `impl_pr_opened` — body: `### Impl PR
      opened\nPR: <url>\nbranch: <ref>\nopened_at:
      <ts>`
    - `impl_pr_merged` — body: `### Impl PR
      merged ✓\nPR: <url>\nmerge_sha:
      <sha>\nelapsed: <duration from spec-merge to
      impl-merge>`
    - `dispatch_failed` — body: `### Dispatch
      failed\nreason: <closed-taxonomy value>\nat:
      <ts>\nrun_id: <id>`
  Templates are constants; rendered with `fmt.Sprintf`.

- **FR-007** Idempotency: every `CommentSpecIssue`
  call MUST check, before posting, whether an existing
  chain event of the corresponding kind
  (`spec_issue_commented`) for this `spec_ref` +
  `template_id` already exists. If so, skip the post
  and emit `spec_issue_comment_skipped` instead. This
  prevents duplicate comments on activity retries.

- **FR-008** Closed chain event taxonomy for this
  spec:
    - `spec_issue_opened` — emitted after FR-001's
      activity succeeds; payload `{spec_ref,
      issue_number, repo}`
    - `spec_issue_commented` — emitted after FR-002
      posts a comment; payload `{spec_ref, issue_number,
      template_id, params}`
    - `spec_issue_comment_skipped` — emitted when
      idempotency check (FR-007) suppresses a duplicate
      post; payload `{spec_ref, issue_number,
      template_id, prior_at}`
    - `spec_issue_closed` — emitted after FR-004's
      activity closes; payload `{spec_ref,
      issue_number}`
    - `spec_issue_update_failed` — emitted when any
      `gh issue *` shell-out returns non-zero; payload
      `{spec_ref, issue_number, op, stderr_tail}`

- **FR-009** All `gh issue *` calls MUST honor the
  existing `CHITIN_KERNEL_BIN` interception pattern
  used by tests — the shell-out goes through a
  configurable binary path so tests stub it.

- **FR-010** Spec 099's existing `/webhook/pr` route
  MUST be the entry point for the dispatch-time event
  handlers (US2 + US3 + US5). When an incoming
  `pull_request_opened` event matches a `chitin/wu/*`
  branch, `CommentSpecIssue(impl_pr_opened)` fires.
  When `pull_request.closed` with `merged: true` for
  a `chitin/wu/*` branch, `CloseSpecIssue` fires.

- **FR-011** The autonomous loop MUST continue to
  function correctly when this spec's machinery is
  disabled. A `CHITIN_SPEC_ISSUE_DISABLED=1` env var
  short-circuits all four activities to no-op-with-log.
  This is the break-glass for the case where GitHub
  Issues API is misbehaving or rate-limited.

## Success criteria

- **SC-001** Within 7 days of deployment, every
  newly-merged spec PR produces exactly ONE
  `chitin/spec`-labeled issue. Measured by
  `gh issue list --label chitin/spec --state all`
  count vs the spec-PR-merge count in the same window.

- **SC-002** Within 7 days, every spec issue's
  lifecycle has comments for all phases that fired
  (dispatch + impl-PR-opened + impl-PR-merged for the
  happy path; dispatch + dispatch-failed for the sad
  path). Measured by chain query: count of
  `spec_issue_commented` events per spec_ref equals
  the expected per-phase count.

- **SC-003** Zero duplicate comments on any spec
  issue over 30 days. Measured by chain query: count
  of `spec_issue_comment_skipped` events bounded; no
  observed duplicates in the GitHub UI.

- **SC-004** A break-glass set
  (`CHITIN_SPEC_ISSUE_DISABLED=1`) immediately
  short-circuits all four activities without
  destabilising the rest of the autonomous loop.
  Measured by toggling the env var on a test
  deployment and asserting webhook dispatch + impl-PR
  flow continues normally.

- **SC-005** Operator-touch-time on "which specs are
  done?" drops by ≥ 80% — the answer becomes a single
  GitHub query (`gh issue list --label chitin/spec
  --state closed`) instead of multi-surface walking.
  Measured qualitatively by the operator post-week-1.

## Scope

In:
  - `activities/spec_issue.go` — four new activities
    (FR-001 + FR-002 + FR-003 + FR-004)
  - `cmd/chitin-orchestrator/factory_listen.go` —
    handlePR + handlePush wiring per FR-005 + FR-010
  - Body template constants — FR-001 / FR-006 rendered
    text
  - Chain emit sites — five new event types per FR-008
  - Idempotency check — FR-007 chain query against
    `spec_issue_commented` history
  - Break-glass `CHITIN_SPEC_ISSUE_DISABLED=1` per FR-011
  - Tests + documentation + runbook

Out:
  - Per-task issues — spec 119 explicitly rejected
    this; this spec keeps whole-spec dispatch as the
    primary unit and creates ONE issue per spec.
  - Issue-driven dispatch — issues are read-mostly UI;
    they don't trigger dispatch. The trigger remains
    `tasks.md` (post-spec-125, the Added path only).
  - GitHub Projects board integration — the
    `chitin/spec` label is the foundation; projects
    can subscribe later, no chitin code change needed.
  - Migration of historical specs (those already
    shipped before this spec deploys). Future PRs
    only. A separate one-shot script can backfill if
    desired (out of scope).
  - Bidirectional sync — the issue is a projection of
    the chain, not a source of truth. Operator
    comments on the issue are ignored by the
    autonomous loop. (Operators who want to drive
    dispatch from issues use the manual `schedule`
    flow.)
  - Per-spec-issue board state machine. The issue is
    open or closed; no extra states. Future specs can
    add labels/states if needed.

## Edge cases

  - **A spec PR merge produces NO impl dispatch**
    (e.g. spec was already shipped; spec 125 filters
    the Modified re-trigger). The issue still opens at
    spec-merge time per US1, but no `dispatch_triggered`
    comment fires (because no dispatch happened). The
    issue stays open until an operator closes it
    manually OR until a subsequent dispatch eventually
    completes. Acceptable: the operator can see "spec
    landed, no impl in flight" by reading the issue.
  - **Multiple impl PRs for the same spec** (legitimate
    in per-task-mode if it returns, or as a
    bug-recovery case). Each `pull_request.opened`
    event for a `chitin/wu/<spec-ref>` branch produces
    one `impl_pr_opened` comment. The issue body's
    placeholder shows the MOST RECENT impl PR URL.
    Closed issues stay closed when a duplicate
    `impl_pr_opened` event fires (no re-open). This is
    correct: a duplicate dispatch is the
    spec-125-filtered case, the original close stands.
  - **`gh issue` rate limit hit mid-cycle.** Per US6,
    the issue update fails, `spec_issue_update_failed`
    event fires, the dispatch/review/merge flow
    continues unaffected. The operator can manually
    reconcile or wait for the rate-limit reset and
    re-fire the event.
  - **Issue labeled `chitin/spec` was manually deleted
    by the operator.** The next chain-driven event for
    that spec_ref hits a "not found" from `gh issue
    list`; the activity logs + emits
    `spec_issue_update_failed`. No re-creation (we
    don't want to fight the operator). The break-glass
    via env var is preferred for "stop creating
    issues."
  - **Two spec PRs with the same `NNN` numeric prefix
    but different slugs** (spec name collision).
    `gh issue list --search "[NNN]"` returns both. The
    activity disambiguates via `spec_ref` (the full
    `NNN-slug` string) in the issue title's first line
    OR by reading a custom field. This spec uses the
    second occurrence: the issue title is `[NNN-slug]
    <title>` rather than `[NNN] <title>`, removing the
    ambiguity.
  - **A spec PR is merged and then reverted.** The
    revert appears as a new spec-PR merge (the file
    re-Added) — but the original issue still exists.
    The activity detects this via the FR-001
    idempotency check (issue already exists for this
    spec_ref) and returns the existing issue number;
    no second issue is created. A `spec_issue_opened`
    chain event MAY still fire for audit, with a
    `was_new: false` flag in the payload.

## Composability

  - **Spec 098** (factory webhook) — this spec hooks
    into the same `/webhook/push` and `/webhook/pr`
    routes, no new endpoints.
  - **Spec 099** (PR webhook handler) — entry point
    for FR-010's per-phase event handlers.
  - **Spec 113** (PR iteration loop) — orthogonal;
    iteration produces fixup commits but doesn't
    touch the issue.
  - **Spec 114** (operator escalation surface) —
    parallel surface. The Discord notifications spec
    114 emits remain; the spec issue is an
    ADDITIONAL surface, not a replacement. Operators
    who prefer Discord still get Discord.
  - **Spec 115** (spec-PR linter + iteration) — the
    issue isn't created until spec PR merges, which
    is AFTER spec 115's review cycle. No interaction
    at the spec-PR-review phase.
  - **Spec 116** (internal re-review) — orthogonal;
    re-review on impl PRs doesn't comment on the
    spec issue (impl-PR-opened + impl-PR-merged
    cover the spec-issue surface).
  - **Spec 119** (whole-spec dispatch) — this spec
    preserves whole-spec dispatch unchanged. One spec
    = one issue = one impl PR = one merge. Per-task
    issues explicitly rejected per the "Why" section.
  - **Spec 123** (auto-merge) — orthogonal but
    composes naturally: when auto-merge fires on a
    `chitin/wu/<spec-ref>` impl PR,
    `CloseSpecIssue` fires next, closing the
    lifecycle without operator action.
  - **Spec 125** (Added-only dispatch filter) — this
    spec depends on 125 for correctness: without 125,
    the duplicate impl dispatches would produce
    duplicate `impl_pr_opened` comments on the spec
    issue. With 125, the spec issue's lifecycle is
    clean.
