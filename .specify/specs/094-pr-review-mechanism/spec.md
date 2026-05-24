# Feature Specification: PR Review Mechanism

**Feature Branch**: `feat/094-pr-review-mechanism`

**Created**: 2026-05-23

**Status**: Draft

**Input**: User description: "A unified, deterministic, orchestrator-driven PR review mechanism for all pull request classes — code and spec alike. Two primary reviewer drivers run in parallel; on disagreement, a class-routed arbiter (operator for governance and spec-only; a third machine driver for impl/live-fix/bookkeeping/research-docs) breaks the tie. Reviewers emit a structured verdict (four enum values plus concerns/recommendations/blockers lists). The merge orchestrator (spec 093) gates merge on the review workflow's pass/halt decision. Honors the constitution §7 mandate to deterministically orchestrate every multi-step flow."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Both primaries agree to approve; merge proceeds without arbiter (Priority: P1)

The operator submits a pull request through the merge queue. The merge orchestrator classifies the PR, sees that the policy table requires dialectic review, and spawns a review workflow. Two primary reviewer drivers are dispatched in parallel; each reads the PR diff and any in-repo spec artifacts the PR is bound to, then returns a structured verdict. Both return any approve-shaped verdict. The dialectic short-circuits — no arbiter is dispatched — and the merge proceeds with zero operator action.

**Why this priority**: This is the dominant case and the MVP. Most well-formed PRs produce agreeing primary verdicts. Without this happy path working end-to-end, the mechanism delivers no value; every other story is a recovery branch.

**Independent Test**: Open a small clean PR (no obvious quality issues). Submit through the merge orchestrator with the policy table set to require dialectic review for the PR's class. Confirm both primary reviewer drivers run in parallel; confirm both return approve-shaped verdicts; confirm no arbiter is dispatched; confirm merge happens.

**Acceptance Scenarios**:

1. **Given** a PR queued under a policy that requires dialectic review, **When** the merge workflow reaches the review gate, **Then** exactly two primary reviewer drivers are dispatched in parallel within the same workflow tick, and the arbiter is not yet engaged.
2. **Given** both primary reviewers return any approve-shaped verdict (either `approve` or `approve-with-comments`) within their per-reviewer time bound, **When** the dialectic aggregator runs, **Then** the gate returns "passed" without dispatching the arbiter and the merge proceeds.
3. **Given** an external observer queries the OTLP telemetry stream after the merge, **When** they reconstruct the review chain for this PR, **Then** they find exactly two reviewer invocations (no arbiter) with their driver IDs, structured-field content hashes, and elapsed times.

---

### User Story 2 — Primaries disagree; class-routed arbiter breaks the tie (Priority: P1)

The two primary reviewers return contradictory verdicts (one approve-shaped, one `request-changes`). The dialectic aggregator detects the disagreement and routes the arbiter dispatch by the PR's class: governance and spec-only route to the operator (engaged via the operator-arbiter surface with structured verdict capture); impl, live-fix, bookkeeping, and research-docs route to a third reviewer-tagged machine driver. The arbiter returns a verdict and that verdict is final — any approve-shaped arbiter verdict proceeds to merge; `request-changes` halts; `abstain` halts and escalates.

**Why this priority**: This is the second load-bearing case and the formalization of the spec-92 multi-agent pattern. Without it, every disagreement halts the queue; with it, machine arbitration resolves what machines can settle and only high-stakes classes escalate to the operator.

**Independent Test**: Submit a PR designed (or fixture-injected) to produce mixed primary verdicts. For a spec-only PR confirm the operator arbiter surface is engaged; for an impl PR confirm a third machine driver is dispatched.

**Acceptance Scenarios**:

