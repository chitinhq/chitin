# 043 — Octi dispatch workflow (port `kanban-dispatch.lobster`)

> Parent: spec 040 (Octi scaffolding).
> Depends on: 041 (event mirror), 042 (agent-bus identity).
> Migration target: `~/.openclaw/workflows/kanban-dispatch.lobster`.

## Summary

Port the six-stage kanban dispatch pipeline from Lobster into a
single Temporal Go workflow (`DispatchTicketWorkflow`). Each Lobster
step becomes a Temporal Activity with a typed input/output struct
and explicit retry policy. The workflow runs deterministically,
replays bit-for-bit, mirrors every decision to the chitin event
store per spec 041, and gates every side-effect Activity through
chitin-kernel per spec 040 §R7.

The existing Lobster file is **not** deleted until e2e parity is
proven over 100 consecutive ticket dispatches. Both pipelines run in
parallel during the bake period; spec 040 §R10's tripwire applies.

## Ticket refs

- Migration target: `~/.openclaw/workflows/kanban-dispatch.lobster`
  (6 stages, ~600 lines)
- Related: `swarm/workflows/_pick_driver.py` (becomes an Activity
  unchanged at first; later refactored)
- Existing dispatch governance: spec 018 (dispatch base-freshness),
  spec 022 (dispatch readiness contract), spec 025 (dispatch
  atomicity invariant), spec 036 (fault tolerance invariants)
- The workflow MUST honor all four prior dispatch specs unchanged;
  this spec is a refactor of the runtime, not the policy

## File-system scope

Worker MAY write under:

- `swarm/octi/workflows/dispatch.go` — `DispatchTicketWorkflow`
- `swarm/octi/activities/dispatch/` — six Activity packages, one per
  stage (R2)
- `swarm/octi/workflows/dispatch_test.go` — unit tests
- `swarm/octi/e2e/dispatch_parity_test.go` — **e2e**: parity vs
  Lobster on a fixture corpus of 100 historical tickets
- `swarm/octi/cmd/octi-dispatch/main.go` — operator CLI to submit a
  dispatch workflow (`octi dispatch <ticket_id> [--board=chitin]
  [--force-driver=codex]`)
- `swarm/bin/octi-dispatch` — wrapper script
- `~/.openclaw/workflows/kanban-dispatch.lobster.octi-bake` — symlink
  during parallel-run period (not deleted; lobster remains canonical
  until tripwire)
- `.specify/specs/043-octi-dispatch-workflow/**`

Worker MUST NOT write under:

- `~/.openclaw/workflows/kanban-dispatch.lobster` (canonical lobster
  file — unchanged during bake)
- `swarm/workflows/_pick_driver.py` (called as Activity unchanged
  during bake; later refactor is a separate spec)
- `chitin.yaml` (policy unchanged)

## Goal

`octi dispatch t_<id>` produces an identical dispatch outcome to
today's Clawta/Lobster pipeline — same driver picked, same model,
same worker spawn args, same kanban transitions — but as a single
replayable Temporal workflow with one OctiEvent stream and one
chitin gate trail per stage. e2e parity holds for ≥99 of 100
historical tickets in the bake fixture; the 1 allowed divergence is
explicit-audit-and-document, not silent.

## Requirements

### R1 — Workflow signature

```go
func DispatchTicketWorkflow(
    ctx workflow.Context,
    input DispatchInput,
) (DispatchResult, error)

type DispatchInput struct {
    TicketID    string  `json:"ticket_id"`     // e.g. "t_8bda2f95"
    Board       string  `json:"board"`         // "chitin" | "readybench" | ...
    ForceDriver string  `json:"force_driver,omitempty"` // smoke override
    OperatorID  string  `json:"operator_id"`   // who triggered this
}

type DispatchResult struct {
    Driver        string `json:"driver"`         // chosen driver
    Model         string `json:"model"`          // chosen model
    WorkerJobID   string `json:"worker_job_id"`  // spawned-worker id
    BranchName    string `json:"branch_name"`    // e.g. "agent/codex-t_8bda2f95"
    StageDecisions []StageDecision `json:"stage_decisions"`
}
```

`StageDecision` is the structured-event record for each of the six
stages (R2). It mirrors verbatim to the OctiEvent stream per spec 041.

### R2 — Six Activities, one per Lobster stage

