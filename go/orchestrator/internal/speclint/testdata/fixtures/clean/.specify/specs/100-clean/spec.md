---
spec_id: 100
title: Clean fixture — every rule passes
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

# Spec 100 — Clean fixture

## Why

A minimal spec that satisfies every L01..L07 rule. Used as the
zero-violations baseline for the hermetic golden-fixture test.

## User stories

### US1 (P1) — A clean user story

> As a tester, I want a fixture that lints clean.

**Independent test:** Run the linter against this fixture and observe
zero violations.

## Functional requirements

- **FR-001** The fixture passes every linter rule. Task T001 covers it.
