# Phase 0 Research: Merge Queue Orchestrator

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-05-23

This document captures the design decisions taken during planning, with rationale and rejected alternatives. Each decision is keyed (R-XXX) so plan, data-model, and contracts can reference them.

---

## R-WF1 — Workflow split: parent owns queue, one child workflow per queue position

**Decision**: `MergeQueueWorkflow` (parent) accepts the full submission and starts one `PRMergeWorkflow` child per queue position, in order, waiting on each child to terminate before starting the next.

**Rationale**:
- **Independent workflow history per PR**: each PR's full lifecycle is a single inspectable Temporal workflow execution. The operator can `temporal workflow describe` a single merge without scrolling past unrelated PRs.
- **Restart correctness is automatic**: Temporal child-workflow semantics handle worker restart cleanly — the parent re-resumes at the awaitChild call; the child either ran-to-completion (resumes with its result) or replays from history.
- **Signal scoping is natural**: per-PR signals (resume, abort, approve) target the individual `PRMergeWorkflow` by ID — no need for parent-side routing.
- **Telemetry per-PR boundary is the workflow boundary**: the per-PR state transitions enumerated in FR-024 map 1:1 to the child workflow's lifecycle events.

**Alternatives considered**:
- **Single workflow with an internal for-loop over the queue**: Simpler at first glance, but every PR's full history bloats one workflow, and per-PR signal targeting requires an awkward `signalName + queuePos` indirection. Rejected.
- **Sibling workflows started from the CLI directly (no parent)**: Loses queue-level halt-on-failure semantics (FR-021); operator would have to police the queue manually. Rejected.

**Reference**: FR-021 (queue order), FR-022 (resume by re-submitting tail), FR-024 (per-position telemetry), FR-027 (restart resumes correctly).

---

## R-WF2 — Continue-As-New threshold for the parent: not needed in v1

**Decision**: `MergeQueueWorkflow` does NOT use Continue-As-New. Its history bound is the queue size × per-position event count, which for v1 typical queues (1–20 PRs) is well below Temporal's 50k-event soft limit.

