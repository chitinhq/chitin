# Phase 0 Research: Operator entrypoint for the spec-DAG scheduler

**Feature**: 097-operator-scheduler-entrypoint
**Date**: 2026-05-23
**Status**: All NEEDS CLARIFICATION resolved. Bug locus (gap locus) confirmed via production-code sweep; design decisions captured below.

## Sweep evidence (the gap is real)

The 2026-05-23 sweep that motivated this spec:

| Symbol / surface | Production callers | Conclusion |
|---|---|---|
| `client.ExecuteWorkflow(SchedulerWorkflow, ...)` | **Zero in non-`_test.go` files** under `go/orchestrator/` and `go/execution-kernel/` | Nothing in chitin triggers SchedulerWorkflow today. |
| `speckit.New().CompileSpec(...)` | **Zero in non-`_test.go` files** | The adapter is exposed but unused by production code. |
| `chitin-orchestrator schedule` (or equivalent subcommand) | Doesn't exist; `cmd/chitin-orchestrator/main.go` has only the worker-host path | No CLI surface. |
| Operator runbook for "start impl for a merged spec" | Not in `docs/`; not in spec 070/076/077/080 | No documented path. |
| Cron / scheduled job that triggers `SchedulerWorkflow` | None found in `go/orchestrator/schedules/` (looked at `argus_ingest.go`, `codex_ingest.go`, `operator_digest.go`, `operator_heartbeat.go`, `architecture_audit.go`, `swarm_audit.go`, `watchdog_mutation_ops.go`) | No automated trigger. |

## Decision summary

| ID | Decision | Confidence |
|---|---|---|
| D1 | Host the new subcommands in the existing `chitin-orchestrator` binary | High |
| D2 | Preserve worker-host default for no-args invocation | High |
| D3 | Pre-validate the DAG before `ExecuteWorkflow` (refuse `NeedsClarification` and unroutable capabilities) | High |
| D4 | Emit `scheduler_started` / `scheduler_canceled` chain events; `status` is read-only and emits nothing | High |
| D5 | Three-tiered exit codes (0 success / 1 user error / 2 runtime error) | High |
| D6 | Env-var + flag overrides for `TEMPORAL_HOSTPORT` and `CHITIN_REPO_ROOT` | High |
| D7 | Use the Temporal RunID as the application-level SchedulerInput.RunID | Medium — see D7 alternatives |
| D8 | Chain-emit failure does NOT roll back the user-visible action | High |
| D9 | Spec ref resolution: exact match first, then unique numeric prefix, then unique slug; ambiguous = error | High |
| D10 | Auto-scheduling on PR merge is explicitly v1.1 — out of scope for v1 | High |

## D1 — Host in `chitin-orchestrator` binary

**Decision**: add `schedule` / `status` / `cancel` subcommands to the existing `chitin-orchestrator` binary at `go/orchestrator/cmd/chitin-orchestrator/main.go`.

**Rationale**:
- The binary already imports `go.temporal.io/sdk/client` and reaches the same Temporal server the scheduler workflows run on. Adding a client-mode path is free.
- The binary's deployment, install path (`/home/red/.local/bin/chitin-orchestrator`), and systemd unit (`chitin-orchestrator.service`) already exist. Reusing them avoids a new install step.
- Subcommand-on-existing-binary is a well-trodden pattern in chitin (`chitin-kernel emit`, `chitin-kernel gate evaluate`, `chitin-kernel speckit-lint`).

**Alternatives considered**:
- **New `chitin-schedule` binary** — rejected. Adds an install step, a binary to track in `swarm/bin/install-*.sh`, and a fourth executable. No upside.
- **Fold into `chitin-kernel`** — rejected. `chitin-kernel` is the policy/gate/chain side; scheduling is the orchestrator side. Mixing them blurs §1's side-effect boundary (kernel writes the chain; orchestrator drives workflows). Cleaner to keep them separate.

## D2 — Worker-host default

**Decision**: when invoked with no subcommand (or a recognized non-subcommand argv), `chitin-orchestrator` continues to register workflows + activities + poll the task queue (current behavior). Subcommand mode is invoked by `chitin-orchestrator <subcommand> ...`.

**Rationale**:
- The systemd unit `chitin-orchestrator.service` calls the binary with no arguments. Breaking that breaks the deployed worker host.
- Subcommand dispatch is an additive code path; the default arm of the dispatcher routes to the existing worker-host main.

