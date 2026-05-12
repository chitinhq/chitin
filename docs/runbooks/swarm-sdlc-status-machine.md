# Swarm SDLC status machine

The chitin swarm runs a real SDLC on the Hermes kanban board. Every
ticket walks a deterministic state machine; every transition emits a
comment AND a `task_events` row. The kanban is the single source of
truth — if a state change isn't visible there, it didn't happen.

## States

| Status        | Meaning                                                  |
|---------------|----------------------------------------------------------|
| `triage`      | Raw idea or auto-filed ticket. Body is rough.            |
| `ready`       | Groomed; clear acceptance criteria; awaiting dispatch.   |
| `in_progress` | A worker has claimed the ticket and started.             |
| `code_review` | PR is open. Awaiting review verdict.                     |
| `blocked`     | Cannot progress (external dep, decision needed).         |
| `done`        | Merged + result recorded.                                |
| `archived`    | Hidden from default views (cancelled, superseded).       |

## Transitions

```
                                 (grooming)
              ┌──────────────────────────────────────────┐
              ▼                                          │
        ┌──────────┐  hermes/operator   ┌────────┐       │
   ────▶│  triage  │ ─────────────────▶ │ ready  │       │
        └──────────┘                    └────┬───┘       │
              ▲                              │           │
              │ clawta demote                │ clawta    │
              │ ("not actually ready")       │ dispatch  │
              │                              ▼           │
              │                       ┌────────────┐     │
              │                       │ in_progress│◀────┘
              │                       └─────┬──────┘
              │                             │ worker opens PR
              │                             ▼
              │                       ┌────────────┐
              │                       │ code_review│
              │                       └─────┬──────┘
              │                             │ reviewer
              │     ┌───────────────────────┴────────┐
              │     │ changes                approved │
              │     ▼                                ▼
              │  in_progress                       done
              │
              └──── (any → blocked → ready)
```

## Who can fire which transition

| Transition                    | Owner       | Mechanism                                  |
|-------------------------------|-------------|--------------------------------------------|
| `triage → ready`              | Hermes / operator | `kanban-flow ready <id>` or hermes grooming reply |
| `ready → in_progress`         | Clawta poller     | dispatch path; `kanban-flow start <id>` from lobster |
| `ready → triage` (demote)     | Clawta poller     | when sequence-check flags "not actually ready" |
| `in_progress → code_review`   | Worker            | `kanban-flow pr <id> <url>` on PR open     |
| `code_review → in_progress`   | Reviewer / clawta | `kanban-flow review <id> changes`          |
| `code_review → done`          | Reviewer / merge  | `kanban-flow review <id> approved` or merge bot |
| `* → blocked`                 | Worker / clawta   | `kanban-flow block <id> <reason>`          |
| `blocked → ready`             | Operator / hermes | `kanban-flow unblock <id>`                 |

Non-canonical: `kanban-flow done <id> --result "<txt>"` is the catch-all
that bypasses code review (use only for tickets that don't produce a PR,
e.g. user-local config tweaks, decisions, research-with-no-PR).

## Audit invariant

For every status change, two records exist:

1. A row in `task_comments` (human-readable, surfaced by `hermes kanban show`)
2. A row in `task_events` with `kind='status_transition'` and payload
   `{"from":"<status>","to":"<status>","by":"<author>"}`

The `kanban-flow` helper enforces both. Direct SQL `UPDATE tasks SET
status=…` without the matching comment + event is a bug — fix it by
backfilling, not by ignoring.

## Tooling

- `scripts/kanban-flow` — lifecycle helper, source of truth for transitions
- `hermes kanban` — display, comments, assign, complete (legacy paths)
- `swarm-elo` — post-merge judge ratings (separate; doesn't drive lifecycle)

## When the state machine drifts

Symptoms:

- Ticket sits in `ready` with assignee set, no `task_runs` row → poller
  isn't picking it up (see Slice C — clawta autonomous poller).
- Ticket in `in_progress` but no recent comments and no worker process →
  worker crashed silently; either `kanban-flow block` it or reset to
  `ready` after investigation.
- Ticket in `code_review` after PR was merged → merge bot didn't fire;
  promote to `done` manually with `kanban-flow review <id> approved`.

Recovery rule: never mutate `status` via raw SQL without also writing
the matching event + comment. The CLI is the cheap path; backfilling
audit later is the expensive path.