1. **Given** primary reviewers return one approve-shaped verdict and one `request-changes` on a spec-only PR, **When** the dialectic detects the mismatch, **Then** the workflow dispatches the operator as arbiter and waits indefinitely on the operator-arbiter surface.
2. **Given** the same primary disagreement on an impl-class PR (when machine arbitration is configured), **When** the dialectic detects the mismatch, **Then** the workflow dispatches a third reviewer-tagged machine driver (excluded from primary slots and from the author identity) and waits on the per-machine-reviewer time bound.
3. **Given** the arbiter returns any approve-shaped verdict, **When** the aggregator completes, **Then** the gate returns "passed" and merge proceeds; the arbiter's verdict supersedes the primaries' disagreement.
4. **Given** the arbiter returns `request-changes`, **When** the aggregator completes, **Then** the gate returns "blocked" with a reason naming the arbiter's blockers, and the merge halts on the standard signal-blocked surface.

---

### User Story 3 — Both primaries reject; halt without arbiter (Priority: P2)

When both primary reviewers independently return `request-changes`, there is no disagreement to arbitrate. The gate halts immediately without dispatching an arbiter. From the halted state, the operator either addresses the listed blockers and signals re-review, or — for non-governance classes — signals an explicit override.

**Why this priority**: Important for cycle efficiency. Dispatching an arbiter when primaries already agree on a block wastes cycles and muddies telemetry. Important enough to spec explicitly so an implementation doesn't accidentally dispatch.

**Independent Test**: Submit a PR designed to produce two `request-changes` verdicts. Confirm no arbiter activity is dispatched; gate returns halted; signals can resolve.

**Acceptance Scenarios**:

1. **Given** both primary reviewers return `request-changes`, **When** the aggregator runs, **Then** the gate returns "blocked" with reason "both primaries request-changes" and no arbiter dispatch is recorded.
2. **Given** the workflow is in this halted state, **When** the operator addresses the listed blockers and sends a re-review signal, **Then** the workflow refreshes the PR snapshot, dispatches a fresh pair of primary reviewers, and re-runs the dialectic from scratch.
3. **Given** the workflow is in this halted state on a non-governance class, **When** the operator sends an override-review signal with a reason, **Then** the workflow records the override in telemetry and proceeds to merge.
4. **Given** the workflow is in this halted state on a governance class, **When** the operator sends an override-review signal, **Then** the signal is rejected with a structured error and the gate remains blocked; the only path to merge is fresh affirmative review.

---

### User Story 4 — Author's driver excluded from every role; pool shortfall halts cleanly (Priority: P2)

Reviewer selection excludes the PR author's driver from every role (primary or arbiter). If the eligible pool after exclusion can't fill the required slots, the workflow halts at selection with a clearly named shortfall — it does not proceed with a degraded gate.

**Why this priority**: Structural integrity invariant aligned with spec 092's no-driver-bypass invariant. Without this, a driver could approve its own work, defeating the gate. Without the clean-shortfall behavior, the mechanism could silently degrade in ways operators wouldn't notice.

**Independent Test**: Open a PR authored by a driver identifier that maps to a reviewer-tagged driver. Confirm that driver is excluded from selection. With v1's initial pool of two reviewer-tagged drivers, this means only one is eligible — the workflow can't fill two primary slots and halts at selection. Adding a third reviewer-tagged driver later resolves the shortfall on the next run.

**Acceptance Scenarios**:

1. **Given** the PR author identifier maps to a known reviewer-tagged driver, **When** the selection activity runs, **Then** the returned eligible-driver list excludes the author's driver from every role.
2. **Given** the eligible pool after exclusion is smaller than the count required for the role being filled, **When** the workflow attempts dispatch, **Then** it halts at selection with a reason naming counts (need N, have M after exclusions).
3. **Given** the PR author identifier does NOT map to any known driver (e.g., the operator authored via the web UI, or a human contributor), **When** the selection runs, **Then** no driver is excluded and the full reviewer pool is eligible.
4. **Given** the arbiter role would require a third machine driver but only the two primaries are tagged after author exclusion, **When** the workflow reaches arbiter dispatch, **Then** the workflow halts with reason "arbiter pool exhausted" and surfaces to the operator for escalation.

---

### User Story 5 — Single reviewer failure does not block the gate (Priority: P3)

