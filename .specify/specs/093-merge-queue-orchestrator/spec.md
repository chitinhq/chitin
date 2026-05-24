# Feature Specification: Merge Queue Orchestrator

**Feature Branch**: `feat/093-merge-queue-orchestrator`

**Created**: 2026-05-23

**Status**: Draft

**Input**: User description: "A Temporal-orchestrated PR merge workflow that makes multi-repo, queue-aware, policy-gated PR merges deterministic, auditable, and recoverable. Constitution §7 (PR #925, merged 2026-05-23) declares 'the swarm is the orchestrator' — every multi-step deterministic flow belongs in Temporal. Ad-hoc `gh pr merge` calls violate this the moment a queue exists. This spec turns the merge process into a first-class orchestrator workload."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Operator submits a queue of ready PRs and they merge themselves (Priority: P1)

The operator has a set of pull requests across one or more repositories that are ready to land in a specific order. Today the operator manually checks each one (mergeability, CI, dependencies), runs `gh pr merge` serially, rebases the next PR when an earlier merge dirties it, watches CI, and deletes branches by hand. With this feature the operator writes a small queue file describing the PRs and their order, submits it once, and the orchestrator merges them deterministically while the operator does other work.

**Why this priority**: This is the MVP. Without queue-aware sequenced merging, the orchestrator delivers nothing the operator cannot already do with `gh` calls. Every other story is an extension of this one.

**Independent Test**: The operator submits a queue containing 2–7 mergeable, non-conflicting PRs of mixed policy classes. The orchestrator processes them in order; all that pass policy land as squash-merges; their source branches are deleted; a single queue summary reports counts and per-PR outcomes. The operator's only inputs are the initial submission and any human-approval signals for governance-class entries.

**Acceptance Scenarios**:

1. **Given** a queue of 3 PRs in the same repo (one spec-only, one implementation, one bookkeeping), all currently mergeable with CI green, **When** the operator submits the queue, **Then** all 3 PRs are squash-merged in the submitted order, all 3 source branches are deleted, and a summary reports 3 of 3 merged.
2. **Given** a queue where PR #2 depends on PR #1 (declared explicitly), and PR #1's merge will dirty PR #2 via a non-conflicting rebase, **When** the operator submits the queue, **Then** PR #1 is merged, PR #2 is automatically rebased onto the new main and force-pushed with lease, CI re-runs, and PR #2 is merged once checks pass — without the operator touching anything between the two merges.
3. **Given** a queue containing PRs in two different repositories where the operator has `gh` credentials for both, **When** the operator submits the queue, **Then** each PR is processed against its own repository with no cross-contamination of branches, checks, or merge state.

---

### User Story 2 — Pointer-file conflicts auto-resolve so batch merges actually finish (Priority: P2)

In the chitin repo every PR conflicts on `.specify/feature.json` and (usually) `CLAUDE.md` because both files are per-branch state by design — each branch points them at its own spec. When the operator merges PR after PR, every PR after the first hits these mechanical conflicts and would normally require a human to open the file, choose "theirs," and force-push. The orchestrator recognizes these conflicts as the canonical pattern they are and resolves them automatically.

**Why this priority**: Without auto-resolution, a queue of N PRs forces N–1 manual conflict resolutions on pointer files alone, which destroys the unattended-operation premise of US1. This story is the difference between "set and forget" and "babysit."

**Independent Test**: Submit a queue of 3 PRs each touching only their own spec directory and the two pointer files. Earlier merges dirty later PRs. The orchestrator rebases each, takes the branch's version of both pointer files, force-pushes with lease, and continues. Operator intervention count is zero.

**Acceptance Scenarios**:

1. **Given** a PR whose only conflict against the new main is `.specify/feature.json`, **When** the orchestrator rebases it, **Then** the rebase resolves to the branch's version of `feature.json` and continues without halt.
2. **Given** a PR whose only conflicts against the new main are `.specify/feature.json` and `CLAUDE.md`, **When** the orchestrator rebases it, **Then** both files resolve to the branch's version and the rebase continues.
3. **Given** a PR whose conflicts include any file outside the auto-resolve list (for example a real conflict on source code or on `constitution.md`), **When** the orchestrator rebases it, **Then** the rebase halts and the workflow surfaces a signal-blocked operator gate before doing anything irreversible.

---

### User Story 3 — Real conflicts halt safely with an operator-resumable gate (Priority: P3)

When a PR has a genuine conflict against the new main — code that actually overlaps, or a non-pointer file that two branches both modified — the orchestrator must not guess. It pauses the workflow at that exact point, leaves the worktree in its conflicted state for the operator to inspect, and waits for an explicit signal from the operator to either resume (after the operator resolves and pushes the fix) or abort (skip this PR, halt the queue).

