---
spec_id: 200
title: Sibling spec for L02 pass fixture
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

# Spec 200 — Sibling for L02 pass

Existence is enough — L02 only checks that the depends_on/related
id resolves to a sibling spec directory under `.specify/specs/`.

## User stories

### US1 (P1) — Exist so L02 can find me

> As a sibling fixture, my job is to exist so L02 resolves the
> reference.

**Independent test:** Listing `.specify/specs/` includes `200-sibling`.

## Functional requirements

- **FR-001** Sibling directory exists at `.specify/specs/200-sibling/`.
  Task T001 documents the resolution target.
