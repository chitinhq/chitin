---
title: L01 fail — frontmatter missing spec_id
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

# L01 fail fixture

This fixture intentionally omits `spec_id` from the frontmatter. L01
must report this as an error-severity violation.

## User stories

### US1 (P1) — Frontmatter-incomplete fixture

> As a tester, I want a fixture that fails L01 specifically.

**Independent test:** Lint produces ≥1 error-severity L01 violation.

## Functional requirements

- **FR-001** Frontmatter is missing required `spec_id`. Task T001
  documents the deliberate gap.
