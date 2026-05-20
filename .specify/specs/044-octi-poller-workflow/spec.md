# 044 — Octi poller workflow (replaces `clawta-poller`)

> Parent: spec 040 (Octi scaffolding).
> Depends on: 043 (dispatch workflow).
> Migration target: `swarm/bin/clawta-poller` + install script
> `swarm/bin/install-clawta-poller.sh`.

## Summary

Replace the standalone `clawta-poller` cron with a Temporal Cron
Schedule that runs `PollerWorkflow` on a fixed cadence. Each tick:
read ready tickets from each enabled board, filter for unassigned
tickets that pass dispatch readiness (spec 022), and launch one
`DispatchTicketWorkflow` (spec 043) per ticket as a child workflow.
The poller has no in-process state — every tick is independent,
deterministic, and replayable.

## Ticket refs

- Migration target: `swarm/bin/clawta-poller`,
  `swarm/bin/clawta-poller-safe-tick`,
  `swarm/bin/install-clawta-poller.sh`
- Prior poller governance: spec 009 (poller respects spec-kit),
  spec 017 (poller dependency unblock veto), spec 028 (clawta poller
  phased rollout)
- Dispatch workflow: spec 043 (called per ticket)

## File-system scope

Worker MAY write under:

- `swarm/octi/workflows/poller.go` — `PollerWorkflow`
- `swarm/octi/activities/poller/` — Activity packages
  - `list_ready_tickets.go` — reads kanban for ready+unassigned tickets
  - `validate_readiness.go` — honors spec 022 dispatch readiness +
    spec 017 unblock veto
- `swarm/octi/workflows/poller_test.go` — unit
- `swarm/octi/e2e/poller_e2e_test.go` — **e2e**: end-to-end tick
  produces N child dispatch workflows
- `swarm/octi/cmd/octi-poller-schedule/main.go` — operator CLI to
  install/inspect the Temporal Schedule
- `swarm/bin/install-octi-poller.sh` — installer (replaces
  install-clawta-poller.sh after bake)
- `.specify/specs/044-octi-poller-workflow/**`

Worker MUST NOT write under:

- `swarm/bin/clawta-poller` (legacy, removed only after bake)
- `~/.hermes/cron/jobs.json` (cron entry removed by install script)
- Existing poller install scripts (deprecated, not modified in place)

## Goal

Operator runs `octi-poller-schedule install --board=chitin
--interval=60s`. The Temporal cluster gains a Schedule that fires
`PollerWorkflow` every 60s. Each tick lists ready tickets,
validates each per spec 022 + spec 017, and starts one child
`DispatchTicketWorkflow` per qualifying ticket. The legacy cron
`clawta-poller` is uninstalled by `install-octi-poller.sh
--migrate`. Behavior parity (which tickets get dispatched, in
what order, with what backoff) is verified by `poller_e2e_test.go`
against a fixture corpus.

## Requirements

### R1 — Workflow signature + cadence

```go
func PollerWorkflow(ctx workflow.Context, input PollerInput) error

type PollerInput struct {
    Board                string  `json:"board"`
    MaxDispatchesPerTick int     `json:"max_dispatches_per_tick"` // default 5
    DryRun               bool    `json:"dry_run"`                  // shadow tick
}
```

Cadence is controlled by the Temporal Schedule, not the workflow.
Default cadence: every 60 seconds, matching `clawta-poller`'s
current cron. Operator-configurable.

### R2 — Per-tick algorithm (deterministic)

1. Call `ListReadyTicketsActivity(board)` → ordered slice of
   ticket IDs
2. For each ticket id (in slice order, not map):
   a. Call `ValidateReadinessActivity(ticket_id, board)` → bool +
      reason
   b. If valid, start child workflow
      `DispatchTicketWorkflow(ticket_id, board, ...)`
   c. If `MaxDispatchesPerTick` reached, break
3. Return tick summary: dispatched IDs, skipped IDs with reasons

The workflow does NOT wait for child dispatches to complete — they
run independently. Each child is a separate Temporal workflow with
its own event history.

### R3 — Ordering is deterministic

`ListReadyTicketsActivity` returns tickets sorted by:
1. Priority (P0 > P1 > P2 > P3)
2. `created_at` ascending (older first)
3. Ticket id ascending (final tie-breaker)

The order is stable across ticks; CI fixture test asserts.

### R4 — Spec 017 unblock veto honored

`ValidateReadinessActivity` honors spec 017's `Blocked until:` veto:
if a ticket's bound spec carries `Blocked until: <condition>`, the
poller skips it. The skip reason
(`spec_017_unblock_veto_active`) appears in the tick summary.

### R5 — Spec 022 readiness contract honored

Same Activity honors spec 022: missing
`invariants_and_boundaries` block → skip with reason
`spec_022_readiness_failed`.

### R6 — Concurrency cap

`MaxDispatchesPerTick` (default 5) caps the number of dispatches
spawned per tick. Prevents thundering-herd on a board with many
ready tickets. Operator-configurable per board.

### R7 — Idempotency

