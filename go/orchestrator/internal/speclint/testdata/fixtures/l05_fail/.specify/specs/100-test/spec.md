---
spec_id: 100
title: L05 fail — invented CLI subcommand and bad gh api path
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

# L05 fail fixture

References two invented surfaces L05 must reject:

  - `chitin-orchestrator definitely-not-a-real-subcommand` — not in
    `.specify/known-cli-surfaces.txt` and not introduced by this spec.
  - `gh api /pulls/42/files` — does not start with `repos/`.

## User stories

### US1 (P1) — Invented CLI surfaces

> As a tester, I want a fixture that fails L05.

**Independent test:** Lint produces ≥1 error-severity L05 violation.

## Functional requirements

- **FR-001** Some flow shells out to
  `chitin-orchestrator definitely-not-a-real-subcommand` and calls
  `gh api /pulls/42/files`. Both are deliberately wrong; L05 must
  surface them. Task T001 covers this FR.
