# Feature Specification: Operator Report Delivery

**Feature Branch**: `085-operator-report-delivery`

**Created**: 2026-05-22

**Status**: Draft

**Input**: User description: "Get a heartbeat-type check or a full telemetry
report delivered to my Discord by Clawta or the Hermes Ares agent. They run all
the time in the gateway — keep them; this is a useful repeating, org-facing
task. I want a telemetry report of everything — orchestration, the kernel
layers, what drivers have been working on, what PRs each driver has done. The
detail can live in the console; what I want is a wrap-up sent to me in Discord
that I can click to view what I need. Research reports and Obsidian notes
should be deliverable the same way."

## Overview

The chitin swarm produces a continuous stream of operational signal — kernel
health, orchestration activity, per-driver work, shipped PRs, research output,
Obsidian notes — but today the operator has to go and look for it. The
continuously-running gateway agents (Clawta and the Hermes Ares agent) are well
placed to close that gap: give them a standing, org-facing job of pushing
operator-facing reports to Discord.

This feature delivers two reports to a designated Discord destination: a
frequent lightweight **heartbeat** (is the system alive?) and a daily
**telemetry digest** (what did the swarm do?). The digest is a skimmable
wrap-up with click-through links into the chitin-console — the operator reads
the summary in Discord and drills in only where they need depth. Research
reports and Obsidian notes ride the same delivery channel.

This feature is the **delivery layer**. It consumes telemetry that already
exists and pushes operator-facing digests. It does not generate new telemetry
and does not build the unified query interface (spec 083 US4).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Heartbeat liveness signal (Priority: P1) 🎯 MVP

The operator needs to know — without logging in or opening a dashboard — that
the swarm is alive: the gateway is up, the chitin kernel is current and
healthy, and the agents are running. Today liveness is invisible until
something is visibly broken.

**Why this priority**: It is the smallest shippable slice and the
highest-frequency need — a liveness signal is valuable on its own and is the
fastest thing to deliver. It also proves the Discord delivery channel
end-to-end before the heavier digest is built on top of it.

**Independent Test**: Wait one heartbeat interval; confirm a liveness message
arrives in the operator's Discord destination stating gateway / kernel / agent
status plus the last kernel-redeploy outcome.

**Acceptance Scenarios**:

1. **Given** the gateway, kernel, and agents are all healthy, **When** a
   heartbeat interval elapses, **Then** a Discord message reports all three
   healthy plus the last redeploy outcome.
2. **Given** the running kernel is stale or the last redeploy failed, **When**
   the heartbeat runs, **Then** the message flags the specific degraded
   component rather than a generic "ok".
3. **Given** a component the heartbeat checks is unreachable, **When** the
   heartbeat runs, **Then** the message reports that component as
   unknown/unreachable — never healthy on the absence of a signal.

---

### User Story 2 - Daily telemetry digest (Priority: P2)

The operator needs a once-a-day wrap-up of what the swarm actually did:
orchestration activity, kernel-layer health, what each driver worked on, and
the PRs each driver shipped — delivered as a concise Discord message they can
skim, with links into the chitin-console to drill into any item.

**Why this priority**: This is the substantive value, but it is larger than the
heartbeat and depends on the delivery channel US1 proves out. It builds on US1.

**Independent Test**: Trigger the digest (scheduled or on-demand); confirm a
Discord message arrives summarizing orchestration, kernel layers, per-driver
activity, and shipped PRs, each summary line linking to the corresponding
chitin-console view.

**Acceptance Scenarios**:

1. **Given** a day of swarm activity, **When** the daily digest runs, **Then**
   the operator receives one Discord message summarizing orchestration status,
   kernel-layer health, per-driver activity, and the PRs each driver shipped.
2. **Given** the operator wants a report off-schedule, **When** they issue the
   on-demand request, **Then** a current digest is delivered within a short,
   bounded time.
3. **Given** any digest summary line with supporting detail, **When** the
   operator clicks its link, **Then** they land on the corresponding detail
   view in the chitin-console.
4. **Given** a telemetry source is unavailable when the digest is built,
   **When** the digest is delivered, **Then** it reports what it could gather
   and explicitly marks the missing section — it is still sent.

---

### User Story 3 - Research reports and Obsidian notes delivered to the operator (Priority: P3)

The operator needs the swarm's research reports and Obsidian notes to reach
them through the same Discord channel, so standing knowledge output is
*received*, not just produced.

**Why this priority**: It extends the proven delivery channel to additional
content. Valuable, but independent of US1/US2 and lower urgency than the
operational reports.

**Independent Test**: When the swarm produces a research report or an Obsidian
note, confirm a Discord message announcing it — with a link to view it —
arrives in the operator's destination.

**Acceptance Scenarios**:

1. **Given** the swarm produces a new research report, **When** it is
   finalized, **Then** the operator receives a Discord message announcing it
   with a click-through link to read it.
2. **Given** a new or updated Obsidian note is published, **When** publication
   completes, **Then** the operator receives a delivery message through the
   same channel.

---

### Edge Cases

- The Discord destination is unreachable (API error, rate limit) when a report
  is due → the report is retried/queued and the delivery failure is itself
  surfaced; a missed report is never silent.
- The delivering agent (Clawta or Ares) is down when a scheduled report is due
  → the miss is detected and the report is delivered or flagged on recovery; a
  down agent does not silently skip a day.
- The on-demand digest is requested repeatedly in quick succession → requests
  are coalesced/rate-limited so the operator is not flooded.
- A digest would be very large (busy day) → the Discord message stays
  skimmable; depth is offloaded to console links, never silently truncated.
- The chitin-console is unreachable when the digest is built → links are still
  included (they resolve when the console returns); the digest does not block
  on console availability.
