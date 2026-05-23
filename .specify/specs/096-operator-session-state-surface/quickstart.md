# Quickstart: spec 096 — verification recipe

**Feature**: 096-operator-session-state-surface
**Date**: 2026-05-23
**Purpose**: step-by-step recipe exercising the lock → status → unlock → status round-trip plus the auto-escalation chain-emit path, asserting SC-001 through SC-005.

## Prerequisites

```bash
# chitin-kernel binary built with spec 096 implementation
cd ~/workspace/chitin/go/execution-kernel
go build -o ~/.local/bin/chitin-kernel ./cmd/chitin-kernel

# Use a sandbox gov.db so we don't perturb the operator's real state
export TEST_DB=$(mktemp /tmp/spec096-XXXXXX.db)
trap 'rm -f "$TEST_DB"' EXIT
```

## Step 1 — Verify schema migration on a fresh database

```bash
# First invocation creates the gov.db with the full new schema
chitin-kernel session status --db-path "$TEST_DB"
echo "exit=$?"
# expected: exit 0, stdout "[]" (empty array — no agents yet)
```

```bash
# Confirm the new columns exist
sqlite3 "$TEST_DB" "PRAGMA table_info(agent_state);" | grep -E "unlock_ts|lock_epoch"
# expected: two lines confirming both columns present
```

**Asserts SC-004** (schema migration succeeds on a fresh database).

## Step 2 — Lock an agent via operator CLI

```bash
chitin-kernel session lock -agent test-agent --db-path "$TEST_DB" -reason "spec 096 quickstart verification"
echo "exit=$?"
# expected: exit 0, stdout matches "locked agent=test-agent lock_epoch=1 reason=\"spec 096 quickstart verification\""
```

```bash
# Verify the lock landed
chitin-kernel session status -agent test-agent --db-path "$TEST_DB"
# expected JSON: {"agent":"test-agent", "locked":true, "lock_epoch":1, "level":"lockdown", ...}
```

```bash
# Verify the chain event landed
jq 'select(.event_type=="session_locked" and .payload.agent=="test-agent")' ~/.chitin/events-*.jsonl | tail -1
# expected: an event with payload.source="operator_cli", lock_epoch_after=1
```

**Asserts SC-001** part 1 — operator CLI lock works in under 5 seconds.

## Step 3 — Unlock the agent (preserves audit history)

```bash
chitin-kernel session unlock -agent test-agent --db-path "$TEST_DB" -reason "test complete"
echo "exit=$?"
# expected: exit 0, stdout "unlocked agent=test-agent lock_epoch=2 reason=\"test complete\""
```

```bash
# Verify the unlock landed and total is preserved
chitin-kernel session status -agent test-agent --db-path "$TEST_DB"
# expected JSON: locked=false, lock_epoch=2, total=10 (NOT cleared — Reset would clear, unlock preserves)
```

```bash
# Verify the chain event
jq 'select(.event_type=="session_unlocked" and .payload.agent=="test-agent")' ~/.chitin/events-*.jsonl | tail -1
# expected: payload.lock_epoch_after=2, payload.reason="test complete", payload.total_at_unlock=10
```

**Asserts SC-001** part 2 — operator unlock works in under 5 seconds and preserves audit history.

## Step 4 — Idempotent unlock (D5 behavior)

```bash
chitin-kernel session unlock -agent test-agent --db-path "$TEST_DB" -reason "safety re-run"
echo "exit=$?"
# expected: exit 0, stdout "unlocked agent=test-agent lock_epoch=2 (was already unlocked)"
# Note: lock_epoch is UNCHANGED (still 2), but a chain event IS emitted
```

```bash
# Verify epoch did NOT advance
chitin-kernel session status -agent test-agent --db-path "$TEST_DB" | jq '.lock_epoch'
# expected: 2 (unchanged)
```

```bash
# Verify the chain event was still emitted (forensic completeness)
jq -c 'select(.event_type=="session_unlocked" and .payload.agent=="test-agent")' ~/.chitin/events-*.jsonl | wc -l
# expected: 2 (one from step 3, one from this step)
```

