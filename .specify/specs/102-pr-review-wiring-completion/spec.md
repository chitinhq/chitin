---
spec_id: 102
title: PR Review Workflow Wiring Completion (Spec 094 v1.0.1)
status: Draft
owner: chitinhq
created: 2026-05-23
depends_on:
  - 094
related:
  - 075
  - 093
  - 097
  - 101
---

# Spec 102 — PR Review Workflow Wiring Completion (Spec 094 v1.0.1)

## Why

Spec 094 (PR Review Mechanism) merged its US1 MVP to main via PR #933. The workflow code (`workflows/pr_review.go`), the activities (`activities/review/*.go`), the verdict math (`activities/review/verdict/*.go`), and the dialectic algorithm all exist. But the wiring that lets the Temporal worker actually run the workflow on a real PR was never completed.

Four gaps, surfaced during the 2026-05-23 live demo when an attempt to manually trigger `PRReviewWorkflow` against PR #950 hit `unable to find workflow type: PRReviewWorkflow` and surfaced more incompleteness behind it:

| # | Gap | Where |
|---|---|---|
| 1 | `PRReviewWorkflow` not in `workflows.Register` | `workflows/hello.go:16-26` (the Register fn lives in hello.go) |
| 2 | Review activities not registered with worker | no `RegisterReviewActivities` exists; activities/review/ has constructors but no Register entry-point |
| 3 | openclaw lacks `CapCodeReview` capability | `driver/openclaw/driver.go:57-61` (only CapCodeImplement, CapCodeRefactor, CapTestAuthor declared) |
| 4 | **`CapturePRSnapshot` activity TODO'd but not implemented** | `workflows/pr_review.go:118` (`TODO(spec-094-impl PR #2): wire CapturePRSnapshot activity that…`) |

Without these, spec 094's dialectic review cannot run on any real PR. The merge queue (spec 093) is design-only too, so there's also no auto-trigger path. The combination means PR review through the orchestrator is currently zero-percent functional on main, despite the code being there.

Spec 102 is the **completion PR family** that takes spec 094 from "code on main" to "actually runs end-to-end on a real PR."

## Scope of "v1.0.1"

The version bump is intentional. Spec 094 is not amended (no new FRs added; existing semantics preserved). This is strictly the wiring that 094 promised but didn't deliver. v1.0.1 instead of v1.1 because:

- v1.1 amendment (in spec 094 itself) covers `review_required` + `arbiter_type` columns once spec 093 ratifies — a forward feature.
- v1.0.1 here covers backfill of v1.0's contract — a hygiene release.

## User Stories

### US1 (P1) — Operator can manually invoke a PR review

> As the operator, I run `chitin-orchestrator review-pr --pr-number 950 --repo chitinhq/chitin --policy-class impl` and the dialectic review workflow runs end-to-end against PR #950. Two reviewers are selected, both run, verdicts aggregate, and the result is recorded in chain telemetry. The review can be triggered before spec 093's merge queue exists.

**Independent test:** Open a draft PR on a test repo. Run the `review-pr` subcommand. Assert (a) `PRReviewWorkflow` starts in Temporal, (b) `SelectReviewers` returns a slate of 2+ drivers, (c) `DispatchMachineReviewer` runs for each, (d) chain emits `pr_review_completed` event with the dialectic outcome.

### US2 (P1) — CapturePRSnapshot fetches real PR state

> The `CapturePRSnapshot` activity (currently a TODO in `pr_review.go:118`) fetches the PR's diff, files, additions, deletions, base/head SHAs via `gh` CLI or the GitHub REST API. Returns a `PRSnapshot` whose content hash anchors the dialectic per FR-032. Re-running the activity against the same PR at the same HEAD yields the same hash.

**Independent test:** Capture a snapshot of PR #950 twice. Assert both invocations return the same `PRSnapshot` and `SnapshotHashRef(snap)` is identical. Modify the PR (push a new commit); capture again; assert the hash changes.

### US3 (P1) — openclaw becomes a reviewer

> openclaw's `Card().Capabilities` adds `CapCodeReview`. With openclaw + codex registered, `SelectReviewers` has the two it needs for the dialectic. No more "shortfall (eligible pool < 2)" halt when copilot/claudecode aren't ready or are pinned out.

**Independent test:** Register registry with `CHITIN_DRIVER_ALLOW=codex,openclaw`. Call `SelectReviewers` for `code.review` capability. Assert returned slate has `Primary1`/`Primary2` ∈ {codex, openclaw} with no shortfall.

### US4 (P2) — Workflow + activity registration plumbing

