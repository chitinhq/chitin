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
| `in_progress` | A worker has claimed the ticket and is working OR a PR is open and awaiting merge. |
| `blocked`     | Cannot progress (external dep, decision needed).         |
| `done`        | Merged + result recorded.                                |
| `archived`    | Hidden from default views (cancelled, superseded).       |

### Why no `code_review` status

Hermes' kanban UI renders only triage/todo/ready/in_progress/blocked/done
columns (hardcoded in hermes-agent; the kanban config block exposes only
`dispatch_in_gateway`, `dispatch_interval_seconds`, and `failure_limit`).
A ticket flipped to `code_review` would vanish from the operator's
board until merged, breaking kanban-as-source-of-truth.

Tickets stay in `in_progress` from the moment a worker claims them
through PR open, review, and merge. The PR's GitHub state is the
review-phase truth. `kanban-flow pr <id> <url>` records the URL as a
comment + `pr_opened` task event without moving the ticket.

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
              │                             │ (stays in_progress;
              │                             │  pr_opened event +
              │                             │  PR-url comment)
              │                             │
              │                             ▼
              │                       PR merged on GitHub
              │                             │
              │                             ▼
              │                          ┌─────┐
              │                          │ done│
              │                          └─────┘
              │
              └──── (any → blocked → ready)
```

## Who can fire which transition

| Transition                    | Owner       | Mechanism                                  |
|-------------------------------|-------------|--------------------------------------------|
| `triage → ready`              | Hermes / operator | `kanban-flow ready <id>` or hermes grooming reply. **Defaults `assignee=clawta` if no terminal lane is already set and no explicit `--assignee NAME` override is passed.** Terminal lanes: codex, copilot, claude-code, gemini, clawta. |
| `ready → in_progress`         | Clawta poller     | dispatch path; `kanban-flow start <id>` from lobster |
| `ready → triage` (demote)     | Clawta poller     | when sequence-check flags "not actually ready" |
| `in_progress → in_progress` (PR open) | Worker            | `kanban-flow pr <id> <url>` — no status flip, audit only |
| `in_progress → done`          | Operator / merge bot | `kanban-flow done <id> --result "<txt>"` after PR merge |
| `* → blocked`                 | Worker / clawta   | `kanban-flow block <id> <reason>`          |
| `blocked → ready`             | Operator / hermes | `kanban-flow unblock <id>`. **Defaults `assignee=clawta` if no terminal lane is already set and no explicit `--assignee NAME` override is passed.** Terminal lanes: codex, copilot, claude-code, gemini, clawta. |

Recovery: `kanban-flow done` is the universal completion verb — accepts
any `from` status. Use it after PR merge, or for tickets that don't
produce a PR (research notes, decisions, config tweaks).

## Audit invariant

For every status change, two records exist:

1. A row in `task_comments` (human-readable, surfaced by `hermes kanban show`)
2. A row in `task_events` with `kind='status_transition'` and payload
   `{"from":"<status>","to":"<status>","by":"<author>"}`

For PR open (which is not a status change), a single `task_events` row
with `kind='pr_opened'` and payload `{"pr_url":"<url>","by":"<author>"}`
plus the comment.

The `kanban-flow` helper enforces these. Direct SQL `UPDATE tasks SET
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
  worker crashed silently OR PR is open and awaiting review. Check
  GitHub PR state first; if no PR, either `kanban-flow block` it or
  reset to `ready` after investigation.
- Ticket in `in_progress` long after the PR merged → merge-completion
  step missed; promote to `done` manually with `kanban-flow done`.

Recovery rule: never mutate `status` via raw SQL without also writing
the matching event + comment. The CLI is the cheap path; backfilling
audit later is the expensive path.