A primary reviewer driver fails (time bound exceeded, crash, malformed output). The dialectic does not stall on the failure — the surviving primary's verdict is recorded, and the workflow dispatches the arbiter to compensate (because a missing verdict is undecidable, not implicitly approve). If the arbiter then completes, the gate decision uses (surviving primary verdict, arbiter verdict) per standard aggregation. If both primaries fail, the workflow halts at the arbiter dispatch boundary with a clear reason.

**Why this priority**: Resilience to driver flakiness is important for trust but is degenerate on the happy path. Without this, any flaky reviewer becomes a single point of failure for every PR.

**Independent Test**: Configure a test scenario where one reviewer-tagged driver always times out. Submit a PR. Confirm the timing-out reviewer is recorded as failed; confirm the arbiter is dispatched in response; confirm the gate is decided from (surviving primary, arbiter).

**Acceptance Scenarios**:

1. **Given** two primaries dispatched and one exceeds the per-reviewer time bound, **When** the surviving primary completes, **Then** the workflow records the timeout as a failed outcome and dispatches the arbiter (treating missing primary as undecidable disagreement).
2. **Given** the arbiter then completes with any verdict, **When** the aggregator runs, **Then** the gate decision is computed from (surviving primary verdict, arbiter verdict) using the standard aggregation rules.
3. **Given** both primaries fail, **When** the workflow detects both failures, **Then** it halts at arbiter dispatch with reason "all primaries failed" and surfaces for operator escalation.

---

### Edge Cases

- **Empty eligible pool**: The author's driver is the only reviewer-tagged driver. Halt at selection with a named shortfall.
- **Arbiter pool exhausted**: The class policy requires a machine arbiter but no third reviewer-tagged driver exists after excluding both primaries and the author. Halt at arbiter dispatch with a named shortfall.
- **Malformed reviewer output**: The driver's output doesn't conform to the structured-verdict shape. Treated as a failed outcome with a malformation reason; recorded in telemetry; dialectic proceeds as if the reviewer failed.
- **Verdict violates structured-field invariants** (e.g., `approve` with non-empty `blockers`): Treated as malformed; failed outcome.
- **PR diff changes mid-review**: Each reviewer sees the PR snapshot captured when its activity started; later changes do not invalidate in-flight reviews. If the PR head moves after the gate passes but before merge, the merge workflow re-checks mergeability and may trigger another full review run depending on policy (the re-review trigger logic lives in spec 093's merge workflow, not here).
- **PR closed or marked draft during review**: Detected at the next mergeability check; the review workflow terminates with reason "PR closed or marked draft during review."
- **Both primaries abstain**: No decisive verdicts; dispatch the arbiter to break the tie. If the arbiter also abstains, halt and escalate to the operator regardless of class.
- **Operator-as-arbiter has not responded within the heartbeat window**: The workflow continues to wait (no operator-arbiter time bound). A repeat Discord notification is sent at the heartbeat boundary so the operator knows attention is still needed.
- **Operator override attempted on governance class**: Rejected with a structured error.
- **Re-review signal arrives during an in-flight review**: Ignored. The signal applies only to halted or blocked states.
- **Disagreement between `approve` and `approve-with-comments`**: NOT a disagreement. Both are approve-shaped; the gate passes without dispatching the arbiter.

## Requirements *(mandatory)*

### Functional Requirements

**Driver capability and review-mode contract**

- **FR-001**: System MUST extend the driver capability metadata (spec 075) so that a driver can declare itself eligible to act as a reviewer.
- **FR-002**: System MUST require any driver declaring the reviewer capability to also conform to a review-mode contract: an input that accepts the PR diff + in-repo spec artifacts + the PR's policy class hint, and an output conforming to the structured-verdict shape defined in FR-013.
- **FR-003**: System MUST allow each reviewer-tagged driver to own its review-mode prompt content; the orchestrator does NOT prescribe prompt text, only the input/output contract.

**Reviewer pool and selection**

- **FR-004**: System MUST select reviewer candidates from the registered driver pool by matching the reviewer capability tag.
- **FR-005**: System MUST exclude the PR author's driver from the eligible pool for every role in the same review workflow execution (the no-self-review rule, aligned with spec 092's no-driver-bypass invariant).
- **FR-006**: System MUST exclude drivers that fail a health check at selection time, even when they declare the reviewer capability.
- **FR-007**: System MUST verify the eligible pool can fill the required roles for the PR's policy class before dispatching; if it cannot, halt at selection with a reason that names the shortfall counts (e.g., "need N, have M after exclusions").

