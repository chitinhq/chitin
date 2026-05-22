# Contracts: SDD Admission Gate

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

The behavioural promises this feature establishes for its consumers
(operators, the orchestrator, the swarm bridge, the telemetry readers).

## C1 — Tool-call classification

The gate classifies every governed tool call into exactly one class.

- The classification is a **pure function of the target path/action** —
  deterministic, no heuristic (INV-001).
- `specs/**` and `.specify/**` writes MUST classify as `spec-authoring` and
  MUST NEVER trigger the gate (FR-005).
- The implementation path set MUST be explicit and auditable; a path not in the
  implementation set and not in the spec-authoring set classifies as `neither`
  and does not trigger.

## C2 — `spec_missing` governance decision

Emitted on the first `implementation`-class tool call in a session with no
resolvable bound spec.

- MUST be emitted **at most once per session** (Decision 4).
- MUST carry the standard attributed actor, `session_id`, `ts`, and
  `rule_id: spec_missing`.
- In `observe` mode the decision's verdict is *allow*; in `enforce` mode it is
  *deny*.
- A consumer counting `spec_missing` decisions over a window gets the true rate
  of spec-less implementation (SC-003).

## C3 — Session→spec binding

- A session MUST be bindable to one or more spec references via an explicit
  command; an unbound session resolves via inference
  (`feature.json` → work-unit lineage → branch) (Decision 2).
- A binding is *resolvable* iff `specs/<spec_id>-*/spec.md` exists. The gate
  checks **existence only** — never quality (FR-008).

## C4 — Orchestrator dispatch admission

- A work unit with a resolvable spec lineage dispatches normally.
- A work unit with no resolvable spec lineage MUST be refused **before**
  worktree setup, and the refusal MUST be visible in the run's node status /
  tick telemetry (FR-001, FR-002, Decision 8).

## C5 — Enforcement mode & failure posture

- The `observe | enforce` mode is a `chitin.yaml` setting, read per-evaluation;
  a change takes effect with no kernel redeploy (Decision 5).
- In `enforce`, a denied mutation MUST return an actionable message naming the
  fix (`/speckit-specify` + `chitin session bind`) (FR-010).
- A spec-resolver fault MUST degrade to `observe` for that evaluation, emit a
  `spec_gate_fault` decision, and allow the action — the gate MUST NOT deny on
  its own failure (FR-013, Decision 7).
- The operator escape hatch MUST allow a declared one-shot/P0 mutation and
  record an `escape-hatch-use` decision (FR-011).

## C6 — One definition of "has a spec"

- The kernel warn-gate, the orchestrator hard-gate, and the swarm bridge's
  `has_spec_kit_entry()` MUST all resolve "does spec NNN exist" through the
  **same** kernel resolver — no divergent re-implementations (FR-012,
  Decision 3).
