# Phase 0 Research: PR Review Mechanism

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-05-23

## Purpose

Resolve every `NEEDS CLARIFICATION` and every open implementation decision the spec deferred to planning, before Phase 1 design begins. Each decision is captured as `R-<short-id>`, with the chosen path, the rationale, and the alternatives rejected.

The spec defers the following to planning (called out explicitly in Assumptions):

- **R-OPSURF** — Operator-arbiter surface mechanism (spec Assumptions: "Operator-arbiter surface deferred to planning")
- **R-SEL** — How to extend or wrap spec 076's `SelectDriver` (spec Assumptions: "extends that activity ... or wraps it in a new selection activity")
- **R-AUTHORID** — Author-identifier-to-driver mapping (spec Assumptions: "the PR author identifier can be mapped to a driver ID")

The remaining decisions below (`R-AGG`, `R-VTRANSPORT`, `R-SNAP`, `R-RERUN`, `R-OVERRIDE`, `R-HEARTBEAT`, `R-POLICYAMEND`) are open implementation choices that the spec implies but does not enumerate; they are resolved here so Phase 1 can produce concrete contracts.

---

## R-OPSURF — Operator-arbiter surface mechanism

**Decision**: Structured GitHub PR comment authored by the operator, parsed by the orchestrator.

The orchestrator posts a comment on the PR containing a pre-filled, copy-pasteable verdict template (a fenced YAML block with `verdict`, `concerns`, `recommendations`, `blockers`, optional `reason`). It also emits a Discord notification linking to that comment (per FR-025 + spec 080). The operator edits the YAML in a reply comment authored by the operator's GitHub identity (the orchestrator listens for new comments via `gh api` polling inside the dispatch-operator-arbiter activity, with the activity heartbeating per FR-029). On comment receipt, the activity parses the YAML, validates against the FR-014 invariants, and either returns a `StructuredVerdict` to the workflow or treats malformed parse as a failed outcome.

**Rationale**:

- **Verdict lives where the PR lives.** GitHub PR comments are the canonical audit surface for PR-related operator decisions; running the arbiter through this surface keeps the audit trail co-located with the PR diff, the discussion history, and the merge metadata. No "where did the operator say yes?" reconstruction problem later.
- **No new tool.** Operator already has `gh` CLI, the GitHub web UI, and the GitHub mobile app — three independently working pathways into a single surface. No new credentials, no new RPC, no new UI to learn.
- **Survives operator-offline.** Because comment polling is the receive mechanism, the operator can respond from any device with GitHub access, including the phone. The heartbeat re-notification (FR-025) goes to Discord — the operator sees a Discord ping, opens the PR, replies in the comment.
- **YAML is human-writeable.** The four-list shape (verdict, concerns, recommendations, blockers) maps cleanly to YAML lists; the operator does not need to learn a tool-specific schema. The template is pre-filled by the orchestrator so the operator sees the exact shape to fill in.
- **Parse failure is recoverable.** If the operator writes malformed YAML, the activity emits a "malformed operator verdict" telemetry event and re-posts the template with a parse-error annotation; the operator corrects in a follow-up comment. No verdict is recorded until validation passes.

**Alternatives considered and rejected**:

| Alternative | Why rejected |
|---|---|
| Discord-thread structured prompt with reaction-emoji shortcuts | Reactions can't carry blocker/recommendation text; Discord threads aren't archived in a way that survives bot churn or channel deletion; not a canonical PR record. |
| Dedicated operator CLI subcommand (`chitin-orchestrator review submit --pr 928 --verdict approve ...`) | Requires the operator to be at a terminal; conflicts with the "operator may respond from phone" expectation; would be a duplicate surface to GitHub comments. |
| Custom web UI in `chitin-console` | Material implementation effort; another surface to host, auth, and back up; no payoff over a GitHub comment for v1. Reconsider once `chitin-console` has the staffing. |
| Slack/Discord slash command with modal form | Modal form is platform-tied; Discord's `interactions` history is not the audit substrate; Discord availability is a single point of failure. |
| GitHub Reviews (the native "request changes" / "approve" with the review body field) | The native review states are tied to the PR submitter, branch protection rules, and the GitHub-imposed approval semantics — none of which align with the orchestrator-driven dialectic. Using PR comments instead keeps the orchestrator the source of truth. |

**Implications for Phase 1**:

