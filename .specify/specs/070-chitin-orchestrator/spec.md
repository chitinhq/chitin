# Feature Specification: Chitin Orchestrator

**Feature Branch**: `070-chitin-orchestrator`

**Created**: 2026-05-20

**Status**: Draft

**Input**: User description: "Chitin Orchestrator — a single deterministic, observable orchestration layer built on Temporal (Go SDK) that replaces today's orchestration sprawl: ~36 cron jobs, ~52 swarm/bin shell scripts, lobster dispatch, and the agent-bus. Each becomes a durable Temporal workflow. Goal: bring determinism and telemetry into swarm orchestration so the swarm can run fully autonomously."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Orchestration you can see and trust (Priority: P1)

The operator opens one place and sees every orchestration action the swarm
has taken — each pull-loop tick, each scheduled run — as a durable,
timestamped, replayable record. When something misbehaves, the operator
opens the exact run, sees every step and its inputs, and knows definitively
what happened — no guessing whether a cron fired.

**Why this priority**: This is the thesis — determinism + telemetry *in*
orchestration. Without it the swarm cannot be trusted to run autonomously.
It is also the smallest provable slice: one workflow (the kanban pull-loop)
carries the whole value.

**Independent Test**: Migrate the kanban pull-loop to a durable workflow,
run it beside the existing cron for one day, and confirm every tick is
individually inspectable and replayable and the two produce equivalent
board mutations.

**Acceptance Scenarios**:

1. **Given** the pull-loop runs as a workflow, **When** the operator looks at the orchestrator, **Then** every tick of the last 24h is listed with its start time, inputs, each step, and outcome.
2. **Given** a pull-loop tick errored, **When** the operator opens that run, **Then** the failing step and its inputs are visible without reading raw logs.
3. **Given** two ticks over the same board state, **When** they are replayed, **Then** they produce identical decisions.

---

### User Story 2 - Failure recovery is deterministic (Priority: P2)

When a gateway process crashes or is restarted mid-dispatch, the in-flight
orchestration resumes exactly where it stopped — no duplicated PRs, no lost
tickets, no half-applied state. Retries and timeouts are declared once, not
re-implemented per script.

**Why this priority**: Durability is what makes "autonomous" *safe*. Today a
restart mid-dispatch can duplicate or strand work. Proven on the dispatch
pipeline.

**Independent Test**: Migrate the dispatch pipeline to a workflow; kill the
host process mid-dispatch; confirm on restart it resumes and produces
exactly one PR / one ticket transition.

**Acceptance Scenarios**:

1. **Given** a dispatch workflow is mid-flight, **When** the host process is killed and restarted, **Then** the workflow resumes from its last completed step.
2. **Given** an activity fails transiently, **When** it is retried, **Then** the declared retry policy is applied automatically.
3. **Given** a dispatch completes, **When** the same trigger fires again, **Then** no duplicate PR or ticket transition is created.

---

### User Story 3 - One orchestrator, zero sprawl (Priority: P3)

All swarm orchestration — pull loops, dispatch, pollers, watchdogs, the
board engine, the Icarus bench loop — runs as workflows in one orchestrator.
The ~36 cron jobs, ~52 shell scripts, lobster, and the agent-bus are
retired. Orchestration lives in one place, observed in one place.

**Why this priority**: The end state — delivers the "decrease scope, kill
sprawl" goal, but only after P1/P2 prove the model.

**Independent Test**: Inventory orchestration before and after; confirm each
retired cron/script has a corresponding workflow and is removed; confirm no
orchestration action originates outside the orchestrator.

**Acceptance Scenarios**:

1. **Given** a cron/script has been migrated, **When** its workflow is proven for one week, **Then** the cron/script is deleted and does not return.
2. **Given** the migration is complete, **When** the orchestration surface is inventoried, **Then** every orchestration action traces to a workflow.

---

### Edge Cases

