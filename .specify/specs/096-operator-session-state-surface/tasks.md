---

description: "Task list — 096 operator session-state surface"
---

# Tasks: Operator session-state surface

**Input**: Design documents from `/specs/096-operator-session-state-surface/`

**Prerequisites**: plan.md (✓), spec.md (✓), research.md (✓), data-model.md (✓), contracts/ (✓), quickstart.md (✓)

**Tests**: included — schema migration backward-compatibility (SC-004) and the operator round-trip (SC-001) both require test coverage at the Go API level AND at the CLI level. Spec 091 v1.1 (the first consumer) will further verify the producer side externally.

**Organization**: Tasks grouped by user story. Spec 096 has US1 (P1 — unlock + audit, the load-bearing recovery primitive), US2 (P1 — consumer detection, exercised via the status query and the lock_epoch contract), and US3 (P2 — operator status listing).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Different files, no dependencies — parallelizable.
- **[Story]**: `[US1]` unlock + audit + lock CLI; `[US2]` status query for consumer detection + epoch contract; `[US3]` operator status listing.
- Paths are absolute-from-repo-root.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Land the schema migration and the dispatcher scaffold. After this phase, the kernel binary's existing behavior is byte-identical to today's, but `session.*` subcommands exist as `not implemented` stubs and the `agent_state` schema has the new columns on every open.

- [ ] T001 Schema migration logic in `go/execution-kernel/internal/gov/escalation.go`: introduce `migrateAgentStateSchema(*sql.DB) error` that introspects via `PRAGMA table_info(agent_state)` and conditionally runs the two `ALTER TABLE ADD COLUMN` statements per `contracts/schema-migration.md`. Call from `OpenCounter()` after `CREATE TABLE IF NOT EXISTS`. Idempotent — re-running is a no-op.
- [ ] T002 Update the `CREATE TABLE` statement for `agent_state` in `escalation.go` to include `unlock_ts TEXT` and `lock_epoch INTEGER NOT NULL DEFAULT 0` so a fresh database creation lands the full new schema in one shot (no migration needed).
- [ ] T003 [P] Schema migration tests in `go/execution-kernel/internal/gov/escalation_migrate_test.go`: cover `TestMigrateAddsColumns_FromOldSchema`, `TestMigrateIdempotent`, `TestMigrateBackwardCompatibleAPIs`, `TestMigrateInterrupted`, `TestMigrateConcurrent`, `TestCreateFreshDB` per `contracts/schema-migration.md` test invariants.
- [ ] T004 Add `session` subcommand parent dispatcher in `go/execution-kernel/cmd/chitin-kernel/session.go`: parse `os.Args[2]` to route to `unlock` / `lock` / `status` subcommand handlers (all initially `not yet implemented` stubs); print help on `session` with no further args.
- [ ] T005 Wire `session` dispatcher into `go/execution-kernel/cmd/chitin-kernel/main.go`'s top-level dispatcher; preserve all existing subcommand routes.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared helpers all three subcommand handlers depend on, plus the chain-emit refactor that lets both CLI subcommands and in-process auto-escalation use the same emission path. After this phase, the three subcommand handlers can be developed independently.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [ ] T006 Extract chain-emit core into `go/execution-kernel/internal/emit/emit.go`: expose `SessionLocked(ctx, agent, lockEpochAfter, source, reason) error` and `SessionUnlocked(ctx, agent, lockEpochAfter, reason, lockedTsBefore, totalAtUnlock) error`. Both write the events to the chain via the same code path `chitin-kernel emit` uses today; the CLI subcommand stays as a thin wrapper around `cmdEmit`. Per D6 — single chain-write seam.
- [ ] T007 [P] Counter.Unlock Go API in `go/execution-kernel/internal/gov/session.go`: `func (c *Counter) Unlock(agent, reason string) (UnlockResult, error)` — runs the unlock transaction per `data-model.md` Entity 2 sequence + idempotent path per D5 (NO epoch advance on unlock-of-unlocked); emits `session_unlocked` event via T006's emit helper; returns `UnlockResult{Idempotent bool, LockEpochAfter int, LockedTsBefore string, TotalAtUnlock int}`.
- [ ] T008 [P] Counter.LockEpoch Go API: `func (c *Counter) LockEpoch(agent string) (int, error)` — read-only single-agent epoch query.
- [ ] T009 [P] Counter.Status Go API: `func (c *Counter) Status(agent string) (*AgentStatus, error)` (single agent — returns nil with ErrNoAgent if missing) AND `func (c *Counter) StatusAll() ([]AgentStatus, error)` (sorted by agent ASCII).
- [ ] T010 Modify Counter.Lockdown (operator Go API kill-switch) in `escalation.go` to advance `lock_epoch` and emit `session_locked` with `source: "operator_go_api"` (per R6). Tests in `session_test.go` verify the addition.
- [ ] T011 Modify `RecordActionDenial` in `escalation.go` to advance `lock_epoch` ON THE LOCK-TRANSITION BRANCH ONLY (when total crosses to ≥10 and we set locked=1) and emit `session_locked` with `source: "auto_escalation"`. Per R4: most denials don't cross the threshold; the chain emit is only on the transition.
- [ ] T012 [P] Tests for FR-005 in `escalation_test.go`: TestAutoEscalationAdvancesEpoch (lock transition advances epoch by 1, regular denial without crossing threshold does NOT advance); TestAutoEscalationEmitsChainEvent (one chain event per transition with source="auto_escalation"); TestAutoEscalationFailSoft (rename kernel-emit binary; assert lockdown still persists in gov.db + warning logged + no error returned per D9).

