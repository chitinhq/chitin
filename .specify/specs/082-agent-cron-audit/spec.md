# Feature Specification: Agent-Runtime Cron Audit

**Feature Branch**: `082-agent-cron-audit`

**Created**: 2026-05-21

**Status**: Draft

**Input**: User description: "Audit all the cron jobs registered under the Hermes agent (30) and the OpenClaw gateway (9). Inventory every job, classify each as keep / disable / delete / migrate-to-orchestrator, document every decision, and follow spec-driven development."

## Background

Chitin's scheduled work runs in **two distinct layers**:

1. **The systemd cron/timer layer** — ~15 host-level `*.timer`/`*.service`
   units. This layer is **spec 081**'s scope (`cron-migration-board-retirement`)
   and is out of scope here.
2. **The agent-runtime cron layer** — scheduled routines each agent runtime
   registers in its own JSON registry. **This is spec 082's scope**: 30 jobs
   in the Hermes registry (`~/.hermes/cron/jobs.json`) and 9 in the OpenClaw
   registry (`~/.openclaw/cron/jobs.json`) — 39 jobs total.

The agent-runtime cron layer accreted faster than it was pruned. As of the audit
snapshot it carries duplicates (the same watchdog registered four times),
a job failing every run, a large block of board-orchestration jobs that the
spec-070 orchestrator was built to replace, and personal career-tracking
jobs mixed in with swarm infrastructure. No single document records what
each job is for or whether it should still exist. This spec produces that
record and stages the cleanup it implies.

### Job landscape (audit snapshot, 2026-05-21)

**Hermes registry — 30 jobs:**

- **Board / dispatch orchestration (~12)** — `board-watchdog`, `board-audit`,
  `board-grooming-loop`, `autonomous-board-engine`, `kanban-pull-loop`,
  `readybench-poller`, `swarm-invoker`, `swarm-controller-loop`,
  `hermes-clawta-bridge`, `research-intake`, `blocked-ticket-digest`,
  `chitin-watchdog`. These implement the human-managed pull-loop that the
  spec-070 orchestrator / spec-076 spec-DAG scheduler replace. Most are
  already paused.
- **Career-tracking checkpoints (9)** — `anthropic-fde-checkpoint`,
  `glean-fde-cp2`…`glean-fde-cp8`. Personal, one-shot (`repeat.times: 1`),
  self-expiring; not swarm infrastructure.
- **Report generation (3)** — `doc-sync`, `industry-scan`, `chain-summary`
  (all paused).
- **Governance / health canaries (3)** — `chain-governance-canary`,
  `chitin-canary`, `chitin-audit`.
- Plus `glean-fde-checkpoint` (a superseded umbrella of the cp-series).

**OpenClaw registry — 9 jobs:**

- `Memory Dreaming Promotion` — an OpenClaw-core memory feature.
- 8 dispatch jobs — `clawta-kanban-poller`, `clawta-blocked-escalator`,
  `clawta-icarus-board-watcher`, `kanban-pull-loop`, and
  **`clawta-stale-worker-watchdog` registered four times** (once under agent
  `clawta`, three duplicates under agent `glm-agent`).

### Known defects the audit must resolve

- `clawta-stale-worker-watchdog` is registered **4×** — one legitimate
  instance and three duplicates. All four fire every 10 minutes.
- `board-watchdog` **fails every run** (`sqlite3.OperationalError: no such
  column: block_reason`) yet remains enabled and scheduled.
- Career-tracking jobs and swarm-infrastructure jobs share one registry with
  no separation of concern.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A complete, classified cron inventory (Priority: P1)

The operator opens one document and sees every agent-runtime cron job: its
name, owning agent, schedule, current state, last run status, what it does,
and a single recorded decision — keep, disable, delete, or migrate — with the
reason for that decision. Nothing is undocumented; nothing is ambiguous.

**Why this priority**: The audit *is* this inventory. Every later action
(culling, migration) depends on a decision record existing first. It is the
smallest standalone deliverable — pure documentation, zero runtime risk — and
it makes the agent-cron layer legible for the first time.

**Independent Test**: Open the decision log; confirm all 39 jobs appear,
each with all required fields populated and exactly one disposition with a
rationale. No registry is mutated to produce it.

**Acceptance Scenarios**:

1. **Given** the audit is complete, **When** the decision log is read, **Then** all 30 Hermes jobs and all 9 OpenClaw jobs are present — 39 records, none omitted.
2. **Given** any job record, **When** it is inspected, **Then** it carries id, name, owning agent, schedule, enabled/paused state, last run status, a plain-language description, exactly one disposition, and a written rationale.
3. **Given** a job whose disposition is `migrate`, **When** its record is read, **Then** it names the spec-070/076 workflow that will subsume it.
4. **Given** a job that is currently `paused`, **When** its record is read, **Then** `paused` is treated as an undecided state — the record still assigns one of keep/disable/delete/migrate, never "leave paused" as a non-decision.

