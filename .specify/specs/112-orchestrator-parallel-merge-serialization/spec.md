---
spec_id: 112
title: Orchestrator parallel-merge serialization — eliminate codex-worker PR conflicts
status: Draft
owner: chitinhq
created: 2026-05-24
depends_on:
  - 070
  - 075
  - 076
related:
  - 094
  - 098
  - 107
  - 108
  - 109
---

# Spec 112 — Parallel-merge serialization

## Why

The 2026-05-24 autonomous-loop dogfood produced **17 implementation PRs** from 4 spec dispatches (specs 107, 108, 109 + 109 redo). Of those:

- **5 merged cleanly** on the first auto-merge attempt
- **12 hit merge conflicts** because parallel codex/claudecode workers each authored a complete view of the same file in their own worktree

Concrete clean-merge rate: ~30%. As spec parallelism grows (DAGs with more `[P]` tasks), the rate worsens — each additional parallel worker that touches the same file is one more guaranteed conflict.

The root cause is structural in the current design:

  1. SchedulerWorkflow spawns N parallel WorkUnitWorkflows for `[P]` tasks (spec 076)
  2. Each WorkUnitWorkflow gets its own worktree from a fresh `git worktree add` against `main` (spec 070)
  3. Each driver runs blind to its siblings — sees `main` as it was at dispatch time
  4. All N workers commit + push their branches + open PRs in parallel
  5. The first PR to merge wins; subsequent merges fail with `Pull Request has merge conflicts`

For docs/test PRs that touch only their own file, this is fine. For impl PRs in the same spec that touch the same source file (the common case), it's a guaranteed bottleneck.

## User stories

### US1 (P1) — Orchestrator pre-resolves file overlap at scheduling time

> As the operator, when I dispatch a spec whose tasks `[P]` touch overlapping files, the SchedulerWorkflow detects the overlap at dispatch and serializes the affected tasks instead of running them in parallel. PRs land in dependency order; no auto-merge conflicts.

**Independent test:** Compile a fixture spec where T001, T002, T003 are marked `[P]` but all write to the same file (e.g., the FilePaths field per spec 077 adapter context). Dispatch. Assert: WorkUnitWorkflows T001 → T002 → T003 run sequentially; PRs open in that order; each lands cleanly.

### US2 (P2) — Auto-rebase next-in-queue after a sibling merges

> When a SchedulerWorkflow's child PR merges to main, any in-flight sibling PR whose branch is now N commits behind gets auto-rebased (or rejected and re-dispatched) before it tries to merge. The operator never sees a conflicting PR from an auto-dispatch.

**Independent test:** Two siblings dispatched in parallel both push branches and open PRs. Merge the first. Assert the second's branch gets rebased onto main (or a re-dispatch kicks in) before the operator attempts to merge it.

### US3 (P2) — File-scope declarations from tasks.md drive the DAG

> Spec author can annotate tasks.md with explicit file-scope hints (`files: [path1, path2]`) so the spec-077 adapter compiles a DAG that respects file-overlap dependencies up front — without the operator needing to know which tasks collide.

**Independent test:** A tasks.md where T001 declares `files: [foo.go]` and T002 declares `files: [foo.go, bar.go]` produces a DAG with a T001 → T002 edge automatically. Two tasks with disjoint files run in parallel.

## Functional requirements

### File-overlap detection (US1)

- **FR-001** Extend the spec-077 adapter to surface every task's expected file scope. Sources, in precedence order: (a) explicit `files:` annotation in tasks.md, (b) backtick-quoted paths in the task description (already extracted as `FilePaths` per `adapter/context.go`), (c) empty (= treated as overlap-anywhere risk).
- **FR-002** Extend the SchedulerWorkflow's DAG planner to inject dependency edges between any two tasks whose file scopes overlap. Tasks with disjoint scopes keep their parallelism.
- **FR-003** Edge injection is observable in the chain via a new event type `dag_overlap_serialized: {spec_ref, from_task, to_task, overlap_files}`. Operator can inspect which serializations the orchestrator applied.

### Auto-rebase (US2)

