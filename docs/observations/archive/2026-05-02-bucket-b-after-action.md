---
date: 2026-05-02
status: after-action
audience: future-self / next agent reading these
purpose: Capture the Bucket-B contamination incident so the next time
  4 PRs ship only `.claude/settings.json` we recognize the pattern
  immediately.
---

# Bucket-B contamination after-action — 2026-05-02

## What happened

Of the 15 PRs the autonomous swarm produced overnight (2026-05-02
03:14Z–04:24Z), four — **#114 wall-timeout-sigkill-propagation,
#117 task-validate-command-pre-activity-gate, #118 chitin-install-
slice-3-agents, #120 rename-local-cloud-driver-misnomer** — committed
**only** a `.claude/settings.json` change and zero work on the
backlog entry's actual `file:` target. Diff shape was identical
across all four: replace the existing Nx-plugin `.claude/settings.json`
with a chitin-governance hook config.

These looked like real PRs in `gh pr list`. They had titles, bodies
referencing the entry id, even passing CI. Only on inspection did the
mismatch between title (`wall-timeout-sigkill-propagation`) and diff
(`.claude/settings.json` lines) become visible.

27% bucket-B rate is unusable. The autonomous loop's value is bounded
by how often the apply step produces an honest PR; one-in-four false
positives is worse than one-in-four no-PRs.

## Root cause (CORRECTED 2026-05-02 afternoon)

> **First analysis was wrong.** The original write-up blamed an
> untracked backup artifact (`.claude/settings.json.chitin-backup-<ts>`)
> in the parent repo's `.claude/` dir. That theory motivated PR #121's
> backlog entry and PR #122's pre-flight scrub. Both shipped. Both
> address a real but smaller issue. Neither fixes Bucket-B.
>
> The actual root cause was found while pulling driver-mix telemetry on
> the 15 overnight runs: 100% of Bucket-B PRs were claude-code-headless,
> 0% were copilot. That asymmetry is not consistent with "shared
> backup artifact in the parent repo" (which would affect both
> drivers equally). Re-reading PR #114's diff revealed it was
> byte-identical to the output of `writeWorktreeClaudeSettings`.

The actual chain:

1. The dispatcher provisions a fresh worktree off `main` for each
   dispatch.
2. For claude-code-headless, the worker calls
   `writeWorktreeClaudeSettings(worktreePath)` (`apps/runner/
   src/activity.ts:219`) before spawning the agent. This writes the
   chitin gate hook config to `worktree/.claude/settings.json` —
   **overwriting** whatever main had checked out there (the Nx-plugin
   config).
3. Whether or not the agent does any task work, the apply step's
   "tracked diff" check (`grooming/apply-workflow-result.ts`) sees a
   modification on `.claude/settings.json` (tracked: yes; in `git
   diff --shortstat`: yes).
4. `trackedDiff` non-empty → auto-commit fallback fires → pushes →
   opens PR.
5. The PR's title and body are generated from the entry's metadata,
   so it *looks* like the agent did the work. The diff is just the
   worker's hook write.

Why only claude-code-headless: copilot dispatches via
`chitin-kernel drive copilot` and never enters the `claude` CLI's
project-settings discovery path, so the worker never touches
`.claude/settings.json` for copilot runs. All 10 copilot dispatches
overnight produced honest PRs; 4 of 5 CCH dispatches produced
Bucket-B.

This is **a security-shaped failure even though no security policy
was bypassed.** The chain says: worker writes hook config + apply
step's permissive auto-commit heuristic = ship something that looks
like the intended work but isn't.

### Why the first analysis was plausible-but-wrong

When I noticed four PRs with identical small diffs, the parent
repo's untracked `.claude/settings.json.chitin-backup-*` was visibly
present and nominally matched "something replaced settings.json with
the chitin hook." The hypothesis would have predicted contamination
on copilot too, but I didn't check driver-mix before publishing the
after-action — closing the loop on driver split would have surfaced
the wrong-cause sooner. **Lesson: ground every "why did this fail?"
in the per-driver / per-tier breakdown before publishing.**

## How it was detected

Not by CI. Not by the apply step. Not by the chain. By a human
(coordinator agent in this case) reading the diff after gathering
"what's open" and noticing every Bucket-B PR's diff was the same
file. The signal was *similarity across PRs*, not any single PR's
content.

This means: passing CI on a PR with a diff that doesn't match the
title is not a contradiction the existing pipeline can detect.
Coverage of "did the agent change what it was asked to change" is
absent.

## What we changed

