---
description: "Task list — 112 parallel-merge serialization"
---

- [ ] T001 [P] [US1] Implement file-scope detection in the spec-kit adapter — add a `Files []string` field to the `Task` struct in `go/orchestrator/adapter/speckit/parse.go` populated from a `files:` per-task annotation in `tasks.md`, then extend `go/orchestrator/adapter/speckit/context.go`'s `NewTaskContext` call to attach the union of `Task.Files` and `adapter.ExtractFilePaths(t.Description)` to `TaskContext.FilePaths`
- [ ] T002 [P] [US1] Implement file-overlap edge injection in the spec-kit edge derivation pass at `go/orchestrator/adapter/speckit/edges.go` — when compiling the DAG, inject dependency edges between any two parallel tasks whose file scopes overlap; emit a `dag_overlap_serialized` chain event per inserted edge
- [ ] T003 [P] [US2] Implement the sibling-rebase activity at `go/orchestrator/activities/sibling_rebase.go` — on a PR merge to main, the activity rebases each in-flight sibling PR onto the new base; emit `sibling_rebase_dispatched` on success or `sibling_rebase_failed` on conflict
- [ ] T004 [P] [US3] Implement the tasks.md frontmatter `files:` parser in `go/orchestrator/adapter/speckit/parse.go` — backward-compatible: tasks without the annotation use the description-derived FilePaths only
- [ ] T005 [P] [US3] Implement the `dag-plan` operator subcommand at `go/orchestrator/cmd/chitin-orchestrator/dag_plan.go` — compile a spec, print the planned DAG with serialization edges marked; add `--json` for machine-readable output
- [ ] T006 [US1] Add a unit test in `go/orchestrator/adapter/speckit/edges_test.go` asserting two parallel tasks touching the same file get an injected edge; two parallel tasks with disjoint files do not
- [ ] T007 [US2] Add an integration test in `go/orchestrator/test/sibling_rebase_test.go` — dispatch two siblings, merge the first, assert the second's branch gets rebased before the operator tries to merge it
- [ ] T008 [US3] Add a regression test in `go/orchestrator/cmd/chitin-orchestrator/dag_plan_test.go` covering both table and `--json` output modes; assert serialization edges are visible in the output
