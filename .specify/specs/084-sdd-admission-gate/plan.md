# Implementation Plan: SDD Admission Gate

**Branch**: `main` (worktrees per work unit, constitution §2) | **Date**: 2026-05-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/084-sdd-admission-gate/spec.md`

## Summary

Make the chitin governance gate enforce spec-driven development. Two
enforcement points sharing one definition of "has a spec": the **orchestrator**
hard-refuses to dispatch a work unit with no resolvable spec lineage (US1); the
**kernel** records a `spec_missing` governance decision when an interactive
session makes its first source-code mutation with no bound spec (US2), and —
once the telemetry shows the blast radius — can escalate that record into a
deny (US3). The gate's trigger surface is *implementation*, never
*spec-authoring*, which dissolves the chicken-and-egg. No new agent capability;
this is admission logic plus a session→spec binding.

## Technical Context

**Language/Version**: Go 1.23+ — the kernel gate (`go/execution-kernel`) and
the orchestrator (`go/orchestrator`). Python 3 for reconciliation with the
existing `has_spec_kit_entry()` in `swarm/workflows/hermes-clawta-bridge.py`.

**Primary Dependencies**: the chitin kernel `gate evaluate` path and its
`gov.Decision` record; the `chitin session` mechanism (extended to carry a
spec binding); the orchestrator's work-unit dispatch (`SchedulerWorkflow` /
`WorkUnitWorkflow`); the spec directory `specs/` (symlink → `.specify/specs/`)
and `.specify/feature.json` as the spec-resolution surface.

**Storage**: append-only governance telemetry — the `spec_missing` decision is
a `gov.Decision` written to the central sink (spec 083's queryable interface).
The session→spec binding is session-scoped state; the per-session
"already-crossed-into-implementation" flag is in-memory session state.

**Testing**: `go test ./...` in `go/execution-kernel` and `go/orchestrator` —
unit tests for path classification (the trigger boundary) and for the
observe/enforce decision; integration tests for orchestrator dispatch refusal.

**Target Platform**: the operator's Linux box — the kernel hook path (all
interactive agent sessions) and the orchestrator worker host.

**Project Type**: governance kernel + multi-agent orchestrator.

**Performance Goals**: the classification + binding lookup runs on every
governed tool call; it MUST be O(1)-ish path-prefix work and add no
operator-perceptible latency (SC-005).

**Constraints**: rollout is observe-before-enforce (FR-009); a gate fault fails
toward *observe*, never blocking work (FR-013); the gate is one mechanism with
the existing §3 / `has_spec_kit_entry` check, not a fourth (FR-012); only the
kernel writes the chain (constitution §1).

**Scale/Scope**: 2 enforcement points; 1 shared spec-resolver; ~6 driver
sessions + orchestrator dispatch as the governed surface; 1 new decision type
(`spec_missing`); 1 enforcement-mode setting.

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

| Rule | Assessment |
|---|---|
| §1 Side-effect boundary — only the kernel writes the chain | ✅ The `spec_missing` decision is written by the kernel gate (the sanctioned chain writer). The orchestrator hard-gate is a dispatch decision, not a chain write. |
| §2 Workers + worktrees | ✅ Implementation runs as work units in dedicated worktrees. |
| §3 Spec-kit promotion gate | ✅ This feature **is** the formalization of §3 — it extends §3's principle from ticket promotion to the governance gate. It has its own spec (084). |
| §4 Tracked installers | ✅ No new runtime artifact; the gate is kernel-internal. Any change to `chitin session` ships in-repo. |
| §6 Kernel-local logic belongs in `cmd/`/`internal/`/`libs/`, not `swarm/` | ✅ The new gate logic lands in `go/execution-kernel/internal/gov`. The existing `has_spec_kit_entry()` in `swarm/workflows/` is **reconciled** — its spec-resolution becomes a caller of the kernel's shared resolver, honoring §6's "kernel-local logic in the kernel" direction. |

**No violations.** Complexity Tracking not required.

## Project Structure

### Documentation (this feature)

```text
specs/084-sdd-admission-gate/
├── plan.md              # This file
├── spec.md              # Feature spec
├── research.md          # Phase 0 — 8 design decisions
├── data-model.md        # Phase 1 — work unit, session binding, classification, decision
├── quickstart.md        # Phase 1 — bind-a-session + verify-the-gate runbook
├── contracts/           # Phase 1 — spec_missing decision, session-bind, dispatch admission
└── tasks.md             # Phase 2 — /speckit-tasks (not produced here)
```

### Source Code (repository root)

```text
go/execution-kernel/
├── internal/gov/
│   ├── gate.go              # US2: classify tool call, check session binding, emit spec_missing
│   ├── specgate.go          # NEW — path classification + spec resolver (shared definition)
│   └── decision.go          # US2: the spec_missing decision shape
├── internal/session/        # US2: chitin session carries a bound spec_id
└── cmd/chitin-kernel/        # session bind subcommand surface

go/orchestrator/
└── workflows/ + activities/  # US1: refuse dispatch of a work unit with no spec lineage

swarm/workflows/hermes-clawta-bridge.py   # FR-012: has_spec_kit_entry() reconciled to
                                          #         call the kernel's shared spec resolver
chitin.yaml                               # US3: observe|enforce mode setting
```

**Structure Decision**: The shared spec-resolver and path classification land
in a new `go/execution-kernel/internal/gov/specgate.go` — one definition of
"has a spec / is this implementation", consumed by both the kernel warn-gate
and (via a small interface) the orchestrator hard-gate, and reconciled with the
Python `has_spec_kit_entry()`. Constitution §6 puts this kernel-local logic in
the kernel, not `swarm/`.

## Phasing — mapped to the user stories

- **US1 (P1) — Orchestrator hard-gate.** At work-unit dispatch, resolve the
  unit's spec lineage; refuse and surface if unresolved. Smallest, no
  ambiguity — lineage already exists on work units.
- **US2 (P2) — Kernel observe-gate.** Path classification (`specgate.go`);
  extend `chitin session` with a spec binding; on the first
  implementation-class mutation in an unbound session, emit `spec_missing`.
  Ships in **observe** mode — records, never blocks.
- **US3 (P3) — Enforce escalation.** Add the observe|enforce mode setting; in
  enforce, deny the triggering mutation with actionable guidance; wire the
  operator escape hatch. Gated on US2's telemetry showing an acceptable blast
  radius.

## Complexity Tracking

No constitution violations — section intentionally empty.
