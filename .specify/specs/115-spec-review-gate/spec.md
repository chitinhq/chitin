---
spec_id: 115
title: Spec PR review gate — automate the consistency checks an agent can do, escalate the judgement calls
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 097
  - 113
related:
  - 094
  - 098
  - 099
  - 114
---

# Spec 115 — Spec PR review gate

## Why

Specs are PRs. Specs **need peer review** for the same reasons code does
— and as #1050 (the specs-113-and-114 PR itself) demonstrated, **Copilot
already reviews spec PRs**, often catching real bugs:

  - #1050 / `114/spec.md:78` — referenced a `chitin-kernel events` CLI
    that doesn't exist. Would have produced a spec the implementer
    couldn't faithfully execute.
  - #1050 / `113/spec.md:113` — referenced a `gh api /pulls/N/comments/M/replies`
    endpoint that returns 404. Same problem.
  - #1050 / `114/spec.md:110` — listed `lease_lost` in the reason
    taxonomy but no FR-003 rule would ever produce that reason. Cross-
    spec drift.
  - #1050 / `113/spec.md:182` — referenced a `pr_iteration_skipped`
    event in the edge cases that wasn't in the FR-010 telemetry list.
    Internal contract drift.

Eight comments total, all valid. Just like code reviews on chitin-
authored implementation PRs, **all eight required the operator to read,
evaluate, fix, push, and reply manually.** Spec 113 builds the
iteration loop for code-PR comments. Spec 115 extends the loop to spec
PRs AND adds spec-specific consistency checks that a code reviewer
wouldn't think to apply.

The substrate that already exists:

- Spec 113 (when shipped) iterates Copilot comments on `chitin/wu/*`
  branches. Spec PRs use a different branch convention (`spec/*` or
  `<author>/spec-...`) so they don't trigger the spec-113 loop without
  a discriminator change.
- Spec 097 + 098 already handle spec PRs at the dispatch layer (a
  merged spec PR triggers the factory). The REVIEW side is missing.

This spec closes the loop: spec PRs go through the same iteration
+ escalation pipeline as code PRs, with spec-specific checks layered on
top of the generic Copilot review.

## User stories

### US1 (P1) — Factory iterates spec-PR Copilot comments to zero or escalation

> As a spec author, when I open a spec PR (any PR that adds or modifies
> a `.specify/specs/NNN-*/spec.md` or `tasks.md`), Copilot's review
> comments are automatically iterated by the same factory loop spec 113
> uses for code PRs — with the same iteration cap and escalation
> semantics, just routed to a spec-tuned driver.

**Independent test:** Open a spec PR with an obvious doc inconsistency
(e.g., a CLI subcommand the spec invents that doesn't exist). Copilot
flags it. Within 5 minutes, a fixup commit lands either correcting the
spec or replying with justification. Chain emits
`spec_iteration_round_started` → `spec_iteration_completed`.

### US2 (P1) — Spec-specific consistency linter runs before Copilot review

> As a spec author, the factory runs a deterministic linter against
> every spec PR BEFORE Copilot reviews it, surfacing the mechanical
> issues (broken cross-refs, undefined events, invented CLI surfaces)
> as PR comments the iteration loop can immediately address. This
> catches the boring 80% of review issues without burning Copilot
> tokens on them.

**Independent test:** Open a spec PR that references a non-existent
`chitin-kernel events` subcommand. Linter posts a PR comment naming
the line + the broken reference within 30 seconds of PR open. The
iteration loop (US1) addresses it without Copilot needing to flag it.

### US3 (P2) — Design-judgment comments escalate, not iterate

> As the operator, when Copilot leaves a comment that's about design
> judgement (e.g., "is US3 really P2 vs P3?") rather than mechanical
> consistency, the factory recognises the difference and escalates
> immediately rather than asking the driver to "fix" it. Mechanical
> comments iterate; semantic comments need me.

**Independent test:** Copilot leaves a comment "this user story
seems redundant with US1." Factory classifies as design-judgement (via
a heuristic — see FR-007), emits `spec_iteration_escalated { reason:
"design_judgement_required" }` without dispatching a driver round.

## Functional requirements

### Trigger discrimination (US1)

- **FR-001** Extend the factory-listen webhook eligibility check (spec
  113 FR-001) to recognise spec PRs. A PR is "spec-class" iff its
  changeset is wholly contained under `.specify/specs/NNN-*/`. Spec
  PRs go through the spec-specific iteration loop (FR-004); code PRs
  go through spec 113's loop unchanged.
