# Spec 062: Spec ↔ build attribution (L2/L3)

**Status**: DRAFT 2026-05-19 — awaiting red sign-off. Implements the
L2/L3 attribution contract of charter spec 060 (charter R3). Inherits
charter Q2 (build_id minting) as an open question below.

**Author lens (Knuth)**: this is one invariant, stated once and
enforced everywhere — "every event carries `(spec_id, build_id)`". The
spec is short because the idea is small; the discipline is that there
is *no* exception.

## Summary

Charter 060 R3 names this the **load-bearing invariant** of the whole
stack: every chitin-kernel chain event (L2) and every Sentinel
telemetry row (L3) MUST carry the `spec_id` and `build_id` it belongs
to. Without it there is no per-spec replay (063) and no per-spec
learning (064) — telemetry that can't be attributed to a spec and a
build is noise.

## Motivation

- **Replay needs a key.** Spec 063 reconstructs a build by selecting
  every event with a given `build_id`. If events aren't tagged, there
  is nothing to select.
- **Learning needs a grain.** Spec 064 mines invariants *per spec* —
  "spec 044-style dispatch tickets fail this way 30% of the time".
  That requires telemetry grouped by `spec_id`.
- **Today there is no `build_id` at all.** The kernel chain records
  events; Sentinel records execution events; neither knows which
  *build* — which dispatch of which spec — produced them. Attribution
  is the missing join.

## Definitions

- **`spec_id`** — the stable spec key from spec 061 (`UnifiedSpec.spec_id`).
- **`build`** — one execution attempt of one spec (or spec set): a Mini
  session, an Octi dispatch, a `/goal` run. A build has a lifecycle
  (started → ... → done/failed).
- **`build_id`** — a unique id minted per build, carried by every
  event that build produces.

## Requirements

### R1 — `build_id` is minted once per build, early

A `build_id` is minted at the moment a build begins and is immutable
for that build's lifetime. "Begins" = the dispatch decision (Octi
DispatchWorkflow, clawta-poller routing to a driver, or `mini_open`).
The minting point is charter Q2 — see Open questions.

### R2 — every chain event carries `(spec_id, build_id)`

The chitin-kernel chain event schema gains `spec_id` and `build_id`
fields. Every event the kernel appends during a build is tagged.
Events with no associated build (e.g. operator-interactive,
swarm-infra) carry a sentinel `build_id` (`"none"`) — never NULL, so
queries are total.

### R3 — every telemetry row carries `(spec_id, build_id)`

Sentinel's `execution_events` (and any derived telemetry table) gains
the same two columns. The Sentinel ingest path stamps them from the
build context.

### R4 — attribution is queryable both directions

- **By build** — "every event + telemetry row for `build_id=X`" — the
  input to replay (063).
- **By spec** — "every build of `spec_id=Y`, with outcomes" — the
  input to learning (064).

Both are first-class indexed queries.

### R5 — the build lifecycle is itself recorded

A build emits a `build.started` and a terminal `build.done` /
`build.failed` chain event carrying `(spec_id, build_id)` and the
outcome. This bookends every build so replay knows the bounds and
learning can compute success rates.

## Boundary cases

1. **Multi-spec build** (`mini_open(specs=["051","052"])`) → one
   `build_id`, multiple `spec_id`s. Attribution is `(build_id, [spec_id...])`;
   events that belong to a specific spec within the build carry the
   single `spec_id`, build-level events carry all.
2. **Build with no spec** (operator break-glass, infra cron) →
   `spec_id="none"`, a real `build_id` still minted so the work is
   replayable.
3. **Re-dispatch of the same spec** → new `build_id` each time; the
   spec's history is the set of its builds (R4 by-spec query).
4. **Event arrives before `build_id` is known** (race at build start)
   → forbidden by R1 (mint *first*, then any event). A test asserts no
   event predates its `build.started`.

## Open questions

- **Q1 — minting point** (charter Q2). Octi DispatchWorkflow,
  clawta-poller, and `mini_open` are three possible mint sites. A build
  may flow through more than one. Proposed: the *outermost* dispatcher
  mints; inner layers inherit via context. A single `build_id` per
  operator-or-poller-initiated unit of work.
- **Q2 — `build_id` shape.** UUID, or structured
  (`build-<spec_id>-<ts>-<hash>`)? Structured is greppable and sorts by
  time; pairs naturally with spec 051's goal_id scheme. Proposed:
  structured.
- **Q3 — migration.** Existing chain events / telemetry rows predate
  attribution. Backfill with `build_id="legacy"`, or leave NULL-free
  via a one-time migration? Proposed: one-time migration stamps
  `"legacy"` so all queries stay total.

## Non-goals

- No change to *what* events the kernel or Sentinel record — only the
  addition of two attribution columns.
- No replay logic here — that is spec 063. 062 only guarantees the
  data replay will need.

## Acceptance criteria

- **AC1** — chain event schema has non-null `spec_id` + `build_id`.
- **AC2** — `execution_events` (and derived telemetry) have the same
  two non-null columns.
- **AC3** — a dispatched build emits `build.started` and exactly one
  terminal `build.done`/`build.failed` (R5).
- **AC4** — the by-build and by-spec queries (R4) return correct,
  complete sets, verified by test over a seeded multi-build fixture.
- **AC5** — no event ever predates its build's `build.started`
  (boundary 4), proven by test.
- **AC6** — the migration leaves zero NULL `build_id`/`spec_id` rows
  (Q3).

## Slice plan

- **Slice 1** — schema columns + minting (R1, R2, R3) + the migration
  (Q3). AC1, AC2, AC6.
- **Slice 2** — build lifecycle events + the two query directions
  (R4, R5). AC3, AC4, AC5.
