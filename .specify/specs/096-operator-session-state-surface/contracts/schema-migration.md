# Contract: `agent_state` schema migration

**Spec**: 096 | **FRs**: 004, 010

## Migration scope

Adds two columns to `agent_state` in `~/.chitin/gov.db`:

- `unlock_ts TEXT` (nullable, defaults to NULL)
- `lock_epoch INTEGER NOT NULL DEFAULT 0`

All other tables (`denials`, `denial_events`) and all existing `agent_state` columns (`agent`, `total`, `locked`, `locked_ts`) are untouched.

## Trigger

Migration runs on every `OpenCounter()` invocation (or equivalent entry point). It MUST be idempotent — re-running has no observable effect after the first successful run.

## Algorithm

```text
1. Open the database (existing behavior).
2. Query: PRAGMA table_info(agent_state)
3. Walk the result rows. Collect the set of existing column names.
4. If "unlock_ts" NOT in the set:
     Execute: ALTER TABLE agent_state ADD COLUMN unlock_ts TEXT;
5. If "lock_epoch" NOT in the set:
     Execute: ALTER TABLE agent_state ADD COLUMN lock_epoch INTEGER NOT NULL DEFAULT 0;
6. Commit. (SQLite auto-commits DDL by default; no explicit BEGIN/COMMIT needed.)
```

## Idempotency contract

- Re-running the migration on a fully-migrated database MUST be a no-op (the `PRAGMA table_info` introspection prevents the `ALTER TABLE` calls from re-running).
- Re-running on a partially-migrated database (one column added, the other not) MUST add only the missing column.
- A migration interrupted by SIGKILL between the two `ALTER TABLE` calls MUST be safely re-runnable — the next invocation completes the remaining column.

## Backward compatibility contract

After migration, all existing API methods MUST continue working byte-identically for callers that don't read the new columns:

| API | Reads/Writes | Post-migration behavior |
|---|---|---|
| `Counter.RecordDenial` | reads `agent_state.total`, writes `total`, `locked`, `locked_ts` | Unchanged. Spec 096 adds `lock_epoch` advance + chain emit on the lock-transition branch. |
| `Counter.RecordActionDenial` | same as above + `denial_events` | Same. |
| `Counter.Level` | reads `total`, `locked` | Unchanged. |
| `Counter.IsLocked` | reads `locked` | Unchanged. |
| `Counter.Lockdown` | writes `locked`, `locked_ts`, `total` | Spec 096 adds `lock_epoch` advance + chain emit. Existing callers see no API surface change. |
| `Counter.Reset` | DELETEs `agent_state` row + `denials` rows + `denial_events` rows | Unchanged (FR-010). The new columns are deleted with the row. |

## Forward-compatibility note

Future schema deltas to `agent_state` (e.g., a `last_seen_ts` column) follow the same pattern — `PRAGMA table_info` introspection + `ALTER TABLE ADD COLUMN` per column. The migration helper extracted as part of this spec's implementation MUST be reusable.

## Failure modes

| Scenario | Behavior |
|---|---|
| `gov.db` doesn't exist | Existing `OpenCounter()` creates it with the full new schema (CREATE TABLE statements updated to include both new columns). No migration needed. |
| `gov.db` exists with pre-spec schema | Migration adds the two new columns. |
| `gov.db` exists with post-spec schema (already migrated) | `PRAGMA table_info` detects the columns; no ALTER runs. |
| `gov.db` corrupt | `OpenCounter()` returns an error per existing behavior; the kernel CLI exits 2. |
| Concurrent migration attempts (two kernel processes start simultaneously) | SQLite serializes writes; one wins the ALTER; the other's PRAGMA-check sees the new column and skips. Safe. |
| ALTER fails for unexpected reason (disk full, etc.) | Return error from `OpenCounter()`; kernel CLI exits 2 with the underlying message. |

## Test invariants

| Test | Assertion |
|---|---|
| `TestMigrateAddsColumns_FromOldSchema` | Fixture gov.db with pre-spec schema; after `OpenCounter()`, `PRAGMA table_info(agent_state)` includes `unlock_ts` and `lock_epoch`. |
| `TestMigrateIdempotent` | Run `OpenCounter()` twice; second invocation does NOT issue ALTER (verify via SQL trace or by asserting no error and identical schema). |
| `TestMigrateBackwardCompatibleAPIs` | Fixture gov.db with pre-spec schema and pre-existing rows; after migration, `Counter.Level`, `Counter.IsLocked`, `Counter.RecordDenial` all behave byte-identically for the existing rows. |
| `TestMigrateInterrupted` | Simulate partial migration (one column added, the other not); next invocation completes the second column without error. |
| `TestMigrateConcurrent` | Two goroutines call `OpenCounter()` simultaneously against a pre-spec fixture; both succeed; final schema has both columns exactly once. |
| `TestCreateFreshDB` | Delete the gov.db, call `OpenCounter()`; assert the new file is created with the full new schema in one shot (no need to migrate). |
