# Spec 059: `/goal` rebuild engine (L7)

**Status**: DRAFT 2026-05-19 — awaiting red sign-off. Implements layer
L7 of charter spec 054 — the apex capability. Depends on specs 055
(unified model), 056 (attribution), 057 (replay), 058 (feedback loop).
Inherits charter Q4 (in-place vs fresh worktree) below.

**Author lens (da Vinci)**: this is the showcase — the capability that
proves the moat. Design it so the rebuild is *comparable* (you can diff
it against what exists) and *honest* (it never claims a determinism it
does not have). The magic is real only if it is measurable.

## Summary

Charter 054 L7: given an app's specs + its accumulated event chains +
the mined improvements, a single **`/goal`** invocation reconstructs
the app — and reconstructs it *better* than the last build, because L6
fed the learnings forward.

This is not scratch-vibes. The rebuild is driven by the **specced,
telemetered, replayable corpus**: the unified specs (055) say what to
build, the attributed event chains (056) and replay (057) say how prior
builds went, and the mined amendments (058) say what to do differently.

## Motivation

- **This is what chitin is *for*.** Every layer below exists to make
  L7 possible. The thesis ("you should be able to build a project with
  chitin, then rewrite it in a single `/goal`") is only true when 059
  ships.
- **It is the demonstration of the moat.** A `/goal` rebuild that is
  visibly better than the last — fewer failed builds, fewer mined
  defects recurring — is the proof no vibes tool can match.
- **It compounds.** Each `/goal` run is itself a build (056) — it is
  telemetered, replayable, and feeds 058. The engine improves the
  engine.

## Definitions

- **App corpus** — for a given app/project: its set of `UnifiedSpec`s
  (055) + all builds' event chains (056) + replay timelines (057) +
  applied amendments and policy versions (058).
- **`/goal` run** — one invocation of the rebuild engine against an
  app corpus. Itself a build, with its own `build_id`.

## Requirements

### R1 — `/goal` takes a goal + an app corpus, not a blank prompt

A `/goal` run is parameterized by (a) the goal statement and (b) the
target app's corpus. The engine MUST load the corpus — specs,
prior-build outcomes, mined amendments — and make it available to the
rebuild. A `/goal` with no corpus is rejected: that is just `mini_open`
with extra words.

### R2 — rebuild to a fresh, diffable worktree

A `/goal` run reconstructs the app into a **fresh worktree**, never
in-place over the existing app (charter Q4). The output is therefore
diffable against the current app — the operator can see exactly what
the rebuild changed, and a bad rebuild is discarded by deleting a
worktree, not by reverting production.

### R3 — the rebuild consumes the learnings (the whole point)

The engine MUST feed the mined amendments and dispatch-policy updates
(058) into the rebuild: known-recurring defects from prior builds are
pre-empted, not re-encountered. A `/goal` run that ignores the corpus
learnings is a 059 contract violation — it would just be a fresh build.

### R4 — the rebuild is decomposed into specced, dispatched work

`/goal` does not hand one giant prompt to one agent. It decomposes the
goal against the app's specs into dispatchable units, and dispatches
them through the existing L4 orchestration (Octi / poller / Mini,
specs 050–053) — kernel-gated, attributed, every step a chain event.
The rebuild is itself a swarm build, not a monolith.

### R5 — a `/goal` run is a build, recursively

A `/goal` run mints its own `build_id` (056), emits `build.started` /
`build.done`, is replayable (057), and feeds 058. The engine is inside
the loop it powers — each `/goal` run makes the next one better.

### R6 — the rebuild is measured against its predecessor

A `/goal` run produces a comparison report: build success rate,
recurrence of previously-mined defects, spec-coverage delta, vs. the
prior build of the same app. "Better than last time" (the charter
claim) is a number in this report, not a vibe. If a rebuild is *not*
better, that is surfaced, not hidden.

### R7 — honest determinism

The engine MUST NOT claim bit-identical reproduction. Model outputs are
non-deterministic; the rebuild is *spec-faithful and learning-informed*,
not byte-reproducible. The comparison report (R6) states what is
guaranteed (every spec's acceptance criteria met) and what is not
(identical source).

## Boundary cases

1. **App with specs but no prior builds** → `/goal` runs as a
   first build: no learnings to feed (R3 is a no-op), the comparison
   report (R6) notes "no predecessor". Valid — the first `/goal` is
   allowed.
2. **Goal contradicts the corpus specs** → the engine surfaces the
   conflict and refuses rather than silently overriding ratified
   specs. A goal that changes intent must amend specs first (058).
3. **A decomposed unit fails repeatedly** → the rebuild does not hang;
   it fails the `/goal` run with a partial-rebuild report (the fresh
   worktree is kept for inspection, R2).
4. **Concurrent `/goal` runs on the same app** → each gets its own
   `build_id` and worktree; they do not collide. Comparing them is an
   operator choice.
5. **Corpus learnings conflict** (058 left contradictory amendments)
   → 058 R3/boundary-1 should have caught it; if one slips through,
   the `/goal` run flags it and proceeds with the ratified spec, not
   the unreviewed amendment.

## Open questions

- **Q1 — in-place vs fresh worktree** (charter Q4). Proposed: fresh
  worktree, always (R2). Sign-off needed — this is the safety call.
- **Q2 — decomposition owner.** Who decomposes the goal into
  dispatchable units (R4) — a planning agent, an Octi workflow, the
  `/goal` engine itself? Proposed: an Octi planning workflow, so the
  decomposition is itself deterministic and replayable.
- **Q3 — "better" metric weighting** (R6). Build success rate, defect
  recurrence, spec coverage — how are they weighted into a single
  verdict? Proposed: report all three separately; no single score in
  v1 — let the operator judge.
- **Q4 — full rebuild vs incremental.** Does `/goal` always rebuild
  the whole app, or can it rebuild only the specs that changed since
  the last build? Proposed: full rebuild in slice 1 (simplest, proves
  the capability); incremental is a later slice.
- **Q5 — scope of the first target.** What is the first app `/goal`
  rebuilds — chitin itself (dogfood), or a small external project?
  Operator call. Dogfooding chitin is the strongest proof but the
  highest risk.

## Non-goals

- No claim of byte-identical reproduction (R7).
- No new orchestration — `/goal` dispatches through existing L4
  (Octi/poller/Mini); it does not build a parallel executor.
- No incremental rebuild in slice 1 (Q4).
- `/goal` does not author specs — it consumes them. Spec authoring
  stays with `/speckit-specify` / hand-authoring / 058 amendments.

## Acceptance criteria

- **AC1** — `/goal` rejects a run with no app corpus (R1).
- **AC2** — a `/goal` run produces a fresh worktree, diffable against
  the current app, never an in-place overwrite (R2).
- **AC3** — the run demonstrably consumes 058 amendments — a
  previously-mined defect, present in the corpus, does not recur in the
  rebuild (R3), proven by a seeded test.
- **AC4** — the goal is decomposed and dispatched through L4
  orchestration; every unit is an attributed, chain-recorded build
  (R4).
- **AC5** — the `/goal` run is itself a build: own `build_id`,
  replayable (057), feeds 058 (R5).
- **AC6** — the run emits a comparison report with success rate,
  defect-recurrence, and spec-coverage vs. the prior build (R6); a
  non-improvement is surfaced.
- **AC7** — the engine never claims bit-identical reproduction; the
  report states guaranteed vs. non-guaranteed properties (R7).

## Slice plan

- **Slice 1** — R1, R2, R4, R5, R7: a `/goal` run that loads a corpus,
  decomposes + dispatches through L4 into a fresh worktree, is itself a
  build, and is honest about determinism. Full rebuild only. AC1, AC2,
  AC4, AC5, AC7.
- **Slice 2** — R3, R6: feeding 058 learnings into the rebuild + the
  comparison report. AC3, AC6. This is the slice that makes `/goal`
  *better than last time* — the moat payload.
- **Slice 3** — incremental rebuild (Q4) — rebuild only changed specs.
