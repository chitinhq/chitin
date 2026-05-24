---
spec_id: 105
title: Driver Capability Declaration — close test.author + docs.write coverage gaps
status: Draft
owner: chitinhq
created: 2026-05-24
depends_on:
  - 070
  - 075
related:
  - 097
  - 098
  - 106
---

# Spec 105 — Driver Capability Declaration

## Why

Dogfood of the autonomous loop (2026-05-24, post spec 099 merge) surfaced two real driver-registry gaps that prevent ANY spec containing test-authoring or docs-writing tasks from being dispatched by the factory:

1. **`test.author` has zero implementers.** The capability constant `CapTestAuthor = "test.author"` is declared in `go/orchestrator/driver/taxonomy.go:33`, but no driver lists it in its `Capabilities:` slice. Tasks that route to `test.author` (via the keyword matcher matching `_test.go`, `unit test`, `add a test`, etc.) fail DAG validation with `no registered driver declares this capability`.

2. **`docs.write` is declared but reaches the validator as unroutable.** `claudecode/driver.go:61` declares `CapDocsWrite`, yet `simulate-webhook` against spec 097 reports `T027 (docs runbook) — capability "docs.write" — no registered driver declares this capability`. Root cause not yet diagnosed — likely one of: (a) Ready-check filter removing claudecode before capability lookup, (b) validator's capability-coverage logic has a bug, (c) registry init order issue.

Both gaps are PR-blocking for the autonomous loop's ability to dispatch real specs whose tasks include tests or docs. This spec adds the missing implementer for `test.author` and root-causes + fixes the `docs.write` unroutability.

Spec 106 (parallel) handles the related-but-separate keyword-matcher coverage gap.

## User Stories

### US1 (P1) — `test.author` is routable

> As an operator dispatching a spec whose tasks include test-authoring work (matched via `_test.go`, `unit test`, `table-driven test`, etc.), the DAG validation step succeeds. The orchestrator picks an appropriate driver and dispatches the work, instead of failing with `no registered driver declares this capability`.

**Independent test:** A fixture spec with a single task `- [ ] T001 [P] Author unit tests for the foo package in foo_test.go` compiles + validates + dispatches via `chitin-orchestrator schedule <ref>`. The DAG node has `Capability=test.author`; the assigned driver's card lists `CapTestAuthor`.

### US2 (P1) — `docs.write` routes to claudecode

> As an operator dispatching a spec whose tasks include documentation work (matched via `documentation`, `runbook`, `write docs`, `update the doc`, etc.), the DAG validation step succeeds; the dispatcher picks claudecode (or another driver that declares `CapDocsWrite`).

**Independent test:** A fixture spec with a single task `- [ ] T001 Operator runbook at docs/operator/foo.md` validates + dispatches. The DAG node has `Capability=docs.write`; the assigned driver is one that declares `CapDocsWrite`.

### US3 (P2) — Audit surface: `chitin-orchestrator validate-driver-coverage`

> The operator can run a one-shot diagnostic listing every capability in the taxonomy alongside the drivers that implement it; missing implementers are flagged. The validator's logic for capability-coverage is the same one used by `ValidateForDispatch`, so passing this audit guarantees a non-trivial spec won't be unroutable due to a registry gap.

**Independent test:** `chitin-orchestrator validate-driver-coverage` prints a table with one row per capability in `KnownCapabilities()`; rows with zero registered implementers exit with code 1. After this spec ships, every taxonomy entry has ≥ 1 implementer.

## Functional Requirements

### Capability declarations

- **FR-001** `go/orchestrator/driver/codex/driver.go` adds `driver.CapTestAuthor` to its `Capabilities:` slice. Rationale: codex is a frontier code model; test authoring is in scope.
- **FR-002** `go/orchestrator/driver/claudecode/driver.go` adds `driver.CapTestAuthor` to its `Capabilities:` slice. Same rationale.
- **FR-003** Root-cause investigation of why `claudecode.CapDocsWrite` reports unroutable. Document the root cause in `research.md` (spec 105 phase 0). Fix lands as the most-localized code change that restores routability.

### Audit subcommand

- **FR-004** New `chitin-orchestrator validate-driver-coverage` subcommand. Lists every capability in `driver.KnownCapabilities()` and the drivers (id + Ready status) that declare it. Exit code 0 iff every capability has ≥ 1 driver registered (Ready or not — Ready is operational, registration is taxonomic).
- **FR-005** Output formats: default human-readable table; `--json` for sentinel / CI consumption.

### Test gates

- **FR-006** Unit test in `go/orchestrator/driver/codex/driver_test.go` asserts the Card lists `CapTestAuthor`.
- **FR-007** Unit test in `go/orchestrator/driver/claudecode/driver_test.go` asserts the Card lists `CapTestAuthor`.
- **FR-008** Regression test in `go/orchestrator/cmd/chitin-orchestrator/validate_driver_coverage_test.go` builds the production registry and asserts every capability has ≥ 1 declaring driver (using the existing `buildRegistry()` helper). Fails on any future taxonomy addition that lacks an implementer.
- **FR-009** Integration test against the fixture in US1 / US2 confirms the schedule subcommand validates clean (no `no registered driver declares` errors).

## Success Criteria

- **SC-001** After this spec ships, `chitin-orchestrator validate-driver-coverage` exits 0 across the full taxonomy.
- **SC-002** Re-running the spec 097 simulate-webhook dogfood from 2026-05-24 12:14pm EDT no longer reports `docs.write` or `test.author` as unroutable. (Other capabilities like the unclassified ones are out of scope — those are spec 106.)
- **SC-003** No regression in existing driver tests; `go test ./go/orchestrator/driver/...` stays green.

## Scope

### In scope

- Adding `CapTestAuthor` to two existing drivers
- Root-causing + fixing the `docs.write` unroutability
- New audit subcommand (FR-004 / FR-005)
- Test coverage that fails-on-future-regressions

### Out of scope

- Adding new capabilities to the taxonomy. The closed-taxonomy invariant (FR-015 of spec 075) stays intact.
- The keyword-matcher coverage gaps (`Create fixture`, `Argv parsing test`, etc.) — that's **spec 106**.
- Driver-registry restructuring (the registry's iteration / Ready / allowlist logic). If FR-003's investigation surfaces a deeper bug, that becomes a separate spec.
- Adding `CapTestAuthor` to all 7 drivers — only the two most-obvious implementers (codex, claudecode) get it. Others can opt-in via later capability-card updates.

## Edge cases

- **A driver's Ready() returns false on the operator host:** the validator still considers it for capability coverage (registration is taxonomic, not operational). A non-Ready driver with `CapTestAuthor` is enough to make a test-authoring spec dispatchable; SelectDriver later filters Ready at runtime.
- **The keyword matcher returns `CapTestAuthor` for an ambiguous description that also matches `CapCodeImplement`:** the matcher already enforces unique-match-or-fail (`adapter/context.go MapCapability`); no change needed here. Spec 106 may relax this.
- **Two drivers both declare `CapDocsWrite`:** SelectDriver's existing tie-breaker logic applies; no change needed.

## Assumptions

- The `docs.write` unroutability is not a deep registry bug; root-cause investigation (FR-003) will land a localized fix.
- `CapTestAuthor` is genuinely in codex's + claudecode's wheelhouse; no compelling reason to gate it behind a separate fitness assessment.
- The validate-driver-coverage subcommand fits the existing subcommand pattern in `cmd/chitin-orchestrator/`; no architectural debate needed.