- The heartbeat and the digest both report on the kernel → they read the same
  `chitin health` signal, so they never disagree.
- An agent attempts to deliver a report to a destination other than the
  operator's configured one → denied; delivery targets are operator-configured.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST deliver a periodic heartbeat message to the
  operator's designated Discord destination reporting the liveness of the
  gateway, the chitin kernel, and the swarm agents.
- **FR-002**: The heartbeat MUST include the kernel's current-vs-stale status
  and the outcome of the most recent kernel redeploy.
- **FR-003**: The heartbeat MUST distinguish healthy, degraded, and
  unknown/unreachable per component — it MUST NOT report a component healthy on
  the absence of a signal.
- **FR-004**: The system MUST deliver a telemetry digest to the operator's
  Discord destination on a daily schedule.
- **FR-005**: The operator MUST be able to request a telemetry digest on demand
  and receive it within a bounded time.
- **FR-006**: The digest MUST summarize, at minimum: orchestration status,
  kernel-layer health, per-driver activity (what each driver worked on), and
  the PRs each driver shipped.
- **FR-007**: The digest MUST be a concise, skimmable message; full detail MUST
  live in the chitin-console, reachable from the message via click-through
  links.
- **FR-008**: Every digest summary line that has supporting detail MUST link to
  the corresponding chitin-console view.
- **FR-009**: When a telemetry source is unavailable, a report MUST still be
  delivered, explicitly marking the missing section rather than failing to send
  or silently omitting it.
- **FR-010**: A failure to deliver a report (destination unreachable,
  delivering agent down) MUST be detected and surfaced — a missed report MUST
  NOT be silent.
- **FR-011**: The system MUST deliver research reports and Obsidian notes to
  the operator through the same Discord delivery channel.
- **FR-012**: Report delivery MUST be performed by a continuously-running
  gateway agent (Clawta or the Hermes Ares agent); the gateway agents are
  retained for this standing role.
- **FR-013**: The delivery destination and schedule MUST be operator-configured;
  an agent MUST NOT redirect reports to any destination other than the
  operator's configured one.
- **FR-014**: On-demand digest requests issued in rapid succession MUST be
  coalesced or rate-limited so the operator is not flooded.

### Key Entities *(include if feature involves data)*

- **Heartbeat**: a periodic liveness message — per-component status (gateway,
  kernel, agents) plus the last-redeploy outcome, captured at a point in time.
- **Telemetry Digest**: a daily (or on-demand) operator-facing summary —
  orchestration, kernel layers, per-driver activity, shipped PRs — composed of
  summary lines, each optionally linked to a console detail view.
- **Delivery Destination**: the operator-configured Discord target a report is
  sent to.
- **Report Schedule**: the cadence configuration — heartbeat interval, digest
  daily time — plus the on-demand trigger.
- **Delivering Agent**: the continuously-running gateway agent (Clawta or Ares)
  that composes and posts reports.
- **Console Detail View**: the chitin-console page a digest line links to for
  full detail.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The operator receives a heartbeat in Discord every interval with
  no manual action, for 7 consecutive days, with zero silent misses.
- **SC-002**: The operator receives the daily digest once per day on schedule;
  an on-demand request is fulfilled within 2 minutes.
- **SC-003**: 100% of digest summary lines that have supporting detail link to
  a working chitin-console view.
- **SC-004**: A heartbeat or digest correctly reflects a real degraded state
  (stale kernel, failed redeploy, an idle driver) when one is induced — zero
  false "all healthy" verdicts.
- **SC-005**: When a telemetry source or the Discord destination is
  unavailable, the failure is surfaced to the operator within one report cycle
  — zero silent misses.
- **SC-006**: The operator can determine the swarm's daily activity (drivers,
  PRs, orchestration, kernel) from the digest message alone, opening the
  console only for detail — verified by the digest containing all four required
  summary categories.

## Assumptions

- The delivery destination is a single designated operator Discord target (a
  channel or DM), configured once. "Operator's Discord inbox" is read as one
  configured destination.
- The heartbeat interval defaults to hourly; the digest runs once daily at an
  operator-set time. Both are operator-tunable configuration, not values fixed
  by the feature.
- The on-demand digest is requested by the operator through Discord (a
  command or message to the delivering agent).
- The specific delivering agent — Clawta or the Hermes Ares agent — is selected
  during planning, based on which already has Discord message-posting and the
  right gateway hooks. Both are viable; this is a planning decision, not a spec
  ambiguity.
- The chitin-console hosts (or will host) detail views for orchestration,
  kernel, driver, and PR telemetry; this feature links to them and does not
  build the console.
- This feature is the delivery layer only. It consumes existing telemetry
  sinks and, where available, the unified query interface from spec 083 US4; it
  does not re-implement telemetry collection. If 083 US4 has not yet shipped,
  the digest reads the underlying sinks directly.
- Heartbeat liveness for the kernel reuses the `chitin health` signals (kernel
  staleness + redeploy health) delivered in spec 083 US2.
- The gateway agents (Clawta, Ares) remain running; this feature gives them a
  standing role and does not change their other responsibilities.
- Research reports and Obsidian notes are already produced by existing swarm
  processes; this feature delivers notification and links for them — it does
  not change how they are generated.

## Dependencies

- **spec 083 US2** — `chitin health` kernel-staleness and redeploy-health
  signals (heartbeat input).
- **spec 083 US4** — the unified telemetry query interface (digest input;
  optional — the digest falls back to reading the sinks directly if US4 has not
  shipped).
- **chitin-console** — the detail views the digest links into.
- **Discord gateway integration** — the delivering agent's existing Discord
  message-posting capability.
