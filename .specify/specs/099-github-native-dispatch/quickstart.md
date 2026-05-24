# Spec 099 — Quickstart

End-to-end smoke against a real GitHub repo. ~5 minutes assuming `gh auth login` is current.

## Prerequisites

- `gh` CLI installed and authenticated (`gh auth status` green)
- A test GitHub repo where GitHub Copilot is installed (`@copilot` is assignable)
- `chitin-orchestrator factory-listen` running and reachable from GitHub (tunnel or App webhook). The chitin-factory-listen.service unit handles this on the operator host.
- The webhook secret loaded at `$HOME/.chitin/factory-webhook.secret`
- A spec dir under `.specify/specs/NNN-fixture/` in the target repo (just a spec.md + tasks.md stub will do)

## Step 1 — Dispatch a spec to Copilot

```bash
chitin-orchestrator schedule 099-fixture \
  --driver copilot \
  --repo your-org/your-test-repo
```

Expected output:

```text
copilot dispatched: https://github.com/your-org/your-test-repo/issues/<NUMBER>
  spec_ref: 099-fixture
  issue_number: <NUMBER>
```

Verify the chain event landed:

```bash
chitin-kernel chain tail --event-type copilot_dispatched | jq '.payload'
```

Expected: a record with `repo`, `spec_ref: "099-fixture"`, `issue_url`, `issue_number`, `dispatched_at`.

## Step 2 — Wait for Copilot to draft a PR

Copilot's drafting SLA varies (minutes to ~30 min for non-trivial work). For the smoke, you can either:

a) Wait for the real PR, OR
b) Open a synthetic PR yourself via `gh pr create --draft --label chitin-dispatch --body "Closes #<NUMBER>"` from the same repo

Option (b) is what the integration test uses; it lets you validate the receiver without depending on Copilot's clock.

## Step 3 — Verify detection + review

Within ~30 seconds of PR open, the factory listener should:

```bash
chitin-kernel chain tail --event-type copilot_pr_detected | jq '.payload'
```

Expected: a record with `repo`, `pr_number`, `pr_url`, `spec_ref: "099-fixture"`, `issue_number: <NUMBER>`, diff stats, `detected_at`.

The spec 094 `PRReviewWorkflow` should also be visible in Temporal:

```bash
temporal workflow list --query 'WorkflowType="PRReviewWorkflow" AND CustomKeywordField="099-fixture"'
```

Expected: one running workflow.

## Step 4 — Verify the verdict comment

After the PRReview workflow completes (typically <60s with mocked drivers, longer with real ones):

```bash
gh pr view --repo your-org/your-test-repo <PR> --comments
```

Expected: a comment with the dialectic verdict (one of `approve`, `approve_with_comments`, `request_changes`, `abstain`).

And the chain has:

```bash
chitin-kernel chain tail --event-type copilot_review_posted | jq '.payload'
```

Expected: `verdict` matches the comment, `review_run_id` matches the Temporal workflow.

## Step 5 — Verify operator visibility

```bash
chitin-orchestrator copilot-list
```

Expected: one row showing the dispatch, with columns `repo`, `issue_number`, `pr_number`, `spec_ref`, `state=review_posted`, `dispatched_at`.

## Step 6 — Verify idempotency (SC-003)

Re-deliver the `pull_request.opened` webhook 5 times via:

```bash
chitin-orchestrator simulate-webhook \
  --port 8765 \
  --event pull_request \
  --action opened \
  --fixture <recorded-payload.json>
```

Expected: only the first delivery emits `copilot_pr_detected` and starts a workflow; the next 4 return `dedup_skipped: true` and emit only `copilot_pr_activity`.

## Step 7 — Verify telemetry capture (FR-013)

```bash
chitin-kernel chain tail --event-type copilot_pr_activity | jq '.payload.event_type, .payload.event_action'
```

Expected: an entry per webhook delivery (including the deduped ones, since `copilot_pr_activity` is per-delivery, not per-PR).

## Cleanup

```bash
gh issue close <NUMBER> --repo your-org/your-test-repo
gh pr close <PR> --repo your-org/your-test-repo
```

## Failure modes worth knowing

| Symptom | Likely cause |
|---|---|
| `gh: command not found` | `gh` not on PATH; dispatch exits 1 with `gh_not_installed` |
| `gh issue create` returns 422 with `"copilot is not a valid assignee"` | Copilot not installed on the target repo; FR-004 fail loud |
| No `copilot_pr_detected` after PR open | check `X-Hub-Signature-256` matches; check webhook delivery in GitHub's Recent Deliveries page; check chain for `copilot_pr_activity` (if absent, the receiver never got the event) |
| `PRReviewWorkflow` doesn't start | check `temporal workflow list` for failures; check `copilot_review_failed` for `failure_kind: temporal_unreachable` |
| `copilot-list` shows `state=stale` | Copilot didn't open a PR within 24h; check the issue's GitHub activity or close/redispatch |
