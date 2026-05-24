# Feature Specification: Operator entrypoint for the spec-DAG scheduler

**Feature Branch**: `feat/097-operator-scheduler-entrypoint`

**Created**: 2026-05-23

**Status**: Draft

**Input**: User description: "Spec 070 + 076 wired the Temporal-based spec-DAG scheduler and spec 077 implemented the spec-kit adapter that compiles a spec's tasks.md into a normalized DAG. But nothing in chitin actually calls ExecuteWorkflow(SchedulerWorkflow). Production sweep on 2026-05-23 found ExecuteWorkflow only in *_test.go files; CompileSpec has zero production callers; no `chitin-orchestrator schedule` CLI subcommand exists. So §7's 'implementation MUST flow through the orchestrator' is presently aspirational because operators have no documented path to start a scheduler run for a specific spec. This spec defines the missing operator entrypoint: a small CLI surface on the orchestrator binary that takes a spec ref, compiles it through the spec-077 adapter, and starts the spec-076 SchedulerWorkflow, plus status and cancel subcommands for the resulting run."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Operator schedules a spec for implementation (Priority: P1)

An operator has just merged a spec to `main` (with `spec.md`, `plan.md`, and `tasks.md` all present). They want the orchestrator to start the implementation work. They run `chitin-orchestrator schedule 096` (numeric prefix) or `chitin-orchestrator schedule operator-session-state-surface` (slug). The binary:

