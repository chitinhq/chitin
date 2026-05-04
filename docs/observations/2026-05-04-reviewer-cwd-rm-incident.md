---
date: 2026-05-04
type: post-mortem
scope: temporal-worker reviewer cwd-rm regression — checkout deleted, swarm dark ~3h
active_soul: da Vinci
related:
  - apps/temporal-worker/src/activity.ts
  - https://github.com/chitinhq/chitin/pull/280
  - docs/observations/2026-05-03-low-success-alarm-investigation.md
---

# Reviewer cwd-rm incident — post-mortem

## One-sentence summary

A reviewer-cwd fix that ran R1/R2/R3 reviewers in `repoRoot` (so `gh pr
diff` would work) was paired with the activity's existing
`finally{ rmSync(workDir) }` cleanup; the first reviewer dispatch
deleted `/home/red/workspace/chitin` outright, dropping every systemd
unit with `WorkingDirectory=%h/workspace/chitin` into `200/CHDIR` and
silencing the autonomous swarm for ~3h until manual recovery.

## Timeline

- **~12:30 UTC** — PR #270 (the original reviewer-cwd fix in a worktree)
  set `workDir = repoRoot` for `req.role === 'reviewer'`.
- **~13:00 UTC** — Worker restart picks up the new code. First
  reviewer dispatch fires.
- **First reviewer turn ends** — activity `finally` block runs
  `rmSync(workDir, { recursive: true, force: true })` because
  `useWorktree=false`. `workDir === repoRoot === /home/red/workspace/chitin`.
  Entire monorepo checkout deleted.
- **Subsequent ticks** — `chitin-dispatcher.service`,
  `chitin-pr-event-ingester.service`, `chitin-envelope-rotate.service`
  fail with `200/CHDIR` (`WorkingDirectory=%h/workspace/chitin` resolves
  to a non-existent path). No new programmer dispatches. No new
  reviewer dispatches. Pre-existing in-flight workflows survive only
  via Temporal's history; their activities can't run.
- **~15:00 UTC** — Operator notices "is the swarm in monitor or guard
  or what mode" and pulls on the thread. `chitin-kernel gate status`
  returns `no_policy_found: no chitin.yaml from /home/red upward`.
  `ls /home/red/workspace/chitin` returns `No such file or directory`.
- **~15:10 UTC** — `git clone https://github.com/chitinhq/chitin.git`
  restores the checkout at `main` (`021cf38`). Worker stopped to
  prevent re-deletion.
- **~15:20 UTC** — Root cause located in
  `apps/temporal-worker/src/activity.ts:697`. PR #280 opened with
  `if (!useWorktree && workDir !== repoRoot)` guard plus four other
  unmerged reviewer-pipeline fixes that were stacked on the deleted
  worktree.
- **~15:35 UTC** — PR #280 merged after Copilot review + regression
  test for env-override pathway (#280 was bundled with workflow-isolate
  purity changes that broke `process.env` reads at module load).
- **~15:41 UTC** — Worker + 4 timers restarted. First post-fix tick
  cleanly enqueues review-graphs for #274 + #277, peer-reviewers for
  #274/#277/#278, comment-responders for #274/#277. Swarm
  operational.

## What worked

