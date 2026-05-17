# 018 — Dispatch base-freshness invariant

> Spec-kit entry for the readybench MVP day-1 incident: re-dispatched worker
> `t_5f18463a` produced a branch (`agent/codex-5f18463a` @ `4aa649d`) that
> would have deleted the entire `apps/portal/` tree on merge because the
> dispatch reused a stale worktree based on `af771d3` — committed before
> PR #915 landed on `origin/swarm`.

## Ticket refs

- Readybench `t_5f18463a` (Portal Overview tab) — surfaced the bug.
- Workspace chitin task #64 — operator log.

## File-system scope

- `swarm/workflows/kanban-dispatch.lobster`
- `swarm/tests/**`
- `.specify/specs/018-dispatch-base-freshness/**`

## Goal

Every worker spawn — first dispatch OR re-dispatch — MUST run on a
worktree whose base matches `origin/<default_branch>` as of spawn time.
Stale base must fail loud, not silently destroy newer commits.

## Problem

`swarm/workflows/kanban-dispatch.lobster` ~L327–L335 creates the
worktree like this:

```sh
WORKTREE_DIR="$HOME/.cache/chitin/swarm-worktrees/${DRIVER}-${TICKET_ID}"
BRANCH="agent/${DRIVER}-${TICKET_ID_SHORT}"
if [[ ! -d "$WORKTREE_DIR" ]]; then
  git -C "$CHITIN_REPO" worktree add -b "$BRANCH" "$WORKTREE_DIR" "origin/$DEFAULT_BRANCH" 2>&1 \
    || git -C "$CHITIN_REPO" worktree add "$WORKTREE_DIR" "$BRANCH" 2>&1
fi
```

Two failure modes:

1. **Re-dispatch case**: `$WORKTREE_DIR` already exists from a previous
   dispatch. The `if [[ ! -d ]]` block is skipped, so the worker starts
   on whatever stale branch + commits the previous run left behind. No
   fetch, no reset. Result: the worker rebuilds against a base from
   hours/days ago, and any new code on `origin/<default_branch>` is
   absent — so when the worker commits + finalize pushes, the branch
   represents an upside-down view of the integration line.

2. **First-dispatch case**: `worktree add ... origin/$DEFAULT_BRANCH`
   does NOT fetch first. If the operator's local `origin/<default_branch>`
   ref is behind the actual remote, the worker still gets stale base.

## Requirements

- **R1**: Before every worker spawn — including re-dispatch — the
  pipeline MUST `git -C "$CHITIN_REPO" fetch --quiet origin "$DEFAULT_BRANCH"`.
- **R2**: In the re-dispatch case (worktree exists), the pipeline MUST
  `git -C "$WORKTREE_DIR" reset --hard "origin/$DEFAULT_BRANCH"` AND
  `git -C "$WORKTREE_DIR" clean -fd` to restore base-freshness.
  Branch ref `agent/$DRIVER-$TICKET_ID_SHORT` is force-reset to the
  fresh base. (This is the chosen recovery path — the alternative
  "delete worktree + recreate" is reserved for an explicit
  `--reset-worktree` operator flag in a follow-up if needed.)
- **R3**: After the worktree is ready, the pipeline MUST verify
  `git -C "$WORKTREE_DIR" rev-parse HEAD == origin/$DEFAULT_BRANCH` and
  fail loud (spawn aborts, ticket left in current state) if not.
- **R4**: A one-line summary of the base state (`base=origin/<branch>@<sha7>`)
  MUST be emitted to the dispatch log before the model starts, so retros
  can pinpoint "what base did this worker run on."
- **R5**: A regression test in `swarm/tests/` must simulate a
  re-dispatch with a stale worktree (worktree HEAD older than
  `origin/<default_branch>`) and assert the pipeline refreshes base
  before spawn.

## Acceptance Criteria

- AC1: After this lands, dispatching a ticket whose worktree already
  exists at a stale commit logs `[base-freshness] reset
  $WORKTREE_DIR → origin/<default_branch>@<sha7>` and the worker
  starts on the fresh sha.
- AC2: If `git fetch` fails (network down, auth missing), spawn aborts
  with `spawn_worker: base-freshness fetch failed` and ticket state is
  unchanged.
- AC3: The regression test exercises the re-dispatch case end-to-end
  and asserts no commits from the prior dispatch leak into the fresh
  base. Test seeds a worktree at `HEAD~3` and confirms the post-spawn
  HEAD matches `origin/<default_branch>`.
- AC4: Manual re-dispatch of `t_5f18463a` AFTER this lands produces a
  branch whose merge-base with `origin/swarm` is `origin/swarm` itself
  (i.e., not destructive). Documented in the PR description.

## Out of scope

- Auto-rebasing the worker's own commits onto the new base when the
  worktree contains unmerged changes from a prior run. This spec
  prefers `reset --hard` because in the re-dispatch case those prior
  commits are exactly what we want to discard — they're already pushed
  as `agent/$DRIVER-$TICKET` and either landed in a PR or were
  abandoned. Future spec can introduce a `--preserve-prior` flag.
- Cross-board worktree namespace collisions (multiple boards on same
  ticket ID). Tracked separately.

## Invariant

> For all dispatches D: at the moment D's worker begins executing,
> `git -C $worktree(D) rev-parse HEAD == git -C $repo(D) rev-parse origin/$default_branch(D)`.
