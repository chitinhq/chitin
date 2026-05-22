# Feature Specification: SDD Admission Gate

**Feature Branch**: `084-sdd-admission-gate`

**Created**: 2026-05-21

**Status**: Draft

**Input**: User description: "Make the chitin governance gate enforce
spec-driven development, so implementation work cannot proceed without a spec."

## Overview

Chitin is a spec-driven project: every feature is supposed to begin as a
`spec.md` under `specs/`. But nothing *enforces* it. Code can be — and is —
written with no spec: this very session implemented spec 083's work ad-hoc and
only specced it afterward, when an operator noticed.

This feature makes the chitin governance gate enforce SDD. Implementation work
that does not trace to a spec is **made visible**, and — once the blast radius
is measured — **blocked**. The gate does not slow anyone down who is following
the process; it only catches work that skipped it.

### The chicken-and-egg, resolved

Enforcing "work needs a spec" appears paradoxical — writing a spec is itself
work. It is not, because the gate watches **implementation**, not **all
activity**. Writing a spec produces files under `specs/` and `.specify/`; that
is *spec-authoring* and is outside the gate's trigger surface entirely — not by
a special exemption, but because it is not the kind of action the gate watches.
The lifecycle is naturally ordered: specify (ungated, produces the spec) →
implement (gated, consumes the spec). You can always write a spec; you simply
cannot write implementation code until its spec exists — which is the correct
order. The gate's own implementation code traces to this spec (084).

### Prior art to reconcile (not duplicate)

- **Constitution §3** already mandates a spec-kit promotion gate for tickets.
- `has_spec_kit_entry()` in `swarm/workflows/hermes-clawta-bridge.py` checks a
  ticket against `specs/NNN-slug/spec.md`.
- **Spec 020 `sdd-tdd-enforcement`** covers related ground.

This feature extends enforcement from *ticket promotion* to the *governance
gate itself* — covering interactive sessions and orchestrator dispatch — and
MUST be one coherent gate with the above, not a fourth parallel check.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Orchestrator refuses spec-less work units (Priority: P1)

The operator needs the orchestrator to refuse to dispatch a unit of work that
has no spec behind it. Orchestrator work units already carry spec lineage, so
this is a clean hard gate with no ambiguity.

**Why this priority**: It is the highest-confidence, lowest-risk slice — the
spec linkage already exists on work units, so the gate is a single check. It
closes the larger of the two channels (automated dispatch) immediately.

**Independent Test**: Submit a work unit with no spec lineage; confirm the
orchestrator refuses to dispatch it and surfaces the refusal.

**Acceptance Scenarios**:

1. **Given** a work unit with resolvable spec lineage, **When** the
   orchestrator schedules it, **Then** it dispatches normally.
2. **Given** a work unit with no resolvable spec lineage, **When** the
   orchestrator schedules it, **Then** dispatch is refused.
3. **Given** a refused work unit, **When** the operator inspects the run,
   **Then** the refusal and its reason are visible — never a silent drop.

---

### User Story 2 - Spec-less implementation is recorded (Priority: P2)

The operator needs visibility into interactive agent sessions (and any path the
orchestrator does not cover) that write implementation code without a spec.
When a session makes its first source-code mutation without a bound spec, the
kernel records a `spec_missing` governance decision — surfacing the gap without
yet blocking it.

**Why this priority**: It catches the channel the orchestrator hard-gate
cannot see (interactive work — exactly how spec 083 slipped through), and it is
the measurement phase: it produces the telemetry that tells the operator the
real blast radius before any hard block (US3).

**Independent Test**: In a session with no bound spec, make a source-code edit;
confirm a `spec_missing` governance decision is recorded. In a separate session,
do only reads and spec-authoring; confirm no such decision is recorded.

**Acceptance Scenarios**:

1. **Given** a session with no bound spec, **When** it makes its first
   source-code mutation, **Then** a `spec_missing` governance decision is
   recorded.
2. **Given** a session bound to an existing spec, **When** it makes source-code
   mutations, **Then** no `spec_missing` decision is recorded.
3. **Given** any session, **When** it only reads, explores, plans, or authors
   files under `specs/` or `.specify/`, **Then** the gate never triggers.
4. **Given** a session, **When** an agent declares the spec it is implementing,
   **Then** that spec is bound to the session for all subsequent decisions.

---

### User Story 3 - Spec-less implementation can be blocked (Priority: P3)

Once the operator has seen the `spec_missing` telemetry from US2 and judged the
blast radius acceptable, they need the ability to escalate the gate from
*observe* to *enforce* — a first source-code mutation with no bound spec is
denied, with an actionable message telling the agent how to create or bind one.

**Why this priority**: It is the actual teeth, but it MUST follow US2 — denying
before the blast radius is measured risks blocking legitimate work (hotfixes,
exploration that turns into a fix) and causing more harm than the gap. Enforce
is a deliberate, data-informed escalation.

**Independent Test**: With the gate in enforce mode, attempt a source-code
mutation in a session with no bound spec; confirm it is denied with guidance.
Confirm the operator escape hatch still permits a declared P0 hotfix.

**Acceptance Scenarios**:

1. **Given** the gate in enforce mode and a session with no bound spec,
   **When** it attempts a source-code mutation, **Then** the action is denied
   with a message explaining how to create or bind a spec.
2. **Given** the gate in enforce mode and a declared operator P0 hotfix,
   **When** the operator uses the escape hatch, **Then** the mutation proceeds
   and is recorded as an escape-hatch use.
3. **Given** the gate, **When** the operator switches between observe and
   enforce mode, **Then** the change takes effect without a redeploy gap.

---

### Edge Cases

