# Contract — `factory-listen` extension for PR events

## Surface

Extends `chitin-orchestrator factory-listen` (spec 098). Adds one new HTTP route to the existing ServeMux.

```text
POST /webhook/pr
```

Accepts payloads for `X-GitHub-Event: pull_request`, `pull_request_review`, `issue_comment`. Same HMAC scheme as `/webhook/push` (`X-Hub-Signature-256` header, secret loaded from `--secret-file`).

## Request

Standard GitHub webhook envelope. Required headers:

| Header | Required | Notes |
|---|---|---|
| `Content-Type` | `application/json` | |
| `X-GitHub-Event` | yes | one of `pull_request`, `pull_request_review`, `issue_comment` |
| `X-GitHub-Delivery` | yes | dedup key for `copilot_pr_activity` |
| `X-Hub-Signature-256` | yes | HMAC; mismatch → 401 |

Body: standard GitHub webhook payload for the event type.

## Response

```json
{
  "received": true,
  "event_type": "pull_request",
  "action": "opened",
  "pr_number": 42,
  "eligible": true,
  "review_started": true,
  "review_run_id": "<temporal-run-id>",
  "dedup_skipped": false,
  "skipped_reason": null
}
```

Fields:

- `eligible` — result of `PREligibility` (FR-007).
- `review_started` — `true` iff this request triggered a new `PRReviewWorkflow`. `false` if the event was eligible but already detected (idempotent skip), or ineligible.
- `dedup_skipped` — `true` iff `copilot_pr_detected` already existed for `(repo, pr_number)`.
- `skipped_reason` — populated when `eligible=false`: one of `missing_label`, `not_draft_or_ready`, `no_closes_reference`, `event_type_ignored`.

## Eligibility logic (FR-007)

A pull-request event triggers a `PRReviewWorkflow` iff ALL:

1. `action ∈ {opened, ready_for_review, reopened, synchronize}`
2. PR carries label `chitin-dispatch`
3. PR body contains a `Closes #N` reference, AND `N` matches an issue that previously emitted `copilot_dispatched`

Conditions 1+2 are necessary even without condition 3 — if (1+2) hold but (3) fails, the PR is still detected (`copilot_pr_detected` with `spec_ref: unknown`) and review fires; only the dispatch-cross-reference is missing.

## Idempotency (FR-008, SC-003)

Before emitting `copilot_pr_detected` or starting the review, the handler queries the chain for an existing `copilot_pr_detected` with the same `(repo, pr_number)`. If found:
- Skip workflow start
- Skip event emit
- Still emit `copilot_pr_activity` (the activity stream is per-delivery, not per-PR)
- Return `dedup_skipped: true`

## Always-on side effect: `copilot_pr_activity`

For ANY incoming event where the PR carries `chitin-dispatch`, the handler emits one `copilot_pr_activity` event regardless of eligibility outcome. This is FR-013 — the PR-level telemetry stream is the deliberate partial-recovery of telemetry lost by dispatching off-machine.

Activity events ARE deduplicated by `X-GitHub-Delivery` header to avoid duplicate-storm under GitHub redelivery.

## Constitutional gates

- §1: chain emit through the kernel; no direct file writes
- §7: starting `PRReviewWorkflow` is orchestrator-driven dispatch — review IS an orchestrator-driven work-unit
