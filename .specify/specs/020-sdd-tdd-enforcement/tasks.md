# Tasks: SDD+TDD Enforcement (Spec 020)

**Input**: Design documents from `.specify/specs/020-sdd-tdd-enforcement/`

**Prerequisites**: plan.md (required), spec.md (required for user stories/ACs)

**Tests**: All test tasks are required — the spec mandates regression tests for every AC.

**Organization**: Tasks grouped by layer (L1, L2, L3) then cross-cutting.

## Format: `[ID] [P?] [Layer] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Layer]**: Which enforcement layer or cross-cutting concern this task belongs to

---

## Phase 1: L1 — no-code-without-test hook

- [ ] T001 [L1] Create `swarm/bin/worker-pre-commit-no-code-without-test.sh` — shell wrapper following scope-hook pattern (resolve git-dir, bypass via `SWARM_SKIP_TEST_CHECK=1`, call python checker, format rejection message per AC1)
- [ ] T002 [L1] Create `swarm/bin/worker-pre-commit-no-code-without-test.py` — python logic: get staged code files (`src/**`, `routes/**`, `services/**`, `lib/**`, `controllers/**`, `models/**`), check for staged test files (`__tests__/**`, `tests/**`, `e2e/**`, `*.test.*`, `*.spec.*`) or `no-test-change-justified:` in commit message, exit 1 with AC1 message on failure
- [ ] T003 [L1] Test `test_l1_rejects_code_without_test` — stage a code file without any test file and without escape clause; assert hook exits 1 with message matching spec AC1
- [ ] T004 [L1] Test `test_l1_accepts_with_test_or_escape_clause` — stage a code file alongside a test file (pass); stage a code file with escape clause in commit message (pass); assert both exit 0 per AC2

---

## Phase 2: L2 — spec-has-test-coverage hook

- [ ] T005 [P] [L2] Create `swarm/bin/worker-pre-commit-spec-has-test-coverage.sh` — shell wrapper (same pattern as L1, no bypass env var per spec, call python checker)
- [ ] T006 [P] [L2] Create `swarm/bin/worker-pre-commit-spec-has-test-coverage.py` — python logic: (a) when a `.specify/specs/*/spec.md` is staged, read content, check `## Test coverage` section exists with ≥1 table row binding AC to test case; (b) when a test file is staged under recognized test directories, check first 20 lines for `spec: NNN-<slug>` reference comment; exit 1 with guidance on either failure
- [ ] T007 [L2] Test `test_l2_rejects_spec_without_e2e_section` — stage a spec.md with `## Test coverage` section removed; assert hook exits 1 per AC3
- [ ] T008 [L2] Test `test_l2_rejects_test_without_spec_reference` — stage a test file lacking `// spec:` reference; assert hook exits 1 per AC4

---

## Phase 3: L3 — no-pr-without-spec gate

- [ ] T009 [L3] Add `before-gh-pr-create` gate step to `swarm/workflows/kanban-dispatch.lobster` — diff the PR branch against base, check if any `.specify/specs/NNN-*/spec.md` is in the diff, or if PR body contains `Spec: NNN-<slug>` referencing existing spec on origin/default; reject with envelope per AC5/AC6
- [ ] T010 [L3] Update `docs/governance-setup-extras/kanban-dispatch.lobster` (mirror) with same L3 gate step
- [ ] T011 [L3] Test `test_l3_blocks_gh_pr_create_without_spec_in_diff_or_body` — simulate a PR whose diff has no spec edit and whose body lacks `Spec:` line; assert gate rejects per AC5
- [ ] T012 [L3] Test `test_l3_allows_gh_pr_create_with_spec_reference` — simulate a PR with `Spec: 020-sdd-tdd-enforcement` in body (spec exists on origin/main); assert gate allows per AC6

---

## Phase 4: Cross-cutting — constitution, lobster install, regression

- [ ] T013 [Cross] Add §1.2 (Spec→test contract) to `.specify/constitution.md` — text per spec's "Constitution amendment" section
- [ ] T014 [Cross] Extend `test_workflow_mirror_matches_canonical` in existing test file — verify L3 gate step appears in both `swarm/workflows/kanban-dispatch.lobster` and its governance-setup-extras mirror
- [ ] T015 [Cross] Add L1 and L2 hook install blocks to the lobster `spawn_worker` step — install both new hooks alongside existing scope hook in worker worktree setup
- [ ] T016 [Cross] Make scripts executable (`chmod +x`) and add `# spec: 020-sdd-tdd-enforcement` reference comments to all new python files per L2's own rule

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (L1)**: No dependencies — can start immediately
- **Phase 2 (L2)**: No dependencies — can start immediately (parallel with Phase 1)
- **Phase 3 (L3)**: No dependencies on L1/L2 — can start in parallel, but L3 gate must coexist with existing lobster flow
- **Phase 4 (Cross)**: Depends on L1/L2/L3 scripts being written (T015 needs hook paths); T013/T014 can start once L3 is in lobster

### Within Each Phase

- Shell wrapper before python logic (T001→T002, T005→T006)
- Tests after their respective hooks (T003/T004 after T001+T002, T007/T008 after T005+T006, T011/T012 after T009)

### Parallel Opportunities

- T001-T002 and T005-T006 can run in parallel (different files)
- T009-T010 can run in parallel with L1/L2 work
- T013 (constitution) is independent

### Implementation Strategy

**Slice per layer**: implement L1 fully (hook + tests) → L2 fully → L3 fully → cross-cutting. This allows incremental validation: each layer is independently testable and independently deployable to worktrees.