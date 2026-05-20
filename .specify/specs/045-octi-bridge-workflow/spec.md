# 045 — Octi bridge workflow (replaces `hermes-clawta-bridge.py`)

> Parent: spec 040 (Octi scaffolding).
> Depends on: 041 (event mirror), 043 (dispatch workflow).
> Migration target: `~/.hermes/scripts/hermes-clawta-bridge.py` +
> `swarm/workflows/hermes-clawta-bridge.py` + install script
> `swarm/bin/install-hermes-clawta-bridge.sh`.

## Summary

Replace the standalone `hermes-clawta-bridge` cron with a long-lived
Temporal workflow `BridgeWorkflow` that listens for ticket-block
signals and runs the escalation / auto-unblock decision tree
deterministically. The bridge today is a 15-minute cron polling
kanban for stuck tickets, classifying them, and either auto-unblocking
or escalating to the operator via Discord + agent-bus. Octi
makes the same decisions, but as a Signal-driven workflow loop —
bounded work, durable, replayable, every decision recorded.

## Ticket refs

- Migration target: `~/.hermes/scripts/hermes-clawta-bridge.py`,
  `swarm/workflows/hermes-clawta-bridge.py`,
  `swarm/bin/install-hermes-clawta-bridge.sh`
- Prior bridge governance: the `hermes-clawta-bridge` cron itself
  (no formal spec — this is the first spec to govern it)

## File-system scope

Worker MAY write under:

- `swarm/octi/workflows/bridge.go` — `BridgeWorkflow`
- `swarm/octi/activities/bridge/` — Activity packages
  - `classify_block.go` — classifies a blocked ticket (auto-unblock
    candidate vs operator-escalation)
  - `auto_unblock.go` — applies kanban unblock + comment
  - `escalate_to_operator.go` — posts to agent-bus + Discord
    via spec 042 identity contract
- `swarm/octi/workflows/bridge_test.go` — unit
- `swarm/octi/e2e/bridge_e2e_test.go` — **e2e**: blocked ticket →
  workflow signaled → correct branch taken
- `swarm/octi/cmd/octi-bridge-trigger/main.go` — operator CLI to
  manually signal the workflow for a specific ticket (debugging)
- `.specify/specs/045-octi-bridge-workflow/**`

Worker MUST NOT write under:

- Legacy bridge files (kept until bake completes)
- `chitin.yaml` (policy unchanged)

## Goal

When a ticket transitions to `blocked` status (via watchdog or
explicit block), the bridge workflow receives a Signal with the
ticket id + block reason, classifies it, and either applies an
auto-unblock or escalates to the operator. The decision tree
matches the existing `hermes-clawta-bridge.py` behavior for the
bake period, with every classification + action emitted as an
OctiEvent. After bake, the legacy script is removed.

## Requirements

### R1 — Workflow signature + lifecycle

```go
func BridgeWorkflow(ctx workflow.Context, input BridgeInput) error

type BridgeInput struct {
    Board string `json:"board"`
}

// Signal payloads
type TicketBlockedSignal struct {
    TicketID    string `json:"ticket_id"`
    BlockReason string `json:"block_reason"`
    BlockedAt   int64  `json:"blocked_at_unix_ns"`
    BlockedBy   string `json:"blocked_by"` // watchdog | operator | agent
}
```

The workflow is long-lived: started once per board, runs indefinitely
in a `Selector` loop receiving `TicketBlockedSignal`. Each signal
spawns a child workflow `HandleBlockedTicketWorkflow` per ticket so
the parent stays responsive.

`workflow.ContinueAsNew` is used every 1000 signals to keep
event-history size bounded.

### R2 — Child workflow per blocked ticket

```go
func HandleBlockedTicketWorkflow(
    ctx workflow.Context,
    sig TicketBlockedSignal,
) (BridgeDecision, error)

type BridgeDecision struct {
    Action       string  `json:"action"`        // "auto_unblock" | "escalate" | "skip"
    Reason       string  `json:"reason"`
    Notification string  `json:"notification,omitempty"` // bus message id, if posted
}
```

The child workflow:
1. Calls `ClassifyBlockActivity(ticket, reason)`
2. Branches on classifier output:
   - `auto_unblockable` → `AutoUnblockActivity`
   - `needs_operator` → `EscalateToOperatorActivity`
   - `transient` → `skip` with `retry_after` (parent re-signals
     after delay)

### R3 — Classifier honors veto rules

