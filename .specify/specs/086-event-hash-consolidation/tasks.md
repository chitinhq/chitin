---
description: "Task list for Event-Hash Consolidation"
---

# Tasks: Event-Hash Consolidation

**Input**: Design documents from `specs/086-event-hash-consolidation/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md (all present)

**Tests**: This feature **explicitly requires** an automated cross-language parity check
(spec User Story 2, FR-005, FR-006). The test tasks below (T010, T011) are therefore
required deliverables, not optional. No TDD test-first tasks are added for US1/US3 — the
spec did not request TDD; those stories are verified by their dedicated verification tasks.

**Organization**: Tasks are grouped by user story so each can be implemented and verified
as an independent increment.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: `[US1]` / `[US2]` / `[US3]` — maps to a spec user story
- Every task names exact file paths

## Path Conventions

This feature lives in the repo's Go module tree under `go/` plus one TypeScript test under
`libs/`. Three Go modules are involved: the new `go/chainhash`, and the existing
`go/execution-kernel` and `go/run-sdk`. All paths below are repo-root-relative.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the new shared module shell so all three user stories have somewhere to build.

- [ ] T001 Create the `go/chainhash/` directory and `go/chainhash/go.mod` with `module github.com/chitinhq/chitin/go/chainhash`, `go 1.25.0`, and **no `require` block** (standard library only — per research.md Decision 1 and `contracts/chainhash-go-api.md`).
- [ ] T002 Wire `go/chainhash` into the Nx build graph: inspect how a sibling Go module is registered (check for `go/run-sdk/project.json` or `go/execution-kernel/project.json`, and `nx.json`) and replicate that registration for `go/chainhash`; if sibling Go modules carry no project file and are auto-discovered from `go.mod`, confirm the new module is picked up and record that no file is needed.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that must exist before user-story work.

No foundational tasks. The `go/chainhash` module shell from Phase 1 is the only shared
prerequisite; the hash implementation itself is the substance of User Story 1.

**Checkpoint**: Module shell ready — User Story 1 can begin.

---

## Phase 3: User Story 1 - An event hashes identically no matter which component emits it (Priority: P1) 🎯 MVP

**Goal**: The kernel and the run SDK share one hashing codepath (`go/chainhash`), so the
same event always receives the same hash. The boundary-case divergence is eliminated.

**Independent Test**: Hash a nested-payload event and a boundary-case event through the
kernel path and the run-SDK path; assert identical hashes. Run `chitin-kernel chain-verify`
on an existing chain; assert every event still verifies.

### Implementation for User Story 1

- [ ] T003 [US1] Implement `go/chainhash/hash.go` — exported `CanonicalJSON(value any) (string, error)`, `Sha256Hex(input string) string`, `HashEvent(event map[string]any) (string, error)`. Copy the logic verbatim from `go/execution-kernel/internal/hash/hash.go`, including the **strict `default` case** (`return fmt.Errorf("unsupported type %T in canonical JSON", value)` — research.md Decision 2) and the package header comment asserting byte-identical parity with `libs/contracts/src/hash.ts`. Must match `contracts/chainhash-go-api.md`.
- [ ] T004 [P] [US1] Add `require github.com/chitinhq/chitin/go/chainhash v0.0.0` and `replace github.com/chitinhq/chitin/go/chainhash => ../chainhash` to `go/execution-kernel/go.mod`, mirroring the existing `replace github.com/github/copilot-sdk/go => ./third_party/copilot-sdk-go` form (line 33).
- [ ] T005 [P] [US1] Repoint every importer of the kernel's `internal/hash` package to `github.com/chitinhq/chitin/go/chainhash`: run `grep -rl '"github.com/chitinhq/chitin/go/execution-kernel/internal/hash"' go/execution-kernel` to find the sites (~6 per research.md), change the import path and the `hash.` → `chainhash.` qualifier. Do NOT delete `internal/hash` yet — that is T013.
- [ ] T006 [P] [US1] Add the same `require` + `replace` for `chainhash` to `go/run-sdk/go.mod`.
- [ ] T007 [P] [US1] Repoint the run-SDK `manifest`-package callers of the unexported `canonicalJSON`/`sha256Hex`/`hashEvent` (defined in `go/run-sdk/hash.go`) to the exported `chainhash.CanonicalJSON`/`chainhash.Sha256Hex`/`chainhash.HashEvent`. Find call sites via `grep -rn 'hashEvent\|canonicalJSON\|sha256Hex' go/run-sdk`. Do NOT delete `hash.go` yet — that is T014.
- [ ] T008 [US1] Verify User Story 1: `go build ./...` passes in `go/chainhash`, `go/execution-kernel`, and `go/run-sdk`; hash a nested-payload event and a boundary-case event through both the kernel and run-SDK paths and assert the hashes are identical; run `chitin-kernel chain-verify --dir ~/.chitin` and confirm every event in an existing chain still verifies (US1 acceptance scenarios 1–3; FR-004).

**Checkpoint**: The divergence is gone — both components hash identically through `chainhash`. This is the shippable MVP. The old copies still exist (dead) and are removed in US3.

---

## Phase 4: User Story 2 - A change that breaks hash agreement is caught before it merges (Priority: P2)

**Goal**: A shared parity corpus and an automated cross-language test prove every
implementation agrees, and fail CI if any drifts.

**Independent Test**: Run the Go and TypeScript parity tests — both green. Inject a
deliberate divergence into the hash; confirm the parity test fails; revert.

**Dependency**: Requires User Story 1 (the `go/chainhash` implementation must exist).

### Implementation for User Story 2

- [ ] T009 [US2] Create the parity corpus `go/chainhash/testdata/parity-corpus.json` per `contracts/parity-corpus-format.md` — one entry per edge case: `ordinary-event`, `historical-chain-record`, `empty-payload`, `deeply-nested-object`, `unicode-strings`, `numeric-extremes`, and `unsupported-type` (with `expected_error: true`). Generate each `expected_hash` from the TypeScript reference `hashEvent` in `libs/contracts/src/hash.ts` (the cross-language spec-of-record, FR-009).
- [ ] T010 [P] [US2] Add `go/chainhash/hash_test.go` — load `testdata/parity-corpus.json` and, for each entry, assert `HashEvent` returns `expected_hash` (or an error when `expected_error` is set). Add direct unit tests for `CanonicalJSON` recursive key-ordering and the strict `default` error path.
- [ ] T011 [P] [US2] Add the cross-language parity test `libs/run-sdk/tests/hash-parity.test.ts` (vitest) — load the same `go/chainhash/testdata/parity-corpus.json`, hash each event with the TypeScript `hashEvent` from `@chitin/contracts`, and assert byte-identical agreement with each entry's `expected_hash`. Model the harness on the existing `libs/run-sdk/tests/go-sdk-schema.test.ts`.
- [ ] T012 [US2] Verify User Story 2: run `go test ./...` in `go/chainhash` and `rtk vitest run libs/run-sdk/tests/hash-parity.test.ts` — both green. Then inject a one-character divergence into `go/chainhash/hash.go` (e.g. alter key sorting), confirm the parity test FAILS, and revert (US2 acceptance scenarios 1–2; FR-005, FR-006).

**Checkpoint**: Hash agreement is now enforced — a future drift is blocked before merge.

---

## Phase 5: User Story 3 - Exactly one Go hash implementation to maintain (Priority: P3)

**Goal**: The two now-dead duplicate Go hash files are removed; one implementation remains.

**Independent Test**: Search the Go tree — exactly one `HashEvent` implementation exists.
Full build and test suites pass with no reference to a removed package.

**Dependency**: Requires User Story 1 (all call sites must already be repointed off the
old copies before they can be safely deleted).

### Implementation for User Story 3

- [ ] T013 [P] [US3] Delete the kernel's duplicate — remove the entire `go/execution-kernel/internal/hash/` directory (`hash.go` and any `hash_test.go`). Safe because T005 already repointed every importer.
- [ ] T014 [P] [US3] Delete the run-SDK's duplicate — remove `go/run-sdk/hash.go`. Safe because T007 already repointed every caller.
- [ ] T015 [US3] Verify User Story 3: `go build ./...` and `go test ./...` pass in all three Go modules; `go vet ./...` is clean; `grep -rl 'func HashEvent' go/` returns exactly one file (`go/chainhash/hash.go`); no source file references a removed `internal/hash` import (US3 acceptance scenarios 1–2; FR-001, FR-008).

**Checkpoint**: One Go hash implementation. All three user stories complete.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final whole-feature verification.

- [ ] T016 [P] Run the full `specs/086-event-hash-consolidation/quickstart.md` verification (all five steps) and confirm each passes.
- [ ] T017 [P] Confirm FR-007 / SC-005: run `go mod graph` in `go/run-sdk` and verify its only module dependency is the first-party `github.com/chitinhq/chitin/go/chainhash` — no external or heavyweight dependency was introduced.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately.
- **Foundational (Phase 2)**: empty.
- **User Story 1 (Phase 3)**: depends on Setup. This is the MVP.
- **User Story 2 (Phase 4)**: depends on User Story 1 (`go/chainhash` must be implemented).
- **User Story 3 (Phase 5)**: depends on User Story 1 (call sites must be repointed before
  the old files can be deleted). Independent of User Story 2 — US3 and US2 may proceed in
  parallel once US1 is done.
- **Polish (Phase 6)**: depends on US1 + US2 + US3.

### Within User Story 1

- T003 (implement `chainhash/hash.go`) blocks T005 and T007 (they import the package).
- T004 and T006 (the two `go.mod` edits) are independent of each other and of T003 — `[P]`.
- T005 depends on T003 + T004; T007 depends on T003 + T006; T005 and T007 touch disjoint
  modules — `[P]` with each other.
- T008 (verify) depends on T005 + T007.

### Within User Story 2

- T009 (corpus) blocks T010 and T011.
- T010 (Go test) and T011 (TS test) are different files — `[P]`.
- T012 (verify) depends on T010 + T011.

### Within User Story 3

- T013 and T014 are different modules — `[P]`.
- T015 (verify) depends on T013 + T014.

### Parallel Opportunities

- T004 ‖ T006 — the two `go.mod` edits.
- T005 ‖ T007 — kernel-importer repoint ‖ run-SDK-caller repoint.
- T010 ‖ T011 — Go parity test ‖ TypeScript parity test.
- T013 ‖ T014 — delete kernel copy ‖ delete run-SDK copy.
- T016 ‖ T017 — the two polish checks.
- Once US1 is done, US2 and US3 can proceed in parallel.

## Parallel Example: User Story 1

```bash
# After T003 (chainhash/hash.go) exists, the two module wirings run in parallel:
Task: "T004 — add require+replace to go/execution-kernel/go.mod"
Task: "T006 — add require+replace to go/run-sdk/go.mod"

# Then the two repoint tasks run in parallel (disjoint modules):
Task: "T005 — repoint kernel internal/hash importers to chainhash"
Task: "T007 — repoint run-sdk manifest-package callers to chainhash"
```

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 Setup (T001–T002).
2. Phase 3 User Story 1 (T003–T008).
3. **STOP and VALIDATE**: both components hash identically; existing chains still verify.
   The correctness bug is fixed — this is a shippable increment on its own.

### Incremental Delivery

1. Setup → US1 → the divergence is fixed (MVP).
2. Add US2 → hash agreement is now enforced in CI.
3. Add US3 → the dead duplicates are removed; one implementation remains.
4. Polish → whole-feature verification.

Each story is a self-contained increment: US1 fixes the bug, US2 protects it, US3 cleans up.

## Notes

- `[P]` = different files, no dependency on an incomplete task.
- `[Story]` label maps a task to a spec user story for traceability.
- Per constitution §2, implementation runs in a dedicated git worktree, not the shared checkout.
- The hashing algorithm and output do not change — historical chains must keep verifying
  (FR-004); T008 checks this explicitly.
- Commit after each task or logical group; stop at any checkpoint to validate a story.