**Rationale**:
- Existing `SchedulerWorkflow` uses Continue-As-New because it runs forever (tick loop). The merge queue workflow runs once per submission and terminates — no need for history bounding.
- Per-position child workflows have their own bounded histories (one PR's lifecycle, sub-100 events typical).

**Alternatives considered**:
- Continue-As-New the parent after N positions. Rejected as premature; if v2 supports unbounded streaming queues, revisit.

---

## R-SIG — Signal payload schemas

**Decision**: Three signals on the per-PR workflow, none on the parent.

| Signal | Payload | Purpose |
|--------|---------|---------|
| `resume` | `{}` (empty) | Operator says "the conflict is resolved on the branch — retry the rebase loop." Workflow re-enters the mergeability check. |
| `abort` | `{reason: string}` | Operator says "stop trying this PR; skip it." Workflow records skip + reason, terminates with `QueueResult` entry status `aborted`. Parent halts the queue per FR-021. |
| `approve` | `{approver: string, note?: string}` | Operator says "the governance gate is approved; proceed." Workflow exits the approval-blocked state and continues to mergeability check. |

Why no signals on the parent: queue-level abort is achieved by signalling `abort` on the currently-running child; the parent's wait-on-child returns and it then halts.

**Rationale**:
- Three signals cover the three operator escape valves named in spec acceptance scenarios (US3 AS2 = resume, US3 AS3 = abort, US4 AS3 = approve).
- Per-child targeting avoids the indirection of a parent-side signal router.
- Payloads are minimal — Temporal records the signal payload in history, so adding fields later is backwards-compatible.

**Alternatives considered**:
- Single `op` signal with a discriminated payload (`{kind: "resume"|"abort"|"approve", ...}`). Rejected for spec clarity; three named signals are self-documenting in `temporal workflow show` output.
- Signal queries (a different Temporal mechanism). Rejected because signal-blocked behavior is exactly what's needed, not query-driven state.

**Reference**: FR-009 (halt with operator gate), FR-018 (governance approval signal), FR-023 (operator abort signal).

---

## R-ID — Workflow ID schema

**Decision**:

| Workflow | ID format | Why |
|----------|-----------|-----|
| `MergeQueueWorkflow` | `merge-queue-${ulid}` | One ULID per submission; sortable, globally unique, time-ordered. |
| `PRMergeWorkflow` | `merge-pr-${owner}-${repo}-${prNumber}` | Stable per `(repo, PR)` pair; enables FR-003 dedup and operator-friendly inspection by PR number. |

**Rationale**:
- The parent ID is per-submission (operator may re-submit the same PR in different queues; the parent IDs differ).
- The child ID is per-PR (cross-submission). If two queues both list `chitinhq/chitin#919`, the second `StartChildWorkflow` call returns the already-existing execution due to `WorkflowIDReusePolicy: REJECT_DUPLICATE` (FR-003, FR-014). The second parent's child-await unwinds with a "already merged" outcome.
- ULIDs are preferred over UUIDs because operator inspection ordering matters.

**Alternatives considered**:
- UUID for both. Rejected — UUIDs aren't time-sortable, and per-PR dedup needs a stable ID derived from the PR, not random.
- `merge-pr-${owner}-${repo}-${prNumber}-${submissionULID}` (parent+child concatenation). Rejected — breaks the dedup property at the cost of nothing useful.

**Reference**: FR-003 (concurrent submissions, no `(repo, PR)` duplication), FR-014 (already-merged detection).

---

## R-POL — Policy table representation: explicit Go struct + classifier function

**Decision**: The policy table lives in a new package `go/orchestrator/activities/merge/policy/` as:

```go
type PolicyClass string

const (
    ClassGovernance    PolicyClass = "governance"
    ClassLiveFix       PolicyClass = "live-fix"
    ClassSpecOnly      PolicyClass = "spec-only"
    ClassResearchDocs  PolicyClass = "research-docs"
    ClassImpl          PolicyClass = "impl"
    ClassBookkeeping   PolicyClass = "bookkeeping"
)

type ClassPolicy struct {
    RequiredChecks  []string
    RequiresApproval bool
    MaxLinesChanged int  // 0 = unbounded
    BranchPrefixes  []string
    PathAllowlist   []string  // glob patterns; empty = any
    PathDenylist    []string  // glob patterns
}

type Table struct {
    Version  string  // semver; bumped on any policy change
    Policies map[PolicyClass]ClassPolicy
}
```

A pure function `Classify(snapshot PRSnapshot, table Table) (PolicyClass, error)` returns the assigned class. Tested with table-driven Go tests.

**Rationale**:
- Direct Go expression — no YAML loader, no schema validation overhead in v1.
- Versioned — the `Version` constant locks the table snapshot per release; bumping is part of any policy change PR. Satisfies FR-006 "policy class determines... gates" and policy-gate-determinism invariant.
- Pure function classifier — easy to test exhaustively against synthetic PRSnapshots without Temporal context.
- Cross-referenced from spec.md (assumption "policy table location") — spec is canonical, code mirrors with comment.

**Alternatives considered**:
- YAML-loaded at startup. Rejected for v1 because runtime-editable policy is explicitly out of scope and adds parse/validate complexity.
- Hardcoded `switch` statement in classifier. Rejected because the table form makes the 6 rows visible in one place — easier to review changes to.
- External package (e.g. `gov-policy`). Rejected because there's no other consumer.

**Reference**: FR-004 through FR-006 (classification by class), spec Assumption "policy table location".

---

## R-CONFLICT — Detecting pointer-file-only conflicts from git rebase output

**Decision**: After `git rebase origin/main` exits non-zero, run `git diff --name-only --diff-filter=U` to list the conflicted (Unmerged) paths. Compute `unresolved := set(conflictedPaths) - set(pointerAllowlist)`. If `unresolved` is empty, auto-resolve every conflicted path with `git checkout --theirs <path> && git add <path>` and call `git rebase --continue`; loop on next commit. Otherwise, halt: leave the worktree as-is for inspection, signal-block the workflow on operator action.

**Pointer allowlist for v1**: exactly `{.specify/feature.json, CLAUDE.md}`.

**Rationale**:
- `--diff-filter=U` is the canonical git invocation for "currently conflicted." Output is one path per line, machine-parseable.
- `git checkout --theirs <path>` during a rebase takes the branch's version (theirs = the commits being replayed), which is the spec-mandated resolution. Verified empirically during this session's bootstrap rebase of PR #923.
- Allowlist is closed-world: any unfamiliar conflict halts. This is the safe direction for the no-bypass invariant.
- Each commit in a multi-commit rebase may trigger a separate conflict (observed in PR #923 rebase: conflict on commit 1 for feature.json, conflict on commit 3 for CLAUDE.md). The rebase-continue loop handles this naturally.

**Alternatives considered**:
- `git merge -X theirs` against main and then push as a single commit. Rejected because the spec mandates squash on `gh pr merge`, not on a local merge that would change the visible history.
- Detecting "pointer-only" by file pattern globs rather than literal paths. Rejected for v1 — generic auto-resolve rules add risk without need.

**Reference**: FR-008 (auto-resolve pointer-file conflicts), FR-009 (halt on non-pointer conflict), SC-002, SC-003.

---

## R-CHECKS — Wait-for-checks: poll with heartbeat

**Decision**: `wait_for_checks` activity polls `gh pr view <PR> --json statusCheckRollup` every 30 seconds, calling `activity.RecordHeartbeat(ctx, <PollAttempt>)` on each iteration. Returns when all required checks (per the PR's policy class) are in `COMPLETED` state with conclusion `SUCCESS` or `NEUTRAL`. Returns failure if any required check completes with conclusion `FAILURE`, `CANCELLED`, `TIMED_OUT`, or `ACTION_REQUIRED`. Activity timeout: 30 minutes (FR-017 wait window).

**Rationale**:
- Polling fits the existing chitin-orchestrator pattern (no webhook receiver to introduce). The 30s cadence keeps GitHub API load light and matches the existing scheduler tick rate.
- `RecordHeartbeat` is mandatory because the activity may run for 30 min; without it, Temporal would surface the activity as stuck. Heartbeat enables operator visibility (FR-016).
- Per-class check sets (FR-015) are pulled from the policy table; the `wait_for_checks` activity is parameterized by the required check name list, not the class enum, so the activity is policy-agnostic.

**Alternatives considered**:
- `gh pr merge --auto` to delegate the wait to GitHub. Empirically validated this session (#923 used `--auto`), but it loses the orchestrator's visibility into the wait state, breaks per-class required-checks selection (GitHub uses repo-level branch protection settings), and skips the heartbeat that FR-016 wants. Rejected.
- Webhook receiver on the orchestrator. Out of scope for v1; would require ingress and credential handling.
- Adaptive backoff (start at 10s, grow to 60s). Rejected for v1 — fixed 30s is simpler and the typical CI duration (1–5 min) means 2–10 polls per PR, not enough to warrant adaptation.

**Reference**: FR-015 (required-checks per class), FR-016 (heartbeat), FR-017 (max wait window).

---

## R-WORKTREE — Reuse spec 070 worktree manager for rebase workspaces

**Decision**: Every `rebase_with_policy` activity invocation requests a fresh disposable worktree via the existing `worktree.Manager` (spec 070) at activity entry, and releases it at activity exit (success or failure). No worktree is reused across activity invocations; no worktree persists past activity completion.

**Rationale**:
- §2 of the constitution mandates worker-plus-worktree for every unit of work.
- Spec 070 already provides the substrate; reusing it avoids creating a second worktree-management code path.
- Disposable-per-activity is the safest scoping — if the activity is retried by Temporal, the retry gets a fresh worktree, not a possibly-corrupted carried-over one.
- Observed worktree-cleanup hygiene in chitin-orchestrator (`worktreeActivityTimeout = 5 * time.Minute`) is identical to what we need.

**Alternatives considered**:
- Pool of long-lived worktrees keyed by branch. Rejected — concurrency safety becomes ugly; if two activities race for the same branch's worktree we need locking; cleanup on crash is harder.
- Single shared worktree per workflow execution. Rejected — adds inter-activity coupling and breaks the workflow-determinism boundary if activities can leave state in a shared location.

**Reference**: Constitution §2, FR-010, spec 070's `worktree.Manager`.

---

## R-CLI — CLI surface: subcommand on the existing orchestrator binary

**Decision**: Extend `chitin-orchestrator` to dispatch on `os.Args[1]`. Default subcommand (no args, or `worker`) preserves current worker behavior. New subcommand: `chitin-orchestrator merge-queue submit <yaml-file>` dials Temporal, validates the YAML, calls `client.ExecuteWorkflow(MergeQueueWorkflow, submission)`, prints the resulting Workflow ID, and exits.

**Rationale**:
- No new binary to build, install, or document.
- Cross-module imports avoided: the CLI and worker live in the same Go module (`go/orchestrator/`), so the CLI can import workflow types directly.
- `chitin-kernel` (the operator's primary CLI) lives in a separate module (`go/execution-kernel/`) with no `go.work` linking them — adding `merge-queue` there would force a cross-module dependency, an awkward Temporal client init inside the kernel binary, or a duplicate workflow-type definition. None are worth the surface continuity.
- The user's operator install already symlinks `chitin-orchestrator` to PATH (per spec 070 install procedure), so `chitin-orchestrator merge-queue submit` works out of the box.

**Alternatives considered**:
- New standalone binary `chitin-merge-queue`. Functionally clean but adds an install/document surface. Rejected.
- Subcommand on `chitin-kernel`. Rejected per rationale above (cross-module pain).
- Bash wrapper that calls a Temporal CLI tool. Rejected — the workflow input is a structured submission, not a string; better to have typed Go parsing.

**Reference**: spec input "operator CLI subcommand," Assumption "submission surface."

---

## R-DRY — Reuse `activities/deliver.go` shell-out pattern, but in a new sub-package

**Decision**: Merge activities live in `go/orchestrator/activities/merge/` (new sub-package), not as additions to the existing top-level `activities/` package. The shell-out helpers (`git()`, `gh()`, error-wrapping) are duplicated minimally (≈30 lines) rather than extracted to a shared package for v1.

**Rationale**:
- Sub-package scoping isolates the merge-orchestrator blast radius — a bug in merge activities can't compile-break the unrelated `DeliverWorkProduct` activity.
- Premature extraction of git/gh helpers into a shared package would require touching the existing `deliver.go` (out of scope) and forcing both consumers to agree on signatures upfront.
- 30 lines of duplicated subprocess helpers is well under the cost of premature abstraction. A future spec can consolidate if a third consumer emerges.

**Alternatives considered**:
- Add merge activities directly to the top-level `activities/` package. Rejected for blast-radius reason above.
- Extract `internal/gitexec/` shared package now. Rejected — premature abstraction with one consumer.

**Reference**: prior art `go/orchestrator/activities/deliver.go`; Knuth lens on premature optimization.

---

## R-CANON — Policy class taxonomy: spec.md is canonical, code mirrors with comment

**Decision**: The 6-class taxonomy (governance / live-fix / spec-only / research-docs / impl / bookkeeping) is defined in `spec.md` (User Story 1, FR-004, FR-005, contracts/policy-table.md). The `policy_table.go` file mirrors the taxonomy with a header comment cross-referencing spec.md.

**Rationale**:
- Single source of truth: spec.md is what humans and reviewers read; the code points back to it.
- Any change to taxonomy is a spec amendment first, code change second — enforces the spec-first flow.
- Reduces drift: if someone changes the code's policy table without amending the spec, the cross-reference comment will surface the discrepancy in review.

**Alternatives considered**:
- Code is canonical; spec describes intent informally. Rejected — defeats spec-first principle and makes the spec harder to validate against.
- Generate policy_table.go from spec.md programmatically. Rejected for v1 — adds tooling without enough rows to justify.

**Reference**: Spec-kit philosophy + constitution §7 (spec drives the DAG, not heuristics).

---

## Summary

| ID | Decision area | Outcome |
|----|---------------|---------|
| R-WF1 | Workflow split | Parent owns queue; one child per position |
| R-WF2 | Continue-As-New | Not needed in v1 |
| R-SIG | Signal payloads | 3 signals on child: resume / abort / approve |
| R-ID | Workflow IDs | `merge-queue-${ulid}` parent; `merge-pr-${owner}-${repo}-${pr}` child |
| R-POL | Policy table | Explicit Go struct + pure classifier function in `activities/merge/policy/` |
| R-CONFLICT | Conflict detection | Parse `git diff --name-only --diff-filter=U`; allowlist `{.specify/feature.json, CLAUDE.md}` |
| R-CHECKS | Check waiting | Poll `gh pr view --json statusCheckRollup` every 30s with heartbeat; 30m timeout |
| R-WORKTREE | Workspace per rebase | Fresh disposable worktree per activity invocation via spec 070 manager |
| R-CLI | CLI surface | Subcommand `chitin-orchestrator merge-queue submit <yaml>` on existing binary |
| R-DRY | Code organization | New sub-package `activities/merge/`; minimal shell-out helper duplication |
| R-CANON | Taxonomy source of truth | spec.md is canonical; code mirrors via comment cross-reference |

All NEEDS CLARIFICATION items from plan Technical Context resolved. Ready for Phase 1.
