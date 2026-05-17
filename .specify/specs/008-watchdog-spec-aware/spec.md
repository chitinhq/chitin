# Feature Specification: Watchdog Spec Awareness — check spec-kit entries before re-blocking loop tickets

**Feature Branch**: `fix/watchdog-spec-aware`

**Created**: 2026-05-16

**Status**: shipped (fec965e, #695)

**Refs**: t_75c8c8c1 (watchdog bug), architecture audit #580

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Watchdog checks spec-kit before re-blocking a loop ticket (Priority: P0)

A ticket was previously caught in a promote-demote loop and blocked by the board-watchdog with `block_reason='promote-demote loop detected: needs manual spec'`. The operator then writes a spec-kit entry for it (`.specify/specs/NNN-<slug>/spec.md`). On the next watchdog tick, the watchdog sees the loop history but also sees the spec-kit entry exists. It does NOT re-block the ticket.

**Why this priority**: This bug caused 4 of 5 just-unblocked tickets to be re-blocked within 3 minutes of unblocking. It's the highest-impact failure mode of the current system.

**Independent Test**: Manually unblock a ticket that has a spec-kit entry. Run the watchdog tick. Verify it does NOT re-block the ticket but instead clears the loop flag.

**Acceptance Scenarios**:

1. **Given** ticket `t_75c8c8c1` was blocked with `block_reason='promote-demote loop detected: needs manual spec'`, **And** `has_spec_kit_entry(conn, 't_75c8c8c1')` returns `True`, **When** the board-watchdog processes this ticket, **Then** it does NOT re-block the ticket, and clears `loop_detected` (or equivalent flag) so future watchdog ticks also skip it.
2. **Given** ticket `t_99999999` was blocked with `block_reason='promote-demote loop detected: needs manual spec'`, **And** `has_spec_kit_entry(conn, 't_99999999')` returns `False`, **When** the board-watchdog processes this ticket, **Then** it re-blocks the ticket with the same reason (current behavior preserved).
3. **Given** a ticket that was never loop-detected, **When** the board-watchdog processes it, **Then** no loop-related action is taken (current behavior preserved).

### User Story 2 — Watchdog comment explains why a ticket was NOT re-blocked (Priority: P2)

When the watchdog skips re-blocking a formerly-loop-detected ticket because its spec-kit entry now exists, it posts a kanban comment explaining the resolution.

**Why this priority**: Without the comment, operators can't tell why a ticket stayed unblocked after the watchdog tick. The comment creates an audit trail.

**Independent Test**: Unblock a loop-detected ticket that has a spec entry. Run the watchdog. Verify a kanban comment appears explaining the spec-kit entry resolved the loop.

**Acceptance Scenarios**:

1. **Given** the watchdog skips re-blocking a ticket because the spec-kit entry exists, **When** the watchdog completes, **Then** a kanban comment is posted on the ticket: `"🔄 Loop previously detected; spec-kit entry now exists at .specify/specs/NNN-<slug>/spec.md — ticket remains unblocked."`

## Edge Cases

- **Spec-kit entry created between watchdog ticks**: First tick after spec creation should resolve the loop.
- **Spec-kit entry deleted after clearing loop**: Watchdog should not re-block on subsequent ticks (the loop flag was already cleared).
- **Board-watchdog is an LLM-driven cron**: The fix is a prompt update, not a code change. The prompt must instruct the LLM to call `has_spec_kit_entry` before re-blocking.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The board-watchdog MUST check `has_spec_kit_entry(conn, tid)` before re-blocking a ticket that was previously loop-detected.
- **FR-002**: If the spec-kit entry exists, the watchdog MUST NOT re-block the ticket and SHOULD clear the loop flag in the kanban DB.
- **FR-003**: If the spec-kit entry does NOT exist, the watchdog MUST preserve the current behavior (re-block with `block_reason='promote-demote loop detected: needs manual spec'`).
- **FR-004**: The watchdog MUST post a kanban comment when it resolves a loop detection due to a spec-kit entry appearing.
- **FR-005**: The watchdog cron prompt MUST include instructions to run `python3 /home/red/.hermes/scripts/hermes-clawta-bridge.py --check-spec <ticket_id>` (or equivalent) before re-blocking loop-detected tickets.

### Key Entities

- **Board-watchdog cron job** (ID `388e38b20bd5`): Hermes cron that detects promote-demote loops and auto-grooms triage tickets.
- **`has_spec_kit_entry(conn, tid)`**: Function from PR #688 that checks whether a ticket references an existing spec-kit entry on disk.
- **`loop_detected` flag**: Kanban DB field or `block_reason` substring indicating a ticket was in a promote-demote loop.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After a spec-kit entry is created for a loop-detected ticket, the next watchdog tick does NOT re-block it.
- **SC-002**: A kanban comment appears on the ticket explaining the resolution.
- **SC-003**: Tickets without spec-kit entries continue to be re-blocked (no regression).

## Assumptions

- The board-watchdog is an LLM-driven hermes cron job (prompt, not code). The fix is a prompt update.
- `has_spec_kit_entry` is callable from the bridge script, which the cron job can invoke via `python3 ~/.hermes/scripts/hermes-clawta-bridge.py --check-spec <ticket_id>`.
- Clearing the loop flag is done via `kanban-flow unblock <id> --author board-watchdog` or equivalent.

## Phased Delivery

- **Phase 1 (this PR)**: Update the board-watchdog cron prompt to check spec-kit entries before re-blocking. Add a regression test scenario.
- **Phase 2**: Add a `--clear-loop` flag to `kanban-flow` or the bridge script for explicit loop flag clearing.

## Out of scope

- Rewriting the board-watchdog as a deterministic script (it remains LLM-driven; the prompt adds the guard).
- Changing the loop detection algorithm (threshold of 3 promote-demote cycles in 24h is unchanged).
- Auto-creating spec-kit entries from ticket bodies (the operator writes specs; the watchdog only checks they exist).