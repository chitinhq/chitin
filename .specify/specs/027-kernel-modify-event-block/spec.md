# 027 — Block agents from modifying event chain entries

> Stub spec filed 2026-05-18 during the overnight goal's Ares-lane
> audit. Cross-lane authored by red because Ares is hermes-agent-
> locked; Ares ratifies post-hoc once operator lifts the lockdown.
>
> **Tier: stub** — names the contract surface and Test coverage
> shape; full ACs + implementation specs land when the work is
> actually scheduled.

## Ticket refs

- chitin `t_1ba34650`
- This stub originated from the chitin spec-kit audit at
  `.specify/specs/audit-2026-05-18/INDEX.md`

## Goal

Kernel policy rule: only chitin-kernel itself writes to the event chain. Workers + skills + dispatch must NOT mutate `~/.chitin/events/*.jsonl` directly. Adds a chitin.yaml policy + a regression test.

## File-system scope (proposed)

- `chitin.yaml`
- `go/execution-kernel/internal/policy/*.go`
- `swarm/tests/test_kernel_event_modify_block.py`
- `.specify/specs/027-kernel-modify-event-block/**`

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