1. **Deleted the parent-repo artifact** (PR #121). Backups created by
   `chitin-kernel install` are noisy but turned out not to be the
   cause. Cleanup still good hygiene.
2. **Filed `dispatcher-preflight-scrub-claude-settings-backup`** as a
   ready entry in `swarm-backlog.md` (PR #121); the swarm picked it
   up and shipped PR #122 implementing the preflight check. Pre-flight
   refuses (or auto-scrubs) when any `.claude/settings.json.chitin-backup-*`
   file is present at tick start. Default to refuse unless dogfood
   shows it's a recurring operator burden.
3. **Closed #114, #117, #118, #120** with a comment naming what we
   *thought* was the root cause. #114's entry is moot — the SIGKILL
   fix already shipped in slice-7a (PR #99). The other three remain
   claimable. After PR #123 lands they'll dispatch cleanly.
4. **PR #123 (the actual fix):** added
   `revertWorktreeSettingsArtifact()` to the apply step. Before the
   trackedDiff check, the apply step runs `git checkout --
   .claude/settings.json` in the worktree. If the worker's hook write
   was the only modification, the worktree returns to clean → no
   auto-commit → no PR. If the agent did real work elsewhere, that
   work is the only thing the heuristic sees.
5. **PR #123 also routes T2 → copilot for now.** Overnight data: T2
   on CCH was 1/4 success vs. T0/T1 on copilot at 9/10. Flip back to
   CCH after this fix has been live for one swarm cycle and the
   bucket-B rate stays at 0.

## What we did NOT change (and why)

- **Did not** broaden the `git add -A` heuristic to "only stage files
  named in the entry's `file:` field." Tempting, but it punishes the
  legitimate case where an entry's target generates auxiliary files
  (e.g. updating a generated test fixture). Fix the artifact at the
  source (the worktree write), not the staging step.
- **Did not** stop calling `writeWorktreeClaudeSettings` itself.
  Removing it would break the chitin gate hook that makes CCH runs
  governable. The fix is at the apply step, not the worker.
- **Did not** force-push reset the four broken branches. Force-push
  is denied by `chitin.yaml`'s `no-force-push` rule, and rightly so —
  rewriting shared history hides this exact kind of failure from
  audit. Closing leaves the bad commits visible in the chain.
- **Did not** mark `wall-timeout-sigkill-propagation` as completed in
  the backlog. The shipped slice-7a code matches the entry's intent,
  but the grooming-pass author should confirm and close the entry
  with a "shipped at PR #99" pointer rather than a silent removal.

## Generalizable lessons

1. **Cross-tab the failure by driver / tier / model before publishing
   a root-cause.** I shipped the wrong root-cause because I didn't
   check whether bucket-B was driver-asymmetric. The 0% / 100%
   copilot-vs-CCH split would have ruled out the parent-repo backup
   theory immediately. *Telemetry first, narrative second.*
2. **The worker's bootstrap writes are pipeline state.** Anything
   `writeWorktreeClaudeSettings`, `provisionOpenclawState`, or any
   future bootstrap helper writes into the worktree is part of the
   diff the apply step will see. Each such write needs an explicit
   apply-step exception OR a write target that's outside the
   worktree's diff surface (`.gitignore`'d, `XDG_CACHE_HOME`, or via
   `git update-index --skip-worktree`).
3. **PR title vs diff mismatch is a real failure mode the pipeline
   doesn't catch.** Worth thinking about whether the apply step
   should refuse to push when the diff doesn't intersect the entry's
   declared `file:` field. Defer until we have a cheap way to
   express "entry's file scope" as a structured constraint that
   `apply-workflow-result.ts` can consume.
4. **Similarity across PRs is a signal.** If you ever see N
   autonomous-swarm PRs with the same one-file diff and divergent
   titles, do not merge any of them until you understand what's
   common. (My initial discovery path was right; the wrong-cause
   only got published because I stopped tracing one step too soon.)
5. **Untracked artifacts in `.claude/`, `.git/`, dotfiles, etc. are
   still not benign** — `git add -A` will sweep them up. PR #122's
   pre-flight scrub is correct hygiene even though the original
   bucket-B wasn't caused by it.

## See also

- **PR #123 — the actual fix.** Apply-step revert of
  `.claude/settings.json` + T2 reroute to copilot.
- PR #121 — the pre-flight scrub backlog entry (correct hygiene,
  wrong target for this bug).
- PR #122 — the swarm-produced implementation of #121's entry. Real
  meta-recursion: swarm shipped a fix for what we *thought* was the
  bucket-B cause.
- PRs #114, #117, #118, #120 — the four closed contaminated PRs;
  diff in #114 was the byte-level evidence that finally cracked the
  real root cause.
- `apps/runner/src/activity.ts:219` — `writeWorktreeClaudeSettings`,
  the worker write that creates the artifact.
- `apps/runner/src/grooming/apply-workflow-result.ts` —
  where the "tracked diff exists → auto-commit + push" heuristic
  lives, and where PR #123 plants the revert.
- `go/execution-kernel/internal/govhookinstall/install.go` — where
  the (red-herring) `.claude/settings.json.chitin-backup-<ts>`
  filename gets minted by the installer.