## Step 5 — Auto-escalation produces chain event with source="auto_escalation"

This step requires forcing 10 denials. The cleanest way is via a test fixture, but it's possible to drive from the CLI:

```bash
# Use the kernel's recordDenial helper if exposed, OR force 10 denials via the policy gate
# Below assumes the implementation exposes a test-only `__test record-denial` subcommand;
# adjust to whatever the implementation lands.
for i in {1..10}; do
  chitin-kernel __test record-denial --db-path "$TEST_DB" -agent auto-test -fp "fp-$i"
done

# Verify auto-escalation locked the agent
chitin-kernel session status -agent auto-test --db-path "$TEST_DB" | jq '.locked'
# expected: true

# Verify the chain event with source="auto_escalation"
jq -c 'select(.event_type=="session_locked" and .payload.source=="auto_escalation" and .payload.agent=="auto-test")' ~/.chitin/events-*.jsonl | tail -1
# expected: one such event present
```

**Asserts FR-005** (auto-escalation emits `session_locked` with source="auto_escalation").

## Step 6 — Status list mode + table output

```bash
chitin-kernel session status --db-path "$TEST_DB"
# expected: JSON array sorted by agent ASCII, containing test-agent and auto-test

chitin-kernel session status --db-path "$TEST_DB" --text
# expected: fixed-column table with header + rows
```

```bash
# Determinism check — list output should be stable across re-runs
chitin-kernel session status --db-path "$TEST_DB" > /tmp/a.json
chitin-kernel session status --db-path "$TEST_DB" > /tmp/b.json
diff /tmp/a.json /tmp/b.json
echo "diff exit=$?"
# expected: diff exit=0 (no differences)
```

## Step 7 — Negative paths (SC-005)

```bash
# Unknown agent on unlock:
chitin-kernel session unlock -agent nonexistent --db-path "$TEST_DB" 2>&1 | tee /dev/stderr
echo "exit=$?"
# expected: exit 1, stderr contains 'no agent_state row for "nonexistent"'

# Unknown agent on status:
chitin-kernel session status -agent nonexistent --db-path "$TEST_DB" 2>&1
echo "exit=$?"
# expected: exit 1

# Corrupt db-path:
chitin-kernel session status --db-path /dev/null 2>&1
echo "exit=$?"
# expected: exit 2
```

**Asserts SC-005** (qualitative — operator-readable errors with stable exit codes).

## Step 8 — Verify backward compatibility (SC-004)

```bash
# Copy a pre-spec gov.db fixture (or simulate one by manually creating a row with no new columns)
cp testdata/pre-096-fixture.db /tmp/legacy.db

# Spec 096 chitin-kernel opens the legacy db; migration runs
chitin-kernel session status --db-path /tmp/legacy.db
echo "exit=$?"
# expected: exit 0, JSON array of the pre-existing rows
```

```bash
# Confirm pre-existing rows have lock_epoch=0 and unlock_ts=null
chitin-kernel session status --db-path /tmp/legacy.db | jq '.[] | {agent, lock_epoch, unlock_ts}'
# expected: every row has lock_epoch=0 and unlock_ts=null
```

```bash
# Confirm existing API calls still work (use a test harness or the Counter API):
# (Equivalent of: Counter.RecordDenial, Counter.Level, Counter.IsLocked)
go test ./go/execution-kernel/internal/gov -run TestMigrateBackwardCompatibleAPIs
# expected: PASS
```

## Cleanup

```bash
rm -f "$TEST_DB" /tmp/legacy.db /tmp/a.json /tmp/b.json
```

## What this DOESN'T verify

- **SC-002** (transition detection over 1000 lock/unlock cycles) — load test better suited to `go test -count=1000` than a manual recipe.
- **SC-003** (spec 091 v1.1 consumer recovers): consumer-side; verified by spec 091 v1.1's own quickstart after that spec ships.
- **SC-005** (operator stops using ad-hoc sqlite queries): behavioral; not testable here.
