# Spec 118 Dispatcher Visibility

## Factory Dispatch Failure Kinds

`factory_dispatch_failed` events carry `payload.failure_kind` plus the legacy
`payload.error` detail.

- `spec_ref_not_found`: the requested spec ref has no matching directory under `.specify/specs/`.
- `spec_ref_ambiguous`: the ref matches more than one spec directory.
- `tasks_md_missing`: the spec directory exists, but `tasks.md` is absent.
- `tasks_md_parse_error`: `tasks.md` exists, but the spec-kit adapter rejected it.
- `temporal_dial_failed`: the scheduler could not connect to Temporal.
- `temporal_start_workflow_failed`: Temporal was reachable, but workflow start failed.
- `capability_mismatch`: at least one task requires a capability no registered driver declares.
- `internal`: closed-taxonomy fallback for anything not yet classified.

## Grep One Class

```sh
rg '"event_type":"factory_dispatch_failed".*"failure_kind":"temporal_dial_failed"' ~/.chitin/events-*.jsonl
```

For a one-shot last-7-days histogram:

```sh
scripts/spec-118-reclassify.py
```

## Triage Silent Drops

`work_unit_completed_without_deliverable` means the activity returned nominal
success but the declared deliverable was missing. In the queue this appears as
reason `silent_drop`.

```sh
go run ./go/orchestrator/cmd/chitin-orchestrator queue --reason silent_drop
```

For rows with a PR number, inspect the PR and activity explanation. For rows
without a PR, use `spec_ref` and `task_id` as the identity, then redispatch or
recover that work unit from the scheduler history.
