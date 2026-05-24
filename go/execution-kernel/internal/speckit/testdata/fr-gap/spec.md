# Feature Specification: FR Gap

**Feature Branch**: `feat/fr-gap`

**Created**: 2026-05-23

**Status**: Draft

**Input**: User description: "A fixture with a numbering gap between FR-001 and FR-003."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Sample (Priority: P1)

A story.

**Acceptance Scenarios**:

1. **Given** state, **When** action, **Then** outcome.

### Edge Cases

- Boundary case.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST do thing one.
- **FR-003**: System MUST do thing three. (Gap: FR-002 is missing.)

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The linter flags the FR numbering gap.

## Assumptions

- FR-002 deliberately omitted to trigger the gap check.
