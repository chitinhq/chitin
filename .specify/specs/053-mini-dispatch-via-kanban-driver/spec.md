# Spec 053: Mini dispatch via kanban driver routing

**Status**: DRAFT 2026-05-19 — awaiting red sign-off (constitution §1
pair-write rule). Slot 053 free.

**Author lens (Sun Tzu)**: this is a routing decision, not new
machinery. The kanban already flows tickets to drivers; the question is
only whether `mini` is a destination on that map. Add the destination,
don't build a second road.

## Summary

Spec 050 made Mini dispatchable — `mini_open(specs=[...])` resolves
spec references and spawns a session. But the *only* caller today is a
human typing the MCP tool. There is no autonomous trigger: a ticket can
sit `ready` on the kanban forever and no Mini session is ever opened
for it.

This spec closes that gap by making `mini` a **driver** the existing
`clawta-poller` can route to. The poller already polls the kanban,
validates readiness, and routes each ticket to a driver via
`_pick_driver`. When it routes a ticket to the `mini` driver, instead
of spawning a `clawta dispatch` worker it resolves the ticket's bound
spec(s) and calls `mini_open(specs=[...])`.

This is deliberately the *small* option. It is not the Octi/Temporal
poller (specs 043/044) — that is a months-long rewrite that is 0%
implemented. Spec 053 reuses the proven poller and ships in days. When
Octi/Temporal eventually lands, the `mini` driver concept ports into
spec 043's `SpawnWorker` activity unchanged.

## Preconditions / dependencies

- **clawta-poller must be installed and running.** As of 2026-05-19 it
  is NOT cron-scheduled on the operator box — only the two
  mention-listeners are. Spec 053 assumes a live poller; restoring the
  poller cron is an operational prerequisite, tracked separately.
- Spec 050 slice 1 (`mini_open` spec dispatch) — shipped, PR #795.
- Spec 004 (driver allowlist) — `mini` must be added to the allowlist.
- Constitution §1 ticket↔spec binding — a ticket's `NNN` prefix binds
  it to `.specify/specs/NNN-<slug>/`. This is the dispatch input.

## Non-goals

- **Not the Octi/Temporal stack.** Specs 043/044 stand; 053 is the
  interim bridge, not their replacement.
- **No new poller.** A second cron polling the kanban would duplicate
  readiness/veto logic. `mini` is a driver, not a poller.
- **No change to non-Mini drivers.** Existing `clawta dispatch` routing
  is untouched.

## Requirements

### R1 — `mini` is a registered driver

`mini` is added to the spec 004 driver allowlist. The poller's driver
validation accepts it like any other driver.

### R2 — routing a ticket to the `mini` driver

When `clawta-poller` resolves a ticket's driver to `mini`, the dispatch
step MUST:

1. Resolve the ticket's bound spec(s) from its `NNN` prefix
   (constitution §1). A ticket may carry one spec or, by an explicit
   field, several.
2. Call `mini_open(specs=[...], invoked_by="clawta-poller", ticket=<id>)`
   — the spec-050 entrypoint, via the CLI (`swarm/bin/mini`) since the
   poller is co-located on the box.
3. Record the returned `goal_id` on the ticket so the lifecycle
   watcher (R4) can correlate.

The dispatch is fire-and-forget at this step — the poller does not
block on the session.

### R3 — how a ticket is routed to `mini`

A ticket reaches the `mini` driver by one of (design review picks the
set):

- **(a) explicit** — a ticket field/label (e.g. `driver: mini`) set
  during grooming. Deterministic, operator/Clawta-controlled.
- **(b) classified** — `_pick_driver` learns to route spec-implementation
  tickets to `mini`. Less predictable.

Proposed: **(a) for slice 1** — explicit `driver: mini` set at grooming
time. Predictable, and it is exactly how Clawta/Ares would groom these
specs onto the board. Classification (b) is a later enhancement.

### R4 — session lifecycle watcher closes the loop

`clawta-poller` dispatches fire-and-forget; Mini sessions are
long-lived and need follow-through. A lifecycle watcher MUST:

- observe the dispatched session's `status.json`,
- on `status=done`/`needs_review` → move the kanban ticket to the
  matching column (`review`/`done` per board config),
- on `status=failed` → move the ticket back to `triage` (or `blocked`)
  with the failure summary,
- on staleness (no `updated_at` change within a window) → nudge the
  session, escalate after N nudges.

This is the unbuilt "Octi controller loop" (spec 038 slice 2) in
minimal form. It MAY be the `mini watch` daemon from spec 050 slice 2,
extended to also write kanban state — design review decides whether to
fold it into 050 slice 2 or keep it here.

### R5 — installer

Per constitution §4, any poller-side change ships with its installer
update in the same PR (`install-clawta-poller.sh` or equivalent).

## Boundary cases

1. **Ticket has no resolvable spec** (no `NNN`-bound spec.md) → the
   poller's existing spec-kit gate already rejects it; `mini` driver is
   never reached. No new handling needed.
2. **`mini_open` raises** (missing spec, ambiguous ref) → dispatch
   fails; ticket stays `ready`; logged. Same shape as any driver
   spawn failure.
3. **Mini session dispatched but the box has no kitty/display** →
   `mini open` fails fast; treated as a dispatch failure (case 2).
4. **Two poller ticks dispatch the same ticket** → the `goal_id`
   recorded on the ticket (R2.3) is the idempotency key; a ticket that
   already has a live `goal_id` is skipped.
5. **clawta-poller not running** → nothing dispatches. This is the
   current state; it is a precondition failure, not a 053 bug.

## Open questions

- **Q1 — routing mechanism.** R3 (a) explicit vs (b) classified.
  Proposed: (a) for slice 1.
- **Q2 — lifecycle watcher home.** R4: fold into spec 050 slice 2's
  `mini watch`, or keep as a 053 component? Proposed: fold into 050
  slice 2 — it is already the per-session watcher; add kanban writes.
- **Q3 — multi-spec tickets.** Can one ticket dispatch a multi-spec
  Mini session (`mini_open(specs=["051","052"])`)? Needs a ticket
  field listing extra specs. Proposed: slice 1 is one-spec-per-ticket;
  multi-spec is a later enhancement.
- **Q4 — poller restoration.** clawta-poller is not currently cronned.
  Is restoring it part of this spec's slice 1, or a separate
  operational ticket? Proposed: separate — 053 assumes a live poller.

## Acceptance criteria

- **AC1** — `mini` is in the spec 004 driver allowlist; the poller
  accepts it as a valid driver.
- **AC2** — a `ready` ticket with `driver: mini` and a bound spec,
  on a poller tick, results in a `mini_open(specs=[...])` call with
  the ticket's spec and `ticket=<id>`.
- **AC3** — the dispatched session's `goal_id` is recorded on the
  ticket; a second poller tick does not re-dispatch it (boundary 4).
- **AC4** — when the dispatched session writes `status=done`, the
  lifecycle watcher moves the ticket to the board's review/done column.
- **AC5** — when the session writes `status=failed`, the ticket
  returns to `triage`/`blocked` with the failure summary attached.
- **AC6** — the poller-side change ships with its installer update
  (constitution §4).

## Slice plan

- **Slice 1** — R1, R2, R3(a), R5. AC1–AC3, AC6. Mini becomes a
  dispatch target; lifecycle is still manual.
- **Slice 2** — R4 lifecycle watcher. AC4, AC5. Closes the loop so a
  dispatched ticket auto-advances on the board.
