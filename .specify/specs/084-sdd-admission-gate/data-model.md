# Phase 1 Data Model: SDD Admission Gate

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

This feature ships no new datastore. The model is the admission state: what a
tool call classifies as, what a session is bound to, and the decision the gate
emits.

## Entities

### Tool Call Classification

The deterministic verdict computed for every governed tool call (Decision 1).

| Class | Trigger? | Paths |
|---|---|---|
| `spec-authoring` | never | `specs/**`, `.specify/**` |
| `implementation` | yes (first per session) | `go/**`, `apps/**`, `libs/**`, `scripts/**`, `swarm/**`; code `git commit`; branch creation |
| `neither` | never | reads, searches, `docs/**`, other non-code writes |

**Invariant INV-001**: classification is a pure function of the tool call's
target path/action — same input, same class, no heuristic.

### Spec Reference / Binding

A link from a Session or Work Unit to a spec.

- **spec_id** — e.g. `084` / `084-sdd-admission-gate`.
- **resolvable** — `true` iff `specs/<spec_id>-*/spec.md` exists.
- **source** — how it was bound: `explicit`, or inferred from `feature.json`
  / work-unit lineage / branch (Decision 2).

A binding to a spec that is later deleted or renamed becomes *unresolvable*
(spec edge case: stale lineage).

### Session

A bounded run of agent work (extends the existing `chitin session`).

- **session_id** — existing.
- **bound specs** — zero or more Spec References (a session may span features).
- **crossed_implementation** — per-session boolean; `false` until the first
  `implementation`-class tool call, then `true` (Decision 4 — gate fires once).
- **escape_hatch** — set when the operator declares a one-shot/P0 hotfix.

State transition:
```text
crossed_implementation:  false ──(first implementation tool call)──▶ true
  └─ on that transition, the gate evaluates the bound specs once.
```

### Work Unit

An orchestrator-dispatched unit of work (existing entity).

- **spec lineage** — a Spec Reference; *resolvable* or not.
- US1: an unresolvable lineage ⇒ dispatch refused.

### Enforcement Mode

The gate's posture, a `chitin.yaml` setting read per-evaluation (Decision 5).

| Mode | On a triggering mutation with no resolvable bound spec |
|---|---|
| `observe` (default) | record `spec_missing`, **allow** |
| `enforce` | record `spec_missing`, **deny** with guidance |

### Governance Decision — gate variants

The records this gate emits into the central telemetry sink (spec 083).

| `rule_id` / kind | When | Verdict |
|---|---|---|
| `spec_missing` | first implementation mutation, no resolvable bound spec | allow (observe) / deny (enforce) |
| `spec_gate_fault` | the spec resolver errored | allow (fail-toward-observe, Decision 7) |
| `escape-hatch-use` | operator one-shot/P0 override used | allow, recorded |

All carry the standard attributed `driver`/`agent`, `session_id`, `ts`.

## Relationships

```text
Tool Call ──classified as──▶ Tool Call Classification
   │
   └─ if `implementation` AND session.crossed_implementation false:
        Session ──bound specs──▶ Spec Reference ──resolvable?──┐
                                                               │
        Enforcement Mode ──────────────────────────────────────┤
                                                               ▼
                                              Governance Decision (spec_missing)

Work Unit ──spec lineage──▶ Spec Reference ──unresolvable──▶ dispatch refused (US1)
```