---

### User Story 2 - Cull the zero-regret cruft now (Priority: P2)

The operator removes the cron jobs whose removal cannot cause a regression:
the three duplicate `clawta-stale-worker-watchdog` registrations, and any job
the audit classifies `delete` outright. The perpetually-failing `board-watchdog`
is fixed or disabled so nothing remains scheduled-but-broken. Each registration
source is corrected so a deleted job does not silently reappear.

**Why this priority**: These are provably safe — a duplicate watchdog and a
job that fails 100% of runs deliver nothing — so they need not wait on the
orchestrator migration. Acting now shrinks the surface the harder migration
work must reason about.

**Independent Test**: After the cull, the OpenClaw registry holds exactly one
`clawta-stale-worker-watchdog`; no job is both enabled and in a perpetual
error state; restart the affected agents and confirm no deleted job
reappears.

**Acceptance Scenarios**:

1. **Given** four `clawta-stale-worker-watchdog` registrations, **When** the cull runs, **Then** exactly one remains and the three duplicates are gone.
2. **Given** the duplicates were created by an agent re-registering on startup, **When** the affected agent restarts, **Then** the duplicates do not return — the registration source was corrected, not just the registry row.
3. **Given** `board-watchdog` fails every run, **When** the cull completes, **Then** it is either fixed (runs succeed) or disabled (no longer scheduled) — it is never left enabled-and-failing.
4. **Given** each registry mutation, **When** it is applied, **Then** a timestamped backup of the registry was taken first.

---

### User Story 3 - Retire the orchestrator-superseded crons (Priority: P3)

The operator retires the board-orchestration and dispatch crons whose function
the spec-070 orchestrator now owns. Each retirement is gated: the cron is
removed only once the orchestrator workflow that replaces it is proven, and
never while both would run at once.

**Why this priority**: This is the largest tranche and the one coupled to
external progress — it can only complete as fast as the orchestrator
workflows prove out (spec 070 Phases 1–3). It ships last because US1 must
record the mapping and US2 must clear the cruft first.

**Independent Test**: Pick one superseded cron with a proven replacement
workflow; confirm the cron's work is covered by the workflow, disable the
cron, and confirm the work still happens — exactly once, from the workflow.

**Acceptance Scenarios**:

1. **Given** a cron mapped to a spec-070/076 workflow, **When** that workflow is proven, **Then** the cron is disabled in the same change that confirms the workflow owns the work.
2. **Given** a cron whose replacement workflow is not yet proven, **When** the audit is executed, **Then** the cron is left enabled (or explicitly disabled with the gap recorded) — never deleted into a coverage gap.
3. **Given** a logical job that exists in both the agent-cron layer and spec 081's systemd layer, **When** dispositions are assigned, **Then** the two specs' decisions for that job agree — no contradiction, no double-run.

---

### Edge Cases

- **Re-registration on restart** — a job deleted from the registry that an
  agent re-creates when it next starts (the likely cause of the triplicated
  watchdog). A durable deletion MUST correct the registration source, not
  just the registry row.
- **Partial coverage** — a cron whose function is only *partly* covered by an
  orchestrator workflow. It MUST NOT be deleted until coverage is complete;
  the gap is recorded instead.
- **One-shot self-expiring jobs** — the career checkpoints have
  `repeat.times: 1` and will not run again once fired. Their disposition MUST
  reflect that they need no active removal.
- **Deletion mid-run** — a job removed while an instance is executing. The
  in-flight run completes; only future scheduling stops.
- **Stale registry backups** — both registries carry many `jobs.json.bak-*`
  files. The audit notes them but does not treat them as live jobs.
- **A job that is both duplicated and superseded** — `kanban-pull-loop`
  exists in both registries. Its records MUST be reconciled to a single
  coherent disposition.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The audit MUST inventory all 39 agent-runtime cron jobs — 30 in the Hermes registry, 9 in the OpenClaw registry — with no job omitted.
