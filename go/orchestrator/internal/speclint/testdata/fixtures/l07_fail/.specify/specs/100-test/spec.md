---
spec_id: 100
title: L07 fail — user story missing Independent test
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

# L07 fail fixture

US1 has an `**Independent test:**` paragraph. US2 deliberately does
not — L07 must flag US2.

## User stories

### US1 (P1) — Story with the required Independent test

> As a tester, I want a passing user story.

**Independent test:** Lint observes US1 has the required paragraph.

### US2 (P2) — Story missing the Independent test

> As a tester, I want a fixture that fails L07 on US2 specifically.

This story deliberately has no `**Independent test:**` paragraph
inside its section. L07 must report a violation pointing at US2.

## Functional requirements

- **FR-001** US2 lacks the required Independent test paragraph. Task
  T001 documents the deliberate gap.
