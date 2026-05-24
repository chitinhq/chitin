# Implementation Plan: Operator entrypoint for the spec-DAG scheduler

**Branch**: `spec/097-operator-scheduler-entrypoint` | **Date**: 2026-05-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/097-operator-scheduler-entrypoint/spec.md`

## Summary

The Chitin Orchestrator's spec-DAG scheduler (spec 076), driver registry (spec 075), and spec-kit adapter (spec 077) are implemented and registered in `cmd/chitin-orchestrator/main.go`. But the only production caller of `client.ExecuteWorkflow` against `SchedulerWorkflow` is the test suite — a sweep on 2026-05-23 found no operator-facing path that takes a spec ref and starts a scheduler run. Constitution §7's "implementation MUST flow through the orchestrator" is consequently aspirational.

This spec adds three subcommands to the existing `chitin-orchestrator` binary — `schedule`, `status`, `cancel` — wrapping `speckit.New().CompileSpec(repoRoot, specRef)` + DAG validation + Temporal client calls. No new binary, no new dependency, no changes to the scheduler workflow or the spec-kit adapter. Implementation = a subcommand dispatcher in `main.go` plus three handler functions, two new chain event types emitted via `chitin-kernel emit`, fixture-based round-trip tests, and a one-page operator runbook.

## Technical Context

**Language/Version**: Go 1.25, matching `go/orchestrator/` (the existing module).

**Primary Dependencies**: `go.temporal.io/sdk/client` (already imported by `cmd/chitin-orchestrator/main.go`), `github.com/chitinhq/chitin/go/orchestrator/adapter/speckit` (already in the module), `github.com/chitinhq/chitin/go/orchestrator/driver` (registry; already in the module). No new dependencies.

**Storage**: None. Spec source is read from `.specify/specs/` via the spec-077 adapter; workflow state lives in Temporal (the existing server); chain events flow to `~/.chitin/events-*.jsonl` via the existing `chitin-kernel emit` path.

**Testing**: `go test ./...` from `go/orchestrator/`. Table-driven tests for argument parsing and exit codes. Fixture spec under `cmd/chitin-orchestrator/testdata/097-fixture/` exercising the full schedule → status → cancel round-trip against a local Temporal dev server (matching the pattern in `go/orchestrator/workflows/scheduler_test.go`).

**Target Platform**: Linux operator boxes running `chitin-orchestrator.service` (already deployed via `swarm/bin/install-chitin-orchestrator.sh`).

**Project Type**: CLI subcommands grafted onto an existing long-running service binary. The binary keeps its worker-host default behavior when invoked without a subcommand (FR-001).

**Performance Goals**: `schedule` completes in <10s wall-clock for a 10-node DAG (SC-001 — compile + validate + ExecuteWorkflow). `cancel` is honored at the next scheduler tick (≤30s default, SC-004). `status` returns a non-stale view within 5s of the last node-status transition (SC-003).

**Constraints**:
- MUST preserve the existing worker-host default (no-args invocation continues to register workflows + activities and poll the task queue). The subcommand dispatcher is a pure addition.
- MUST NOT modify spec 076 (`SchedulerWorkflow`, `SchedulerStatus` query, `SchedulerInput` shape) or spec 077 (`CompileSpec`, `MapCapability`). Implementation is a pure consumer.
- Chain emission for `scheduler_started` / `scheduler_canceled` MUST flow through `chitin-kernel emit -event-json -` — the kernel is the only chain writer per constitution §1.
- DAG validation MUST refuse to dispatch a DAG with `NeedsClarification` capabilities or unroutable capabilities. Better to fail fast than to start a workflow that's destined for `blocked-unroutable`.

**Scale/Scope**: ~3 subcommands, ~5-7 implementation tasks, estimated 250-400 lines in `cmd/chitin-orchestrator/`. The spec is small by design — this is a thin glue layer over already-implemented machinery.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Evaluated against `.specify/memory/constitution.md` §1-§7:

| § | Rule | Verdict | Why |
|---|------|---------|-----|
| 1 | Side-effect boundary — kernel is the only chain writer | ✅ PASS | New chain events (`scheduler_started`, `scheduler_canceled`) flow through `chitin-kernel emit`. The new subcommands write to Temporal (workflow start/cancel), not to the chain directly. |
| 2 | Worker + worktree discipline | ✅ PASS | Implementation runs in dedicated worktree per §2. This spec doesn't write code that runs in worker context — it writes operator-facing CLI subcommands; the scheduler the CLI triggers already creates per-node worktrees via `WorkUnitWorkflow` (spec 076 FR-008). |
| 3 | Spec-kit promotion gate | ✅ PASS | This spec exists. PR #936 is open. |
| 4 | Tracked installers | ✅ PASS (vacuous) | The `chitin-orchestrator` binary already has its installer at `swarm/bin/install-chitin-orchestrator.sh`. New subcommands ride with the existing binary; no new installer needed. |
| 5 | Board-aware scripts | ✅ PASS (vacuous) | This spec does NOT touch any kanban code; FR-012 explicitly forbids it. |
| 6 | Swarm tooling is the exception | ✅ PASS | New code lives under `go/orchestrator/cmd/chitin-orchestrator/`, not `swarm/`. |
| **7** | **The swarm is the orchestrator** | ✅ **PASS — load-bearing** | This spec is the missing piece that makes §7 enforceable. It is the operator surface through which implementation work enters the orchestrator. Without it, §7's "implementation MUST flow through the orchestrator" is unenforceable because operators have no specced path to start a flow. With it, every implementation work-unit can trace its origin to a `scheduler_started` chain event. |

**Initial gate verdict**: 7/7 PASS. No complexity tracking entries required.

**Post-design recheck**: stays 7/7. The chosen approach (subcommand dispatcher in the existing binary + pure-consumer of spec 076/077 interfaces + chain emission via existing kernel path) introduces no new layers, frameworks, or bypasses.

## Project Structure

### Documentation (this feature)

```text
specs/097-operator-scheduler-entrypoint/
├── spec.md                       # committed (with FR-001..FR-012 + ASCs)
├── plan.md                       # this file
├── research.md                   # Phase 0 — design decisions captured
├── data-model.md                 # Phase 1 — entities (subcommand inputs/outputs, chain events)
├── quickstart.md                 # Phase 1 — verification recipe (schedule → status → cancel round-trip)
├── contracts/
│   ├── schedule-subcommand.md    # the schedule CLI contract
│   ├── status-subcommand.md      # the status CLI contract
│   ├── cancel-subcommand.md      # the cancel CLI contract
│   └── chain-events.md           # scheduler_started + scheduler_canceled schemas
├── checklists/
│   └── requirements.md           # committed
└── tasks.md                      # Phase 2 — created by /speckit-tasks
```

### Source code (this feature touches)

```text
go/orchestrator/
├── cmd/chitin-orchestrator/
│   ├── main.go                   # MODIFIED — subcommand dispatcher; preserves no-args worker-host default
│   ├── schedule.go               # NEW — schedule subcommand handler
│   ├── status.go                 # NEW — status subcommand handler
│   ├── cancel.go                 # NEW — cancel subcommand handler
│   ├── validate.go               # NEW — DAG pre-validation (NeedsClarification + unroutable checks)
│   ├── emit.go                   # NEW — chain-event emission helper (wraps `chitin-kernel emit`)
│   ├── *_test.go                 # NEW — table-driven tests + fixture-based round-trip
│   └── testdata/
│       └── 097-fixture/
│           ├── spec.md           # fixture spec for round-trip test
│           ├── plan.md
│           ├── tasks.md          # small DAG (3-4 tasks) with mapped capabilities
│           └── checklists/requirements.md
docs/operator/
└── scheduling.md                 # NEW — operator runbook for the three subcommands
```

### Files explicitly UNTOUCHED

```text
go/orchestrator/workflows/scheduler.go          # spec 076; pure consumer here
go/orchestrator/workflows/work_unit.go          # spec 076; pure consumer here
go/orchestrator/activities/select_driver.go     # spec 075/076; pure consumer here
go/orchestrator/adapter/speckit/adapter.go      # spec 077; pure consumer here
go/orchestrator/driver/registry.go              # spec 075; pure consumer here
go/orchestrator/driver/taxonomy.go              # spec 075; capability constants reused unchanged
```

**Structure Decision**: single-binary edit. The implementation adds subcommand handlers to the existing `chitin-orchestrator` binary; it does NOT add a new binary or a new package outside `cmd/chitin-orchestrator/`. The worker-host default behavior is preserved (no-args invocation still registers workflows + polls the task queue). This is the minimum-surface-area change that closes the trigger gap.

## Phase 2 Execution Strategy (preview — owned by /speckit-tasks)

Estimated 6-8 tasks, all in a single worktree partition:

1. **Subcommand dispatcher in `main.go`** — parse argv, route to handlers, preserve no-args worker-host default.
2. **`schedule` handler** — wraps `speckit.New().CompileSpec` + DAG validation + `ExecuteWorkflow` + chain emit.
3. **`status` handler** — `ListWorkflows` (no `-run-id`) + `Query` (with `-run-id`) + JSON/text output.
4. **`cancel` handler** — `CancelWorkflow` + idempotency check + chain emit.
5. **DAG validation** — refuse `NeedsClarification` capabilities; refuse capabilities no registered driver declares.
6. **Chain emit helper** — wraps `chitin-kernel emit -event-json -`; fail-soft (log + continue) on emit failure.
7. **Fixture spec + round-trip integration test** — `cmd/chitin-orchestrator/testdata/097-fixture/` + a test that exercises `schedule → status → cancel` against a local Temporal dev server.
8. **Operator runbook** — `docs/operator/scheduling.md` documenting the three subcommands, JSON/text output shapes, exit codes, chain event types.

Each task lands in the same worktree partition; one commit per logical change; one PR consolidating the implementation.

## Risk flags (handed off to /speckit-tasks)

1. **R1 — Temporal client connection in non-worker mode**: The existing `main.go` connects to Temporal as a worker (registers workflows). Subcommand mode also needs a client connection but NOT worker registration. Risk: a subcommand inadvertently starts a worker that competes with the running `chitin-orchestrator.service`. Mitigation: factor connection setup so subcommands get a `client.Client` without calling `worker.New(...).Run(...)`.

2. **R2 — DAG validation drift vs. scheduler runtime**: The validation in `schedule` (FR-004) must agree with what the scheduler does at runtime — if validation accepts a DAG the scheduler later rejects (or vice-versa), we get inconsistent behavior. Mitigation: validation calls into the same `driver.Registry.Lookup` and `MapCapability` paths the scheduler uses; no parallel re-implementation.

3. **R3 — Concurrent subcommand invocations**: Two operators run `schedule` against the same spec at the same wall-clock instant. Both proceed (FR / edge case allows this). Risk: both also emit `scheduler_started` with the same `spec_ref` but different `run_id`. Acceptable per spec — operators are responsible for cleanup if duplication was unintentional — but flag this for the operator runbook.

4. **R4 — Chain emit binary path**: `emit.go` shells out to `chitin-kernel emit`. Risk: the `chitin-kernel` binary is not on PATH at the moment a subcommand is invoked. Mitigation: respect an env var override (`CHITIN_KERNEL_BIN`) AND fall back to PATH; on missing binary, log warn and continue (FR-009 — chain emit failure must not block the user-visible action).

5. **R5 — Numeric prefix ambiguity**: "09" matches every spec from 091..099. The spec calls this out in US1 acceptance scenario 3 (error with the list). Implementation must list candidates in stable (sorted) order to keep the error text reproducible across invocations. Test fixture must exercise this.

## Complexity Tracking

No constitution violations. This section is intentionally empty.
