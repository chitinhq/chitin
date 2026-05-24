---
description: "Task list — 108 two-tier driver allowlist (impl vs review)"
---

- [X] T001 [P] [US1] Implement the two-tier driver allowlist plumbing in `go/orchestrator/cmd/chitin-orchestrator/main.go` — refactor `buildRegistry()` to take a `role` parameter (`"impl"` or `"review"`); for `impl`, filter by `CHITIN_DRIVER_ALLOW_IMPL` env first, fall back to `CHITIN_DRIVER_ALLOW`; for `review`, filter by `CHITIN_DRIVER_ALLOW_REVIEW` env first, fall back to `CHITIN_DRIVER_ALLOW`; unset = no filter
- [X] T002 [P] [US1] Implement the call-site updates so the schedule subcommand passes `"impl"` and the pr-review subcommand passes `"review"` when constructing the registry; verify schedule + pr-review subcommands both build registries against the right pool
- [X] T003 [US1] Add a unit test in `go/orchestrator/cmd/chitin-orchestrator/registry_role_test.go` — set `CHITIN_DRIVER_ALLOW_IMPL=codex` AND `CHITIN_DRIVER_ALLOW_REVIEW=codex,claudecode`, build both registries, assert impl registry has 1 driver and review registry has 2 drivers
- [X] T004 [US2] Add a regression test in `registry_role_test.go` for backward compatibility — set only `CHITIN_DRIVER_ALLOW=codex,claudecode`, build both registries, assert both have 2 drivers (no behavior change for existing operators)
- [ ] T005 [US3] Extend the validate-driver-coverage subcommand in `go/orchestrator/cmd/chitin-orchestrator/validate_driver_coverage.go` to show both impl and review pools per capability when they differ; add an integration test asserting the new columns render correctly