A child `DispatchTicketWorkflow` is started with workflow ID
`dispatch-<ticket_id>-<tick_seq>`, where **`tick_seq` is the
Temporal Schedule's action sequence number** — the monotonically
increasing counter Temporal assigns to each scheduled firing of
`PollerWorkflow`, available on the workflow as
`workflow.GetInfo(ctx).??` / passed in `PollerInput`. It is
deterministic (assigned by the Schedule, recorded in event
history) and unique per tick. If a ticket was dispatched in the
previous tick (still in-progress), the next tick's start call
returns `WorkflowExecutionAlreadyStartedError` and the poller logs
a structured skip with reason `already_dispatching`. No double-dispatch.

Note: because `tick_seq` differs per tick, the workflow ID is
unique per (ticket, tick) — the `AlreadyStarted` collision is
detected via a deterministic `dispatch-<ticket_id>-*` query, not
by ID equality. The implementation keys idempotency on
`ticket_id` with the most-recent in-flight dispatch, and uses
`tick_seq` only to make each ID unique for Temporal's history.

### R8 — Dry-run mode

`PollerInput.DryRun = true` runs the entire algorithm but skips
the child-workflow start at step 2.b. Outputs the would-have-been
dispatch list. Useful for "what would the next tick do" without
side effects.

### R9 — Multi-board

Operator can install one Schedule per board. `PollerInput.Board`
scopes the workflow to one board per tick. Cross-board polling is a
separate Schedule, never a single workflow.

### R10 — Migration cutover

`swarm/bin/install-octi-poller.sh --migrate`:
1. Disables `clawta-poller` cron entry in `~/.hermes/cron/jobs.json`
2. Installs the Temporal Schedule
3. Asserts the Schedule is firing within one cadence interval
4. If assertion fails, restores the cron entry and reports the
   failure loud

Reversible: `--rollback` undoes step 1 + 2.

## Acceptance criteria

1. `octi-poller-schedule install --board=chitin --interval=60s`
   creates a Temporal Schedule visible via `octi-poller-schedule list`.
2. A tick on a fixture board with 3 ready tickets starts 3 child
   `DispatchTicketWorkflow`s.
3. A tick with 10 ready tickets and `MaxDispatchesPerTick=5` starts
   exactly 5 child workflows; remaining 5 surface on the next tick.
4. Ordering: same fixture corpus, run twice, produces identical
   dispatch order (R3 determinism).
5. Spec 017 veto: a fixture ticket bound to a spec with
   `Blocked until: foo` is skipped with reason
   `spec_017_unblock_veto_active`.
6. Spec 022 readiness: a fixture ticket missing
   `invariants_and_boundaries` is skipped with reason
   `spec_022_readiness_failed`.
7. Re-dispatch protection: same ticket dispatched in tick N, still
   in-progress at tick N+1 → tick N+1 skips with
   `already_dispatching`.
8. `--dry-run` produces the dispatch list without starting any
   child workflows; verified by Temporal Schedule listing showing
   zero child runs.
9. `install-octi-poller.sh --migrate` disables the legacy
   `clawta-poller` cron entry and confirms the Schedule fires within
   one interval.
10. `--rollback` restores the legacy cron entry and removes the
    Schedule.
11. **Cross-board boundary (I4)**: a `PollerWorkflow` run with
    `Board=chitin` reads only the chitin kanban DB and starts only
    child dispatches scoped to `Board=chitin`; an e2e test runs the
    poller against a fixture environment with both chitin and
    readybench boards populated and asserts zero readybench DB
    access and zero readybench-scoped child workflows.

## Test coverage

- `swarm/octi/workflows/poller_test.go` — unit: tick algorithm with
  mocked Activities
- `swarm/octi/activities/poller/*_test.go` — unit per Activity
- `swarm/octi/e2e/poller_e2e_test.go` — **e2e**: AC2, AC3, AC4
- `swarm/octi/e2e/poller_veto_test.go` — **e2e**: AC5, AC6
- `swarm/octi/e2e/poller_idempotency_test.go` — **e2e**: AC7
- `swarm/octi/e2e/poller_cross_board_test.go` — **e2e**: AC11
  (cross-board boundary — I4 enforcement)

All files carry `// spec: 044-octi-poller-workflow`.

## Invariants

- **I1**: tick ordering is deterministic across runs (R3).
- **I2**: no double-dispatch — workflow ID per (ticket_id,
  tick_seq) enforces idempotency.
- **I3**: spec 017 + spec 022 vetoes are honored unchanged.
- **I4**: per-board scoping; no cross-board polling in one workflow.
- **I5**: rollback to legacy `clawta-poller` is always possible —
  `--rollback` restores the cron entry.

## Out of scope

- Multi-board single-Schedule (out of R9) — separate Schedules per
  board
- Adaptive cadence (faster polling when many tickets ready) — fixed
  cadence per Schedule, operator-tuned
- Priority-based preemption — handled by `DispatchTicketWorkflow`
  ordering, not poller
- Console UI — Temporal Web UI suffices

## References

- Migration target: `swarm/bin/clawta-poller`,
  `swarm/bin/install-clawta-poller.sh`
- Prior poller specs: 009, 017, 022, 028
- Dispatch child workflow: spec 043
- Temporal Schedules docs:
  https://docs.temporal.io/develop/go/schedules
- Parent: spec 040
