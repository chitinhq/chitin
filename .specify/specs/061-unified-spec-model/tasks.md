# Tasks: Spec 061 — Unified spec model + framework adapters

**Input**: spec.md + plan.md in `.specify/specs/061-unified-spec-model/`

**Prerequisites**: spec.md (✅ ratified), plan.md (✅ this directory)

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **Story**: Maps to spec requirements/slices

---

## Phase 1: Schema & Interface (Slice 1 — R1, R2)

**Purpose**: Define the canonical `UnifiedSpec` shape and the `SpecAdapter` interface.

- [x] T001 [P] [R1] Define `UnifiedSpec` JSON Schema at `libs/contracts/schemas/unified-spec.schema.json`
- [x] T002 [P] [R1] Implement TypeScript/Zod bindings at `libs/contracts/src/unified-spec.schema.ts`
- [x] T003 [P] [R1] Implement Go `UnifiedSpec` types at `go/execution-kernel/internal/spec/spec.go`
- [x] T004 [P] [R1] Implement Python `UnifiedSpec` dataclass at `python/analysis/unified_spec.py`
- [x] T005 [R2] Define `SpecAdapter` interface (`Detect` + `Parse` + `Framework`) in `go/execution-kernel/internal/spec/adapter/adapter.go`
- [x] T006 [R2] Implement adapter registry (`Register`, `Lookup`, `All`, `DetectAdapters`) in `adapter.go`

**Checkpoint**: Schema + interface defined; no adapter yet.

---

## Phase 2: Spec-kit/House Adapter (Slice 1 — R3, R6)

**Purpose**: Build the reference adapter that parses all house-format specs.

- [x] T007 [R3] Implement `SpeckitAdapter.Detect()` — match `.specify/specs/NNN-slug/spec.md` pattern
- [x] T008 [R3] Implement `SpeckitAdapter.Parse()` — extract spec_id, title, status, requirements, acceptance, boundaries, slices, open_questions
- [x] T009 [R3] Handle all observed spec.md format variations (title formats, heading styles, bold markers)
- [x] T010 [R3] Implement `SpecKitParseError` (typed error with path + section) for boundary case 3
- [x] T011 [R3] Implement `DuplicateIDError` for boundary case 2 (collision detection)
- [x] T012 [R3] Implement Python `speckit_adapter.py` mirroring the Go adapter
- [x] T013 [R6] Verify round-trip integrity: `parse` then `to_dict()` produces semantically equivalent output

**Checkpoint**: Spec-kit adapter parses all house specs; round-trip is lossless.

---

## Phase 3: Tests & Validation (Slice 1 — AC1–AC6)

**Purpose**: Prove all acceptance criteria against the real spec corpus.

- [x] T014 [AC1] Validate `UnifiedSpec` schema is documented and passes JSON Schema validation
- [x] T015 [AC2] Run spec-kit adapter over all 88 house specs — zero failures, zero data loss
- [x] T016 [AC3] Verify `detect` routes each path to exactly one adapter
- [x] T017 [AC4] Verify malformed specs raise typed `ParseError` (not half-populated model)
- [x] T018 [AC5] Verify round-trip losslessness for house format
- [x] T019 [AC6] Verify adding a new adapter is a one-entry registry change (blank-import + `Register`)

**Checkpoint**: All AC1–AC6 pass; Slice 1 is done.

---

## Phase 4: Superpowers Adapter (Slice 2 — R5)

**Purpose**: Add the Superpowers markdown adapter.

- [x] T020 [R5] Implement `SuperpowersAdapter.Detect()` — match `docs/superpowers/` plans
- [x] T021 [R5] Implement `SuperpowersAdapter.Parse()` — map plan fields to `UnifiedSpec`, mark missing fields honestly
- [x] T022 [R5] Register superpowers adapter via `init()` in `superpowers/register.go`
- [x] T023 [R5] Add tests for Superpowers adapter detect/parse

**Checkpoint**: Both spec-kit and Superpowers adapters are registered and working.

---

## Phase 5: OpenSpec Adapter (Slice 3 — R4)

**Purpose**: Add the OpenSpec adapter (pending Q3 — OpenSpec format confirmation).

- [ ] T024 [R4] Confirm OpenSpec on-disk format with operator/design-review
- [ ] T025 [R4] Implement `OpenSpecAdapter.Detect()` — match OpenSpec layout
- [ ] T026 [R4] Implement `OpenSpecAdapter.Parse()` — map OpenSpec fields to `UnifiedSpec`
- [ ] T027 [R4] Register OpenSpec adapter in the registry
- [ ] T028 [R4] Add tests for OpenSpec adapter detect/parse

**Checkpoint**: All three adapters registered; L1 is complete.

---

## Phase 6: Polish & Cross-Cutting

**Purpose**: Clean up the Python package and finalize grooming artifacts.

- [ ] T029 [P] Restore Python `spec_adapter/` package source files (currently only `.pyc` remains)
- [ ] T030 [P] Resolve spec open questions Q1 (model owner), Q3 (OpenSpec format), Q4 (adapter priority)
- [ ] T031 Update spec status from DRAFT to RATIFIED after all open questions resolved

---

## Dependencies & Execution Order

- Phase 1 → Phase 2 (adapter needs interface) → Phase 3 (tests need adapter)
- Phase 4 depends on Phase 2 (registry pattern established)
- Phase 5 depends on Q3 resolution (OpenSpec format)
- Phase 6 is cleanup, can start once Phase 3 passes

### Parallel Opportunities

- T001, T002, T003, T004 can run in parallel (different languages)
- T020–T023 can run in parallel with Phase 3 tests once Phase 2 is done
- T029, T030 are independent cleanup tasks