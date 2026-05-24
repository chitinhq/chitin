# Quickstart: PR Review Mechanism

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-05-23

## Purpose

Operator-facing recipes that demonstrate each Success Criterion (SC-001 through SC-012) against the implemented workflow. Each recipe is a self-contained sequence the operator can run and observe; together they form the manual verification surface that pairs with the unit and integration tests.

Prerequisites:

- `chitin-orchestrator` worker running (the same worker that hosts spec 093's `PRMergeWorkflow`).
- Spec 093 v1.1.0 amendment applied (policy table has `review_required` and `arbiter_type` columns).
- Spec 075 driver registry has `hermes` and `openclaw` entries declaring `capabilities: [reviewer]` and a `review_mode` block.
- A test repo with at least two test PRs to exercise.

CLI assumed: `chitin-orchestrator merge-queue submit <yaml>` from spec 093.

---

## Recipe SC-001 — Happy path (both primaries approve)

**Claim**: An operator can land a PR through the merge orchestrator with dialectic review enabled; if both primaries approve, the merge happens with zero operator input and the arbiter is never dispatched.

```bash
# Submit a small, clean spec-only PR through the merge queue.
chitin-orchestrator merge-queue submit <<'YAML'
queue:
  - repo: chitinhq/chitin
    pr: 999  # a small spec-typo-fix PR designed to pass review easily
YAML

# Observe (in a separate terminal):
chitin-orchestrator merge-queue inspect <workflow-id>
```

**Expected**: The inspect output shows two `ReviewerInvocation` records with role=primary, both with verdict=`approve` or `approve-with-comments`. No third invocation (no arbiter). The gate decision is `passed` with reason "both primaries approve". The PR is merged.

**Pass check**: `chitin-orchestrator merge-queue events <workflow-id> --filter review` shows exactly two reviewer-invocation events, both approve-shaped, and one gate-decision event with state=passed.

---

## Recipe SC-002 — Operator arbiter on spec-only disagreement

**Claim**: When primaries disagree on a spec-only PR, the workflow dispatches the operator as arbiter via the GitHub-comment surface, waits indefinitely, and proceeds on receipt of a valid verdict.

```bash
# Submit a spec-only PR designed (or fixture-injected) to produce mixed primary verdicts.
chitin-orchestrator merge-queue submit <<'YAML'
queue:
  - repo: chitinhq/chitin
    pr: <pr-number-with-mixed-verdicts>
YAML
```

**Expected sequence**:

1. Two reviewer-invocation events with mixed verdicts.
2. A `notification.kind = "operator-arbiter.dispatch"` event with a Discord ping containing the PR link.
3. A new GitHub comment posted on the PR with the operator-arbiter prompt template (see [contracts/operator-arbiter-surface.md](./contracts/operator-arbiter-surface.md)).
4. Workflow waits.

**Operator action**: Reply to the prompt comment with a fenced YAML block containing a valid `# operator-arbiter-verdict`. Example:

````markdown
```yaml
# operator-arbiter-verdict
verdict: approve
concerns: []
recommendations: []
blockers: []
```
````

**Pass check**: After the comment lands, the events stream shows a third reviewer-invocation event with `driver_id="operator"`, `role="arbiter"`, verdict matching the YAML. Gate decision derives from the arbiter; if approve-shaped, the PR is merged.

---

## Recipe SC-003 — Machine arbiter on impl disagreement

**Claim**: When primaries disagree on an impl-class PR for which the policy specifies `arbiter_type: machine`, the workflow dispatches a third reviewer-tagged machine driver; the gate decision uses the arbiter's verdict.

**Note**: At v1.1.0 ship, only `hermes` and `openclaw` are reviewer-tagged drivers, so this recipe is operationally degenerate — the workflow halts with reason "arbiter pool exhausted" (per Acceptance Scenario 4.4) when impl class disagreement occurs and `arbiter_type: machine`. To exercise SC-003 affirmatively, the policy table must be temporarily mutated to set `arbiter_type: operator` for impl (the v1.1.0 default), OR a third reviewer-tagged driver must be added first.

When a third driver IS available (post-v1.1):

```bash
chitin-orchestrator merge-queue submit <<'YAML'
queue:
  - repo: chitinhq/chitin
    pr: <impl-pr-with-mixed-verdicts>
YAML
```

**Expected**: Three reviewer-invocation events — primary1, primary2, arbiter (all `driver_id` in {hermes, openclaw, codex/copilot/gemini/local-llm — whichever is the third}). No `operator-arbiter.dispatch` event.

**Pass check**: The arbiter `driver_id` is NOT the PR's author-mapped driver and NOT either of the two primaries. The gate decision derives from the arbiter's verdict.

---

## Recipe SC-004 — Both primaries reject (halt without arbiter)

**Claim**: When both primaries return `request-changes`, the workflow halts immediately without dispatching the arbiter.

```bash
# Submit a PR designed to produce two request-changes verdicts (e.g., an impl PR with no tests and clear policy violations).
chitin-orchestrator merge-queue submit <<'YAML'
queue:
  - repo: chitinhq/chitin
    pr: <pr-designed-to-fail-review>
YAML
```

**Expected**: Exactly two reviewer-invocation events, both `verdict="request-changes"` with non-empty blockers. No third invocation. Gate decision = `blocked`, reason = "both primaries request-changes". The merge halts.

**Pass check**: `chitin-orchestrator merge-queue events <workflow-id> --filter review` shows exactly two events; arbiter-dispatch count = 0.

**Optional recovery**:

```bash
# Operator addresses both blockers in commits, then signals re-review:
chitin-orchestrator merge-queue signal <workflow-id> re-review --reason "addressed blockers in commits abc..def"
```

This spawns a fresh `PRReviewWorkflow` child with a new snapshot. See Recipe SC-011 for the audit-preservation check.

---

## Recipe SC-005 — No-self-review enforced

**Claim**: The PR author's driver appears in zero reviewer-invocation events for that PR.

**Setup**: Open a PR authored by a reviewer-tagged driver. The simplest way is to have `hermes` open a spec PR (its driver ID is `hermes`, git_identity is `hermes-bot`).

```bash
# Hermes opens a spec PR via its normal authoring flow (Discord → spec PR).
# Then submit it to the merge queue:
chitin-orchestrator merge-queue submit <<'YAML'
queue:
  - repo: chitinhq/chitin
    pr: <pr-authored-by-hermes>
YAML
```

**Expected at v1.1.0**: The selection activity excludes `hermes` from the eligible pool. With only `openclaw` remaining, the pool size (1) is less than the required count (2 primaries), so the workflow halts at selection with reason "need 2 primaries, have 1 after exclusions" (per Acceptance Scenario 4.2).

**Pass check**: `chitin-orchestrator merge-queue events <workflow-id> --filter selection` shows the shortfall event with `excluded_author="hermes"` and `eligible_count=1`. Zero reviewer-invocation events exist for `hermes`.

When a third reviewer-tagged driver IS available (post-v1.1):

```bash
# Same submission, but now eligible_count = 2 (openclaw + third).
```

**Expected**: Two reviewer invocations, neither with `driver_id="hermes"`. Arbiter (if dispatched) also not `hermes`.

---

## Recipe SC-006 — Governance no override

**Claim**: A governance-class PR with a blocked review gate cannot be passed by an override-review signal.

```bash
# Submit a governance PR (e.g., a constitution amendment) that fails review.
chitin-orchestrator merge-queue submit <<'YAML'
queue:
  - repo: chitinhq/chitin
    pr: <governance-pr-with-blocking-verdicts>
YAML

# Wait for the gate to land on `blocked`. Then attempt override:
chitin-orchestrator merge-queue signal <workflow-id> override-review --reason "operator authority"
```

**Expected**: The signal handler emits `override-review.rejected` with reason `governance class` and returns a structured error to the signal sender. The gate state remains `blocked`. The merge does NOT proceed.

**Pass check**: `chitin-orchestrator merge-queue inspect <workflow-id>` shows gate.state=blocked, override-attempts=1, override-rejected-count=1. The only path to merge is `re-review` with affirmative verdicts.

---

## Recipe SC-007 — Failure isolation

**Claim**: When one primary fails and the surviving primary approves, the workflow dispatches the arbiter and the arbiter's verdict drives the gate.

```bash
# Setup: configure a test driver `flaky-reviewer` declaring CapabilityReviewer
# whose review tool always times out. Or use the standard testsuite fault injector
# in pr_review_test.go for unit-level verification.
```

**Expected (unit-test surface)**:

1. Two primary dispatches.
2. One returns `FailureTimeout` after 30 minutes (or the test-configured shorter bound).
3. The surviving primary returns `verdict=approve`.
4. The aggregator sees `(approve, failure)` which is NOT both-approve-shaped → dispatches arbiter.
5. Arbiter returns a verdict; gate decision derives from arbiter.

**Pass check (in test suite)**: `pr_review_test.go/TestFailureIsolation` covers this. Manual verification at the integration level requires a fixture failing-driver, which is a v1.x extension.

---

## Recipe SC-008 — Extensibility = zero workflow change

**Claim**: Adding a new reviewer-tagged driver makes it eligible for selection on the next review workflow run with no review-workflow code change.

```bash
# Edit the driver registry to add a new entry (or update an existing one):
# .specify/registry/drivers.yaml (or wherever the registry persists):
#   drivers:
#     codex:
#       capabilities: [reviewer]   # NEW
#       review_mode:
#         tool_name: review
#         prompt_template: "..."
#         max_bytes_in: 200000
#       git_identity: codex-bot

# Reload the registry (orchestrator-side):
chitin-orchestrator registry reload

# Submit a fresh PR:
chitin-orchestrator merge-queue submit <<'YAML'
queue:
  - repo: chitinhq/chitin
    pr: <fresh-test-pr>
YAML
```

**Expected**: The selection activity now has three reviewer-tagged drivers in the pool. With author exclusion, at least two remain — fills primaries. If `arbiter_type=machine` on the PR's class, the third slot is also fillable.

**Pass check**: `chitin-orchestrator merge-queue events <workflow-id> --filter selection` shows `eligible_count >= 2 or 3`, depending on author. Zero changes to `pr_review.go` or `select_reviewers.go` were made — only the registry was updated.

---

## Recipe SC-009 — Parallel dispatch verified

**Claim**: The wall-clock duration of a two-primary dialectic with no arbiter is dominated by the slower of the two primaries (within 10%), not by their sum.

```bash
# Configure two reviewer drivers with controllable per-review latency, e.g., a 60s and a 90s response.
# Submit:
chitin-orchestrator merge-queue submit <<'YAML'
queue:
  - repo: chitinhq/chitin
    pr: <fresh-test-pr>
YAML

# Time observation:
chitin-orchestrator merge-queue events <workflow-id> --filter review --format json | jq '.events[] | {kind, ts, driver_id}'
```

**Expected**: The two reviewer-invocation `started` events have timestamps within 1 second of each other (parallel dispatch). The total wall-clock from `dispatch-start` to `gate-decision` is ≤ 99 seconds (the slower primary + 10% headroom), not ~150 seconds.

**Pass check**: `(gate_decision_ts - dispatch_start_ts)` <= 1.1 × max(p1_elapsed, p2_elapsed).

---

## Recipe SC-010 — Telemetry reconstruction

**Claim**: An external observer reading the telemetry stream can reconstruct, for any past PR, the full dialectic chain.

```bash
# After any review workflow completes (any of recipes SC-001 through SC-007), query telemetry:
chitin-orchestrator telemetry query --pr 928 --kinds "review.*"
```

**Expected**: For each reviewer invocation, the telemetry stream contains:

- `review.invocation.started` with workflow_id, invocation_id, driver_id, role, snapshot_hash_ref.
- `review.invocation.completed` with verdict (or failure_kind), elapsed_ms, content hashes for concerns/recommendations/blockers.
- `review.gate.decided` with state, reason, arbiter_engaged.

**Pass check**: From the telemetry alone (no workflow-history access required), an external observer can answer: "which two primaries reviewed PR 928?", "did the arbiter engage?", "what verdict did each role return?", "what total wall-clock did the gate take?" All four answers are present.

Note: raw verdict text is NOT in the telemetry stream — only content hashes are (FR-032). To get raw text, the observer queries workflow history (`chitin-orchestrator merge-queue inspect <workflow-id>`).

---

## Recipe SC-011 — Re-review preserves audit, replaces gate

**Claim**: An operator can re-trigger the review on a halted PR via a single signal; the new dialectic produces fresh verdicts that supersede the old gate decision; the audit trail preserves both.

```bash
# Start from a blocked gate (Recipe SC-004 left the workflow blocked).
chitin-orchestrator merge-queue signal <workflow-id> re-review --reason "addressed both blockers in commit abc123"
```

**Expected**:

1. Signal handler emits `re-review.accepted` event with the reason.
2. (If no child was in flight, skip cancellation; if one was, it's cancelled first.)
3. A fresh `PRReviewWorkflow` child is spawned with a fresh snapshot.
4. The new dialectic runs end-to-end.

**Pass check**:

- `chitin-orchestrator merge-queue events <workflow-id>` shows BOTH the original review-invocation events (immutable per FR-015) AND the new review-invocation events from the fresh execution. Total events: at least 4 reviewer invocations (2 old + 2 new), possibly more with arbiter.
- The new gate decision supersedes the old; merge proceeds (if new gate is passed) or remains blocked (if still failing).

---

## Recipe SC-012 — Override recorded explicitly

**Claim**: For a non-governance PR whose review gate is blocked, an operator override-review signal allows merge to proceed; telemetry records both the original blocking verdict and the override.

```bash
# Start from a blocked gate on a non-governance class (Recipe SC-004 on a spec-only or impl class).
chitin-orchestrator merge-queue signal <workflow-id> override-review --reason "blocker is stylistic; spec 094 allows operator override on non-governance"
```

**Expected**:

1. Signal handler verifies class != governance → passes.
2. Handler verifies `reason` is non-empty → passes.
3. `override-review.accepted` event emitted with operator's reason.
4. Gate state mutates to `passed (override)`.
5. Merge proceeds.

**Pass check**:

- `chitin-orchestrator merge-queue events <workflow-id>` shows the original 2 reviewer-invocation events (both request-changes), the gate-decided event (state=blocked), the override-review.accepted event with operator reason, and the merge.completed event with `gate.state="passed (override)"`.
- Telemetry stream from an external observer preserves the same trace.

---

## End-to-end smoke (all-recipe sequence)

A minimal end-to-end smoke is to run SC-001 (happy path) and SC-006 (governance no override) on real PRs. If both pass, the workflow is functionally healthy:

```bash
chitin-orchestrator merge-queue submit <<'YAML'
queue:
  - repo: chitinhq/chitin
    pr: <small-clean-spec-pr>
  - repo: chitinhq/chitin
    pr: <governance-pr-designed-to-fail>
YAML
```

Verify:
- First PR: `chitin-orchestrator merge-queue events <wid-1> --filter review.gate.decided` → state=passed.
- Second PR: gate=blocked; attempted `override-review` rejected; only `re-review` with new affirmative verdicts can land it.

If both behave as expected, SC-001 + SC-006 pass and the dialectic gate is operational.
