# Implementation Plan: Spec-Kit Adapter

**Branch**: `077-spec-kit-adapter` | **Date**: 2026-05-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/077-spec-kit-adapter/spec.md`

## Summary

Specs arrive in different kits — GitHub spec-kit, OpenSpec, superpowers —
each with its own files and conventions. The Spec-DAG Scheduler (spec 076)
consumes exactly one shape: the normalized Work-Unit DAG. This spec builds
the bridge: a single `SpecKitAdapter` interface, one concrete adapter per
kit, an adapter registry that detects which kit a repo uses, and a
spec→DAG compile activity that turns a kit's artifacts into the 076 DAG.
Adding a kit is adding one adapter — **zero** changes to the scheduler or
orchestrator core (FR-002, SC-001).

Compilation is a pure, deterministic, side-effect-free transformation:
spec files in, in-memory DAG out. It runs inside a Temporal activity (the
scheduler's compile step, 076 FR-001) — never in workflow code, never
writing to a chain or a board. The adapter draws node capability tags from
spec 075's closed capability taxonomy and produces nodes/edges in spec
076's normalized schema; where a kit's artifacts are ambiguous it emits
`NEEDS CLARIFICATION` rather than inventing an edge or a tag.

## Technical Context

**Language/Version**: Go 1.23+ — matches the Chitin Kernel and the spec-070 orchestrator; one language across kernel + orchestrator + adapter.

**Primary Dependencies**: spec 076's normalized DAG schema (`go/orchestrator/dag/`) — the output contract; spec 075's capability taxonomy — the closed vocabulary for node capability tags; the Temporal Go SDK — compilation is invoked as a Temporal activity from the scheduler's compile step.

**Storage**: None new. Adapters read spec files from a repo on disk and emit an in-memory DAG; nothing is persisted by this layer. The DAG's durability is the scheduler's concern (spec 076).

**Testing**: `go test`; table-driven tests over fixture spec directories, one fixture tree per kit (`adapter/testdata/speckit/`, `adapter/testdata/openspec/`, `adapter/testdata/superpowers/`); a fixture with a known ambiguous dependency to verify the `NEEDS CLARIFICATION` path; a known before/after spec pair to verify the DAG diff.

**Target Platform**: Linux, single box (chimera-ant); self-hosted.

**Project Type**: A Go package within the spec-070 orchestrator module — not a standalone service.

**Performance Goals**: Compilation is invoked once per spec change, not in a hot loop. The goal is **determinism + honest ambiguity-marking**, not throughput; the same spec files always compile to the same DAG.

**Constraints**: Compilation MUST be pure and deterministic — no `time.Now`, no network, no filesystem writes, no chain writes. Adding a kit MUST NOT touch the scheduler (076) or orchestrator core. Capability tags MUST come from the spec-075 taxonomy; an unmappable task is `NEEDS CLARIFICATION`, never an invented tag.

**Scale/Scope**: Three concrete adapters delivered (spec-kit, OpenSpec, superpowers); chitin's own `specs/` tree (~70+ specs) is the primary corpus the spec-kit adapter dogfoods against.

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

| Principle | Assessment |
|-----------|------------|
| §1 Side-effect boundary | PASS — the adapter is a read-only pure transform: it reads spec files and returns an in-memory DAG. It writes no chain events, mutates no kanban state, produces no side effects. Nothing to route through the kernel because there is nothing to gate. |
| §2 Branch & worktree (amended: always workers + worktrees) | PASS — compilation only *reads* spec files; it produces no branch work and edits nothing. The work units the emitted DAG describes run in worktrees, but that is the scheduler's (076) enforcement, not this layer's. |
| §3 Spec-kit promotion gate | PASS — 077 has `spec.md` + this `plan.md`; `tasks.md` follows. |
| §4 Tracked installers | N/A — this is a library package compiled into the orchestrator binary, not a script that runs standalone on the operator's box. No installer to ship. |
| §5 Board-aware scripts | N/A — the adapter does not touch the kanban; it has no `--board` surface. |
| §6 Swarm tooling is the exception | PASS — the adapter is genuine kernel-adjacent infra (a driver/compiler layer); it lives under `go/orchestrator/adapter/`, not `swarm/`. |

No violations → Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/077-spec-kit-adapter/
├── plan.md          # This file
├── research.md      # Phase 0 — kit-format survey (spec-kit / OpenSpec / superpowers conventions)
├── data-model.md    # Phase 1 — SpecKitAdapter / Kit / Registry / TaskContext / DAGDiff entities
├── quickstart.md    # Phase 1 — compile chitin's own specs/ into a DAG, run it through the 076 scheduler
└── tasks.md         # Phase 2 — /speckit-tasks output (not created here)
```

