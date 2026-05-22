# Feature Specification: Orchestrator Operational Completion

**Feature Branch**: `080-orchestrator-ops-completion`

**Created**: 2026-05-21

**Status**: Draft

**Input**: User description: "Orchestrator operational completion: Gemini and Copilot agent drivers, a Discord notification surface, and a first-class chitin-console service"

## Overview

The Chitin Orchestrator (spec 070) is functional end to end — a spec compiles
to a Work-Unit DAG, the scheduler walks it, work units run in isolated
worktrees and ship PRs. This feature closes three operational gaps surfaced
while reviewing that working system:

1. Two installed agent runtimes — **Gemini** and **Copilot** — are not yet
   drivers, so the scheduler cannot route to them.
2. The orchestrator has **no human-facing notification surface**. The old
   persistent Hermes/Clawta agents posted to Discord; the driver model
   replaced those agents, and nothing posts to Discord now.
3. **chitin-console has no service** — it runs only as an ad-hoc dev server,
   so the operator's review surface keeps disappearing.

Each is an independent slice; none blocks another.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Gemini and Copilot agent drivers (Priority: P1)

The operator wants the orchestrator to route work to every agent runtime the
box has, not just five of them. Gemini and Copilot are installed but
unreachable. Adding them as drivers takes the roster to seven peers and — for
Copilot specifically — gives the `code.review` capability a home.

**Why this priority**: The orchestrator's value scales with driver coverage.
This is the lowest-risk slice (an additive, agent-agnostic extension that
touches no scheduling logic) and the highest-leverage (more routable
capability for every future run).

**Independent Test**: Register the two drivers, start the worker host, and run
driver selection for a capability each declares — the node routes to the new
driver. Fully testable without the Discord or console work.

**Acceptance Scenarios**:

1. **Given** the `gemini` CLI is on the worker's PATH, **When** the worker
   host starts, **Then** the `gemini` driver is in the registry and reports
   Ready.
2. **Given** the `copilot` CLI is on the worker's PATH, **When** a node
   requiring `code.review` is routed, **Then** driver selection can choose
   `copilot`.
3. **Given** a new driver's CLI is absent from PATH, **When** its readiness is
   probed, **Then** it reports not-ready with a reason and the scheduler routes
   elsewhere — exactly as the existing five drivers behave.

---

### User Story 2 - Discord notification surface (Priority: P2)

The operator wants an ambient feed of what the swarm is doing. The orchestrator
posts work events — a work unit completed, a PR opened, a node blocked, a run
stalled — to a Discord channel. It is strictly one-way: the orchestrator posts
*out*; it never receives or pulls work *in*. Dispatch stays DAG-driven; Discord
is a human surface, decoupled from scheduling.

**Why this priority**: It restores a human-facing surface the old
persistent-agent model provided, without re-coupling dispatch to Discord. It is
observe-only, so it carries no scheduling risk.

**Independent Test**: Run a scheduler tick that completes a work unit; with a
webhook configured, a Discord message is posted; with none configured, the post
degrades to a logged no-op. Testable without the driver or console work.

**Acceptance Scenarios**:

1. **Given** a Discord webhook is configured, **When** a work unit opens a PR,
   **Then** a message carrying the PR URL is posted to the channel.
2. **Given** no Discord webhook is configured, **When** a notification event
   fires, **Then** the notifier logs and drops it — the workflow never faults.
3. **Given** the Discord endpoint is unreachable or rate-limited, **When** a
   post is attempted, **Then** the failure is logged and the workflow proceeds
   unaffected.
4. **Given** the `/orchestrator` console diagram, **When** it is viewed,
   **Then** it shows a human-surfaces lane (Discord notifications, console,
   GitHub PRs) so the dispatch-vs-human-surface boundary is explicit.

---

### User Story 3 - chitin-console as a first-class service (Priority: P3)

The operator wants the console to be *always there* — not an `nx serve` process
that must be hand-started and vanishes on reboot. chitin-console runs as a
persistent systemd user service serving its built bundle.

**Why this priority**: The console is the operator's review surface; it must be
always-on. It is the smallest slice and unblocks reviewing everything else, but
it is pure ops with no effect on orchestrator behavior.

**Independent Test**: Install the unit, start the service, and curl the console
— HTTP 200. Restart the box (or the service) and confirm it returns with no
human action.

**Acceptance Scenarios**:

1. **Given** the console service is installed and enabled, **When** the box
   restarts, **Then** the console is reachable again with no human action.
