# Spec 058: Telemetry → spec feedback loop (L6)

**Status**: DRAFT 2026-05-19 — awaiting red sign-off. Implements layer
L6 of charter spec 054. Depends on specs 056 (attribution) and 057
(replay).

**Author lens (da Vinci)**: this is the flywheel — the part that makes
chitin *learn*. Design the loop so each turn provably leaves the corpus
smarter, and so a bad mined "improvement" can never silently degrade a
future build.

## Summary

Charter 054 L6 — the flywheel. Sentinel already mines invariant
proposals from telemetry (L3). This spec closes the loop: mined
invariants, attributed per spec (056) and grounded in replayable
builds (057), become **spec amendments** and **dispatch-policy
updates** — so the next build of a spec is informed by every prior
build of it.

## Motivation

- **Mining without feedback is a dead end.** Sentinel mines invariant
  proposals today, but they do not flow anywhere — they are reports a
  human may or may not read. The corpus does not get smarter on its
  own.
- **This is the moat's compounding term.** charter thesis: "telemetry
  aggregated across builds makes the next build better." Without L6
  every build is still zero-state; with it, build N benefits from
  builds 1..N-1. That compounding is what `/goal` (L7) monetizes.
- **Per-spec grain is now available.** Spec 056 made telemetry
  attributable to `spec_id`; spec 057 made each build replayable. L6
  can finally ask "how does *this spec* tend to fail, across all its
  builds?" and act on the answer.

## Definitions

- **Mined invariant** — a Sentinel-produced proposal: "across builds
  of spec X, pattern P holds / fails Q% of the time".
- **Spec amendment** — a proposed edit to a `UnifiedSpec` (055): a new
  boundary case, a tightened acceptance criterion, an added invariant.
- **Dispatch-policy update** — a proposed change to how the
  orchestration layer (L4) dispatches: driver choice, retry, veto,
  concurrency.

## Requirements

### R1 — mined invariants are attributed and grounded

Every mined invariant carries the `spec_id`(s) it concerns and cites
the `build_id`s that evidence it (056 attribution). A claim with no
cited builds is rejected — no ungrounded "improvements".

### R2 — an invariant becomes a typed proposal

A mined invariant is converted to one of two typed proposals:
`SpecAmendment{ spec_id, section, change, rationale, evidence[] }` or
`DispatchPolicyUpdate{ scope, change, rationale, evidence[] }`. The
proposal is data, not prose — reviewable and applicable mechanically.

### R3 — operator review gate (no silent self-modification)

A proposal is NEVER auto-applied to a spec or to dispatch policy. It
lands in a review queue; an operator (or a delegated reviewing agent
under constitution §1) approves, edits, or rejects. The loop learns;
it does not self-rewrite unsupervised. This is the safety boundary of
the flywheel.

### R4 — an approved amendment edits the spec through 055

An approved `SpecAmendment` is applied to the `UnifiedSpec` and
written back through the spec 055 native-format renderer (055 R6
round-trip). The spec's `status` and git history record the amendment;
the originating `build_id`s are cited in the spec.

### R5 — an approved policy update is versioned

An approved `DispatchPolicyUpdate` changes dispatch policy as a
versioned, attributable change — never an unlogged mutation. The L4
layer reads the current policy version; a replay (057) of an old build
can see which policy version was in force.

### R6 — the loop is itself telemetered

Each turn of the loop emits telemetry: proposals raised, approved,
rejected, and — critically — whether builds *after* an applied
amendment actually improved on the cited failure. A proposal that did
not help is flagged for revert. The flywheel measures itself.

## Boundary cases

1. **Contradictory proposals** (two invariants propose opposite
   amendments to one spec section) → both surface in the review queue
   flagged as conflicting; the operator resolves. Never auto-merge.
2. **Proposal evidence goes stale** (cited builds pruned past
   retention — see 057 Q3) → the proposal is marked
   `evidence-incomplete`; it may still be reviewed but the gap is
   explicit.
3. **An amendment that made things worse** (R6 detects post-amendment
   builds still fail) → auto-flagged for revert; revert itself goes
   through the R3 review gate.
4. **A spec with too few builds to mine** → no proposals; not an error.
   Mining has a minimum-evidence threshold (see Q1).

## Open questions

- **Q1 — minimum evidence threshold.** How many builds of a spec before
  an invariant may be proposed? Too low → noise; too high → slow
  learning. Proposed: configurable, default 5 builds.
- **Q2 — reviewing agent.** charter §1 allows a delegated agent to
  review under operator sign-off. May Clawta/Ares triage the proposal
  queue, with red as final approver? Operator call.
- **Q3 — policy version store.** Where does dispatch-policy version
  live — chitin-kernel config, a kanban-board config, a dedicated
  table? Resolve with L4 (Octi) design.
- **Q4 — amendment to a ratified spec.** Editing a ratified spec via an
  approved amendment — does it re-open `status` to `draft`, or land as
  a ratified revision? Proposed: ratified revision with a changelog
  entry; no re-draft churn.

## Non-goals

- No auto-application — R3 is absolute. 058 is a *proposal* engine, not
  an autonomous rewriter.
- No new mining algorithms — Sentinel's existing detection passes are
  the source; 058 is the *routing and feedback*, not the mining.
- No `/goal` rebuild — that is spec 059. 058 makes the corpus smarter;
  059 consumes the smarter corpus.

## Acceptance criteria

- **AC1** — every mined invariant carries `spec_id`(s) + cited
  `build_id`s; an ungrounded one is rejected (R1).
- **AC2** — an invariant produces a typed `SpecAmendment` or
  `DispatchPolicyUpdate` proposal (R2).
- **AC3** — no proposal is ever applied without passing the review
  gate (R3), proven by test — an unreviewed proposal cannot mutate a
  spec or policy.
- **AC4** — an approved amendment edits the spec losslessly via the
  055 renderer and records the citing builds (R4).
- **AC5** — dispatch-policy updates are versioned and visible to a
  057 replay (R5).
- **AC6** — the loop emits its own telemetry, including
  post-amendment improvement/regression detection (R6).

## Slice plan

- **Slice 1** — R1, R2, R3: attributed proposals + the review queue +
  the no-auto-apply guarantee. AC1–AC3.
- **Slice 2** — R4, R5: applying approved amendments + versioned
  policy. AC4, AC5.
- **Slice 3** — R6: loop self-telemetry + post-amendment regression
  detection. AC6.
