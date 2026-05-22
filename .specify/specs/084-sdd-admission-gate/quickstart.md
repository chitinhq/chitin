# Quickstart: Working Under the SDD Admission Gate

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

How to work with the gate in place, and how to verify it behaves.

## The normal flow (no friction)

1. **Specify first** — `/speckit-specify` creates `specs/NNN-slug/spec.md`.
   Authoring it touches only `specs/` / `.specify/` — the gate never fires.
2. **Bind your session** — declare what you are implementing:
   ```bash
   chitin session bind --spec NNN
   ```
   (Or rely on inference: an up-to-date `.specify/feature.json`, the work
   unit's lineage, or the branch name.)
3. **Implement** — edits under `go/`, `apps/`, etc. now resolve to an existing
   bound spec; the gate passes silently.

## What the gate does to spec-less work

- **Observe mode (default)**: the first source-code edit in an unbound session
  produces a `spec_missing` governance decision — work continues. Review the
  decisions to see how often the process is skipped.
- **Enforce mode**: that first edit is denied with a message telling you to run
  `/speckit-specify` then `chitin session bind --spec NNN`.

## Verify the gate (acceptance probe)

```bash
# US2 — observe: an unbound session crossing into implementation is recorded.
#   In a fresh session with NO spec bound, edit a source file (e.g. go/...),
#   then check the central telemetry for the decision:
chitin telemetry query --kind spec_missing --session <session_id>   # expect 1 row

# Negative — spec-authoring must NOT trigger:
#   In a fresh session, create specs/999-probe/spec.md only.
chitin telemetry query --kind spec_missing --session <session_id>   # expect 0 rows

# US1 — orchestrator hard-gate:
#   Submit a work unit with no spec lineage; confirm dispatch is refused
#   and the refusal is visible in the run's node status / tick telemetry.

# US3 — enforce:
#   Set the mode to enforce, repeat the US2 probe; the edit is denied with
#   actionable guidance. Then confirm a declared escape-hatch use proceeds
#   and is recorded as `escape-hatch-use`.
```

## Verdict rules

- **pass** — implementation work has a resolvable bound spec; no
  `spec_missing` decision.
- **observed gap** — `spec_missing` recorded, work allowed (observe mode).
- **blocked** — `spec_missing` recorded, work denied (enforce mode).
- **never triggers** — reads, searches, `docs/` edits, and all `specs/` /
  `.specify/` writes.

## Mode control

```bash
# inspect / change the rollout posture (chitin.yaml setting):
chitin policy show sdd-gate.mode          # observe | enforce
# escalate only after observe-mode telemetry shows an acceptable blast radius.
```