**Checkpoint**: Foundation ready — three subcommand handlers can now be implemented in parallel.

---

## Phase 3: User Story 1 — Operator unlocks an agent and audit history survives (Priority: P1) 🎯 MVP-half-1

**Goal**: An operator runs `chitin-kernel session unlock -agent clawta -reason "X"` and the agent transitions to unlocked WITHOUT losing `denials` / `denial_events` history. Also includes `session lock` as the operator kill-switch CLI.

**Independent Test**: with an agent locked (via `Counter.RecordActionDenial` to ≥10 denials), run `session unlock`; assert (a) gov.db has `locked=0, unlock_ts populated, lock_epoch advanced, total unchanged, denials rows untouched`, (b) one `session_unlocked` chain event with the operator reason, (c) `Counter.RecordDenial` continues working.

### Tests for User Story 1

- [ ] T013 [P] [US1] Argv parsing tests in `cmd/chitin-kernel/session_unlock_argv_test.go`: parse `-agent X -reason Y`; `-agent` missing → exit 1 with usage; unknown flag → exit 1.
- [ ] T014 [P] [US1] Unlock handler unit tests in `cmd/chitin-kernel/session_unlock_test.go`: happy path (locked → unlocked, epoch advanced); idempotent path (already-unlocked → epoch unchanged, chain event still emitted); unknown agent → exit 1 with spec'd stderr.
- [ ] T015 [P] [US1] Lock handler tests in `cmd/chitin-kernel/session_lock_test.go`: happy path; bootstrap-lock for unseen agent (creates row); re-lock advances epoch (NOT idempotent per `lock-subcommand.md`); chain event emitted with `source: "operator_cli"`.

### Implementation for User Story 1

