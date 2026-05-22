---
description: "Task list for Agent-Runtime Cron Audit"
---

# Tasks: Agent-Runtime Cron Audit

**Input**: Design documents from `specs/082-agent-cron-audit/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, quickstart.md

**Tests**: No code-test tasks — this feature ships no code. Verification is the
spec's acceptance scenarios, run against the live registries (the `Verify`
tasks below).

**Organization**: Tasks are grouped by user story. US1 (the decision log) is
the MVP; US2 and US3 consume it.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files/registries, no dependencies)
- **[Story]**: US1 / US2 / US3
- Paths are registry files (`~/.hermes/cron/jobs.json`,
  `~/.openclaw/cron/jobs.json`) and spec artifacts under
  `specs/082-agent-cron-audit/` — this feature has no `src/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Make every later mutation backed-up and the snapshot honest.

- [x] T001 [P] Snapshot `~/.hermes/cron/jobs.json` to `~/.hermes/cron/jobs.json.bak-082-audit-<timestamp>`
- [x] T002 [P] Snapshot `~/.openclaw/cron/jobs.json` to `~/.openclaw/cron/jobs.json.bak-082-audit-<timestamp>`
- [x] T003 Re-confirm live registry counts against the dated audit snapshot — 30 Hermes jobs, 9 OpenClaw jobs — via `hermes cron list` and registry inspection; record any drift in `specs/082-agent-cron-audit/research.md`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Resolve the two open items US2/US3 execution depends on.

**⚠️ CRITICAL**: T004–T005 block the *execution* tasks of US2 and US3 (not the US1 decision log, which is documentation-only).

- [x] T004 Confirm the exact disable/delete subcommands for both runtimes (Hermes `hermes cron`, the OpenClaw cron path) and record them in `specs/082-agent-cron-audit/quickstart.md` (resolves research.md open item 1)
- [ ] T005 Confirm the restart procedure for each owning agent (hermes, clawta, glm-agent, openclaw-core) used by the restart-durability check; record it in `specs/082-agent-cron-audit/quickstart.md`

**Checkpoint**: Safe-mutation runbook is concrete and executable.

---

## Phase 3: User Story 1 - Complete classified cron inventory (Priority: P1) 🎯 MVP

**Goal**: Every one of the 39 jobs has a complete, classified decision record.

**Independent Test**: Open `decision-log.md`; confirm 39 records, each with all required fields and exactly one disposition + rationale. No registry is mutated.

### Implementation for User Story 1

- [x] T006 [P] [US1] Create `specs/082-agent-cron-audit/decision-log.md` with the record schema from data-model.md as the table header
- [x] T007 [US1] Inventory all 30 Hermes jobs from `~/.hermes/cron/jobs.json` into `decision-log.md` — id, name, owning agent, schedule, enabled/paused state, last status, description
- [x] T008 [US1] Inventory all 9 OpenClaw jobs from `~/.openclaw/cron/jobs.json` into `decision-log.md` — same fields
- [x] T009 [US1] Determine each job's registration source (manual vs agent-managed + path) and record it in `decision-log.md`; resolve the `glm-agent` `clawta-stale-worker-watchdog` triplicate source specifically (research.md Decision 2)
- [x] T010 [US1] Assign a disposition (keep/disable/delete/migrate), a non-empty written rationale, and a `concern` (swarm-infra / personal, per FR-013) to each of the 39 jobs in `decision-log.md`
- [x] T011 [US1] For every `migrate` job, name its spec-070/076 replacement workflow and its proven/unproven status in `decision-log.md`
- [x] T012 [US1] Cross-reference spec 081's job inventory; set `spec_081_crossref` on every job present in both layers (`kanban-pull-loop`, `clawta-poller`) and confirm a consistent direction
- [x] T013 [US1] Assign each job an execution phase (A/B/C) in `decision-log.md`; verify the 39 records partition cleanly — every job once, no gaps, no fourth state (INV-001)
- [x] T014 [US1] Verify US1: `decision-log.md` holds 39 records, each field populated, exactly one disposition + rationale per job (SC-001)

**Checkpoint**: The decision log is complete — the audit's primary deliverable. US2/US3 can now execute against it.

---

## Phase 4: User Story 2 - Cull the zero-regret cruft (Priority: P2)

**Goal**: Duplicates removed, the broken job disabled — durably.

**Independent Test**: OpenClaw registry holds exactly one `clawta-stale-worker-watchdog`; no job is enabled-and-perpetually-failing; restart the affected agents and confirm no deleted job reappears.

### Implementation for User Story 2