**Why this priority**: Without this, US2's auto-resolution becomes unsafe — there's no clear boundary between "trivial known-safe conflict" and "real overlap" without operator inspection. This story makes the boundary explicit and gives the operator a clean recovery path.

**Independent Test**: Submit a queue where one PR has a real conflict on a source file. The workflow halts at that PR with a clear pause indication. The operator inspects the conflict, resolves it manually on the feature branch, force-pushes, then signals the workflow to resume. The workflow continues from where it paused; no PRs before or after are double-processed.

**Acceptance Scenarios**:

1. **Given** a PR with a real (non-pointer) conflict against main, **When** the orchestrator attempts rebase, **Then** the workflow enters a paused state, emits a "needs operator" telemetry event identifying the conflicted files, and does not attempt merge.
2. **Given** a paused workflow waiting on operator resolution, **When** the operator signals "resume" after pushing a clean rebase to the branch, **Then** the workflow re-checks mergeability, waits for CI, and proceeds to merge.
3. **Given** a paused workflow waiting on operator resolution, **When** the operator signals "abort this PR," **Then** the workflow records this PR as skipped, halts the rest of the queue at this position, and emits a queue summary.

---

### User Story 4 — Governance-class PRs always require human approval (Priority: P3)

PRs that touch the project constitution or strategic documentation must never auto-merge, even when submitted by the operator's own queue. The orchestrator classifies these PRs as governance class and blocks on an explicit human-approval signal before merging, regardless of who submitted the queue or how green the checks are. This honors the no-bypass invariant aligned with spec 092.

**Why this priority**: This is a safety invariant rather than a productivity feature. It prevents the orchestrator from being used to fast-path a constitutional change just because the operator forgot to remove it from a batch. It's a smaller user-visible story than the conflict-handling ones but it's load-bearing for governance integrity.

**Independent Test**: Submit a queue containing a governance-class PR (one touching `.specify/memory/constitution.md` or `docs/strategy/`). The workflow stops at that PR with a clear "approval required" pause regardless of CI state. Only an explicit operator approval signal causes the merge to proceed.

**Acceptance Scenarios**:

1. **Given** a queue containing a PR that modifies `.specify/memory/constitution.md`, **When** the orchestrator processes that queue position, **Then** the workflow blocks on an approval signal even if all checks are green and the operator submitted it themselves.
2. **Given** a queue containing a PR that only modifies files under `docs/strategy/`, **When** the orchestrator processes that queue position, **Then** the workflow classifies it as governance class and blocks on approval.
3. **Given** a paused governance PR, **When** the operator sends an approval signal naming the PR, **Then** the workflow proceeds to merge that PR and continues the queue.

---

### Edge Cases

- **Worker restart mid-queue**: The Temporal substrate must resume the workflow at its last completed activity boundary. No PR should be double-merged, and the queue summary must accurately reflect what landed before vs. after the restart.
- **PR closed or marked draft while queued**: The orchestrator must detect this on mergeability check and halt the queue at that position with a clear reason, not attempt a merge against a closed PR.
- **CI takes longer than the wait window**: The wait-for-checks step has an upper bound; on timeout the workflow pauses for operator input rather than failing the queue silently.
- **Force-push race**: If the branch moved between the orchestrator's rebase and its push (for example because a human or another agent pushed concurrently), the lease-protected push must fail safely without overwriting the other change.
- **Repo without operator credentials**: If a queue entry names a repository where the worker has no `gh` credentials, the workflow halts at that position with a credentials-missing reason rather than failing silently.
- **Policy-class mismatch**: If a PR is submitted as spec-only but actually touches code paths, the workflow halts at that position with a class-mismatch reason and offers the operator either to re-classify or to skip.
- **Concurrent queues touching the same PR**: If two queue submissions both list the same PR, only one workflow merges it; the other detects the already-merged state and records "skipped — already merged" without error.
- **Dependency cycle in submitted queue**: If the operator's queue declares a dependency cycle, the workflow rejects the submission up front with a clear cycle description rather than starting work.

## Requirements *(mandatory)*

### Functional Requirements

**Queue ingestion and validation**

- **FR-001**: System MUST accept a merge queue from the operator as an ordered list of entries, each naming a repository (`{owner}/{name}`), a PR number, and an optional `dependsOn` list referencing earlier entries in the same submission.
- **FR-002**: System MUST validate the submitted queue before starting any work, rejecting submissions with unknown repositories (no credentials), dependency cycles, unparseable entries, or duplicate `(repo, PR)` pairs within the same submission.
- **FR-003**: System MUST treat each submission as an independent execution, allowing concurrent submissions as long as they do not name the same `(repo, PR)` pair.

