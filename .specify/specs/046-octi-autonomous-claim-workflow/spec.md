# 046 — Octi autonomous claim workflow (replaces `autonomous-board-engine.sh`)

> Parent: spec 040 (Octi scaffolding).
> Depends on: 043 (dispatch), 044 (poller).
> Migration target: `~/.hermes/scripts/autonomous-board-engine.sh`.

## Summary

Replace the `autonomous-board-engine` cron (30-minute interval) with
a Temporal Schedule + `AutonomousClaimWorkflow` that walks the
board for unclaimed high-priority (P0/P1) tickets and claims them
for hermes — but only when each ticket passes the spec-driven
readiness gate (spec 022 `invariants_and_boundaries`). Claimed
tickets are then dispatched via the poller's path (spec 044).

The autonomous engine is the swarm's "what should I work on next"
heuristic. Octi turns it from a script-grade routine into a
replayable workflow with explicit veto rules and a recorded
decision per candidate.

## Ticket refs

- Migration target: `~/.hermes/scripts/autonomous-board-engine.sh`
- Prior governance: AGENTS.md "spec queue gate" rule; spec 022
  (readiness contract); spec 017 (unblock veto)

## File-system scope

### MAY write under

- `swarm/octi/workflows/claim.go` — `AutonomousClaimWorkflow`
- `swarm/octi/activities/claim/` — Activity packages
  - `list_unclaimed_candidates.go` — reads kanban for unclaimed P0/P1
  - `score_candidate.go` — applies the heuristic ranking
  - `claim_ticket.go` — kanban assignee update (idempotent)
- `swarm/octi/workflows/claim_test.go` — unit
- `swarm/octi/e2e/claim_e2e_test.go` — **e2e**: fixture board →
  workflow run → expected claim set
- `swarm/octi/cmd/octi-claim-schedule/main.go` — operator CLI
- `swarm/bin/install-octi-claim.sh` — installer
- `.specify/specs/046-octi-autonomous-claim-workflow/**`

### MUST NOT write under

- `autonomous-board-engine.sh` (legacy, removed only after bake)
- Existing cron entries (rewritten by installer)

## Goal

Operator installs the Schedule once per board:
`octi-claim-schedule install --board=chitin --interval=30m
--agent=hermes`. Every tick the workflow ranks unclaimed
candidates, vetoes per spec 017 + 022, and claims the top N for
the given agent. The claim itself is just a kanban assignee update
— dispatch follows on the next poller tick (spec 044). The cron
`autonomous-board-engine.sh` is uninstalled after parity bake.

## Requirements

### R1 — Workflow signature

```go
func AutonomousClaimWorkflow(
    ctx workflow.Context,
    input ClaimInput,
) (ClaimResult, error)

type ClaimInput struct {
    Board       string `json:"board"`
    AgentID     string `json:"agent_id"`        // typically "hermes"
    Priorities  []string `json:"priorities"`    // ["P0","P1"] by default
    MaxClaims   int     `json:"max_claims"`     // default 3 per tick
    DryRun      bool    `json:"dry_run"`
}

type ClaimResult struct {
    Claimed       []ClaimDecision `json:"claimed"`
    SkippedReasons map[string]int `json:"skipped_reasons"` // reason → count
}
```

### R2 — Algorithm (deterministic)

1. `ListUnclaimedCandidatesActivity(board, priorities)` → ordered
   slice of ticket IDs
2. For each candidate (in slice order):
   a. `ValidateReadinessActivity` (reused from spec 044 §R5)
   b. If not ready, record skip reason and continue
   c. `ScoreCandidateActivity` — applies the ranking heuristic
3. After all scored, sort by score descending; stable tie-break by
   ticket id
4. Take top `MaxClaims`; `ClaimTicketActivity` for each
5. Return ClaimResult

### R3 — Scoring heuristic (frozen contract)

```
score = priority_weight + age_bonus + dependency_bonus

priority_weight:
  P0 = 1000
  P1 = 100
  P2 = 10
  P3 = 1

age_bonus = min(7, days_since_created) * 5
  // older tickets get a small boost, capped at 35

dependency_bonus = 50 if ticket unblocks ≥1 other ticket else 0
```

