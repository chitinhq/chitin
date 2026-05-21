# Spec 063: Cross-layer replay (L5)

**Status**: DRAFT 2026-05-19 — awaiting red sign-off. Implements layer
L5 of charter spec 060. Depends on spec 062 (attribution). Inherits
charter Q3 (replay scope) as an open question below.

**Author lens (Knuth)**: state the replay invariant precisely. "Same
specs + same events ⇒ same result" — but *which* result? An
observational reconstruction and an executable re-run are different
claims. This spec must not blur them.

## Summary

Charter 060 L5: the chitin-kernel event chain (L2) + the OctiEvent
mirror (L4) + agent-bus history together are a *complete* record of a
build. This spec defines **cross-layer replay** — given a `build_id`,
reconstruct that build's full timeline from all three event sources,
keyed by the attribution from spec 062.

Per charter R4, replay MUST work before learning (064) and `/goal`
(065) are built — a rebuild with no replay is prompt-and-hope again.

## Motivation

- **Three event sources, no unified view.** The kernel chain knows
  tool calls; OctiEvent knows workflow steps; agent-bus knows
  inter-agent messages. Today nothing stitches them into one ordered
  story of "what happened in build X".
- **Replay is the audit substrate.** Spec 064's invariant mining and
  spec 065's `/goal` rebuild both consume a build's full history. They
  need one timeline, not three logs.
- **Determinism is only a claim until you can replay it.** The kernel
  is append-only; OctiEvent is replayable; but "the build is
  reproducible" is unverified until a replay actually reproduces it.

## Definitions

- **Observational replay** — reconstruct *what happened*: an ordered,
  cross-source timeline of a build, for analysis/audit. No code runs.
- **Executable replay** — *re-run* the build's recorded decisions and
  obtain the same artifacts. Code runs; results are diffed.

## Requirements

### R1 — gather all three sources by `build_id`

Replay takes a `build_id` and selects every kernel chain event, every
OctiEvent, and every agent-bus message tagged with it (spec 062
attribution). Missing a source is not silent — replay reports which
sources had zero events.

### R2 — one ordered, merged timeline

The three sources merge into a single timeline ordered by a total
order — primary key timestamp, tie-broken by a stable per-source
sequence then source name (the Knuth tie-breaker rule). Two replays of
the same `build_id` produce a byte-identical timeline.

### R3 — observational replay (slice 1)

Slice 1 delivers observational replay: the merged timeline, rendered
as a structured artifact (JSON + a human-readable view). It shows, in
order: build start, each tool call (gated/denied), each workflow step,
each agent message, each status transition, build end. This is enough
for 064 (learning) to consume.

### R4 — executable replay (slice 2, gated on Q1)

Executable replay re-runs the build's recorded decisions in a fresh
worktree and diffs the produced artifacts against the original. It is
strictly harder (non-determinism in model outputs, time, network) and
is a separate slice — see Q1.

### R5 — replay is itself attributable

A replay run emits its own `build.started`/`build.done` events
(spec 062) with a `replay_of=<original_build_id>` field, so replays are
distinguishable from original builds and a replay can itself be
replayed.

### R6 — replay degrades honestly

If a source is incomplete (events lost, a daemon was down), replay
produces the partial timeline and marks the gaps explicitly. It never
silently presents a partial history as complete.

## Boundary cases

1. **`build_id` with events in only one source** → valid; timeline is
   that source only; R1 reports the two empty sources.
2. **Clock skew across sources** → the stable per-source sequence
   tie-breaker (R2) keeps ordering deterministic even when timestamps
   collide or mildly disagree.
3. **`build_id="none"` / `"legacy"`** (spec 062 sentinels) → replay
   refuses — these are not real builds. Typed error.
4. **A build still in progress** → replay produces the timeline so far,
   marked `in_progress` (a special case of R6).
5. **Multi-spec build** → one timeline; each event keeps its own
   `spec_id` so the timeline can be filtered per spec.

## Open questions

- **Q1 — observational vs executable scope** (charter Q3). Proposed:
  063 slice 1 = observational only; executable replay (R4) is slice 2
  and may itself spin off a dedicated spec given model-output
  non-determinism. Sign-off needed on shipping observational-first.
- **Q2 — timeline storage.** Is a replayed timeline persisted (a
  replay artifact table) or recomputed on demand? Proposed: recompute
  on demand from the event sources (they are the source of truth);
  cache only if 064/065 make it a hot path.
- **Q3 — agent-bus retention.** The kernel chain is append-only
  forever; agent-bus history may be pruned. If bus messages for an old
  `build_id` are gone, replay is permanently partial. Does the bus
  need a retention guarantee for replay? Operator/design-review call.

## Non-goals

- No executable replay in slice 1 (R4 is slice 2).
- No replay UI — the artifact is JSON + a text view; visualization is
  later.
- No change to what L2/L3/L4 record — 063 only *reads* and merges.

## Acceptance criteria

- **AC1** — given a seeded `build_id` with events in all three
  sources, replay produces one ordered timeline containing all of them.
- **AC2** — two replays of the same `build_id` produce byte-identical
  timelines (R2 determinism).
- **AC3** — a `build_id` present in only one source replays with that
  source's events and an explicit report of the two empty sources.
- **AC4** — `build_id="none"`/`"legacy"` is refused with a typed error.
- **AC5** — a partial-history build is rendered with gaps explicitly
  marked, never as complete (R6).
- **AC6** — a replay run is itself attributed with `replay_of` (R5).

## Slice plan

- **Slice 1** — observational replay: gather, merge, render. R1, R2,
  R3, R5, R6. AC1–AC6.
- **Slice 2** — executable replay (R4) — gated on Q1; may become its
  own spec.
