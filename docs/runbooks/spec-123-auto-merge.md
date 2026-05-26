# Spec 123 Auto-Merge Runbook

Auto-merge starts from the same `/webhook/pr` listener that handles PR review and sibling-rebase events. A `pull_request.labeled` delivery whose label is `chitin/ready-to-merge` starts `AutoMergeWorkflow` with workflow id `auto-merge-pr-<PR>-<X-GitHub-Delivery>`.

The producer is spec 116: internal re-review applies `chitin/ready-to-merge` after the PR has cleared the multi-driver verdict. Spec 123 is only the mechanical consumer. It checks that the label is still present, the PR is open and not draft, CI is green, and GitHub reports the PR mergeable. The merge action is `gh pr merge <PR> --squash --delete-branch`.

## Failure Modes

CI failed: the workflow emits `auto_merge_ci_failed`, removes the label, posts a PR comment listing failed checks, and sends `DiscordNotify` with reason `auto_merge_ci_failed`.

Merge conflict: the workflow emits `auto_merge_conflict`, removes the label, posts a PR comment naming the conflict condition, and sends `DiscordNotify` with reason `auto_merge_conflict`. It never rebases or resolves conflicts.

CI timeout: the workflow waits with Temporal timers using 60s, 120s, 240s, then 480s polling up to `merge_timeout_seconds` (default 3600). On timeout it emits `auto_merge_ci_timeout`, removes the label, comments, and notifies.

Generic merge failure: if `gh pr merge` fails for branch protection, missing review, permissions, or another non-CI reason, the workflow emits `auto_merge_failed` with stderr capped to 1 KiB and notifies.

## Status

Use the read-only status command:

```sh
chitin-orchestrator auto-merge status <PR>
```

Exit code `0` means the last terminal event was `auto_merge_succeeded`; `2` means the last terminal event was a failure or cancel path; `3` means no auto-merge events were found for that PR.

## Break Glass

Set:

```sh
CHITIN_AUTO_MERGE_DISABLED=1
```

When set, the webhook handler logs the ready-label delivery id and does not start the workflow. It emits no chain event, so disabling the system does not create audit noise.

## Stuck PR

Check CI and the status command first. If the label was removed by a failure branch, fix the underlying problem and re-apply `chitin/ready-to-merge`; a new GitHub delivery id starts a fresh workflow. If a sibling PR merged ahead cleanly, spec 112 still handles sibling rebase fan-out after any successful merge.

This closes the loop observed on 2026-05-26 with PR #1135, where spec 116 and spec 113 got the PR to green and ready but the operator still had to click merge manually. Spec 114 remains the escalation surface for the failure paths.
