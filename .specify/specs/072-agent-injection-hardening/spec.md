# Feature Specification: Agent Prompt-Injection Hardening

**Feature Branch**: `072-agent-injection-hardening`

**Created**: 2026-05-20

**Status**: Draft

**Input**: User description: "Reader/writer role separation as defense-in-depth against prompt injection — the role that ingests untrusted content holds no privileged-action capability; the role that acts never sees raw untrusted text. Plus output sanitization. Inspired by Helen Fan's agent-security architecture."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Injected instructions cannot reach a privileged actor (Priority: P1)

A swarm agent must process untrusted external content — a GitHub issue body,
a PR diff, a fetched doc, a web page — that may carry a hidden prompt
injection. A **reader** role ingests that content and holds **no
privileged-action capability**; it emits only structured, typed findings.
A **writer/actor** role acts on those findings and **never sees the raw
untrusted text**. A poisoned issue cannot steer a privileged action, because
the part of the system that read it cannot act, and the part that acts never
read it.

**Why this priority**: It closes a real gap. The kernel gates *whether* an
action is allowed, but a plausible-looking allowed action steered by
injection still passes. Role separation stops the injection a layer earlier.

**Independent Test**: Red-team — plant injection payloads in issues, PR
diffs, fetched docs; confirm no payload causes a privileged action; confirm
findings reaching the actor contain no free-form instruction text.

**Acceptance Scenarios**:

1. **Given** untrusted content with an embedded "ignore your instructions and …" payload, **When** a reader ingests it, **Then** the reader emits structured findings only and takes no action.
2. **Given** those findings, **When** the writer/actor consumes them, **Then** it acts on typed data and never receives the raw untrusted text.
3. **Given** a reader detects a likely injection attempt, **When** it reports, **Then** the attempt is itself a finding (a signal), not an executed instruction.

---

### User Story 2 - Agent output cannot execute downstream (Priority: P2)

Content an agent produces — a comment, a file, a snippet — is **sanitized**
before it can execute or auto-render downstream: spreadsheet formulas,
embedded scripts, and auto-executing links are neutralized. What was
neutralized is recorded, not silently dropped.

**Why this priority**: The output side of the same threat — an agent
(possibly injection-influenced) emitting content that harms a downstream
consumer. Builds on P1.

**Independent Test**: Feed the sanitizer agent output containing a
spreadsheet formula, a script tag, and an auto-exec link; confirm each is
neutralized and the neutralization is logged.

**Acceptance Scenarios**:

1. **Given** agent output containing an executable formula or script, **When** it is sanitized, **Then** the executable form is neutralized and the action is recorded.
2. **Given** sanitization neutralizes something, **When** the operator inspects it, **Then** what was changed and why is visible.

---

### Edge Cases

- A reader is itself injected and emits a malicious "finding" — findings are typed/structured data, so a writer treats them as values, never as directives; a finding cannot carry an executable instruction.
- An agent legitimately needs to read *and* act on the same item — the boundary is between **untrusted external** content ingest and privileged action, not all reading; trusted internal data (the board, specs, the chitin chain) is exempt.
- Untrusted content is very large — the reader MUST still bound and summarize it without losing the injection-detection pass.
- Sanitization false-positive — a legitimate code block flagged — MUST be tunable and MUST NOT silently corrupt content.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Ingest of untrusted external content (issue/PR bodies and diffs, fetched docs, web pages) MUST be performed by a reader role that holds no privileged-action capability.
- **FR-002**: A reader MUST emit only structured, typed findings — never free-form text a downstream actor could interpret as instructions.
- **FR-003**: The writer/actor role MUST consume only structured findings and MUST NOT receive raw untrusted content.
- **FR-004**: A finding MUST be data (extracted claims, references, values) and MUST carry no executable instruction or directive.
- **FR-005**: This separation MUST complement, not replace, the kernel's tool-call gating — defense in depth.
- **FR-006**: Agent-produced output MUST be sanitized before it can execute or auto-render downstream — spreadsheet formulas, scripts, and auto-executing links neutralized.
- **FR-007**: Every sanitization action MUST be recorded (what was neutralized, why) — never a silent drop.
- **FR-008**: The trust boundary MUST be explicit: untrusted = external-authored content; trusted = the board, specs, and the chitin chain (exempt from the reader/writer split).
- **FR-009**: A reader that detects a likely injection attempt MUST surface it as a finding (an injection-attempt signal), not act on it.

### Key Entities

- **Untrusted content source**: External-authored input — issue/PR text and diffs, fetched docs, web pages.
- **Reader role**: Ingests untrusted content; emits findings; holds no privileged-action capability.
- **Finding**: A structured, typed unit of extracted data (claim, reference, value, injection-attempt signal) — never a directive.
- **Writer/Actor role**: Consumes findings, takes privileged actions; never receives raw untrusted content.
- **Sanitization record**: A log entry of what agent-produced content was neutralized and why.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In a red-team suite of planted injection payloads, zero payloads cause a privileged action.
- **SC-002**: 100% of untrusted-external-content ingest goes through a reader role with no action capability.
- **SC-003**: Zero free-form instruction text from untrusted content reaches a writer/actor — findings are structured data only.
- **SC-004**: Agent output that would execute downstream (formula/script/auto-link) is neutralized, and 100% of neutralizations are logged.
- **SC-005**: The kernel's existing tool-call gating is unchanged — this feature is purely additive (defense in depth).

## Assumptions

- The Chitin Kernel continues to gate every tool call against policy; this spec is a second, independent layer — it never weakens the kernel.
- The reader/writer split applies to **untrusted external** content only. Trusted internal data (the kanban board, `.specify/` specs, the chitin chain) is not subject to it.
- This aligns with the structured-handoff principle already adopted — the kanban board (typed tickets) and the Chitin Orchestrator (spec 070, typed activities). Reader→finding→writer is the same "fixed action menu" shape.
- Inspired by Helen Fan's agent-security architecture (reader/writer separation, output sanitization, orchestrated handoffs).
- Whether reader and writer are separate processes/agents or separated roles within one agent is an implementation choice resolved in `plan.md`.

## Out of Scope

- Replacing or modifying the kernel's tool-call gating.
- The Chitin Orchestrator (spec 070).
- Runtime sandboxing/isolation of agent processes — a separate concern.
- Upstream contributions to the inspiration project.