- A workflow's code changes while instances are mid-flight — the orchestrator MUST preserve replay determinism across code versions.
- The orchestrator's backing service is down — orchestration pauses and MUST resume cleanly when it returns; no work is lost.
- A workflow is long-running by design (the pull-loop never "ends") — the orchestrator MUST support indefinitely-running workflows without unbounded history growth.
- A cron is cut over to its workflow while a run is in flight — the cutover MUST NOT double-run or drop the in-flight tick.
- An agent (Ares/Clawta) the orchestrator coordinates is unavailable — the workflow MUST wait/retry per policy, not fail silently.
- Two work units run concurrently — each MUST run in its own isolated worktree and never observe the other's changes; the shared checkout is never a work surface.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Every swarm orchestration unit (pull loop, dispatch, poller, watchdog, board engine, bench loop) MUST run as a durable workflow whose execution survives process restarts.
- **FR-002**: Every workflow run MUST be individually inspectable after the fact — inputs, every step, every retry, and outcome — without reading raw logs.
- **FR-003**: Workflow execution MUST be deterministic — replaying a run over the same inputs MUST produce the same decisions.
- **FR-004**: Retry and timeout behavior MUST be declared as workflow policy, not re-implemented ad hoc per script.
- **FR-005**: Side-effecting steps MUST be exactly-once per workflow run — a restart MUST NOT duplicate a PR, ticket transition, or dispatch.
- **FR-006**: Migration MUST be incremental — a new workflow MUST be able to run alongside the cron/script it replaces until proven.
- **FR-007**: A cron job or shell script MUST be deleted once its replacement workflow is proven; the orchestrator MUST become the single origin of orchestration.
- **FR-008**: The orchestrator MUST emit run telemetry to the Chitin Telemetry layer.
- **FR-009**: The orchestrator MUST support indefinitely-running workflows without unbounded state growth.
- **FR-010**: The orchestrator MUST NOT depend on the agent-bus (decommissioned — spec 069); it coordinates agents directly (see FR-015/016).
- **FR-011**: The operator MUST be able to start, stop, inspect, and replay any workflow.
- **FR-012**: A workflow code change MUST NOT break in-flight runs — version/replay safety is required.
- **FR-013**: Every work unit the orchestrator dispatches MUST execute as a **worker** process in its own **dedicated git worktree** — never in the primary/shared repository checkout. The platform flow always uses workers + worktrees; orchestration and work never mutate the shared checkout.
- **FR-014**: A worktree MUST be created fresh per work unit and removed on completion; a worktree orphaned by a crashed worker MUST be reclaimable (garbage-collected) by the orchestrator.
- **FR-015**: The orchestrator MUST be the source of truth for work **sequencing and scheduling** — derived deterministically (mathematically) from the spec task graph. No heuristic optimizer; no human-managed kanban deciding order.
- **FR-016**: A kanban / activity log is a **telemetry read-surface only** — a projection of orchestrator state that humans may read. The orchestrator MUST NOT depend on it to decide what runs next. (The Hermes Kanban's driving role moves into the orchestrator; its readable role moves into Chitin Telemetry — the Hermes Kanban is end-of-life.)
- **FR-017**: The orchestrator MUST be **agent-agnostic and driver-agnostic** — no dependency on Hermes plugins or the Hermes Kanban. Any agent — Ares, Clawta, Claude Code, Copilot, or a future first-party Chitin agent — is a routing choice, not an architectural dependency.

### Key Entities

- **Workflow**: A durable, deterministic orchestration unit — one per former cron/script (pull-loop, dispatch, poller, …). Carries a full run history.
- **Activity**: A single side-effecting step within a workflow (a board mutation, a `gh` call, an agent invocation) — retryable, timeout-bounded.
- **Orchestrator service**: The runtime that executes and persists workflows and exposes their history.
- **Migration register**: The mapping of each legacy cron/script → its replacement workflow → its retirement status.
- **Worker**: An isolated process that executes exactly one work unit (a driver — codex/copilot/gemini/claude-code — or a generic task runner). Spawned by a workflow; short-lived.
- **Worktree**: A dedicated git worktree, created fresh per work unit — the worker's isolated filesystem, torn down on completion. The shared checkout is never the work surface.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of orchestration actions are attributable to a named workflow run; zero originate from an un-migrated cron or script after migration completes.
- **SC-002**: Any orchestration failure can be diagnosed from its workflow run alone — no log-grepping — in under 2 minutes.
- **SC-003**: A gateway restart mid-orchestration loses zero work and creates zero duplicates.
- **SC-004**: The orchestration surface shrinks from ~36 cron jobs + ~52 shell scripts to a count near zero.
- **SC-005**: The pull-loop (P1 slice) runs as a workflow for 7 consecutive days with every tick inspectable before the next slice begins.
- **SC-006**: Time to answer "did orchestration X run, and what did it do?" drops to seconds (open the run) from minutes (guessing + logs).
- **SC-007**: Zero dispatched work units run in or mutate the primary repository checkout — every unit is isolated in its own worktree.

## Assumptions

- **Temporal is the chosen engine** (operator decision 2026-05-20; see `docs/strategy/chitin-orchestrator-options-2026-05-20.md`). This spec treats durable-workflow execution as a given and does not re-litigate the engine choice.
- The orchestrator runs on the existing single box (one-operator dogfood); a self-hosted single-binary deployment is sufficient. Managed/cloud hosting is out of scope until scale demands it.
- The orchestrator is written in Go, consistent with the Chitin Kernel.
- The agents (Ares, Clawta, Claude Code) remain the reasoning layer; the orchestrator coordinates and schedules them — it does not replace agent reasoning.
- The agent-bus is being decommissioned in parallel (spec 069); the orchestrator never depends on it.
- The design thinking in the retired "Octi" specs 040–048 is the starting basis, re-homed under "Chitin Orchestrator."
- The Chitin Board remains the coordination substrate and Chitin Telemetry remains the observability sink; this spec integrates with both, it does not redefine them.

## Out of Scope

- Replacing agent reasoning or the agents themselves.
- Extracting the Chitin Board from Hermes (separate spec).
- LLM-internal agent reasoning graphs (LangGraph-style) — a different layer.
- Multi-box / clustered / cloud deployment of the orchestrator.
