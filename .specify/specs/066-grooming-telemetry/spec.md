# 066 — Grooming telemetry: structured decision records + drift analysis

> Operator request 2026-05-20 after observing that grooming decisions lack
> structured rationale and there is no way to mine kanban event data for
> drift, stall, or accuracy patterns.

## Ticket refs

- chitin `t_70a085ab` — grooming telemetry ticket

## File-system scope

- `swarm/bin/board-drift` (new) — drift analysis CLI
- `swarm/bin/board-groom-emit` (new) — structured decision emitter
- `hermes_cli/kanban_drift.py` (new) — drift analysis command for hermes CLI
- `.specify/specs/066-grooming-telemetry/spec.md` (this file)

## Goal

Make grooming observable. Every grooming action emits a structured JSON
comment on the affected ticket, and a `hermes kanban drift` command mines
the event databases for six drift dimensions so the operator can identify
where Ares needs improvement, skills, or behavioral changes.

## Acceptance criteria

AC1. **Structured decision records.** Every grooming action by Ares (or
board-groom cron) emits a comment on the affected ticket containing a
parseable JSON object with these fields:

| Field              | Type   | Example                              |
|--------------------|--------|--------------------------------------|
| `grooming_decision` | bool  | `true`                               |
| `action`           | string | `"archive"`                          |
| `rationale`        | string | `"stale auto-decomposed debris"`     |
| `confidence`       | string | `"high"` or `"low"`                  |
| `stage`            | int    | `8`                                  |
| `pipeline_position`| string | `"close"` or `"research"`            |

AC2. **Drift analysis tool.** `hermes kanban drift` (or `board-drift`)
produces a report covering these six dimensions:

1.  Time-in-status histograms (created→ready, ready→claimed, claimed→done)
2.  Bounce detection (tickets that cycle through statuses)
3.  Assignment stability (reassignment frequency per assignee)
4.  Stall detection (tickets in `ready` with no `claimed` event for >2 h)
5.  Grooming accuracy (unblock-then-reblock rate, archive-then-restore rate)
6.  `assignee=default` frequency (tickets that needed correction)

AC3. **Per-session grooming accuracy.** The drift report breaks down
accuracy by session: what percentage of unblocks stuck, what percentage
of archives stayed archived.

AC4. **Spec-kit entry.** This file exists at
`.specify/specs/066-grooming-telemetry/spec.md` and is registered in
`INDEX.md`.

AC5. **Governance gates pass.** The PR introducing this spec passes
chitin governance gates: `bounds` ceiling not exceeded, policy signature
valid.

AC6. **Comment parseability.** Structured grooming comments are valid
JSON parseable by `jq .grooming_decision` without error. Schema is
versioned; adding fields must not break existing consumers.

## Observability

The structured comments are the primary telemetry surface. Downstream
consumers (cron jobs, dashboards, the drift tool) query `task_comments`
where `body LIKE '%grooming_decision%'` and parse the JSON.

The drift tool reads `task_events` and `task_comments` from all board
databases under `~/.hermes/kanban/boards/*/kanban.db` and produces a
single Markdown report.

## Dependencies

- **Spec 054** (assembly line) — grooming decisions are stage-8 → stage-0
  flywheel telemetry. This spec is the observability layer for that
  flywheel.
- **Spec 022** (dispatch readiness contract) — drift dimension 6
  (`assignee=default` frequency) overlaps with dispatch gate analysis.

## Open questions

O1. Should the drift tool live in `hermes_cli` as `hermes kanban drift`
   or as a standalone script in `swarm/bin/board-drift`? Proposed:
   both — `hermes kanban drift` is the primary interface,
   `swarm/bin/board-drift` is the cron-callable wrapper.

O2. Should grooming decision comments be compact (single-line JSON) or
   human-readable (Markdown with embedded JSON block)? Proposed:
   single-line JSON — downstream consumers parse it; human readers
   already have the freeform comments.

O3. Should the drift report include recommendations (e.g. "3 tickets
   bounced ready→triage→ready — consider raising the specify quality
   gate") or just raw data? Proposed: raw data first; recommendations
   are stage-0 research that the existing grooming loop can surface.