| # | Activity | Replaces Lobster step | Side effects |
|---|---|---|---|
| 1 | `ResolveBoardConfigActivity` | `resolve_board` | reads chitin-kernel board-config |
| 2 | `FetchTicketActivity` | `fetch_ticket` | reads kanban sqlite |
| 3 | `ClassifyTicketActivity` | `classify` | LLM call via Clawta |
| 4 | `PickDriverActivity` | `pick_driver` | wraps `swarm/workflows/_pick_driver.py` subprocess |
| 5 | `SpawnWorkerActivity` | `spawn_worker` | wraps `swarm/workflows/spawn_worker_subprocess.py` |
| 6 | `FinalizeActivity` | `finalize` | kanban transition + agent-bus post + Discord mirror |

Each Activity:
- Has a typed input struct + typed output struct (no `map[string]any`)
- Gates through chitin-kernel before any side effect (spec 040 §R7)
- Emits a `StageDecision` that mirrors to the OctiEvent stream
- Has an explicit retry policy with bounded attempts (R5)

### R3 — Determinism

Workflow code:
- Reads time only via `workflow.Now(ctx)`
- Sleeps only via `workflow.Sleep(ctx, ...)`
- Branches based ONLY on Activity results, never on local clock or
  random
- Iterates the stage list in fixed order (slice, not map)
- `workflowcheck` passes on `swarm/octi/workflows/dispatch.go`

### R4 — Retry policy (per-Activity)

| Activity | MaxAttempts | InitialInterval | BackoffCoefficient | RetryableErrors |
|---|---|---|---|---|
| ResolveBoardConfig | 3 | 1s | 2.0 | network, sqlite-busy |
| FetchTicket | 3 | 1s | 2.0 | sqlite-busy |
| Classify | 5 | 10s | 2.0 | LLM-timeout, rate-limit |
| PickDriver | 3 | 5s | 2.0 | LLM-timeout (if LLM path), subprocess-crash |
| SpawnWorker | 1 | n/a | n/a | none — spawn is destructive; failure means human review |
| Finalize | 5 | 2s | 2.0 | sqlite-busy, Discord rate-limit |

`SpawnWorker` is single-attempt by design — a half-spawned worker
must surface to operator, not be silently retried (spec 025
atomicity).

### R5 — Honor prior dispatch specs

The workflow MUST NOT change behavior governed by:
- Spec 018 (base-freshness): `SpawnWorkerActivity` runs the
  base-freshness check unchanged
- Spec 022 (readiness contract): `FetchTicketActivity` validates
  `invariants_and_boundaries` block present, fails fast otherwise
- Spec 025 (atomicity): `SpawnWorkerActivity` is single-attempt
  (R4)
- Spec 036 (fault tolerance invariants): the Activity retry policies
  (R4) match or exceed the prior Lobster fault-tolerance posture

Each of the four specs gets an explicit reference in the relevant
Activity's doc comment.

### R6 — Parallel bake period

For the first 30 days after merge, every Lobster dispatch ALSO
triggers `DispatchTicketWorkflow` in shadow mode (`octi.shadow=true`
input flag). The shadow workflow does NOT mutate kanban or spawn
workers — its Activities short-circuit at the side-effect boundary
and only emit StageDecisions. The bake harness
(`swarm/octi/cmd/octi-dispatch-bake/`) compares shadow output to
canonical Lobster output and reports divergences.

Tripwire to delete the Lobster file: 99/100 consecutive shadow
runs match canonical exactly, AND zero divergences are
operator-rated as "real bug in Octi" (vs Lobster artifact).

### R7 — Force-driver override is honored

`DispatchInput.ForceDriver`, when non-empty, bypasses
`ClassifyTicketActivity` and `PickDriverActivity` entirely and
proceeds directly to `SpawnWorkerActivity` with the forced driver.
This preserves
`scripts/smoke-hermes-clawta-chain.sh`'s deterministic smoke path.

### R8 — Board scoping

`DispatchInput.Board` selects which kanban DB (per spec 003 kanban
isolation) and which workspace_root (per spec 030 multi-repo support).
The workflow MUST NOT cross board boundaries during a single
dispatch.

### R9 — Discord + agent-bus notifications

`FinalizeActivity` posts a single agent-bus thread message
announcing the dispatch outcome (driver, model, branch, ticket id).
The post uses the spec-042 identity contract — the message includes
the OctiEvent run_id as anchor reference for downstream replies.

### R10 — Operator CLI

