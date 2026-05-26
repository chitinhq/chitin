# Spec 125 Factory-listen Added-only Dispatch

Spec 125 stops factory-listen from re-dispatching an implementation workflow
when an already-implemented spec's `tasks.md` is only modified.

## Dispatch Contract

Factory-listen dispatches from GitHub push payloads only when a path matching
`.specify/specs/<spec-ref>/tasks.md` appears in a commit's `added` list.

It does not dispatch when the same path appears only in `modified`. That case
is implementation noise: whole-spec drivers mark task boxes `[ ]` to `[x]`
before their PRs merge, so the merge commit modifies `tasks.md` after the work
has already completed.

If GitHub reports the same `tasks.md` path in both `added` and `modified` for a
single commit, Added wins. The listener dispatches once and emits no filtered
event for the redundant Modified entry.

## Why This Exists

On 2026-05-26, modify-only `tasks.md` pushes produced duplicate implementation
PRs after the real implementation PR had already merged:

- Observation S1799: operator incident note for the duplicate-dispatch run.
- [PR #1132](https://github.com/chitinhq/chitin/pull/1132) and
  [PR #1133](https://github.com/chitinhq/chitin/pull/1133): duplicate
  dispatches after specs 118 and 120 merged via whole-spec implementation PRs.
- [PR #1138](https://github.com/chitinhq/chitin/pull/1138): duplicate
  dispatch after spec 121's implementation PR merged.

The bug was structural: the listener treated `added ∪ modified` as the dispatch
signal. Spec 125 makes Added the contract and treats Modified as auditable
filter input only.

## Manual Re-dispatch

To intentionally re-run an existing spec, use the manual scheduler path:

```sh
chitin-orchestrator schedule <spec-ref>
```

Example:

```sh
chitin-orchestrator schedule 121-driver-output-blob-store
```

Do not modify `tasks.md` to trigger a re-dispatch. Modified-only changes are
filtered by design.

## Auditing Filtered Events

Each filtered modify-only `tasks.md` match emits:

```json
{
  "event_type": "factory_dispatch_filtered",
  "payload": {
    "spec_ref": "121-driver-output-blob-store",
    "commit_sha": "<push after SHA>",
    "path": ".specify/specs/121-driver-output-blob-store/tasks.md",
    "reason": "modify_only"
  }
}
```

Query the local chain directly:

```sh
rg '"event_type":"factory_dispatch_filtered"' ~/.chitin/events-*.jsonl \
  | jq -c '{ts, spec_ref: .payload.spec_ref, commit_sha: .payload.commit_sha, path: .payload.path, reason: .payload.reason}'
```

The only valid reason today is `modify_only`. New filter causes require a spec
amendment before they are added to the reason taxonomy.

## Deployment Replay Validation

After deploying the rebuilt `chitin-orchestrator` binary:

1. Observe the next spec PR merge in the queue, such as #1136, #1137, or #1139.
2. Confirm the spec dispatches exactly once from the Added path.
3. When that implementation PR merges, confirm no second implementation PR is
   opened for the same spec.
4. Query the chain and confirm the implementation merge's Modified
   `tasks.md` entry produced one `factory_dispatch_filtered` event.

## Context

- Spec 098 introduced the factory-listen push webhook.
- Spec 119 made whole-spec dispatch the normal implementation shape.
- Spec 123's auto-merge path depends on the dispatch signal being reliable; it
  must not auto-merge duplicate implementation PRs produced by modify-only
  noise.
