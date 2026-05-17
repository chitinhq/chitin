# Dispatch: Respect Board default_branch in Worker Commit Gate

> Spec-kit entry for Chitin ticket `t_f4c7a89f` and PR #728.
> Incident source: ReadyBench board uses `default_branch=swarm`; worker finalization hardcoded `origin/main..HEAD` and failed before worker results could be classified.

## Goal

`spawn_worker_subprocess.py` must evaluate worker commits against the board's configured default branch instead of assuming `origin/main`. ReadyBench dispatches workers from `origin/swarm`; the empty-branch/commit-count gate must use that same base so non-main boards do not fail with ambiguous git revisions or false zero-commit results.

## Acceptance Criteria

- `spawn_worker_subprocess.py` resolves the base ref from `config.default_branch` when present.
- Bare branch names such as `swarm` resolve to `origin/swarm`.
- Already-qualified refs such as `origin/develop` are accepted unchanged.
- If the config has no `default_branch`, resolution falls back to `origin/HEAD` when available, then `origin/main` for legacy behavior.
- `commits_ahead_of_base()` counts commits from the resolved merge-base to `HEAD`, not from hardcoded `origin/main`.
- Worker run summaries pass the full dispatch config into commit-counting so board default branch is visible at finalization time.
- Unit coverage proves `default_branch=swarm` resolves to `origin/swarm` and error paths name the resolved base ref.
- Existing main-default boards remain behavior-compatible.

## Boundaries

- boundary: read-only branch selection — this change reads dispatch config and git refs only; it does not mutate board config.
- boundary: no per-ticket branch override — ticket-specific base branches remain out of scope.
- boundary: no worker scheduling changes — poller task selection, retry policy, and assignee routing are unchanged.
- boundary: no remote changes — this does not add/remap git remotes or change push targets.

## Verification

- `python -m unittest swarm.tests.test_spawn_worker_subprocess`
- Manual ReadyBench dispatch/finalization should no longer emit `fatal: ambiguous argument 'origin/main..HEAD'` when the board default branch is `swarm`.
- For a worker branch based on `origin/swarm`, `commit_count_ahead` should reflect commits ahead of `origin/swarm`, not `origin/main`.

## Rollout Notes

- The production hotfix landed in PR #728 because the dispatch pipeline was blocked.
- This spec records the expected behavior under the #905 "specs for all work" rule and should be referenced by any follow-up dispatch-default-branch repairs.
