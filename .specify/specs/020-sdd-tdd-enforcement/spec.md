# 020 — Chitin enforces SDD + TDD as governance policy

> Operator methodology call 2026-05-17 (during the readybench day-1
> retro that produced spec 019): *"And chitin could enforce
> spec-driven development and test-driven development."*

## Ticket refs

- Workspace chitin task #67 — operator log; this spec is the
  implementation contract.
- Companion to bench-devs spec 019 (which introduced the
  spec→playwright contract at the artifact level; this spec moves
  the enforcement into chitin so the contract is automatic).

## File-system scope

- `swarm/bin/worker-pre-commit-no-code-without-test.sh` (new)
- `swarm/bin/worker-pre-commit-no-code-without-test.py` (new helper)
- `swarm/bin/worker-pre-commit-spec-has-e2e-section.sh` (new)
- `swarm/bin/worker-pre-commit-spec-has-e2e-section.py` (new helper)
- `swarm/workflows/kanban-dispatch.lobster` (extend hook installer)
- `docs/governance-setup-extras/kanban-dispatch.lobster` (mirror sync)
- `swarm/tests/test_sdd_tdd_enforcement.py` (new)
- `swarm/tests/test_kanban_dispatch_zero_commit_regression.py`
  (extend mirror-match assertion)
- `chitin.yaml` (declare two new gate actions; operator re-signs after)
- `.specify/specs/020-sdd-tdd-enforcement/**`

Worker MUST NOT touch any other path under `chitin/`. No edits to
`swarm/bin/worker-pre-commit-scope-*` (existing, separate concern).

## Goal

Three layers of automatic enforcement so the operator never has to
remember to spec-first or test-first — chitin rejects the diff at
the same point it rejects out-of-scope writes or recursive deletes.

## Layers

### Layer 1 — `no-code-without-test` (pre-commit hook in worker worktree)

The hook installed beside the existing scope hook. When any file
under `src/**`, `routes/**`, `services/**`, `lib/**`, `controllers/**`,
or `models/**` is staged, the hook requires at least one staged file
under `__tests__/**`, `tests/**`, `e2e/**`, `*.test.*`, `*.spec.*`,
OR a commit-message line `no-test-change-justified: <reason>`.

- **Why both halves**: the test-path glob is the happy enforcement;
  the message escape hatch is for legitimate refactors that move
  code without behavior change. The escape hatch must be a typed
  reason so the audit log can review them.
- **Bypass**: `SWARM_SKIP_TEST_CHECK=1` for the operator's manual
  use (mirrors `SWARM_SKIP_SCOPE_CHECK=1`).

### Layer 2 — `spec-has-e2e-section` (pre-commit hook on spec files)

When any file under `.specify/specs/**/spec.md` is staged, the hook
requires the new contents to contain an `## E2E coverage` section
with at least one table row binding an AC to a named test case.

- Also requires every staged test file under `**/e2e/**` to contain
  a `// spec: NNN-<slug>` (or `# spec:` for Python) comment in the
  first 20 lines.
- **Why both halves**: spec without coverage = unverifiable; test
  without spec = unattributed.

### Layer 3 — `no-pr-without-spec` (chitin gate action `before-gh-pr-create`)

A new chitin policy action declared in `chitin.yaml`. When the
worker (or operator) invokes `gh pr create`, the gate inspects the
diff between the PR branch and its base. The PR is allowed iff
either:

1. The diff contains a file under `.specify/specs/NNN-*/spec.md`, OR
2. The PR body (passed to the gate via stdin or env) contains a
   line matching `Spec: NNN-<slug>` referencing an existing spec on
   `origin/<default_branch>`.

- **Bypass**: `--allow-no-spec` flag is NOT supported in MVP. The
  only legitimate "spec-less" PR is one that adds a spec (case 1).
  Discussion-only / docs-only PRs use case 2 by referencing a
  parent spec.

## Acceptance Criteria

- **AC1**: A worker commit that stages `apps/foo/src/feature.ts`
  without staging any matching test file fails the pre-commit hook
  with the message `commit blocked: no test file changed alongside
  code (spec 020 L1). Add a test, or use 'no-test-change-justified:
  <reason>' in the commit message.`
