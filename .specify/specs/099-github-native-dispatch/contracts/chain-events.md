# Contract — Chain event schemas (spec 099)

Six new event types. All payloads are JSON objects emitted via the existing kernel emit path (`chitin-kernel emit -event-json -`). All wrap in the standard chain event envelope (`event_id`, `prev_hash`, `this_hash`, `ts`, `event_type`, `workflow_run_id`, `payload`).

## Schema versioning

All payloads carry an implicit `schema_version: 1` of the chitin-protocol envelope. Field additions are non-breaking; removal or rename require a new event type.

## Event 1: `copilot_dispatched`

```json
{
  "event_type": "copilot_dispatched",
  "workflow_run_id": "<run-id-or-ad-hoc-uuid>",
  "payload": {
    "repo": "owner/name",
    "spec_ref": "099-github-native-dispatch",
    "issue_url": "https://github.com/owner/name/issues/42",
    "issue_number": 42,
    "dispatched_at": "2026-05-24T12:34:56Z"
  }
}
```

**Required:** all fields. **Emitted by:** `cmdSchedule` on the Copilot branch.

## Event 2: `copilot_pr_detected`

```json
{
  "event_type": "copilot_pr_detected",
  "workflow_run_id": "<router-workflow-run-id>",
  "payload": {
    "repo": "owner/name",
    "pr_number": 100,
    "pr_url": "https://github.com/owner/name/pull/100",
    "spec_ref": "099-github-native-dispatch",
    "issue_number": 42,
    "commits": 3,
    "additions": 120,
    "deletions": 45,
    "changed_files": 8,
    "detected_at": "2026-05-24T12:40:01Z"
  }
}
```

**Required:** repo, pr_number, pr_url, detected_at. Diff stats may be 0 if PR object lacked them at receive time. `spec_ref` is `"unknown"` and `issue_number` is `0` if the `Closes #N` reference is missing or unresolvable.

**Emitted by:** factory-listen `/webhook/pr` handler on the first eligible event per `(repo, pr_number)`.

## Event 3: `copilot_review_posted`

```json
{
  "event_type": "copilot_review_posted",
  "workflow_run_id": "<router-workflow-run-id>",
  "payload": {
    "repo": "owner/name",
    "pr_number": 100,
    "review_run_id": "<temporal-pr-review-run-id>",
    "verdict": "approve_with_comments",
    "posted_at": "2026-05-24T12:45:30Z"
  }
}
```

**Required:** all fields. `verdict` ∈ `{approve, approve_with_comments, request_changes, abstain}`.

**Emitted by:** post-PRReviewWorkflow callback (activity) after the verdict is commented on the PR.

## Event 4: `copilot_review_failed`

```json
{
  "event_type": "copilot_review_failed",
  "workflow_run_id": "<router-workflow-run-id>",
  "payload": {
    "repo": "owner/name",
    "pr_number": 100,
    "review_run_id": "",
    "failure_kind": "temporal_unreachable",
    "detail": "dial tcp 127.0.0.1:7233: connection refused",
    "failed_at": "2026-05-24T12:46:00Z"
  }
}
```

**Required:** repo, pr_number, failure_kind, failed_at. `review_run_id` may be empty if the workflow never started. `detail` truncated to 1 KiB.

**failure_kind** enumeration (closed):
- `temporal_unreachable`
- `all_drivers_failed`
- `dispatch_error`
- `gh_comment_failed`

**Emitted by:** router workflow failure handler.

## Event 5: `copilot_pr_activity` (FR-013)

```json
{
  "event_type": "copilot_pr_activity",
  "workflow_run_id": "",
  "payload": {
    "repo": "owner/name",
    "pr_number": 100,
    "event_type": "pull_request",
    "event_action": "synchronize",
    "delivery_id": "<x-github-delivery-uuid>",
    "payload": { "...": "full webhook body" },
    "received_at": "2026-05-24T12:47:12Z"
  }
}
```

**Required:** repo, pr_number, event_type, event_action, delivery_id, received_at. `payload` is the full webhook body minus authentication headers; truncated to 64 KiB.

`workflow_run_id` is empty (this event is not tied to a workflow; it's PR-level audit telemetry).

**Emitted by:** factory-listen `/webhook/pr` handler for every event received on a PR carrying `chitin-dispatch`, regardless of eligibility.

## Event 6: `copilot_dispatch_stale`

```json
{
  "event_type": "copilot_dispatch_stale",
  "workflow_run_id": "<original-dispatch-run-id>",
  "payload": {
    "repo": "owner/name",
    "issue_number": 42,
    "spec_ref": "099-github-native-dispatch",
    "dispatched_at": "2026-05-23T12:34:56Z",
    "stale_threshold_hours": 24,
    "stale_at": "2026-05-24T12:34:56Z"
  }
}
```

**Required:** all fields. **Emitted by:** the stale-check workflow (periodic, default every 1h) when `dispatched_at + threshold_hours < now()` and no `copilot_pr_detected` exists for `(repo, issue_number)`.

## Replay invariants

All six event types are append-only and the chain hash chain stays intact across them — they participate in the canonical `prev_hash`/`this_hash` linkage like any other kernel-emitted event. No special handling for replay.

## Dedup invariants

| Event | Dedup key | Source of truth |
|---|---|---|
| `copilot_dispatched` | none (operator may dispatch the same spec twice; produces two events) | — |
| `copilot_pr_detected` | `(repo, pr_number)` | chain query (FR-008) |
| `copilot_review_posted` | none (re-review is allowed on `synchronize`; each posts a fresh event) | — |
| `copilot_review_failed` | none | — |
| `copilot_pr_activity` | `delivery_id` | header-based dedup |
| `copilot_dispatch_stale` | `(repo, issue_number)` per stale-check window | chain query in the stale-check workflow |