- `contracts/operator-arbiter-surface.md` documents the YAML template, the comment-posting protocol, the comment-polling protocol, and the parse-error recovery flow.
- The dispatch-operator-arbiter activity has two activity-level operations under the hood: (a) post the prompt comment and Discord ping; (b) long-poll the PR comments for a verdict reply from the operator's GitHub identity, with heartbeat every 30s and a 2h re-notification boundary per FR-025/029.
- The orchestrator stores no operator-arbiter state outside Temporal — the activity is signal-blocked on a `PRReviewWorkflow` signal that the comment-poller activity sends when it parses a valid YAML reply.

---

## R-SEL — Extend vs wrap spec 076's `SelectDriver`

**Decision**: Extend `SelectDriver` in place with two new optional parameters: `requireCapability` (string, e.g. `"reviewer"`) and `excludeIdentities` (string list).

**Rationale**:

- **One source of truth for selection logic.** Driver selection already lives in spec 076's `SelectDriver` activity. Wrapping it in a parallel `SelectReviewers` activity duplicates the registry-read code, the health-filter code, and the capability-tag-match code. Every future selection-behaviour change would need to be applied twice and would inevitably drift.
- **Additive parameters preserve backward compatibility.** The existing callers (spec 086, spec 092 dispatchers) pass neither parameter and get unchanged behaviour. The new caller (spec 094's pool-selection activity) passes both.
- **The selection rules are themselves driver-table rules, not review-specific.** "Don't pick the author" is exactly the kind of policy that lives in `SelectDriver` because it composes with other selection constraints (capability, health, prior-invocation history, etc.). Keeping it in `SelectDriver` means the same logic can be reused by any future workflow that has a no-self-bypass invariant (e.g., a spec drafting workflow that needs to dispatch a reviewer ≠ the drafting driver).

**Alternatives considered and rejected**:

| Alternative | Why rejected |
|---|---|
| New `SelectReviewers` activity that wraps `SelectDriver` | Duplicates logic; introduces a second selection surface that diverges. |
| Inline the selection logic in `PRReviewWorkflow` | Workflow code must be deterministic — directly reading the driver registry is I/O, not allowed inside workflow functions. |
| Add a new selection field on the driver registry instead of a parameter (`is_reviewer_eligible` bool per driver) | The "eligibility" is per-PR-context, not per-driver-static (the eligibility excludes the PR's author). Static-per-driver would lose that. |

**Implications for Phase 1**:

- `data-model.md` notes the `SelectDriver` signature extension and the new `ReviewerSlate` return type that `select_reviewers.go` constructs by calling `SelectDriver` three times (once for each primary slot with the current-set running excluded, once for the arbiter slot with both primaries and the author excluded).
- The signature change is additive; spec 086 and spec 092's existing call sites get no semantic change.

---

## R-AUTHORID — Author-identifier-to-driver mapping

**Decision**: Use the existing driver-registry `git_identity` field (added in spec 075 v1.1) as the mapping source. The selection activity calls `Registry.LookupByGitIdentity(prAuthor)` and gets either a driver ID or `nil`. If the lookup returns a driver ID, that ID is added to `excludeIdentities`. If it returns `nil`, no driver is excluded.

**Rationale**:

- **The mapping already exists on disk.** Spec 075's driver-registry v1.1 added `git_identity` per driver (e.g., `hermes`'s `git_identity` is `"hermes-bot"`, `openclaw`'s is `"clawta-bot"`, etc.) for telemetry attribution. Re-using it for the exclusion check costs zero new state.
- **Unmapped authors return `nil` cleanly.** Per the spec's edge case "the PR author identifier does NOT map to any known driver" (e.g., the operator's GitHub identity, a human contributor's identity, GitHub Actions bot), the lookup returns `nil` and no driver is excluded — matching the spec's Acceptance Scenario 4.3.
- **Single resolution point.** The registry is the source of truth for "what drivers exist and how to identify their work" — putting the mapping in the registry keeps the exclusion check honest under registry mutation (new driver added → its git_identity is immediately matchable).

**Alternatives considered and rejected**:

| Alternative | Why rejected |
|---|---|
| Hardcoded map in the selection activity (`{"hermes-bot": "hermes", ...}`) | Drifts the moment a new driver is added; violates the spec 075 "registry is the metadata source" rule. |
| Parse the PR body or commit trailer for a driver-id stamp | Adds an authoring discipline burden; bypasses the existing registry; vulnerable to spoofing. |
| Lookup by PR submitter (the `gh pr` `author.login`) directly without registry indirection | Couples the workflow to GitHub-specific identifiers; harder to extend to non-GitHub sources later. |

**Implications for Phase 1**:

- `data-model.md` documents the `git_identity` field on the registry entity and the `LookupByGitIdentity` registry method (which spec 075 already has if v1.1 has shipped; if not, this becomes a small dependency added inline).
- The `select_reviewers.go` activity calls `LookupByGitIdentity` once at the top of its body and threads the result into the `excludeIdentities` list passed to `SelectDriver`.

---

## R-AGG — Dialectic aggregation function

**Decision**: A pure Go function in `verdict/`, called from `PRReviewWorkflow` (not from an activity), implementing FR-009 verbatim:

```go
func Aggregate(primaries []ReviewerOutcome, arbiter *ReviewerOutcome) ReviewGateDecision {
    // Primaries always come in pairs in v1; len(primaries) == 2 invariant enforced by workflow.
    p1, p2 := primaries[0], primaries[1]
    if p1.IsApproveShaped() && p2.IsApproveShaped() {
        return Passed("both primaries approve", arbiterEngaged: false)
    }
    if p1.IsRequestChanges() && p2.IsRequestChanges() {
        return Blocked("both primaries request-changes", arbiterEngaged: false)
    }
    // All other combinations require arbiter.
    if arbiter == nil {
        // Should not occur in a well-formed workflow; defensive.
        return Halted("arbiter required but not dispatched", arbiterEngaged: false)
    }
    return decisionFromArbiter(*arbiter)
}
```

A `ReviewerOutcome` is either a validated `StructuredVerdict` or a failed outcome (timeout / error / malformed). `IsApproveShaped()` returns true only for validated `approve` or `approve-with-comments`. `IsRequestChanges()` returns true only for validated `request-changes`. Failed outcomes are neither approve-shaped nor request-changes — they always fall to the arbiter case.

**Rationale**:

- **Pure function = trivially testable.** Aggregation is a closed-form decision on six inputs (two primary outcomes, optionally one arbiter outcome). Table-driven tests cover every cell of the cartesian product in milliseconds.
- **Workflow-side keeps it deterministic.** Calling a pure function from inside `PRReviewWorkflow` does not violate determinism — no I/O, no clocks, no randomness.
- **The decision tree is the spec.** Encoding FR-009 in one function in one file makes the spec/code correspondence one-to-one. Any spec change is a one-file diff.

**Alternatives considered and rejected**:

| Alternative | Why rejected |
|---|---|
| Aggregation as an activity | Adds an unnecessary activity boundary; activities are for I/O, and aggregation is pure compute. |
| Aggregation as state-machine workflow signals | Over-engineered for a pure decision; obscures the decision tree behind workflow-engine plumbing. |

**Implications for Phase 1**:

- `data-model.md` documents `ReviewerOutcome`, `ReviewGateDecision`, and the `Aggregate` function signature.
- `pr_review_test.go` table-tests every combination of primary outcomes (3 verdicts × 3 verdicts + failed × {everything}) including the arbiter cases.

---

## R-VTRANSPORT — How reviewer drivers return `StructuredVerdict` to the workflow

**Decision**: Each reviewer driver implements a `review` tool (the review-mode tool from FR-002). The dispatch-machine-reviewer activity invokes that tool via the driver's existing dispatch surface (Discord-side for Ares/Clawta; cloud API for Codex/Copilot; local subprocess for local-llm), with the input payload being the PR diff + spec artifacts + the policy class hint. The driver's `review` tool returns a JSON document conforming to the structured-verdict schema. The activity parses the JSON, calls `verdict.Validate(...)` (FR-014 invariants), and returns either a `StructuredVerdict` or a failed outcome to the workflow.

For Ares and Clawta specifically, the existing `chitin-kernel gate evaluate` path applies to all tool calls the driver makes while authoring the verdict (e.g., reading the diff, opening files in the PR, reading spec artifacts). The `review` tool itself is registered in the kernel-gated tool registry like any other tool; the verdict-emit is the tool's return value.

**Rationale**:

- **Reviewer drivers are drivers, not a parallel system.** They have their own model, their own prompt (the review-mode prompt they author per FR-003), their own normal tool-use path. The `review` tool is one more tool in their registry, not a special-cased channel.
- **Verdict transport is just the tool's return value.** No new transport, no new RPC, no new queue. The orchestrator activity calls the tool; the driver answers; the answer is the verdict.
- **Per-driver prompt freedom (FR-003).** Each driver owns its own review-mode prompt; the orchestrator only owns the input/output contract. This decouples driver-internal cognition from workflow-internal policy.

**Alternatives considered and rejected**:

| Alternative | Why rejected |
|---|---|
| Reviewer drivers post verdicts as GitHub comments, orchestrator polls | Mixes the operator-arbiter surface (R-OPSURF) with the machine-reviewer surface; makes the machine reviewer's verdict editable by a human, breaking FR-015 (immutability per invocation). |
| Reviewer drivers post verdicts via Discord, orchestrator polls | Same coupling problem; Discord availability becomes a hard dependency of machine review. |
| Reviewer drivers write a verdict file to a shared filesystem, orchestrator reads | Re-introduces shared mutable state that spec 087 retired. |
| Custom verdict-emit RPC endpoint | New surface to host, auth, and back up; no payoff over the existing tool-call return path. |

**Implications for Phase 1**:

- `contracts/review-mode-driver-contract.md` documents the `review` tool's input schema (PR diff + spec artifacts + policy class hint), output schema (StructuredVerdict JSON), and the kernel-gating expectation.
- The dispatch-machine-reviewer activity is a thin wrapper around the driver-dispatch substrate — no special-cased channel.

---

## R-SNAP — PR snapshot capture and re-review semantics

**Decision**: Each `PRReviewWorkflow` execution captures a `PRReviewSnapshot` at the moment the select-reviewers activity completes (a single `gh pr view --json files,headRefOid,body,...` call), and that snapshot is the immutable view passed to every reviewer in this execution. If the PR head moves while a review is in flight, the in-flight reviewers continue to see the old snapshot. On gate-passed, the parent `PRMergeWorkflow` performs the standard mergeability check before squash-merge; if the head has moved, the parent's behaviour is governed by spec 093's existing logic, not this workflow.

A re-review (FR-021) is a *new* `PRReviewWorkflow` child execution — not a mutation of an in-flight one. The parent `PRMergeWorkflow` receives the re-review signal, terminates any in-flight child review workflow (if still running), then spawns a fresh child with a fresh snapshot.

**Rationale**:

- **One snapshot per workflow execution preserves verdict-to-PR correspondence.** A reviewer's verdict is about a specific PR state; if the state changes mid-review, the verdict either becomes stale (if read before the change) or inconsistent (if read after). Locking the snapshot at workflow start gives "the verdict was about this exact PR head" as a workflow-history invariant.
- **Re-review = new workflow honors FR-015.** Verdicts are immutable per invocation; re-review with mutated state would make the same invocation produce different verdicts. A new workflow execution makes the audit trail correct: the old verdicts (about the old snapshot) and the new verdicts (about the new snapshot) both exist in history, attached to distinct workflow runs.
- **Spec 093 already owns mergeability re-checking.** This workflow doesn't need to handle PR-head-moves-during-review because spec 093's parent does the final mergeability check. Keeping the responsibility there avoids duplicating logic.

**Alternatives considered and rejected**:

| Alternative | Why rejected |
|---|---|
| Mutate the in-flight workflow's snapshot on PR head change | Breaks FR-015; reviewers can't reason about "what they reviewed." |
| Re-dispatch only the failed primary (rather than restart whole workflow) | Adds complexity for negligible benefit; the dialectic gate's two-primary symmetry is the spec's invariant, and restart is cheap. |
| No snapshot — reviewers fetch the PR live | Non-deterministic from the workflow's point of view; can't reproduce a past review chain. |

**Implications for Phase 1**:

- `data-model.md` documents `PRReviewSnapshot` (PR metadata + file diff + spec-artifact bundle + head-OID + capture timestamp) and notes that it's content-hashed for the telemetry stream (FR-032).
- `contracts/workflow-signal-schemas.md` documents the re-review signal as a *parent-workflow* signal that terminates the in-flight child and spawns a new one.

---

## R-RERUN — Re-review signal semantics

**Decision**: The re-review signal is a signal on `PRMergeWorkflow` (the parent), not on `PRReviewWorkflow` (the child). On receipt:

1. If a child `PRReviewWorkflow` is currently running and its gate is not yet decided, the parent sends a cancellation signal to the child and waits for it to complete with a `cancelled` decision (recorded in telemetry).
2. The parent then spawns a fresh `PRReviewWorkflow` child with a fresh snapshot capture.
3. If the parent receives a re-review signal while no child is running (e.g., after a previous gate decided `blocked`), step 1 is skipped and step 2 proceeds immediately.

Per the spec's edge case "Re-review signal arrives during an in-flight review: Ignored", the parent **does not** spawn a new child if the existing child has not yet been cancelled. The re-review signal is debounced — a second re-review signal received while the first cancel/spawn cycle is in flight is silently dropped (logged in telemetry).

**Rationale**:

- **Parent-owned signal preserves a single per-PR control surface.** The parent already receives `resume`, `abort`, `approve` signals from spec 093. Adding `re-review` and `override-review` to the same surface keeps the operator-to-workflow surface uniform (one workflow ID = one PR = one signal target).
- **Cancel-then-spawn is determinism-preserving.** Termination of the in-flight child is a Temporal signal pattern that the testsuite can replay; the cancelled child's history shows the cancellation cause, the new child's history shows the new dispatch.
- **Debounce avoids signal storms.** If the operator double-clicks the re-review button, the second click is dropped — no chaos.

**Alternatives considered and rejected**:

| Alternative | Why rejected |
|---|---|
| Re-review signal on `PRReviewWorkflow` directly | Two control surfaces per PR (parent + child); operator has to know which is in-flight to target the signal correctly; ambiguous when no child is running. |
| Re-review = restart parent | Wipes the merge workflow's own history (squash-merge attempts, conflict resolution, etc.); not what re-review should do. |
| Queue multiple re-reviews | Adds complexity for an operationally-rare case; debouncing is more correct in v1. |

**Implications for Phase 1**:

- `contracts/workflow-signal-schemas.md` documents the `re-review` signal as a parent signal with payload `{reason: string}`.
- `pr_review_test.go` includes a test where the child workflow receives a cancellation signal mid-dispatch and exits cleanly.

---

## R-OVERRIDE — Override-review signal interaction with `PRMergeWorkflow`

**Decision**: The override-review signal is also a parent signal (`PRMergeWorkflow`). On receipt:

1. If the PR's class is `governance`, the signal is rejected immediately with a structured error event in telemetry and a Discord notify back to the operator. The gate state is unchanged. (FR-023)
2. If the PR's class is non-governance, the signal must include a non-empty `reason` field. If `reason` is missing or empty, reject as above.
3. If reason is present and class is non-governance, the parent records an `override` telemetry event with the operator's `reason` (FR-022), marks the review gate as `passed` (override), and proceeds to merge as if the review had passed normally.

The override does **not** trigger any new reviewer dispatch; it bypasses the gate by operator authority. The original blocked verdicts and the override event both live in workflow history.

**Rationale**:

- **Governance non-overridability is a constitutional invariant (FR-023 + constitution §1 side-effect boundary).** Encoding it in the signal handler — not in policy — makes it irrevocable per-PR-class regardless of policy mutation.
- **Mandatory reason gates against blind overrides.** Operator must articulate the override reason; the reason is the audit substrate.
- **Bypass-without-new-review honors the operator's authority while preserving evidence.** The original verdicts stay in history (FR-034 immutability); the override stacks on top with an explicit reason.

**Alternatives considered and rejected**:

| Alternative | Why rejected |
|---|---|
| Override triggers a fresh review (rather than bypass) | That's `re-review`, not `override`; two signals for two semantics is correct. |
| Override is allowed for governance with a 2nd-operator-approver requirement | No 2nd-operator substrate exists yet; out of v1 scope. |
| Override is silently allowed without reason | Loses the audit substrate; violates spec 092's audit-trail invariants. |

**Implications for Phase 1**:

- `contracts/workflow-signal-schemas.md` documents the `override-review` signal with payload `{reason: string}` and the class-gated rejection rule.
- `pr_review_test.go` and `pr_merge_test.go` (in spec 093's existing test file, extended) cover both the rejection path and the accept-and-proceed path.

---

## R-HEARTBEAT — Heartbeat mechanism

**Decision**: Two heartbeat mechanisms layered:

1. **Activity-level heartbeat** (FR-026 + FR-030): every machine-reviewer dispatch activity calls `activity.RecordHeartbeat(ctx, ...)` every 30 seconds. Temporal's existing activity-heartbeat-timeout (configured at 60s) terminates a stalled activity. Worker restart resumes from the last heartbeat boundary.
2. **Workflow-level heartbeat** (FR-029): every two hours of wall-clock, the workflow emits a heartbeat telemetry event ("still running, gate decision pending") via a recurring child timer + activity. This is independent of activity heartbeats — it exists for operator visibility when the workflow has been waiting on the operator-arbiter for a long time. It does NOT cancel anything.

**Rationale**:

- **Two different scales need two different mechanisms.** Activity heartbeat is for "this activity is alive" (seconds-scale, terminates on failure). Workflow heartbeat is for "this PR is still in review" (hours-scale, never terminates anything). Conflating them either kills activities too aggressively or fails to surface long-running operator-arbiter waits.
- **Both are Temporal-native.** No new infrastructure.

**Alternatives considered and rejected**:

| Alternative | Why rejected |
|---|---|
| Only activity heartbeat | Misses the operator-arbiter long-wait case; FR-029 requires visibility heartbeat. |
| Only workflow heartbeat | Loses worker-restart resilience for machine reviewers. |
| External cron emitting heartbeats per PR | New infrastructure; not Temporal-native. |

**Implications for Phase 1**:

- `data-model.md` notes the two heartbeat boundaries and their distinct purposes.
- `pr_review.go` uses `workflow.NewTimer` for the workflow heartbeat; `dispatch_machine_reviewer.go` uses `activity.RecordHeartbeat` for the activity heartbeat.

---

## R-POLICYAMEND — Spec 093 policy-table amendment shape

**Decision**: The spec 093 policy table v1.x amendment adds exactly two columns:

| Column | Type | Per-class default (v1) |
|---|---|---|
| `review_required` | bool | `governance`: true · `spec-only`: true · `impl`: true · `live-fix`: false · `bookkeeping`: false · `research-docs`: true |
| `arbiter_type` | enum (`operator` \| `machine`) | `governance`: `operator` · `spec-only`: `operator` · all others: `operator` at v1 ship (operationally degenerate — see spec 094 Assumptions), `machine` once a third reviewer-tagged driver exists |

The amendment lands as a separate PR after spec 094 ratifies (spec 093 v1.1.0). The two columns are pure additions — no existing column changes type or semantics. The existing v1.0.0 policy table works unchanged if both new columns default to `review_required: false, arbiter_type: operator`.

**Rationale**:

- **Spec 094 captures the columns; spec 093 owns the table.** The amendment lives in spec 093's policy-table file; spec 094's `contracts/policy-table-amendment.md` is the source-of-truth document describing the amendment. After ratification, the amendment is applied to spec 093's `contracts/policy-table.md`.
- **Pure additive change preserves spec 093 v1.0.0 callers.** Anyone consuming the policy table for non-review reasons gets unchanged behaviour.
- **Per-class defaults match the spec.** `governance` and `spec-only` require operator arbiter (FR-020); the rest default to operator at v1 ship because of the two-reviewer-tagged-driver limitation (Assumptions: "v1 known limitation — machine arbiter not operationally viable").

**Alternatives considered and rejected**:

| Alternative | Why rejected |
|---|---|
| Add the columns directly in spec 093's v1.0.0 table before that spec ships | Couples the two specs' release timelines; the spec 093 PR is already designed and would have to be re-opened. |
| Encode review policy in a separate review-policy table | Forks the operator's mental model — "two tables to consult per PR" is worse than "one table with two more columns." |
| Encode review policy as per-driver settings | Review behaviour is per-PR-class (a `governance` PR demands operator arbitration regardless of driver), not per-driver. |

**Implications for Phase 1**:

- `contracts/policy-table-amendment.md` is the source-of-truth document describing this amendment. It is the artifact spec 093 v1.1.0 will copy into its own contracts when it ships.

---

## Closure check

All `NEEDS CLARIFICATION` markers in spec.md have been resolved (none existed in the as-shipped spec — speckit-lint verified). All planning-deferred items called out in spec Assumptions have been resolved by `R-OPSURF`, `R-SEL`, and `R-AUTHORID`. All implementation decisions the spec implicitly required have been resolved by `R-AGG`, `R-VTRANSPORT`, `R-SNAP`, `R-RERUN`, `R-OVERRIDE`, `R-HEARTBEAT`, and `R-POLICYAMEND`. Phase 1 (data-model, contracts/, quickstart) proceeds with no open clarifications.