- **FR-002**: Each job record MUST capture: id, name, owning agent, schedule, enabled/paused state, last run status, and a plain-language description of what the job does.
- **FR-003**: Each job MUST be assigned exactly one disposition — `keep`, `disable`, `delete`, or `migrate` — accompanied by a written rationale.
- **FR-004**: A `migrate` disposition MUST name the spec-070 / spec-076 workflow that subsumes the job, and MUST NOT be executed until that workflow is *proven* — see `research.md` Decision 4 for the definition of "proven" (the "run beside, then retire" rule shared with spec 081).
- **FR-005**: For each job the audit MUST identify its **registration source** — manually created, created by an agent startup/role script, or otherwise — so that a deletion can be made durable.
- **FR-006**: Duplicate registrations MUST be collapsed to exactly one instance; specifically the four `clawta-stale-worker-watchdog` registrations MUST become one, and the source MUST be corrected so duplicates do not reappear.
- **FR-007**: No job may remain `enabled` while in a persistent error state — a perpetually-failing job MUST be fixed or disabled (the erroring `board-watchdog`).
- **FR-008**: The career-tracking checkpoint jobs MUST be classified `keep`, marked out-of-scope for swarm orchestration, and confirmed one-shot / self-expiring so they need no migration.
- **FR-009**: At any moment a logical job's work MUST have exactly one active origin — its cron registration OR an orchestrator workflow, never both.
- **FR-010**: Every disposition MUST be recorded in a durable, reviewable decision log; decisions MUST NOT be applied silently.
- **FR-011**: Execution of dispositions MUST be staged and reversible — `disable` precedes `delete`, and a timestamped backup of a registry MUST be taken before it is mutated.
- **FR-012**: The audit MUST cross-reference spec 081 so that any logical job appearing in both the agent-cron and systemd-cron layers receives consistent, non-contradictory dispositions.
- **FR-013**: The audit MUST separate concerns within the Hermes registry — swarm-infrastructure jobs versus personal career-tracking jobs MUST be distinguishable in the decision log.

### Key Entities

- **Agent-runtime cron job**: one scheduled routine in an agent's cron registry — id, name, owning agent, schedule, prompt or script, enabled state, run history.
- **Cron registry**: the per-runtime `jobs.json` file holding an agent's job set (`~/.hermes/cron/jobs.json`, `~/.openclaw/cron/jobs.json`).
- **Disposition**: the audit's decision for one job — `keep`, `disable`, `delete`, or `migrate` — with its rationale.
- **Registration source**: where a job is created from (a manual command, an agent startup/role script); determines whether a deletion is durable.
- **Decision log**: the documented, per-job inventory and rationale — the audit's primary deliverable.
- **Replacement workflow**: the spec-070 / spec-076 orchestrator workflow that subsumes a `migrate`-disposition job.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of the 39 jobs carry a recorded disposition and rationale — zero jobs unclassified.
- **SC-002**: After the cull, the OpenClaw registry contains exactly one `clawta-stale-worker-watchdog` (down from four).
- **SC-003**: Zero agent-runtime crons remain in a perpetual-error state — every scheduled job either succeeds or is disabled.
- **SC-004**: For every executed deletion, the job does not reappear across a restart of its owning agent.
- **SC-005**: During migration, no logical job is observed running from both a cron and an orchestrator workflow at the same time.
- **SC-006**: Every `migrate` disposition names its replacement workflow, and no migration is executed before that workflow is proven.
- **SC-007**: The decision log and spec 081 assign consistent dispositions to every job that appears in both layers — zero contradictions.

## Assumptions

- The Hermes and OpenClaw `jobs.json` registries are the authoritative source of agent-runtime crons; the ~15 systemd cron/timer units are spec 081's scope.
- The spec-070 orchestrator and spec-076 spec-DAG scheduler are the migration *target*; this spec consumes them and does not build or modify them.
- The career-tracking checkpoint jobs belong to the operator's personal `career` board and are not swarm infrastructure.
- The operator approves each disposition before it is executed — the audit documents and stages; it does not auto-delete.
- One operator, one box; registries may be edited while their agents run, so every mutation must be restart-safe.
- The audit snapshot (job counts, states, defects) is taken 2026-05-21; counts are re-confirmed at execution time before any registry is mutated.

## Invariants

- **INV-001**: At the end of this spec every agent-runtime cron is in exactly one terminal state — kept, disabled, or deleted. No job is left unclassified, and there is no fourth state.
- **INV-002**: A still-needed logical job has exactly one active origin at any time — a cron or an orchestrator workflow, never both and never neither.
- **INV-003**: A deletion is durable — the registration source is corrected so the job cannot silently reappear.
- **INV-004**: The audit changes a job's *fate*, never a kept job's *behavior* — what a kept job does is unchanged by this spec.

## Out of Scope

- The ~15 systemd cron/timer units — spec 081 (`cron-migration-board-retirement`).
- Building, modifying, or proving orchestrator workflows — specs 070 and 076.
- The kanban board read-model retirement — spec 081 User Story 1.
- Changing what any `keep` job does — this spec decides each job's fate, not its behavior.
- Pruning the accumulated `jobs.json.bak-*` backup files — noted, not actioned.

## Dependencies

- **Spec 070** (Chitin Orchestrator) — defines the migration target for the board / dispatch crons.
- **Spec 076** (Spec-DAG Scheduler) — the P1 orchestrator slice that replaces the human-managed pull-loop; the named replacement for the pull-loop crons.
- **Spec 081** (Cron-to-Workflow Migration and Board Retirement) — the sibling systemd-cron audit; shares the "run beside, then retire" pattern and the no-double-run rule, and must agree on any job present in both layers.
