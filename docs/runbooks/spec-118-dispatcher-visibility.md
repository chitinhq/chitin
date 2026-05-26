# Spec 118 Dispatcher Visibility

## factory_dispatch_failed kinds

- `spec_ref_not_found`: the pushed spec ref does not resolve under `.specify/specs/`.
- `spec_ref_ambiguous`: the ref matches more than one spec directory.
- `tasks_md_missing`: the spec directory exists, but `tasks.md` is absent.
- `tasks_md_parse_error`: `tasks.md` exists, but the speckit adapter rejected it.
- `temporal_dial_failed`: the scheduler could not connect to Temporal.
- `temporal_start_workflow_failed`: Temporal connected, but workflow start failed.
- `capability_mismatch`: at least one task required a capability no registered driver declares.
- `internal`: fallback for unclassified failures; if this grows, write a follow-up taxonomy spec.

## Grep one failure class

```sh
rg '"event_type":"factory_dispatch_failed"' "$CHITIN_DIR"/events-*.jsonl \
  | rg '"failure_kind":"temporal_dial_failed"'
```

If `CHITIN_DIR` is unset, use `~/.chitin/events-*.jsonl`.

To estimate the last seven days with the new classifier:

```sh
scripts/spec-118-reclassify.py
```

## Triage silent drops

Silent drops surface as `work_unit_completed_without_deliverable` chain events
and as queue reason `silent_drop`.

```sh
chitin-orchestrator queue --reason silent_drop
```

For a row with a PR number, inspect the PR and its triggering event payload.
For a row with `spec_ref/task_id` and no PR number, the delivery activity
completed without opening its declared PR. Re-run or reclaim that work unit by
spec/task, then check the event payload reason:

- `no_changes_to_commit`: the worktree had no diff.
- `git_push_failed`: commit succeeded, but push failed.
- `gh_pr_create_failed`: push succeeded, but `gh pr create` failed.
- `activity_declined_without_failure`: delivery skipped PR creation without a hard activity error, commonly missing `origin` or unavailable `gh`.
