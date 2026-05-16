# Feature Specification: Poller Respects Spec-Kit Entries

**Feature Branch**: `fix/poller-respects-spec-kit`

**Created**: 2026-05-16

**Status**: Draft

**Refs**: Bug 2 from §10.6 standup dispatch — clawta-poller demotes ready tickets even when a valid spec-kit entry exists.

## Goal

The dispatch gate must treat the spec-kit artifact as the source of truth. A ticket should not need a copied `Spec: .specify/specs/.../spec.md` line in its kanban body when the owning repo already has a spec file that references the ticket id.

## Acceptance criteria

- [ ] `clawta-poller` accepts a ready/todo ticket when an existing board-appropriate spec file contains that ticket id, even if the ticket body does not copy the spec path.
- [ ] Existing explicit `Spec: .specify/specs/NNN-<slug>/spec.md` references continue to work.
- [ ] Missing spec files still fail closed and produce the existing missing spec-kit demote reason.
- [ ] Hermes claim path uses the same ticket-id fallback so high-priority tickets cannot diverge from poller behavior.
- [ ] Regression tests cover ticket-id resolution and the demote path.

## Boundaries

- **Empty spec root**: a ticket without a body spec path and with no matching spec file still demotes as missing spec-kit entry.
- **Body path present**: explicit body refs remain accepted for backward compatibility.
- **Ticket id in spec**: exact ticket id matches only; partial ids or prose substrings do not count.
- **Shared/team repos**: lookup uses `spec_dir_for_board()` so shared boards resolve to the workspace spec root, not the target team repo.

## Out of scope

- Changing board-watchdog behavior.
- Fixing `clawta-pr-lifecycle` PR-to-ticket false matching.
- Editing existing kanban ticket bodies to copy spec paths.
