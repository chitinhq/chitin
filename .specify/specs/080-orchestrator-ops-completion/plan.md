# Implementation Plan: Orchestrator Operational Completion

**Branch**: `080-orchestrator-ops-completion` | **Date**: 2026-05-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/080-orchestrator-ops-completion/spec.md`

## Summary

Three independent operational slices on top of the working Chitin Orchestrator
(spec 070): add **Gemini** and **Copilot** as agent drivers, give the
orchestrator a **write-only Discord notification surface**, and make
**chitin-console a persistent systemd service**. None is new architecture —
each follows an established pattern (the spec-075 AgentDriver contract, a
Temporal write-only activity, a systemd user unit). The three slices share no
code and ship as three independent PRs.

## Technical Context

**Language/Version**: Go 1.23 (`go/orchestrator/`); TypeScript / Angular
(`apps/chitin-console/`); Bash (systemd installers).

**Primary Dependencies**: Temporal Go SDK; the existing `driver`, `activities`,
and `workflows` packages; the spec-075 AgentDriver contract; Angular + mermaid;
`systemctl --user`.

**Storage**: N/A — the drivers and the notifier are stateless. The notifier is
write-only; it persists nothing.

**Testing**: `go test ./driver/... ./activities/... ./workflows/...` (driver
and activity unit tests, table-driven); `nx build chitin-console`; systemd
verification via the installer's `--verify` mode.

**Target Platform**: Linux — the orchestrator worker host (systemd `--user`).

**Project Type**: Extension of an existing multi-package Go service, an Angular
app, and a set of systemd units. No new project.

**Performance Goals**: The notifier is off the scheduling critical path; a post
is best-effort. Driver invocation latency is bounded by the agent, not the
orchestrator.

**Constraints**: The notifier MUST NOT fault or stall a workflow under any
Discord failure. New drivers MUST honor the AgentDriver readiness contract
(absent binary → not-ready). The console unit installer MUST be idempotent.

**Scale/Scope**: Seven drivers total; notification volume is a handful of
events per scheduler run; one console service on one port.

## Constitution Check

*GATE: Must pass before implementation. Re-checked after each slice.*

- **Workflows over agents** — the Discord notifier is a deterministic Temporal
  *activity*, not an agent; the drivers are agent runtimes invoked only through
  the existing agent-node path. PASS.
- **Determinism** — the notifier performs its I/O inside an activity; workflow
  code only decides *that* an event fires, never does the network call. PASS.
- **Governance** — the notifier is strictly write-only (no inbound tool-call
  surface); the new drivers are gated by the kernel exactly as the existing
  five. PASS.
- **Spec-in / PR-out** — this work is itself spec-driven (this spec) and ships
  as reviewable PRs. PASS.

No violations; Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/080-orchestrator-ops-completion/
├── spec.md      # feature specification
├── plan.md      # this file
└── tasks.md     # task breakdown (/speckit-tasks output)
```

### Source Code (repository root)

```text
go/orchestrator/
├── driver/
│   ├── gemini/driver.go         # US1 — new: Gemini AgentDriver
│   ├── gemini/driver_test.go    # US1 — new
│   ├── copilot/driver.go        # US1 — new: Copilot AgentDriver
│   └── copilot/driver_test.go   # US1 — new
├── activities/
│   ├── notify.go                # US2 — new: Discord notification activity
│   └── notify_test.go           # US2 — new
├── workflows/
│   └── work_unit.go             # US2 — emit notification events post-delivery
├── workflows/scheduler.go       # US2 — emit run-terminal / blocked events
└── cmd/chitin-orchestrator/main.go  # US1 register 2 drivers; US2 wire notifier

apps/chitin-console/src/app/pages/orchestrator-diagram.page.ts  # US2 — diagram lane

swarm/systemd/chitin-console.service   # US3 — new: console systemd user unit
swarm/bin/install-chitin-console.sh    # US3 — new: idempotent console installer
```

**Structure Decision**: No new packages beyond two driver sub-packages
(`driver/gemini`, `driver/copilot`) that mirror the existing
`driver/hermes` etc., and one activity file (`activities/notify.go`) that
mirrors the existing write-only activities (board projection, telemetry). The
console service reuses the `swarm/systemd` + `swarm/bin` install pattern of the
orchestrator unit.

## Implementation Phases

The three user stories are independent and ship as separate PRs.

- **Phase 1 — US1: Gemini + Copilot drivers (P1).** Two driver sub-packages
  implementing the spec-075 AgentDriver contract — `Ready` probes the CLI on
  PATH, `Invoke` shells out in the worktree, `Card` declares the runtime's
  capabilities. Register both in `main.go`. Driver unit tests. The registry
  goes from five to seven.

- **Phase 2 — US2: Discord notification surface (P2).** A `notify.go`
  write-only activity that posts a typed Notification Event to a Discord
  webhook (URL from env); a missing/failing webhook degrades to a logged
  no-op. `work_unit.go` and `scheduler.go` emit events (work-unit settled, PR
  opened, blocked-unroutable, run terminal). Update the `/orchestrator` diagram
  with the human-surfaces lane. Activity unit tests cover the configured,
  unconfigured, and unreachable cases.

- **Phase 3 — US3: chitin-console service (P3).** A `chitin-console.service`
  systemd user unit serving the built bundle, plus an idempotent
  `install-chitin-console.sh` (build the bundle, install the unit, enable +
  start) with `--verify` and `--remove` modes — same shape as
  `install-chitin-orchestrator.sh`.

## Complexity Tracking

No constitution violations — this section is intentionally empty.
