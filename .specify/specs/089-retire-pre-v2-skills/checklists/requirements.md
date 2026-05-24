# Specification Quality Checklist: Retire pre-chitin-v2 / pre-orchestration skills

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-05-22
**Feature**: [Link to spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) — spec stays at "delete these files / strip these catalog blocks" level; no scripts or tooling described.
- [x] Focused on user value and business needs — value is "the next Claude Code session is grounded against current substrate, not a stale catalog."
- [x] Written for non-technical stakeholders — phrasing is in terms of skills, catalogs, sessions, and substrate; no internal-API names.
- [x] All mandatory sections completed — User Scenarios, Requirements, Success Criteria all present.

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain.
- [x] Requirements are testable and unambiguous — each FR names exact file paths, line ranges, or commit shape.
- [x] Success criteria are measurable — each SC is a single grep, find, or `git log` invocation with a definite expected result.
- [x] Success criteria are technology-agnostic — described in terms of file/catalog state, not which tooling produces them.
- [x] All acceptance scenarios are defined — both user stories carry explicit independent test commands.
- [x] Edge cases are identified — 4 edge cases covering revival, borderline-skill ambiguity, dirty working tree, and unrelated untracked files.
- [x] Scope is clearly bounded — explicit In/Out scope lists; specifically excludes the chitin `.claude/commands/` and gstack operator skills.
- [x] Dependencies and assumptions identified — six named predecessors (chitin v2, spec 070, 081, 069, #908, decision 2a) plus the assumption that retained skills are genuinely live.

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria — every FR maps to an SC or a directly observable file/catalog state.
- [x] User scenarios cover primary flows — P1a (catalog and files gone), P1b (retained skills untouched). Both required, both independently testable.
- [x] Feature meets measurable outcomes defined in Success Criteria — SC-001–004 cover catalog state, file presence, retained-skill function, and commit history.
- [x] No implementation details leak into specification.

## Notes

- This spec is intentionally small and acts as the system-of-record for the cull. The destructive action (13 deletions + CLAUDE.md edit) lands on the workspace repo; this spec lands on chitin and serves as the rationale + scope contract.
- The "borderline three" (`/ship`, `/ship-review`, `/triage`) are retired here because each leaned on the dead model. If their function is wanted again against the current substrate, that is a future feature, not a revival.
- All items pass.
