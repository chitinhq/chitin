# Feature Specification: Driver Governance & Telemetry Integrity

**Feature Branch**: `083-driver-governance-telemetry`

**Created**: 2026-05-21

**Status**: Draft

**Input**: User description: "Driver governance and telemetry integrity. Every
agent driver the chitin system dispatches — claude, codex, copilot, gemini,
hermes, openclaw — must be governed by the chitin kernel and emit verified,
inspectable governance telemetry into a queryable sink."

## Overview

Chitin governs AI coding agents at runtime: every tool call an agent makes is
evaluated by the chitin kernel, which records a **governance decision**. That
decision stream is the system's only evidence that an agent is actually being
governed — and the substrate the watchdogs, audits, and the orchestrator's
self-improvement loop all read.

A telemetry audit on 2026-05-21
(`docs/2026-05-21-orchestrator-driver-telemetry-audit.md`) found that this
evidence is unreliable. Of the six drivers the system dispatches, only three
were proven to emit governance telemetry on a real invocation; the other three
were ungoverned, regressed, or unverifiable — and the tooling that is supposed
to report this (`chitin doctor`) reported all of them healthy.

This feature makes driver governance **provable**: every driver the system
dispatches either emits verified governance telemetry into one queryable sink,
or is visibly flagged as unverified — never silently trusted.

### Audit baseline (2026-05-21)

| Driver | Governed & proven? | Finding |
|---|---|---|
| claude | ✅ | Emits to the central governance log. |
| codex | ⚠️ partial | Emits only **post-hoc**, into per-session files, not the central log; central telemetry regressed after May 18. |
| openclaw | ✅ | Emits to its own per-runtime events file. |
| gemini | ❌ unverifiable | The Gemini CLI is unauthenticated and cannot run. |
| copilot | ❌ broken | The hook never fires; the kernel shim aborts on a CLI/SDK version mismatch. |
| hermes / clawta | ❌ regressed | Governance telemetry stopped; a merged fix never reached the running kernel. |

### Root cause of the regression

The kernel-redeploy pipeline is broken: a merged fix that restores
hermes/clawta telemetry was committed but never built into the running kernel,
because the redeploy job fails silently. Merged kernel fixes strand
indefinitely, and nothing surfaces the staleness. This delivery gap is itself
in scope — restoring telemetry without fixing delivery only resets the clock.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Restore the regressed governance telemetry (Priority: P1)

The operator needs the Hermes and Clawta agents — the live swarm — to resume
emitting governance decisions. Today they run ungoverned-in-evidence: their
tool calls produce no inspectable governance record, so there is no proof the
kernel is gating them and the watchdogs have nothing to read.

**Why this priority**: This is a live regression on the most active agents in
the system. Until it is fixed, the swarm operates without governance evidence —
the highest-severity gap and the smallest, most urgent slice.

**Independent Test**: Trigger a Hermes/Clawta unit of work; confirm a
governance decision attributed to that agent appears in the governance log
within the same run.

**Acceptance Scenarios**:

1. **Given** a Hermes-driven unit of work, **When** it makes a tool call,
   **Then** a governance decision attributed to `hermes` is recorded.
2. **Given** a Clawta-driven unit of work, **When** it makes a tool call,
   **Then** a governance decision attributed to `clawta` is recorded.
3. **Given** the regression's root-cause fix has merged, **When** the running
   kernel is inspected, **Then** it reflects that fix (not an older build).

---

### User Story 2 - Kernel-fix delivery cannot strand (Priority: P2)

The operator needs a merged kernel or policy fix to reliably reach the running
kernel. Today the redeploy job fails silently, so fixes sit in the main branch
for hours-to-days while the swarm runs a stale binary — exactly how US1's
regression was created.

**Why this priority**: This is the meta-cause. Without it, US1 (and every
future kernel fix) re-strands; with it, the system self-heals. It gates the
durability of every other story in this spec.

**Independent Test**: Merge a trivial kernel change; confirm the running kernel
reflects it within the redeploy cadence, with no manual intervention.

**Acceptance Scenarios**:

1. **Given** a kernel-relevant change merged to the main branch, **When** the
   redeploy cadence elapses, **Then** the running kernel binary reflects it.
2. **Given** the redeploy job encounters an error, **When** it fails, **Then**
   the failure is surfaced to the operator — not silently swallowed.
