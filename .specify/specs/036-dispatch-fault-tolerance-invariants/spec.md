# 036 — Dispatch fault-tolerance invariants

> Operator call 2026-05-18 ~07:30 EDT after a 9-hour silent-dead
> window in the MVP dispatch pipeline:
>
> > *"We need to spec and e2e test each one. This is getting
> > ridiculous."*
>
> Each "one" = each failure mode that bit the dispatch pipeline
> tonight without a spec or test backing the fix. This spec names
> all four as named invariants + binds each to an e2e/integration
> test. Per spec 020 §1.2 — every behavioral change is spec'd +
> tested.

## Ticket refs

- Operator pain log this session: 9-hour dispatch-dead window
  (22:15 EDT → 07:30 EDT next day) caused by undocumented failure
  modes that hot-patches fixed without spec/test.
- Cross-spec: extends spec 022 (PR #744) gate enumeration with
  *fault-tolerance gates*. Spec 022 covered readiness; 036 covers
  recovery.

## File-system scope

- `swarm/workflows/_pick_driver.py` (the hot-patch from tonight
  lands here properly via this PR)
- `swarm/tests/test_dispatch_fault_tolerance_invariants.py` (new
  — one test per invariant)
- `swarm/bin/dispatch-recovery-doctor.sh` (new — operator-facing
  recovery runbook automation; codifies what red did manually
  tonight)
- `.specify/specs/036-dispatch-fault-tolerance-invariants/**`

## The four invariants

### Inv-1: Classify-step robustness

The `classify` step in `kanban-dispatch.lobster` invokes the
openclaw-agent CLI to produce a JSON classification of a ticket.
When the gateway is degraded (cached error envelopes, session
lock, etc.), it returns non-JSON text. `_pick_driver.py` reads
that as stdin and crashes at `json.loads(raw)`, taking down the
whole dispatch.

**Invariant**: `_pick_driver.py` MUST NOT crash on non-JSON
stdin. Fallback path: empty classify → deterministic capability
ranking proceeds.

**Test**: `test_pick_driver_tolerates_garbage_stdin` — feed
`_pick_driver.py` stdin containing `"<html>500 internal server
error</html>"` and assert exit 0 with `router_mode=deterministic`
in the output.

### Inv-2: Stale `task_runs` recovery

When a worker dies mid-dispatch (gateway crash, kernel lockdown,
etc.), its `task_runs` row stays `status='running'` forever. The
poller's "incomplete task_run already exists" guard prevents new
dispatches indefinitely. Operator (or recovery tooling) must
explicitly close the stale run.

**Invariant**: When a `task_runs` row has been `status='running'`
for >2× its `max_runtime_seconds` (default 3600 → 2hr threshold),
the next poller pass MUST treat it as stale + mark it `failed`
with `error='stale-detected; auto-closed by poller'`, then
proceed with normal dispatch logic.

**Test**: `test_poller_auto_closes_stale_running_task_runs` —
seed a `task_runs` row with `started_at=now-3hr` + `status='running'`,
run the poller, assert the row is `failed` + new dispatch happens.

### Inv-3: Stale `agent/<driver>-*` local-branch cleanup

When a worker dies after creating a worktree + local branch but
before pushing, the local branch keeps its (potentially destructive)
commits. The lobster's `worktree add -b ... origin/$DEFAULT_BRANCH
|| worktree add ... $BRANCH` falls back to the second form on the
next dispatch, which checks out the STALE local branch — so spec
018's base-freshness check fires correctly but blocks dispatch
indefinitely until operator manually deletes the local branch.

**Invariant**: When the lobster's worktree-add second form would
fall through (existing local branch detected), it MUST instead
delete the local branch + re-attempt the first form (which
creates fresh from origin). Lost worker commits were never pushed;
they're not recoverable anyway.

**Test**: `test_lobster_recovers_stale_local_agent_branch` — set
up a fixture worktree dir, create a local `agent/codex-TEST`
branch with a commit, run the lobster's worktree-setup block,
assert the worktree's HEAD matches `origin/<default_branch>`.

### Inv-4: Openclaw-gateway crash recovery

When the openclaw-gateway service crashes (~/.openclaw process
died, port 18789 not listening), every dispatch hangs at the
classify step. Operator must `systemctl --user restart
openclaw-gateway` to recover.

**Invariant**: A new operator-facing recovery script
`swarm/bin/dispatch-recovery-doctor.sh` checks for the gateway
crash + restarts it idempotently. Cron runs this hourly so
gateway crashes self-heal within an hour.

**Test**: `test_recovery_doctor_detects_dead_gateway_and_restarts`
— mock systemctl + curl health endpoint; assert the doctor
detects port-not-listening + invokes `systemctl --user restart
openclaw-gateway`.

## Test coverage

### Why integration + static-analysis (per spec 020 §1.2 exception)

The end-to-end surface for these four invariants is the **dispatch
code path under failure conditions**. No browser or HTTP boundary
the user crosses. The authentic e2e is:
- Inv-1: `_pick_driver.py` subprocess with garbage stdin (integration)
- Inv-2: real poller against a fixture kanban DB with seeded stale
  task_run (integration)
- Inv-3: real lobster shell against a fixture worktree dir
  (integration — most authentic since the bug is shell-level)
- Inv-4: shell script with mocked systemctl/curl (integration)

This matches the spec 020 §1.2 exception clause: "the artifact IS
the surface" — the dispatch pipeline IS what these invariants
protect; there's no abstraction layer to drive through a browser.

| Invariant | Test case | Lives in |
|-----------|-----------|----------|
| Inv-1 classify robustness | `test_pick_driver_tolerates_garbage_stdin` | `swarm/tests/test_dispatch_fault_tolerance_invariants.py` |
| Inv-2 stale task_runs recovery | `test_poller_auto_closes_stale_running_task_runs` | same |
| Inv-3 stale local branch cleanup | `test_lobster_recovers_stale_local_agent_branch` | same |
| Inv-4 gateway crash recovery | `test_recovery_doctor_detects_dead_gateway_and_restarts` | same |

## Acceptance Criteria

- **AC1**: `_pick_driver.py` returns a valid result envelope when
  stdin is non-JSON or empty (does NOT raise SystemExit or
  JSONDecodeError). Hot-patch from 2026-05-18 07:35 EDT is
  encoded in this PR.
- **AC2**: A poller pass run against a kanban DB with a `task_runs`
  row that's been `running` for >2hr auto-closes it + proceeds
  with dispatch. No operator intervention needed.
- **AC3**: A lobster dispatch into a worktree dir whose local
  `agent/<driver>-<id>` branch exists with stale commits forces
  fresh creation from `origin/<default_branch>`. The HEAD post-
  setup matches `origin/<default_branch>`.
- **AC4**: `dispatch-recovery-doctor.sh` detects a degraded
  openclaw-gateway (port not listening within 5s timeout) and
  restarts the systemd user service.

## Invariants (meta)

- **inv-meta-1**: every dispatch failure mode discovered in
  operation gets retroactively named + spec'd + tested. Reactive
  hot-patches WITHOUT spec/test = a process bug, not a tactic.
- **inv-meta-2**: tests are integration-level by default for
  dispatch invariants (justification subsection above). Browser-
  e2e doesn't apply.

## Out of scope

- LLM classify-step robustness (better prompts, retry logic) —
  upstream; this spec is the *downstream* defense
- A general "all CLI tools tolerate garbage input" framework — if
  other tools have the same shape, file per-tool specs
- Auto-restart of the openclaw-gateway from cron (R4 is operator-
  attended for MVP; cron auto-restart can be a follow-up spec
  once we've validated the doctor pattern)

## Why this spec exists

The 9-hour silent-dead window tonight cost operator visibility +
real ship velocity. Each failure mode was fixable in <5 minutes
once identified — but identification took ~30 minutes of grep +
log-reading because nothing was named or asserted. Spec 020 §1.2
already said "every behavioral change has a spec + test." Tonight
violated that contract. This spec retroactively fixes the
violation.