> `workflows.Register(w)` includes `PRReviewWorkflow`. A new `review.RegisterActivities(w, deps)` function (parallel to existing `activities.RegisterSchedulerActivities`) registers `SelectReviewers`, `DispatchMachineReviewer`, `EmitReviewTelemetry`, and `CapturePRSnapshot`. The main worker boot calls both. Idempotent: re-registering on restart is harmless.

**Independent test:** Restart the orchestrator service. Inspect Temporal worker registration via `temporal task-queue describe --task-queue chitin --task-queue-type=workflow` and `--task-queue-type=activity`. Assert all four review activities and `PRReviewWorkflow` appear in the supported lists.

## Functional Requirements

### CapturePRSnapshot

- **FR-001** New file `activities/review/capture_pr_snapshot.go`. Implements `CapturePRSnapshot` activity with constructor `NewCapturePRSnapshot(githubClient GitHubClient) *CapturePRSnapshot` and `ActivityName() string { return "CapturePRSnapshot" }`.
- **FR-002** Input `CapturePRSnapshotInput{Repo string, PRNumber int}`. Output is the existing `PRSnapshot` type from `snapshot.go`.
- **FR-003** Implementation fetches via `gh pr view <num> --repo <repo> --json files,additions,deletions,headRefOid,baseRefOid` OR via go-github SDK. Either way, the returned `PRSnapshot` must populate the content-bearing fields (Files, SpecArtifacts) so `SnapshotHashRef(snap)` produces a stable hash.
- **FR-004** `SpecArtifacts` extraction: scan the PR's changed files for `.specify/specs/NNN-name/{spec,plan,tasks}.md` paths; include their contents in the snapshot. Empty if no spec artifacts in the PR (most PRs).
- **FR-005** Activity errors loudly on `gh` not found, on repo/PR not accessible, on rate limit. Temporal retry policy: 3 attempts with exponential backoff capped at 30s.

### Worker registration

- **FR-006** Add `w.RegisterWorkflow(PRReviewWorkflow)` to `workflows.Register(w)` in `workflows/hello.go` (or factor Register out to its own file if hello.go is the wrong home).
- **FR-007** New `review.RegisterActivities(w worker.Worker, deps ReviewActivityDeps)` in `activities/review/register.go`. Registers all 4 review activities. `ReviewActivityDeps{Registry *driver.Registry, GitHubClient GitHubClient}`.
- **FR-008** Main worker boot in `cmd/chitin-orchestrator/main.go` calls `review.RegisterActivities(w, review.ReviewActivityDeps{Registry: registry, GitHubClient: ghClient})` alongside the existing scheduler activities registration. `ghClient` is constructed from operator's `gh auth token` or env (`GH_TOKEN`).

### Capability cards

- **FR-009** `driver/openclaw/driver.go` Card adds `driver.CapCodeReview` to its capability list. Comment notes: "GLM 5.1 via Ollama Cloud is a frontier model and competent at code review; pairs with codex as the second dialectic primary without burning Anthropic credit."
- **FR-010** Verify gemini and hermes Card declarations — if they're also competent reviewers, add CapCodeReview to them too. Decision documented per driver in the impl PR.

### Operator entry-point

- **FR-011** New `chitin-orchestrator review-pr --pr-number N [--repo owner/name] [--pr-author login] [--policy-class impl] [--arbiter operator]` subcommand. (The 2026-05-23 demo built this — it lives in PR-attached `review_pr.go`; this spec re-codifies it as part of the v1.0.1 deliverable.)
- **FR-012** PR author auto-detection via `gh pr view --json author --jq .author.login` when `--pr-author` is empty.
- **FR-013** `chitin-orchestrator status --workflow-type pr-review` lists active PR review workflows. (Optional polish.)

### Chain events

- **FR-014** New chain event `pr_review_completed` emitted at workflow terminal. Payload: `{repo, pr_number, run_id, decision, reason, arbiter_engaged, primary1_driver, primary2_driver, arbiter_driver?, snapshot_hash}`.
- **FR-015** `pr_review_failed` emitted on workflow error (snapshot fetch failed, shortfall, activity timeout). Payload includes the failure reason.

## Success Criteria

