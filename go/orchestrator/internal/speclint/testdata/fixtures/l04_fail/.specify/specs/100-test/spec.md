---
spec_id: 100
title: L04 fail — event_type referenced outside canonical taxonomy
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

# L04 fail fixture

`**FR-002**` is the canonical telemetry block (contains `Chain
events`). It declares `foo_started` and `foo_completed`. `**FR-001**`
references `bar_started` outside that block — L04 must flag this as a
freelance event.

## User stories

### US1 (P1) — Freelance event_type

> As a tester, I want a fixture that fails L04.

**Independent test:** Lint produces ≥1 error-severity L04 violation
naming `bar_started`.

## Functional requirements

- **FR-001** During some flow, the kernel emits `bar_started` to mark
  the start of a bar phase. (This event is intentionally not in the
  canonical FR-002 block — the fail point for L04.)
- **FR-002** Chain events (closed taxonomy):
  - `foo_started { pr_number }`
  - `foo_completed { pr_number }`