- **AC2**: The same commit, with a test file staged or with the
  escape clause in the message, passes.
- **AC3**: Editing `.specify/specs/099-foo/spec.md` to remove the
  `## E2E coverage` section fails the pre-commit hook.
- **AC4**: A new file `apps/portal/e2e/something.spec.ts` that
  lacks a `// spec: NNN-<slug>` comment fails the pre-commit hook.
- **AC5**: `gh pr create` against an integration branch from a
  branch whose diff includes no spec edit AND whose body lacks
  `Spec: NNN-<slug>` returns the gate-rejection envelope and the
  PR is NOT created.
- **AC6**: The same `gh pr create` with `Spec: 020-sdd-tdd-enforcement`
  in the body (and that spec existing on origin/main) succeeds.

## E2E coverage

| Spec AC | Test case (in `swarm/tests/test_sdd_tdd_enforcement.py`) | What breaks if removed |
|---------|----------------------------------------------------------|------------------------|
| AC1 | `test_l1_rejects_code_without_test` | Workers can ship untested code |
| AC2 | `test_l1_accepts_with_test_or_escape_clause` | Hook is too strict; refactors blocked |
| AC3 | `test_l2_rejects_spec_without_e2e_section` | Specs lose the playwright contract |
| AC4 | `test_l2_rejects_test_without_spec_reference` | Tests become unattributed |
| AC5 | `test_l3_blocks_gh_pr_create_without_spec_in_diff_or_body` | PRs slip without a spec |
| AC6 | `test_l3_allows_gh_pr_create_with_spec_reference` | False-positive gate; legit PRs blocked |
| (regression) | `test_workflow_mirror_matches_canonical` (extend existing) | Hook installer drift between canonical + mirror lobster |

All tests are static-analysis style (against the lobster text + a
synthetic worktree for the hooks), matching the existing pattern in
`test_kanban_dispatch_zero_commit_regression.py` and `test_dispatch_
base_freshness_regression.py`.

## Invariants

- **inv-1: enforcement = same shape as existing scope gate.** The
  new hooks slot into the same lobster install block; the new gate
  action uses the same chitin-policy mechanism. No new abstraction.
- **inv-2: every block is recoverable.** L1 has the escape clause;
  L2 has the obvious fix (add the section); L3 has the obvious fix
  (add `Spec: NNN-...`). Workers stuck on a gate must be able to
  unstick themselves without operator intervention 90% of the time.
- **inv-3: cheap before expensive.** L1+L2 run pre-commit in the
  worker (zero round-trip cost). L3 runs at PR-create, which is the
  latest moment the worker can still self-correct. None of the
  layers wait for CI.

## Out of scope

- "Tests must come BEFORE code" — TDD strict. Commit ordering is
  invisible after the fact; we enforce "tests arrive with code,"
  which is the auditable shape.
- Coverage-percentage gates (covered separately if/when a coverage
  policy lands).
- Spec-coverage gates on `bench-devs-platform` PRs originating from
  the GitHub UI (outside chitin's enforcement surface; addressed
  via repo-level branch-protection rules in a follow-up).

## Constitution amendment (lands with this spec)

Add to `.specify/constitution.md` after §1.1:

> **§1.2 Spec→test contract.** Every spec under
> `.specify/specs/NNN-<slug>/spec.md` MUST contain an `## E2E
> coverage` section binding each acceptance criterion to a named
> test case. Every test file MUST reference its spec via
> `// spec: NNN-<slug>` (or `# spec:` for Python) in the first 20
> lines. Chitin enforces both halves at commit time (spec 020).

## Why this spec exists (the retro)

Spec 019 introduced the spec→playwright contract at the artifact
level — one spec, one set of tests, manually checked in code review.
The operator's response was: *"and chitin could enforce."* This is
right. Every governance rule we have started as a human convention
that got tired of being remembered. Scope rule → §1.1 + 3-layer
defense. Branch protection → pre-push guard. Spec/test pairing is
the next one. The same playbook applies — pre-commit hook in the
worktree (cheap), policy gate at PR-create (definitive), regression
tests against the lobster text (locked).
