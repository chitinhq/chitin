# Implementation Plan: Operator session-state surface

**Branch**: `spec/096-session-state-surface` | **Date**: 2026-05-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/096-operator-session-state-surface/spec.md`

## Summary

The chitin-kernel today has `Counter.Lockdown()` (kill-switch) and `Counter.Reset()` (full row delete) as Go API methods on the gov.db `agent_state` table — but no CLI surface exposes either, and there's no chain event recording lock or unlock transitions. Spec 091 v1.1 surfaced the gap operationally: an in-process plugin can't recover from a sticky stop-hook without a process restart because the kernel has no operator-facing "soft unlock that preserves audit" path.

This spec adds three subcommands to the existing `chitin-kernel` binary — `session unlock`, `session lock`, `session status` — backed by two additive schema columns (`unlock_ts`, `lock_epoch`) and two new chain event types (`session_locked`, `session_unlocked`). The most subtle FR is FR-005: automatic lockdowns inside `Counter.RecordActionDenial` (the total ≥10 escalation path) MUST also advance `lock_epoch` and emit `session_locked` — without that the epoch is dishonest and consumers comparing epochs would miss auto-escalation transitions.

Implementation = (a) schema migration in `escalation.go` CREATE TABLE statement + a tiny ALTER-TABLE migrate pass for pre-existing rows; (b) three subcommands wired into the existing `cmd/chitin-kernel/main.go` dispatcher; (c) chain event emission via the canonical `chitin-kernel emit` path; (d) modification of `RecordActionDenial` to advance epoch + emit on auto-lockdown; (e) `Counter.Reset()` preserved unchanged per FR-010.

## Technical Context

**Language/Version**: Go 1.25, matching the existing `go/execution-kernel/` module.

**Primary Dependencies**: `database/sql` + `github.com/mattn/go-sqlite3` (already used by `escalation.go`); `flag` from the standard library (used by the kernel's existing subcommand framework). No new dependencies.

**Storage**: SQLite at `~/.chitin/gov.db` — schema extended with `unlock_ts TEXT NULL` and `lock_epoch INTEGER NOT NULL DEFAULT 0` columns on `agent_state`. The `denials` and `denial_events` tables are untouched.

**Testing**: `go test ./...` from `go/execution-kernel/`. Table-driven tests for argument parsing and exit codes. Schema migration test loads a fixture gov.db with pre-existing rows (no new columns) and asserts post-migration behavior is byte-identical for existing API callers (Counter.RecordDenial, Counter.Level, Counter.IsLocked).

**Target Platform**: Linux operator boxes; `chitin-kernel` runs as a per-invocation CLI (not a long-running service), invoked by the openclaw plugin and other consumers.

**Project Type**: CLI subcommands grafted onto an existing binary (matches the pattern used by spec 097 against `chitin-orchestrator`).

**Performance Goals**: `session unlock` completes in <5s wall-clock (SC-001). `session status` is read-only and returns in <100ms for a single agent. The added work in `RecordActionDenial` (FR-005 epoch advance + chain emit on auto-escalation) MUST NOT add more than 50ms to that path's existing latency, measured against the pre-spec baseline.

**Constraints**:
- Schema migration MUST be additive and backward-compatible (FR-004). Existing kernel API callers reading the old schema MUST continue working byte-identically.
- Chain emit MUST go through the existing `chitin-kernel emit` path per §1 — even though we're inside the kernel binary, the canonical emit path is the single chain-write seam (no direct chain writes from `gov/escalation.go`).
- `Counter.Reset()` MUST NOT be modified (FR-010). The new `session unlock` is a softer sibling; Reset stays as the destructive sibling.
- All three subcommands MUST honor `--policy-file` / `--db-path` override flags matching the existing kernel CLI convention (FR-007).

**Scale/Scope**: ~3 subcommands, ~9-12 implementation tasks, estimated 400-700 lines across `cmd/chitin-kernel/` and `internal/gov/`. Larger than spec 097 because of the schema migration and the RecordActionDenial modification.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| § | Rule | Verdict | Why |
|---|------|---------|-----|
| 1 | Side-effect boundary — kernel is the only chain writer | ✅ PASS — load-bearing | The new chain events flow through the existing `chitin-kernel emit` subprocess pattern even when the writer is the kernel binary itself. Preserves the invariant that ALL chain writes go through the documented emit path — no shortcut just because we're already inside the kernel process. |
| 2 | Worker + worktree discipline | ✅ PASS | Implementation runs in dedicated worktree per §2. |
| 3 | Spec-kit promotion gate | ✅ PASS | This spec exists. PR #935 is open. |
| 4 | Tracked installers | ✅ PASS (vacuous) | `chitin-kernel` already has its installer; new subcommands ride with the existing binary. |
| 5 | Board-aware scripts | ✅ PASS (vacuous) | No kanban interaction. |
| 6 | Swarm tooling is the exception | ✅ PASS | New code under `go/execution-kernel/`, not `swarm/`. |
| 7 | The swarm is the orchestrator | ✅ PASS | Operator-facing infrastructure supporting orchestrator-driven implementations; does not introduce any driver-bypass surface. |

**Initial gate verdict**: 7/7 PASS. No complexity tracking entries.

**Post-design recheck**: stays 7/7. The chosen approach introduces no new layers.

## Project Structure

### Documentation (this feature)

```text
specs/096-operator-session-state-surface/
├── spec.md                       # committed
├── plan.md                       # this file
├── research.md                   # Phase 0 — design decisions
├── data-model.md                 # Phase 1 — schema, payloads, CLI shapes
├── quickstart.md                 # Phase 1 — verification recipe
├── contracts/
│   ├── unlock-subcommand.md      # chitin-kernel session unlock contract
│   ├── lock-subcommand.md        # chitin-kernel session lock contract
│   ├── status-subcommand.md      # chitin-kernel session status contract
│   ├── chain-events.md           # session_locked + session_unlocked schemas
│   └── schema-migration.md       # additive agent_state delta + migration rules
├── checklists/
│   └── requirements.md           # committed
└── tasks.md                      # Phase 2 — created by /speckit-tasks
```

### Source code (this feature touches)

```text
go/execution-kernel/
├── cmd/chitin-kernel/
│   ├── main.go                       # MODIFIED — dispatcher routes session.* subcommands
│   ├── session.go                    # NEW — session subcommand parent + dispatcher
│   ├── session_unlock.go             # NEW — unlock handler
│   ├── session_lock.go               # NEW — operator kill-switch CLI
│   ├── session_status.go             # NEW — status handler (list + inspect modes)
│   └── *_test.go                     # NEW — handler tests
├── internal/gov/
│   ├── escalation.go                 # MODIFIED — schema additions + FR-005 auto-escalation hook
│   ├── escalation_test.go            # MODIFIED — schema migration + epoch + chain emit tests
│   ├── session.go                    # NEW — Counter.Unlock(agent, reason), Counter.LockEpoch(agent), Counter.Status Go API
│   └── session_test.go               # NEW — Go API tests
docs/operator/
└── session-state.md                  # NEW — operator runbook
```

### Files explicitly UNTOUCHED

```text
go/execution-kernel/internal/gov/policy.go      # policy evaluation unchanged
go/execution-kernel/internal/gov/budget.go      # budget logic unchanged
apps/openclaw-plugin-governance/                # spec 091 v1.1 is the consumer; lives in its own PR
```

**Structure Decision**: single-binary edit on `chitin-kernel`. The implementation adds session subcommands to the existing kernel CLI; the schema migration is additive — existing tables and existing API callers continue working unchanged.

## Phase 2 Execution Strategy (preview — owned by /speckit-tasks)

Estimated 12 tasks across 5 phases:

1. **Setup** — schema migration logic in `escalation.go` + dispatcher scaffold
2. **Foundational** — Counter.* Go API extensions (Unlock, LockEpoch, Status) + RecordActionDenial modification + chain emit helper + Counter.Lockdown epoch instrumentation
3. **US1 (unlock + audit)** — `session unlock` subcommand + `session lock` operator kill-switch + tests
4. **US2 (consumer-facing status query)** — `session status -agent <id>` inspect mode + epoch semantics validation tests
5. **US3 (operator status listing)** — `session status` no-arg list mode + --text output + tests
6. **Polish** — operator runbook + CHANGELOG + quickstart end-to-end

## Risk flags (handed off to /speckit-tasks)

1. **R1 — Schema migration ordering**: pre-existing gov.db files may have rows in `agent_state` with no `unlock_ts` / `lock_epoch` columns. The migration must (a) add the columns with backward-compatible defaults, (b) NOT crash on a database that's already been migrated (idempotent re-run), (c) NOT corrupt data on a partial migration interrupted by SIGKILL. Mitigation: introspect via `PRAGMA table_info`; transactionally add columns; idempotency via "are the columns there already?" check before the migration runs.

2. **R2 — Concurrent epoch advance**: two operators (or one operator + one auto-escalation event) advance `lock_epoch` for the same agent within milliseconds. SQLite serializes transactions, but the order isn't deterministic relative to operator intent. Consequence: rare race where the first event's chain emission sees a lower epoch than the second's gov.db write. Mitigation: read-after-write inside the same transaction — emit chain event with the epoch read after the UPDATE, not before. Document as acceptable behavior in the contracts.

3. **R3 — Self-call chain emit recursion**: `chitin-kernel session unlock` shells out to `chitin-kernel emit`. The emit path itself goes through `gov/` reads but does NOT call back into session_*. No actual recursion — but flag it loudly in implementation comments to prevent future code from accidentally creating a cycle.

4. **R4 — RecordActionDenial latency**: FR-005 adds a chain emit subprocess to the auto-escalation path. Acceptable upper bound: 50ms (Performance Goals). Mitigation: only emit on the transition (when locked flips from 0 to 1), not on every RecordActionDenial call. Most denials don't cross the threshold; the cost is amortized.

5. **R5 — Chain emit failure during RecordActionDenial**: if chain emit fails during auto-escalation, do we fail the denial recording? Per the spec's general posture (matches spec 091 FR-009 and spec 097 D8): no. The lockdown is persisted; the chain entry is lost; warn-and-continue.

6. **R6 — `Counter.Lockdown()` Go API call path**: the existing in-process Go API kill-switch must also advance `lock_epoch` and emit `session_locked` with `source: "operator_go_api"`. Otherwise in-process Go callers produce locks that don't appear in the chain.

## Complexity Tracking

No constitution violations. This section is intentionally empty.
