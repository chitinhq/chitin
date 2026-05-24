---
spec_id: 099
title: GitHub Copilot Driver via Issue Assignment
status: Draft
owner: chitinhq
created: 2026-05-23
depends_on:
  - 070
  - 075
  - 094
  - 098
related:
  - 097
---

# Spec 099 — GitHub Copilot Driver via Issue Assignment

## Why

The Chitin orchestrator already dispatches specs locally via spec 097's `chitin-orchestrator schedule` CLI and (with spec 098) via push webhook. Every existing driver — Codex, Gemini, Llama Cloud, Hermes, claudecode, openclaw, local — runs inside the orchestrator's worktree on the operator's machine. That stays.

GitHub Copilot is structurally different: Copilot's coding agent runs **inside GitHub**, not in our worktree. You ask Copilot to do work by creating an issue and assigning it to `@copilot`. Copilot drafts a pull request autonomously, on GitHub's infrastructure, against the target repo. There is no place for the orchestrator to "run" Copilot locally — Copilot is the act of assigning the issue.

This spec adds **Copilot as an orchestrator-aware driver** by giving the orchestrator two roles:

1. **Producer:** when a spec's tasks.md routes to Copilot, the orchestrator creates the GitHub issue and assigns it to @copilot. This replaces the spec-097 "schedule a local SchedulerWorkflow" step for this one driver.
2. **Consumer:** when Copilot opens the resulting draft PR, the orchestrator detects it (via the same webhook receiver spec 098 added) and routes it through spec 094's `PRReviewWorkflow` for dialectic review.

Every other driver's dispatch path is **unchanged** — Codex/Gemini/Llama/Hermes/claudecode/openclaw/local stay 100% local. This spec is **not** "all drivers dispatch via GitHub." It is "Copilot dispatches via GitHub because that's how Copilot works."

## User Stories

### US1 (P1) — Local dispatch routes to Copilot via GitHub

> As the operator, I run `chitin-orchestrator schedule <spec-ref> --driver copilot` on my machine. The orchestrator does NOT start a SchedulerWorkflow. Instead it creates a GitHub issue on the target repo titled "Run spec NNN-name", body containing the spec ref + a `Closes` clause for any companion issue, assigns the issue to @copilot, and applies a `chitin-dispatch` label. The CLI prints the issue URL. Operator is done — Copilot takes it from here.

**Independent test:** Run the CLI with `--driver copilot` against a test repo. Assert (a) an issue is created with the expected title/body/labels, (b) the issue is assigned to copilot, (c) the chain emits `copilot_dispatched` with `issue_url` + `spec_ref` + `repo`, (d) no SchedulerWorkflow is started in Temporal.

### US2 (P1) — Orchestrator detects Copilot's draft PR and routes review

> Copilot drafts a PR within its own SLA. The PR is opened against main, marked draft, and (because Copilot's GitHub integration propagates the issue's labels) carries the `chitin-dispatch` label and a `Closes #ISSUE` clause linking back to the original issue. The orchestrator's webhook receiver (spec 098, extended for `pull_request.opened`) detects the draft PR and starts spec 094's `PRReviewWorkflow`. The dialectic verdict comments on the PR. The human reviews the verdict and merges or asks Copilot to iterate.

**Independent test:** Open a draft PR on a test repo with the `chitin-dispatch` label and a `Closes #N` body where issue #N was created via US1. Assert (a) the webhook receiver detects the PR within 30s, (b) spec 094's `PRReviewWorkflow` starts and runs, (c) the verdict posts as a PR comment, (d) the chain emits `copilot_pr_detected` then `copilot_review_posted`.

### US3 (P2) — Driver routing is operator-explicit

