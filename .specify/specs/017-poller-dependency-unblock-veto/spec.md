# 017 — Poller dependency-unblock veto for explicit spec blockers

> Spec-kit entry for the morning fix after the `t_7cb9cf49` over-unblock loop.

## Ticket refs

- `t_7cb9cf49` exposed the bug: spec 004 existed, but also declared `Blocked until: chitin-kernel drivers list --json`; the poller repeatedly treated spec presence / dependency ticket status as sufficient and moved the ticket toward ready while the command still returned `unknown_subcommand`.
- Depends on no new operator decision. This is a safety hardening change to the existing dependency gate.

## Problem

The poller dependency-unblock path treats dependency tickets in `in_progress` as resolved enough to let downstream work advance. That is usually useful for broad backlog flow, but it is unsafe when a reviewed spec declares an explicit capability gate with `Blocked until:`. In that case, spec presence is not readiness.

The poller must fail closed when a blocked ticket has:

1. a current dependency-gate block reason showing an unresolved concrete condition; or
2. a bound `.specify/specs/NNN-*/spec.md` containing a `Blocked until:` condition that is not satisfied.

## Requirements

- R1: `auto_unblock_dependency_tickets()` MUST NOT unblock a ticket when any bound spec contains an unsatisfied `Blocked until:` line.
- R2: `chitin-kernel drivers list --json` is a concrete capability condition. The condition is satisfied only when the command exits 0 and returns JSON without an `error` field.
- R3: Unknown `Blocked until:` conditions MUST fail closed; they require operator or future code support before auto-unblock.
- R4: A dependency-gate `block_reason` that still records a concrete unresolved condition such as `currently unknown_subcommand` MUST veto auto-unblock.
- R5: The poller report should avoid presenting such tickets as actually ready/routed until the task state changes safely.

## Acceptance Criteria

- A blocked ticket with spec 004 reverse binding and `Blocked until: chitin-kernel drivers list --json` remains blocked while that command returns `unknown_subcommand`, even if the referenced dependency ticket is `in_progress`.
- A blocked ticket whose `block_reason` says `dependency gate: ... currently unknown_subcommand` remains blocked and is not passed to `kanban-flow unblock`.
- Existing PR-based dependency unblock behavior still works when the PR is merged and there is no explicit blocker.
- Unit tests cover both the explicit spec blocker and the unresolved `block_reason` veto.