- [ ] T016 [US1] Implement `cmdSessionUnlock(args []string) int` in `cmd/chitin-kernel/session_unlock.go` per `contracts/unlock-subcommand.md`: parse argv → open Counter (db-path) → call `Counter.Unlock(agent, reason)` (T007) → print outcome line → return ExitSuccess. Stable stderr messages for the spec'd failure modes.
- [ ] T017 [US1] Implement `cmdSessionLock(args []string) int` in `cmd/chitin-kernel/session_lock.go` per `contracts/lock-subcommand.md`: parse argv → open Counter → call a new `Counter.OperatorLock(agent, reason)` Go API that wraps `Counter.Lockdown(agent)` semantics with chain emit + epoch advance + source="operator_cli" (distinguish from Counter.Lockdown's source="operator_go_api" by passing source as a parameter).
- [ ] T018 [US1] Wire `session unlock` and `session lock` into the `session` subcommand dispatcher (T004); replace the `not yet implemented` stubs.
- [ ] T019 [US1] Integration test in `cmd/chitin-kernel/session_integration_test.go`: build a fresh gov.db; auto-escalate an agent to locked state (10 denials); call `cmdSessionUnlock` against it; assert all post-state invariants per the Independent Test description above (gov.db state + chain events).

**Checkpoint**: After this phase, an operator can unlock or kill-switch-lock agents via the CLI. The status query is still a stub.

---

## Phase 4: User Story 2 — Consumer can detect operator unlock via epoch comparison (Priority: P1) — MVP-half-2

**Goal**: An in-process consumer (the openclaw plugin per spec 091 v1.1) caches the `lock_epoch` at lock-set time, then queries `session status -agent X` after a suspected unlock; if the epoch advanced OR `locked=false`, the consumer transitions to unlocked. Spec 096 owns the producer side (the status query that returns a fresh epoch); spec 091 v1.1 will own the consumer side.

**Independent Test**: lock an agent (epoch=1); cache that epoch externally; unlock (epoch=2); call `session status -agent X` and assert the returned epoch is 2 AND locked=false; advance manually with a second lock (epoch=3); call status and assert the new epoch is observed.

### Tests for User Story 2

- [ ] T020 [P] [US2] Status inspect-mode tests in `cmd/chitin-kernel/session_status_test.go`: known agent → JSON object with all spec'd fields; unknown agent → exit 1 with spec'd stderr; `--text` produces table with single data row.
- [ ] T021 [P] [US2] Epoch monotonicity tests in `internal/gov/session_test.go`: 100 lock/unlock cycles → `LockEpoch` advances by 200; idempotent unlocks interspersed → no extra advances; concurrent goroutine cycles (10 goroutines × 100 cycles) → final epoch = sum (no skipped or duplicated advances).

### Implementation for User Story 2

- [ ] T022 [US2] Implement `cmdSessionStatus(args []string) int` inspect-mode in `cmd/chitin-kernel/session_status.go` per `contracts/status-subcommand.md`: when `-agent` is present → call `Counter.Status(agent)` (T009) → render JSON or table → exit 0; absent agent path is US3.
- [ ] T023 [US2] Wire inspect-mode routing in the session dispatcher (T004); status-with-agent is now functional even though list-mode is still a stub.

**Checkpoint**: After this phase, the producer side of US2 (the inspectable lock_epoch) is live. Spec 091 v1.1 implementation can begin consuming it in its own PR.

---

## Phase 5: User Story 3 — Operator inspects session state on demand (Priority: P2)

**Goal**: An operator runs `chitin-kernel session status` (no `-agent`) and sees the live state of every agent — JSON-by-default for piping into jq, `--text` for human reading.

**Independent Test**: insert 3 agents in non-sorted order; run `session status` and assert array is sorted by agent ASCII; run `--text` and assert fixed-column table with header.

### Tests for User Story 3

- [ ] T024 [P] [US3] List-mode tests in `cmd/chitin-kernel/session_status_list_test.go`: 0 agents → JSON `[]` + exit 0; 3 agents in non-sorted order → JSON sorted ASC; deterministic output across two consecutive invocations (asserts FR-009).
- [ ] T025 [P] [US3] Text-mode formatting tests in `cmd/chitin-kernel/session_status_text_test.go`: fixed-column widths per `contracts/status-subcommand.md`; long agent names truncated with `…`; null `unlock_ts` rendered as `-`.

### Implementation for User Story 3

- [ ] T026 [US3] Extend `cmdSessionStatus` (T022) with list mode: when `-agent` absent → call `Counter.StatusAll()` (T009) → render JSON array or text table.
- [ ] T027 [US3] Wire list-mode routing; remove the remaining `not yet implemented` stub from the dispatcher.

**Checkpoint**: All three subcommands work end-to-end. Spec 096 MVP is complete.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Operator runbook, CHANGELOG, end-to-end verification, and a lint sanity check.

- [ ] T028 [P] Operator runbook at `docs/operator/session-state.md` documenting the three subcommands, their JSON/text shapes, exit codes, the `lock_epoch` model, and the two chain event types.
- [ ] T029 [P] CHANGELOG entry under the next `chitin-kernel` release section: mention `session unlock` / `session lock` / `session status` subcommands + the additive `agent_state` schema migration + the two new chain event types.
- [ ] T030 [P] Speckit-lint clean check on spec 096 — assert 0 findings from `chitin-kernel speckit-lint specs/096-operator-session-state-surface`.
- [ ] T031 Run quickstart.md end-to-end against a real chitin-kernel build with a sandbox gov.db; record wall-clock for unlock (<5s for SC-001) and verify the chain events landed correctly; attach the recorded outputs to the implementation PR body.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: schema + dispatcher; T001 + T002 land together; T003 tests them; T004+T005 stage the dispatcher.
- **Foundational (Phase 2)**: T006-T012; T006 (emit refactor) blocks T007/T010/T011 (they all use it); T007/T008/T009 are parallelizable.
- **US1 (Phase 3)**: depends on Phase 2 — handlers consume the new Counter Go APIs.
- **US2 (Phase 4)**: depends on Phase 2 — status reads the new state. Can run in parallel with US1.
- **US3 (Phase 5)**: depends on US2 (extends `cmdSessionStatus`).
- **Polish (Phase 6)**: depends on all stories complete.

### User Story Dependencies

- US1 + US2 are both P1 and both part of the MVP; US3 is P2 and ships in the same PR but is optional for an MVP demo.
- US2 and US3 share the same implementation file (`session_status.go`) — they must be staged on the same task list but US3 extends US2's handler rather than replacing it.

### Parallel Opportunities

- T003 (schema migration tests) runs in parallel with T004/T005 (dispatcher scaffold)
- T007, T008, T009 are all in `internal/gov/session.go`; same file but logically distinct functions — implement together
- T013-T015 (US1 tests) are independent; can be authored in parallel
- T020-T021 (US2 tests) are in different files; parallel
- T028, T029, T030 (Polish docs) are all in different files; parallel

---

## Implementation Strategy

### MVP First (US1 + US2 together)

The MVP is `session unlock` (US1) + `session status -agent X` (US2 producer side). Spec 091 v1.1 cannot consume `session status` without it existing; spec 091 v1.1 cannot exercise `session unlock` without it existing. Both must land together.

1. Complete Phase 1: Setup (schema migration + dispatcher scaffold)
2. Complete Phase 2: Foundational (Counter API + chain emit refactor + RecordActionDenial / Counter.Lockdown modifications)
3. Complete Phase 3: US1 (unlock + lock CLIs)
4. Complete Phase 4: US2 inspect-mode (`session status -agent X`)
5. **STOP and VALIDATE**: run quickstart steps 1-4 against a sandbox gov.db
6. (US3 may follow in the same PR or as a quick follow-up.)

### Incremental Delivery (single-developer flow)

1. Setup + Foundational → schema and Go APIs ready; no operator surface yet
2. + US1 → unlock + lock CLIs work; demo-able
3. + US2 → status query works; spec 091 v1.1 implementation can begin
4. + US3 → list mode works; full operator UX
5. + Polish → docs and CHANGELOG ship

### Parallel Team Strategy

If two developers staffed:

1. Both complete Phase 1 + Phase 2 together
2. Developer A: US1 (Phase 3)
3. Developer B: US2 + US3 (Phases 4 + 5) — different files from US1
4. Polish phase converges

---

## Notes

- `[P]` tasks = different files, no dependencies
- `[Story]` label maps task to its user story
- Schema migration MUST be idempotent — re-running has no observable effect
- Chain emit failure during auto-escalation MUST be fail-soft (warn + continue, NOT roll back) per D9
- Lock-of-locked is NOT idempotent (advances epoch + emits event); unlock-of-unlocked IS idempotent (no epoch advance, BUT still emits event per D5)
- `Counter.Reset()` is NOT modified by this spec (FR-010)