`ClassifyBlockActivity` MUST NOT auto-unblock tickets that:
- Are assigned to `red` (per AGENTS.md anti-pattern: "Never
  auto-unblock tickets assigned to red")
- Were blocked by `watchdog` with reason starting `spec_022_*` or
  `spec_017_*` (those are policy-driven, not transient)
- Have `block_reason` containing `governance` or `policy` (operator
  attention required)

Each veto is a separate branch with a stable reason string.

### R4 — Auto-unblock applies kanban transition + comment

`AutoUnblockActivity`:
1. Reads ticket via kanban CLI
2. Verifies still in `blocked` status (race protection)
3. Transitions to previous status (typically `ready` or
   `in_progress`)
4. Posts a comment: `[octi-bridge] auto-unblocked: <reason>`
5. Returns the new status + comment id

Idempotent: re-running on an already-unblocked ticket is a no-op.

### R5 — Escalation via spec 042 identity

`EscalateToOperatorActivity` posts to agent-bus thread named
`bridge-escalation-<board>` (or creates it):
- Audience: `red`
- Body: ticket id, block reason, classifier reasoning, suggested
  next action
- Uses spec 042 anchor — operator replies route back to the same
  thread, not the channel catchall

### R6 — Determinism in the loop

`BridgeWorkflow` uses `workflow.Selector` to await signals; no
`select` on Go channels. Iteration through pending signals is
ordered by signal arrival time (Temporal guarantee). `Now()` calls
go through `workflow.Now(ctx)`.

### R7 — Bounded history via ContinueAsNew

Every 1000 signals processed, the parent calls `ContinueAsNew` with
the current state (mostly empty — the parent is stateless except
for a signal counter). This keeps the workflow's event history
bounded.

### R8 — Manual trigger

`octi-bridge-trigger <ticket_id> --board=<board>` synthesizes a
`TicketBlockedSignal` and sends it to the running `BridgeWorkflow`.
Useful for testing or operator-initiated re-processing.

### R9 — Migration cutover

`install-octi-bridge.sh --migrate`:
1. Disables `hermes-clawta-bridge` cron in
   `~/.hermes/cron/jobs.json`
2. Starts `BridgeWorkflow` for each enabled board
3. Hooks the kanban-watchdog so it signals the workflow on every
   block (replacing today's "watchdog writes to a file the cron
   reads next tick" path)
4. Asserts the workflow is receiving signals within one minute
5. On assertion fail, restores the cron entry

`--rollback` is symmetric.

### R10 — Honor existing bridge behavior

The classifier reasoning (R3) MUST replicate the current
`hermes-clawta-bridge.py` decision tree for the bake period — same
inputs, same outputs. e2e test asserts parity over a fixture of
100 historical blocked tickets.

## Acceptance criteria

1. `BridgeWorkflow` for board=chitin starts and listens for
   signals; verified via Temporal Web UI.
2. Sending a `TicketBlockedSignal` for a fixture ticket spawns a
   `HandleBlockedTicketWorkflow` child within 1s.
3. A fixture ticket with reason `spec_022_readiness_failed` is
   classified `needs_operator` and an escalation post is created.
4. A fixture ticket with reason `transient_network_timeout` is
   classified `auto_unblockable`, transitioned back to its prior
   status, and a comment is appended.
5. A fixture ticket assigned to `red` is NEVER auto-unblocked,
   regardless of reason.
6. Escalation post uses spec 042 anchor; subsequent Discord reply
   routes back to the same bus thread.
7. After 1000 signals, parent calls `ContinueAsNew`; new run
   inherits empty state cleanly.
8. `octi-bridge-trigger t_<id>` produces a signaled workflow run.
9. Parity e2e (`bridge_parity_test.go`) over 100 historical blocked
   tickets matches `hermes-clawta-bridge.py` decisions ≥99/100.
10. `--migrate` disables cron + starts workflow; `--rollback`
    reverses.

## Test coverage

- `swarm/octi/workflows/bridge_test.go` — unit: signal handling,
  child spawn, ContinueAsNew
- `swarm/octi/activities/bridge/*_test.go` — unit per Activity,
  with mocked kanban + agent-bus
- `swarm/octi/e2e/bridge_e2e_test.go` — **e2e**: AC2, AC3, AC4, AC5
- `swarm/octi/e2e/bridge_parity_test.go` — **e2e**: AC9
- `swarm/octi/e2e/bridge_continue_as_new_test.go` — **e2e**: AC7

All files carry `// spec: 045-octi-bridge-workflow`.

## Invariants

- **I1**: tickets assigned to `red` are never auto-unblocked.
- **I2**: spec 017 / spec 022 blocked reasons always escalate.
- **I3**: every classification + action is mirrored as an OctiEvent.
- **I4**: parent workflow history is bounded by `ContinueAsNew`.
- **I5**: idempotent auto-unblock — same signal twice is a no-op
  on the second.

## Out of scope

- New classifier logic (changes to who-gets-auto-unblocked) —
  policy stays unchanged in this spec; a later spec can extend
- Auto-fix attempts (re-running the worker) — bridge is for
  block decisions, not retry; spec 043 dispatch retry policy
  handles worker-level retry
- Cross-board bridge — one workflow per board

## References

- Migration target: `~/.hermes/scripts/hermes-clawta-bridge.py`
- Discord routing for escalations: spec 042
- Event mirror: spec 041
- Parent: spec 040
