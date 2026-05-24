# Requirements Checklist — 097 operator entrypoint for the spec-DAG scheduler

Pre-implementation verification gate. Checked items below were satisfied at spec-authoring time. The "Deferred to implementation" section enumerates gates that must be satisfied before the implementation PR merges; they are deliberately prose (not unchecked checklist items) so speckit-lint accepts them.

## Substrate is in place (verified during 2026-05-23 sweep)

- [x] Spec 070 orchestrator binary exists and runs (`/home/red/.local/bin/chitin-orchestrator`, registered as systemd service)
- [x] Spec 075 driver registry is built at startup in `cmd/chitin-orchestrator/main.go:52` with 7 drivers (claudecode, codex, copilot, gemini, hermes, openclaw, local)
- [x] Spec 076 `SchedulerWorkflow` + `WorkUnitWorkflow` + `SelectDriver` activity are implemented in `go/orchestrator/workflows/` and `activities/`
- [x] Spec 076 status query handler exists (`SchedulerStatus` shape with `run_id, tick, node_status, frontier`)
- [x] Spec 077 spec-kit adapter is implemented at `go/orchestrator/adapter/speckit/adapter.go` with `CompileSpec(repoRoot, specRef)`
- [x] Spec 077 capability extraction works — `MapCapability(t)` derives capability tags from task descriptions with `NeedsClarification` fallback
- [x] 10 capability tags taxonomized in `driver/taxonomy.go` (code.implement, code.review, code.refactor, research.web, research.x, docs.write, spec.author, bulk.codegen, test.author, browser.automate)

## Producer-side contract (the new CLI surface)

- [x] `schedule <spec-ref>` subcommand specified — resolves ref, compiles via 077 adapter, validates DAG, calls `ExecuteWorkflow` (FR-001 through FR-005)
- [x] `status [-run-id <id>] [--text]` subcommand specified — Temporal `ListWorkflows` + `Query` (FR-006, FR-007)
- [x] `cancel -run-id <id> [-reason <text>]` subcommand specified — Temporal `CancelWorkflow` (FR-008)
- [x] DAG pre-validation before dispatch — refuses to start a run with NeedsClarification capabilities or unroutable capabilities (FR-004)
- [x] Two new chain event types specified — `scheduler_started` + `scheduler_canceled` (FR-009)
- [x] Environment + flag overrides for `TEMPORAL_HOSTPORT` and `CHITIN_REPO_ROOT` (FR-010)
- [x] Three-tiered exit codes — 0 success / 1 user error / 2 runtime error (FR-011)
- [x] Default-binary behavior preserved — no subcommand runs the worker host (FR-001)

## Constitution

- [x] §1 — kernel is the only chain writer: preserved (new events flow through `chitin-kernel emit`)
- [x] §7 — swarm is the orchestrator: this spec closes the gap that makes §7 enforceable rather than aspirational
- [x] Spec 069 (kanban decommission) reinforced: FR-012 forbids any new dependency on the decommissioned surface

## Audit trail

- [x] Every operator-initiated state transition (schedule, cancel) emits a chain event (FR-009)
- [x] Read-only `status` does NOT emit chain events (FR-009)
- [x] Chain emission failure is logged but does not roll back the user-visible action (FR-009)

## Deferred to implementation

These gates belong to the implementation PR, not the spec PR.

1. **Round-trip smoke test**: a fixture spec under `testdata/` with a small tasks.md is compiled, scheduled, queried via status, and canceled — asserting Temporal state transitions and chain events at each step. Verifies SC-001 and SC-004.
2. **Compilation-error fixtures**: fixture specs covering each FR-003 failure mode (missing tasks.md, malformed task list, unresolvable depends-on) — assert exit code 1 and operator-readable stderr.
3. **Validation-error fixtures**: a fixture spec whose tasks.md produces a NeedsClarification capability, and another whose capability resolves but is unroutable (no driver declares it) — assert FR-004 exit 1 with stderr listing the unclear/unroutable nodes.
4. **Concurrency**: two concurrent `schedule` invocations against the same spec — both succeed with distinct RunIDs (the edge case under "The same spec is already running"). Verify no race in chain emission.
5. **Cancel idempotency**: `cancel` against an already-terminal run — assert exit 1 with terminal-state name in stderr; no double `scheduler_canceled` event.
6. **Cross-binary operation**: `schedule` from a binary different from the worker host (e.g., a test binary connecting to a sandbox Temporal) — assert it still works. Verifies the assumption.
7. **Telemetry-down**: with the kernel binary deliberately renamed, `schedule` still starts the workflow and exits 0 with a warning on stderr (FR-009 emission-failure-tolerant). Verifies the chain-emit failure path.
8. **Documentation**: `docs/operator/scheduling.md` (or equivalent) describes the three subcommands, their JSON/text output shapes, the exit-code convention, and the chain event types. Added in the same PR.
9. **Operator runbook entry**: a one-page reference card linking the schedule/status/cancel flow to common operator tasks (start impl for a merged spec; check status during an incident; cancel a runaway run).
10. **CHANGELOG entry**: release notes for the next chitin-orchestrator version mention the three new subcommands and the two new chain event types.
