# Implementation Plan: Merge Queue Orchestrator

**Branch**: `feat/093-merge-queue-orchestrator` | **Date**: 2026-05-23 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/093-merge-queue-orchestrator/spec.md`

## Summary

A Temporal-based PR merge workflow that dogfoods constitution §7 ("the swarm is the orchestrator") by turning the merge process itself into a first-class orchestrator workload. Operator submits an ordered queue of PRs through a new CLI subcommand; a parent `MergeQueueWorkflow` spawns one `PRMergeWorkflow` child per queue position; each child classifies its PR into one of six policy classes (governance / live-fix / spec-only / research-docs / impl / bookkeeping), drives the per-PR merge loop (rebase → push → wait-for-checks → squash-merge → delete-branch) with policy-determined gates, and emits telemetry per state transition. Pointer-file conflicts (`.specify/feature.json`, `CLAUDE.md`) auto-resolve to the branch's version; any other conflict halts on a signal-blocked operator gate. Governance PRs always block on a human approval signal regardless of submitter — the no-bypass invariant aligned with spec 092.

The technical approach reuses the existing chitin orchestrator worker (Go, single task queue `"chitin"`) and patterns established by `SchedulerWorkflow` (parent shape) and `WorkUnitWorkflow` (child shape). All git/gh shell-out follows the `activities/deliver.go` graceful-degradation pattern. Worktrees come from the spec 070 `worktree.Manager`. No new service, no new datastore — Temporal workflow history is the queue's system of record.

## Technical Context

**Language/Version**: Go 1.22 (matches existing chitin-orchestrator module).

**Primary Dependencies**: Temporal Go SDK (already vendored at `go/orchestrator/`); `os/exec` for shell-out to `git` and `gh`; existing `worktree.Manager` from spec 070; existing OTLP telemetry sink; existing Discord notifier from spec 080.

**Storage**: Temporal workflow history (per FR-028, "the underlying workflow engine is the source of truth for queue state; no separate queue store is maintained"). No relational store. No file-on-disk queue.

**Testing**: Go `testing` package + Temporal `testsuite` for workflow unit tests; integration tests that drive a real (test-only) repository through the workflow; quickstart-driven manual verification of all 10 success criteria against the production `chitinhq/chitin` repo using the 7-PR backlog.

**Target Platform**: Linux server, running as part of the existing `chitin-orchestrator` systemd service. No new service binary.

**Project Type**: Go module addition — new workflows + activities under `go/orchestrator/`, plus extending the existing `chitin-orchestrator` binary to be subcommand-aware so that `chitin-orchestrator merge-queue submit <yaml>` becomes the operator submission surface (with current worker behavior as the default subcommand). See research.md decision R-CLI for the rationale (avoids cross-module imports between `go/execution-kernel/` and `go/orchestrator/` since there is no `go.work`).

**Performance Goals**: Per SC-010, orchestrator-attributable wall-clock overhead is below 10% of total queue duration for a non-conflicting 5-PR queue. Per-PR steady-state activity time is dominated by CI wait (network-bound, minutes); orchestrator overhead is sub-second per state transition.

**Constraints**: Temporal workflow code must be deterministic — no clocks, no I/O, no randomness inside workflow functions. All side effects (git, gh, telemetry, notifier) flow through activities. Activity timeouts: fetch-metadata 30s, rebase 5m, force-push 1m, wait-for-checks 30m with 30s heartbeat, merge 1m, delete-branch 30s, human approval gate signal-blocked (no timeout). Force-push always uses `--force-with-lease`. Squash-merge only.

**Scale/Scope**: v1 typical queue size 1–20 PRs; no enforced upper bound. Single-worker single-task-queue; no multi-worker quorum. Concurrent queue submissions allowed if they don't share `(repo, PR)` pairs.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| § | Rule | Compliance | Evidence |
|---|------|-----------|----------|
| §1 | Side-effect boundary — kernel is the only chain-writer; everything else routes through hermes/openclaw for kanban; no bypass | PASS | This spec touches neither chain events nor kanban. PR merges via `gh` are direct operator-equivalent calls, not bypassed kernel actions. |
| §2 | Worker + worktree mandatory for every unit of work — primary checkout is never a work surface | PASS | Spec FR-010 mandates disposable worktrees for all rebase work. Reuses spec 070 `worktree.Manager`. Verified empirically during this session's bootstrap rebase (`/tmp/chitin-091-rebase`). |
| §3 | Spec-kit promotion gate — every `triage→ready` ticket has matching `spec.md` | PASS | This spec exists at `.specify/specs/093-merge-queue-orchestrator/spec.md`. |
| §4 | Tracked installers — every operator-box script has idempotent installer | N/A | No new operator-box script. Adds a subcommand to existing `chitin` CLI; no new install surface. |
| §5 | Board-aware scripts — kanban touching scripts must accept `--board` | N/A | Workflow does not touch kanban. |
| §6 | `swarm/` is transitional housing exception | PASS | New code lives in `go/orchestrator/` and `cmd/chitin/`, not in `swarm/`. |
| §7 | Swarm is the orchestrator — deterministic orchestration, telemetry at every layer, kernel gates every tool call; no implementation work reaches a driver except via orchestrator dispatch from a work-unit | PASS — DIRECT DOGFOOD | The merge workflow IS an orchestrator workload. Each merge step is a Temporal activity (deterministic dispatch). Telemetry per state transition (FR-024) covers the "telemetry at every layer" mandate. The workflow does not invoke any driver — it acts on PRs directly through git/gh activities — so the "no implementation work to a driver except via orchestrator" rule is honored vacuously (no driver is invoked). |

**Verdict:** All gates pass on initial check. No complexity to justify; no `Complexity Tracking` rows needed.

## Project Structure

### Documentation (this feature)

```text
specs/093-merge-queue-orchestrator/
├── plan.md                                  # This file (/speckit-plan output)
├── spec.md                                  # Feature specification (/speckit-specify output)
├── research.md                              # Phase 0 — 8 decisions with rationale + alternatives
├── data-model.md                            # Phase 1 — entities and workflow I/O types
├── quickstart.md                            # Phase 1 — operator recipes + SC verification
├── contracts/
│   ├── queue-submission-schema.md           # Phase 1 — YAML schema for `chitin merge-queue submit`
│   ├── workflow-signal-schemas.md           # Phase 1 — resume / abort / approve signal payloads
│   └── policy-table.md                      # Phase 1 — the 6-class policy table (canonical version)
├── checklists/
│   └── requirements.md                      # Spec quality checklist (created by /speckit-specify)
└── tasks.md                                 # Phase 2 — created by /speckit-tasks (NOT this command)
```

### Source Code (repository root)

```text
go/orchestrator/
├── workflows/
│   ├── merge_queue.go                       # NEW — MergeQueueWorkflow (parent)
│   ├── pr_merge.go                          # NEW — PRMergeWorkflow (child)
│   ├── merge_queue_test.go                  # NEW — workflow unit tests with testsuite
│   └── pr_merge_test.go                     # NEW
├── activities/
│   ├── merge/                               # NEW package — namespaced merge activities
│   │   ├── fetch_pr_metadata.go             # Activity: gh pr view → PRSnapshot
│   │   ├── classify_pr.go                   # Activity: PRSnapshot + PolicyTable → PolicyClass
│   │   ├── check_mergeability.go            # Activity: gh pr view → MergeabilityStatus
│   │   ├── rebase_with_policy.go            # Activity: worktree + git rebase with auto-resolve
│   │   ├── force_push_with_lease.go         # Activity: git push --force-with-lease
│   │   ├── wait_for_checks.go               # Activity: polled gh pr view --json statusCheckRollup with heartbeat
│   │   ├── merge_pr.go                      # Activity: gh pr merge --squash --delete-branch
│   │   ├── delete_branch.go                 # Activity: separate fallback if merge_pr couldn't delete
│   │   ├── emit_queue_telemetry.go          # Activity: OTLP event per state transition
│   │   └── notify_operator.go               # Activity: Discord notify (start / pause / complete)
│   └── merge/policy/                        # NEW package — policy table as code
│       ├── policy_table.go                  # 6-class table, version constant, classifier function
│       └── policy_table_test.go             # Table-driven tests including governance file allowlist
├── cmd/chitin-orchestrator/
│   ├── main.go                              # MODIFIED — subcommand dispatch + register workflows
│   └── merge_queue.go                       # NEW — `merge-queue submit <yaml-file>` subcommand handler
└── ...                                      # untouched
```

**Structure Decision**: Single Go module addition rather than a new microservice or repo. New workflows/activities live in their existing parent packages (`go/orchestrator/workflows/` and `go/orchestrator/activities/`) so the worker entrypoint at `go/orchestrator/cmd/chitin-orchestrator/main.go` registers them with the same `worker.New(c, "chitin", ...)` instance. The merge-specific activities are namespaced under a new `activities/merge/` sub-package to keep blast radius isolated and make package-level testing easy. The policy table is its own sub-package (`activities/merge/policy/`) so the classifier function can be tested independently of any Temporal context. The CLI subcommand lives in the orchestrator binary directory, dispatched from a subcommand-aware `main.go` (the current single-purpose worker `main` becomes one branch of a dispatch switch).

## Complexity Tracking

> Constitution Check passed without violations. This section is intentionally empty.
