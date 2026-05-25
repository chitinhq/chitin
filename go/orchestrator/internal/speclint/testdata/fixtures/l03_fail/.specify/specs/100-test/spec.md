---
spec_id: 100
title: L03 fail — task references FR not declared in spec
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

# L03 fail fixture

spec.md declares only `**FR-001**`. tasks.md references `FR-999` which
does not exist in spec.md (bidirectional cover broken).

## User stories

### US1 (P1) — Unmatched FR reference

> As a tester, I want a fixture that fails L03.

**Independent test:** Lint produces ≥1 error-severity L03 violation
naming `FR-999`.

## Functional requirements

- **FR-001** Spec declares only this FR. Task T001 exercises it; task
  T002 deliberately references a non-existent FR-999.
