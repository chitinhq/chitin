---
description: "Task list — 110 codex review-mode trust-check + StructuredVerdict contract"
---

- [ ] T001 [P] [US1] Implement the review-mode subprocess argv construction in `go/orchestrator/driver/codex/review_mode.go` — define `reviewArgvFor(wu driver.WorkUnit, model string) []string` that returns `["exec", "--skip-git-repo-check", "--model", model, prompt]` for review-mode invocations
- [ ] T002 [P] [US2] Implement the review-mode prompt template + JSON post-processor in `review_mode.go` — define `reviewPromptFor(wu driver.WorkUnit) string` and `extractVerdictJSON(raw string) (string, error)` (parity with spec 109 FR-001 and FR-003)
- [ ] T003 [US1] Wire the review-mode dispatch in `codex/driver.go`'s `Invoke` method — when `wu.Tool` is the review-mode discriminator, build argv via `reviewArgvFor`, capture stdout, run through `extractVerdictJSON`, validate via `verdict.Validate`, return `Result{Status: StatusSucceeded}` on success or `Result{Status: StatusFailed, Explanation: "malformed_verdict: ..."}` on failure
- [ ] T004 [P] [US1] Add a unit test in `go/orchestrator/driver/codex/review_mode_test.go` asserting `--skip-git-repo-check` is in the argv ONLY for review-mode invocations
- [ ] T005 [P] [US3] Add a regression test in `review_mode_test.go` asserting NON-review-mode invocations do NOT include `--skip-git-repo-check` (preserves worktree-trust safety on local-driver work)
- [ ] T006 [P] [US2] Add a unit test in `review_mode_test.go` for the clean JSON-only review response — assert `StatusSucceeded` + validated verdict body
- [ ] T007 [P] [US3] Add a unit test in `review_mode_test.go` for the malformed-prose review response — assert `StatusFailed` + `malformed_verdict` in explanation
