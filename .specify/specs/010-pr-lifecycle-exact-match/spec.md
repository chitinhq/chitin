# Feature Specification: PR Lifecycle Exact Match — close tickets only on explicit close keywords, not substring or reference mentions

**Feature Branch**: `fix/pr-lifecycle-exact-match`

**Created**: 2026-05-16

**Status**: shipped (fec965e, #695)

**Refs**: t_3e13b0d5 (false-close bug)

## User Scenarios & Testing *(mandatory)*

### User Story 1 — PR that references a ticket does NOT close it (Priority: P0)

A PR body says "Refs t_3e13b0d5, t_25cd184e, t_12568dca" to link related tickets for context. When this PR merges, `clawta-pr-lifecycle` must NOT close any of those tickets — they are references, not closures.

**Why this priority**: This bug caused t_3e13b0d5 to be false-closed 4 times in one day. Every merged PR that mentions the ticket in its body triggers a false close.

**Independent Test**: Create a PR with body "Refs t_3e13b0d5" (no "closes/fixes/resolves" keyword). Merge it. Verify t_3e13b0d5 status remains unchanged.

**Acceptance Scenarios**:

1. **Given** a PR with body "Refs t_3e13b0d5" (no close keyword), **When** `clawta-pr-lifecycle` processes the merged PR, **Then** it does NOT mark t_3e13b0d5 as done.
2. **Given** a PR with body "Closes t_3e13b0d5" OR "Fixes t_3e13b0d5" OR "Resolves t_3e13b0d5", **When** `clawta-pr-lifecycle` processes the merged PR, **Then** it marks t_3e13b0d5 as done (existing behavior preserved).
3. **Given** a PR with both "Refs t_25cd184e" and "Closes t_3e13b0d5" in the body, **When** processed, **Then** only t_3e13b0d5 is marked done; t_25cd184e is linked but not closed.

### User Story 2 — Branch name convention still resolves ticket (Priority: P1)

A PR from branch `swarm/hermes-b8e5337c` is merged. `infer_ticket` extracts `t_b8e5337c` from the branch name. The ticket is marked done.

**Why this priority**: Branch-based ticket resolution is the primary and most reliable mapping. It must continue to work.

**Independent Test**: Create a PR from branch `swarm/hermes-abc12345` targeting main. Merge it. Verify `infer_ticket` returns `t_abc12345`.

**Acceptance Scenarios**:

1. **Given** a PR from branch `swarm/hermes-b8e5337c`, **When** `infer_ticket` is called, **Then** it returns `t_b8e5337c`.
2. **Given** a PR from branch `hermes/spec-kit-batch-005-007` (no 8-hex suffix), **When** `infer_ticket` is called, **Then** the branch pattern does not match, and it falls through to the body/title scan.

### User Story 3 — Multiple tickets referenced in PR body (Priority: P2)

A PR body says "Closes t_12568dca. Refs t_25cd184e, t_3e13b0d5." Only the explicitly closed ticket is marked done. The referenced tickets are linked (comment posted) but not closed.

**Why this priority**: Multi-ticket PRs are common in spec-kit batches. The lifecycle must distinguish close from reference.

**Independent Test**: Create a PR with body "Closes t_12568dca. Refs t_25cd184e." Merge it. Verify only t_12568dca is marked done; t_25cd184e gets a comment but no status change.

**Acceptance Scenarios**:

1. **Given** a PR body "Closes t_12568dca. Refs t_25cd184e, t_3e13b0d5.", **When** processed, **Then** only t_12568dca is marked done.
2. **Given** a PR body "Refs t_25cd184e, t_3e13b0d5" (no close keyword), **When** processed, **Then** neither ticket is marked done; both receive a "referenced by PR #N" comment.

## Edge Cases

- **PR body has "Closes t_X" but branch resolves to t_Y**: Branch name takes precedence (it's the most reliable mapping). A comment is posted on t_X noting the body reference, but only t_Y is marked done.
- **PR body mentions t_X in a repair summary or diff**: Only `(closes|fixes|resolves) t_X` triggers close. Generic mentions ("this fixes the issue reported in t_X") do NOT trigger close unless they use the exact close keyword.
- **Self-referencing PR**: A PR that its body says "Closes t_X" but its branch resolves to t_X — this is correct; the PR closes its own ticket.
- **No ticket found at all**: PR is unrelated to any kanban ticket. `infer_ticket` returns None. No status change attempted.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: `infer_ticket` MUST distinguish between close keywords (`closes`, `fixes`, `resolves`) and reference keywords (`refs`, `references`, `ticket`, `task`, `kanban`).
- **FR-002**: Only close keywords trigger `mark_done`. Reference keywords trigger a comment but no status change.
- **FR-003**: `infer_ticket` MUST return a structured result: `{ticket_id, close_intent: bool}` instead of a bare string.
- **FR-004**: Branch-name resolution always implies close intent (backward-compatible: branch-named PRs close their ticket).
- **FR-005**: When a PR body contains both close and reference keywords for different tickets, only the explicitly closed tickets are marked done.
- **FR-006**: Reference-only tickets receive a "referenced by PR #N" comment but no status change.

### Key Entities

- **`infer_ticket`**: Function in `clawta-pr-lifecycle` that resolves a PR to its kanban ticket. Currently returns `str | None`; modified to return `{ticket_id, close_intent}`.
- **`mark_done`**: Function that transitions a ticket to `done` status. Called only when `close_intent=True`.
- **Close keywords**: `closes`, `fixes`, `resolves` (and their `close:` / `fixes:` prefixed variants).
- **Reference keywords**: `refs`, `references`, `ticket`, `task`, `kanban` (link-only, no close).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A merged PR with body "Refs t_3e13b0d5" does NOT change t_3e13b0d5's status.
- **SC-002**: A merged PR with body "Closes t_3e13b0d5" DOES mark t_3e13b0d5 as done.
- **SC-003**: A merged PR from branch `swarm/hermes-b8e5337c` marks t_b8e5337c as done (branch pattern still works).
- **SC-004**: t_3e13b0d5 is NOT false-closed by any subsequent merged PRs that reference it without a close keyword. (Regression test for the original bug.)

## Assumptions

- The PR lifecycle script is the only automated process that marks tickets as done on merge. Manual `kanban-flow done` is unaffected.
- Close keyword semantics match GitHub's supported keywords: `closes`, `fixes`, `resolves` (and their capitalized/ past-tense variants).
- This change is in `swarm/bin/clawta-pr-lifecycle`, the same file modified by the kanban-isolation spec (003).

## Phased Delivery

- **Phase 1 (this PR)**: Modify `infer_ticket` to return `{ticket_id, close_intent}`. Split regex into close-group and reference-group. Only call `mark_done` when `close_intent=True`. Add reference-only comment path. Add tests.
- **Phase 2**: Support multiple ticket closes in a single PR body (currently `infer_ticket` returns one ticket; multi-close requires parsing all close keywords).

## Out of scope

- Changing the branch-name regex (it already works correctly).
- Supporting close intent from comments (only PR body/title are trusted sources per the existing comment).
- Auto-linking referenced tickets to GitHub PRs (the comment is kanban-only).