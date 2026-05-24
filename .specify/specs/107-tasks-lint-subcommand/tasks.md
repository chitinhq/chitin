---
description: "Task list — 107 tasks-lint operator subcommand"
---

- [ ] T001 [P] [US1] Implement the tasks-lint subcommand in `go/orchestrator/cmd/chitin-orchestrator/tasks_lint.go` — define the `cmdTasksLint` entrypoint, parse argv (positional spec-ref + --json + --repo-root flags), resolve repo root, resolve spec ref via the three-tier resolver, compile via the spec-077 adapter, iterate DAG agent nodes, call `adapter.MapCapability` per task, emit table or JSON output, exit 0 iff all classify; reuse `resolveRepoRoot`, `resolveSpecRef`, and the spec-077 adapter from `schedule.go`
- [ ] T002 [P] [US1] Add the subcommand dispatch case for `tasks-lint` in `go/orchestrator/cmd/chitin-orchestrator/main.go` `runMain` switch — route to `cmdTasksLint(args[2:])` and update `printUsage` to list the new subcommand
- [ ] T003 [US1] Add a unit test in `go/orchestrator/cmd/chitin-orchestrator/tasks_lint_test.go` — use `fixtureRepo` helper from `schedule_test.go` to set up a spec with three tasks (two well-formed classifying to code.implement and docs.write, one unclassified "Do the thing"); assert exit code is 1, stdout contains the three rows, stderr names the unclassified task id
- [ ] T004 [US2] Add an integration test in `tasks_lint_test.go` for `--json` output — same fixture as T003, assert `--json` flag produces a parseable JSON array with one object per task, `classified` field is true|false per task
- [ ] T005 [P] Add a regression test in `tasks_lint_test.go` that asserts `tasks-lint` and `runSchedule`'s DAG validation agree on classification for the same fixture spec — the matcher must be the same codepath (SC-002)