These weights are frozen at v1. Tuning is a separate spec.

### R4 — Veto rules

`ClaimTicketActivity` MUST NOT claim a ticket that:
- Is already assigned (race) → skip with `already_assigned`
- Fails `ValidateReadinessActivity` (spec 022) → skip with
  `spec_022_readiness_failed`
- Has spec 017 unblock veto active → skip with
  `spec_017_unblock_veto_active`
- Is in `triage` status (operator hasn't promoted) → skip with
  `awaiting_operator_promotion`
- Is assigned to `red` historically (recently unassigned but still
  in operator's queue per `metadata.operator_queue=true`) → skip
  with `operator_owned`

Veto reasons are stable strings; consumers can grep.

### R5 — Idempotent claim

`ClaimTicketActivity` performs:
```sql
UPDATE tasks SET assignee = ?, claimed_at = ?
WHERE id = ? AND assignee IS NULL
```

Returns the affected row count. If 0, the ticket was claimed in
parallel by another path (race) — the workflow records
`already_assigned` and continues.

### R6 — Dry-run

`DryRun=true` produces the would-have-been claim list without
executing the kanban writes. Same output structure for diffing.

### R7 — Multi-board

One Schedule per board, matching spec 044's pattern. The workflow
itself is single-board per run.

### R8 — Cadence

Default 30 minutes (matches current cron). Operator-configurable
via Schedule update.

### R9 — Migration cutover

`install-octi-claim.sh --migrate`:
1. Disables `autonomous-board-engine` cron
2. Installs the Temporal Schedule
3. Asserts the Schedule fires within 1 interval
4. `--rollback` reverses

### R10 — Honor existing claim behavior

The veto list (R4) + scoring (R3) MUST match the current
`autonomous-board-engine.sh` decisions for the bake period.
Parity e2e over 100 historical claim cycles asserts ≥99/100 match.

## Acceptance criteria

1. `AutonomousClaimWorkflow` over a fixture board with 5 P0
   candidates, all ready, claims the top 3 (MaxClaims=3) in score
   order.
2. A candidate in `triage` status is skipped with
   `awaiting_operator_promotion`.
3. A candidate with spec 022 readiness failure is skipped with
   `spec_022_readiness_failed`.
4. A candidate already assigned is skipped with `already_assigned`.
5. Score ordering is stable: same fixture, two runs, identical
   claim set + order.
6. Dry-run produces the same decision list with zero kanban
   writes; verified by row-count assertion.
7. Idempotency: race-claim test — two workflow runs against the
   same fixture, the second sees `already_assigned` for the first
   run's claims.
8. Parity e2e ≥99/100 matches against historical
   `autonomous-board-engine.sh` decisions.
9. `--migrate` disables cron + installs Schedule; `--rollback`
   reverses.
10. CI gate: PR changes to scoring weights (R3) require a
    `// spec: 046 R3-weight-change-rationale` comment and an
    accompanying weight-tuning spec reference.

## Test coverage

- `swarm/octi/workflows/claim_test.go` — unit
- `swarm/octi/activities/claim/*_test.go` — unit
- `swarm/octi/e2e/claim_e2e_test.go` — **e2e**: AC1, AC2, AC3,
  AC4
- `swarm/octi/e2e/claim_parity_test.go` — **e2e**: AC8
- `swarm/octi/e2e/claim_idempotency_test.go` — **e2e**: AC7

All files carry `// spec: 046-octi-autonomous-claim-workflow`.

## Invariants

- **I1**: tickets owned by `red` (operator queue) are never claimed
  autonomously.
- **I2**: scoring weights are frozen at v1; changes require spec
  amendment.
- **I3**: claim is idempotent — race-safe.
- **I4**: spec 017 + 022 vetoes are honored.
- **I5**: ranking is deterministic across runs (stable tie-break).

## Out of scope

- Auto-promoting `triage` → `ready` (operator decision)
- Cross-board claiming (one workflow per board)
- Adaptive scoring (ML-tuned weights)
- Console UI

## References

- Migration target: `~/.hermes/scripts/autonomous-board-engine.sh`
- Readiness validation Activity: shared with spec 044
- Spec 017 veto, spec 022 readiness
- Parent: spec 040
