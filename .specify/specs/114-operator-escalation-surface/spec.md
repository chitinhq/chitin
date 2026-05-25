---
spec_id: 114
title: Operator escalation surface — filter PR queue to "what needs Jared"
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 098
  - 099
  - 112
related:
  - 113
---

# Spec 114 — Operator escalation surface

## Why

The factory opens PRs continuously (autonomous loop, spec 098). Each PR
needs SOME attention SOMETIME — but the operator currently has no way to
distinguish PRs the factory handled cleanly from PRs where the factory
hit a wall and needs operator judgement.

Today's operator routine (observed 2026-05-25): open `gh pr list`,
read EVERY open PR's title + review state + recent activity, decide
whether to merge / comment / close. The cognitive cost scales with PR
count regardless of how many actually need operator input.

This spec inverts the default: a single queue surface that shows ONLY
PRs that need the operator, and hides everything the factory is handling.
Composes with spec 113 (which produces the "factory handled it" signal).

## User stories

### US1 (P1) — One-command "what needs me" queue

> As the operator, I run `chitin-orchestrator queue` and see a single
> markdown table of the PRs that need my attention right now — with a
> one-line reason per PR. Anything not in that table is one of:
> in-flight, factory-iterating, or already-clean and awaiting auto-
> merge. I don't need to look at those.

**Independent test:** With 8 open PRs (4 in flight, 2 iterating via
spec 113, 1 escalated, 1 conflicting), `chitin-orchestrator queue`
returns exactly 2 rows (the escalated + the conflicting). The other 6
are hidden.

### US2 (P2) — Daily digest to the operator notification channel

> As the operator, every morning I get a Discord post summarising
> overnight escalations: count, top blockers, action items. So I know
> what to look at first when I sit down without running the command.

**Independent test:** A scheduled job at 09:00 runs `queue --since 24h
--format md` and posts to the configured Discord webhook. Output
matches the markdown shape.

### US3 (P3) — Drill-down per escalation reason

> As the operator, `chitin-orchestrator queue --reason sibling_rebase_failed`
> shows me only the PRs that hit spec 112 US2's fail-soft path so I can
> manually rebase them in one batch.

**Independent test:** `--reason sibling_rebase_failed` returns only PRs
with that escalation reason; `--reason iteration_cap_hit` returns only
those.

## Functional requirements

### Queue computation (US1)

- **FR-001** New subcommand `chitin-orchestrator queue
  [--repo OWNER/NAME] [--since DURATION] [--format table|json|md]
  [--reason KIND]`. Default: repo from `$CHITIN_REPO`, since=`168h` (7d),
  format=table.
- **FR-002** Source of truth: union of (a) live `gh pr list` for open
  PRs in the repo + (b) chain events read directly from
  `~/.chitin/events-<run_id>.jsonl` files (the canonical append-only
  chain store written by `chitin-kernel emit`). The scanner walks every
  `events-*.jsonl` under `$CHITIN_DIR` (default `~/.chitin`), filters
  rows by `event_type` ∈ the escalation taxonomy from FR-008, and
  indexes by `payload.pr_number`. `chitin-kernel emit` remains the
  only WRITER; the queue is a pure reader so requires no new kernel
  subcommand surface.
