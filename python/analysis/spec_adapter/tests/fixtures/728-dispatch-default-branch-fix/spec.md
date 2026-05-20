# Dispatch: Respect Board default_branch in Worker Commit Gate

> Spec-kit entry for Chitin ticket `t_f4c7a89f`.
> Incident source: ReadyBench board uses `default_branch=swarm`.

## Goal

`spawn_worker_subprocess.py` must evaluate worker commits against the
board's configured default branch instead of assuming `origin/main`.

## Acceptance Criteria

- AC1: `spawn_worker_subprocess.py` resolves the base ref from config.
- AC2: Bare branch names such as `swarm` resolve to `origin/swarm`.
- AC3: Already-qualified refs accepted unchanged.
- AC4: Fallback to `origin/HEAD` then `origin/main` for legacy.
- AC5: Worker run summaries pass the full dispatch config.
- AC6: Unit coverage proves `default_branch=swarm` resolves.
- AC7: Existing main-default boards remain compatible.

## Boundaries

- boundary: read-only branch selection — reads dispatch config and git
  refs only; does not mutate.
- boundary: no per-ticket branch override.
- boundary: no worker scheduling changes.

## Verification

- `python -m unittest swarm.tests.test_spawn_worker_subprocess`