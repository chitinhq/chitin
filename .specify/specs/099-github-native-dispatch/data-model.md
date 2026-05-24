# Spec 099 — Phase 1 Data Model

This spec introduces no new persistent storage. All state lives in the chain (`~/.chitin/events-*.jsonl`). Below: chain event types + the transient Go structs the dispatcher and listener pass through.

## Chain event types (new)

### `copilot_dispatched`

Emitted by `chitin-orchestrator schedule --driver copilot` after the GitHub issue is successfully created.

| Field | Type | Notes |
|---|---|---|
| `repo` | string | `owner/name` slug |
| `spec_ref` | string | `NNN-name` |
| `issue_url` | string | full HTTPS URL |
| `issue_number` | int | GitHub issue number |
| `dispatched_at` | RFC 3339 | server-side timestamp from chitin (not GitHub) |

Required FR mapping: FR-005.

### `copilot_pr_detected`

Emitted by the factory-listen `/webhook/pr` handler on the first `pull_request.opened` (or `ready_for_review` if PR opens as draft and is later flipped) that satisfies FR-007 eligibility.

| Field | Type | Notes |
|---|---|---|
| `repo` | string | |
| `pr_number` | int | |
| `pr_url` | string | |
| `spec_ref` | string | Recovered from the `Closes #ISSUE` reference; `"unknown"` if not recoverable (edge case in spec.md) |
| `issue_number` | int | The originating Copilot dispatch issue number (or `0` if unknown) |
| `commits` | int | Diff stat at detection time |
| `additions` | int | |
| `deletions` | int | |
| `changed_files` | int | |
| `detected_at` | RFC 3339 | |

Idempotency: at most one event per `(repo, pr_number)` tuple per FR-008. Enforced by the dispatcher querying the chain before emit.

### `copilot_review_posted`

Emitted after the spec 094 `PRReviewWorkflow` completes and the verdict is commented on the PR.

| Field | Type | Notes |
|---|---|---|
| `repo` | string | |
| `pr_number` | int | |
| `review_run_id` | string | Temporal RunID of the PRReviewWorkflow |
| `verdict` | string | one of `approve`, `approve_with_comments`, `request_changes`, `abstain` |
| `posted_at` | RFC 3339 | |

### `copilot_review_failed`

Emitted when the PRReviewWorkflow itself errors (Temporal failure, all drivers FailureError, etc.).

| Field | Type | Notes |
|---|---|---|
| `repo` | string | |
| `pr_number` | int | |
| `review_run_id` | string | may be empty if workflow never started |
| `failure_kind` | string | `temporal_unreachable`, `all_drivers_failed`, `dispatch_error`, etc. |
| `detail` | string | error message, truncated to 1 KiB |
| `failed_at` | RFC 3339 | |

### `copilot_pr_activity` (FR-013)

Emitted for every `pull_request.*` / `pull_request_review.*` / `issue_comment.created` webhook received for a PR carrying the `chitin-dispatch` label. **PR-level telemetry stream** that partially recovers what we lose by dispatching off-machine.

| Field | Type | Notes |
|---|---|---|
| `repo` | string | |
| `pr_number` | int | |
| `event_type` | string | the `X-GitHub-Event` header value |
| `event_action` | string | e.g. `opened`, `synchronize`, `closed`, `reopened`, `labeled`, `created` |
| `delivery_id` | string | `X-GitHub-Delivery` header (for dedup against GitHub redelivery) |
| `payload` | object | full webhook body MINUS auth headers; truncated to 64 KiB if larger |
| `received_at` | RFC 3339 | |

### `copilot_dispatch_stale`

Emitted by a periodic stale-check (default 24h after `copilot_dispatched`) when no `copilot_pr_detected` event has been observed for the same `(repo, issue_number)`. Surfaced in `copilot-list` as `state=stale`.

| Field | Type | Notes |
|---|---|---|
| `repo` | string | |
| `issue_number` | int | |
| `spec_ref` | string | |
| `dispatched_at` | RFC 3339 | timestamp of the original dispatch |
| `stale_threshold_hours` | int | the threshold used (24 by default; configurable) |
| `stale_at` | RFC 3339 | when the stale check fired |

## Transient Go types (orchestrator-internal)

These don't persist. They flow through the dispatcher and the webhook handler.

### `internal/github/issue.go`

```go
type IssueCreateInput struct {
    Repo     string   // "owner/name"
    Title    string
    Body     string
    Labels   []string // e.g. {"chitin-dispatch", "driver:copilot"}
    Assignee string   // "copilot"
}

type IssueCreateOutput struct {
    Number int
    URL    string
}
```

### `internal/factory/pr_eligibility.go`

```go
type PREligibility struct {
    Eligible    bool
    Reasons     []string // populated when !Eligible
    SpecRef     string   // recovered from "Closes #N" body parse; may be "unknown"
    IssueNumber int      // recovered from "Closes #N"; 0 if unknown
}
```

### `internal/factory/listener.go` (extended)

```go
type prPayload struct {
    Action      string `json:"action"`
    Number      int    `json:"number"`
    PullRequest struct {
        URL       string `json:"html_url"`
        Draft     bool   `json:"draft"`
        Body      string `json:"body"`
        Commits   int    `json:"commits"`
        Additions int    `json:"additions"`
        Deletions int    `json:"deletions"`
        Changed   int    `json:"changed_files"`
        Labels    []struct {
            Name string `json:"name"`
        } `json:"labels"`
    } `json:"pull_request"`
    Repository struct {
        FullName string `json:"full_name"`
    } `json:"repository"`
}
```

(Subset of the GitHub `pull_request` webhook payload. Extend as new fields become load-bearing.)

## State transitions visible in `copilot-list`

Derived from chain events; no state stored separately.

```text
dispatched              ← copilot_dispatched emitted
   │
   ├──→ pr_detected     ← copilot_pr_detected emitted (issue→PR cross-reference)
   │       │
   │       ├──→ review_posted   ← copilot_review_posted emitted
   │       │
   │       └──→ review_failed   ← copilot_review_failed emitted
   │
   └──→ stale           ← copilot_dispatch_stale emitted (24h timeout, no PR seen)
```

A dispatch can move from `pr_detected` back into `review_posted` multiple times across Copilot iterations (force-push triggers `pull_request.synchronize` → re-review). `copilot-list` shows the most recent terminal state, with a counter for review iterations.