- **SC-001** End-to-end manual review on PR #950: `chitin-orchestrator review-pr --pr-number 950` returns within 5 minutes with a chain event recording the dialectic decision. Verdict posted as a PR comment (FR — TBD whether comment posting is in spec 094 v1.0 or this spec).
- **SC-002** With `CHITIN_DRIVER_ALLOW=codex,openclaw` and no other env, the dialectic runs through entirely on subscription-paid drivers — zero Anthropic credit spent, zero Copilot metered spend. (Composes with spec 101 once that lands.)
- **SC-003** `CapturePRSnapshot` is deterministic for a given (repo, pr_number, head_sha) — 100 repeated invocations return identical `PRSnapshot` objects with identical `SnapshotHashRef`.
- **SC-004** Workflow + activity registration is idempotent: 10 successive `systemctl restart chitin-orchestrator.service` calls all leave the worker in the same registered state, verified via Temporal task-queue describe.

## Scope

### In scope

- `CapturePRSnapshot` activity implementation (gh CLI or REST)
- `PRReviewWorkflow` registered in `workflows.Register`
- New `review.RegisterActivities(w, deps)` + main wires it
- `openclaw` adds `CapCodeReview` (+ gemini/hermes audit)
- `chitin-orchestrator review-pr` subcommand (operator manual trigger)
- `pr_review_completed` / `pr_review_failed` chain events
- End-to-end live test against PR #950 as the validation case

### Out of scope

- **Spec 093 merge queue auto-trigger.** Spec 093 stays design-only. This spec is operator-manual-trigger.
- **Class-routed arbiter (spec 094 v1.1).** v1.0.1 stays with the operator-arbiter default. v1.1 ratifies the class table.
- **PR comment posting of the verdict.** May already be in spec 094 v1.0; if not, this is an addendum decided at impl time. Either way scope is to make sure the workflow RUNS; commenting is icing.
- **GitHub-native Copilot path (spec 099).** Orthogonal — 099 is for issue-assigned Copilot PRs; 102 is for any PR.
- **Cost-aware reviewer selection.** Spec 101 handles that. 102 keeps the current `SelectReviewers` algorithm; 101 layers cost-awareness on top.

## Edge Cases

- **PR has no diff (empty change):** snapshot returns `PRSnapshot{Files: []}`; reviewers see an empty change and should abstain. Telemetry records this as `decision: abstain`.
- **PR is private / orchestrator's gh auth lacks access:** `CapturePRSnapshot` fails loud with the repo and access error. `pr_review_failed` emitted.
- **PR's head ref has advanced between trigger and snapshot:** the snapshot captures whatever was at HEAD when `CapturePRSnapshot` ran. Snapshot hash is the audit anchor; if HEAD advanced, the hash differs from any prior snapshot for the same PR. Operator can re-run the review.
- **Pool shortfall (< 2 ready reviewers with CapCodeReview):** workflow halts cleanly with `decision: halted, reason: pool_shortfall`. No primary dispatched, no telemetry event for nonexistent reviewers.
- **One primary times out / errors:** existing v1.0 workflow handles via arbiter engagement (per pr_review.go). v1.0.1 doesn't change that.

## Assumptions

- Operator has `gh` CLI authenticated and the token has `pull_requests:read` for every repo whose PRs the orchestrator reviews.
- The chitin Temporal worker accepts new workflow/activity registrations on restart (verified throughout the session — yes).
- Spec 094's verdict math and dialectic algorithm work as documented (covered by the existing pr_review_test.go suite — passing on main).

## Notes for Implementation Phase

**Implementation deferred** — design-only. Recommended sequence as 2 follow-up PRs:

### PR-A — Wiring (small, low-risk)

1. Add `CapCodeReview` to openclaw Card (1 line)
2. Add `w.RegisterWorkflow(PRReviewWorkflow)` in `workflows/hello.go` (1 line)
3. Create `activities/review/register.go` with stub `RegisterActivities` that registers existing activities (~20 lines)
4. Wire `review.RegisterActivities` in main.go (1 line + small deps struct)
5. Add `review-pr` subcommand from the 2026-05-23 session's local `review_pr.go` (~120 lines, already drafted)
6. Build + ship + test against a stub PR

Note: PR-A's review workflow will still fail at `CapturePRSnapshot` because that activity is still TODO. That's OK — PR-A's goal is to prove the registration plumbing, not the snapshot path. Test by mocking the snapshot input.

### PR-B — CapturePRSnapshot impl

7. Implement `activities/review/capture_pr_snapshot.go` (~150-250 lines incl. `gh` invocation and PRSnapshot population)
8. Add SpecArtifacts extraction (~50 lines)
9. End-to-end test against PR #950 (~30 lines test + recipe)
10. `pr_review_completed` chain event emission
11. Operator runbook `docs/operator/review-pr.md`

PR-B is the load-bearing impl. Bounds gate (2000 lines) easily fits both PRs.

After PR-B: SC-001 demo lands. Cell 14 of the system-state matrix closes.
