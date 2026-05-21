# 033 — Typed egress + MCP trust policy for chitin-kernel

> Stub spec filed 2026-05-18 during the overnight goal's Ares-lane
> audit. Cross-lane authored by red because Ares is hermes-agent-
> locked; Ares ratifies post-hoc once operator lifts the lockdown.
>
> **Tier: stub** — names the contract surface and Test coverage
> shape; full ACs + implementation specs land when the work is
> actually scheduled.

## Ticket refs

- chitin `t_c7bb6c64`
- This stub originated from the chitin spec-kit audit at
  `.specify/specs/audit-2026-05-18/INDEX.md`

## Goal

Kernel-level policy declaring which network egress patterns and which MCP servers are trusted per agent/skill. Reduces blast radius of compromised tools by failing-closed on unknown egress.

## File-system scope (proposed)

- `chitin.yaml`
- `go/execution-kernel/internal/policy/egress*.go`
- `go/execution-kernel/internal/policy/mcp_trust*.go`
- `swarm/tests/test_typed_egress_mcp_trust.py`
- `.specify/specs/033-typed-egress-mcp-trust-policy/**`

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
