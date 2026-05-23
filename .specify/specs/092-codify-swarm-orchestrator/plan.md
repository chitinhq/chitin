# Implementation Plan: Codify the swarm-is-orchestrator architecture

**Branch**: `feat/092-codify-swarm-orchestrator` | **Date**: 2026-05-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/092-codify-swarm-orchestrator/spec.md`

## Summary

The §7 amendment was drafted, edited, and ratified across multiple turns in conversation with Ares (hermes/GPT-5.5) and Clawta (openclaw/GLM-5.1) reviewing, and the operator approving final wording. The canonical text is preserved in `contracts/canonical-section-7.md` and constitutes the implementation contract. Implementation is a single-file edit to `.specify/memory/constitution.md` — prepending a 2026-05-22 amendment HTML comment and appending the §7 section.

## Technical Context

**Language/Version**: Markdown — `.specify/memory/constitution.md`. No code.
**Primary Dependencies**: spec-kit (`/speckit-plan` reads the constitution as a gate via the Constitution Check section). No runtime dependencies.
**Storage**: N/A (single text file in repo).
**Testing**: 8 success criteria from `spec.md` as `grep` / `test -f` invocations against the merged constitution file. Verified post-merge via `quickstart.md`.
**Target Platform**: any operator box running chitin; the constitution is read by all spec-kit skill invocations across the project.
**Project Type**: meta-documentation (governance gate). Single-file change.
**Performance Goals**: zero performance cost.
**Constraints**: spec-kit Constitution Check passes; no vendor names in §7 body (citations live in `docs/strategy/`); preserves the existing HTML-comment amendment convention.
**Scale/Scope**: ~150 lines added to a ~85-line file. One PR. No code edits.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Evaluating the proposed §7 amendment against the existing chitin constitution overlay (§1-§6):

| § | Existing rule | §7 interaction |
|---|---|---|
| 1 | Side-effect boundary — kernel is the only chain-writer | **Affirms.** §7's third load-bearing part literally restates the kernel-gates-every-tool-call rule. |
| 2 | Workers + worktrees discipline | **Affirms + builds on.** §7's "hierarchical, not flat" paragraph names the orchestrator as the supervisor that dispatches each work-unit to its own worktree (per spec 070 FR-013/14). |
| 3 | Spec-kit promotion gate | **Strengthens.** Implementation now requires not only a spec but specifically a DAG-resolved or ad-hoc orchestrator work-unit (the implementation gate). |
| 4 | Tracked installers | **Untouched.** §7 doesn't address installer-shipping. |
| 5 | Board-aware scripts | **Supersedes** where §5 describes kanban as a live driving surface (the kanban substrate is end-of-life per spec 087; §7 names this transition explicitly in its supersession block). |
| 6 | Swarm tooling is the exception | **Supersedes** the "`swarm/` is transitional housing" framing — `swarm/` is operator-side glue, not the swarm itself. The swarm is the orchestrator. |

**Initial gate verdict**: 4/6 affirm, 2/6 superseded (with §7's supersession block naming them explicitly per Ares's edit). PASS — no unjustified violations.

**Post-design recheck (after Phase 1)**: §7 as preserved in `contracts/canonical-section-7.md` matches the gate-pass evaluation above. PASS retained.

## Project Structure

### Documentation (this feature)

```
specs/092-codify-swarm-orchestrator/
├── spec.md                          # The user-facing spec (committed)
├── plan.md                          # This file (/speckit-plan output)
├── research.md                      # Phase 0 — decisions + alternatives
├── data-model.md                    # Phase 1 — the amendment as the entity
├── quickstart.md                    # Phase 1 — verification recipe
├── contracts/
│   └── canonical-section-7.md       # Phase 1 — the canonical §7 text (implementation contract)
├── checklists/
│   └── requirements.md              # committed at /speckit-specify
└── tasks.md                         # Phase 2 (created by /speckit-tasks — NOT here)
```

### Source code (repository root)

The single file changed by this spec's implementation:

```
.specify/memory/
└── constitution.md                  # prepend amendment comment + append §7 (canonical text in contracts/)
```

The chitin-console diagrams and the 2026-05-20 strategy doc are cited by §7 as visual sources of truth; they are NOT modified by this spec (they already say what §7 codifies).

**Structure Decision**: single-file edit. No new packages, no new modules, no new scripts. The "structure" of this feature is the canonical §7 text in `contracts/canonical-section-7.md`, which the implementation phase transcribes verbatim into the constitution.

## Phase 2 Execution Strategy (preview — owned by /speckit-tasks)

Implementation reduces to one or two atomic operations:

1. **Apply the amendment**: prepend the 2026-05-22 HTML comment block; append §7 from `contracts/canonical-section-7.md` to `.specify/memory/constitution.md`.
2. **(Optional)**: run the post-merge verification commands from `quickstart.md` against the working tree to confirm SCs satisfied locally before opening the PR.

A single worktree partition. One commit. One PR.

## Complexity Tracking

No constitution violations. This section is intentionally empty.
