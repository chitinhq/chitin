---
spec_id: 100
title: L06 fail — reason string not in canonical taxonomy
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

# L06 fail fixture

`**FR-002**` declares the canonical reason set: `{iteration_cap_hit,
lease_lost}`. `**FR-001**` references `reason: "totally_made_up"` —
L06 must flag the freelance reason.

## User stories

### US1 (P1) — Freelance reason string

> As a tester, I want a fixture that fails L06.

**Independent test:** Lint produces ≥1 error-severity L06 violation
naming `totally_made_up`.

## Functional requirements

- **FR-001** When the workflow gives up, it emits an escalation event
  with `reason: "totally_made_up"`. (Not in FR-002's canonical set —
  the fail point for L06.) Task T001 covers this FR.
- **FR-002** Canonical escalation `reason` set (closed taxonomy):
  - `iteration_cap_hit`
  - `lease_lost`
