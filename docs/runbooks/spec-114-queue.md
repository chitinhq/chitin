# `chitin-orchestrator queue` runbook

Status: spec 114 v1 — shipped behind feature work units T001–T013.

The queue is the operator's morning surface. Default `gh pr list` shows every
open PR; this command shows only the PRs the factory could not finish on its
own. If the queue is empty, nothing needs you — go work on something else.

## What this command answers

> "Which PRs need a human decision right now, and why?"

Anything not in the queue is one of:

- **in-flight** — driver still working, no review yet
- **factory-iterating** — spec 113 PR-comment-respond loop is addressing
  Copilot feedback; expect a fixup commit shortly
- **clean** — green CI, no review blockers, auto-merge will land it

Those three buckets are intentionally hidden. The queue surfaces only the
escalations from the FR-008 closed taxonomy below.

## Quickstart

```bash
# Default: 7-day window, table format, $CHITIN_REPO
chitin-orchestrator queue

# Today's escalations as markdown (paste into Discord/Slack)
chitin-orchestrator queue --since 24h --format md

# Just the sibling-rebase failures so you can batch-rebase them
chitin-orchestrator queue --reason sibling_rebase_failed

# Different repo
chitin-orchestrator queue --repo chitinhq/chitin

# Machine-readable for downstream tooling
chitin-orchestrator queue --format json | jq '.[] | .pr_number'
```

Flags:

| Flag | Default | Notes |
|---|---|---|
| `--repo OWNER/NAME` | `$CHITIN_REPO` | required if env unset |
| `--since DURATION` | `168h` (7d) | Go duration string (`24h`, `4h30m`, etc.) |
| `--format` | `table` | one of `table`, `md`, `json` |
| `--reason KIND` | (none) | filter to a single reason from FR-008 |

Empty queue prints `✅ no PRs need attention` instead of an empty table — if
you see that and it feels wrong, widen `--since` or check `gh pr list` for
PRs the factory hasn't touched yet (which means they aren't in the chain
and won't be picked up here).

## Reason kinds — what each one means and what to do

Each row in the queue carries exactly one `reason` from this closed set. The
rule that fires the reason is described in spec 114 FR-003; the triage
guidance below is the operator side of that contract.

### `iteration_cap_hit`

**What it means:** Spec 113's PR-comment-respond loop ran the maximum
number of fixup iterations and Copilot is still requesting changes. The
driver isn't converging on its own.

**Triage:**

1. Open the PR. Read the most recent Copilot review.
2. If the unresolved comment is a real defect, write the fix yourself and
   push to the branch — `chitin-orchestrator` will see the new commit and
   the next Copilot pass will re-evaluate.
3. If the comment is wrong or out of scope, resolve it manually in the
   GitHub UI and merge. The cap exists to stop the loop from chasing
   noise, not to flag genuinely broken code.

### `iteration_completed_with_skips`

**What it means:** The spec 113 driver ran but explicitly chose not to
address one or more Copilot comments — usually because the comment was
out of scope or the driver judged it incorrect (see spec 113 edge cases).

**Triage:**

1. Read the driver's explanation in the PR thread (it posts a brief
   reason per skipped comment).
2. Either accept the skip (merge) or write the fix yourself if the
   driver's judgement was wrong.

### `human_reviewer_present`

**What it means:** A non-bot reviewer commented on the PR. The factory
defers to humans by design — once any human engages, the driver stops
auto-iterating.

**Triage:** Read the human reviewer's comment and either address it
yourself or ping them for the next round. Auto-iteration is paused on
this PR until you take it back into the loop manually.

### `sibling_rebase_failed`

**What it means:** Spec 112 US2's fail-soft path — a sibling worktree's
rebase against `main` produced a conflict the kernel couldn't resolve.
Not retried automatically.

**Triage:**

1. Group all `sibling_rebase_failed` PRs:
   `chitin-orchestrator queue --reason sibling_rebase_failed`.
2. For each: `cd` into the worktree, `git rebase origin/main`, fix the
   conflict by hand, `git push --force-with-lease`.
3. Batching these is faster than one-at-a-time because conflicts in
   sibling worktrees often share a root cause (one big main-branch
   change has rippled through several in-flight branches).

### `lease_lost`

**What it means:** A driver tried to push a fixup commit and its
`--force-with-lease` was rejected because someone (you, another driver,
GitHub) advanced the branch since the driver last fetched.

**Triage:**

1. Decide whether the conflicting commit on the branch is the one you
   want. If yes: dismiss the queue row; the driver will pick up the new
   head on next iteration.
