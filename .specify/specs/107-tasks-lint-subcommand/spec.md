---
spec_id: 107
title: tasks-lint operator subcommand — validate tasks.md before factory dispatch
status: Draft
owner: chitinhq
created: 2026-05-24
depends_on:
  - 077
  - 097
  - 106
related:
  - 098
---

# Spec 107 — `tasks-lint` operator subcommand

## Why

Spec 106 closed the keyword-matcher coverage gap. The matcher now classifies the common engineering wording in `.specify/specs/NNN/tasks.md` so the factory can dispatch real specs. But spec authors still discover unclassifiable tasks the hard way — by pushing the spec, firing the factory webhook, and watching the chain event `factory_dispatch_failed` come back.

Spec 106 FR-004/005 promised a `chitin-orchestrator tasks-lint <spec-ref>` operator subcommand that runs the same matcher against a tasks.md file BEFORE pushing it. This spec implements that promise as a small, self-contained follow-up — it's intentionally deferred from spec 106's impl PR because the matcher coverage was the dogfood-blocking work; the operator-audit surface is quality-of-life.

## User stories

### US1 (P1) — Lint a tasks.md before pushing

> As a spec author who just wrote `.specify/specs/107-my-feature/tasks.md`, I can run `chitin-orchestrator tasks-lint 107-my-feature` and see per-task classification: matched capability or "unclassified". Exit 0 iff every task classifies. I catch typos + ambiguities BEFORE the factory rejects them.

**Independent test:** Author a spec with two tasks — one well-formed ("Implement the foo handler in foo.go"), one not ("Do the thing"). `tasks-lint <spec-ref>` prints two rows, exits 1, names the unclassified task in stderr.

### US2 (P2) — Machine-readable output for CI

> The subcommand supports `--json` so a CI gate can fail PRs whose spec adds an unclassifiable task to tasks.md. Output is one object per task with `task_id`, `capability`, `description_excerpt`, `classified` (bool).

**Independent test:** `tasks-lint <ref> --json | jq '.[] | select(.classified == false)'` returns rows only for unclassified tasks.

## Functional requirements

- **FR-001** New subcommand `chitin-orchestrator tasks-lint <spec-ref> [--json] [--repo-root <path>]`. Resolves the spec ref the same way `schedule` does (three-tier resolver). Compiles the spec via the spec-077 adapter. For every agent node in the resulting DAG, runs the keyword matcher and emits one row.
- **FR-002** Default output is a human-readable table: columns `task_id`, `capability` (or `unclassified`), `description_excerpt` (first 60 chars). Sorted by `task_id` ascending.
- **FR-003** `--json` output is a JSON array of objects, one per task: `{"task_id": "T001", "capability": "code.implement" | null, "description_excerpt": "...", "classified": true|false}`. Stable key order.
- **FR-004** Exit code 0 iff every task in the spec classifies; exit code 1 if any task is unclassified. Stderr names the unclassified task ids when exit is 1.
- **FR-005** Subcommand registered in `runMain` dispatch alongside the existing subcommands. Help text mentions it in `printUsage`.

## Success criteria

- **SC-001** A spec author can run `tasks-lint <ref>` and get an answer in under 1 second for any spec the factory accepts.
- **SC-002** `tasks-lint` and `factory-listen`'s schedule path agree on classification for every task — they share the same `MapCapability` codepath (no separate matcher).
- **SC-003** Running `tasks-lint` against every existing spec in `.specify/specs/NNN-*/tasks.md` produces the same classifications the factory would have used.

## Scope

### In scope

- New `tasks-lint` subcommand at `go/orchestrator/cmd/chitin-orchestrator/tasks_lint.go`
- Tests at `tasks_lint_test.go` covering happy path, --json output, exit code semantics
- Help-text update in `printUsage`

### Out of scope

- Auto-suggesting better task wording (would require a separate dictionary; future spec)
- Linting against driver capability coverage (that's `validate-driver-coverage` from spec 105)
- CI integration (operator decides whether to wire it into PR checks)

## Notes for the dispatched driver

This spec is being dispatched through the factory itself as a dogfood. The driver picking up T001 should:

- Read `go/orchestrator/cmd/chitin-orchestrator/schedule.go` for the existing subcommand pattern
- Reuse `resolveRepoRoot` + `resolveSpecRef` + `speckit.New().CompileSpec` from `runSchedule`
- Use `adapter.MapCapability` (the function from `go/orchestrator/adapter/context.go`) — do NOT duplicate the keyword logic
- Add unit tests that use `fixtureRepo` from `schedule_test.go` for spec setup
