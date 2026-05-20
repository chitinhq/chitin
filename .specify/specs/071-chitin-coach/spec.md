# Feature Specification: Chitin Coach

**Feature Branch**: `071-chitin-coach`

**Created**: 2026-05-20

**Status**: Draft

**Input**: User description: "Adopt the analysis engine of microsoft/AI-Engineering-Coach (MIT) into Chitin as the operator-facing arm of Chitin Telemetry — the operator does not use VS Code, so lift the analysis core and re-host it on Chitin's own telemetry; one engine coaches both the operator's AI-coding and the swarm agents."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Context-health that prevents truncation (Priority: P1)

Every AI-coding session — the operator's own and each swarm agent's — carries
a running **context-health score**. When a session approaches saturation,
shows a compaction storm, or shows runaway context growth, Chitin Coach
flags it **before** the session truncates. The recurring "Response remained
truncated" failure (open as task #18) becomes a prediction, not a
post-mortem.

**Why this priority**: It fixes a live, recurring bug and proves the whole
adoption on the smallest slice — one analyzer wired to Chitin telemetry.

**Independent Test**: Replay historical Claude Code / agent sessions through
the context-health analyzer; confirm sessions that later truncated are
flagged as at-risk before their truncation point.

**Acceptance Scenarios**:

1. **Given** a session whose context utilization is climbing, **When** it crosses the saturation threshold, **Then** Chitin Coach surfaces an at-risk warning before the next request truncates.
2. **Given** a session with 4+ compaction events, **When** it is analyzed, **Then** it is flagged as a compaction storm.
3. **Given** a finished session, **When** the operator opens its report, **Then** a context-health score and the contributing factors are shown.

---

### User Story 2 - Anti-pattern detection from declarative rules (Priority: P2)

Chitin Coach flags anti-patterns in **both** the operator's AI-coding habits
and the swarm agents' behavior — runaway loops, session drift, mega-sessions,
lazy prompting, no-spec-driven-development, and so on. Rules are **data**: a
rule set the operator can read, edit, and extend without a code change.

**Why this priority**: The bulk of the Coach's value, and it serves the
operator-as-developer directly. Builds on the P1 engine.

**Independent Test**: Add a new anti-pattern rule as data (no code change);
run the analyzer; confirm the new rule fires on matching sessions.

**Acceptance Scenarios**:

1. **Given** the declarative rule set, **When** a new rule is added as data, **Then** it is enforced on the next analysis run with no code change or redeploy.
2. **Given** an operator session and an agent session, **When** both are analyzed, **Then** anti-patterns are reported for each, labelled by source.

---

### User Story 3 - The full insight surface (Priority: P3)

The operator opens one Chitin surface and sees the Coach's four faces:
**Observe** (practice score, session timeline, activity heatmap),
**Measure** (token/consumption analytics, work-pattern analysis),
**Improve** (anti-patterns, context-health), **Level Up** (skill discovery,
SDLC insights) — for their own coding and for the swarm.

**Why this priority**: The complete experience; depends on P1+P2 being proven.

**Independent Test**: Open the Chitin Coach surface; confirm all four
insight categories render from Chitin telemetry with no VS Code involved.

**Acceptance Scenarios**:

1. **Given** Chitin telemetry, **When** the operator opens the Coach surface, **Then** Observe / Measure / Improve / Level Up all render.
2. **Given** the surface, **When** the operator switches between "my coding" and "the swarm", **Then** the same insight categories render for each.

---

### Edge Cases

- A telemetry source is empty or unparseable — the Coach MUST degrade gracefully (skip, not crash) and report coverage.
- An agent session and an operator session have different log shapes — one engine MUST handle both via per-source parsers.
- A session is still in flight — context-health MUST be computable on a partial session (that is the point of P1).
- A declarative rule is malformed — it MUST be rejected with a clear error, not silently ignored.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Chitin Coach MUST ingest AI-coding session telemetry from Chitin's own sources (the chitin chain, Claude Code session logs, the Argus session index) — never from a VS Code installation.
- **FR-002**: One analysis engine MUST cover both the operator's own AI-coding sessions and the swarm agents' (Ares, Clawta) sessions.
- **FR-003**: The Coach MUST compute a per-session context-health score from token-context utilization, compaction events, and context-growth rate.
- **FR-004**: The Coach MUST detect context saturation, compaction storms, and runaway context growth, and warn **before** a session truncates.
- **FR-005**: Anti-pattern detection MUST be driven by declarative rules (rules-as-data interpreted by an engine), not hardcoded passes.
- **FR-006**: The rule set MUST be extensible — a rule can be added or edited without a code change or redeploy.
- **FR-007**: Insights MUST surface through a Chitin surface (the Chitin console or a `chitin` CLI command) — never a VS Code extension.
- **FR-008**: All analysis MUST run locally; no session data leaves the machine.
- **FR-009**: Every reported finding MUST be labelled by source (operator session vs a named agent session).
- **FR-010**: The Coach MUST produce the four insight categories — Observe, Measure, Improve, Level Up.
- **FR-011**: Code lifted from microsoft/AI-Engineering-Coach MUST retain its MIT licence attribution.

### Key Entities

- **Telemetry source**: An origin of session data — the chitin chain, Claude Code session logs, or the Argus session index. Each has a parser.
- **Session**: One AI-coding session (operator or agent), with requests, token counts, compaction events, timestamps.
- **Context-health score**: A per-session measure of context-window pressure — utilization, compaction storms, growth rate — with an at-risk verdict.
- **Anti-pattern rule**: A declarative, data-defined detection rule — id, description, severity, detection logic — runnable by the rule engine.
- **Finding**: One detected anti-pattern or health issue on one session, labelled by source.
- **Insight surface**: The Chitin-side presentation of Observe / Measure / Improve / Level Up.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A session heading toward truncation is flagged as at-risk **before** it truncates — the task-#18 failure mode is predicted, not discovered after the fact.
- **SC-002**: 100% of the operator's AI-coding sessions and the swarm agents' sessions are analyzed by one engine.
- **SC-003**: A new anti-pattern rule can be added as data and takes effect on the next run with zero code changes.
- **SC-004**: Zero session data leaves the machine.
- **SC-005**: The operator can see their practice score and top anti-patterns in one Chitin surface, without VS Code.
- **SC-006**: Recurring context-truncation incidents (task #18) drop to near zero after the P1 slice ships.

## Assumptions

- The microsoft/AI-Engineering-Coach analysis core (`src/core/` — analyzers, rule engine + DSL, the Claude/Codex session parsers) is MIT-licensed and liftable; the VS Code shell (`src/webview/`) is dropped.
- Chitin already produces the telemetry the Coach needs (the chitin chain, Claude Code session logs, the Argus session index); the Coach consumes it — it does not add new collection.
- The operator works in Claude Code, not VS Code; VS Code is never a runtime dependency.
- The Coach already ships `parser-claude.ts` / `parser-codex.ts` — a working basis for Chitin-side session parsing.
- The implementation language and the exact surface (Chitin console vs CLI) are resolved in `plan.md`; this spec stays capability-level.
- Chitin Coach is the operator-facing arm of Chitin Telemetry; the swarm-watching arm (Argus/Sentinel) shares its engine.

## Out of Scope

- The microsoft/AI-Engineering-Coach VS Code extension shell — not adopted.
- Changing how Chitin collects telemetry — the Coach consumes existing telemetry.
- The Chitin Orchestrator (spec 070).
- Contributing changes back upstream to microsoft/AI-Engineering-Coach.
