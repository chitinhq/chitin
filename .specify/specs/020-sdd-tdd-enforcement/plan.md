# Implementation Plan: SDD+TDD Enforcement (Spec 020)

**Branch**: `020-sdd-tdd-enforcement` | **Date**: 2026-05-22 | **Spec**: [020-sdd-tdd-enforcement](./spec.md)

**Input**: Feature specification from `.specify/specs/020-sdd-tdd-enforcement/spec.md`

## Summary

Three-layer enforcement (L1: pre-commit no-code-without-test, L2: pre-commit spec-has-test-coverage, L3: before-gh-pr-create no-pr-without-spec) so the operator never has to remember spec-first or test-first. L1+L2 are shell+python pre-commit hooks in `swarm/bin/` mirroring the existing scope-check pattern. L3 is a chitin gate action added to `chitin.yaml`. Regression tests in `swarm/tests/test_sdd_tdd_enforcement.py` validate all layers against the lobster text + synthetic worktrees. A constitution amendment adds §1.2.

## Technical Context

**Language/Version**: Python 3.11+ (hooks/tests), Bash (hook wrappers), YAML (chitin config)

**Primary Dependencies**: git (pre-commit hooks), pytest, chitin-kernel (gate evaluation)

**Storage**: Filesystem (git worktrees, .specify/specs/, lobster files)

**Testing**: pytest — static-analysis style tests against lobster text + synthetic worktrees (matching existing pattern in `test_kanban_dispatch_zero_commit_regression.py` and `test_dispatch_base_freshness_regression.py`)

**Target Platform**: Linux (chitin swarm worker environment)

**Project Type**: Developer tooling / governance enforcement

**Performance Goals**: Pre-commit hooks must complete in <1s for typical diffs. L3 gate must complete in <2s.

**Constraints**: Hooks must work in worktree context (git-dir != worktree root). L3 must not block `gh pr edit`. Must not slow workers following the process.

**Scale/Scope**: 3 hooks + 1 gate action + 1 constitution amendment + 7 test cases + lobster install integration

## Constitution Check

- §1.1 (file-system scope): All new files are within spec-declared MAY paths. ✓
- §2 (branch/worktree): New hooks run inside worker worktrees, mirroring scope hook. ✓
- §3 (spec-kit promotion): L3 gate at PR-create adds a second enforcement point for spec presence. ✓
- §1.2 (new amendment): Adds spec→test contract; this is the spec that introduces it. ✓

No violations.

## Project Structure

### Documentation (this feature)

```text
.specify/specs/020-sdd-tdd-enforcement/
├── spec.md              # Ratified spec
├── plan.md              # This file
└── tasks.md             # Task breakdown
```

### Source Code (repository root)

```text
swarm/
├── bin/
│   ├── worker-pre-commit-no-code-without-test.sh    # L1 hook (shell wrapper)
│   ├── worker-pre-commit-no-code-without-test.py    # L1 hook (python logic)
│   ├── worker-pre-commit-spec-has-test-coverage.sh  # L2 hook (shell wrapper)
│   └── worker-pre-commit-spec-has-test-coverage.py  # L2 hook (python logic)
├── workflows/
│   └── kanban-dispatch.lobster                      # L3 gate step added here
├── tests/
│   └── test_sdd_tdd_enforcement.py                 # All AC tests
└── (existing scope hooks unchanged)
```

### Configuration

```text
chitin.yaml                           # L3 gate action declaration
.specify/constitution.md              # §1.2 amendment
```

**Structure Decision**: Follows existing pattern — shell wrapper + python logic in `swarm/bin/`, tests in `swarm/tests/`, lobster step for L3 in `swarm/workflows/`.

## Complexity Tracking

No constitution violations to justify.

## Implementation Approach

### L1 — no-code-without-test (pre-commit)

Mirrors `worker-pre-commit-scope-hook.sh` pattern: shell wrapper resolves `git rev-parse --git-dir`, finds scope/paths, calls python checker. The python checker:
1. Gets staged files via `git diff --cached --name-only --diff-filter=AM`
2. Filters for code paths (`src/**`, `routes/**`, etc.)
3. Checks if any staged file matches test patterns (`__tests__/**`, `tests/**`, etc.)
4. Falls back to commit message scan for `no-test-change-justified:` escape clause
5. Exits 0 if no code files, or if test/escape found; exits 1 with guidance otherwise

### L2 — spec-has-test-coverage (pre-commit)

Same shell+python pattern:
1. Gets staged files matching `.specify/specs/*/spec.md`
2. For each staged spec, reads content and checks for `## Test coverage` section
3. Verifies at least one table row binding an AC to a test case
4. Also checks staged test files for `spec: NNN-<slug>` reference in first 20 lines
5. Exits 1 with guidance on either failure

### L3 — no-pr-without-spec (chitin gate)

Added as a step in `kanban-dispatch.lobster` before `gh pr create`. The step:
1. Gets the diff between PR branch and base
2. Checks if any file under `.specify/specs/NNN-*/spec.md` is in the diff (add/modify/delete)
3. If not, checks PR body for `Spec: NNN-<slug>` line referencing existing spec on origin/default
4. Exits 1 with guidance if neither check passes
5. No `--allow-no-spec` bypass in MVP

### Constitution Amendment

Add §1.2 to `.specify/constitution.md` as specified in the spec.

### Regression Tests

All 7 test cases from spec AC table, plus the workflow mirror regression test extension. Tests follow the static-analysis pattern (read lobster text, assert structural properties) used by existing dispatch tests.