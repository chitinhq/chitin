# Implementation Plan: Spec 099 — GitHub Copilot Driver via Issue Assignment

**Branch**: `spec/099-github-native-dispatch` | **Date**: 2026-05-24 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `.specify/specs/099-github-native-dispatch/spec.md`

## Summary

Add GitHub Copilot as an orchestrator-aware driver. Copilot is structurally off-machine (runs inside GitHub, not in the operator's worktree), so dispatch becomes "create GitHub issue, assign `@copilot`" instead of "start a local SchedulerWorkflow". The orchestrator also becomes a consumer: the existing factory-listen receiver (spec 098) extends to handle `pull_request.opened` / `pull_request.ready_for_review`, detects Copilot's draft PR via the `chitin-dispatch` label + `Closes #ISSUE` cross-reference, and hands the PR to spec 094's `PRReviewWorkflow` for dialectic review.

Technical approach:
- **Producer path** is a new branch inside `chitin-orchestrator schedule`: when `--driver copilot` is passed, the CLI calls `gh issue create` (via `os/exec` against the `gh` binary, fall back to REST if `gh` is absent), assigns `@copilot`, applies the `chitin-dispatch` + `driver:copilot` labels, and emits a `copilot_dispatched` chain event. No SchedulerWorkflow starts on this path.
- **Consumer path** extends the factory-listen HTTP handler to dispatch on `pull_request.*` and `issue_comment.*` events for PRs carrying `chitin-dispatch`. Idempotency is enforced by chain dedup: before starting a `PRReviewWorkflow`, query the chain for an existing `copilot_pr_detected` event keyed on `(repo, pr_number)`.
- **Operator surface** is a new `copilot-list` subcommand that aggregates chain events to render a per-dispatch state table.

The implementation footprint is small — the heavy lifting is GitHub's (Copilot drafts the PR) and spec 094's (reviews it). The orchestrator is the **mediation layer**: dispatch on the producer side, detection on the consumer side, chain bookkeeping in between.

## Technical Context

**Language/Version**: Go 1.25 (orchestrator + kernel); TypeScript (factory-listen handler reuses existing TS extension if any — confirm in research)

**Primary Dependencies**:
- `go/orchestrator/cmd/chitin-orchestrator/` — existing CLI binary
- `go/orchestrator/workflows/scheduler.go` — existing SchedulerWorkflow (untouched on the Copilot path)
- `go/orchestrator/workflows/pr_review.go` — spec 094 PRReviewWorkflow (called from new dispatcher activity)
- `go/orchestrator/internal/factory/listener.go` — existing factory-listen HTTP handler (spec 098)
- `gh` CLI binary, OR `github.com/google/go-github/v58` for REST fallback
- Existing chain emitter in `go/execution-kernel/internal/emit/`

**Storage**: Chain events at `~/.chitin/events-<run_id>.jsonl` (append-only, hash-chained). No new storage primitives — all state lives in the chain.

**Testing**:
- Unit: `go test ./...` against orchestrator package
- Integration: extend `go/orchestrator/test/factory_e2e_test.go` to add a Copilot-path scenario using a mocked `gh` binary on PATH
- Contract: chain event schemas validated by replay test against `internal/event/replay_test.go`

**Target Platform**: Linux (operator host); GitHub Actions for CI

**Project Type**: Single project (Go monorepo with TS apps; this spec touches Go only)

**Performance Goals**:
- SC-001: median <10s, p99 <30s from CLI invocation to `copilot_dispatched` chain event
- SC-002: median <30s, p99 <120s from `pull_request.opened` webhook to `copilot_pr_detected`
- SC-003: 100 redelivered webhooks in 5 minutes → exactly 1 PRReviewWorkflow start
- SC-004: 0 GitHub issues created on dispatches that omit `--driver copilot`, measured over 7 days

**Constraints**:
- §1 (kernel-side-effect boundary): `gh issue create` is a tool call. MUST route through `chitin-kernel gate evaluate` before execution.
- §7 (orchestrator-as-implementation-gate): the Copilot dispatch IS the orchestrator-intaked work-unit. Constitutional — Copilot is listed in §7's driver table.
- §2 (worker + worktree): the Copilot path does not spawn a local worker (no worktree to allocate); this is intentional and matches the driver definition ("dispatched (cloud)").
- Repo credentials: orchestrator MUST have a `gh auth login` session or `GH_TOKEN` with `issues:write` on every target repo.

**Scale/Scope**: Single operator, single host (chimera-ant). Per-repo Copilot dispatches counted in tens per week initially; scaling to hundreds is bounded by GitHub's Copilot SLA, not chitin.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| § | Rule | Compliance |
|---|---|---|
| §1 | Kernel gates every tool call | `gh issue create` MUST be routed through `chitin-kernel gate evaluate` before exec. Plan calls this out in Phase 1 contract for the orchestrator's GH client. ✅ |
| §1 | Only the kernel writes chain events | Orchestrator emits `copilot_dispatched` / `copilot_pr_detected` / etc. through the existing kernel emitter (`go/execution-kernel/internal/emit`). No direct file writes. ✅ |
| §2 | Workers + worktrees for every implementation work-unit | The Copilot path is **not** a local implementation work-unit — Copilot does the implementation on GitHub's infrastructure. The orchestrator's local work (issue creation, PR detection, review dispatch) is bookkeeping, not implementation mutation. ✅ |
| §3 | Spec-kit promotion gate | This plan is the spec-kit entry. ✅ |
| §4 | Tracked installers for new operator-box scripts | No new operator-box script; extends `chitin-orchestrator` (already installed) and `factory-listen` (already installed via `swarm/systemd/chitin-orchestrator.service` and `chitin-factory-listen.service`). ✅ |
| §5 | Board-aware (kanban) | Not applicable — this spec doesn't touch kanban. ✅ |
| §6 | Swarm/ exception | No new swarm/ artifact. ✅ |
| §7 | Implementation gate: no impl PR without orchestrator | The orchestrator IS the dispatcher. Copilot is listed as a §7 driver. The `chitin-orchestrator schedule --driver copilot` path is a DAG-resolved or ad-hoc orchestrator-intaked work-unit. ✅ |
| §7 | Driver selection by capability | Copilot's capability tag exists in the driver registry (verify in Phase 0); the `--driver copilot` flag is an explicit operator override, allowed per §7 ad-hoc rules. ✅ |
| §7 | Telemetry at every observable layer | FR-013 (`copilot_pr_activity` capturing every webhook event) is the explicit partial-recovery of telemetry that we lose by dispatching off-machine. Documented as a deliberate tradeoff in spec.md §Risks. ✅ |

No violations. Complexity tracking section is empty.

## Project Structure

### Documentation (this feature)

```text
.specify/specs/099-github-native-dispatch/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── cli-driver-flag.md
│   ├── factory-listen-pr-events.md
│   └── chain-events.md
└── tasks.md             # Phase 2 output (via /speckit-tasks)
```

### Source Code (repository root)

```text
go/orchestrator/
├── cmd/chitin-orchestrator/
│   ├── main.go                              # add --driver flag wiring; route to copilot path
│   ├── schedule.go                          # MODIFY: branch on --driver copilot
│   ├── copilot_dispatch.go                  # NEW: gh issue create + chain emit
│   ├── copilot_list.go                      # NEW: copilot-list subcommand
│   └── copilot_dispatch_test.go             # NEW
├── internal/
│   ├── factory/
│   │   ├── listener.go                      # MODIFY: handle pull_request.* + issue_comment.*
│   │   ├── pr_eligibility.go                # NEW: FR-007 eligibility check
│   │   ├── pr_dedup.go                      # NEW: FR-008 idempotency via chain query
│   │   └── pr_handler_test.go               # NEW
│   ├── github/
│   │   ├── client.go                        # NEW: thin wrapper over gh CLI + REST fallback
│   │   ├── issue.go                         # NEW: CreateIssue, AssignTo, AddLabels
│   │   └── client_test.go                   # NEW (table-driven against mocked gh)
│   └── chain/
│       └── copilot_events.go                # NEW: typed emit helpers for the 6 event types
├── activities/
│   └── review/
│       └── start_pr_review_workflow.go      # NEW or MODIFY: callable activity wrapper
└── test/
    ├── factory_e2e_test.go                  # MODIFY: add Copilot scenario
    └── fixtures/
        └── github_pr_opened.json            # NEW: webhook payload fixture
```

**Structure Decision**: Single Go module under `go/orchestrator/`. Reuses existing factory-listen (spec 098) and chain emitter. New code is one new internal subpackage (`internal/github`), three new files under `internal/factory/`, and a small dispatch path under `cmd/chitin-orchestrator/`. No new top-level project.

## Complexity Tracking

No constitutional violations. No complexity exemptions.