### Source Code (repository root)

```text
go/orchestrator/adapter/
├── adapter.go            # the SpecKitAdapter interface — Detect + Compile
├── registry.go           # adapter registry + per-repo kit detection (FR-008)
├── compile.go            # the spec→DAG compile Temporal activity (pure transform; FR-001, FR-003)
├── diff.go               # DAG-diff logic — added / removed / changed nodes (FR-012)
├── context.go            # Task Context extraction — FR refs, file paths, spec/plan excerpts (FR-005)
├── constitution.go       # canonical constitution → per-kit location projection (FR-013)
├── errors.go             # malformed-artifact + dangling-reference failures (FR-010, FR-011)
├── speckit/              # GitHub spec-kit adapter — .specify/specs/NNN-name/ (FR-004)
├── openspec/             # OpenSpec adapter — openspec/changes/<name>/ (FR-006, FR-007)
├── superpowers/          # superpowers adapter — skill-driven plans (FR-006)
└── testdata/             # fixture spec trees, one per kit + ambiguity + before/after pair

go/orchestrator/dag/      # spec 076 — the normalized Work-Unit DAG schema (consumed, not owned here)
```

**Structure Decision**: A new `go/orchestrator/adapter/` package inside the
spec-070 orchestrator module, beside `go/orchestrator/dag/` (spec 076) and
`go/orchestrator/driver/` (spec 075). Spec compilation is a **pure,
deterministic, side-effect-free transformation** — spec files in, in-memory
DAG out — invoked from a Temporal **activity** (`compile.go`), never from
workflow code: a parser doing filesystem reads belongs in an activity, and
keeping it there keeps the scheduler workflow replay-clean. The interface
and registry live at the package root; each kit is an isolated sub-package
(`speckit`, `openspec`, `superpowers`) so a new kit is a new directory and
nothing else. The DAG schema and capability taxonomy are imported, never
redefined — 076 owns the output contract, 075 owns the capability
vocabulary.

## Phases

- **Phase 0 — Research.** Survey the three kit formats — spec-kit's
  `tasks.md` ordering + `[P]` markers, OpenSpec's `openspec/changes/<name>/`
  proposal/apply/archive + ADDED/MODIFIED/REMOVED deltas, superpowers'
  skill-driven plans. Confirm the 076 DAG schema and 075 taxonomy as fixed
  inputs. Exit: each kit's dependency-bearing artifacts identified.
- **Phase 1 — Foundational.** The `SpecKitAdapter` interface, the registry
  + detection, the compile activity, the DAG-diff function, the
  malformed-artifact and dangling-reference failure paths. Exit: an adapter
  can be registered and a compile activity invoked.
- **Phase 2 — Spec-kit adapter (the P1 slice).** The GitHub spec-kit
  adapter — compile `.specify/specs/NNN-name/`, one node per `tasks.md`
  task, edges from ordering + `[P]`, capability + priority from metadata,
  task-context extraction. Exit: chitin's own specs compile to a DAG the
  076 scheduler runs (SC-002).
- **Phase 3 — Second kit (the P2 slice).** The OpenSpec adapter — parse
  `openspec/changes/<name>/`, preserve ADDED/MODIFIED/REMOVED change-kind
  as node metadata. Exit: two kits compile through one interface with zero
  scheduler diff (SC-001, SC-003).
- **Phase 4 — Detection + honest ambiguity (the P3 slice).** Multi-kit
  detection with explicit-choice-on-ambiguity, `NEEDS CLARIFICATION` on
  ambiguous dependencies and unmappable capabilities, constitution
  projection into each kit's location. Exit: every ambiguity surfaces, zero
  invented edges (SC-004).
- **Phase 5 — Polish.** Fixture-based test suite per kit; re-run the
  Constitution Check.

## Complexity Tracking

None — no constitution violations to justify.
