# 025 — Dispatch atomicity invariant (block↔close single-owner)

> Implements the atomicity invariant Clawta + Ares agreed on in
> the spec 020 RFC (msgs 2828 + 2859):
>
> > *"block→close must be single-owner — no unblock/re-dispatch
> > while finalization is in flight."*
>
> The PR-liveness invariant (other half of the pair) shipped in
> chitin PR #739. This spec ships the atomicity half.
>
> Authorship contract: red-implemented + Clawta-ratified per the
> 3-way overnight agreement (Clawta's C-ii choice). Clawta's
> ratification = lane execution under the 3-way contract.

## Ticket refs

- Chitin task `t_f391ba00` — "Clawta: dispatch atomicity invariant"
- Spec 020 RFC ratification: kanban comment on `t_42010063` (2026-05-18 05:27:51 UTC)
- Spec 022 (PR #744) — the broader dispatch readiness contract;
  this spec is Gate-3 of that.

## File-system scope

Worker MAY write under:
- `swarm/bin/dispatch-finalize-lock.sh`
- `swarm/workflows/kanban-dispatch.lobster`
- `docs/governance-setup-extras/kanban-dispatch.lobster`
- `scripts/kanban-flow`
- `swarm/tests/test_dispatch_atomicity_invariant.py`
- `swarm/tests/test_kanban_dispatch_zero_commit_regression.py`
- `.specify/specs/025-dispatch-atomicity-invariant/**`
- `.specify/specs/INDEX.md`

Worker MUST NOT write under:
- `src/**`
- `routes/**`
- `services/**`
- `lib/**`
- `controllers/**`
- `models/**`
- `go/**`

Any other path under `chitin/` requires a spec amendment before dispatch.

## Goal

A finalize_dispatch in flight cannot be interrupted by a concurrent
unblock; an unblock in flight cannot be interrupted by a concurrent
finalize. The TOCTOU race in finalize_dispatch's existing
"if CURRENT_STATUS == blocked then refuse" guard is eliminated by
holding a lock across the entire block-or-close decision.

## Problem (current code)

`kanban-dispatch.lobster:finalize_dispatch` reads `CURRENT_STATUS`
via `hermes kanban show`, then proceeds to push + open PR if not
blocked. Between the read and the push, another process can run
`kanban-flow unblock <id>` and flip status — by the time finalize
notices, it's already pushing a branch the operator blocked.

The compounding observed in spec 020 RFC msg 2826 was:
1. PR-liveness invariant: poller re-associates a closed PR → ticket
   reopens (this half shipped in chitin PR #739)
2. **Atomicity invariant**: while finalize is mid-cleanup, the
   re-association unblocks the ticket, finalize doesn't see the
   block, pushes anyway → destructive merge surface

## Lock surface

Per-ticket-id file lock at:

```
~/.chitin/locks/dispatch-{ticket_id}.lock
```

Acquired with `flock --exclusive --nonblock` (matching the pattern
in `services/agent-bus/discord_mirror.py:cmd_poll_all`, spec 023
R6 — locking infrastructure is consistent across the codebase).

Lock scope:
- `finalize_dispatch` holds the lock from its first `hermes kanban
  show` through the last write (PR-open, comment, status flip)
- `kanban-flow unblock` holds the lock from its `assert_status` check
  through `flip_status` + `set_assignee` + `emit_transition`

Helper script `swarm/bin/dispatch-finalize-lock.sh` encapsulates
acquire/release + the wait-or-fail strategy (FAIL FAST: if the lock
is held, the caller exits non-zero with a named error; that caller
will retry on its own cron tick).

## Requirements

- **R1**: `swarm/bin/dispatch-finalize-lock.sh acquire <ticket_id>`
  uses `flock -nE 75 <lockfile>` (E=75 is the conventional fail-not-
  acquired exit code). Lockfile is created in `~/.chitin/locks/` with
  parent-dir created if missing.
- **R2**: `finalize_dispatch` in lobster acquires the lock at the
  start of the step. If acquisition fails (concurrent unblock or
  finalize), the step exits 0 with a known message
  `🦞 ${ticket_id}: finalize skipped; lock held by concurrent
  finalize/unblock` and the cron retries.
- **R3**: `kanban-flow unblock <id>` acquires the lock at the start
  of the command. If acquisition fails, exits with an explicit error
  message + exit code 75. The operator/cron retries.
- **R4**: Lock is released on normal exit AND on SIGTERM/SIGINT
  (use `trap` in shell wrappers; fcntl auto-releases on fd close).
- **R5**: A new integration regression test in
  `swarm/tests/test_dispatch_atomicity_invariant.py` exercises the
  race: spawn 2 subprocesses, one running a fake-finalize that
  sleeps 200ms inside the lock, the other running unblock; assert
  only one completes, the other gets the named error.

## Test coverage

### Why integration + static-analysis (not browser-e2e)

The end-to-end surface IS the kanban-flow + lobster code path on a
real (test-fixture) kanban DB with real subprocesses. There is no
browser/HTTP boundary. The authentic tests are:
1. Static-analysis: lobster + kanban-flow text contains the lock
   acquire/release in the right places.
2. Integration: 2 subprocesses + real flock + assertion on
   serialization.

This is the same justification pattern as spec 018 + spec 023.

| Spec AC | Test case | What breaks if removed |
|---------|-----------|------------------------|
| R1 helper exists + executable | `test_lock_helper_present_and_executable` | The other tests don't have anything to call |
| R2 lobster finalize acquires lock | `test_finalize_dispatch_acquires_lock` (grep lobster for the acquire call before the `hermes kanban show`) | Drift: race re-opens |
| R3 kanban-flow unblock acquires lock | `test_kanban_flow_unblock_acquires_lock` (grep script for the acquire call before `flip_status`) | Concurrent unblock during finalize sneaks through |
| R5 integration race serialized | `test_concurrent_unblock_during_finalize_serializes` (2 subprocesses; assert only one wins; the other gets exit 75) | The actual atomicity is unverified at runtime |
| Mirror sync | `test_workflow_mirror_matches_canonical` (extends existing zero-commit regression test) | docs/governance-setup-extras drifts |

## Acceptance Criteria

- **AC1**: A worker finalizing PR push for ticket `t_X` cannot have
  ticket `t_X` simultaneously unblocked by another process. Either
  finalize completes (then unblock can run), or unblock completes
  (then finalize sees the new state).
- **AC2**: A concurrent attempt returns exit code 75 (lock-not-
  acquired) with a named error string. The caller retries on its
  next cron tick (cron is the natural retry loop).
- **AC3**: Lock files at `~/.chitin/locks/dispatch-*.lock` are
  reclaimed automatically on process death (fcntl behavior). No
  manual cleanup needed.
- **AC4**: 5 named test cases all pass against the live lobster +
  kanban-flow text + a temp fixture workspace.

## Invariants

- **inv-1: single owner per ticket per lock.** No matter the
  invocation path, the same lockfile is used (keyed by ticket id).
- **inv-2: fail fast, retry via cron.** No blocking waits. If you
  can't get the lock, you log + exit; the cron cadence is the
  retry. Block-and-wait would deadlock if cron jobs stack.
- **inv-3: lock release is unconditional.** Trap on shell exit; fd
  close releases. A crashed shell does NOT leak the lock.

## Out of scope

- Cross-host locking (single operator box; multi-host is octi's lane)
- Lock-fairness (FIFO, etc.) — fail-fast + cron retry suffices
- Lock metrics / observability dashboard (sentinel may grow this)
- Replacing the PR-liveness guard (separate; already shipped)

## Cross-spec

- **Spec 020 §1.2**: this spec's test layer is integration with the
  `### Why integration + static-analysis` justification subsection.
- **Spec 022**: the broader dispatch-readiness contract. Spec 025
  is Gate-3 ("atomicity at the boundary"). Spec 022 still pending
  operator ratification.
- **Spec 023 R6**: locking pattern (`fcntl.LOCK_EX | LOCK_NB`)
  reused for consistency.

## Why this spec exists (the retro)

Three undocumented gates surfaced in the spec 020 + spec 022 cycles
today. Two of three (`owned_orgs` default, hardcoded BOARDS dict)
have been patched. The third — atomicity — is this spec. Pair this
with the already-shipped PR-liveness invariant (#739) and the full
race surface from spec 020 RFC msg 2826 is closed.
