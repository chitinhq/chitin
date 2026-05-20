# Chitin as a Spec-Driven Development Platform

> Strategy doc. Authored 2026-05-19 (red + claude, chitin-console session).
> Status: DRAFT — the thesis and capability stack below are the input to
> charter spec `054-chitin-sdd-platform-charter`.

## The thesis (the moat)

Agent coding today is **vibes**: you prompt, the agent does something, you
hope. There is no source of truth, no record of why a decision was made,
no way to replay a build, and nothing learns across builds. Every run
starts from zero.

Chitin's bet: **spec-driven development becomes trustworthy when it is
deterministic and observable.** Specs are the source of truth. Every
agent action is gated and recorded against an append-only event chain.
The whole build is replayable. Telemetry aggregated across many builds
mines invariants that make the *next* build better.

The showcase capability — the thing that proves the moat — is **`/goal`
mode**: because the app's specs, the kernel event chain, and the mined
improvements all persist, a single `/goal` invocation can **rebuild the
entire app** — and rebuild it *better* than last time, because the
system learned from every prior run.

Nobody else has this. Cursor/Copilot/Claude Code are single-session
vibes tools. spec-kit/OpenSpec are spec *formats* with no runtime.
Temporal is determinism with no spec or agent layer. Chitin is the only
stack that puts **spec → deterministic execution → telemetry → replay →
aggregated learning** in one loop. That loop is the moat.

## The capability stack

Seven layers. Each is a contract; the layer above depends only on the
contract below.

```
  ┌─────────────────────────────────────────────────────────┐
  │ L7  /goal rebuild        single-goal app (re)construction │
  ├─────────────────────────────────────────────────────────┤
  │ L6  aggregation/learning telemetry → mined invariants →   │
  │                          fed back into specs + dispatch   │
  ├─────────────────────────────────────────────────────────┤
  │ L5  observability+replay event chain + OctiEvent mirror + │
  │                          agent-bus history = full replay  │
  ├─────────────────────────────────────────────────────────┤
  │ L4  orchestration        Octi workflows + swarm (agent-   │
  │                          bus, Mini, drivers, poller)      │
  ├─────────────────────────────────────────────────────────┤
  │ L3  telemetry            Sentinel — execution events,     │
  │                          detection passes, invariant mine │
  ├─────────────────────────────────────────────────────────┤
  │ L2  determinism          chitin-kernel — gate every tool  │
  │                          call, append to the event chain  │
  ├─────────────────────────────────────────────────────────┤
  │ L1  spec                 multi-framework spec model —     │
  │                          spec-kit, OpenSpec, Superpowers  │
  └─────────────────────────────────────────────────────────┘
```

### L1 — Spec layer (multi-framework)

Specs are the source of truth. Chitin must not be married to one spec
*format*. The three the operator named:

- **spec-kit** (github/spec-kit) — now installed (PR #802).
  `.specify/specs/NNN-slug/spec.md`, `/speckit-*` commands.
- **OpenSpec** — a competing spec-format/workflow. Not integrated.
- **Superpowers** — the gstack skill/plan system. `docs/superpowers/`
  exists; plans live there. Partially present.

The contract L1 owes the rest of the stack: a **unified spec model** —
a normalized `{id, title, requirements[], acceptance_criteria[],
boundaries[], slices[]}` shape that any framework's spec is adapted
into. Layers above never see the source format.

### L2 — Determinism (chitin-kernel)

The kernel gates every tool call and appends to the event chain — an
append-only ledger of *what happened*. This is the determinism
substrate. It already exists. The contract: every side effect is a
chain event with enough information to be replayed.

### L3 — Telemetry (Sentinel)

Sentinel ingests execution events, runs detection passes, and mines
invariant proposals. It is the *what went wrong / what patterns
recur* analysis. It exists. The contract: telemetry is queryable and
attributable to a spec + a build.

### L4 — Orchestration (Octi + swarm)

Multi-agent execution, kernel-gated for safety. Octi (Temporal
workflows, specced 040–049, not yet built) is the deterministic
controller; the swarm (agent-bus, Mini sessions, drivers, the poller)
is the worker fabric. The contract: a spec in → workers dispatched →
status out, every step a chain event.

### L5 — Observability + replay

The event chain (L2) + OctiEvent mirror (L4) + agent-bus history
together are a *complete* record of a build. The contract: any build
can be replayed deterministically from its recorded events — same
specs + same events ⇒ same result.

### L6 — Aggregation / learning

The flywheel. Telemetry across *many* builds (L3) feeds invariant
mining; mined invariants flow back into specs (L1) and dispatch
policy (L4). Each build makes the corpus smarter. The contract:
learnings are durable, attributable, and consumable by the next build.

### L7 — `/goal` rebuild

The apex. Given an app's specs + its accumulated event chains + the
mined improvements, a single `/goal` invocation reconstructs the app.
Not from scratch-vibes — from the *aggregated, specced, telemetered*
history. The rebuild is better than the last because L6 fed forward.

## Current state — built vs. gap

| Layer | Status | Evidence |
|---|---|---|
| L1 spec | spec-kit ✅ (PR #802); OpenSpec ❌; Superpowers ◐ | `.specify/`, `docs/superpowers/` |
| L2 determinism | ✅ kernel event chain exists | `go/execution-kernel/` |
| L3 telemetry | ✅ Sentinel exists | `/sentinel` skill, Neon `execution_events` |
| L4 orchestration | ◐ agent-bus + Mini ✅; Octi specced-not-built; **poller currently OFF** | specs 040–049, `swarm/` |
| L5 obs + replay | ◐ chain exists; unified cross-layer replay ❌ | — |
| L6 aggregation | ◐ Sentinel mines invariants; feedback loop into specs ❌ | — |
| L7 `/goal` rebuild | ❌ not built | — |

The substrate (L2, L3) is real. The gaps are: a **unified spec model**
(L1), **cross-layer replay** (L5), the **feedback loop** (L6), and the
**`/goal` engine** (L7). Octi (L4) is specced; the poller needs
restoring (operational, in flight).

## Spec roadmap

This vision is realized as specs — chitin eats its own dog food. The
charter spec `054` binds this roadmap; each capability below is a
numbered spec to be written and ratified in sequence.

1. **054 — SDD platform charter** *(this roadmap, ratifiable)*
2. **055 — unified spec model + framework adapters** (L1) — normalize
   spec-kit / OpenSpec / Superpowers into one model.
3. **056 — spec ↔ build attribution** (L2/L3) — every chain event and
   telemetry row carries its spec id + build id.
4. **057 — cross-layer replay** (L5) — replay a build from chain +
   OctiEvent + bus history.
5. **058 — telemetry → spec feedback loop** (L6) — mined invariants
   become spec amendments / dispatch policy.
6. **059 — `/goal` rebuild engine** (L7) — single-goal app
   reconstruction from the specced + telemetered corpus.

Existing specs that ladder into this: 038/039/050/051/052/053 (Mini —
the L4 worker primitive), 040–049 (Octi — the L4 controller),
001 (agent-bus), the Sentinel specs (L3).

## Sequencing principle

Build **bottom-up on the contracts**: L1 unified spec model first
(everything references specs), then L2/L3 attribution (so telemetry is
spec-anchored), then L5 replay, then L6 feedback, then L7. Do not start
L7 before L5 — a rebuild with no replay is just vibes again.

## Near-term (this is where the swarm starts)

1. Land PR #802 (spec-kit) and PR #799 (specs 050s2/051/052/053).
2. Restore the dispatch poller (operational — handed to Clawta).
3. Ratify charter spec 054.
4. Write + groom specs 055–059 onto the kanban; the swarm implements
   bottom-up.