- **FR-003** A PR is "needs operator" iff ANY of the following holds.
  Each rule maps 1-to-1 to a canonical `reason` kind in FR-008 (the
  rule name and the reason name are the same string):
  - `iteration_cap_hit` — `pr_iteration_escalated` event with
    `reason: "iteration_cap_hit"` in last `--since` window
  - `iteration_completed_with_skips` — `pr_iteration_escalated` event
    with that reason (driver ducked one or more comments — see spec
    113 edge cases)
  - `human_reviewer_present` — `pr_iteration_escalated` event with
    that reason, OR (without spec 113 deployed) a non-bot reviewer
    comment present on the PR
  - `sibling_rebase_failed` — chain has any `sibling_rebase_failed`
    event for the PR (spec 112 US2 fail-soft outcome; not retried)
  - `lease_lost` — `pr_iteration_escalated` event with that reason
    (spec 113's force-push-lost-lease promotion)
  - `dialectic_request_changes` — spec 094 dialectic verdict
    `RequestChanges` with non-empty Blockers
  - `stale_no_automation` — PR open > 24h with no
    `chitin-orchestrator`-authored commit since the last review
  - `conflicting_persistent` — PR `mergeable == CONFLICTING` for > 1h
    (filters out the transient post-merge state)
- **FR-004** A PR is HIDDEN (not in queue) iff NONE of the above hold
  AND the PR has either: a `chitin-iterating/active` label, an
  `pr_iteration_completed` event with no escalation, or no review at
  all yet (still authoring).

### Output formats (US1)

- **FR-005** `--format table` (default): one row per PR with columns
  PR #, title (truncated), reason, age, last-automated-action-age,
  spec_ref (parsed from `sched/run/<id>` label).
- **FR-006** `--format md`: GitHub-flavoured markdown table with the
  same columns + clickable PR links. Suitable for Discord / Slack /
  email digests.
- **FR-007** `--format json`: machine-readable shape for downstream
  tooling. One object per PR with all the FR-005 fields plus the raw
  escalation event payload.

### Drill-down (US3)

- **FR-008** `--reason KIND` filters the output to PRs matching only
  the named reason. The vocabulary is the SAME closed set as FR-003's
  rule names + spec 113 FR-011's `reason` strings — each kind below
  corresponds 1-to-1 to either a FR-003 rule or a chain event payload:
  - `iteration_cap_hit`
  - `iteration_completed_with_skips`
  - `human_reviewer_present`
  - `sibling_rebase_failed`
  - `lease_lost`
  - `dialectic_request_changes`
  - `stale_no_automation`
  - `conflicting_persistent`

### Daily digest (US2)

- **FR-009** New scheduled job `chitin-job-operator-digest` (already
  exists in the scheduled-job infrastructure from spec 081) runs at
  09:00 daily, executes `queue --since 24h --format md`, and posts the
  result to the operator notification channel (existing `DiscordNotify`
  activity, spec 080).
- **FR-010** Digest includes a "since yesterday" delta: how many
  escalations new today, how many resolved, breakdown by reason.

## Success criteria

- **SC-001** With spec 113 deployed, on a typical post-dogfood morning
  with 8 chitin-authored PRs in flight, `queue` returns ≤ 3 rows (the
  ones genuinely needing operator judgement) — 60%+ reduction in
  cognitive cost vs scanning `gh pr list`.
- **SC-002** Subcommand completes in ≤ 2 seconds against a 50-PR repo.
  Chain-event scan is bounded; gh API uses paginated listing.
- **SC-003** Daily digest delivery latency ≤ 30 seconds from scheduled
  trigger to Discord post.

## Scope

### In scope

- `chitin-orchestrator queue` subcommand with three formats
- Chain-event scan filter + live PR state composition
- Reason taxonomy + `--reason` filter
- Scheduled-job daily digest hook

### Out of scope

- A web UI (markdown digest + CLI table is sufficient v1)
- Multi-repo aggregation (one repo per invocation)
- Bidirectional sync to GitHub Projects / Notion / etc. (markdown
  digest is read-only)
- Auto-resolution of escalations (the operator is the resolver — this
  spec only surfaces, doesn't act)

## Edge cases

- **Chain event partially written / orphan PR ref:** queue skips and
  logs warning; never crashes on malformed events.
- **PR was closed mid-window:** included in the digest's "resolved
  since yesterday" delta; not in the live queue.
- **Reason kind unknown in `--reason`:** error with the list of
  valid kinds.
- **No escalations at all:** queue prints "✅ no PRs need attention"
  rather than an empty table.

## Composability with spec 113

113 emits `pr_iteration_completed` (handled) vs `pr_iteration_escalated`
(needs operator). 114 filters by escalated only. Without 113, EVERY PR
with a Copilot comment shows up in 114's queue (because the
"stale_no_automation" rule fires) — which is correct but uninformative.
With 113 deployed, only the genuinely-blocked PRs surface.
