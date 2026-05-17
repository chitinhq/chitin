# Feature Specification: Poller Respects Spec-Kit Entries

**Feature Branch**: `clawta/spec-gate-ticket-ref`

**Created**: 2026-05-16

**Status**: shipped (a0302d6, #696)

**Refs**: Bug 2 from validation run; affected tickets included `t_25cd184e`, `t_12568dca`, `t_77f5b407`, and `t_75c8c8c1`.

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Ready tickets with valid specs are not demoted (Priority: P1)

When a ticket has a valid spec-kit entry on disk, `clawta-poller` must treat it as dispatch-eligible and must not demote it to triage for missing specification.

**Why this priority**: The spec-kit gate is now the dispatch prerequisite. If the poller cannot recognize valid specs, spec-complete work loops between ready and triage and no worker can dispatch.

**Independent Test**: Create a ready ticket whose body does not contain a `.specify/specs/.../spec.md` path, but whose board-appropriate `spec.md` contains the exact ticket id. Run the poller missing-spec check. Verify it returns no demotion reason.

**Acceptance Scenarios**:

1. **Given** a ready chitin ticket `t_75c8c8c1` and `.specify/specs/002-scripts-manifest/spec.md` containing `t_75c8c8c1`, **When** `clawta-poller` evaluates missing spec-kit state, **Then** it does not demote the ticket.
2. **Given** a ready ticket with a ticket-body reference to an existing `.specify/specs/NNN-slug/spec.md`, **When** the poller evaluates it, **Then** it still accepts the forward binding.
3. **Given** a ready ticket with neither a ticket-body spec path nor an exact ticket id in any board-appropriate `spec.md`, **When** the poller evaluates it, **Then** it demotes/blocks according to the existing missing-spec behavior.

### User Story 2 — Poller and bridge agree on spec existence (Priority: P1)

The poller and Hermes bridge use equivalent spec binding semantics so a ticket cannot be accepted by one controller and rejected by another.

**Why this priority**: Validation exposed controller disagreement. Both controllers must agree before end-to-end dispatch can stabilize.

**Independent Test**: For the same synthetic ticket and spec root, verify both `clawta-poller.has_spec_kit_entry()` and `hermes-clawta-bridge.has_spec_kit_entry()` return true for reverse binding and false for missing/exact-mismatch cases.

**Acceptance Scenarios**:

1. **Given** a spec file contains exact ticket id `t_12568dca`, **When** poller and bridge check the ticket, **Then** both return true.
2. **Given** a spec file contains `t_12568dca0` but the ticket id is `t_12568dca`, **When** poller and bridge check the ticket, **Then** both return false.

## Edge Cases

- **Exact token matching**: `t_abc12345` must not match `t_abc123450` or `x_t_abc12345`.
- **Shared/team repos**: reverse lookup must use the same board-appropriate spec root as the existing ownership-aware resolver.
- **Unreadable spec file**: skip it and continue checking other specs; do not crash the poller.
- **Both bindings present**: either valid binding satisfies the gate.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: `clawta-poller` MUST accept an existing board-appropriate `spec.md` that contains the exact ticket id, even if the ticket body lacks a spec path.
- **FR-002**: `clawta-poller` MUST preserve existing support for ticket-body `.specify/specs/NNN-slug/spec.md` references.
- **FR-003**: `hermes-clawta-bridge` MUST use equivalent forward and reverse spec binding semantics.
- **FR-004**: Reverse lookup MUST scan only the board-appropriate spec root returned by the ownership-aware resolver.
- **FR-005**: Reverse lookup MUST use exact ticket-id matching, not substring matching.
- **FR-006**: Regression tests MUST cover valid reverse binding, missing spec, and exact-id mismatch.

### Key Entities

- **Forward binding**: ticket body references `.specify/specs/NNN-slug/spec.md` and the file exists under the board spec root.
- **Reverse binding**: a board-appropriate `spec.md` contains the exact ticket id.
- **Spec root**: owned repos use repo-local `.specify/specs`; shared repos use workspace `.specify/specs`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Targeted poller and bridge tests pass for forward and reverse bindings.
- **SC-002**: A local live probe returns true for the four spec-complete validation tickets: `t_25cd184e`, `t_12568dca`, `t_77f5b407`, and `t_75c8c8c1`.
- **SC-003**: On the next deployed poller tick, spec-complete tickets are not demoted to triage for missing spec-kit entries.

## Assumptions

- Specs are durable and may be written after tickets already exist.
- Operators may choose reverse binding as the primary durable association because it avoids mutating historical ticket bodies.
- `board_resolver.spec_dir_for_board()` remains the source of truth for spec root selection.

## Out of scope

- Changing dispatch driver selection.
- Rewriting board-watchdog loop detection.
- Automatically editing ticket bodies to add spec paths.