3. **Given** the running kernel is older than the merged source, **When** the
   operator checks system health, **Then** the staleness is reported.

---

### User Story 3 - Every dispatched driver is provably governed (Priority: P3)

The operator needs every driver the system can dispatch — claude, codex,
copilot, gemini, hermes, openclaw — to be governed and to *prove* it: a real
invocation must produce an inspectable governance decision. Today copilot is
ungoverned (its hook never fires and its kernel shim is broken), codex records
only after the fact, and dispatched work runs in worktrees that never inherit
the governance hooks.

**Why this priority**: It extends the guarantee from "the regressed drivers"
(US1) to the full driver set, and closes the orchestrator's
dispatch-into-ungoverned-worktree gap. High value, but it builds on the
delivery reliability US2 provides.

**Independent Test**: For each driver, run a one-shot probe that makes a single
tool call; confirm a governance decision attributed to that driver is recorded.

**Acceptance Scenarios**:

1. **Given** the copilot driver, **When** it runs a unit of work, **Then** a
   governance decision attributed to `copilot` is recorded.
2. **Given** the codex driver, **When** it runs a unit of work, **Then** its
   governance decisions reach the central queryable sink, not only a
   per-session file.
3. **Given** the orchestrator dispatches a work unit into a fresh worktree,
   **When** the agent makes a tool call, **Then** it is governed — the worktree
   is not a governance blind spot.
4. **Given** a driver whose CLI cannot run (e.g. unauthenticated), **When**
   governance is checked, **Then** the driver is reported as *unverified*, never
   as governed.

---

### User Story 4 - Trustworthy, unified observability (Priority: P4)

The operator needs one place to see whether governance is working and one
verdict they can trust. Today governance telemetry is split across three
unrelated sinks with no unified view, and `chitin doctor` reports drivers
healthy that emit zero real telemetry.

**Why this priority**: It makes the prior stories *durable and visible* — a
regression like US1's would have been caught immediately with a trustworthy
doctor and a unified view. Important, but the restoration itself (US1–US3)
delivers value first.

**Independent Test**: Query one interface and see governance decisions for
every driver; run `chitin doctor` and confirm its verdict matches a real probe
for every driver.

**Acceptance Scenarios**:

1. **Given** governance decisions from any driver, **When** the operator
   queries the unified telemetry interface, **Then** all drivers' decisions are
   visible from that one place.
2. **Given** a driver whose hook is installed but whose live CLI ignores it,
   **When** `chitin doctor` runs, **Then** it reports that driver as **failing**
   — it does not pass on a file check alone.
3. **Given** a driver governed by a global (non-project) hook, **When**
   `chitin doctor` runs, **Then** it reports that driver as **passing**.

---

### Edge Cases

- A driver's hook config is installed but the live CLI silently ignores it →
  must be reported as ungoverned, not healthy (the doctor false-positive).
- A driver's CLI cannot start (unauthenticated, missing binary) → reported as
  *unverified*, distinct from both *governed* and *ungoverned*.
- The running kernel is older than the merged kernel source → flagged as stale.
- The redeploy job fails (build error, version-control conflict) → surfaced,
  with the prior working kernel left in place.
- A dispatched work unit runs in a fresh worktree with no governance policy or
  hook present → the work unit is governed regardless, or refused.
- A driver emits governance decisions to a per-session or per-runtime file but
  never to the central sink → treated as a telemetry gap, not as "governed".
- A governance decision is recorded with no attributed driver/agent → counts
  as an attribution failure.
- An agent attempts to govern, reset, or redeploy its own governance
  infrastructure → denied; only an operator/supervisor may.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Every driver the system can dispatch (claude, codex, copilot,
  gemini, hermes, openclaw) MUST be governed by the chitin kernel — each of its
  tool calls evaluated and recorded as a governance decision.
- **FR-002**: Each driver's governance MUST be **provable**: a real one-shot
  invocation produces at least one inspectable governance decision attributed
  to that driver.
- **FR-003**: Every governance decision MUST carry an attributed driver or
  agent identity; an unattributed decision is a defect.
- **FR-004**: Governance decisions from all drivers MUST be queryable through a
  single interface — telemetry MUST NOT be permanently fragmented across
  per-driver sinks with no unified view.
