---
spec_id: 106
title: Capability keyword matcher — coverage for common task wording
status: Draft
owner: chitinhq
created: 2026-05-24
depends_on:
  - 070
  - 075
  - 077
related:
  - 097
  - 105
---

# Spec 106 — Capability Keyword Matcher Coverage

## Why

Dogfood of the autonomous loop (2026-05-24) surfaced the second of two routability gaps. Spec 105 handles the first (`test.author` + `docs.write` driver-registry coverage). This spec handles the second:

**`MapCapability` in `go/orchestrator/adapter/context.go` is too narrow for real task wording.** Tasks 001 / 002 / 003 / 010 / 017 / 023 / 028 in spec 097 (operator-scheduler-entrypoint) all failed mapping with `task description did not map to a known capability keyword (amend tasks.md to use a recognized keyword set)`. The wording was perfectly reasonable engineering English; the matcher just didn't have the keyword for it.

Examples (from spec 097's tasks.md):

| Task | Description (excerpt) | Should match |
|---|---|---|
| T001 | "Create fixture spec under … with spec.md, plan.md, tasks.md (3-4 tasks all mapping to code.implement)" | code.implement OR spec.author (ambiguous) |
| T002 | "Add the `--temporal-host`, `--repo-root` flags to `runSchedule`" | code.implement (matches "Add the") |
| T003 | "Wire spec-077 adapter into `runSchedule` so the schedule subcommand resolves the spec ref + compiles the DAG before dispatch" | code.implement (matches "wire ") |
| T011 | "Argv parsing test in `…/schedule_argv_test.go`" | test.author |
| T012 | "Spec-ref resolution unit tests in `…/specref_test.go`" | test.author (matches "_test.go" — but also "unit tests" matches) |
| T027 | "Operator runbook at `docs/operator/scheduling.md`" | docs.write |

Some of these (T002, T011/T012, T027) DO match the existing keyword set; the failures we saw were a mix of:
- Genuinely unmatched: T001 ("Create fixture …"), T028 (TBD inspect)
- Multi-matched (the matcher requires unique match): some descriptions hit BOTH test.author AND code.implement keywords

The matcher's strictness (FR-014 / `MapCapability` returns false unless exactly one capability matches) is the right invariant. The fix isn't to relax strictness — it's to:

1. Add keyword coverage for missing common wording
2. Curate the keyword set so genuine-ambiguity cases (where multiple capabilities legitimately apply) get a documented disambiguation rule
3. Add a tasks-lint subcommand so spec authors can validate their tasks.md BEFORE pushing it through the factory

## User stories

### US1 (P1) — Common task wording maps cleanly

> As a spec author writing realistic engineering task descriptions ("Create fixture spec…", "Argv parsing test…", "Operator runbook at…"), the spec-077 adapter classifies each task to exactly one taxonomy capability. The DAG validation step doesn't reject the spec for unclassified-keyword reasons.

**Independent test:** Re-run `simulate-webhook --spec-ref 097-operator-scheduler-entrypoint`. Every task that was previously `unclassified capability` now maps to a single capability (the test asserts on the chain event payload, not on dispatch — dispatch is spec 105's concern).

### US2 (P1) — Multi-match cases get documented disambiguation

> As a spec author, when my task description legitimately hits keywords for two capabilities, the matcher returns the more-specific capability per a documented disambiguation rule. The decision is not "fail closed" — that's noise the operator has to learn.

**Independent test:** A task description containing both "Author unit tests" (CapTestAuthor) AND "implement" (CapCodeImplement) returns CapTestAuthor (the more-specific tag). Documented in `metadata.go` and tested in `metadata_test.go`.

### US3 (P2) — `chitin-orchestrator tasks-lint <spec-ref>` audit subcommand

> As an operator, I can validate a spec's tasks.md against the matcher BEFORE pushing the spec through the factory. The subcommand prints per-task: matched capability, OR "unclassified" with the suggestion that the spec author tighten the wording.

**Independent test:** `chitin-orchestrator tasks-lint 097-operator-scheduler-entrypoint` prints a table; exit 0 iff every task matches a capability. Re-running after this spec ships on the post-fix wording produces 0 unclassified tasks.

## Functional requirements

### Keyword set extensions

- **FR-001** Add keyword coverage to `MapCapability` for these common wordings (not exhaustive; this is the audit-derived starter set):

| Capability | New keywords |
|---|---|
| `code.implement` | "add a", "create a", "create fixture", "add the flag", "add the option" |
| `test.author` | "argv parsing test", "regression test", "smoke test", "integration test" |
| `docs.write` | "operator runbook", "developer doc", "update docs", "add a CHANGELOG" |
| `spec.author` | "draft a spec", "update the spec", "create spec.md" |

Concrete additions land in `go/orchestrator/adapter/context.go capabilityKeywords`. Each new keyword has a one-line comment citing its source spec/task.

### Disambiguation

- **FR-002** Add a documented precedence rule in `MapCapability`: when a description matches multiple capabilities, prefer the **most-specific** capability via this fixed precedence order: `test.author > docs.write > spec.author > code.refactor > bulk.codegen > code.implement`. Rationale: tests + docs + specs are the specialized work; code.implement is the catch-all and should lose ties.
- **FR-003** Update `metadata.go` comment block documenting the disambiguation rule. Add a `metadata_test.go` case per precedence-pair.

### tasks-lint subcommand

- **FR-004** New `chitin-orchestrator tasks-lint <spec-ref> [--json]` subcommand. Reads the spec via the spec-077 adapter; for each task, runs `MapCapability`; prints a row per task: `task_id | capability_or_unclassified | description_excerpt`. Exit 0 iff every task maps; exit 1 if any unclassified.
- **FR-005** Add `chitin-orchestrator tasks-lint` to the help text and CHANGELOG.

### Test gates

- **FR-006** Per-FR-001 keyword: at least one unit test case in `go/orchestrator/adapter/context_test.go` asserting the keyword resolves to the right capability.
- **FR-007** Per-FR-002 precedence pair: a unit test case showing the higher-precedence tag wins.
- **FR-008** Integration test against the fixture spec from spec 105's US1/US2 fixture asserts every task in spec 097's tasks.md maps clean post-fix.

## Success criteria

- **SC-001** Re-running the spec 097 simulate-webhook dogfood from 2026-05-24 12:14pm EDT no longer reports unclassified-capability errors for T001/T002/T003/T010/T017/T023/T028.
- **SC-002** `chitin-orchestrator tasks-lint` reports zero unclassified tasks across the 5 most-recently-merged specs in `.specify/specs/`.
- **SC-003** No regression in `MapCapability` existing tests; the disambiguation rule is additive.

## Scope

### In scope

- Keyword set extensions (additive; the matcher's strictness-on-zero-match invariant stays)
- Disambiguation precedence rule + tests
- `tasks-lint` audit subcommand
- Per-keyword test coverage

### Out of scope

- Changing the closed-taxonomy invariant (FR-015 of spec 075)
- Removing the multi-match-fails behavior — it's replaced (not removed) by precedence
- Driver registry / capability declaration work (spec 105)
- LLM-based task classification (out of scope — keyword matching is intentionally deterministic)

## Edge cases

- **A task description matches no keywords:** still `unclassified` (matcher's failure mode unchanged; `tasks-lint` flags it; operator amends).
- **A task description matches two keywords from the SAME capability:** counts as one match (existing dedup via `map[Capability]struct{}`).
- **A task description hits two keywords from two capabilities of EQUAL precedence:** documented as a failure; operator must disambiguate the wording. (Equal-precedence ties shouldn't happen with the FR-002 list, but document the policy.)
- **Future capability additions to the taxonomy:** spec author MUST add the capability to the FR-002 precedence list as part of the same PR. Enforced by a test (FR-007 extension): every capability in `KnownCapabilities()` has a precedence rank.

## Assumptions

- The dogfood-derived keyword starter set (FR-001 table) is representative enough for common operator-written specs. Future iteration is fine; this spec is the v1 cut.
- The precedence order (FR-002) is defensible: tests + docs + specs are the specialized work that should NOT lose to a catch-all `code.implement` match.
- `tasks-lint` is a low-cost feature that pays for itself within ~5 specs authored.