- **FR-002** The discriminator is computed from the GitHub webhook's
  `pull_request.changed_files` field via the existing `gh api
  repos/<owner>/<repo>/pulls/<N>/files` call — no new gh-api endpoint.

### Mechanical linter (US2)

- **FR-003** `chitin-orchestrator spec-lint <spec-dir>` subcommand
  performs the following deterministic checks against a spec.md +
  tasks.md pair. Each check is pure (no network), with a named exit
  code and a structured JSON output:
  - **L01 — frontmatter complete**: spec_id, title, status, owner,
    created, depends_on, related all present + well-formed
  - **L02 — cross-spec refs resolve**: every depends_on / related
    spec_id has a matching `.specify/specs/<N>-*/` directory
  - **L03 — task-to-FR coverage**: every `[FR-NNN]` referenced in
    tasks.md exists in spec.md; every FR in spec.md has at least one
    task touching it
  - **L04 — event taxonomy closure**: every event_type mentioned
    anywhere in spec.md (in `FR-*`, edge cases, or success criteria)
    appears in the canonical FR-NNN telemetry block; no freelance
    events
  - **L05 — CLI surface check**: every `gh api ...` invocation uses
    `repos/<owner>/<repo>/...` form; every `chitin-orchestrator ...`
    and `chitin-kernel ...` subcommand referenced is in the known
    subcommand set (the linter reads the kernel's `--help` output OR a
    curated allowlist at `.specify/known-cli-surfaces.txt`)
  - **L06 — reason taxonomy alignment**: every `reason:` string
    referenced in spec.md or tasks.md is in the canonical reason set
    declared by an `FR-NNN` of the same spec (or one of its
    depends_on)
  - **L07 — user-story test presence**: every user story has an
    `**Independent test:**` paragraph
- **FR-004** The linter runs as a deterministic step (no driver) at
  PR-open time, posting any violations as PR review comments via the
  same gh-api path the iteration loop uses
  (`repos/<owner>/<repo>/pulls/<N>/reviews` with one
  comment per violation). Linter findings are deduped by
  (rule, file, line) so a re-run on the same PR doesn't double-post.

### Spec-tuned iteration (US1)

- **FR-005** `SpecIterationWorkflow` is structurally identical to
  spec 113's `PRIterationWorkflow` but invokes a driver from the
  `spec-author` capability set (claudecode or codex with a spec-tuned
  prompt template). Reuses spec 112 US2's `worktree.Manager.Checkout`
  for the spec-PR branch.
- **FR-006** Spec-iteration prompt template differs from the code
  iteration template in three ways:
  - Includes the FULL current spec.md + tasks.md as context (not a
    diff — the spec author needs full context to reason about
    consistency)
  - Includes the linter's violations (FR-004) as structured input,
    distinguished from Copilot's comments
  - Requires the driver to address each linter violation EITHER by
    fixing the spec OR by patching the linter's allowlist (e.g.,
    "this CLI subcommand is being introduced by THIS spec, add it to
    `.specify/known-cli-surfaces.txt`")

### Design-judgement classification (US3)

- **FR-007** A Copilot comment is classified `design_judgement` iff its
  body matches any of:
  - Contains phrases like "consider", "might want", "is this really",
    "could be" (heuristic — the linter regex set is in
    `.specify/judgement-phrases.txt`, operator-editable)
  - References user-story priority (`P1`, `P2`, `P3`) or scope debate
    (`in scope`, `out of scope`)
  - Asks for spec restructuring (`should this be split`, `should this
    be merged`)
- **FR-008** When all of a Copilot review's comments classify as
  design-judgement, the workflow skips driver dispatch and emits
  `spec_iteration_escalated { reason: "design_judgement_required" }`
  immediately. When some are mechanical and some are judgement, the
  workflow iterates the mechanical ones AND escalates the judgement
  ones in the same round.

### Telemetry

- **FR-009** Chain events (closed taxonomy, same shape contract as
  spec 113 FR-010):
  - `spec_lint_completed { pr_number, rule_violations: [{rule, file, line, severity}] }`
  - `spec_iteration_round_started { pr_number, round, reviewer, comment_count, lint_violations_count }`
  - `spec_iteration_completed { pr_number, round, fixup_sha, replies_posted, action_counts: {fix, reply, skip, lint_fix} }`
  - `spec_iteration_failed { pr_number, round, failure_kind, detail }`
  - `spec_iteration_escalated { pr_number, rounds_attempted, last_review_id, reason }`
  - `spec_iteration_skipped { pr_number, reason }`