- **FR-005**: The codex driver's governance decisions MUST reach the central
  queryable sink, not only per-session files.
- **FR-006**: The copilot driver MUST have a working governance path that
  produces governance decisions on a real run.
- **FR-007**: Hermes and Clawta governance telemetry MUST be restored — both
  emit attributed governance decisions on a real unit of work.
- **FR-008**: Agent work the orchestrator dispatches into a fresh worktree MUST
  be governed; a worktree MUST NOT be a governance blind spot.
- **FR-009**: A merged kernel- or policy-relevant change MUST reach the running
  kernel within a bounded, declared cadence without manual intervention.
- **FR-010**: A failure of the kernel-redeploy process MUST be surfaced to the
  operator, not silently swallowed; a failed redeploy MUST leave the prior
  working kernel in place.
- **FR-011**: The system MUST detect and report when the running kernel is
  stale relative to the merged kernel source.
- **FR-012**: `chitin doctor` (the governance-readiness check) MUST validate by
  observing a real governed invocation produce a governance decision — it MUST
  NOT report a driver healthy on a configuration-file check alone.
- **FR-013**: `chitin doctor` MUST credit a driver governed by a global
  (non-project-scoped) hook as passing.
- **FR-014**: A driver whose CLI cannot run (unauthenticated, unavailable) MUST
  be reported as **unverified** — a state distinct from *governed* and
  *ungoverned* — and MUST NOT be reported as governed.
- **FR-015**: An agent MUST NOT be able to govern, reset, or redeploy its own
  governance infrastructure; such actions require operator/supervisor authority.

### Key Entities

- **Driver**: an agent runtime the system dispatches work to (claude, codex,
  copilot, gemini, hermes, openclaw). Each has a governance path (a hook or a
  kernel-mediated shim) and a governance status.
- **Governance Decision**: the record the kernel emits when it evaluates a tool
  call — the attributed driver/agent, the action, the verdict, and the reason.
  The unit of governance telemetry.
- **Telemetry Sink**: where governance decisions are written. Today three exist
  (central log, per-session files, per-runtime files); the target is one
  queryable interface over them.
- **Governance Hook**: the per-driver mechanism that routes a tool call through
  the kernel. Installed globally and/or per-project.
- **Kernel Build**: the running kernel binary, versioned against the kernel
  source; may be current or stale.
- **Driver Governance Status**: a driver's state — *governed* (proven),
  *ungoverned* (proven not), or *unverified* (cannot be checked).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of runnable drivers produce an inspectable governance
  decision attributed to that driver on a one-shot probe.
- **SC-002**: Hermes and Clawta both emit attributed governance decisions on a
  real unit of work — confirmed after the fix lands.
- **SC-003**: 100% of governance decisions carry an attributed driver/agent;
  zero unattributed decisions over a representative window.
- **SC-004**: Every driver's governance telemetry is visible from one query
  interface — no driver requires a separate sink-specific lookup.
- **SC-005**: `chitin doctor`'s per-driver verdict matches a real probe for
  100% of drivers — zero false positives and zero false negatives.
- **SC-006**: A merged kernel-relevant change reaches the running kernel within
  the declared redeploy cadence, with no manual step.
- **SC-007**: Kernel staleness and redeploy failures are surfaced to the
  operator within one redeploy cadence of occurring.
- **SC-008**: Zero orchestrator-dispatched work units run ungoverned.

## Assumptions

- The six drivers named are the current dispatchable set; `local`/Icarus and
  any future drivers inherit the same requirements but are not enumerated here.
- Hermes remains an active driver; it is in scope, not retired.
- Authenticating a driver's CLI (e.g. providing Gemini credentials) is an
  operator precondition, not work this feature performs; the feature's
  obligation for such a driver is correct *unverified* reporting (FR-014) until
  the precondition is met.
- The chitin kernel, its hooks, and the `gov-decisions` record already exist;
  this feature restores, unifies, and proves them — it does not introduce
  governance.
- The systemd cron/timer layer (spec 081) and the agent-runtime cron registries
  (spec 082) are explicitly out of scope.
- The kernel-redeploy mechanism exists (`install-kernel.sh` and its timer); this
  feature makes it reliable, it does not replace it.
- "Bounded redeploy cadence" is assumed to be the existing ~15-minute timer
  interval unless the operator sets otherwise.
