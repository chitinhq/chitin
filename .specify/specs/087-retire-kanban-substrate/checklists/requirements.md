# Specification Quality Checklist: Retire the Kanban Substrate

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-05-22
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

Validation passed in 1 iteration. Zero `[NEEDS CLARIFICATION]` markers.

**Standing note on "no implementation details":** this is an infrastructure-retirement
feature — the spec names the *kanban substrate* and its constituent surfaces (MCP server,
kernel CLI, console routes, UI pages, swarm scripts) because they are the **things being
removed**, the problem domain itself, not prescribed implementation choices. The spec does
not prescribe HOW the removal happens or what replaces it internally; the precise file
list and the visibility-replacement design are explicitly deferred to the plan phase
(Assumptions and FR boundaries make this clear).

**Operator visibility (US2 / FR-008 / SC-004):** the spec asserts the orchestrator-side
surfaces cover the operator's needs, and that any unsatisfied view is a separate UI gap.
This is a deliberate non-block on the retirement — a UI gap surfaces as its own ticket
against the orchestrator/sessions UI, not as a reason to keep kanban alive. The planning
phase will validate this by walking through the operator's actual daily routine.

**Data preservation (US3 / FR-008 / SC-005):** the spec is explicit that on-disk
`kanban.db` files are NOT deleted by this change — code retires, data persists. The
operator manages archival on their own schedule.

**Two genuine planning-phase decisions** the spec deliberately leaves to `/speckit-plan`:
1. The precise file/directory list to delete (needs a targeted scope scan against
   current `main` — Pathfinder F7/F12/F17/F20 gave the landscape; planning needs the
   per-file ground truth).
2. The threads-page disposition. The console-UI threads page is mentioned as "tied to
   kanban *if* tied to kanban" — its actual data source is agent-bus, which spec 069
   decommissioned. Planning confirms whether the threads page is in-scope-for-retirement
   or its own separate live-or-dead question.

Ready for `/speckit-plan`.
