---
spec_id: 108
title: Two-tier driver allowlist — implementation pool vs review pool
status: Draft
owner: chitinhq
created: 2026-05-24
depends_on:
  - 075
  - 094
  - 101
related:
  - 105
  - 107
---

# Spec 108 — Two-tier driver allowlist (implementation vs review)

## Why

The 2026-05-24 autonomous-loop dogfood ran end-to-end with the operator pin `CHITIN_DRIVER_ALLOW=codex`. The loop completed: 5 work units → 5 implementation PRs from worker branches. Then spec 094's `PRReviewWorkflow` halted on PR #1003 with `reviewer pool shortfall: need 2 primaries, have 1 after exclusions`.

Root cause: `CHITIN_DRIVER_ALLOW` is **one allowlist that applies to every driver-selection codepath**. The single pin serves two semantically-different roles:

| Role | Driver count constraint |
|---|---|
| Implementation dispatch (SelectDriver for a code.implement work unit) | exactly 1 driver picked deterministically — wider pool is fine but unused |
| Review dispatch (SelectReviewers for spec 094's dialectic) | **at least 2 primaries required**; spec 094 halts on `pool shortfall` |

The operator's intent on 2026-05-24 was "pin implementation to codex for cost/audit". That's correct for SelectDriver but starves SelectReviewers. The constitutional fix is a two-tier allowlist: distinct env-driven pools per role.

## User stories

### US1 (P1) — Operator pins implementation driver without breaking review

> As the operator, I set `CHITIN_DRIVER_ALLOW_IMPL=codex` (one driver) AND `CHITIN_DRIVER_ALLOW_REVIEW=codex,claudecode` (two drivers). My implementation work dispatches deterministically to codex. My PRs get dialectic review via the 2-primary spec 094 gate using both drivers.

**Independent test:** Start orchestrator with the above env. Dispatch a code.implement work unit → SelectDriver returns codex. Dispatch a `pr-review` for a PR → SelectReviewers returns a 2-primary slate from {codex, claudecode}.

### US2 (P2) — Backward-compatible fallback to single allowlist

> If an operator only sets `CHITIN_DRIVER_ALLOW=<list>` (the existing env), it applies to BOTH pools — preserving existing behavior. The new env vars are opt-in overrides.

**Independent test:** With only `CHITIN_DRIVER_ALLOW=codex,claudecode` set, both SelectDriver and SelectReviewers see the same pool. With `CHITIN_DRIVER_ALLOW_IMPL=codex` ALSO set, SelectDriver restricts further.

### US3 (P2) — Audit subcommand surfaces the active tiered config

> `chitin-orchestrator validate-driver-coverage` (spec 105 FR-004) prints both pools: impl + review, with the resolved driver IDs. Operator sees at a glance what's pinned where.

**Independent test:** With the two env vars set, the validate-driver-coverage table shows separate `impl` and `review` rows for each capability, listing only the drivers the relevant pool admits.

## Functional requirements

- **FR-001** New env var `CHITIN_DRIVER_ALLOW_IMPL`, comma-or-space separated driver IDs. When set, filters the registry used by `SelectDriver` (the dispatcher's per-work-unit driver pick) — the path through `buildRegistry()` that the SchedulerWorkflow's `select_driver` activity consumes.
- **FR-002** New env var `CHITIN_DRIVER_ALLOW_REVIEW`, same format. When set, filters the registry used by `SelectReviewers` (spec 094's dialectic-pool selection) — the path through the `pr-review` subcommand and the `PRReviewWorkflow`.
- **FR-003** Existing `CHITIN_DRIVER_ALLOW` is the **fallback** for either env: if `_IMPL` or `_REVIEW` is unset, the corresponding pool uses `CHITIN_DRIVER_ALLOW` (or all drivers, if also unset). Backward-compatible.
- **FR-004** `chitin-orchestrator validate-driver-coverage` (spec 105 FR-004) extends to show both pools per capability when they differ. JSON output includes both `impl_drivers` and `review_drivers` keys per capability row.
- **FR-005** Test gate: a regression test asserts that setting `CHITIN_DRIVER_ALLOW_IMPL=codex` AND `CHITIN_DRIVER_ALLOW_REVIEW=codex,claudecode` produces a 1-driver SelectDriver pool but a 2-driver SelectReviewers pool. Cross-checks the two paths don't bleed.

## Success criteria

- **SC-001** Operator can run the autonomous loop end-to-end (impl → review → verdict comment) with codex pinned for implementation and {codex,claudecode} pinned for review.
- **SC-002** Existing operators with only `CHITIN_DRIVER_ALLOW` set experience zero behavior change.
- **SC-003** No regression in `go test ./go/orchestrator/...`.

## Scope

### In scope

- Two new env vars + the fallback semantics
- Refactor of `buildRegistry()` to take a role parameter (impl vs review) OR build two registries upfront
- `validate-driver-coverage` extension for the two-pool display
- Tests covering all 4 (impl, review) × (set, unset) combinations

### Out of scope

- Per-capability allowlists (e.g. "for docs.write only allow claudecode") — that's a finer-grained extension; future spec if needed
- Per-spec routing overrides (e.g. tasks.md frontmatter `driver: codex`) — separate spec
- Dynamic re-pinning (env vars are read at startup; restart required to change)

## Edge cases

- **Both `_IMPL` and `_REVIEW` unset:** use `CHITIN_DRIVER_ALLOW` (or no filter). Existing behavior preserved.
- **`_IMPL` set but `_REVIEW` unset:** review pool falls back to `CHITIN_DRIVER_ALLOW` or all drivers. Impl is restricted; review stays wider. (This is the operator's intent in the 2026-05-24 incident.)
- **`_REVIEW` set to 1 driver:** spec 094 halts on pool shortfall as before. The two-tier mechanism doesn't paper over a too-narrow review pool.
- **`_IMPL` set to a driver not on the host:** SelectDriver returns `blocked-unroutable`. Same as today's `CHITIN_DRIVER_ALLOW=<unknown>`.

## Assumptions

- The `select_driver` activity is the single chokepoint for implementation dispatch. (Verify in implementation phase.)
- The `SelectReviewers` activity is the single chokepoint for review pool selection. (Verify in implementation phase.)
- The constitutional "deterministic orchestration" gate (§7) tolerates per-role pool semantics — what's deterministic is the SELECTION within a pool, not the pool boundaries themselves.

## Implementation phase notes

This spec is design-only. Implementation likely lands as 1-2 PRs:
1. Plumb the two env vars through `buildRegistry()` (probably parameterize it with a `role` argument)
2. Wire `SelectReviewers` to use the review registry, extend `validate-driver-coverage` to surface both
