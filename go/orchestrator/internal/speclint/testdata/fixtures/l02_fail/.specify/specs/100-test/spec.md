---
spec_id: 100
title: L02 fail — dangling depends_on
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 999
related: []
---

# L02 fail fixture

`depends_on: [999]` deliberately references a spec that has no
matching `.specify/specs/999-*/` directory anywhere in this fixture's
workspace. L02 must flag the dangling reference.

## User stories

### US1 (P1) — Dangling depends_on

> As a tester, I want a fixture that fails L02.

**Independent test:** Lint produces ≥1 error-severity L02 violation.

## Functional requirements

- **FR-001** Frontmatter references spec_id 999 which does not exist.
  Task T001 documents the deliberate dangling pointer.