- [x] T015 [US2] De-duplicate `clawta-stale-worker-watchdog` in `~/.openclaw/cron/jobs.json` — delete the 3 `glm-agent` duplicate registrations, keep one — per the quickstart.md disable→delete protocol
- [ ] T016 [US2] Correct the `glm-agent` registration source identified in T009 so the watchdog duplicates cannot re-appear; restart `glm-agent` and confirm durability (SC-004)
- [x] T017 [US2] Disable the failing `board-watchdog` in `~/.hermes/cron/jobs.json` (it errors every run and is orchestrator-superseded — disable, not fix; plan.md §4 rationale)
- [x] T018 [US2] Execute every other execution-phase-B `delete` disposition from `decision-log.md`, following the disable→observe→delete protocol; back up first
- [ ] T019 [US2] Verify US2: OpenClaw registry shows exactly one `clawta-stale-worker-watchdog` (SC-002); no job is enabled with a perpetual error status (SC-003); each delete survived an agent restart (SC-004)

**Checkpoint**: The cruft is gone; the layer is smaller and honest.

---

## Phase 5: User Story 3 - Retire the orchestrator-superseded crons (Priority: P3)

**Goal**: Superseded board/dispatch crons retired only behind a proven workflow.

**Independent Test**: Pick one superseded cron with a proven replacement workflow; confirm coverage, disable the cron, confirm the work still happens exactly once from the workflow.

### Implementation for User Story 3

- [ ] T020 [US3] For each `migrate` job, confirm whether its spec-070/076 replacement workflow is proven; update the proven/unproven status in `decision-log.md`
- [ ] T021 [US3] For each `migrate` job whose replacement is proven, disable the cron in the same change that confirms the workflow owns the work — no window of double-run, none of neither (FR-009, INV-002)
- [ ] T022 [US3] For each `migrate` job whose replacement is not yet proven, leave the cron pending and record the coverage gap in `decision-log.md` — do not delete into a gap (US3 AS2)
- [ ] T023 [US3] Reconcile with spec 081 at execution time — confirm no logical job (`kanban-pull-loop`, `clawta-poller`) runs from both the agent-cron and systemd layers (SC-007)
- [ ] T024 [US3] Verify US3: no logical job observed running from both a cron and a workflow (SC-005); every executed migration named its replacement workflow and ran only after it was proven (SC-006)

**Checkpoint**: Every superseded cron is either retired behind a proven workflow or explicitly pending with its gap recorded.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T025 [P] Update `decision-log.md` with the final executed state of every job; confirm INV-001 — all 39 jobs are kept, disabled, or deleted, none unclassified
- [ ] T026 [P] Run the `quickstart.md` acceptance verification end-to-end (SC-002 → SC-005)
- [ ] T027 Commit the spec 082 artifacts (spec.md, plan.md, research.md, data-model.md, quickstart.md, decision-log.md, tasks.md) via PR from a worktree

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately.
- **Foundational (Phase 2)**: Depends on Setup. Blocks the *execution* tasks of US2/US3 (T015–T024), not the US1 documentation tasks.
- **US1 (Phase 3)**: Depends on Setup. Documentation-only — zero registry mutation.
- **US2 (Phase 4)**: T015–T017 (dedup + disable board-watchdog) depend only on Foundational — the defects are known now. T018 depends on US1's `decision-log.md`.
- **US3 (Phase 5)**: Depends on US1 (`decision-log.md` supplies the migrate mapping) and on Foundational.
- **Polish (Phase 6)**: Depends on all desired stories.

### User Story Dependencies

- **US1 (P1)**: Independent — the decision log. The MVP.
- **US2 (P2)**: Headline actions (dedup, disable the broken job) are independent of US1; T018 (other deletes) consumes the decision log.
- **US3 (P3)**: Depends on US1 — it executes the `migrate` dispositions the decision log records. Not independent of US1 by design (an audit's actions follow its findings).

### Parallel Opportunities

- T001 ∥ T002 — different registry files.
- T006 ∥ T025 ∥ T026 are marked [P] but belong to different phases — run [P] only within their own phase.
- Within US1, T007 and T008 both write `decision-log.md` — **not** parallel.
- US2's T015–T017 can run in any order (different registries / different jobs).

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 Setup → Phase 2 Foundational → Phase 3 US1.
2. **STOP and VALIDATE**: `decision-log.md` has all 39 classified records.
3. The decision log alone is a shippable deliverable — it makes the agent-cron
   layer legible and is operator-reviewable before any mutation.

### Incremental Delivery

1. Setup + Foundational → runbook ready, registries backed up.
2. US1 → the decision log → **operator reviews and approves dispositions**.
3. US2 → zero-regret cull → verify → the layer is smaller.
4. US3 → gated retirement, one `migrate` job at a time as its workflow proves.

### Operator gate

Per spec Assumptions, the operator approves the dispositions before US2/US3
execute. US1 is safe to run unattended; US2/US3 mutate live registries and
should run only with that approval.

---

## Notes

- [P] = different files/registries, no dependency.
- The decision log (`decision-log.md`) is the audit's primary artifact; every
  US2/US3 task reads its dispositions.
- Every registry mutation is backup-first and disable-before-delete
  (quickstart.md).
- A delete is complete only after the restart-durability check passes.
- `board-watchdog` is dispositioned `disable`, not `fix` — it is
  orchestrator-superseded, so fixing the SQLite error would be wasted work.