- **Detection.** Operator caught the silent failure within ~3h via a
  process-of-elimination question ("is the swarm in monitor or guard
  mode?"), not via an alarm. The `200/CHDIR` failures were in the
  journal for hours but not on any dashboard.
- **Recovery.** Re-cloning from `origin/main` was the correct first
  move — there was nothing in the deleted worktree we couldn't
  reconstruct. The orphaned worktree dirs at
  `/home/red/.cache/chitin/swarm-worktrees/` were unaffected because
  they don't depend on the parent checkout's `.git`.
- **Bundle landing.** Five reviewer-pipeline bugs that had been
  stacked across separate worktrees (`chitin-tail-bytes`,
  `chitin-envelope-rotate`) were ported into the fresh clone and
  shipped as one PR (#280). Bundling let CI / Copilot review them
  together rather than fighting through five rebases.

## What didn't

- **The fix author (me) didn't audit cleanup paths when changing
  workDir semantics.** Setting `workDir = repoRoot` for reviewer mode
  was a clean read-side fix. Pairing it with the existing
  `finally{ rmSync(workDir) }` was a write-side disaster. The
  cleanup block had been correct under the previous invariant
  ("workDir is always a tempdir we own"); the new code broke that
  invariant without touching the consumer.
- **No alarm on `200/CHDIR`.** The PR-event-ingester failures were
  visible in `journalctl --user -u chitin-pr-event-ingester` from
  the moment the deletion happened. There's no alert on systemd
  unit start failures piped into chain or Slack.
- **The trap was guard-by-blacklist-friendly.** The fix in #280 is
  `workDir !== repoRoot`, which protects against the specific
  variable name. A future developer who adds `workDir = someExternalDir`
  for a *new* role isn't protected. The structurally-correct fix is
  `workDir.startsWith(tmpdir())` — only rm paths the worker
  unambiguously owns.

## Why it bit so hard

Three independent infra components share `WorkingDirectory=%h/workspace/chitin`:

1. **chitin-dispatcher** — the programmer-side tick that picks
   backlog entries.
2. **chitin-pr-event-ingester** — the post-PR review-graph trigger.
3. **chitin-envelope-rotate** — the cost-envelope auto-rotator.

Once the directory was gone, ALL three units were down, which means:

- No new programmer work.
- No reviewer dispatches even for already-open PRs.
- No envelope rotation, which would have caused a *second* cascade
  (envelope-closed deny-cascade) ~5 minutes after the first
  envelope's TTL.

The blast radius was the entire swarm because the directory is the
universal anchor for systemd's `%h/workspace/chitin` substitution.

## Action items

1. **Structural fix (PR #280).** `workDir !== repoRoot` guard plus
   the four bundled fixes. Merged, restored, verified.
2. **Codify the trap** — saved as feedback memory
   (`feedback_workdir_repo_root_rm_trap.md`) so future-me / future
   contributor can grep for it before reaching for `workDir = X`.
3. **Backlog entry — "alarm on 200/CHDIR systemd failures"** — the
   journal had the signal for hours, the operator didn't see it.
   `chitin-alarm-feeder` should detect `code=exited, status=200/CHDIR`
   and surface it.
4. **Backlog entry — "tighten cleanup ownership check"** — replace
   `workDir !== repoRoot` blacklist with `workDir.startsWith(tmpdir())
   || workDir.startsWith(SWARM_WORKTREES_ROOT)` allowlist. Done as
   a follow-up by the swarm itself.
5. **Backlog entry — "agent-lockdown auto-recovery"** — separate from
   this incident, surfaced by it. The envelope-closed deny-cascade
   from a *prior* outage left `copilot-cli` lockdown'd; same pattern
   as the envelope rotator. A `chitin-agent-unlock` timer should
   age-out stale lockdowns whose denials were infrastructure (cost
   envelope closed, policy file unreadable) rather than agent
   misbehavior.

## Empirical lessons

- **Read-side fixes can have write-side consequences.** The
  `workDir = repoRoot` change was a one-line read-side enabler;
  the cleanup block 90 lines below silently rewrote it into a
  destructive operation. Always grep for every consumer of the
  variable being changed.
- **`%h/workspace/chitin` is a single point of failure.** Five+
  systemd units anchor on it. A future hardening pass could either
  (a) make the path configurable via `Environment=CHITIN_REPO_ROOT=`
  and have units read from that, so a missing repo can be
  re-pointed, or (b) auto-clone-on-missing, so the systemd units
  self-heal.
- **Recovery automation needs to be tested under failure, not
  designed under success.** The envelope rotator (PR #277) was
  designed as "pre-emptively rotate before close." It would not
  have helped here because the *checkout itself* was the failure
  mode. The unit's `WorkingDirectory` was the ceiling.
