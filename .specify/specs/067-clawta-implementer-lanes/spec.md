# 067 — CLAWTA_IMPLEMENTER_LANES + Stage 5 Detection

> Parent: spec 049 (role architecture), spec 054 (assembly line).
> Resolves: the poller routes ALL `assignee=clawta` tickets to
> terminal workers, even when Clawta should personally execute
> Stage 5 implementation work that arrived through the assembly
> line. This spec defines the two-path query that distinguishes
> routing tickets from implementer tickets.

## Summary

The clawta-poller's `fetch_routable()` function treats every
`assignee=clawta` ticket as a routing candidate — it sends the
ticket through `_pick_driver`, which reassigns it to a terminal
worker (codex, copilot, claude-code, gemini). This is correct for
tickets that landed on Clawta's lane because the poller or
operator assigned them for triage/routing. But it is WRONG for
tickets that carry a Stage 5 handoff packet — those came through
the Octi assembly line (spec 054) and Clawta is the designated
implementer per its capability profile (spec 049 §R6). Routing
them away subverts the assembly line's role assignment.

This spec introduces a `CLAWTA_IMPLEMENTER_LANES` constant and
a `stage5_handoff` detection mechanism so that `fetch_routable()`
splits into two paths:

- **Path A (implementer)**: `assignee=clawta` + Stage 5 handoff
  present → returned directly dispatchable to Clawta (treated as
  a terminal lane for this ticket only).
- **Path B (routing)**: `assignee=clawta` + no Stage 5 handoff →
  current behavior: route through `_pick_driver` to a terminal
  worker.

No existing ticket's behavior changes. The new code path
activates ONLY when a Stage 5 handoff marker exists on the
ticket.

## Ticket refs

- Parent specs: 049 (role architecture), 054 (assembly line).
- Governs: `swarm/bin/clawta-poller` lines 94-104 (ROUTING_LANES),
  381-404 (`fetch_routable`), 1000-1021 (`route_clawta_assigned`).
- Related: `swarm/bin/clawta-poller` lines 1145-1170
  (`dispatch_ready_batch`), 1173-1254 (`tick`).
- Constitution §1.1: spec-first SDD gate applies.

## File-system scope

Worker MAY write under:

- `.specify/specs/067-clawta-implementer-lanes/**`
- `.specify/specs/INDEX.md` (add the 067 row)
- `swarm/bin/clawta-poller` — modify `fetch_routable()`,
  `route_clawta_assigned()`, `tick()`, add new constants + helpers
- `swarm/tests/test_clawta_poller.py` — add tests for the two-path
  split, Stage 5 detection, backward compatibility

Worker MUST NOT write under:

- `swarm/octi/` — the Octi runtime implements the handoff packet
  creation side; this spec defines the CONSUMPTION side only
- `go/` — chitin kernel unchanged
- `chitin.yaml` — governance policy unchanged
- Agent runtime code, hermes scripts, openclaw workflows

## Goal

After implementation, the poller will correctly distinguish
between two categories of `assignee=clawta` tickets:

1. Tickets that need routing (the poller should reassign them to
   a terminal worker via `_pick_driver`).
2. Tickets that need execution (Clawta should personally execute
   them as the implementer role, per the assembly line's Stage 5
   assignment).

The detection mechanism must be unambiguous, queryable from
SQLite, backward-compatible with all existing tickets, and
documented sufficiently that an implementer can code against it
without ambiguity.

## Requirements

### R1 — Lane distinction: routing vs. implementer

A **routing lane** ticket is one whose `assignee=clawta` because
the ticket was placed there for triage — the poller should
resolve it to a terminal worker. A **implementer lane** ticket is
one whose `assignee=clawta` because the Octi assembly line (spec
054, Stage 4 → Stage 5 transition) produced a handoff packet
naming Clawta as the implementer for Stage 5.

The distinction is NOT based on the assignee alone. Both
categories have `assignee=clawta`. The distinction is based on the
presence or absence of a Stage 5 handoff marker on the ticket.

