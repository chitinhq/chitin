# Implementation Plan: 066 Grooming Telemetry

**Branch**: `066-grooming-telemetry` | **Date**: 2026-05-20 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `.specify/specs/066-grooming-telemetry/spec.md`

## Summary

Add structured, parseable grooming decision telemetry and a board drift report
that mines Hermes kanban databases for health signals. The implementation has
two delivery slices: first a sanctioned emitter for grooming decision comments,
then a drift analysis CLI that reads `task_events` and `task_comments` across
Hermes boards.

## Technical Context

**Language/Version**: Python 3 for swarm tooling; shell wrappers where needed.

**Primary Dependencies**: Python standard library (`argparse`, `json`,
`sqlite3`, `statistics`, `pathlib`, `datetime`); existing `hermes kanban`
command for writes.

**Storage**: Read-only access to `~/.hermes/kanban/boards/*/kanban.db`.
Writes use Hermes comments only, producing JSON in `task_comments`.

**Testing**: `pytest` under `swarm/tests/`; targeted command smoke checks for
`swarm/bin/board-groom-emit` and `swarm/bin/board-drift`.

**Target Platform**: Linux developer/operator workstation running Chitin swarm
cron jobs.

**Project Type**: Repository-local CLI/tooling.

**Performance Goals**: Drift report completes in under 10 seconds for the
current Hermes board corpus; queries should operate over indexed columns and
avoid long-lived write locks.

**Constraints**:

- Do not mutate kanban SQLite tables directly.
- Do not change existing task statuses as part of analysis.
- Structured comments must remain valid single-object JSON.
- Policy must respect `scripts/check-swarm-kanban-isolation.sh`.

**Scale/Scope**: All Hermes boards under `~/.hermes/kanban/boards/`, with
primary acceptance on the `chitin` board.

## Constitution Check

GATE: Passes with constraints.

- Kanban mutation isolation: all writes go through `hermes kanban comment`;
  drift analysis opens SQLite read-only.
- Governance authority boundary: this feature observes board health and emits
  comments; it does not bypass kernel/governance policy.
- Spec-kit traceability: implementation and tests must include
  `spec: 066-grooming-telemetry` references where applicable.
- E2E-default coverage: add realistic SQLite fixtures that include tasks,
  events, comments, archives, unblocks, reblocks, and reassignments.

## Project Structure

### Documentation (this feature)

```text
.specify/specs/066-grooming-telemetry/
├── spec.md
├── plan.md
└── tasks.md
```

### Source Code

```text
swarm/bin/
├── board-groom-emit      # new executable structured comment emitter
└── board-drift           # new executable drift report CLI

swarm/tests/
├── test_board_groom_emit.py
└── test_board_drift.py
```

**Structure Decision**: Keep the feature in `swarm/bin` because the repository
does not currently contain a `hermes_cli/` package. If Hermes later exposes a
plugin command surface, `hermes kanban drift` can delegate to `swarm/bin/board-drift`
without changing the report schema.

## Data Model

### GroomingDecisionRecord

Single JSON object written as a kanban comment:

```json
{
  "schema": "chitin.grooming_decision.v1",
  "grooming_decision": true,
  "action": "archive",
  "rationale": "stale auto-decomposed debris",
  "confidence": "high",
  "stage": 8,
  "pipeline_position": "close",
  "session_id": "optional-session-id",
  "actor": "ares"
}
```

Required fields are the six fields in AC1 plus `schema`; optional metadata must
not break consumers that only read `.grooming_decision`.

### DriftReport

Markdown report with six stable sections:

1. Time-in-status histograms
2. Bounce detection
3. Assignment stability
4. Stall detection
5. Grooming accuracy
6. Assignee default frequency

Each section includes board and ticket identifiers so follow-up tickets can
cite exact evidence.

## Implementation Notes

- Parse `task_events.payload` defensively; malformed payloads should be counted
  in warnings, not crash the report.
- Treat status changes from both explicit `status_transition` events and legacy
  event kinds where present.
- Stall detection should use `ready_since` from events when available and fall
  back to task `created_at` or latest transition timestamp.
- Per-session accuracy should group by `session_id` from the structured comment
  when present, otherwise by `actor` and day.
- `board-groom-emit --dry-run` should print the exact JSON it would submit.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|--------------------------------------|
| Standalone `board-drift` instead of native `hermes kanban drift` | The repo has no `hermes_cli/` package to patch in-tree | Direct Hermes CLI modification would require changing code outside this repository from this ticket |
