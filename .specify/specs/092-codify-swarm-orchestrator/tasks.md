# Tasks: Codify the swarm-is-orchestrator architecture (constitution §7)

**Feature**: 092-codify-swarm-orchestrator
**Branch**: `feat/092-codify-swarm-orchestrator`
**Plan**: [plan.md](plan.md) · **Spec**: [spec.md](spec.md) · **Contract**: [contracts/canonical-section-7.md](contracts/canonical-section-7.md)

## Overview

Total tasks: **4** (2 implementation + 1 verification + 1 PR-prep)
Estimated effort: **~10 minutes** end-to-end. Single-file edit.

This is a minimal task set because the feature reduces to one text edit. The §7 text is content-frozen in `contracts/canonical-section-7.md` (ratified through multi-agent review); the tasks just transcribe it into the constitution and verify the success criteria.

## Phase 1: Setup

*(No setup required — the constitution file already exists; the canonical §7 text is already in `contracts/`.)*

## Phase 2: Foundational

*(No foundational prereqs — the §7 amendment is independent of in-flight specs 087-091.)*

## Phase 3: User Story 1 — All drivers read the same architectural ground truth (P1)

**Story goal**: After this story completes, `.specify/memory/constitution.md` contains §7 with the canonical text, AND the 2026-05-22 amendment HTML comment is prepended. Future spec-kit Constitution Check invocations evaluate against §7 alongside §1-§6.

**Independent test**: Quickstart commands SC-001 through SC-006 all pass against the working tree. (`grep` returns expected counts; `awk`+`grep` for vendor-name leak returns 0.)

### Tasks

- [ ] T001 [US1] Apply the 2026-05-22 amendment HTML comment block (from `specs/092-codify-swarm-orchestrator/contracts/canonical-section-7.md`, "## The amendment HTML comment" section) by prepending it directly below the existing `# Chitin Repo Constitution Overlay` heading at the top of `.specify/memory/constitution.md`. The new amendment comment goes ABOVE the existing 2026-05-20 amendment comment (newest first). Preserve all surrounding whitespace and the existing 2026-05-20 amendment block exactly.

- [ ] T002 [US1] Append the §7 section (from `specs/092-codify-swarm-orchestrator/contracts/canonical-section-7.md`, "## The §7 section" code-fenced markdown) to the end of `.specify/memory/constitution.md`. Append it after the existing §6 ("## 6. Swarm tooling is the exception, not the pattern") with a blank line between §6 and the new `## 7. The swarm is the orchestrator` heading. Transcribe the canonical text VERBATIM — do not rewrite, paraphrase, or "improve" any wording. Preserve the table formatting (pipe alignment) and the ✅/❌ Unicode glyphs.

## Phase 4: User Story 2 — The implementation gate is empirically auditable (P1)

**Story goal**: The constitutional gate stated in §7 is queryable. The follow-up "no-driver-bypass invariant test" spec (NOT in this PR) will turn this into a Sentinel property test. For THIS spec, the deliverable is that the gate's text is precise enough to be checked — verified by the `grep`/`awk` commands in `quickstart.md`.

**Independent test**: Quickstart command SC-003 returns 1 (the implementation MUST sentence is present verbatim). The wording is mechanically testable, not paraphrased prose.

### Tasks

- [ ] T003 [US2] Run the quickstart verification block from `specs/092-codify-swarm-orchestrator/quickstart.md` ("## Run-all" section) against the working tree. Confirm SC-001 returns 1, SC-002 enumerates all 7 drivers (each ≥1), SC-003 returns 1, SC-004 returns ≥1, SC-005 returns 1, SC-006 returns 0. SC-007 stays PENDING until the companion research PR lands (acceptable for this spec's PR). SC-008 is a post-merge operator check (out of scope for this task).

## Phase 5: Polish & Cross-Cutting

- [ ] T004 Stage the constitution change, commit on `feat/092-codify-swarm-orchestrator` with a commit message referencing spec 092 and the multi-agent ratification, then push and open the PR against `main`. PR body MUST link the spec, the canonical contract, and explicitly note that the companion deep-research report lands as a separate PR (per Ares's "constitutional amendments need crisp diffs" guidance).

## Dependency graph

```
T001 → T002 → T003 → T004
 (HTML comment) → (§7 section) → (verify SCs) → (commit, push, PR)
```

No parallelism opportunities — every task touches the same file or depends on the prior task's output. Linear execution.

## Parallel execution examples

*(None — see dependency graph.)*

## Implementation strategy

**MVP scope**: T001 + T002 + T003 alone constitute a working amendment locally. T004 ships it.

There is no incremental delivery — the amendment is atomic. Either §7 lands as ratified, or it doesn't land at all. Partial states (e.g., HTML comment without §7 prose, or vice versa) leave the constitution in an inconsistent state and should never be committed.

## Format validation

All tasks above conform to the required checklist format:
- ✅ Every task starts with `- [ ]`
- ✅ Every task has a sequential ID (T001-T004)
- ✅ User-story-phase tasks have `[US1]` or `[US2]` labels; Polish-phase task (T004) has no story label
- ✅ Every task includes a file path or explicit file reference
- ✅ Descriptions are specific enough to execute without additional context