> Driver choice is per-invocation, not auto-routed. The operator picks `--driver copilot` explicitly on the CLI (or sets `driver: copilot` in the spec's plan.md frontmatter). The orchestrator does NOT default to Copilot — defaults stay with the existing local drivers. Operator choice is the only way work goes to Copilot.

**Independent test:** Run `chitin-orchestrator schedule <ref>` without `--driver`. Assert no GitHub issue is created. Run again with `--driver copilot`. Assert exactly one issue is created.

## Functional Requirements

### Producer side (orchestrator → GitHub)

- **FR-001** Extend `chitin-orchestrator schedule` with a `--driver` flag. When `--driver copilot` is passed, the orchestrator takes the Copilot path; otherwise it takes the existing local SchedulerWorkflow path.
- **FR-002** Copilot path uses the `gh` CLI (or the GitHub REST API directly) to create an issue on the target repo. The orchestrator does NOT start a Temporal SchedulerWorkflow on this path.
- **FR-003** The created issue has: title `Run spec <NNN-name>: <spec title>`, body containing the spec ref + a link to the spec's tasks.md on main + a "Dispatched by chitin-orchestrator at <ts>" footer. Labels: `chitin-dispatch`, `driver:copilot`. Assignee: `copilot`.
- **FR-004** If the target repo lacks the `copilot` user as an assignable (Copilot not installed there), the orchestrator MUST fail loud with a non-zero exit and a stderr message naming the repo + the installation URL. NO partial dispatch.
- **FR-005** The CLI MUST emit a `copilot_dispatched` chain event with `repo`, `spec_ref`, `issue_url`, `issue_number`, and `dispatched_at`.

### Consumer side (GitHub → orchestrator)

- **FR-006** Extend spec 098's `factory-listen` HTTP receiver to handle the `pull_request.opened` and `pull_request.ready_for_review` event types (in addition to `push`).
- **FR-007** A pull-request event is **eligible for Copilot review** when ALL of: (a) PR is in draft state OR marked ready-for-review by Copilot, (b) PR carries the `chitin-dispatch` label, (c) PR body contains a `Closes #ISSUE` reference to an issue that previously emitted `copilot_dispatched`. Other PRs are ignored on this code path (they continue through spec 098's push-driven flow if applicable).
- **FR-008** Eligible PR detection MUST be idempotent: re-receiving the same `pull_request.opened` event MUST NOT start a second review workflow. The orchestrator deduplicates by checking the chain for an existing `copilot_pr_detected` event keyed on `(repo, pr_number)`.
- **FR-009** On detection, the orchestrator MUST start a `PRReviewWorkflow` (spec 094) with the PR number, repo, and the original spec ref (recovered from the `copilot_dispatched` chain event via the issue cross-reference).
- **FR-010** Chain events: `copilot_pr_detected` (on detection), `copilot_review_posted` (after the review verdict is commented on the PR), `copilot_review_failed` (if the review workflow itself errors).

### Operator surface

- **FR-011** `chitin-orchestrator copilot-list` prints all in-flight Copilot dispatches across every repo the orchestrator has seen — columns: `repo`, `issue_number`, `pr_number` (if detected), `spec_ref`, `state` (dispatched / pr_detected / review_posted / failed), `dispatched_at`. State is derived from chain events.
- **FR-012** Failed dispatches (issue-creation failures, missing `@copilot` assignee, draft PR never appearing within a configurable timeout) MUST be visible via `copilot-list` and emit a chain event named for the failure mode.

## Success Criteria

- **SC-001** End-to-end CLI→issue-created latency: median <10s, p99 <30s, measured from `chitin-orchestrator schedule --driver copilot` invocation to `copilot_dispatched` chain event.
- **SC-002** Draft-PR detection latency: median <30s, p99 <120s, measured from `pull_request.opened` webhook timestamp to `copilot_pr_detected` chain event.
- **SC-003** Idempotency: re-emitting the same `pull_request.opened` webhook 100 times within 5 minutes MUST result in exactly 1 `PRReviewWorkflow` start (FR-008 invariant).
- **SC-004** Local drivers unaffected: every spec dispatched without `--driver copilot` continues to take the existing SchedulerWorkflow path. Measured by: 0 GitHub issues created during a 7-day window of CLI dispatches that did not specify `--driver copilot`.

## Scope

### In scope

- `--driver copilot` flag on `chitin-orchestrator schedule`.
- Issue-creation logic against the target repo via `gh` CLI or REST.
- Spec 098 webhook receiver extension for `pull_request.opened` and `pull_request.ready_for_review` event types.
- Idempotent draft-PR detection via chain dedup.
- Hand-off to spec 094's `PRReviewWorkflow` for the dialectic verdict.
- New chain event types (`copilot_dispatched`, `copilot_pr_detected`, `copilot_review_posted`, `copilot_review_failed`).
- `copilot-list` operator subcommand.

### Out of scope

- **Routing logic that decides Copilot vs. local drivers automatically.** Driver choice is operator-explicit (FR US3). No auto-routing in this spec.
- **Other drivers' dispatch surface.** Codex, Gemini, Llama Cloud, Hermes, claudecode, openclaw, local — all stay on the existing local SchedulerWorkflow path. This spec touches only the Copilot path.
- **Per-driver budget tracking.** Budget enforcement is an operator concern (don't pass `--driver copilot` if Copilot is over budget). The orchestrator does not track Copilot spend.
- **Authoring specs from GitHub issues.** This spec assumes the spec ref already exists; the issue is the *dispatch trigger*, not the spec content. Spec authoring from issue body is a separate idea, not addressed here.
- **Copilot's own behavior.** The orchestrator does not control how Copilot drafts the PR — that's GitHub's surface. The orchestrator only observes the resulting PR.

## Edge Cases

- **Operator runs `--driver copilot` on a repo where Copilot is not installed:** orchestrator fails loud with the installation URL (FR-004). No partial state.
- **Copilot opens a PR without the `chitin-dispatch` label:** orchestrator does NOT pick it up. Operator can manually add the label to recover; on the next `pull_request.labeled` event the orchestrator detects it.
- **Copilot opens a PR with the label but no `Closes #ISSUE`:** orchestrator detects the PR but cannot recover the original spec ref. It still routes the PR through spec 094 review (the review doesn't strictly require the spec ref) and emits `copilot_pr_detected` with `spec_ref: unknown`.
- **Copilot never opens a PR within a configurable timeout (default 24h):** orchestrator emits `copilot_dispatch_stale` and the issue shows up in `copilot-list` with state `stale`. Operator decides whether to nudge Copilot or close the issue.
- **The same spec is dispatched twice to Copilot (operator runs the CLI twice):** orchestrator creates two issues. Idempotency is per-PR-detection, not per-dispatch. If the operator wants idempotent dispatch, they check `copilot-list` first. (Could be tightened in a future spec.)
- **Copilot opens the PR, the review posts, and Copilot then iterates by force-pushing to the same PR:** the `pull_request.synchronize` event arrives; the orchestrator detects it via chain dedup (PR already known) and re-runs the review on the new HEAD. The new review verdict appends as a fresh comment, not edits the prior one.

## Risks

### Telemetry blind spot (the load-bearing risk)

**We lose our telemetry surface the second the dispatch happens on GitHub.**

For every local driver — Codex, Gemini, Llama Cloud, Hermes, claudecode, openclaw, local — the kernel observes every tool call, every gate decision, every stop-hook event, every error recovery, and writes them to `~/.chitin/events-<run_id>.jsonl` as a hash-chained event stream. Sentinel (the analyzer passes, the `/sentinel` skill, the execution_events table in Neon) consumes that stream to detect failure patterns, lockdown loops, and performance regressions.

Copilot's coding agent runs inside GitHub's infrastructure. We have **no visibility** into:

- The tools Copilot tried before settling on the PR diff.
- The errors Copilot hit and recovered from.
- The latency breakdown of Copilot's drafting steps.
- Rate-limit events, retries, or governance interventions on GitHub's side.
- Anything Copilot considered and discarded.

The only things we observe are the **artifacts**: the issue creation timestamp, the PR opening timestamp, the diff, the commits, any progress comments Copilot posts on the PR. That is a strictly poorer telemetry surface than what the local drivers produce.

### Mitigations (partial — these reduce the loss but do not close it)

- **PR-level event capture (FR-013 below):** the orchestrator records every `pull_request.*` and `issue_comment.*` webhook event into the chain with full payload. PR commit timestamps, force-push events, Copilot's own comments on the PR, draft→ready transitions — all of it lands in the event stream, just at PR granularity not tool-call granularity.
- **Diff statistics:** at `copilot_pr_detected` time the orchestrator captures `commits`, `additions`, `deletions`, `changed_files` from the PR object. Coarse but useful for "size of work" trend analysis.
- **Wall-clock latency:** `dispatched_at` → `pr_opened_at` is a meaningful Copilot-throughput metric the orchestrator can graph over time.
- **Spec 094 review verdict:** the dialectic review is the *only* point where we get tool-call-grained telemetry on the work, because spec 094 runs in our worker pool. This makes the review surface load-bearing for Copilot-dispatched work in a way it isn't for local-dispatched work.

### What this means for routing decisions

Choosing `--driver copilot` is choosing **less observability in exchange for offloaded compute**. The right shape for spec adoption is:

- **Specs where the implementation is well-understood and the operator wants Copilot's drafting throughput** — route to Copilot, accept the telemetry loss, lean on spec 094's review as the quality gate.
- **Specs where the implementation behavior itself is what we're trying to learn from** — keep them on local drivers so sentinel can observe execution and surface failure patterns.

This is a deliberate, operator-visible tradeoff, not a regression. The spec records the tradeoff so future routing-policy work (a potential spec 100) can encode it.

### Additional FR for telemetry capture

- **FR-013** The webhook receiver MUST capture and chain-emit every `pull_request.*`, `issue_comment.created`, and `pull_request_review.*` event for any PR with the `chitin-dispatch` label. Event type: `copilot_pr_activity`. Payload includes the full webhook body (minus auth headers). This is the **PR-level event stream** — the deliberate partial-recovery of the telemetry surface we lose by dispatching off-machine.

## Assumptions

- GitHub Copilot is installed on the target repo and `copilot` is an assignable user there.
- Spec 098's `factory-listen` HTTP receiver is running and reachable from GitHub (via tunnel or App webhook URL).
- Spec 094's `PRReviewWorkflow` is on main and callable as a Temporal workflow.
- The orchestrator has a GitHub credential (`GH_TOKEN` or `gh auth login`) with `issues:write` permission on every target repo.

## Notes for Implementation Phase

This spec is **design-only**. Implementation will land in a separate PR. Likely sequence:

1. Extend `chitin-orchestrator schedule` with `--driver` flag and the Copilot branch.
2. Add the `gh issue create` logic + chain emission.
3. Extend `factory-listen` for `pull_request.*` event types.
4. Wire the spec 094 review workflow start as a Temporal activity.
5. Add `copilot-list` subcommand.
6. End-to-end demo: `chitin-orchestrator schedule --driver copilot 099-fixture` against a test repo, watch Copilot draft, watch orchestrator ingest, watch review verdict post.

The implementation footprint is small — most of the heavy lifting is done by GitHub (Copilot drafts the PR) and by spec 094 (reviews it). The orchestrator's role is the **mediation layer**: issue creation on dispatch, PR detection on ingest, chain bookkeeping in between.