**Dialectic gate**

- **FR-008**: System MUST dispatch exactly two primary reviewers in parallel within a single workflow tick when review is required (no sequential primary dispatch).
- **FR-009**: System MUST classify the combined primary outcome using these rules, applied in order:
   - Both verdicts in {`approve`, `approve-with-comments`} → agreement-to-pass (no arbiter).
   - Both verdicts equal to `request-changes` → agreement-to-halt (no arbiter).
   - Any other combination (including any abstain or any failed outcome) → dispatch arbiter.
- **FR-010**: System MUST short-circuit the gate to "passed" when both primaries agree to pass, without dispatching the arbiter.
- **FR-011**: System MUST short-circuit the gate to "blocked" when both primaries agree to halt, without dispatching the arbiter.
- **FR-012**: System MUST dispatch exactly one arbiter on any other combined primary outcome, with the arbiter's verdict being final for the gate decision.

**Structured verdict and validation**

- **FR-013**: System MUST require every reviewer (primary, arbiter, machine or operator) to emit a structured verdict object containing exactly one of four `verdict` enum values (`approve`, `approve-with-comments`, `request-changes`, `abstain`) plus three list fields (`concerns`, `recommendations`, `blockers`) of free-text strings, plus an optional `reason` string used only for abstain.
- **FR-014**: System MUST validate the structured verdict on receipt and treat structurally-invalid verdicts as failed outcomes:
   - `verdict=request-changes` ⇒ `blockers` MUST be non-empty.
   - `verdict=approve` ⇒ `blockers` MUST be empty.
   - `verdict=approve-with-comments` ⇒ `blockers` MUST be empty AND at least one of `concerns`/`recommendations` MUST be non-empty.
   - `verdict=abstain` ⇒ all three lists MUST be empty (the optional `reason` field may be set).
- **FR-015**: System MUST treat a reviewer's verdict as immutable for the review invocation that produced it; re-reviews are new invocations with new verdicts.

**Class-tunable arbiter routing**

