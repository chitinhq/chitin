# Quickstart: spec 097 — verification recipe

**Feature**: 097-operator-scheduler-entrypoint
**Date**: 2026-05-23
**Purpose**: a step-by-step recipe an operator (or CI smoke test) can run to exercise the schedule → status → cancel round-trip against a fixture spec, asserting the success criteria from `spec.md` (SC-001 through SC-006).

## Prerequisites

```bash
# Temporal dev server running
pgrep -f 'temporal server start-dev' || temporal server start-dev --ui-port 8233 &

# chitin-orchestrator binary built with spec 097 implementation
cd ~/workspace/chitin/go/orchestrator && go build -o ~/.local/bin/chitin-orchestrator ./cmd/chitin-orchestrator

# Worker host running (or invoke once for this verification)
pgrep -f 'chitin-orchestrator$' || systemctl --user start chitin-orchestrator.service
```

## Fixture

The implementation PR ships a fixture spec at `go/orchestrator/cmd/chitin-orchestrator/testdata/097-fixture/`. Layout:

```text
097-fixture/
├── spec.md           # minimal spec.md passing speckit-lint
├── plan.md
├── tasks.md          # 3-4 tasks, mapped capabilities, one [P] parallel marker
└── checklists/
    └── requirements.md
```

The fixture's `tasks.md` is intentionally small (3-4 tasks) and uses capability keywords that all map to `code.implement` (declared by claudecode + codex + openclaw drivers) so dispatch is guaranteed to find a driver in the default registry.

## Step 1 — Schedule the fixture

```bash
chitin-orchestrator schedule 097-fixture --repo-root ~/workspace/chitin/go/orchestrator/cmd/chitin-orchestrator/testdata
```

**Expected**:

- Exit code `0`
- stdout matches `scheduled spec 097-fixture (3 nodes, 1 capabilities required); run_id=<uuid>`
- A new workflow appears in the Temporal UI at `http://localhost:8233/`
- A `scheduler_started` event appears in the chain:

  ```bash
  jq 'select(.event_type=="scheduler_started" and .payload.spec_ref=="097-fixture")' ~/.chitin/events-*.jsonl | tail -1
  ```

**Asserts SC-001** (one-shot schedule from a freshly-merged spec in under 10 seconds).

## Step 2 — List active scheduler runs

```bash
chitin-orchestrator status
```

**Expected**:

- Exit code `0`
- stdout is a JSON array containing at least one entry whose `spec_ref` is `097-fixture` and `run_id` matches what step 1 printed.

```bash
chitin-orchestrator status --text
```

**Expected**:

- Exit code `0`
- stdout is a fixed-column table with the same row.

## Step 3 — Inspect the single run

```bash
RUN_ID=<the run_id from step 1>
chitin-orchestrator status -run-id $RUN_ID
```

**Expected**:

- Exit code `0`
- stdout is a JSON object matching `SchedulerStatus`:

  ```json
  {
    "run_id": "<RUN_ID>",
    "tick": <some integer >= 1>,
    "node_status": {
      "task-1": "<status>",
      "task-2": "<status>",
      "task-3": "<status>"
    },
    "frontier": ["..."]
  }
  ```

**Asserts SC-003** (status returns non-stale view within 5s of the last node transition).

## Step 4 — Cancel the run

```bash
chitin-orchestrator cancel -run-id $RUN_ID -reason "spec 097 quickstart verification"
```

**Expected**:

- Exit code `0`
- stdout matches `canceled run_id=<RUN_ID> reason="spec 097 quickstart verification"`
- A `scheduler_canceled` event appears in the chain:

  ```bash
  jq 'select(.event_type=="scheduler_canceled" and .payload.run_id=="'$RUN_ID'")' ~/.chitin/events-*.jsonl | tail -1
  ```

**Asserts SC-004** (cancel honored within 30 seconds).

## Step 5 — Verify cancel idempotency

```bash
chitin-orchestrator cancel -run-id $RUN_ID  # second invocation against the same run
```

**Expected**:

- Exit code `1` (already-terminal)
- stderr matches `error: run_id "<RUN_ID>" already in terminal state "Canceled"` (or `"Completed"` if the workflow happened to finish before the cancel was honored)
- No second `scheduler_canceled` chain event for this run.

## Step 6 — Verify error paths (SC-005)

```bash
# Bad spec ref:
chitin-orchestrator schedule 999-doesnotexist
# expected: exit 1, stderr contains "no spec matching"
echo "exit=$?"

# Ambiguous ref:
chitin-orchestrator schedule 09 --repo-root ~/workspace/chitin
# expected: exit 1, stderr lists candidates
echo "exit=$?"

# Temporal unreachable:
TEMPORAL_HOSTPORT=127.0.0.1:9999 chitin-orchestrator schedule 097-fixture --repo-root ~/workspace/chitin/go/orchestrator/cmd/chitin-orchestrator/testdata
# expected: exit 2, stderr contains "Temporal unreachable"
echo "exit=$?"
```

**Asserts SC-005** (three exit codes are mutually exclusive and accurate).

## Step 7 — Verify chain-emit fail-soft (per D8)

```bash
# Rename the kernel binary so emit fails:
sudo mv /usr/local/bin/chitin-kernel /usr/local/bin/chitin-kernel.disabled
# (or wherever chitin-kernel actually lives; check `which chitin-kernel` first)

# Schedule still succeeds despite chain emit failure:
chitin-orchestrator schedule 097-fixture --repo-root ~/workspace/chitin/go/orchestrator/cmd/chitin-orchestrator/testdata
# expected: exit 0, stdout has the success line, stderr has "warning: chain emit failed: ..."
echo "exit=$?"

# Restore:
sudo mv /usr/local/bin/chitin-kernel.disabled /usr/local/bin/chitin-kernel
```

**Asserts D8** — chain emit failure logs a warning but does not roll back the user-visible action.

## Cleanup

```bash
# Cancel any leftover fixture runs:
chitin-orchestrator status | jq -r '.[] | select(.spec_ref=="097-fixture") | .run_id' | while read rid; do
  chitin-orchestrator cancel -run-id "$rid" -reason "quickstart cleanup"
done
```

## What this DOESN'T verify

- **SC-002** (every implementation work-unit traces to a `scheduler_started` event): a one-shot verification can't verify a temporal-property invariant. SC-002 is verified by the sentinel or a separate audit script running over a representative time window after the spec ships.
- **SC-006** (operators stop using ad-hoc `temporal workflow start`): a behavioral claim about operator habits; not testable here.

Those two SCs are verified out-of-band per their definitions in `spec.md`.
