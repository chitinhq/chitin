# Implementation Plan: Spec 103 — Swarm Work Orchestration

**Branch**: `spec/103-swarm-research-orchestration` | **Date**: 2026-05-24 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `.specify/specs/103-swarm-research-orchestration/spec.md`

## Summary

Evolve the chitin factory from "implementation orchestrator" to **scheduler + observer + ingester for the swarm**. Three jobs:

1. **Schedule** — fire `(agent, cadence, message, tag?, skills?)` tuples through their gateways (`ares→hermes-mcp`, `clawta→openclaw-cli`) via Temporal Schedules, emit `swarm_invocation` chain events.
2. **Observe** — kernel chain captures every tool call (already on main). New `swarm-summary` CLI aggregates the chain by `(agent, tag, day)` so ecosystem work that produces no queue rows is still visible.
3. **Ingest** — `IngestionWorkflow` watches `~/Documents/Obsidian Vault/Research/<TOPIC>/sources/` (and other operator-declared paths), parses frontmatter, writes a row to `~/.chitin/swarm-results.db`. Chain-mining output routes into sentinel's existing finding surface via the recommended `~/.chitin/sentinel-findings/swarm-mined-*.json` path.

Critical framing: chitin does NOT classify swarm work. `message`, `tag`, and `skills` are operator-defined free-form. Chitin validates structure only. Recognized starter recipes live in `docs/operator/swarm-recipes.md` as runbook examples, NOT as a closed enum.

Three load-bearing constraints baked into the design:
- **Security boundary** (FR-008): Hermes MCP + OpenClaw CLI stay local-stdio. Never HTTP.
- **Private surfaces** (FR-017): research findings + chain-mining outputs stay off the OSS repo. Local SQLite + filesystem only.
- **Open by design**: schedule entries are free-form; new use cases (calendar, browser, customer triage) work without spec amendments.

## Technical Context

**Language/Version**: Go 1.25.

**Primary Dependencies**:
- `go/orchestrator/cmd/chitin-orchestrator/` — existing CLI; adds `swarm-*` subcommands
- `go/orchestrator/workflows/` — existing Temporal workflows; adds `SwarmInvocationWorkflow`, `IngestionWorkflow`, `SwarmAskWorkflow`
- Spec 081 `EnsureSchedules` pattern — reused for `ensure-swarm`
- `github.com/fsnotify/fsnotify` — vault watch
- `modernc.org/sqlite` (pure-Go, no cgo) — `swarm-results.db`
- `gopkg.in/yaml.v3` — config parsing
- `os/exec` — OpenClaw CLI subprocess
- Hermes MCP stdio client (JSON-RPC over stdin/stdout) — small internal client

**Storage**:
- `~/.chitin/swarm-schedule.yml` — operator-declared schedule entries (read-only to chitin)
- `~/.chitin/ingestion-sources.yml` — operator-declared filesystem sources
- `~/.chitin/swarm-results.db` — SQLite queue (private, never committed)
- `~/.chitin/sentinel-findings/swarm-mined-*.json` — convention path the sentinel watcher picks up
- Chain events at `~/.chitin/events-*.jsonl` — append-only, kernel-emitted

**Testing**:
- Unit: `go test ./...` per package
- Integration: `go test ./test/swarm_e2e_test.go` (mock gateway adapters; assert chain events + queue rows)
- Adapter mock: a fake `openclaw` binary on PATH that prints a deterministic JSON envelope
- fsnotify integration: temp dir with fixtures; assert queue row within ingestion debounce window

**Target Platform**: Linux (operator host); GitHub Actions CI for unit tests; integration tests gated on operator-host smoke (requires real Hermes + OpenClaw).

**Project Type**: Single Go project (orchestrator additions); no new top-level dirs.

**Performance Goals**:
- SC-001: vault ingestion p99 < 60s per touch over 100 sequential creates
- SC-002: 50 orchestrator restarts/week leave every schedule firing on cadence
- SC-004: operator triage round-trip median < 1 day
- SC-007: ecosystem-tagged schedules surface ≥ 80% tool-call breakdown in `swarm-summary`

**Constraints**:
- §1: every adapter subprocess MUST route through `chitin-kernel gate evaluate`. `os/exec` calls are tool calls per the gate.
- §7: scheduled invocations are orchestrator-intaked work-units; ad-hoc `swarm-ask` is operator-initiated (constitutional under §7 ad-hoc rules).
- §1: three new event types via existing kernel emit (`swarm_invocation`, `swarm_finding_queued`, `swarm_finding_triaged`).
- **FR-008 security boundary**: adapter code MUST NOT contain HTTP listen, tunnel setup, or remote-callable wrappers. Enforced by code review (`contracts/gateway-adapter.md` documents the invariant).
- **FR-017 privacy**: `~/.chitin/swarm-results.db` is local-only.