2. If the driver's intended fixup is what you want instead: pull, reset
   to the driver's commit, force-push.

This usually means two drivers raced on the same PR. The fix is upstream
(spec 113 lease coordination), not in the queue.

### `dialectic_request_changes`

**What it means:** The spec 094 dialectic verdict for this PR returned
`RequestChanges` with non-empty `Blockers`. Higher-stakes than a Copilot
nit — the dialectic gate caught something structural.

**Triage:** Read the dialectic verdict body (linked from the PR
description). Blockers are usually architectural or safety issues that
need a human call — fix them or close the PR.

### `stale_no_automation`

**What it means:** PR has been open > 24h with no `chitin-orchestrator`-
authored commit since the last review. Either the driver isn't watching
this PR or it ran and produced nothing.

**Triage:**

1. Check `gh pr view <N> --json labels` — does it have
   `chitin-iterating/active`? If yes, the driver is supposed to be on it;
   check the orchestrator's recent runs (`chitin-orchestrator status`)
   for a dispatch failure.
2. If no automation label, the PR slipped the filter. Either tag it for
   the loop or finish it manually.

### `conflicting_persistent`

**What it means:** GitHub reports `mergeable == CONFLICTING` for > 1h.
The 1h floor filters out the transient post-merge state (where GitHub
briefly marks every PR conflicting while it recomputes).

**Triage:** Rebase the PR onto `main`. If the conflict is mechanical,
push the rebase yourself. If it's substantive, the driver that owns the
branch probably needs a fresh dispatch (close + re-open the work unit).

## Typical morning triage flow

A normal post-dogfood morning (per SC-001) should leave ≤ 3 rows. The
sequence:

1. **Read the Discord digest** (FR-009 — auto-posted at 09:00). The
   "since yesterday" delta tells you whether overnight was quiet or
   busy.
2. **Run `chitin-orchestrator queue`** to get the live view (the digest
   is 09:00; reality is now).
3. **Triage in this order:**
   - `dialectic_request_changes` first — these are the highest-stakes
     blockers
   - `sibling_rebase_failed` next — batchable, fastest to clear
   - `iteration_cap_hit` and `iteration_completed_with_skips` — needs
     reading the PR thread
   - `human_reviewer_present` — usually means you're already engaged;
     just continue the conversation
   - `stale_no_automation` last — often a configuration/label issue,
     not a code issue
   - `lease_lost` and `conflicting_persistent` — clear when convenient

4. **If you cleared every row, walk away.** The factory is fine. Do not
   open `gh pr list` to "double-check" — that defeats the entire purpose
   of this spec.

## When the queue lies

Things that look like queue bugs but aren't:

- **Empty queue but you know there's a stuck PR.** Either the PR has no
  chain events yet (the driver never dispatched), or the stuck condition
  isn't in the FR-008 taxonomy. File a spec amendment to add the
  reason; don't widen the filter ad-hoc.
- **Reason kind not in FR-008 in your `--reason` flag.** The command
  errors with the valid list — that's the contract, not a bug.
- **Queue takes > 2 seconds.** SC-002 budget is 2s. Likely cause: the
  chain-event scan is walking thousands of `events-*.jsonl` files. Run
  `ls ~/.chitin/events-*.jsonl | wc -l` — if it's growing unbounded,
  archive old run files (the kernel doesn't garbage-collect them).

## Daily digest (US2)

The scheduled job `operator-digest` (spec 081 infra, cron `0 8 * * *`
America/Detroit per `schedules/operator_digest.go`) runs `queue --since
24h --format md` and posts to the operator Discord webhook. SC-003
budget is 30s end-to-end.

If the digest stops arriving:

1. Check `chitin-orchestrator status` for the scheduled-job run.
2. Check the Discord webhook is still live (`DiscordNotify` activity
   logs).
3. The digest is best-effort — a failed post does NOT requeue. Run
   `chitin-orchestrator queue --since 24h --format md` by hand and
   paste it into Discord yourself; investigate the scheduler asynchronously.

## Source-of-truth references

- Spec: `specs/114-operator-escalation-surface/spec.md`
- Upstream signal producer: spec 113 (PR-comment-respond loop)
- Sibling-rebase fail-soft: spec 112 US2
- Dialectic verdicts: spec 094
- Chain-event store contract: `chitin-kernel emit` →
  `~/.chitin/events-<run_id>.jsonl`
- Scheduled-job infra: spec 081, `go/orchestrator/schedules/`
