# Spec 054: Chitin SDD Platform — charter

**Status**: DRAFT 2026-05-19 — awaiting red sign-off (constitution §1
pair-write rule). Slot 054 free (050 last on main; 051–053 in PR #799).

**Type**: charter spec. Unlike an implementation spec, 054 does not ship
code. It ratifies an *architecture* and *binds a spec roadmap* — its
acceptance is that the layer contracts and the downstream spec sequence
are agreed. The strategy narrative is
`docs/strategy/chitin-spec-driven-platform.md`; this spec is its
ratifiable contract form.

**Author lens (da Vinci)**: this is open-ended architecture — name the
layers, the contracts between them, and the order of construction.
Resist specifying L7 before L1's contract is firm; the whole structure
fails if the foundation is vague.

## Summary

Chitin's moat is **spec-driven development made deterministic and
observable**: specs are the source of truth, every agent action is
gated and recorded, builds are replayable, and telemetry aggregated
across builds makes the next build better — culminating in `/goal`
mode, single-invocation app reconstruction from the specced +
telemetered corpus.

This charter ratifies the seven-layer capability stack, the contract
each layer owes the one above, the current built/gap state, and the
numbered spec roadmap (055–059) that realizes the gaps.

## Motivation

1. **Agent coding has no source of truth.** Prompt-and-hope produces no
   record of *why*, no replay, no cross-build learning. Every run is
   zero-state.
2. **Spec formats are inert.** spec-kit, OpenSpec, Superpowers define
   how to *write* a spec — none provide a deterministic runtime that
   executes against it, records the execution, and learns.
3. **Determinism engines have no spec/agent layer.** Temporal replays
   workflows but knows nothing of specs or agents.
4. **Chitin already owns the rare middle.** It has a kernel event chain
   (determinism), Sentinel (telemetry), agent-bus + Mini + Octi
   (swarm orchestration), and now spec-kit (specs). The pieces exist
   but are not yet unified into one spec→execute→record→replay→learn
   loop. 054 names that loop and sequences its completion.

## The capability stack (ratified architecture)

Seven layers; each depends only on the contract of the layer below.

| Layer | Name | Contract it provides upward |
|---|---|---|
| L1 | Spec | A unified spec model — `{id, title, requirements[], acceptance_criteria[], boundaries[], slices[]}` — any framework's spec normalized into it. |
| L2 | Determinism | Every side effect is an append-only chain event with enough data to replay it. |
| L3 | Telemetry | Execution telemetry is queryable and attributable to a `(spec_id, build_id)`. |
| L4 | Orchestration | A spec in → workers dispatched, kernel-gated → status out; every step a chain event. |
| L5 | Observability + replay | Any build replays deterministically: same specs + same events ⇒ same result. |
| L6 | Aggregation / learning | Mined invariants are durable, attributable, and consumable by the next build. |
| L7 | `/goal` rebuild | Given specs + accumulated chains + mined improvements, one `/goal` reconstructs the app. |

## Requirements

### R1 — the layer contracts are the ratified interface

Each layer's upward contract (table above) is binding. A downstream
spec (055–059) MUST implement its layer's contract without requiring
changes to the contract of any lower layer. If a lower contract proves
insufficient, that is a charter amendment (new 054 revision), not an
ad-hoc reach-through.

### R2 — multi-framework spec support is non-negotiable (L1)

The platform MUST NOT be coupled to a single spec format. spec-kit,
OpenSpec, and Superpowers are all first-class *inputs*; each gets an
adapter into the unified spec model. Layers L2–L7 see only the unified
model, never a source format.

### R3 — spec + build attribution is universal (L2/L3)

Every chain event (L2) and every telemetry row (L3) MUST carry the
`spec_id` and `build_id` it belongs to. Without attribution there is no
per-spec replay (L5) and no per-spec learning (L6). This is the
load-bearing invariant of the whole stack.

### R4 — replay before learning, learning before `/goal`

The construction order is fixed: L5 (replay) MUST be working before L6
(aggregation/learning) is built, and L6 MUST be working before L7
(`/goal` rebuild). A `/goal` rebuild without replay is prompt-and-hope
with extra steps. Specs 057 → 058 → 059 land in that order.

### R5 — the roadmap is bound to numbered specs

The gaps are realized by these specs, written and ratified in order:

| Spec | Layer | Scope |
|---|---|---|
| 055 | L1 | Unified spec model + framework adapters (spec-kit / OpenSpec / Superpowers) |
| 056 | L2/L3 | Spec ↔ build attribution — `(spec_id, build_id)` on every event + telemetry row |
| 057 | L5 | Cross-layer replay — reconstruct a build from chain + OctiEvent + bus history |
| 058 | L6 | Telemetry → spec feedback loop — mined invariants become spec amendments / dispatch policy |
| 059 | L7 | `/goal` rebuild engine — single-goal app reconstruction |

Existing specs ladder in unchanged: 038/039/050–053 (Mini — L4 worker
primitive), 040–049 (Octi — L4 controller), 001 (agent-bus — L4
transport), the Sentinel specs (L3).

### R6 — the charter is the north star for grooming

Any ticket promoted that claims to advance "the platform" MUST cite the
layer (L1–L7) it serves and the numbered spec (055–059) it implements.
Work that fits no layer is either out of scope or a charter amendment.

## Current state — built vs. gap

| Layer | Status |
|---|---|
| L1 spec | spec-kit installed (PR #802); OpenSpec ❌; Superpowers partial |
| L2 determinism | chitin-kernel event chain — built |
| L3 telemetry | Sentinel — built |
| L4 orchestration | agent-bus + Mini built; Octi specced-not-built; poller restoration in flight |
| L5 replay | gap |
| L6 aggregation | Sentinel mines invariants; feedback loop — gap |
| L7 `/goal` rebuild | gap |

## Non-goals

- 054 does not implement any layer — it ratifies the architecture and
  roadmap only.
- 054 does not pick OpenSpec/Superpowers integration *mechanics* — that
  is spec 055.
- 054 does not redesign the kernel, Sentinel, Octi, or agent-bus — they
  are existing contracts L055+ build on.

## Open questions

- **Q1 — unified spec model owner.** Does the normalized spec model
  live in the kernel (Go), in a Python service, or as a schema-only
  contract? Resolve in spec 055.
- **Q2 — build_id minting.** What mints a `build_id`, and when — at
  dispatch, at `mini_open`, at the first chain event? Resolve in 056.
- **Q3 — replay scope.** Is L5 replay *observational* (reconstruct what
  happened for analysis) or *executable* (re-run and get the same
  artifacts)? They are very different builds. Proposed: observational
  first (057 slice 1), executable later. Resolve in 057.
- **Q4 — does `/goal` rebuild in place or to a fresh worktree?** A
  rebuild that overwrites is dangerous; a rebuild to a fresh tree is
  comparable/diffable. Proposed: fresh worktree. Resolve in 059.
- **Q5 — OpenSpec / Superpowers priority.** spec-kit is in. Which of
  OpenSpec / Superpowers is the second adapter, and is the third
  deferred? Operator call, feeds spec 055.

## Acceptance criteria

A charter is accepted when its architecture and roadmap are ratified —
not when code passes.

- **AC1** — red signs off the seven-layer stack and the L1–L7 contracts
  (R1).
- **AC2** — red signs off the 055–059 roadmap and the fixed
  replay→learn→`/goal` ordering (R4, R5).
- **AC3** — the strategy doc `docs/strategy/chitin-spec-driven-platform.md`
  and this charter are consistent — no contradiction between them.
- **AC4** — Q1–Q5 are either answered here or explicitly delegated to a
  named downstream spec.
- **AC5** — on ratification, specs 055–059 are created as `triage`
  kanban tickets so the swarm can groom them (constitution §1).

## Slice plan

Single artifact — a charter is not sliced. Its *downstream* specs
(055–059) are where slicing happens. Construction order is fixed by R4:
055 → 056 → 057 → 058 → 059, bottom-up on the contracts.
