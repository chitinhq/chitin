# Feature Specification: PR Lifecycle Exact Ticket Matching

**Feature Branch**: `clawta/pr-lifecycle-exact-ticket-match`

**Created**: 2026-05-16

**Status**: Draft

**Refs**: validation bug where `clawta-pr-lifecycle` repeatedly marked `t_3e13b0d5` done from unrelated PR activity.

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Lifecycle only marks the ticket explicitly mapped to the PR (Priority: P1)

When a PR merges, `clawta-pr-lifecycle` marks a kanban ticket done only if the PR explicitly references that exact ticket id in an allowed mapping location.

**Why this priority**: The lifecycle controller falsely marked `t_3e13b0d5` done multiple times from unrelated PRs. False closes erase real unshipped work and break the spec-kit pipeline's source of truth.

**Independent Test**: Provide a merged PR whose body references `t_3e13b0d5` only in historical/commentary text or as part of an unrelated comment; verify lifecycle does not mark `t_3e13b0d5` done.

**Acceptance Scenarios**:

1. **Given** a merged PR body contains `Refs t_3e13b0d5` in the canonical PR body mapping section, **When** lifecycle runs, **Then** it may mark exactly `t_3e13b0d5` done if all other gates pass.
2. **Given** a merged PR has comments mentioning `t_3e13b0d5` but the PR body/title does not explicitly map the PR to that ticket, **When** lifecycle runs, **Then** it does not mark `t_3e13b0d5` done.
3. **Given** a PR body mentions `t_3e13b0d5` in prose such as "not related to t_3e13b0d5", **When** lifecycle runs, **Then** it does not treat that as a mapping.

### User Story 2 — Exact ticket ids prevent substring and stale-comment matches (Priority: P1)

Ticket matching uses exact `t_[a-f0-9]{8}` tokens from approved fields and never substring scans against branch names, comments, commit messages, or stale lifecycle notes.

**Why this priority**: A substring/comment scan can attach unrelated PRs to stale tickets. The lifecycle controller needs deterministic mapping, not fuzzy inference.

**Independent Test**: Run lifecycle mapping tests over PR fixtures containing ticket id substrings, multiple ids, comments-only ids, and canonical body refs; verify only canonical exact refs map.

**Acceptance Scenarios**:

1. **Given** a PR title contains `3e13b0d5` without the `t_` prefix, **When** lifecycle maps tickets, **Then** no ticket is matched.
2. **Given** a branch name contains `agent/codex-3e13b0d5`, **When** lifecycle maps tickets, **Then** no ticket is matched unless the PR body has the canonical ticket ref.
3. **Given** a PR body has `Refs t_aaaa1111, t_bbbb2222`, **When** lifecycle maps tickets, **Then** it returns exactly those two ticket ids and no ids from comments.

### User Story 3 — Ambiguous mappings are surfaced, not auto-closed (Priority: P2)

If lifecycle finds multiple possible ticket ids or cannot distinguish canonical mapping from prose, it reports an ambiguity and leaves the ticket unchanged.

**Why this priority**: Safe no-op beats false completion. Ambiguous mappings need human/operator attention rather than automated done transitions.

**Independent Test**: Feed lifecycle a PR body with conflicting canonical refs and exclusion prose; verify lifecycle emits a `mapping_ambiguous` classification and does not call `kanban-flow done`.

**Acceptance Scenarios**:

1. **Given** a PR body has both `Refs t_11111111` and `Not related to t_22222222`, **When** lifecycle runs, **Then** it maps only `t_11111111` and ignores the negated prose.
2. **Given** a PR body has no canonical mapping but comments mention a ticket, **When** lifecycle runs, **Then** it reports `unmapped` and does not mark any ticket done.
3. **Given** a PR body has a malformed ticket id, **When** lifecycle runs, **Then** it reports the malformed ref and does not infer a ticket.

## Edge Cases

- **Multiple canonical refs**: allowed only when the PR intentionally closes multiple tickets; lifecycle marks only those exact ids.
- **Case variation**: ticket ids are lowercase; uppercase variants are ignored unless normalized by a dedicated parser with tests.
- **Markdown links**: `[t_3e13b0d5](...)` counts only if it appears in an allowed mapping line.
- **PR comments**: comments are audit context, not mapping authority.
- **Commit messages**: commit messages are not mapping authority.
- **Branch names**: branch names are not mapping authority.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: `clawta-pr-lifecycle` MUST derive ticket mappings only from approved PR metadata fields: PR body canonical mapping lines and, if already supported, a dedicated machine-readable marker.
- **FR-002**: `clawta-pr-lifecycle` MUST NOT derive done mappings from PR comments, commit messages, branch names, or arbitrary substring scans.
- **FR-003**: The ticket parser MUST match exact `t_[a-f0-9]{8}` tokens only.
- **FR-004**: Canonical mapping lines MUST be explicit, e.g. `Refs t_xxxxxxxx`, `Closes t_xxxxxxxx`, or an agreed HTML marker such as `<!-- kanban: t_xxxxxxxx -->`.
- **FR-005**: Negated prose such as `not related to t_xxxxxxxx` MUST NOT create a mapping.
- **FR-006**: If no canonical mapping exists, lifecycle MUST classify the PR as `unmapped` and skip `kanban-flow done`.
- **FR-007**: Regression tests MUST include the `t_3e13b0d5` false-close scenario.

### Key Entities

- **Canonical ticket mapping**: explicit PR body line/marker that binds a PR to one or more kanban ticket ids.
- **Unmapped PR**: PR with no canonical mapping; lifecycle may review/merge status but cannot mark a ticket done.
- **Ambiguous mapping**: malformed or conflicting refs that require operator review.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Unit tests prove comments-only references do not mark tickets done.
- **SC-002**: Unit tests prove branch-name ticket suffixes do not mark tickets done.
- **SC-003**: Unit tests prove canonical `Refs t_xxxxxxxx` body lines still map correctly.
- **SC-004**: Live lifecycle dry-run no longer classifies unrelated merged PRs as `mark-done` for `t_3e13b0d5`.

## Assumptions

- PR body is the authoritative human-editable mapping surface for swarm PRs.
- Existing lifecycle gates for checks/reviews/mergeability remain unchanged.
- This change only narrows ticket mapping authority; it does not alter merge policy.

## Out of scope

- Redesigning kanban↔PR schema.
- Backfilling historical PR bodies.
- Auto-reopening incorrectly closed historical tickets beyond `t_3e13b0d5` unless separately requested.
