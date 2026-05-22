# Phase 0 Research: SDD Admission Gate

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

The spec carries no `NEEDS CLARIFICATION` markers. Phase 0 fixes the design
decisions the spec's Assumptions left to planning: the path-classification
basis, the session→spec binding mechanism, reconciliation with the existing
gate, and the rollout control.

## Decision 1 — Trigger classification is path-prefix based

**Decision**: Classify each tool call deterministically by the repository path
it writes:
- **spec-authoring** (never gated) — writes under `specs/` and `.specify/`.
- **implementation** (gated) — writes under the code trees: `go/`, `apps/`,
  `libs/`, `scripts/`, `swarm/`, and a code `git commit` or branch creation.
- **neither** (never gated) — reads, searches, and writes under `docs/` or
  other non-code, non-spec paths.

**Rationale**: Path prefix is O(1), deterministic, and auditable — no heuristic
or model judgment in the hot path. It makes the chicken-and-egg structural:
spec-authoring is a different path class, so the gate cannot fire on it.

**Alternatives considered**: classify by intent/content — rejected, non-deterministic
and unauditable; classify every write as gated — rejected, would gate spec
authoring and deadlock.

## Decision 2 — Session→spec binding: explicit declare, with inference fallback

**Decision**: Extend `chitin session` to carry a `spec_id`. An agent binds it
explicitly (`chitin session bind --spec NNN`, or at `session start`). If
unbound, the kernel infers from, in order: `.specify/feature.json`, then the
work-unit's spec lineage, then the git branch name. An inference that resolves
to an existing spec satisfies the gate; the agent may always override.

**Rationale**: Explicit is honest and unambiguous; inference keeps the common
case (operator already on a feature) friction-free so the gate is not a tax on
people following the process.

**Alternatives considered**: explicit-only — rejected, too much friction;
inference-only — rejected, ambiguous when feature.json is stale (as it was
mid-session during 083↔084 authoring).

## Decision 3 — One definition of "has a spec", two enforcement points

**Decision**: A single shared resolver in
`go/execution-kernel/internal/gov/specgate.go` answers "does spec NNN exist".
Both enforcement points call it: the orchestrator hard-gate (US1) and the
kernel warn-gate (US2). The Python `has_spec_kit_entry()` in
`hermes-clawta-bridge.py` is reconciled to call the same resolver (via the
kernel CLI) rather than re-implementing spec lookup.

**Rationale**: FR-012 — one coherent gate, not a fourth parallel check.
Constitution §6 puts the canonical logic in the kernel. Three enforcement
*points* (ticket promotion, dispatch, tool call) is fine; three *definitions*
of "has a spec" is the bug.

**Alternatives considered**: leave `has_spec_kit_entry()` independent —
rejected, drift between definitions; replace it entirely — rejected, it gates a
real surface (ticket promotion) the kernel does not see.

## Decision 4 — The gate fires once per session, on first crossing

**Decision**: The kernel tracks a per-session boolean — "has this session made
an implementation-class mutation yet". The gate evaluates the spec binding on
the **first** such mutation and emits at most one `spec_missing` decision per
session. Subsequent implementation mutations in the same session are not
re-evaluated.

**Rationale**: One decision per session keeps the telemetry one-row-per-gap and
the enforce-mode deny a single, comprehensible event — not a deny on every
edit. Matches the spec's "first source mutation" language (FR-004).

**Alternatives considered**: evaluate every mutation — rejected, noisy
telemetry and a maddening repeated deny.

## Decision 5 — observe|enforce is a policy setting, default observe

**Decision**: A single `chitin.yaml` setting controls the mode. Default is
`observe`. `enforce` is set deliberately by the operator after reviewing US2
telemetry. The mode is read per-evaluation so a change takes effect without a
kernel redeploy.

**Rationale**: FR-009 — observe-before-enforce. A live-readable setting avoids
coupling a policy change to the (spec-083-tracked) redeploy pipeline.

**Alternatives considered**: compile-time mode — rejected, needs a redeploy to
flip; per-driver modes — rejected, unnecessary for v1.

## Decision 6 — Escape hatch reuses the existing one-shot/P0 carve-out

**Decision**: The operator escape hatch is the existing one-shot/P0 hotfix
carve-out (workspace constitution §1, chitin constitution §3) — surfaced as an
explicit, recorded marker on the session. Each use writes an
`escape-hatch-use` governance decision.

**Rationale**: Do not invent a second override scheme; the constitution already
defines the hotfix exception. Recording every use keeps it honest.

**Alternatives considered**: a new bypass flag — rejected, duplicates the
constitution's carve-out and risks an unaudited backdoor.

## Decision 7 — Gate faults fail toward observe

**Decision**: If the spec resolver errors or is unavailable, the gate degrades
to `observe` for that evaluation — record a `spec_gate_fault` decision, allow
the action. It never denies on its own failure.

**Rationale**: FR-013, SC-007 — a governance-tooling fault must never cause a
work outage. Mirrors the kernel's telemetry-is-non-authoritative principle
(spec 070 FR-008).

**Alternatives considered**: fail-closed (deny on fault) — rejected, a resolver
bug would halt all implementation work.

## Decision 8 — Orchestrator hard-gate sits at dispatch admission

**Decision**: The orchestrator checks work-unit spec lineage at **dispatch
admission** — before a `WorkUnitWorkflow` is started. An unresolved lineage
refuses dispatch and records the refusal on the run (visible in tick
telemetry / the run's node status).

**Rationale**: Refusing before dispatch wastes no worktree, driver budget, or
agent time. The orchestrator already computes per-node readiness on each tick —
the spec-lineage check is one more readiness predicate.

**Alternatives considered**: gate after worktree setup — rejected, burns setup
cost on work that will be refused.

## Cross-references

- **Constitution §3** — the spec-kit promotion gate this feature formalizes.
- **Spec 020 `sdd-tdd-enforcement`** — related enforcement; reconcile, do not duplicate.
- **Spec 083** — provides the queryable governance-telemetry sink the
  `spec_missing` decision lands in.
- **`has_spec_kit_entry()`** — `swarm/workflows/hermes-clawta-bridge.py`, the
  existing ticket-promotion check, reconciled per Decision 3.