```
octi dispatch <ticket_id> [--board=chitin] [--force-driver=codex]
                          [--shadow]
                          [--operator-id=red]
```

`--shadow` runs the workflow without side effects (same as R6's
bake mode). Useful for "what would Octi pick for this ticket"
without committing.

## Acceptance criteria

1. `octi dispatch t_<id> --board=chitin` runs to completion on a
   fixture ticket and produces a DispatchResult identical (on
   driver, model, branch) to the canonical Lobster output.
2. `workflowcheck ./swarm/octi/workflows/dispatch.go` passes.
3. Every Activity in `swarm/octi/activities/dispatch/` issues a
   chitin gate evaluation before its side effect; CI grep gate
   (spec 040 §R7) passes.
4. OctiEvent stream contains exactly six `activity.completed`
   records per successful dispatch (one per stage), in the
   expected order.
5. e2e test `dispatch_parity_test.go` runs the workflow against a
   fixture corpus of 100 historical tickets in shadow mode;
   asserts ≥99 match canonical Lobster output, with any divergence
   producing a structured report.
6. Force-driver override path (`--force-driver=codex`) skips
   classify + pick_driver Activities; OctiEvent stream shows only
   stages 1, 2, 5, 6.
7. SpawnWorkerActivity is single-attempt; a forced Activity failure
   in test surfaces as workflow failure, not retry.
8. Replay test (`swarm/octi/e2e/dispatch_replay_test.go`) replays a
   completed dispatch from OctiEvent mirror alone (spec 041 §R6) and
   re-derives the same DispatchResult.
9. Spec references in source: `dispatch.go` doc comment references
   specs 018, 022, 025, 036; each Activity's doc comment references
   the specific spec it honors.
10. Tripwire condition documented and observable: a status command
    `octi dispatch bake-status` reports current shadow match rate and
    the 99/100 threshold.
11. **Cross-board boundary (I5)**: a `DispatchTicketWorkflow` run
    with `Board=chitin` touches ONLY the chitin kanban DB and
    chitin workspace_root; an e2e test runs a dispatch with a
    fixture ticket id that also exists on the readybench board and
    asserts no readybench DB read/write occurs (verified by
    per-DB access trace). A dispatch whose ticket id resolves on a
    different board than `Board` fails fast with
    `cross_board_violation`.

## Test coverage

- `swarm/octi/workflows/dispatch_test.go` — unit: workflow logic
  with all Activities mocked; covers force-driver, shadow, error
  paths
- `swarm/octi/activities/dispatch/*_test.go` — unit per Activity
- `swarm/octi/e2e/dispatch_parity_test.go` — **e2e**: AC1, AC5
- `swarm/octi/e2e/dispatch_replay_test.go` — **e2e**: AC8
- `swarm/octi/e2e/dispatch_spawn_atomicity_test.go` — **e2e**: AC7
  (single-attempt invariant)
- `swarm/octi/e2e/dispatch_cross_board_test.go` — **e2e**: AC11
  (cross-board boundary — I5 enforcement)

All files carry `// spec: 043-octi-dispatch-workflow`.

## Invariants

- **I1**: dispatch is bit-for-bit replayable from OctiEvent mirror.
- **I2**: SpawnWorker is single-attempt; spec 025 atomicity preserved.
- **I3**: workflow honors specs 018, 022, 025, 036 unchanged.
- **I4**: shadow mode has zero side effects; CI asserts no kanban
  writes, no worker spawns, no Discord posts when `octi.shadow=true`.
- **I5**: cross-board dispatch is forbidden — `Board` is per-workflow,
  not per-Activity.

## Out of scope

- Refactoring `_pick_driver.py` into native Go — Activity wraps it
  as subprocess; refactor is a later spec
- New routing heuristics or model choices — policy unchanged
- Console UI for dispatch monitoring — Temporal Web UI suffices for
  bake
- Multi-ticket batch dispatch — single ticket per workflow run; batch
  is a parent workflow concern

## References

- Migration target: `~/.openclaw/workflows/kanban-dispatch.lobster`
- Subprocess unchanged: `swarm/workflows/_pick_driver.py`,
  `swarm/workflows/spawn_worker_subprocess.py`
- Prior dispatch governance: specs 018, 022, 025, 036
- Multi-board scoping: spec 003 (kanban isolation), spec 030 (multi-repo)
- Parent: spec 040; depends on specs 041, 042
