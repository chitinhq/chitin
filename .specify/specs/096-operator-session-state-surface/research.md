# Phase 0 Research: Operator session-state surface

**Feature**: 096-operator-session-state-surface
**Date**: 2026-05-23
**Status**: All NEEDS CLARIFICATION resolved. Design grounded in the existing `gov/escalation.go` implementation reviewed during the 2026-05-23 sweep that motivated spec 091 v1.1.

## Decision summary

| ID | Decision | Confidence |
|---|---|---|
| D1 | Subcommands live on the existing `chitin-kernel` binary (`session unlock`, `session lock`, `session status`) | High |
| D2 | Additive schema delta: `unlock_ts TEXT NULL` + `lock_epoch INTEGER NOT NULL DEFAULT 0` | High |
| D3 | `lock_epoch` advances on EVERY lock transition (auto-escalation OR operator CLI OR Go API kill-switch) and EVERY unlock | High |
| D4 | Two chain event types — `session_locked` (covers all lock sources via `source` field) and `session_unlocked` | High |
| D5 | Idempotent unlock-of-unlocked emits a chain event but does NOT advance `lock_epoch` | High |
| D6 | Chain emit goes through `chitin-kernel emit` even from within the kernel binary (preserves §1's single chain-write seam) | High |
| D7 | `Counter.Reset()` preserved unchanged (FR-010); destructive wipe stays as a separate API | High |
| D8 | Migration is idempotent via `PRAGMA table_info` introspection | High |
| D9 | Chain emit failure during auto-escalation logs warn and continues — lock state in gov.db is the source of truth | High |
| D10 | Concurrent epoch advance: read-after-write inside the same SQL transaction | High |

## D1 — Subcommands on `chitin-kernel`

**Decision**: add `session unlock`, `session lock`, `session status` as subcommands on the existing `chitin-kernel` binary at `go/execution-kernel/cmd/chitin-kernel/main.go`.

**Rationale**:
- `gov.db` is owned by the kernel; the binary that writes it should expose the operator surface that mutates it.
- The kernel already has a subcommand framework (`emit`, `gate evaluate`, `gate hook`, `speckit-lint`). Adding three more subcommands is a clean fit.
- Constitution §1 is preserved — chain emission stays inside the kernel.

**Alternatives considered**:
- Subcommands on `chitin-orchestrator` — rejected. The orchestrator is the dispatch/runtime side; session state is policy/governance side. Separating them matches the §1 boundary.
- A new `chitin-session` binary — rejected. Adds an install step and a binary to track. No upside.

## D2 — Additive schema delta

**Decision**: add two columns to `agent_state`:

```sql
ALTER TABLE agent_state ADD COLUMN unlock_ts TEXT;
ALTER TABLE agent_state ADD COLUMN lock_epoch INTEGER NOT NULL DEFAULT 0;
```

**Rationale**:
- The current schema (4 columns: agent, total, locked, locked_ts) is too thin for the recovery semantics.
- `unlock_ts` records when the agent transitioned from locked to unlocked. `locked_ts` records the most recent lock. Together they bracket the lockdown window.
- `lock_epoch` is the load-bearing transition counter — consumers use it to detect generation changes without depending on timestamps (which can fall victim to clock skew).
- Both columns are nullable / have safe defaults so existing rows survive the migration.

**Alternatives considered**:
- A separate `agent_locks` table with one row per lock event — rejected. Adds a join for the common "is this agent locked" query; the live state we care about is one-row-per-agent.
- Encoding epoch as `total << 16 | epoch` — rejected. Saves a column at the cost of clarity; gov.db is small, columns are cheap.

## D3 — Epoch semantics

**Decision**: `lock_epoch` is a monotonically-increasing per-agent integer. It advances by 1 on EVERY lock transition (whether the source is `RecordActionDenial`'s auto-escalation, `Counter.Lockdown()` Go API, or `chitin-kernel session lock` CLI) AND by 1 on EVERY unlock (whether `chitin-kernel session unlock` CLI or `Counter.Reset()` Go API).

**Rationale**:
- Without consistent epoch advance across ALL transitions, consumers comparing epochs would miss some real transitions and falsely treat the agent as unchanged.
- Two scenarios make this critical:
  1. Plugin caches `lock_epoch=5` at lock-set. Auto-escalation fires (would-be epoch 6) then operator unlocks (epoch 7). If auto-escalation didn't advance epoch, plugin sees epoch 6 and clears — but really there's a NEW lock now. Wrong recovery.
  2. Two operators rapidly lock/unlock the same agent. Without per-transition epoch advance, the second operator's lock could overwrite the first's without a visible chain event.

**Counter-decision (D5)**: idempotent unlock (operator unlocks an already-unlocked agent) does NOT advance epoch. The chain event IS still emitted — for forensic completeness — but the epoch is unchanged. This prevents "noise advances" from confusing consumers.

## D4 — Two chain event types

**Decision**: `session_locked` covers ALL lock transitions via a `source` field (`"auto_escalation" | "operator_cli" | "operator_go_api"`). `session_unlocked` covers all unlocks. No more granular event types.

**Rationale**:
- The `source` field provides the discrimination chain consumers need without exploding the event-type surface.
- Spec 091 v1.1's consumer (the openclaw plugin) only cares "did the lock state change" — it doesn't distinguish sources. Future consumers that DO care can filter on the field.

**Alternatives considered**:
- Three event types (`session_locked_auto`, `session_locked_operator_cli`, `session_locked_operator_go_api`) — rejected. More event-type surface to maintain; less queryable.

## D5 — Idempotent unlock semantics

**Decision**: `chitin-kernel session unlock -agent X` against an already-unlocked X succeeds (exit 0), emits a `session_unlocked` chain event with the operator-supplied reason, but does NOT advance `lock_epoch`.

**Rationale**:
- Operators may run unlock as a safety re-run; rejecting an idempotent operation would be hostile UX.
- The chain event IS still emitted because the operator action happened (forensic completeness).
- Epoch not advancing keeps consumers' transition detection clean — an unlock-of-unlocked isn't a transition.

## D6 — Chain emit through canonical path

**Decision**: even though we're inside the `chitin-kernel` binary, `session unlock` / `session lock` / auto-escalation chain emissions go through the canonical `chitin-kernel emit` subprocess (or its in-process equivalent factored out as a shared library function). NO direct chain writes from `gov/escalation.go`.

**Rationale**:
- Constitution §1: "the kernel is the only chain writer" — specifically, the chain-emit code path is the single seam. Bypassing it just because we're inside the kernel binary creates two write paths, both must be audited, and skew becomes possible.
- Refactor for cleanliness: extract `chitin-kernel emit`'s core logic into an internal package (`internal/emit`) that both the CLI subcommand AND `gov/escalation.go` call. The CLI subcommand is then a thin wrapper; auto-escalation calls the same function in-process.

**Alternatives considered**:
- Direct chain writes from `escalation.go` — rejected. Violates §1's invariant.
- Subprocess-only chain writes (even from inside the kernel) — rejected. The 10-50ms subprocess overhead on every auto-escalation is excessive. The in-process variant of the same code path is the right trade-off.

## D7 — `Counter.Reset()` preserved

**Decision**: do not modify `Counter.Reset()`. It continues to DELETE the entire `agent_state` row + the agent's `denials` and `denial_events`. The new `session unlock` is the softer sibling.

**Rationale**:
- `Reset()` is used by test fixtures and operator "decommission this agent" gestures. Preserving its existing destructive semantics avoids breaking those callers.
- `session unlock` is the audit-preserving operation; `Reset()` is the audit-clearing operation. Different tools for different jobs.
- Per FR-010: implementers should NOT consolidate the two.

## D8 — Idempotent migration

**Decision**: the schema migration runs on every kernel invocation but is idempotent — it queries `PRAGMA table_info(agent_state)` to detect whether the new columns are already present and skips the `ALTER TABLE` if they are.

**Rationale**:
- The kernel is invoked many times per minute by openclaw and other consumers. Migration cannot be a one-shot — it must be safe to re-run.
- SQLite's `ALTER TABLE ADD COLUMN` is fast for an empty add (just metadata), but failing on already-present columns would crash the kernel on every call after the first invocation.
- Introspection via `PRAGMA table_info` is the canonical idempotency check.

## D9 — Chain emit failure during auto-escalation

**Decision**: if `RecordActionDenial` triggers auto-escalation (total ≥10 → locked=1) and the chain emit fails, the lock state in gov.db is the source of truth. Log a warning; the kernel continues. Do NOT roll back the lock state.

**Rationale**:
- The lockdown is a safety operation. Rolling it back because telemetry failed would leave a dangerous agent unlocked. Worse than losing a chain entry.
- Matches spec 091 FR-009 and spec 097 D8 — telemetry never blocks the load-bearing action.

## D10 — Concurrent epoch advance handling

**Decision**: epoch advances happen inside the same SQL transaction as the lock/unlock UPDATE. The chain event payload reads the post-UPDATE `lock_epoch` value to ensure the chain entry and gov.db are consistent.

**Rationale**:
- SQLite serializes transactions; the read-after-write pattern guarantees the emitted epoch equals what gov.db now holds.
- The transaction boundary is the consistency anchor: anything outside (subprocess emit, in-process emit, return to caller) sees a committed, stable epoch.

## No remaining NEEDS CLARIFICATION

All technical context items in `plan.md` resolve to concrete values. No `[NEEDS CLARIFICATION]` markers in the spec or plan.
