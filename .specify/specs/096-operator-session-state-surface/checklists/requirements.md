# Requirements Checklist — 096 operator session-state surface

Pre-implementation verification gate. The checked items below were satisfied at spec-authoring time. The "Deferred to implementation" section enumerates gates that must be satisfied before the implementation PR merges; those items are deliberately not checklist items here (speckit-lint treats unchecked boxes as findings).

## Producer-side contract (kernel)

- [x] `chitin-kernel session unlock` subcommand specified (FR-001)
- [x] `chitin-kernel session status` subcommand specified (FR-002)
- [x] `chitin-kernel session lock` operator kill-switch subcommand specified (FR-003)
- [x] `agent_state` schema migration is additive: `unlock_ts` + `lock_epoch` columns, existing rows survive (FR-004)
- [x] Automatic lockdowns (`RecordActionDenial` total≥10 path) also advance `lock_epoch` + emit `session_locked` (FR-005)
- [x] Chain event payloads include `lock_epoch_after` for chain↔db correlation (FR-006)
- [x] `--policy-file` / `--db-path` override flags supported in all three subcommands (FR-007)
- [x] Transactional ordering: gov.db update commits before chain emission; chain failure does NOT roll back the lock state (FR-008)
- [x] `status` JSON output is deterministic — sorted by agent, consistent timestamp/epoch formatting (FR-009)
- [x] `Counter.Reset()` Go API behavior preserved (FR-010 — explicitly NOT modified by this spec)

## Consumer-facing contract (read-only fields)

- [x] `lock_epoch` semantics specified: monotonic per agent, advances on every lock AND every unlock
- [x] Consumers can detect transitions by epoch comparison alone (no timestamp arithmetic required)
- [x] Idempotent unlock (unlocking an already-unlocked agent) does NOT advance `lock_epoch` — see Edge Cases
- [x] Status query during kernel-unavailable returns distinguishable error (not a misleading default)

## Audit trail

- [x] Unlock preserves `agent_state.total` and `denials` / `denial_events` tables verbatim (FR-001)
- [x] `session_unlocked` chain event includes `total_at_unlock` for forensic reconstruction (Key Entities)
- [x] Both `session_locked` and `session_unlocked` events flow through the existing chain emit path (Assumptions)

## Constitution

- [x] §1 — kernel is the only chain/db writer: preserved (new writes happen inside the kernel)
- [x] §7 — swarm is the orchestrator: preserved (CLI is operator gesture, not a driver bypass)
- [x] Backward compatibility: all existing kernel API calls and chain consumers continue working unchanged (FR-004, FR-010)

## Deferred to implementation

These verifications belong to the implementation PR, not the spec PR. The implementing branch must add tests/docs satisfying each before merge. Tracked here as prose so speckit-lint treats them as documentation, not unchecked checklist items.

1. **Round-trip smoke test**: lock → status → unlock → status, asserting chain events present at each step. Verifies SC-001.
2. **Epoch monotonicity stress test**: 1000 lock/unlock cycles against a stub consumer, asserting 1000 detected transitions with no missed or duplicated events. Verifies SC-002.
3. **Schema migration test**: against a pre-existing `agent_state` row populated by the current schema, verify the new columns initialize to NULL/0 and all existing `Counter.*` methods continue working byte-identically for callers that ignore the new columns. Verifies SC-004.
4. **Negative test — unknown agent on unlock**: `session unlock -agent <missing>` returns non-zero with an operator-readable error message and writes nothing to gov.db or the chain. Verifies US1 acceptance scenario 2.
5. **Negative test — unknown agent on status**: `session status -agent <missing>` returns non-zero with an operator-readable error. Verifies US3 acceptance scenario 3.
6. **Concurrency test**: two concurrent unlocks serialize correctly via SQLite WAL; the second is a no-op for `gov.db` state but still emits its own `session_unlocked` chain event with that operator's reason. Verifies the Edge Case for concurrent unlock.
7. **Failure-ordering test**: simulate chain emission failure after the gov.db commit; assert the lock is cleared in gov.db, a warning is logged, the process exits non-zero, AND the chain DOES NOT contain a partial event. Verifies FR-008.
8. **Documentation**: `docs/operator/session-state.md` (or the operator-runbook equivalent already in use) describes the three subcommands, their JSON shapes, and the `lock_epoch` model. The implementation PR adds this doc.
9. **Spec 091 v1.1 retargeting**: the amendment doc in `specs/091-fix-clawta-lockdown-loop/amendments/v1.1-operator-unlock-recovery.md` is updated to reference this spec for the kernel surface, removing any inline subcommand definitions. (Done in the same PR as this spec.)
10. **CHANGELOG entry**: release notes for the next chitin-kernel version mention the new subcommands and the additive schema migration.