**Detection logic**: `len(os.Args) >= 2 && knownSubcommands[os.Args[1]]` enters subcommand mode; otherwise worker-host.

## D3 — Pre-validate the DAG

**Decision**: before calling `ExecuteWorkflow`, validate that (a) the DAG is acyclic (spec 077's adapter already enforces this; we trust the error path), and (b) every node's capability resolves to a non-`NeedsClarification` value AND is declared by at least one registered driver. Validation failure exits 1 with a list of offending nodes; no workflow starts; no chain event emitted.

**Rationale**:
- A DAG with `NeedsClarification` capabilities will be `blocked-unroutable` immediately on first tick (spec 076 FR-010) — better to fail at the operator's keyboard than inside Temporal.
- The driver registry is built at startup of the worker host; this spec's subcommand can reach the same registry by constructing it the same way (the same code path as `main.go:52`).
- Spec-076's `SelectDriver` activity does the same check at runtime; pre-validation aligns the two so behavior is consistent.

**Implementation seam**: `validate.go` exposes `ValidateForDispatch(dag.DAG, driver.Registry) []ValidationError`. Returns empty slice for valid DAGs.

**Alternatives considered**:
- **Skip validation, let the scheduler handle it** — rejected. The operator wouldn't see the error until they ran `status` and noticed everything was `blocked-unroutable`. Worse UX.
- **Validate but only warn** — rejected. Better to refuse upfront than silently dispatch a DAG we know is broken.

## D4 — Chain event types

**Decision**: emit two new chain event types via `chitin-kernel emit`:

- `scheduler_started` on successful `schedule` — payload `{event_type, spec_ref, run_id, node_count, capabilities_required, ts}`
- `scheduler_canceled` on successful `cancel` — payload `{event_type, run_id, reason, ts}`

`status` is read-only and emits nothing.

**Rationale**:
- The chain is the audit anchor. Every operator action that changes scheduler state needs a chain event so the "who scheduled what when" question is answerable from the chain.
- Two events (not one) keep payload schemas tight and let chain readers filter cleanly.
- Per constitution §1, the kernel is the only chain writer — the subcommand shells out to `chitin-kernel emit` instead of writing directly. Matches the existing pattern in the openclaw plugin's `emitStopSignalIgnored` (spec 091 FR-009).

**Alternatives considered**:
- **Emit a single `scheduler_event` with a `verb` field** — rejected. Less queryable; harder to evolve payload per event type.
- **Skip chain emit entirely; rely on Temporal history** — rejected. Temporal history is the workflow's record; the chain is chitin's cross-cutting audit. Both are needed.

## D5 — Three-tiered exit codes

**Decision**:
- `0` — success
- `1` — user error (bad ref, ambiguous ref, no such run-id, terminal-state cancel, DAG validation failure, missing artifact)
- `2` — runtime error (Temporal unreachable, kernel binary missing when configured `denyOnError`, IO failure on repo read)

**Rationale**:
- Two-tiered (0 / non-zero) is too coarse for scripts that need to distinguish "I typed wrong" from "the platform is broken."
- Three tiers match the convention used by other modern CLIs (rg, jq, git porcelain in some modes).
- Operators can branch shell scripts on this reliably: `if [ $? -eq 1 ]; then # bad input; fix and retry`.

## D6 — Env-var + flag overrides

**Decision**: support `TEMPORAL_HOSTPORT` (default `127.0.0.1:7233`, matching `main.go:39`) and `CHITIN_REPO_ROOT` (default: `git rev-parse --show-toplevel` from cwd). Flag overrides env; env overrides default.

**Rationale**:
- Matches the convention used by existing chitin CLIs (`chitin-kernel`'s `CHITIN_POLICY_FILE`, the openclaw plugin's env reads).
- Required for integration tests against a sandbox Temporal — the test harness sets `TEMPORAL_HOSTPORT` to a fixture port.

## D7 — Use Temporal RunID as application RunID

**Decision**: the `SchedulerInput.RunID` is set to the same value Temporal will use as the WorkflowID. The subcommand computes a fresh UUID before calling `ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: uuid, TaskQueue: ...}, SchedulerWorkflow, SchedulerInput{RunID: uuid, ...})`. Both Temporal and the workflow's internal telemetry then refer to the same identifier.

**Rationale**:
- One identifier is simpler than two. Operators using `status` see one RunID.
- Spec 076's `SchedulerInput.RunID` is described as "stable across every Continue-As-New of the run" — using the Temporal WorkflowID guarantees this property because WorkflowID survives Continue-As-New.
- This is what spec 076's tests already do; we'd be consistent.

**Alternatives considered**:
- **Two distinct IDs** (Temporal RunID + app-level RunID) — rejected. Adds complexity without benefit. The chain events would carry one; the Temporal UI would show the other; correlation is harder.
- **Derive RunID from `spec_ref + timestamp`** — rejected. Predictable IDs invite accidental collision; UUID is the cheapest unique-by-construction option.

## D8 — Chain emit failure does not roll back

**Decision**: if the chain emit fails after `ExecuteWorkflow` (or `CancelWorkflow`) returns successfully, log a warning to stderr but exit 0 (or in the cancel case, the appropriate user-visible code). The user-visible action completed; the audit log lost an entry.

**Rationale**:
- Rolling back `ExecuteWorkflow` is not free — there's no atomic transaction across Temporal and the chain. The workflow already started; faking that it didn't is worse than logging a warning.
- Operators can later replay the start event from Temporal history if forensic reconstruction is needed.
- Matches spec 091 FR-009's posture: telemetry failure must not break the load-bearing action.

## D9 — Spec ref resolution

**Decision**: resolve `<spec-ref>` to a unique `.specify/specs/NNN-name/` directory by:

1. **Exact directory-name match** — `<repo>/specs/<spec-ref>/` exists as a directory.
2. **Numeric prefix match** — extract leading digits from `<spec-ref>`; find the unique `NNN-*` directory matching those digits.
3. **Slug match** — find the unique `*-<spec-ref>` directory (case-sensitive, exact suffix).

If steps 1-3 yield zero matches: error 1 with `error: no spec matching ref "<spec-ref>"` and a list of available specs.

If any step yields >1 match: error 1 with `error: ref "<spec-ref>" is ambiguous — matched <N> specs:` and a sorted list.

**Rationale**:
- Matches the convention used by `speckit.New().CompileSpec` already (per `adapter_test.go:154-156` — `TestCompileSpecRefByPrefix`).
- Three-tier resolution is small enough to implement cleanly and large enough to cover operator habits (`096` and `operator-session-state-surface` both work).

**Alternatives considered**:
- **Numeric-only** — rejected. Operators sometimes know the slug, not the number.
- **Fuzzy match (Levenshtein)** — rejected. Surprising; operators want exactness.

## D10 — Auto-schedule-on-merge is v1.1

**Decision**: this spec covers the operator CLI only. A webhook or cron that auto-schedules when a spec's `Status` flips to Ratified (or when a PR adding a `tasks.md` merges) is desirable but deferred to a v1.1 amendment after the CLI proves itself.

**Rationale**:
- Auto-scheduling has its own design questions (which Temporal cluster? which branch?). Bundling them into v1 would bloat the spec.
- The CLI is the load-bearing primitive — once it exists, the webhook is a thin wrapper. Doing them in this order keeps the v1 scope crisp and the v1.1 trivial.

**Out of scope for this research doc**: the actual webhook implementation, the merge-detection signal source (GitHub webhook? cron polling `git log`?), the cluster routing.

## Open questions resolved during research

- **OQ1 (during spec authoring): does the speckit adapter handle capability metadata?** → YES, `MapCapability(t)` in `go/orchestrator/adapter/speckit/adapter.go:302` derives capability from task descriptions with a `NeedsClarification` fallback. No new spec needed; this spec consumes that interface.
- **OQ2 (during spec authoring): what does the worker host registration look like?** → confirmed at `cmd/chitin-orchestrator/main.go:52-95`. The driver registry is built there; the subcommand path reuses the same construction.
- **OQ3 (during spec authoring): is there a kanban-shaped trigger we should leave intact?** → NO. Spec 069 decommissioned the kanban; spec 081 is in flight to remove its residue. FR-012 forbids any new dependency on the decommissioned surface.

## No remaining NEEDS CLARIFICATION

All technical context items in `plan.md` resolve to concrete values. No `[NEEDS CLARIFICATION]` markers in the spec or plan.
