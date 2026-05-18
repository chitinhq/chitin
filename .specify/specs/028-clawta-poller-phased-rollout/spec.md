# 028 — Clawta-poller phased rollout + backfill audit

> Stub spec filed 2026-05-18 during the overnight goal's Ares-lane
> audit. Cross-lane authored by red because Ares is hermes-agent-
> locked; Ares ratifies post-hoc once operator lifts the lockdown.
>
> **Tier: stub** — names the contract surface and Test coverage
> shape; full ACs + implementation specs land when the work is
> actually scheduled.

## Ticket refs

- chitin `t_26dc166c`
- This stub originated from the chitin spec-kit audit at
  `.specify/specs/audit-2026-05-18/INDEX.md`

## Goal

When a new invariant ships, the poller runs it in shadow mode for N cron ticks before enforcing, logging delta. After enforce flip, a backfill audit reports tickets that would have been affected if the invariant had been live during the shadow period.

## File-system scope (proposed)

- `swarm/bin/clawta-poller`
- `swarm/tests/test_clawta_poller_phased_rollout.py`
- `.specify/specs/028-clawta-poller-phased-rollout/**`

## Test coverage

### Why this is a stub for now

The implementation hasn't been scheduled yet; the test surface is
named but not bound to live test files. When the work dispatches,
the implementation PR fills the table per spec 020 §1.2 with
named test cases.

| Surface | Test layer (proposed) | Notes |
|---------|----------------------|-------|
| Primary surface | e2e (per spec 020 §1.2 default) | Replace with named test cases when impl ships |

## Invariants (TBD)

- TBD — author of the impl PR fills these per the actual surface

## Acceptance Criteria (TBD)

- TBD — author of the impl PR enumerates these against the goal

## Out of scope

- Implementation in this PR (this is a stub spec only; the work
  awaits operator-attended scheduling)

## Status

- **draft** — awaiting Ares ratification + operator scheduling