**Per-PR classification**

- **FR-004**: System MUST classify each PR into exactly one policy class based on the files it touches, its branch name, and its title prefix. The classes are: `governance`, `live-fix`, `spec-only`, `research-docs`, `impl`, and `bookkeeping`.
- **FR-005**: System MUST classify any PR that modifies `.specify/memory/constitution.md` or any file under `docs/strategy/` as `governance` regardless of other heuristics.
- **FR-006**: System MUST use the policy class to determine which checks are required, which gates are triggered, and whether human approval is mandatory.

**Mergeability and rebase handling**

- **FR-007**: System MUST detect when a PR is not currently mergeable (because the base moved, because checks failed, or because conflicts exist) and route the workflow to the appropriate handler.
- **FR-008**: System MUST automatically rebase a PR whose only conflicts are on the auto-resolve list (`.specify/feature.json`, `CLAUDE.md`), taking the branch's version of those files, and continue the workflow without operator intervention.
- **FR-009**: System MUST halt the workflow with a signal-blocked operator gate when a PR has any conflict on a file outside the auto-resolve list, leaving the conflicted state intact for inspection.
- **FR-010**: System MUST perform all rebase work in a disposable workspace isolated from the operator's primary checkout, and clean that workspace up on workflow completion (success or failure).

**Pushing and merging**

- **FR-011**: System MUST use lease-protected force-push when updating a rebased branch, refusing to overwrite the remote if it moved unexpectedly.
- **FR-012**: System MUST squash-merge every PR; other merge styles MUST NOT be used regardless of repository configuration.
- **FR-013**: System MUST delete the source branch of every successfully merged PR.
- **FR-014**: System MUST detect and report PRs that are already merged (because of an earlier concurrent action) and skip them without error.

**Check-wait behavior**

- **FR-015**: System MUST wait for required checks to complete before attempting a merge, with the required set determined by the policy class.
- **FR-016**: System MUST emit a heartbeat at a bounded interval during check-wait so external observers can confirm the workflow is alive.
- **FR-017**: System MUST bound the maximum check-wait duration per PR; on timeout, the workflow MUST pause for operator input rather than fail the queue silently.

**Human gates**

- **FR-018**: System MUST block on an explicit operator approval signal before merging any `governance`-class PR, even when the operator submitted the queue containing it.
- **FR-019**: System MUST never short-circuit a policy gate based on the identity of the queue submitter (no-bypass invariant).
- **FR-020**: System MUST surface paused workflows in a way the operator can discover without prior knowledge of the workflow ID (for example via tick telemetry).

**Queue control flow**

- **FR-021**: System MUST process queue positions in submitted order, honoring declared dependencies, and MUST NOT attempt later positions when an earlier position halts.
- **FR-022**: System MUST allow the operator to resume a halted queue from the halt position by re-submitting the remaining tail.
- **FR-023**: System MUST allow the operator to abort an in-flight queue cleanly via signal, terminating before the next queue position rather than mid-PR.

**Auditing and telemetry**

- **FR-024**: System MUST emit one telemetry event per queue position state transition (submitted, classified, rebased, checks-waiting, paused, merged, halted, skipped) to the existing OTLP sink.
- **FR-025**: System MUST record a per-PR result in the queue summary, including merge SHA on success, halt reason on failure, and policy-class assignment.
- **FR-026**: System MUST be inspectable by an operator via the underlying workflow-engine query surface, exposing current queue position, current state, and last reason.

**Resilience**

- **FR-027**: System MUST resume in-flight queues correctly across worker restarts, without double-merging any PR.
- **FR-028**: System MUST treat the underlying workflow engine as the source of truth for queue state; no separate queue store is maintained.

### Key Entities

