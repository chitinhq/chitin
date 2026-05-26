# Spec 126 Runbook: Spec Issues

Spec 126 projects the spec lifecycle into one GitHub issue labeled
`chitin/spec`. The issue is operator-facing UI only; dispatch still comes from
the spec-kit `tasks.md` change path.

## What the Issue Contains

Each issue title is `[<spec_ref>] <spec title>`, for example
`[126-spec-issue-for-visibility] Spec issue for visibility`.

The body links to:

- the merged spec PR
- `spec.md` on `main`
- `tasks.md` on `main`
- the current impl PR anchor (`<!-- chitin:impl_pr -->...`)

Find a spec issue with:

```sh
gh issue list --label chitin/spec --state all --search 126-spec-issue-for-visibility
```

## Lifecycle Comments

The issue accumulates fixed-template comments as existing events fire:

- Spec PR merged: issue is created.
- Whole-spec dispatch succeeds: `### Dispatch triggered` with run ID, driver,
  capability, and timestamp.
- Impl PR opens: `### Impl PR opened` with PR URL, branch, and timestamp. The
  body impl-PR anchor is patched to the latest PR URL.
- Dispatch fails: `### Dispatch failed` with spec 118's failure reason. The
  issue remains open.
- Impl PR merges: `### Impl PR merged ✓` with PR URL, merge SHA, and elapsed
  time, then the issue is closed.

A closed `chitin/spec` issue is the "this spec shipped" convention.

## Break Glass

Set this in the orchestrator environment to disable all issue activity:

```sh
export CHITIN_SPEC_ISSUE_DISABLED=1
```

When set, issue activities do no GitHub API work. They emit
`spec_issue_update_failed` with `op=disabled_by_env` so the chain records why
the projection stopped.

## Deduplication

`CommentSpecIssue` checks prior `spec_issue_commented` chain events for the
same `spec_ref` and `template_id`. Duplicate activity retries emit
`spec_issue_comment_skipped` instead of posting a duplicate GitHub comment.

Spec 125's Added-only dispatch filter remains the main protection against
duplicate whole-spec dispatches; this dedup layer protects retries and webhook
replays.

## Manual Reconciliation

If a GitHub Issues API call fails, the activity emits
`spec_issue_update_failed` with `op` and `stderr_tail`; dispatch and PR flows
continue. To reconcile:

1. Query the chain for `spec_issue_update_failed` for the affected `spec_ref`.
2. Find the issue:

   ```sh
   gh issue list --label chitin/spec --state all --search <spec_ref>
   ```

3. Apply the missing issue comment, body patch, or close manually using the
   fixed templates above.

## Related Specs

- Spec 098: factory webhook `/webhook/push`.
- Spec 099: PR webhook `/webhook/pr`.
- Spec 119: whole-spec dispatch is preserved; issues are not dispatch units.
- Spec 125: dispatch reliability dependency that avoids duplicate Modified-path
  dispatches.