- **FR-004** When a chitin/wu/* PR merges to main, the orchestrator detects in-flight sibling PRs (same spec dispatch run_id) and triggers a rebase activity per sibling.
- **FR-005** Rebase activity is best-effort: if the rebase produces conflicts, the activity emits `sibling_rebase_failed: {pr, conflict_files}` and the PR is left in the operator's queue for manual resolution. No auto-resolve.
- **FR-006** Rebase happens via the same worker that owns the worktree (re-dispatches the driver against the new base). Operator-visible chain event: `sibling_rebase_dispatched: {pr, new_base_sha}`.

### tasks.md file-scope annotations (US3)

- **FR-007** Adopt a tasks.md frontmatter convention `files:` per task, parsed by the spec-077 adapter. Backward-compatible: tasks without the annotation use the description-derived FilePaths.
- **FR-008** `tasks-lint` (spec 107) extends to surface file-scope per task in its table + JSON output (validator can flag "no file scope declared and description has no path hints" as a warning).

### Telemetry

- **FR-009** Chain event `dag_planned: {spec_ref, parallel_groups: [{tasks, file_scopes}], serialized_pairs: [{from, to, overlap}]}` per dispatch. Lets the operator see how parallel the spec actually is after overlap detection.
- **FR-010** `chitin-orchestrator dag-plan <spec-ref>` operator subcommand that prints the planned DAG with the serialization edges marked, so the operator can preview what dispatch will look like before pushing.

## Success criteria

- **SC-001** Re-dispatching specs 107, 108, 109 (the dogfood failures) with this spec implemented produces zero post-merge conflicts. Every codex-authored PR merges cleanly in DAG order.
- **SC-002** Clean-merge rate across the next 5 spec dispatches: ≥ 95%.
- **SC-003** No regression in scheduler performance — file-overlap detection adds ≤ 100ms to DAG compilation for a 20-task spec.

## Scope

### In scope

- File-scope detection in the spec-077 adapter (extends existing FilePaths field)
- DAG edge injection in SchedulerWorkflow's planner
- Auto-rebase activity in WorkUnitWorkflow (best-effort, fail-soft)
- tasks.md `files:` annotation convention + `dag-plan` audit subcommand
- Chain events for observability

### Out of scope

- Auto-resolving merge conflicts (file-scope analysis only — content merging is hard)
- Pre-compute optimization that finds the optimal parallel/serial cut (greedy serialization is good enough for v1)
- Multi-spec dispatch coordination (cross-spec overlap is operator concern; spec 113 territory if needed)
- Locking files in main for in-flight workers (would need git server-side hooks; out of scope)

## Edge cases

- **A task's description has no backtick-paths and no `files:` annotation:** treat as overlap-with-everything. Either the spec author fixes the wording or accepts that the task runs alone (no parallel siblings).
- **Two siblings touch the same file but the changes are non-overlapping (e.g., one adds a function, one adds a const):** still serialized under current design. Future spec could relax this if the autonomous loop can statically detect non-overlapping diffs.
- **A sibling rebase encounters a real conflict (operator merged something else mid-flight):** activity emits `sibling_rebase_failed`, PR stays open in conflict state, operator handles. The system never auto-clobbers.

## Dogfood evidence (motivating data)

Spec 109 dispatched 7 tasks (T001-T007). Auto-merge outcomes:

| Task | PR | Result |
|---|---|---|
| T001 (prompt template, file: review_mode.go) | #1015 | conflict — manually rebased |
| T002 (post-processor, file: review_mode.go) | #1014 | merged ✓ |
| T003 (wire dispatch, file: review_mode.go) | #1016 | conflict — abandoned manual rebase |
| T004 (clean-JSON test, file: review_mode_test.go) | #1017 | merged ✓ |
| T005 (markdown test, file: review_mode_test.go) | #1019 | conflict |
| T006 (prose test, file: review_mode_test.go) | -- | (folded into T005 PR likely) |
| T007 (validation-failure test, file: review_mode_test.go) | #1018 | conflict |

5/7 conflicted. Two files (impl + test) each had 3 sibling collisions. Spec 112 would have inserted T001→T002→T003 serialization on review_mode.go and T004→T005→T007 serialization on review_mode_test.go, eliminating all 5 conflicts.