- **FR-010** Canonical `reason` strings for `spec_iteration_escalated`
  (closed set — extends spec 113 FR-011's vocabulary with the
  spec-specific kinds):
  - `iteration_cap_hit` — same semantics as spec 113
  - `human_reviewer_present` — same semantics
  - `lease_lost` — same semantics
  - `design_judgement_required` — FR-008 classification fired
  - `lint_violation_unresolvable` — driver couldn't fix the lint
    violation and didn't justify patching the allowlist

## Success criteria

- **SC-001** Re-running the #1050 scenario (specs 113+114 spec PR with
  the 8 Copilot comments) with this spec deployed produces a single
  factory fixup commit addressing the 6 mechanical comments + a single
  escalation for the 2 design-judgement comments. Operator attention:
  ≤ 5 minutes total (the escalation review) vs the ~30 minutes the
  actual #1050 fixup pass took.
- **SC-002** Mechanical linter (FR-003) catches ≥ 75% of the
  consistency issues Copilot historically flags on spec PRs (measured
  by replaying the last 10 spec PRs' Copilot comments through the
  linter — if the linter would have flagged it first, count as caught).
- **SC-003** Design-judgement classifier (FR-007) precision ≥ 80% on a
  curated test set of 20 Copilot comments hand-labelled
  judgement-vs-mechanical (false positives are worse than false
  negatives — a judgement comment escalating to the operator is fine
  even if they decide it was mechanical).

## Scope

### In scope

- Spec-PR discriminator in factory-listen
- Deterministic spec linter (`spec-lint` subcommand + 7 rules)
- `SpecIterationWorkflow` mirroring spec 113's shape with spec-tuned
  prompt + linter integration
- Design-judgement classifier + escalation routing
- Chain events for full observability
- Spec 114 `queue` integration: spec-PR escalations appear in the same
  queue with reason kinds prefixed `spec_*` (small additive change to
  spec 114 FR-008's taxonomy)

### Out of scope

- A spec template generator (`speckit specify` already exists)
- Spec content quality (this spec checks consistency, not "is the
  user story worth shipping")
- Cross-repo spec aggregation (one repo at a time)
- Auto-resolving design-judgement comments (the operator is the
  authority on design — this spec ESCALATES those, doesn't try to
  resolve)

## Edge cases

- **Spec PR also modifies code:** the FR-001 discriminator (changeset
  wholly under `.specify/specs/`) fails — the PR is treated as a code
  PR and runs through spec 113's loop. The spec linter does NOT run.
  Spec authors who want both linter + code review should split into
  two PRs.
- **Linter has a bug and posts false positives:** linter violations
  are tagged `severity: warning|error`. Only `error` violations gate
  the iteration; `warning` ones are informational. Operator can
  silence a rule for a spec by adding `lint:disable:LNN` to the spec
  frontmatter (per-rule, per-spec).
- **Spec dispatches to factory while still in iteration:** spec
  dispatch happens on `push` to main (spec 097/098); spec iteration
  runs on the PR pre-merge. They don't race.
- **Linter allowlist gets out of date:** if `.specify/known-cli-surfaces.txt`
  is stale, ALL spec PRs flag false positives. Operator runs
  `chitin-orchestrator spec-lint --refresh-allowlist` (a separate
  flag, FR-003 addendum) which re-scans the kernel/orchestrator binaries
  and prints the diff for human approval before writing.
- **Comment classified judgement but is actually mechanical:** the
  iteration loop still completes via Copilot's natural re-review on
  the next round. Operator can also reply with `@chitin-orchestrator
  iterate` on the comment thread to force-iterate it.

## Composability

- With **spec 113**: this spec's `SpecIterationWorkflow` is the
  spec-PR sibling of `PRIterationWorkflow`. Both share the workflow
  shape, deterministic WorkflowID convention, fail-soft activity
  contract, and `worktree.Manager.Checkout` for branch ops.
  Discriminator at the dispatcher routes the right workflow.
- With **spec 114**: spec-PR escalations show up in the same operator
  queue. Spec 114 FR-008 needs the small additive change to include
  the spec-specific reason kinds (`design_judgement_required`,
  `lint_violation_unresolvable`). Otherwise the same queue, same
  digest, same `--reason` drill-down works for both code and spec PRs.
- With **spec 094 (dialectic review)**: spec PRs that ALSO modify
  contracts (e.g., `contracts/*.md` under a spec dir) might want the
  dialectic verdict gate too. Out of scope here; future spec can layer
  dialectic on top of the spec-iteration loop if needed.