1. Resolves the spec ref to a unique `.specify/specs/NNN-name/` directory (fails fast and operator-readably if ambiguous or missing).
2. Compiles the spec via the existing spec-077 adapter (`speckit.New().CompileSpec(repoRoot, specRef)`).
3. Validates the resulting DAG (acyclic; all task capabilities resolve to non-`NeedsClarification` tags; at least one driver in the registered registry can satisfy each capability — re-uses spec-076's existing rejection semantics).
4. Calls `client.ExecuteWorkflow(ctx, SchedulerWorkflow, SchedulerInput{...})` against the orchestrator's task queue.
5. Prints the Temporal RunID and WorkflowID to stdout, plus a one-line summary (`scheduled spec 096 (8 nodes, 3 capabilities required); run_id=<uuid>`), and exits 0.

The operator can then walk away — the orchestrator does the rest per specs 075/076.

**Why this priority**: this is the deliverable. Constitution §7 says implementation MUST flow through the orchestrator. Today nothing flows through it because nothing triggers it. This story closes the trigger gap and makes §7 enforceable in practice rather than aspirational.

**Independent Test**: in a sandbox repo with a fixture spec under `.specify/specs/099-fixture/`, run `chitin-orchestrator schedule 099`; verify (a) exit code 0, (b) a RunID prints to stdout, (c) the Temporal UI shows a started `SchedulerWorkflow`, (d) querying the workflow's status returns a non-empty NodeStatus map matching the fixture's tasks.

**Acceptance Scenarios**:

1. **Given** a repo with a valid spec at `.specify/specs/096-operator-session-state-surface/` carrying `spec.md`, `plan.md`, and `tasks.md`, **When** the operator runs `chitin-orchestrator schedule 096`, **Then** the spec compiles, the scheduler workflow starts, the RunID and node count print to stdout, exit code is 0, and a `scheduler_started` chain event is emitted with `{spec_ref:"096", run_id, node_count, ts}`.
2. **Given** no spec matches the ref `chitin-orchestrator schedule 999`, **When** the command runs, **Then** stderr contains `error: no spec matching ref "999"` listing the available numeric prefixes, exit code is 1, no workflow is started, and no chain event is emitted.
3. **Given** the ref `chitin-orchestrator schedule 09` matches MORE than one spec directory (e.g., 091, 092, …), **When** the command runs, **Then** stderr contains `error: ref "09" is ambiguous — matched 9 specs` listing them, exit code is 1, no workflow is started, and no chain event is emitted.
4. **Given** the spec exists but its `tasks.md` is missing or unreadable, **When** the command runs, **Then** stderr contains an operator-readable error pinpointing the missing artifact, exit code is 1, and no workflow is started.
5. **Given** the spec compiles but the resulting DAG carries one or more tasks whose capability resolves to `NeedsClarification`, **When** the command runs, **Then** stderr lists the unclear tasks, exit code is 1 (refuse to dispatch a partially-routable DAG), and no workflow is started. The operator's path forward is to amend `tasks.md` until every task carries a routable capability.
6. **Given** the Temporal server is unreachable (`hostPort` env unset and default port closed), **When** the command runs, **Then** stderr contains `error: Temporal unreachable at <host:port>`, exit code is 2 (runtime error, distinct from the user-error exit code 1), and no chain event is emitted.

---

### User Story 2 — Operator queries and controls a running scheduler run (Priority: P1)

While a scheduler run is in flight, the operator wants visibility and control. They run `chitin-orchestrator status` to list every active scheduler run with its RunID, the spec it's running for, tick number, and runnable-frontier size. They run `chitin-orchestrator status -run-id <id>` to see one run's full node-status map (matching the existing `SchedulerStatus` Temporal query handler). They run `chitin-orchestrator cancel -run-id <id> [-reason <text>]` to cancel a run cleanly (via the Temporal cancellation signal); the scheduler honors the cancellation at its next tick boundary and emits a terminal telemetry event.

**Why this priority**: a trigger without observability is a black box; a black box that can't be stopped is a runaway. Status + cancel are not nice-to-haves — they're the minimum operator surface that makes the trigger usable in practice. Anything that can be started must be inspectable and stoppable.

**Independent Test**: with a known scheduler run in flight (from US1), run `status` to verify it's listed, run `status -run-id <id>` to verify the node-status map matches the Temporal query handler's return, run `cancel -run-id <id> -reason "operator abort"` and verify the workflow transitions to Canceled in the Temporal UI within one tick, with a `scheduler_canceled` chain event carrying the reason.

**Acceptance Scenarios**:

1. **Given** two scheduler runs are in flight (RunIDs A and B), **When** the operator runs `chitin-orchestrator status`, **Then** stdout contains a JSON array (or table under `--text`) listing both runs each with `{run_id, spec_ref, tick, frontier_size, started_at}` sorted by `started_at` descending.
2. **Given** RunID A is in flight, **When** the operator runs `chitin-orchestrator status -run-id A`, **Then** stdout contains the full SchedulerStatus JSON (`run_id, tick, node_status map, frontier`) — matching the response the existing Temporal `status` query handler returns. Exit code is 0.
3. **Given** RunID A is in flight, **When** the operator runs `chitin-orchestrator cancel -run-id A -reason "policy changed"`, **Then** the Temporal workflow receives a cancellation signal, exit code is 0 after the cancel is accepted by Temporal (not after the workflow has fully wound down), stdout confirms `canceled run_id=A reason="policy changed"`, and one `scheduler_canceled` chain event is emitted with that reason.
4. **Given** a RunID `Z` that does not exist, **When** the operator runs `chitin-orchestrator status -run-id Z` or `cancel -run-id Z`, **Then** stderr contains `error: no scheduler run with run_id "Z"`, exit code is 1.
5. **Given** RunID A has already completed (terminal state), **When** the operator runs `cancel -run-id A`, **Then** stderr contains `error: run_id "A" already in terminal state "completed"`, exit code is 1, and no chain event is emitted (idempotent — cancel-of-completed is a no-op, not a double-cancel).

### Edge Cases

- **A spec ref matches a directory but the directory has no `spec.md`** — fail fast with `error: spec dir <path> has no spec.md`. Don't try to compile a malformed directory.
- **`tasks.md` exists but has zero tasks** — compile succeeds (empty DAG), scheduler workflow starts, immediately reaches terminal state, exit code 0 with a stdout note `scheduled spec NNN (0 nodes); run will complete immediately`. This is a legitimate "no implementation needed" path, not an error.
- **The same spec is already running** (RunID A in flight against spec 096, operator runs `schedule 096` again) — the new run starts with a fresh RunID. Two concurrent scheduler runs against the same spec is allowed (e.g., a re-run after a partial failure with manual cleanup). The chain event records both starts; the operator is responsible for deciding which run to cancel if duplication was unintentional.
- **The orchestrator binary is not the one currently running as the Temporal worker** — irrelevant. The CLI subcommand only needs to reach Temporal as a client; it doesn't need to BE the worker host. A test-mode operator running a different orchestrator binary against the same Temporal can still schedule.
- **A scheduler run is in `Continue-As-New` between ticks** when the operator queries status — Temporal exposes the live workflow ID stably across Continue-As-New; the status query handler returns the most recent tick's state without ambiguity. No special handling required.
- **The chain emit fails after the workflow starts** (telemetry path down, kernel binary missing) — the scheduler run is still active in Temporal (source of truth), but the chain event is lost. Log a warning to stderr, exit code 0 (the user-visible scheduling action succeeded). Operators can later replay the start event from Temporal history if needed.
- **The operator passes `--temporal-host` pointing at a different Temporal cluster** — the subcommand connects there instead of the default. Required for sandboxed integration tests; documented but operators rarely use it in production.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The `chitin-orchestrator` binary MUST gain three new subcommands — `schedule <spec-ref>`, `status [-run-id <id>] [--text]`, and `cancel -run-id <id> [-reason <text>]` — dispatched from `main.go` alongside the existing worker-host startup path. When invoked without a subcommand, the binary continues to run as the worker host (preserving existing behavior for `chitin-orchestrator.service`).

- **FR-002**: `schedule <spec-ref>` MUST resolve the ref via the spec-077 adapter's existing `(repoRoot, specRef)` resolver, supporting both numeric prefix (`096`) and slug (`operator-session-state-surface`). Ambiguous or missing refs MUST exit 1 with stderr listing the candidates or the absence.

- **FR-003**: `schedule` MUST compile the resolved spec via `speckit.New().CompileSpec(repoRoot, specRef)`, surfacing compilation errors (missing `tasks.md`, malformed task list, unresolvable depends-on edges) to the operator with the underlying error text. Compilation failure MUST exit 1; no workflow MUST start.

- **FR-004**: `schedule` MUST validate the compiled DAG before invoking `ExecuteWorkflow`: every node's capability MUST resolve to a non-`NeedsClarification` tag, AND at least one driver in the registered registry MUST declare that capability. A DAG that fails validation MUST exit 1 with stderr listing the unclear or unroutable nodes; no workflow MUST start. The orchestrator MUST NOT dispatch a DAG it knows in advance cannot complete.

- **FR-005**: `schedule` MUST call `client.ExecuteWorkflow(ctx, SchedulerWorkflow, SchedulerInput{RunID, Nodes, Edges, Tick: 0})` against the orchestrator's task queue (constant `TaskQueue` in `cmd/chitin-orchestrator/main.go`). The RunID MUST be a fresh UUID, distinct from any prior run for the same spec. On success the subcommand MUST print the RunID and a one-line summary to stdout and exit 0.

- **FR-006**: `status` with NO `-run-id` flag MUST list every currently-active scheduler workflow (via Temporal's `ListWorkflows` filtered to `WorkflowType="SchedulerWorkflow"` and execution status `Running` or `ContinuedAsNew`). Output MUST be JSON-by-default; `--text` switches to a fixed-column table. Sorted by `started_at` descending.

- **FR-007**: `status -run-id <id>` MUST issue a Temporal `Query` against the workflow's existing `status` query handler (from spec 076) and emit the response JSON unchanged to stdout. An unknown RunID MUST exit 1 with an operator-readable error.

- **FR-008**: `cancel -run-id <id> [-reason <text>]` MUST call `client.CancelWorkflow(ctx, id, "")` to send a Temporal cancellation signal. The subcommand MUST exit 0 once the cancel is accepted (not blocked on workflow shutdown). An unknown RunID MUST exit 1; a RunID in a terminal state MUST exit 1 with the terminal-state name in the error.

- **FR-009**: Every operator-initiated state transition (start, cancel) MUST emit a chain event via the existing `chitin-kernel emit` path. New event types: `scheduler_started` (payload `{spec_ref, run_id, node_count, capabilities_required: [...], ts}`) and `scheduler_canceled` (payload `{run_id, reason, ts}`). `status` is read-only and MUST NOT emit. Chain emission failure MUST log a warning to stderr but MUST NOT cause the user-visible action to fail or roll back.

- **FR-010**: Subcommands MUST honor environment-variable overrides for `TEMPORAL_HOSTPORT` (default `127.0.0.1:7233`, matching the existing worker-host default) and `CHITIN_REPO_ROOT` (default: `git rev-parse --show-toplevel` from the cwd). Both also exposable via `--temporal-host` and `--repo-root` flags. Flag takes precedence over env; env takes precedence over default. This matches the existing kernel CLI's convention.

- **FR-011**: Exit codes MUST be three-tiered for stable scripting: `0` = success, `1` = user error (bad ref, ambiguous ref, no such run, terminal-state cancel, DAG validation failure, missing artifact), `2` = runtime error (Temporal unreachable, kernel binary missing for chain emit when configured `denyOnError`, IO failure on repo read). Operators and scripts can branch on these reliably.

- **FR-012**: `schedule` MUST NOT consult the kanban or any decommissioned coordination surface — the spec ref is resolved against `.specify/specs/` directly via the spec-077 adapter, and the DAG flows straight to Temporal. This spec reinforces spec 069 (kanban decommissioned) by avoiding any new dependency on its residual code.

### Key Entities

- **Spec ref** — a string the operator passes to `schedule` that resolves to a unique `.specify/specs/NNN-name/` directory. Supported forms: numeric prefix (`096`, `91`), slug-only (`operator-session-state-surface`), full directory name (`096-operator-session-state-surface`). Resolution is exact-match-first then unique-prefix; non-unique resolution is an error.

- **SchedulerRunID** — the Temporal RunID returned by `ExecuteWorkflow`. Carries the entire run's lifecycle; operators use it for `status` and `cancel`. Distinct from the SchedulerInput.RunID (which is the application-level run identifier the scheduler workflow stamps onto its own telemetry); these may be the same value if the binary uses the Temporal RunID as the application identifier (recommended).

- **`scheduler_started` chain event** — emitted by `schedule` after `ExecuteWorkflow` returns successfully. Payload: `{event_type:"scheduler_started", spec_ref, run_id, node_count, capabilities_required:[capability,...], ts}`. This is the operator-side audit anchor — every spec that ever entered the orchestrator must have one of these in the chain.

- **`scheduler_canceled` chain event** — emitted by `cancel` after Temporal accepts the cancellation. Payload: `{event_type:"scheduler_canceled", run_id, reason, ts}`. The workflow itself may emit additional terminal events from its own teardown path (per spec 076); this event is the operator-action-anchor specifically.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can take a freshly-merged spec and start its implementation via the orchestrator with a single CLI invocation in under 10 seconds wall-clock (compile + validate + ExecuteWorkflow). Verified by: end-to-end test that merges a fixture spec, runs `schedule`, and confirms the Temporal workflow is in Running state within the time bound.

- **SC-002**: Every implementation work-unit that lands in `main` after this spec ships traces back to a `scheduler_started` chain event for its parent spec. Measured by: chain query over a representative time window after spec 097's implementation ships — every `work_unit_completed` event has an ancestor `scheduler_started` event within the same chain. (Pre-097 PRs are exempt; the property holds going forward.)

- **SC-003**: The `status` subcommand returns a non-stale view: a node that transitioned to `done` within the last 5 seconds appears as `done` in the next `status` invocation. Verified by: integration test that triggers a node completion and polls `status` within the time bound.

- **SC-004**: `cancel` is honored at the next scheduler tick boundary (≤30 seconds in default config). Verified by: integration test that schedules a long-running fixture, issues `cancel`, and asserts the workflow is in Canceled state within the bound.

- **SC-005**: The three exit codes are mutually exclusive and accurate. Verified by: a small test suite invoking each error path (bad ref, Temporal unreachable, IO failure) and asserting both the exit code and the stderr text.

- **SC-006**: Operator reports (qualitatively, first two weeks post-merge) that the CLI subcommands replace ad-hoc `temporal workflow start` invocations. Measured by: no operator-side incident or runbook in the first two weeks references manual `temporal workflow start --type SchedulerWorkflow` for a chitin spec.

## Assumptions

- The chitin orchestrator binary is the right host for these subcommands. Alternatives considered (a new `chitin-schedule` binary; folding into `chitin-kernel`) were rejected because the orchestrator binary is already the Temporal client (it imports `go.temporal.io/sdk/client`) and runs as a long-lived service; reusing its build and deployment is cheaper than adding a fourth binary.
- The spec-077 adapter's `CompileSpec` API is stable and correct for chitin's own `.specify/specs/` layout. This spec consumes that interface; it does not modify spec 077.
- The spec-076 scheduler workflow's `SchedulerInput`, `SchedulerStatus`, and `status` query handler are stable. This spec consumes them unchanged.
- Temporal's `ListWorkflows`, `Query`, and `CancelWorkflow` client APIs are available and reliable. The `chitin-orchestrator` binary already depends on the Temporal Go SDK; no new dependency is introduced.
- Operators can run the CLI from any directory inside the repo; the `CHITIN_REPO_ROOT` resolution via `git rev-parse --show-toplevel` is sufficient. Outside a repo, the operator must pass `--repo-root` explicitly.
- The chain emit path (`chitin-kernel emit -event-json -`) is already wired and load-bearing. This spec adds two new event types but introduces no new emission mechanism. Per constitution §1, the kernel remains the only chain writer.
- A webhook auto-trigger (schedule-on-merge) is desirable but explicitly out of scope for v1 — see Scope.

### Scope

**In scope**:

- `chitin-orchestrator schedule <spec-ref>` subcommand and its compile + validate + execute flow
- `chitin-orchestrator status [-run-id <id>] [--text]` subcommand
- `chitin-orchestrator cancel -run-id <id> [-reason <text>]` subcommand
- Two new chain event types (`scheduler_started`, `scheduler_canceled`) and their payload schemas
- DAG pre-validation (no `NeedsClarification` capabilities, every capability routable to at least one registered driver) before dispatch
- Exit-code conventions (0/1/2) and stderr message conventions for stable scripting
- An end-to-end smoke test exercising the schedule → status → cancel round-trip against a fixture spec
- A short operator runbook under `docs/operator/` documenting the three subcommands

**Out of scope**:

- A merged-PR webhook that auto-schedules on spec merge — desirable but a v1.1 amendment after the CLI surface proves itself. Scheduling stays operator-initiated for v1.
- A `pause` / `resume` subcommand — Temporal's signal model could support it, but the current scheduler workflow doesn't expose a pause signal. Treat as future work.
- An interactive TUI for browsing scheduler runs — `status --text` is the human-readable surface for v1; a richer UI is future work.
- Modifying the spec-076 scheduler workflow or the spec-077 adapter — this spec is a consumer of both; if either reveals shortcomings during implementation, they get their own follow-up specs.
- Cross-host coordination (e.g., scheduling against a Temporal cluster on a different machine) — `--temporal-host` makes it possible for testing, but the production deployment story (auth, mTLS, network policy) is a separate concern.
- The driver capability taxonomy itself — that's spec 075's domain. This spec consumes the existing 10 capabilities unchanged.

### Dependencies

- **Spec 070 (Chitin Orchestrator)**: the binary this spec extends. Phases 0–2 already shipped.
- **Spec 075 (Agent Driver Contract)**: the driver registry this spec's `schedule` subcommand consults during DAG validation (FR-004). Already implemented.
- **Spec 076 (Spec-DAG Scheduler)**: the workflow this spec invokes (`SchedulerWorkflow`) and the query handler it reads (`status`). Already implemented.
- **Spec 077 (Spec-Kit Adapter)**: the compiler this spec calls (`speckit.New().CompileSpec`). Already implemented; provides `MapCapability` for capability inference from task descriptions.
- **Constitution §1**: kernel is the only chain writer — preserved (new chain events flow through `chitin-kernel emit`).
- **Constitution §7**: implementation MUST flow through the orchestrator — this spec is the missing piece that makes §7 enforceable rather than aspirational.
- **Spec 069 (kanban decommission)**: reinforced — FR-012 explicitly forbids any new dependency on the decommissioned surface.