- **MergeQueueSubmission** — A single operator-issued request to merge an ordered list of PRs. Carries a submission ID, an ordered list of `QueueEntry` items, and an optional human-readable label. Lives for one workflow execution.
- **QueueEntry** — One position in a submission. Identifies a repository, a PR number, an optional ordered list of `dependsOn` indices, and an optional override for the auto-classified policy class (the override is rejected if it would relax a governance classification).
- **PRSnapshot** — The orchestrator's view of a PR at classification time: title, branch, base, file list, current mergeability state, current check status, current author. Snapshotted at classification and refreshed on each loop iteration to detect drift.
- **PolicyClass** — One of `governance`, `live-fix`, `spec-only`, `research-docs`, `impl`, `bookkeeping`. Each class names a required check set and a gate set.
- **PolicyTable** — The versioned mapping from PR attributes to policy class, plus the per-class required checks and gates. Versioned so a given queue execution always uses one consistent policy table.
- **QueueResult** — The summary produced when a queue execution ends. Lists every queue position with its final state (merged, paused-resumed-merged, skipped, halted), merge SHA where applicable, halt reason where applicable, and overall duration.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can land a 7-PR mixed-class queue (the seven currently waiting: #926, #927, #919, #920, #921, #922, #924) with one submission action and only the operator inputs that the no-bypass policy requires (zero or more approval signals); no manual rebase, push, or merge command is required.
- **SC-002**: A PR whose only rebase conflicts are on the auto-resolve list is merged without human intervention between the upstream-merge that dirtied it and its own merge.
- **SC-003**: A PR with a non-pointer conflict halts the workflow within one rebase activity boundary, leaves the conflicted state intact for inspection, and emits a discoverable paused-state telemetry event within the same boundary.
- **SC-004**: A governance-class PR is never merged in any test scenario that lacks an explicit approval signal, including scenarios where the operator submitted the queue, where checks are green, and where the operator is the sole party.
- **SC-005**: A lease-protected force-push attempt against a remote branch that moved between rebase and push fails with a clear reason and does not overwrite the remote.
- **SC-006**: Simulated worker restart at every activity boundary of a 3-PR queue produces the same final merged set as an uninterrupted run, with no PR merged more than once.
- **SC-007**: Every PR merged through the orchestrator has its source branch deleted; the queue summary reports a delete-count equal to the merged-count.
- **SC-008**: An external observer reading the OTLP stream can reconstruct, for any past queue execution, the sequence of state transitions per queue position and the final outcome — without needing access to the workflow engine.
- **SC-009**: An operator can determine, for any in-flight queue, the current position, current state, and last reason within one query against the workflow-engine inspection surface.
- **SC-010**: Time from queue submission to "all green PRs merged" for a non-conflicting 5-PR queue is dominated by per-PR CI duration (not by orchestrator overhead) — orchestrator-attributable wall-clock overhead is below 10% of the total.

## Assumptions

- **Substrate**: The orchestrator and its task queue named in constitution §7 already exist (Temporal worker registered at the chitin orchestrator entrypoint, single task queue). This spec adds new workflows and activities to that existing worker rather than introducing a new runtime.
- **Worktree substrate**: Spec 070's worktree manager is the substrate for disposable rebase workspaces. The merge orchestrator reuses it.
- **Credentials**: The worker has `gh` CLI credentials for `chitinhq/chitin` already. v1 supports any additional repositories where credentials are already in place; provisioning new credentials is out of scope.
- **Repo conventions**: The repository policy for chitin is squash-merge only and branches must be deleted on merge. Even though the GitHub repo settings permit other merge styles and do not auto-delete branches, the orchestrator enforces both rules unconditionally.
- **Pointer files**: The set of auto-resolved pointer files is exactly `.specify/feature.json` and `CLAUDE.md` for v1. Adding new entries requires an explicit spec amendment because the resolution rule (take the branch's version) only works for files that are by-design per-branch.
- **Policy table location**: The policy table is defined in code for v1 and amended via PRs to the orchestrator. Externalization to a runtime-editable store is explicitly future work.
- **Submission surface**: v1 accepts queues only through an operator CLI subcommand. Label-driven, kanban-driven, and other automation triggers are explicitly future work.
- **Per-class checks**: Each policy class has a fixed required-checks set in v1. Per-PR check overrides are not supported.
- **Notifications**: Discord notifications (via the existing notifier from spec 080, when enabled) are sent on submission, on each pause that needs operator action, and on queue completion. Per-state-transition notifications are not sent; the OTLP stream covers that need.
- **Cross-repo queues**: A single submission may include PRs across multiple repositories. Each PR is processed against its own repository independently; cross-repo CI orchestration (such as waiting for PR A's effect on repo B's CI) is explicitly not modeled.
- **Concurrent submissions**: Two queues may run concurrently as long as they do not share any `(repo, PR)` pair. If they do, the workflow that reaches the merge step first wins; the other detects already-merged state and records skip.
- **Resume after halt**: The supported recovery path after a halt is "operator fixes the PR locally and re-submits the remaining queue tail." The orchestrator does not attempt to monkey-patch a halted workflow back into motion.
- **Audit**: The orchestrator's workflow history (preserved by Temporal) is the system-of-record for what was attempted, what merged, when, and by whom. No separate audit log is maintained.
