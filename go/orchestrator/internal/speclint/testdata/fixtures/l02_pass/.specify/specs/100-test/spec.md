---
spec_id: 100
title: L02 pass — depends_on resolves
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 200
related:
  - 200
---

# L02 pass fixture

`depends_on: [200]` resolves to the sibling spec at
`.specify/specs/200-sibling/`. L02 must accept it.

## User stories

### US1 (P1) — Resolved depends_on

> As a tester, I want a fixture that passes L02 even though it has
> cross-spec references.

**Independent test:** Lint produces zero L02 violations.

## Functional requirements

- **FR-001** Frontmatter references spec_id 200 which resolves to the
  sibling fixture. Task T001 documents the resolution.