**Conditions:**

| Category | assignee | stage5_handoff marker | Poller behavior |
|---|---|---|---|
| routing lane | clawta (or chitin-worker) | absent | Route through `_pick_driver` → terminal worker |
| implementer lane | clawta | present | Dispatch directly to Clawta (treat as terminal lane for this ticket) |

`chitin-worker` is always a routing lane — it never has a Stage 5
handoff. Only `clawta` can be an implementer lane per spec 049
§R6 (Clawta is the canonical implementer).

### R2 — Stage 5 handoff packet detection

The poller must detect whether a ticket carries a Stage 5
handoff packet. The chosen mechanism:

**Structured comment with a recognized prefix.** A ticket has a
Stage 5 handoff marker when its most recent comment authored by
`octi-handoff` (or any author ending in `-handoff`) contains a
line matching:

```
Stage-5-handoff: clawta
```

The prefix `Stage-5-handoff:` is case-sensitive. The value after
the colon is the agent ID of the designated implementer.

**Why a structured comment (not a ticket field, label, or
column):**

- (a) A dedicated column or metadata field (`stage5_handoff`)
  would require a schema migration on the kanban DB, breaking
  backward compatibility with all existing boards and requiring
  operator intervention. A comment is append-only and requires
  no migration.
- (b) A label/tag system does not exist in the current kanban
  schema and would require adding one. A comment reuses existing
  infrastructure.
- (c) The comment approach is queryable via a single SQL JOIN
  against `task_comments`, is backward-compatible (tickets without
  such a comment simply have no match), and is auditable (the
  comment body carries the full handoff context, not just a flag).

**Detection SQL:**

```sql
SELECT t.id, t.title, t.body, t.assignee, t.priority, t.created_at
  FROM tasks t
  WHERE t.status IN ('ready', 'todo')
    AND t.assignee = 'clawta'
    AND EXISTS (
      SELECT 1
        FROM task_comments c
       WHERE c.task_id = t.id
         AND c.author LIKE '%-handoff'
         AND c.body LIKE '%Stage-5-handoff: clawta%'
       ORDER BY c.created_at DESC, c.id DESC
       LIMIT 1
    )
  ORDER BY t.priority DESC, t.created_at ASC
```

**Detection helper function:**

```python
STAGE5_HANDOFF_RE = re.compile(
    r"^Stage-5-handoff:\s*clawta\s*$",
    re.MULTILINE,
)

def fetch_stage5_handoff_comments(task_id: str) -> list[str]:
    """Return comment bodies from handoff authors on the task."""
    with sqlite3.connect(str(DB_PATH)) as conn:
        conn.row_factory = sqlite3.Row
        rows = conn.execute(
            """
            SELECT body
              FROM task_comments
             WHERE task_id = ?
               AND author LIKE '%-handoff'
             ORDER BY created_at DESC, id DESC
            """,
            (task_id,),
        ).fetchall()
    return [str(row["body"]) for row in rows]


def ticket_has_stage5_handoff(ticket_id: str) -> bool:
    """True when the ticket carries a Stage 5 handoff marker."""
    for body in fetch_stage5_handoff_comments(ticket_id):
        if STAGE5_HANDOFF_RE.search(body):
            return True
    return False
```

