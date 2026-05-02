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

## Root cause

The workspace's `.claude/` directory contained a leftover artifact:

```
.claude/settings.json.chitin-backup-20260429T210349Z
```

This was created by `chitin-kernel install --surface claude-code
--project` on 2026-04-29 — the installer's normal behavior is to back
up the previous settings.json before overwriting. Backups are
append-only and never auto-cleaned.

The dispatcher provisions a worktree off `main` for each dispatch and
spawns the agent in it. The agent's prompt names a specific target
file and tells it not to invent scope. But the apply step
(`grooming/apply-workflow-result.ts`) ran `git add -A` over the entire
worktree to stage anything the agent had touched. The backup file
isn't tracked on `main` — it lives only in the live workspace's
`.claude/` directory. Each dispatch's worktree inherited it as
untracked content.

Critically, on at least four runs, the agent (claude-code-headless)
never touched the actual target file at all — likely the wall_timeout
fired before tool-dispatch completed, or the agent declined to commit.
But because the backup artifact existed in the worktree and `git add
-A` swept it up, the apply step found "tracked diff" content (the
backup file got promoted from untracked to staged), called the
auto-commit fallback path with `defaultCommitMessage()`, and pushed.
The PR opened with a generated title from the entry id — looking, to
the reader, like the agent had done the work.

This is **a security-shaped failure even though no security policy
was bypassed.** The chain says: artifact + permissive `git add -A` +
auto-commit-on-tracked-diff = ship something that looks like the
intended work but isn't.

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

1. **Deleted the artifact** (this commit). Backups created by
   `chitin-kernel install` will continue to land in `.claude/` —
   future hardening is in (2) below.
2. **Filed `dispatcher-preflight-scrub-claude-settings-backup`** as a
   ready entry in `swarm-backlog.md` (PR #121). Pre-flight refuses
   (or auto-scrubs) when any `.claude/settings.json.chitin-backup-*`
   file is present at tick start. Default to refuse unless dogfood
   shows it's a recurring operator burden.
3. **Closed #114, #117, #118, #120** with a comment naming the root
   cause. #114's entry is moot — the SIGKILL fix already shipped in
   slice-7a (PR #99). The other three remain claimable; the next
   swarm tick after (2) lands will pick them up cleanly.

## What we did NOT change (and why)

- **Did not** broaden the `git add -A` heuristic to "only stage files
  named in the entry's `file:` field." Tempting, but it punishes the
  legitimate case where an entry's target generates auxiliary files
  (e.g. updating a generated test fixture). Fix the artifact at the
  source, not the staging step.
- **Did not** force-push reset the four broken branches. Force-push
  is denied by `chitin.yaml`'s `no-force-push` rule, and rightly so —
  rewriting shared history hides this exact kind of failure from
  audit. Closing leaves the bad commits visible in the chain.
- **Did not** mark `wall-timeout-sigkill-propagation` as completed in
  the backlog. The shipped slice-7a code matches the entry's intent,
  but the grooming-pass author should confirm and close the entry
  with a "shipped at PR #99" pointer rather than a silent removal.

## Generalizable lessons

1. **Untracked artifacts in `.claude/`, `.git/`, dotfiles, etc. are
   not benign.** A `git add -A` will sweep them up. The dispatcher's
   pre-flight is now the right place to gate this.
2. **PR title vs diff mismatch is a real failure mode the pipeline
   doesn't catch.** Worth thinking about whether the apply step
   should refuse to push when the diff doesn't intersect the entry's
   declared `file:` field. Defer until we have a cheap way to
   express "entry's file scope" as a structured constraint that
   `apply-workflow-result.ts` can consume.
3. **Similarity across PRs is a signal.** If you ever see N
   autonomous-swarm PRs with the same one-file diff and divergent
   titles, do not merge any of them until you understand what's
   common.
4. **Installer backups are state.** Anything the kernel writes into
   the user's working tree is part of the pipeline's environment.
   If the installer leaves backups, the dispatcher needs an opinion
   about them — and the kernel should probably namespace its backups
   to a path the dispatcher's pre-flight can scrub generically (e.g.
   `~/.cache/chitin/install-backups/<surface>/<ts>`) rather than
   leaving them in `.claude/` next to live config.

## See also

- PR #121 — the pre-flight scrub backlog entry.
- PRs #114, #117, #118, #120 — the four closed contaminated PRs;
  comments name the root cause.
- `apps/temporal-worker/src/grooming/apply-workflow-result.ts` —
  where the "tracked diff exists → auto-commit + push" heuristic
  lives.
- `go/execution-kernel/internal/govhookinstall/install.go` — where
  the `.claude/settings.json.chitin-backup-<ts>` filename is minted
  by the installer.
