# Implementation Plan: Operator Report Delivery

**Branch**: `feat/085-operator-report-delivery` | **Date**: 2026-05-22 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/085-operator-report-delivery/spec.md`

## Summary

Deliver two operator-facing reports to the operator's Discord: a frequent
**heartbeat** (gateway/kernel/agent liveness + last redeploy) and a daily +
on-demand **telemetry digest** (orchestration, kernel layers, per-driver
activity, PRs per driver) — a skimmable message with click-through links into
chitin-console.

Approach (Phase 0 research): report **composition** is a pure Go subcommand
(`chitin-kernel report {heartbeat|digest}`, backed by `internal/report/`) that
reads telemetry and prints a message; report **delivery** is a thin swarm
script that pipes that output to `openclaw message send --channel discord`
(Clawta is the deliverer — it already has production Discord posting; the Hermes
Ares agent does not). The heartbeat and digest are two Temporal Schedule
`JobSpec`s (the spec 081 pattern). The split keeps the kernel side-effect-free
(Constitution §1).

## Technical Context

**Language/Version**: Go 1.24 (`go/execution-kernel` and `go/orchestrator`
modules); Bash for the delivery script.

**Primary Dependencies**: Temporal Schedules (existing `go/orchestrator/schedules`
substrate); `openclaw` CLI (Discord egress, already wired); `gh` CLI (PRs per
driver); the existing `internal/health` and `internal/gov` kernel packages.

**Storage**: Reads existing JSONL telemetry sinks (`~/.chitin/gov-decisions-*.jsonl`,
`~/.chitin/events-*.jsonl`) and `chitin-kernel health` output. Writes one new
append-only delivery log (`~/.cache/chitin/operator-report.jsonl`) mirroring
`install-kernel.jsonl`. No new database.

**Testing**: `go test` for `internal/report/` (composition, grouping, link
formatting, degradation); `bash -n` + the `install_kernel_script_test.go`
harness pattern for the delivery script; `quickstart.md` as the acceptance pass.

**Target Platform**: Linux — the operator's gateway box.

**Project Type**: A CLI subcommand + two scheduled jobs + a delivery script,
inside the chitin polyglot monorepo. No new standalone project.

**Performance Goals**: Heartbeat composes in < 2 s. On-demand digest delivered
within 2 minutes (SC-002).

**Constraints**: The `chitin-kernel report` command MUST NOT produce side
effects (Constitution §1) — it only reads, composes, and prints. Reports MUST
degrade to a partial report on a missing telemetry source (FR-009) and MUST NOT
fail a delivery silently (FR-010).

**Scale/Scope**: One operator destination; ~6 drivers; one hourly heartbeat,
one daily digest, plus on-demand. Message volume is small and bounded.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Status | Notes |
|---|---|---|
| §1 Side-effect boundary | ✅ PASS | `chitin-kernel report` only reads telemetry and prints. The Discord side effect is performed by `openclaw message send` in the swarm delivery script — never by the kernel. |
| §2 Branch & worktree | ✅ PASS | Implementation runs as worker units in worktrees per the platform flow; this plan changes no checkout discipline. |
| §3 Spec-kit gate | ✅ PASS | `specs/085-operator-report-delivery/spec.md` exists; this feature is spec-kit-tracked. |
| §4 Tracked installers | ✅ PASS | `swarm/bin/deliver-operator-report.sh` ships with `swarm/bin/install-operator-report.sh` in the same PR. |
| §5 Board-aware scripts | ✅ PASS | The digest's orchestration section accepts `--board` / `KANBAN_BOARD`; `chitin` is a default only. |
| §6 Swarm is the exception | ✅ PASS | Report composition is kernel-local (`internal/report/`, `cmd/chitin-kernel/`); only the cross-cutting `openclaw` delivery glue lives in `swarm/`. |

**Result**: No violations. Complexity Tracking is not required.

## Project Structure

### Documentation (this feature)

```text
specs/085-operator-report-delivery/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   └── report-contracts.md
├── checklists/
│   └── requirements.md  # Spec quality checklist (/speckit-specify output)
└── tasks.md             # Phase 2 output (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
go/execution-kernel/
├── internal/report/
│   ├── heartbeat.go        # heartbeat composition — liveness + last redeploy
│   ├── digest.go           # digest composition — 4 sections, console links
│   ├── sources.go          # telemetry adapters: health, gov-decisions, gh PRs
│   ├── render.go           # message rendering (skimmable text + links)
│   └── *_test.go           # unit tests — grouping, links, degradation, bounds
└── cmd/chitin-kernel/
    └── report.go           # `chitin-kernel report {heartbeat|digest}` subcommand

swarm/bin/
├── deliver-operator-report.sh     # run `chitin-kernel report`, post via openclaw
└── install-operator-report.sh     # tracked installer (Constitution §4)

go/orchestrator/schedules/
├── operator_heartbeat.go   # JobSpec — hourly
├── operator_digest.go      # JobSpec — daily
└── schedules.go            # Registry() updated to include both

apps/chitin-console-api/
└── src/server.mjs          # new read-only endpoints for report detail views (US2/US3, as needed)
```

**Structure Decision**: Extend the existing chitin polyglot monorepo — no new
top-level project. Report-composition logic is kernel-local Go under
`go/execution-kernel/internal/report/` and a `cmd/chitin-kernel` subcommand;
delivery is one `swarm/bin` script plus its installer; scheduling is two
`JobSpec`s in `go/orchestrator/schedules/`. Console detail views are additive
endpoints on the existing `chitin-console-api`.

## Phasing (user stories → delivery slices)

1. **US1 — Heartbeat (P1, MVP)**: `report heartbeat` + delivery script +
   `operator_heartbeat` JobSpec. Proves compose → openclaw → Discord, scheduled.
2. **US2 — Telemetry digest (P2)**: `report digest` (4 sections + console
   links) + `operator_digest` JobSpec + the on-demand Discord trigger + any new
   chitin-console detail endpoints.
3. **US3 — Research & Obsidian delivery (P3)**: route research-report and
   Obsidian-note publication through the same delivery script/channel.