- **FR-016**: System MUST consult the per-class policy to determine the arbiter type for that class: one of `operator` or `machine`.
- **FR-017**: System MUST dispatch the operator as arbiter when the per-class policy specifies `operator`, via the operator-arbiter surface defined in FR-018.
- **FR-018**: System MUST treat operator-arbiter verdicts as structurally identical to machine-arbiter verdicts (same schema as FR-013), even though they are collected via a different surface. The specific surface mechanism (chat prompt, structured PR comment, dedicated operator tool, etc.) is an implementation choice resolved during planning, but the verdict captured in workflow history must conform to the same shape.
- **FR-019**: System MUST dispatch a single third reviewer-tagged driver as arbiter (excluded from both primary slots and from the author's identity) when the per-class policy specifies `machine`.
- **FR-020**: System MUST enforce the class-policy invariant: the `governance` and `spec-only` classes MUST specify `operator` arbiter; the `impl`, `live-fix`, `bookkeeping`, and `research-docs` classes MAY specify either but default to `machine` (subject to the v1 known limitation in Assumptions).

**Operator interaction**

- **FR-021**: System MUST accept a re-review signal addressed to a merge workflow whose review gate is blocked or halted; on receipt, the workflow refreshes the PR snapshot and spawns a fresh review workflow with new dispatches.
- **FR-022**: System MUST accept an override-review signal addressed to a merge workflow whose review gate is blocked; on receipt the workflow records the override in telemetry with the operator-provided reason and treats the gate as passed.
- **FR-023**: System MUST reject override-review signals for governance-class PRs and return a structured error to the signal sender; governance's only path to merge is fresh affirmative review.
- **FR-024**: System MUST expose the current review state via a queryable surface so the operator can see — for any in-flight or halted PR — which roles have been dispatched (primary 1, primary 2, arbiter if engaged), what verdicts have arrived, and what the current gate status is.
- **FR-025**: System MUST send a notification (via the existing Discord notifier from spec 080, when enabled) when an operator-as-arbiter dispatch needs operator attention; if no operator verdict has arrived after a fixed heartbeat interval, the notification is re-sent so the operator knows attention is still required.

**Resilience and time bounds**

- **FR-026**: System MUST set a per-machine-reviewer time bound (recommended 30 minutes) that bounds the time any single machine reviewer can take.
- **FR-027**: System MUST NOT enforce a time bound on operator-as-arbiter dispatch; the workflow waits indefinitely. The heartbeat in FR-029 is for visibility, not for terminating the workflow.
- **FR-028**: System MUST treat any machine reviewer failure (time bound exceeded, error, malformed verdict) as a failed outcome that triggers arbitration if the failure occurs on a primary slot, or halts the workflow with operator escalation if the failure occurs on the arbiter slot.
- **FR-029**: System MUST emit a heartbeat telemetry event at a recommended two-hour boundary indicating the workflow is still running; this does NOT terminate the workflow.
- **FR-030**: System MUST be resilient to worker restart mid-review: the underlying workflow engine resumes the workflow at the last activity boundary; in-flight reviewer activities either complete normally or are retried per their activity options.

**Audit and integrity**

- **FR-031**: System MUST honor the no-bypass invariant: the PR submitter's identity does NOT exempt the review gate; the same gate runs regardless of who submitted the merge queue or how the PR was authored. This applies to ad-hoc operator-initiated PRs (e.g., the operator asking a driver to draft a spec) — origin does not exempt the gate.
- **FR-032**: System MUST emit one telemetry event per reviewer invocation containing: the reviewer driver identifier (or "operator" for operator-arbiter), the role (`primary` or `arbiter`), the verdict enum value, content hashes of the structured-field lists, the elapsed time, and the PR snapshot identifier. Raw content text is recorded in workflow history (FR-033), not in the streamed telemetry event.
- **FR-033**: System MUST treat the underlying workflow engine's history as the system of record for the full review chain, including the raw text of every blocker, concern, and recommendation; no separate review store is maintained.
- **FR-034**: System MUST record the raw structured-verdict content immutably in workflow history; verdicts cannot be retroactively edited.

**Integration with the merge orchestrator (spec 093)**

- **FR-035**: System MUST integrate into spec 093's merge orchestrator such that a class's `review_required` flag (added in a spec 093 v1.x amendment) causes the merge workflow to spawn a review workflow before attempting merge.
- **FR-036**: System MUST allow the merge workflow's existing wait-for-checks step (spec 093 FR-015) to run concurrently with the review gate, so CI and review can complete in parallel — review does NOT serialize after CI.
- **FR-037**: System MUST consume from spec 093's policy table, per class, two fields added in a spec 093 v1.x amendment: `review_required` (bool) and `arbiter_type` (enum: `operator` or `machine`).

### Key Entities

- **ReviewerDriver** — A driver from the spec 075 registry that declares the reviewer capability tag and conforms to the review-mode contract (FR-002). Identified by the driver's existing driver ID (e.g., `hermes` for Ares, `openclaw` for Clawta).
- **ReviewerPool** — The set of reviewer drivers currently eligible for a given PR after applying the no-self-review filter (FR-005) and the health filter (FR-006).
- **ReviewerRole** — One of two primary slots or one arbiter slot. The primary slots are always filled by machine drivers in v1. The arbiter slot is filled by the operator or a machine driver, per the class policy (FR-016).
- **ReviewerInvocation** — A single dispatch of a reviewer for a specific role. Carries an invocation ID, driver ID (or `operator`), role, start time, and terminal outcome (verdict or failure).
- **StructuredVerdict** — The schema-validated output of a completed reviewer invocation: `verdict` enum + `concerns` list + `recommendations` list + `blockers` list + optional `reason` (abstain only). Immutable once recorded.
- **DialecticResult** — The aggregate of all per-reviewer outcomes for one review workflow execution, plus the gate decision derived from FR-009 through FR-012.
- **ReviewGateDecision** — One of `passed`, `blocked`, or `halted`, each with a reason field and a flag indicating whether the arbiter was engaged. Consumed by the parent merge workflow.
- **OperatorArbiterDispatch** — A specialization of ReviewerInvocation where the driver field is `operator`; collected via the operator-arbiter surface. Produces the same StructuredVerdict shape.
- **PRReviewSnapshot** — The view of the PR provided to reviewer drivers at invocation start: PR metadata + file diff + in-repo spec artifacts that the PR is bound to. Captured at the moment the activity starts; later PR changes do not invalidate in-flight reviews.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001 (happy path)**: An operator can land a PR through the merge orchestrator with dialectic review enabled; if both primary drivers return any approve-shaped verdict, the merge happens with zero operator inputs after submission and the arbiter is never dispatched.
- **SC-002 (operator arbiter on spec-only disagreement)**: When primaries disagree on a spec-only PR, the workflow dispatches the operator as arbiter, surfaces a notification to the operator, waits indefinitely for the structured verdict, and proceeds based on that verdict.
- **SC-003 (machine arbiter on impl disagreement)**: When primaries disagree on an impl-class PR for which the policy specifies a machine arbiter, the workflow dispatches a third reviewer-tagged machine driver, the arbiter completes within the per-reviewer time bound, and the gate decision uses the arbiter's verdict. (See assumption about v1 operational degeneracy when only two reviewer-tagged drivers exist.)
- **SC-004 (both reject → halt without arbiter)**: When both primaries return `request-changes`, the workflow halts immediately without dispatching the arbiter; telemetry records exactly two reviewer invocations and no arbiter dispatch.
- **SC-005 (no-self-review enforced)**: The PR author's driver appears in zero reviewer-invocation events for that PR; verified by submitting a PR authored by a reviewer-tagged driver and confirming that driver appears nowhere in the gate's invocation chain.
- **SC-006 (governance no override)**: A governance-class PR with a blocked review gate cannot be passed by an override-review signal; the signal is rejected with a structured error and the gate remains blocked.
- **SC-007 (failure isolation)**: When one primary fails and the surviving primary returns any approve-shaped verdict, the workflow dispatches the arbiter (treating missing-primary as undecidable) and the arbiter's verdict drives the gate.
- **SC-008 (extensibility = zero workflow change)**: Adding a new reviewer-tagged driver (declaring the capability and shipping a review-mode prompt) makes it eligible for selection on the next review workflow run with no review-workflow code change.
- **SC-009 (parallel dispatch verified)**: The wall-clock duration of a two-primary dialectic with no arbiter is dominated by the slower of the two primaries (within 10%), not by their sum.
- **SC-010 (telemetry reconstruction)**: An external observer reading the telemetry stream can reconstruct, for any past PR, the full dialectic chain — which two primaries dispatched, whether the arbiter engaged, who arbitrated, what verdict each role returned, what structured-field content hashes each carried, what total wall-clock the gate consumed.
- **SC-011 (re-review preserves audit, replaces gate)**: An operator can re-trigger the review on a halted PR via a single signal; the new dialectic produces fresh verdicts that supersede the old gate decision; the audit trail preserves both the original verdicts AND the new ones.
- **SC-012 (override recorded explicitly)**: For a non-governance PR whose review gate is blocked due to a `request-changes` outcome, an operator override-review signal allows merge to proceed; telemetry records both the original blocking verdict and the override with the operator-provided reason.

## Assumptions

- **Merge orchestrator substrate exists**: The merge orchestrator from spec 093 exists and exposes the integration points named in FR-035 through FR-037. This spec does NOT re-spec the merge orchestrator. The spec 093 policy table will be amended in a v1.x change set after spec 094 ratifies to add the `review_required` and `arbiter_type` fields.
- **Driver registry exists**: The driver registry from spec 075 exists with capability metadata. This spec extends the metadata with the reviewer capability tag and the review-mode contract requirement; it does NOT introduce a parallel registry.
- **Driver selection exists**: The selection activity from spec 076 exists and can pick drivers by capability. This spec either extends that activity with a reviewer-tagged filter and no-self-review exclusion or wraps it in a new selection activity that delegates to it — the choice is an implementation decision deferred to planning.
- **Initial reviewer-tagged drivers**: At v1 ship, the qualified drivers are `hermes` (Ares) and `openclaw` (Clawta). Both author their own review-mode prompt templates per FR-003. The dialectic primary slots can both be filled by these two.
- **v1 known limitation — machine arbiter not operationally viable**: With only two reviewer-tagged drivers at v1 ship, machine arbitration cannot fill the third role after the two primaries are placed. The class-tunable mechanism (FR-016 through FR-020) is implemented but operationally degenerate for impl, live-fix, bookkeeping, and research-docs until a third reviewer-tagged driver is added. At v1 ship, all class policies route to `operator` arbiter. This is a documented and accepted trade-off — adding a third reviewer-tagged driver (codex, copilot, or another) is a follow-up that does not require any review-workflow change (SC-008).
- **Author identity mapping**: The PR author identifier can be mapped to a driver ID for the FR-005 exclusion check. For author IDs that don't map (operator-authored, human contributor, generic bot), no driver is excluded.
- **Reviewer context scope**: Reviewer drivers receive the PR diff + in-repo spec artifacts (spec.md, plan.md, contracts/*, data-model.md, research.md when present) + the PR's classified policy class as a hint. They do NOT receive access to the Obsidian vault, external systems, or the history of other PRs. If broader context is needed, the PR author includes it in the PR body.
- **Operator-arbiter surface deferred to planning**: This spec commits to "operator emits a StructuredVerdict via some surface" but does NOT lock the specific surface. Planning will pick among candidates such as a Discord-thread structured prompt, a structured GitHub PR comment that the orchestrator parses, or a dedicated operator-facing tool. Whichever surface is chosen, the operator's input is captured as a StructuredVerdict in workflow history per FR-018.
- **Per-machine-reviewer time bound**: 30 minutes is the recommended upper bound on a single machine reviewer dispatch. Most reviews are expected to complete in 1–10 minutes; the long bound accommodates rare deep-dive cases.
- **No operator-arbiter time bound**: Operator-as-arbiter waits indefinitely (FR-027). The two-hour heartbeat (FR-029) exists for telemetry visibility and re-notification, not for terminating the workflow.
- **Verdict schema versioning**: The StructuredVerdict schema (verdict enum + three lists + optional reason) is implicitly version 1. Future schema changes are either backwards-compatible with prior verdict records or carry an explicit schema version field added at that time.
- **Notification scope**: A notification is sent (via spec 080's Discord notifier, when enabled) on (a) operator-as-arbiter dispatch (FR-025), (b) the heartbeat boundary if the operator has not yet responded (FR-025), and (c) gate halt for any class via the spec 080 standard halt-notification flow. Per-machine-reviewer dispatch events are recorded in telemetry only, not in chat.
- **Bootstrap of spec 094 itself**: This spec is a spec PR. Once spec 094 ships and spec 093's policy table is amended to require dialectic review for spec-only PRs, the new mechanism applies to subsequent spec PRs. The spec 094 PR ITSELF cannot use its own mechanism (chicken-and-egg). Spec 094's merge bootstraps via the constitution §7 ad-hoc carve-out: operator-driven multi-agent ratification (the same pattern that landed spec 092). This is a one-time exemption, not a recurring carve-out.
- **Origin-blind gate**: Per FR-031, the review gate runs regardless of how a PR was authored. Ad-hoc operator-initiated PRs (the operator asking a driver to draft a spec) follow the same dialectic as queue-submitted PRs. There is no "ad-hoc lane" that bypasses review.
