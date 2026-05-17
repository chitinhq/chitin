# Dispatch: Enforce File-system Scope and Blocked-ticket Race Guards

> Spec-kit entry for the ReadyBench day-0 wrong-app retro (workspace PR #418).
> Chitin ticket: `t_6dab0529`.

## Goal

Prevent worker output from reaching GitHub when it violates the spec's declared file-system scope, and prevent stale active runs from reopening/pushing work after an operator blocks the ticket.

## File-system scope

- `swarm/workflows/spawn_worker_subprocess.py`
- `swarm/workflows/kanban-dispatch.lobster`
- `docs/governance-setup-extras/kanban-dispatch.lobster`
- `scripts/kanban-flow`
- `swarm/tests/**`
- `.specify/specs/730-path-scope-race-guards/**`

## Acceptance Criteria

- Worker dispatch config carries the board spec root/workspace context needed to resolve `.specify/specs/<slug>/spec.md` for both repo-local Chitin specs and workspace-overlay ReadyBench specs.
- `spawn_worker_subprocess.py` parses a spec's `## File-system scope` section into allow/deny globs.
- After a successful worker run with commits, the helper compares changed files against the declared scope and returns `status=failed`, `exit_reason=path-scope-violation` when any touched file falls outside scope.
- Existing specs with no scope section are not retroactively broken; enforcement activates when a scope section is present.
- Tickets that reference a spec path that cannot be resolved fail loud before PR creation with `exit_reason=path-scope-spec-not-found`.
- Structured worker failures still reach `finalize_dispatch`, so Lobster can block/crash the ticket with detail instead of leaving stale `in_progress` runs.
- Dispatch aborts before spawn if the ticket is no longer `in_progress` after the audit/start step.
- Finalize refuses to push/open a PR if the ticket was blocked or moved out of `in_progress` while the worker was running.
- `kanban-flow block` finalizes the active run when blocking an `in_progress` ticket.

## Boundaries

- boundary: no worker re-dispatch in this PR.
- boundary: no ReadyBench spec amendments in this PR; Ares owns A1.
- boundary: no new dry-run/planning gate; this is a post-spawn safety gate.
- boundary: no change to PR lifecycle merge policy.

## Verification

- `python3 -m unittest swarm.tests.test_spawn_worker_subprocess -v`
- `python3 -m unittest swarm.tests.test_kanban_dispatch_zero_commit_regression -v`
- `python3 -m unittest swarm.tests.test_kanban_flow -v`
- `bash -n scripts/kanban-flow`
