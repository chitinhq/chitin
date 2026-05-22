# Dispatch pipeline runbook

Status: current as of 2026-05-17.

This runbook describes the Chitin swarm dispatch path from a Hermes kanban ticket to a merged PR and closed ticket. It is operational documentation for the transitional `swarm/` tooling that still lives in this repo; it is not kernel product surface.

## Source of truth

- Board state lives in Hermes kanban (`chitin` board unless `--board` / `KANBAN_BOARD` says otherwise).
- Chitin-owned work uses repo-local specs at `.specify/specs/NNN-<slug>/spec.md`.
- Shared/team repos use the workspace overlay described by the workspace constitution.
- Worker branches currently use `agent/*`. `swarm/*` and `clawta/*` are legacy/controller prefixes.
- Runtime scripts must resolve board/repo/default branch from board config or explicit board flags. Do not infer from cwd.

## Pipeline

1. **Spec gate before dispatch**
   - A ticket promoted from `triage`/`blocked` to `ready` must have a merged/tracked spec-kit entry.
   - The gate is bidirectional: it accepts either a ticket body reference such as `Spec: .specify/specs/NNN-slug/spec.md` or a spec file that exactly references the ticket id (`t_1234abcd`).
   - Untracked files in a local checkout do not count. If the live gate appears to pass only because of primary-checkout dirt, stop and clean up.

2. **Watchdog sanity**
   - Board-watchdog may block malformed or unsafe tickets, but it must respect valid spec-kit entries.
   - `loop_detected` tickets are never auto-unblocked.
   - Operator-owned/red-assigned tickets are sacred unless red explicitly authorizes mutation in-thread.

3. **Controller selection**
   - `swarm/bin/swarm-controller` reads board config, filters dispatchable tickets, routes `clawta` assignments, and dispatches bounded batches.
   - Tracking epics / `no_dispatch` tickets must not dispatch.
   - Missing-spec tickets must fail the spec-kit gate with an explicit reason.

4. **Driver selection**
   - `_pick_driver.py` / driver selection should prefer board-aware configuration and tracked driver metadata.
   - If the picker times out or cannot read driver state, it must fail visibly or use a documented fallback. Do not silently broaden dispatch eligibility without logging.

5. **Worker spawn**
   - Workers create sibling worktrees and branch from the configured default branch.
   - The primary checkout at `~/workspace/chitin` is for read/controller operations only.
   - The branch name carries the ticket suffix, usually `agent/<driver>-<ticket-suffix>`.

6. **Finalize and empty-branch gate**
   - Before push/finalize, the workflow must prove nonzero delta with:

     ```bash
     git rev-list --count origin/$DEFAULT_BRANCH..HEAD
     ```

   - A zero count crashes the ticket with `failure-kind=empty_branch` and evidence instead of pushing a zombie branch.

7. **Push and PR creation**
   - Successful workers push the branch and open a PR.
   - Worker PR bodies should include explicit close intent, for example:

     ```text
     Closes ticket t_1234abcd
     ```

   - Use `Refs t_1234abcd` only when the PR should link without closing the ticket on merge.

8. **Review and merge**
   - Per workspace constitution §10.1, every non-trivial PR needs cross-review from an agent that did not author it.
   - Governance/spec/dispatch/worker-output PRs do not self-merge.
   - Auto-merge, when enabled, is gated by green checks, mergeability, mapped in-progress ticket, and current-head Clawta approval.

9. **Lifecycle close**
   - `swarm/bin/clawta-pr-lifecycle --board chitin --apply --prefix agent/` scans current worker PRs.
   - Exact-match behavior:
     - branch-derived ticket suffix implies close intent;
     - `Closes/Fixes/Resolves t_...` implies close intent;
     - `Refs/References/Ticket/Task/Kanban t_...` links only and must not close.
   - A merged PR with close intent marks the mapped ticket done with the PR URL/number as evidence.

## Known footguns

- **Prefix drift:** default lifecycle prefixes historically included only `swarm/` and `clawta/`; current worker branches are `agent/*`. If lifecycle appears idle, pass `--prefix agent/` and fix the default before relying on cron.
- **Primary-checkout dirt:** untracked specs in `~/workspace/chitin` can make file-based gates appear to pass before review. Preserve useful drafts in a worktree PR, then remove untracked primary copies.
- **Lazy ticket matching:** arbitrary comments or `Refs` text must not close tickets. Close only from trusted branch/body close intent.
- **Runtime drift:** controller, bridge, lifecycle, watchdog prompt, and workflows are live behavior. Install/verify from tracked source and treat deployed drift as a bug.

## Minimal verification commands

```bash
# Inspect ready/in-progress tickets.
hermes kanban --board chitin list

# Dry-run lifecycle classification for agent worker PRs.
./swarm/bin/clawta-pr-lifecycle --board chitin --prefix agent/

# Apply lifecycle closure for already-merged worker PRs.
./swarm/bin/clawta-pr-lifecycle --board chitin --apply --prefix agent/

# Check a worker branch is non-empty before finalize.
git rev-list --count origin/main..HEAD
```

## Escalation

Use the bus channel before waking red. Include ticket id, PR/branch, command, exact output, and whether the failure is spec gate, watchdog, controller, driver picker, worker finalize, PR checks, or lifecycle close.
