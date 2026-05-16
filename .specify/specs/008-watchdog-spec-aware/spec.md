# Feature Specification: Watchdog Spec-Aware Loop Clearing

**Feature Branch**: `clawta/watchdog-spec-aware-loop-clear`

**Created**: 2026-05-16

**Status**: Draft

**Refs**: validation bug after spec-kit gate launch; affected ready tickets include `t_75c8c8c1`, `t_77f5b407`, `t_12568dca`, `t_25cd184e`, `t_3e13b0d5`.

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Watchdog does not re-block tickets that now have specs (Priority: P1)

After an operator writes a spec-kit entry and clears an old `loop_detected` block, the board-watchdog must recognize that the manual-spec requirement is satisfied and leave the ticket ready for the poller.

**Why this priority**: The spec-kit gate is now live. If the watchdog re-applies stale loop blocks without checking the current spec state, spec-complete tickets remain frozen forever and the new gate cannot be validated end-to-end.

**Independent Test**: Create a blocked ticket with historical promote/demote loop comments and a body referencing an existing `.specify/specs/NNN-slug/spec.md`; run the watchdog loop detector; verify it does not call `kanban-flow block` and clears/ignores the stale loop condition.

**Acceptance Scenarios**:

1. **Given** a ticket has historical `loop_detected=true` comments and an existing spec-kit entry, **When** board-watchdog evaluates it, **Then** it does not re-apply `block_reason='promote-demote loop detected: needs manual spec'`.
2. **Given** the same ticket is currently blocked only because of the stale loop reason, **When** board-watchdog evaluates it, **Then** it clears or leaves cleared the stale loop reason and logs that the spec-kit entry satisfies the manual-spec requirement.
3. **Given** a ticket has historical loop comments but no existing spec-kit entry, **When** board-watchdog evaluates it, **Then** it may still block with the manual-spec loop reason.

### User Story 2 — Watchdog uses the same spec resolver as poller/bridge (Priority: P1)

The watchdog checks spec existence through the same ownership-aware resolver used by `clawta-poller` and `hermes-clawta-bridge`, so owned repos and shared-repo overlays behave consistently.

**Why this priority**: Divergent spec resolution was the root class of the validation failure. The three controllers must agree on whether a ticket has a valid spec.

**Independent Test**: In unit tests, patch the spec directory resolver to an owned repo path and a workspace overlay path; verify board-watchdog accepts both when `spec.md` exists.

**Acceptance Scenarios**:

1. **Given** a chitin-board ticket references `.specify/specs/008-watchdog-spec-aware/spec.md`, **When** the watchdog checks the ticket, **Then** it resolves the path under the chitin repo-local `.specify/specs/` directory.
2. **Given** a shared-repo board ticket references a workspace-side spec, **When** the watchdog checks the ticket, **Then** it resolves through the workspace overlay path, not the target repo.

## Edge Cases

- **Spec path mentioned but missing**: treat as no spec; keep/manual-spec block remains valid.
- **Multiple spec references**: any existing board-appropriate `spec.md` satisfies the manual-spec requirement.
- **Operator-owned tickets**: do not auto-unblock operator-owned tickets beyond clearing stale internal loop metadata; never override the operator assignment.
- **Historical comments remain**: old loop comments stay in history but must not force a fresh block once spec state changes.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: board-watchdog MUST call a `has_spec_kit_entry(conn, task_id)`-equivalent check before applying any manual-spec promote/demote loop block.
- **FR-002**: The spec check MUST use the shared ownership-aware spec resolver introduced for the poller/bridge gate.
- **FR-003**: If a valid spec exists, board-watchdog MUST NOT call `kanban-flow block` with `promote-demote loop detected: needs manual spec` for that ticket.
- **FR-004**: If a valid spec exists and the only current block reason is the stale manual-spec loop reason, board-watchdog MUST clear or ignore that stale reason without mutating operator assignment.
- **FR-005**: If no valid spec exists, existing loop-detection behavior remains unchanged.
- **FR-006**: Unit tests MUST cover: historical loop + spec exists, historical loop + spec missing, and no destructive mutation of operator-owned assignment.

### Key Entities

- **Spec-kit entry**: existing `.specify/specs/NNN-slug/spec.md` resolved through board ownership rules.
- **Loop block**: block reason/comment matching `loop_detected=true`, `promote-demote loop detected`, or `needs manual spec`.
- **board-watchdog**: cron/controller that detects promote-demote loops and blocks tickets needing manual specification.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A regression test proves a ticket with historical loop comments and an existing spec is not re-blocked.
- **SC-002**: A regression test proves a ticket with historical loop comments and no spec is still blocked.
- **SC-003**: In live dry-run output, spec-complete tickets are reported as spec-satisfied rather than loop-block candidates.
- **SC-004**: The five recently spec-complete tickets remain ready long enough for the poller to route/dispatch instead of being re-blocked by watchdog.

## Assumptions

- The poller/bridge spec resolver is available to import or can be factored into a shared helper without changing behavior.
- Old loop comments are durable audit history and should not be deleted.
- This fix is a controller correctness change; it does not change the spec-kit gate itself.

## Out of scope

- Rewriting the watchdog's full loop-detection algorithm.
- Changing poller demotion behavior.
- Auto-generating missing specs.
