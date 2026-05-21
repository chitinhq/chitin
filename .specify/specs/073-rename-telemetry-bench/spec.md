# Feature Specification: Collapse to Chitin Telemetry + Chitin Bench

**Feature Branch**: `073-rename-telemetry-bench`

**Created**: 2026-05-20

**Status**: Draft

**Input**: User description: "No abstract code names in Chitin — name things for what they do. Collapse Sentinel and Argus into one 'Chitin Telemetry' subsystem; rename Icarus to 'Chitin Bench'."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One observability subsystem, plainly named (Priority: P1)

Today observability is split across two code-named subsystems — **Argus**
(`python/argus`: session indexer, anomaly detectors, digests) and
**Sentinel** (detection passes + analysis in `python/analysis`, plus the
`/sentinel` skill). A reader has to learn two myth names to understand one
job. They collapse into **one subsystem, "Chitin Telemetry"** — one package,
one skill — named for what it does.

**Why this priority**: It is the bigger of the two renames and the one that
removes a genuine *split* (two things doing one job), not just a name.

**Independent Test**: After the collapse, a reader finds all observability
under one Chitin Telemetry surface; no `argus`/`sentinel` subsystem name
remains in active code, config, or skills.

**Acceptance Scenarios**:

1. **Given** the collapse is done, **When** the observability code is inventoried, **Then** it lives under one Chitin Telemetry package, not two.
2. **Given** the `/sentinel` skill, **When** it is invoked, **Then** it runs as the Chitin Telemetry skill — same capability, descriptive name.
3. **Given** a grep for `argus` / `sentinel` as subsystem names, **When** run over active code/config/skills, **Then** it returns nothing (only historical spec files).

---

### User Story 2 - The bench, called the bench (Priority: P2)

**Icarus** — the terminal-bench harness (`swarm/icarus_harness/`), the
runner/loop/emitter/watcher scripts, the `icarus-bench` systemd service,
the `jobs/icarus/` output, and the `icarus` kanban board — is renamed to
**Chitin Bench**. The rename is *safe*: the bench keeps running and no
operational state is lost.

**Why this priority**: A pure rename, but it touches a **running service and
a live board** — it must not drop in-flight runs or open tickets.

**Independent Test**: Rename end to end; confirm the bench service still
runs continuously across the cutover and every open board ticket survives.

**Acceptance Scenarios**:

1. **Given** the bench is running, **When** it is renamed to Chitin Bench, **Then** bench runs continue with no lost in-flight run.
2. **Given** the `icarus` board has open failure tickets, **When** the board is renamed, **Then** every ticket is preserved under the new name.
3. **Given** a grep for `icarus`, **When** run over active code/config, **Then** it returns nothing (only historical spec files).

---

### User Story 3 - No abstract code names left (Priority: P3)

Every reference to the old names — across specs, `INDEX.md`, the `/sentinel`
and `/evolve` skills, docs, and code — is updated. The naming principle holds
repo-wide: subsystems are named for what they do.

**Why this priority**: The cleanup tail — done after the two renames land.

**Independent Test**: Repo-wide, the only matches for the retired subsystem
names are historical/superseded spec files.

**Acceptance Scenarios**:

1. **Given** the renames are complete, **When** docs and `INDEX.md` are read, **Then** they reference Chitin Telemetry and Chitin Bench.

### Edge Cases

- The bench systemd service is mid-run during the rename — the new-named service MUST be able to run beside the old until cutover; no in-flight run is dropped.
- The bench kanban board has open tickets — renaming the board MUST preserve every ticket, comment, and status.
- The `/sentinel` and `/evolve` skills are invoked during the transition — they MUST keep working throughout.
- A worker or cron still references an old name post-rename — MUST be caught (a grep gate) before the spec is considered done.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Argus and Sentinel MUST be collapsed into ONE observability subsystem named **Chitin Telemetry** — one package and one skill, not two code-named halves.
- **FR-002**: Icarus MUST be renamed to **Chitin Bench** across the harness, the runner/loop/emitter/watcher scripts, the systemd service, the `jobs/` output directory, and the kanban board.
- **FR-003**: The rename MUST preserve operational state — the running bench service's in-flight runs and the bench board's tickets, comments, and statuses survive the cutover.
- **FR-004**: A renamed service or loop MAY run beside the old name until the cutover is proven — no big-bang switch.
- **FR-005**: Every reference — specs, `INDEX.md`, the `/sentinel` and `/evolve` skills, docs, code imports, cron/service definitions — MUST be updated; no dangling subsystem reference to a retired name.
- **FR-006**: Agent identities (Ares, Clawta) MUST NOT be renamed — they are agent personas, not subsystem code names.
- **FR-007**: After the rename, a repo-wide search for the retired subsystem names (`argus`, `sentinel`, `icarus`) MUST return only historical/superseded spec files.

### Key Entities

- **Chitin Telemetry**: The single observability subsystem — session indexing, anomaly detection, digests, detection passes — collapsed from Argus + Sentinel.
- **Chitin Bench**: The terminal-bench evaluation subsystem — harness, runner, loop, service, board — renamed from Icarus.
- **Rename map**: The old-name → new-name table covering directories, modules, scripts, the systemd unit, the board, and skills — the auditable record of the cutover.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Observability lives under one Chitin Telemetry subsystem — zero `argus`/`sentinel` subsystem names in active code, config, or skills.
- **SC-002**: Zero `icarus` references in active code or config; the bench is Chitin Bench.
- **SC-003**: The bench service runs continuously across the rename — zero in-flight runs lost.
- **SC-004**: 100% of the bench board's tickets are preserved under the new board name.
- **SC-005**: The `/sentinel` and `/evolve` skills keep working throughout the transition.

## Assumptions

- **New names** (operator-delegated, per the 2026-05-20 audit): the collapsed
  observability subsystem is **Chitin Telemetry**; the renamed bench is
  **Chitin Bench**.
- Argus = `python/argus`; Sentinel = the detection/analysis parts of
  `python/analysis` + the `/sentinel` skill; Icarus = `swarm/icarus_harness/`,
  `swarm/bin/icarus-bench-*`, `swarm/bin/icarus-watcher`, the
  `icarus-bench` systemd service, `jobs/icarus/`, and the `icarus` board.
- Historical/superseded spec files keep their original names (history is not
  rewritten); only active surfaces are renamed.
- Whether a kanban board can be renamed in place or is recreated-and-migrated
  is an implementation choice resolved in `plan.md`.

## Out of Scope

- Renaming the agents Ares and Clawta.
- The Chitin Orchestrator (spec 070).
- Re-architecting Telemetry or the bench — this is a rename + collapse, not a
  redesign.