**Scale/Scope**: Single operator, single host. Initial schedules ~5–10; growth bounded by operator's runbook entries. Chain mining window = 24h by default; the ~5,116-event chain on the host today fits comfortably.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| § | Rule | Compliance |
|---|---|---|
| §1 | Kernel gates every tool call | Adapter subprocesses (`openclaw sessions send`, `hermes mcp serve`) route through PreToolUse hooks; chain emit through existing kernel path. ✅ |
| §1 | Only the kernel writes chain events | All three new event types use `emitChainEvent`; no direct file writes. ✅ |
| §2 | Workers + worktrees for implementation | Scheduled swarm invocations are NOT implementation work-units (no code mutation). No worktree allocation. ✅ |
| §3 | Spec-kit promotion gate | This plan IS the spec-kit entry. ✅ |
| §4 | Tracked installers | All work lands inside existing `chitin-orchestrator` binary. No new operator-box script. ✅ |
| §5 | Board-aware kanban | Not applicable. ✅ |
| §6 | Swarm/ exception | No new `swarm/` artifact. ✅ |
| §7 | Implementation gate | Spec 103 does NOT initiate code mutations. Scheduled invocations may produce findings that LATER drive spec authoring (spec 078) — separate pass. The invocations themselves are kernel-gated, DAG-free ad-hoc work-units. ✅ |
| §7 | Driver routing | Not applicable — fixed agent→gateway mapping. ✅ |
| §7 | Telemetry at every observable layer | `swarm_invocation` per firing; gateway adapters propagate kernel observation; `swarm_finding_queued` per ingestion; `swarm_finding_triaged` per operator action; `swarm-summary` aggregates the chain for ecosystem-work observability. ✅ |

**Special gate: FR-008 security boundary.** No code change exposes the gateway over HTTP, opens a listener port, or wraps it as a network-callable service. Documented as a code-review invariant in `contracts/gateway-adapter.md`.

No violations.

## Project Structure

### Documentation (this feature)

```text
.specify/specs/103-swarm-research-orchestration/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── swarm-schedule-config.md
│   ├── ingestion-sources-config.md
│   ├── gateway-adapter.md
│   ├── chain-events.md
│   ├── swarm-cli.md
│   └── queue-schema.md
└── tasks.md             # Phase 2 output (via /speckit-tasks)
```

### Source Code (repository root)

```text
go/orchestrator/
├── cmd/chitin-orchestrator/
│   ├── main.go                                # MODIFY: register swarm-* subcommands
│   ├── swarm_schedules.go                     # NEW: ensure-swarm subcommand
│   ├── swarm_ask.go                           # NEW: swarm-ask subcommand
│   ├── swarm_queue.go                         # NEW: swarm-queue list/show/mark
│   ├── swarm_summary.go                       # NEW: chain-aggregation CLI
│   └── *_test.go                              # NEW per file
├── internal/
│   ├── swarm/
│   │   ├── schedule_config.go                 # NEW: parse swarm-schedule.yml
│   │   ├── ingestion_config.go                # NEW: parse ingestion-sources.yml
│   │   ├── queue_db.go                        # NEW: SQLite open + migrate
│   │   ├── queue_repo.go                      # NEW: typed queries (list, show, mark, insert)
│   │   └── *_test.go
│   ├── gateway/
│   │   ├── openclaw.go                        # NEW: OpenClaw CLI adapter
│   │   ├── hermes_mcp.go                      # NEW: Hermes MCP stdio adapter
│   │   └── *_test.go
│   └── chain/
│       └── swarm_events.go                    # NEW: typed emit helpers
├── activities/
│   └── swarm/
│       ├── send_openclaw.go                   # NEW: SwarmSendOpenClaw activity
│       ├── send_hermes.go                     # NEW: SwarmSendHermes activity
│       ├── ingest_file.go                     # NEW: FetchAndRead + frontmatter parse
│       └── *_test.go
├── workflows/
│   ├── swarm_invocation.go                    # NEW: per-firing workflow
│   ├── ingestion.go                           # NEW (or extend spec 079): fsnotify-driven
│   └── swarm_ask.go                           # NEW: on-demand workflow
└── test/
    └── swarm_e2e_test.go                      # NEW

apps/chitin-console/src/app/pages/
└── swarm-queue.page.ts                        # NEW (PR-C deliverable)

docs/operator/
└── swarm-recipes.md                           # NEW (PR-C): starter recipes runbook
```

**Structure Decision**: Single Go module additions. New packages: `internal/swarm/`, `internal/gateway/`, `internal/chain/swarm_events.go`, `activities/swarm/`. Reuses existing Temporal Schedule pattern (spec 081), existing chain emitter, existing CLI subcommand registration.

## Implementation slices (3 PRs)

Per spec.md risk section: "Until PR-A + PR-B land (~1 week impl), ares + clawta produce nothing scheduled."

- **PR-A** (US1 + US3 + US7 blocking deps): schedule config + `ensure-swarm` + gateway adapters + `SwarmInvocationWorkflow` + `swarm_invocation` chain event. Lets schedules fire.
- **PR-B** (US2 + US5 + US8 stubs): vault ingestion + queue DB + ingestion config + sentinel-findings convention + `swarm_finding_queued` chain event + correlation field plumbing. Lets findings land.
- **PR-C** (US4 + US6 + US7 finish): `swarm-summary` + `swarm-queue list/show/mark` + `swarm-ask` + console page + runbook + `swarm_finding_triaged` event. Operator-visible surfaces.

Each PR's CLI surface is independent of the next; chain events accumulate even before all surfaces ship.

Each PR will need internal slicing under the 2000-line bounds cap (likely 2–4 sub-PRs each).

## Complexity Tracking

No constitutional violations. No complexity exemptions.

**Note on SQLite choice:** `modernc.org/sqlite` (pure-Go) preferred over `mattn/go-sqlite3` (cgo) to keep `chitin-orchestrator` build cgo-free. Verify in Phase 0 research.