2. **Given** the console service is running, **When** the operator opens
   `/orchestrator`, **Then** the architecture diagram renders.
3. **Given** the console service crashes, **When** the supervisor notices,
   **Then** it is restarted automatically.

---

### Edge Cases

- A new driver's CLI binary is missing from PATH → the driver reports
  not-ready; the scheduler does not select it (existing AgentDriver contract).
- A new driver's CLI exits non-zero → the work unit settles failed with the
  driver's stderr, exactly as for the existing CLI drivers.
- The Discord webhook URL is unset, malformed, unreachable, or rate-limited →
  the notifier degrades to a logged no-op; no workflow ever fails on it.
- A notification payload exceeds Discord's message-length limit → it is
  truncated before posting.
- The console service starts before its bundle is built → the deploy/install
  step builds the bundle first; the service serves whatever bundle exists.
- Two console services bind the same port → the install is idempotent and the
  port is a single declared value.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide a Gemini agent driver implementing the
  spec-075 AgentDriver contract, with stable id `gemini`, invoking the
  `gemini` CLI in the work unit's dedicated worktree.
- **FR-002**: The system MUST provide a Copilot agent driver implementing the
  spec-075 AgentDriver contract, with stable id `copilot`, invoking the
  `copilot` CLI in the work unit's dedicated worktree.
- **FR-003**: Both new drivers MUST be registered into the driver registry at
  worker-host startup, so the registry exposes seven drivers.
- **FR-004**: Each new driver's capability card MUST declare only capabilities
  the runtime genuinely supports, drawn from the spec-075 closed taxonomy.
- **FR-005**: The orchestrator MUST emit a notification for each of: a work
  unit completing (done or failed), a pull request opened, a node becoming
  blocked-unroutable, and a scheduler run reaching a terminal state
  (complete or stalled).
- **FR-006**: The Discord notification path MUST be write-only — it never
  receives, polls, or dispatches work. Work dispatch remains DAG-driven.
- **FR-007**: A missing, malformed, unreachable, or rate-limited Discord
  configuration MUST degrade to a logged no-op; a notification fault MUST
  never fail or stall a workflow.
- **FR-008**: chitin-console MUST run as a persistent systemd user service
  serving its built bundle on a single declared port.
- **FR-009**: The console service MUST restart automatically on failure and
  start on boot, and its installer MUST be idempotent.
- **FR-010**: The `/orchestrator` console diagram MUST be updated to show the
  human-surfaces lane — Discord notifications, the console, and GitHub PRs —
  distinct from the dispatch path.

### Key Entities

- **Agent Driver** (spec 075): the existing contract — `gemini` and `copilot`
  are two new conforming instances; the roster grows from five to seven.
- **Notification Event**: a typed, write-only record the orchestrator emits to
  the human surface — an event kind, the work unit / run it concerns, and a
  human-readable summary (PR URL, blocked reason, terminal status).
- **Console Service**: the systemd user unit plus the served static bundle
  that make chitin-console a persistent, always-on review surface.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The driver registry reports seven drivers; a node whose
  capability only Gemini or only Copilot satisfies routes to that driver.
- **SC-002**: With a webhook configured, every completed work unit and every
  opened PR produces exactly one Discord message — no duplicates, none missed.
- **SC-003**: A total Discord outage produces zero workflow failures and zero
  stalled runs.
- **SC-004**: After a box restart, the console answers HTTP 200 with no human
  action.
- **SC-005**: The `/orchestrator` diagram renders the human-surfaces lane, so a
  viewer can tell dispatch from notification at a glance.

## Assumptions

- The `gemini` and `copilot` CLIs are installed and on the orchestrator
  service's PATH (verified: `gemini` at `~/.vite-plus/bin`, `copilot` at
  `~/.local/bin`; both already covered by the unit PATH from spec-070 work).
- Discord delivery is via an incoming webhook URL supplied through environment
  configuration; a stateful Discord bot is out of scope.
- Discord remains a strictly one-way human surface — two-way Discord control of
  the orchestrator is explicitly out of scope.
- The console service serves the static production build; a rebuild-on-deploy
  step (the installer) produces the bundle.
- Phase 3–5 of the spec-070 rollout (migrating the remaining crons, watchdogs,
  and the Icarus bench into workflows) is the rollout of spec 070 and is out of
  scope for this feature.
- The new drivers run agents headless and one-shot per work unit, identical to
  the existing hermes / codex / openclaw drivers — no persistent agent process.