- **The bootstrap** — authoring a spec writes under `specs/`/`.specify/`; this
  is never gated. Writing the gate's own implementation code traces to spec 084.
- **A rubber-stamp spec** — an empty or low-quality spec satisfies the gate
  (existence only). Spec quality is out of scope (see Assumptions); the gate
  must not pretend to assess it.
- A session does only reads, searches, and exploration → the gate never fires.
- A session authors a spec, then implements against it in the same session →
  gated correctly once it crosses into source-code mutation, and it passes
  because the spec it just authored exists.
- A work unit's bound spec was later deleted or renamed → treated as no
  resolvable spec (stale lineage).
- A session legitimately spans two features → it may bind more than one spec;
  every bound spec must exist.
- A P0 production hotfix with no time to spec → the operator escape hatch
  (constitution §1/§3 one-shot hotfix) permits it and records the use.
- The gate itself is unavailable or errors → it must fail toward *observe*
  (record, do not block) so a gate fault never causes a work outage.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The orchestrator MUST refuse to dispatch a work unit that has no
  resolvable spec lineage.
- **FR-002**: A refused work unit MUST be surfaced to the operator with its
  reason — never silently dropped.
- **FR-003**: The gate MUST classify each tool call as *spec-authoring* or
  *implementation* deterministically by path: writes under `specs/` and
  `.specify/` are spec-authoring; writes under source-code paths are
  implementation.
- **FR-004**: The gate MUST trigger only on the **first implementation
  mutation** of a session — a source-code Write/Edit, a code commit, or a
  branch creation. Reads, searches, exploration, planning, and spec-authoring
  MUST NOT trigger it.
- **FR-005**: Spec-authoring activity MUST NEVER be gated — it is the upstream
  that produces specs; gating it would deadlock the process.
- **FR-006**: A session MUST be able to declare and bind the spec(s) it is
  implementing, so subsequent governance decisions can be evaluated against
  that binding.
- **FR-007**: When a triggering mutation occurs in a session with no bound,
  existing spec, the kernel MUST emit a `spec_missing` governance decision.
- **FR-008**: The gate MUST verify only that a spec **exists** for the bound
  reference; it MUST NOT assess spec quality or completeness.
- **FR-009**: The gate MUST support an *observe* mode (record `spec_missing`,
  allow) and an *enforce* mode (record and deny); rollout MUST begin in
  observe.
- **FR-010**: In enforce mode, a denied mutation MUST return an actionable
  message describing how to create or bind a spec.
- **FR-011**: An operator escape hatch MUST exist for declared one-shot/P0
  hotfixes; each use MUST be recorded.
- **FR-012**: The gate MUST be one coherent mechanism with the existing
  spec-kit promotion gate (constitution §3, `has_spec_kit_entry()`, spec 020) —
  not a duplicate or conflicting check.
- **FR-013**: A gate fault (the gate is unavailable or errors) MUST fail toward
  *observe* — record, do not block — so the gate can never cause a work outage.

### Key Entities

- **Work Unit**: a unit of orchestrator-dispatched work; carries (or lacks)
  spec lineage — a reference resolving to an existing spec.
- **Session**: a bounded run of agent work; may have zero or more bound spec
  references.
- **Spec Reference / Binding**: the link from a Work Unit or Session to a
  `specs/NNN-slug/spec.md`; *resolvable* iff that file exists.
- **Tool Call Classification**: the deterministic, path-based verdict —
  *spec-authoring*, *implementation*, or *neither (read/explore)* — that
  decides whether the gate triggers.
- **Governance Decision (`spec_missing`)**: the record emitted when
  implementation occurs without a bound spec — the unit of this gate's
  telemetry.
- **Enforcement Mode**: *observe* or *enforce* — the gate's current posture.
- **Escape-Hatch Use**: a recorded instance of the operator override.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of orchestrator-dispatched work units either have resolvable
  spec lineage or are refused — zero spec-less dispatches.
- **SC-002**: 0% false triggers — no `spec_missing` decision is ever emitted
  for a read, a search, exploration, or spec-authoring activity.
- **SC-003**: In observe mode, 100% of implementation mutations made without a
  bound spec produce a `spec_missing` decision — the gap becomes fully
  measurable.
- **SC-004**: After escalation to enforce mode, the rate of spec-less code
  changes reaching a commit drops to zero (escape-hatch uses excepted).
- **SC-005**: The gate adds no operator-perceptible latency to a governed tool
  call.
- **SC-006**: The gate's own implementation traces to spec 084 — demonstrating
  the bootstrap resolves with no special-casing.
- **SC-007**: A gate fault never blocks work — measured by zero work outages
  attributable to the gate.

## Assumptions

- "Source-code path" vs "spec-authoring path" is decidable by repository
  location: `specs/` and `.specify/` are spec-authoring; the code trees
  (e.g. `go/`, `apps/`, `libs/`, `scripts/`, `swarm/`) are implementation.
  The exact path set is an implementation detail recorded in planning.
- Spec **quality** is out of scope — owned by the `/speckit-specify` quality
  checklist, `/speckit-clarify`, and human review. This gate enforces
  *existence* only (FR-008).
- The chitin session mechanism exists and can carry additional binding data
  (a spec reference); this feature extends it, it does not create sessions.
- The orchestrator already records a spec lineage on work units; US1 enforces
  what is already present.
- Default rollout posture is *observe*; escalation to *enforce* is a
  deliberate operator decision informed by US2's telemetry.
- The operator escape hatch follows the existing one-shot/P0 hotfix carve-out
  in the workspace constitution §1 and chitin constitution §3.
- Spec 083 (driver governance & telemetry) and the speckit workflow tooling
  itself are out of scope; this gate consumes the governance-decision
  telemetry that spec 083 makes reliable.