**Ambiguity resolution:** If multiple Stage-5-handoff comments
exist with different implementer values, the most recent one
wins. If the most recent names an implementer other than `clawta`,
it is NOT a Clawta implementer-lane ticket (it may be a terminal
worker's implementer lane, which is out of scope for this spec).

### R3 — `fetch_routable()` query split

The current `fetch_routable()` returns ALL tickets where
`assignee IN ROUTING_LANES`. It must split into two functions:

```python
CLAWTA_IMPLEMENTER_LANES = ("clawta",)  # subset of ROUTING_LANES

def fetch_routable_for_routing() -> list[dict[str, Any]]:
    """Return ready/todo tickets assigned to a routing lane that
    do NOT carry a Stage 5 handoff for that lane.

    These tickets need their assignee resolved to a terminal
    lane via the LLM router (/_pick_driver) before they can be
    dispatched. The poller routes them and reassigns to the
    chosen terminal lane.
    """
    if not ROUTING_LANES:
        return []
    status_placeholders = ",".join("?" * len(DISPATCHABLE_STATUSES))
    routing_placeholders = ",".join("?" * len(ROUTING_LANES))
    with sqlite3.connect(str(DB_PATH)) as conn:
        conn.row_factory = sqlite3.Row
        rows = conn.execute(
            f"""
            SELECT id, title, body, assignee, priority, created_at
              FROM tasks
             WHERE status IN ({status_placeholders})
               AND assignee IN ({routing_placeholders})
               AND NOT EXISTS (
                 SELECT 1
                   FROM task_comments c
                  WHERE c.task_id = tasks.id
                    AND c.author LIKE '%-handoff'
                    AND c.body LIKE '%Stage-5-handoff: clawta%'
                    AND tasks.assignee = 'clawta'
               )
             ORDER BY priority DESC, created_at ASC
            """,
            (*DISPATCHABLE_STATUSES, *ROUTING_LANES),
        ).fetchall()
    return [dict(r) for r in rows]


def fetch_routable_for_implementer() -> list[dict[str, Any]]:
    """Return ready/todo tickets assigned to Clawta that carry a
    Stage 5 handoff marker. These tickets should be dispatched
    directly to Clawta as the implementer — the poller treats
    Clawta as a terminal lane for these tickets only.
    """
    if "clawta" not in ROUTING_LANES:
        return []
    status_placeholders = ",".join("?" * len(DISPATCHABLE_STATUSES))
    with sqlite3.connect(str(DB_PATH)) as conn:
        conn.row_factory = sqlite3.Row
        rows = conn.execute(
            f"""
            SELECT id, title, body, assignee, priority, created_at
              FROM tasks
             WHERE status IN ({status_placeholders})
               AND assignee = 'clawta'
               AND EXISTS (
                 SELECT 1
                   FROM task_comments c
                  WHERE c.task_id = tasks.id
                    AND c.author LIKE '%-handoff'
                    AND c.body LIKE '%Stage-5-handoff: clawta%'
               )
             ORDER BY priority DESC, created_at ASC
            """,
            (*DISPATCHABLE_STATUSES,),
        ).fetchall()
    return [dict(r) for r in rows]
```

The existing `fetch_routable()` becomes a composition for callers
that need the union (e.g., `fetch_poller_owned_ready_or_todo_with_bodies`):

```python
def fetch_routable() -> list[dict[str, Any]]:
    """Return all routable tickets (routing + implementer)."""
    return fetch_routable_for_routing() + fetch_routable_for_implementer()
```

**`ROUTING_LANES` and `CLAWTA_IMPLEMENTER_LANES` interaction:**

- `ROUTING_LANES` = `("clawta", "chitin-worker")` — the full set
  of agents whose tickets the poller may need to resolve. Unchanged.
- `CLAWTA_IMPLEMENTER_LANES` = `("clawta",)` — the subset of
  `ROUTING_LANES` that can serve as implementer lanes when carrying
  a Stage 5 handoff. Only `clawta` qualifies per spec 049 §R6.
- An `assignee=clawta` ticket WITHOUT a Stage 5 handoff is part of
  `ROUTING_LANES` but NOT `CLAWTA_IMPLEMENTER_LANES` for that
  ticket — it goes through the routing path.
- An `assignee=chitin-worker` ticket is ALWAYS routing, never
  implementer — `chitin-worker` is not in
  `CLAWTA_IMPLEMENTER_LANES`.

**New environment variable:**

```
CLAWTA_IMPLEMENTER_LANES   comma list, default: clawta
```

This parallels `TERMINAL_LANES` and `ROUTING_LANES`. It defaults
to `"clawta"` and MUST be a subset of `ROUTING_LANES`.

### R4 — tick() step-5 modification

The current `tick()` Step 5 (lines 1227-1235) routes
`clawta-assigned` tickets and then dispatches newly-routed
terminal-lane tickets. It must be modified to also dispatch
implementer-lane tickets directly to Clawta:

```python
# Step 5: If dispatch capacity remains, route routing-lane
# tickets to fill that capacity, then immediately dispatch.
# Then dispatch any implementer-lane tickets directly to Clawta.
remaining = max_dispatch - len(dispatched)
routed: list[str] = []
if remaining > 0:
    routed = route_clawta_assigned(dry_run, limit=remaining,
                                  routing_only=True)
    more_dispatched, more_demoted, queue_size_after_route = \
        dispatch_ready_batch(remaining, dry_run)
    dispatched.extend(more_dispatched)
    demoted.extend(more_demoted)
    queue_size = max(queue_size, queue_size_after_route)

# Step 5b: Dispatch implementer-lane tickets directly to Clawta.
implementer_tickets = fetch_routable_for_implementer()
implementer_tickets, skipped_impl = \
    filter_tickets_with_incomplete_runs(implementer_tickets)
for tid in skipped_impl:
    log(f"tick: skipping {tid} — incomplete task_run on "
        "implementer-lane ticket")
for t in implementer_tickets[:max_dispatch - len(dispatched)]:
    reason = ("Stage 5 implementer-lane ticket; dispatching "
              "directly to Clawta per spec 067 R4")
    if dispatch_ticket(t["id"], "clawta", reason, dry_run):
        dispatched.append(t["id"])
        log(f"tick: dispatched implementer-lane {t['id']} -> clawta")
```

**`route_clawta_assigned()` modification:** Accept a
`routing_only=True` keyword that restricts routing to tickets
from `fetch_routable_for_routing()` instead of the full
`fetch_routable()`. This prevents the routing step from
processing implementer-lane tickets (which would incorrectly
reassign them to a terminal worker).

```python
def route_clawta_assigned(dry_run: bool, limit: int | None = None,
                          routing_only: bool = False) -> list[str]:
    if routing_only:
        routable = fetch_routable_for_routing()
    else:
        routable = fetch_routable()
    # ... rest unchanged
```

### R5 — Backward compatibility

**Existing `assignee=clawta` tickets without Stage 5 handoff
comments are entirely unaffected.** The new SQL in
`fetch_routable_for_routing()` includes a `NOT EXISTS` subquery
that filters out implementer-lane tickets. A ticket with no
handoff comment at all will NOT match the `EXISTS` clause in
`fetch_routable_for_implementer()`, so it stays in the routing
path — exactly the current behavior.

**No schema migration required.** The detection uses existing
`task_comments` rows. If the Octi runtime is not yet emitting
handoff comments, everything behaves exactly as before.

**No new columns, no new tables, no env var required to adopt.**
The `CLAWTA_IMPLEMENTER_LANES` env var has a default of
`"clawta"`, matching the expected single-implementer scenario.

**Ticket state machine unchanged.** Implementer-lane tickets
follow the same `ready → in_progress → done` flow. The only
difference is which dispatch path moves them to `in_progress`.

**Monitoring gap:** Until the Octi runtime (spec 054 Stage 4 → 5
transition) actually emits handoff comments, the
implementer-lane path exercises zero tickets. This is correct —
the poller must not dispatch to Clawta as implementer until the
assembly line explicitly signals that intent. The
`octi.handoff.created` telemetry event (spec 054 §R5) records
when handoffs occur; operators can audit with:

```bash
jq -r 'select(.event_type == "octi.handoff.created"
  and .payload.next_role == "implementer")' \
  ~/.chitin/octi-events-*.jsonl
```

### R6 — Telemetry

Two new log-level telemetry signals from the poller (not Octi
events — these are poller-internal dispatch signals):

| Signal | When | Fields |
|---|---|---|
| `clawta-poller implementer-dispatch` | Poller dispatches a Stage 5 ticket directly to Clawta | `ticket_id, stage5_handoff_author` |
| `clawta-poller routing-skipped-implementer` | Poller skips routing a clawta-assigned ticket because it carries a Stage 5 handoff | `ticket_id, stage5_handoff_author` |

These signals log at the poller's standard `log()` function. They
do NOT emit OctiEvents — the assembly line's own
`octi.handoff.created` event is the authoritative handoff record.
The poller signals are for operator debugging of the two-path
split, not for replay.

### R7 — Edge cases

1. **Handoff comment names a non-clawta implementer.** The
   `EXISTS` subquery checks for `Stage-5-handoff: clawta`
   specifically. A `Stage-5-handoff: codex` comment does NOT
   trigger the implementer-lane path. That scenario (terminal
   workers receiving handoff packets) is out of scope and would
   require a separate spec amendment.

2. **Multiple handoff comments with conflicting implementers.**
   The SQL `EXISTS` subquery matches any qualifying comment, so
   the presence of at least one `Stage-5-handoff: clawta` comment
   triggers the implementer path. If a later comment changes the
   implementer to someone else but an earlier `clawta` comment
   remains, both paths could match. **Mitigation:** the poller
   checks implementer-lane first (`fetch_routable_for_implementer`)
   and removes matched tickets from the routing set. If a ticket
   appears in both sets due to conflicting comments, the
   implementer path wins (Clawta executes it).

3. **Handoff comment on a non-clawta routing-lane ticket.** The
   `EXISTS` subquery in `fetch_routable_for_implementer()` checks
   `assignee = 'clawta'`, so a `chitin-worker` ticket with a
   stray handoff comment is NOT affected — it stays in the
   routing path.

4. **Handoff comment with extra whitespace or formatting.** The
   regex `^Stage-5-handoff:\s*clawta\s*$` is strict: the line
   must start with `Stage-5-handoff:`, followed by optional
   whitespace, `clawta`, and optional trailing whitespace, then
   end of line. Markdown bold/fences around the marker are NOT
   recognized. The handoff-creating code (Octi runtime) must emit
   the marker as a plain-text line, not inside a code block or
   bold span.

5. **Ticket reassigned away from clawta after handoff.** If an
   operator or agent reassigns an implementer-lane ticket from
   `clawta` to another agent, the implementer-lane SQL will not
   match (it checks `assignee = 'clawta'`). If reassigned to a
   terminal-lane worker, the terminal-lane dispatch handles it.
   If reassigned to `chitin-worker`, it enters the routing path.
   The handoff comment is preserved for audit but no longer
   affects dispatch.

## Acceptance criteria

1. `fetch_routable_for_routing()` returns `assignee=clawta`
   tickets WITHOUT a Stage 5 handoff comment, plus all
   `assignee=chitin-worker` tickets. Verified by unit test with
   a fixture DB containing both categories.
2. `fetch_routable_for_implementer()` returns `assignee=clawta`
   tickets WITH a Stage 5 handoff comment. Verified by unit test
   with a fixture DB.
3. Existing `assignee=clawta` tickets without handoff comments
   behave identically to the pre-spec poller — `fetch_routable()`
   returns them in the routing path, and the test
   `test_fetch_routable_includes_chitin_worker_lane` continues to
   pass unchanged.
4. `CLAWTA_IMPLEMENTER_LANES` is configurable via the
   `CLAWTA_IMPLEMENTER_LANES` env var, defaults to `("clawta",)`,
   and MUST be a subset of `ROUTING_LANES`. A poller start with
   `CLAWTA_IMPLEMENTER_LANES=clawta,codex` where `codex` is not
   in `ROUTING_LANES` logs a warning and uses only the
   intersection.
5. The `tick()` step-5 dispatches implementer-lane tickets
   directly to Clawta after exhausting routing-lane capacity.
   Unit test with a fixture DB containing both ticket types
   verifies Clawta receives the implementer-lane dispatch.
6. The `route_clawta_assigned(routing_only=True)` call in `tick()`
   does NOT attempt to route implementer-lane tickets through
   `_pick_driver`. Unit test verifies no `_pick_driver` subprocess
   call for implementer-lane tickets.
7. Edge case R7.2 (conflicting handoff comments): implementer
   path wins. Unit test with two handoff comments (one clawta,
   one codex) verifies the ticket dispatches to Clawta.
8. Edge case R7.3 (handoff on chitin-worker ticket): no effect.
   Unit test verifies `chitin-worker` ticket with a handoff
   comment still enters the routing path.
9. The spec is reviewable as a standalone document that an
   implementer can code against without ambiguity — no external
   references required beyond specs 049 and 054 for role context.

## Test coverage

- `swarm/tests/test_clawta_poller.py`:
  - `test_fetch_routable_for_routing_excludes_stage5_handoff` —
    fixture DB with clawta ticket + handoff comment, assert NOT
    in routing result
  - `test_fetch_routable_for_implementer_includes_stage5_handoff` —
    fixture DB with clawta ticket + handoff comment, assert IS in
    implementer result
  - `test_fetch_routable_backward_compat_no_handoff` — existing
    clawta ticket without handoff stays in routing path
  - `test_clawta_implementer_lanes_env_var` —
    `CLAWTA_IMPLEMENTER_LANES=clawta,codex` with non-subset entry
    logs warning, uses intersection
  - `test_tick_step5b_dispatches_implementer_lane` — tick() with
    both ticket types, assert Clawta dispatch
  - `test_route_clawta_assigned_routing_only_skips_implementer` —
    verify no `_pick_driver` call for implementer tickets
  - `test_conflicting_handoff_comments_implementer_wins` — R7.2
  - `test_handoff_on_chitin_worker_ignored` — R7.3

All test files carry `# spec: 067-clawta-implementer-lanes`.

## Invariants

- **I1**: An `assignee=clawta` ticket with a Stage 5 handoff
  comment is NEVER routed through `_pick_driver`. It is always
  dispatched directly to Clawta.
- **I2**: An `assignee=clawta` ticket WITHOUT a Stage 5 handoff
  comment is ALWAYS routed through `_pick_driver`. This is the
  pre-spec behavior and must not change.
- **I3**: `CLAWTA_IMPLEMENTER_LANES` is always a subset of
  `ROUTING_LANES`. Membership outside the intersection is logged
  as a warning and silently dropped.
- **I4**: The Stage 5 handoff marker line is `Stage-5-handoff:
  clawta` — case-sensitive, no bold/fence wrapping, plain text
  in a comment body.
- **I5**: The detection mechanism requires no schema migration,
  no new columns, and no new tables. It uses existing
  `task_comments` infrastructure.
- **I6**: No ticket's dispatch behavior changes unless a Stage 5
  handoff comment exists on that ticket.

## Out of scope

- The Octi runtime's Stage 4 → 5 handoff emission (that belongs
  in a separate implementation spec for the Octi process layer).
- Terminal workers receiving handoff packets (e.g.,
  `Stage-5-handoff: codex`) — this requires a separate spec to
  define what "terminal worker as implementer" means.
- Changing the role architecture (spec 049) or the assembly line
  stages (spec 054) — this spec is a CONSUMER of those, not a
  modifier.
- Automatic promotion of implementer-lane tickets into
  `in_progress` without going through `dispatch_ticket()` — the
  standard dispatch flow must be respected for audit consistency.

## References

- Parent: spec 049 (role architecture, §R6 implementer role)
- Parent: spec 054 (assembly line, §R1 Stage 5 implementation)
- Current poller code: `swarm/bin/clawta-poller` lines 94-104
  (ROUTING_LANES), 381-404 (fetch_routable), 1000-1021
  (route_clawta_assigned), 1173-1254 (tick)
- Test file: `swarm/tests/test_clawta_poller.py`