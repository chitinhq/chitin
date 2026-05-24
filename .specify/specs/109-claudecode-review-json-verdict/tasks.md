---
description: "Task list — 109 claudecode review-mode StructuredVerdict JSON contract"
---

- [ ] T001 [P] [US1] Implement the review-mode prompt template in `go/orchestrator/driver/claudecode/review_mode.go` — define `reviewPromptFor(wu driver.WorkUnit) string` that embeds the StructuredVerdict JSON schema + an example + the explicit "Return ONLY a JSON document matching this schema" instruction; cite the spec 094 contract URL
- [ ] T002 [P] [US1] Implement the output post-processor in `review_mode.go` — define `extractVerdictJSON(raw string) (string, error)` that strips markdown fences, extracts the largest balanced `{...}` block, and falls back to the raw string when no JSON-shaped substring exists
- [ ] T003 [US1] Implement the review-mode dispatch in `claudecode/driver.go`'s `Invoke` method — when `wu.Tool` is the review-mode discriminator, use `reviewPromptFor`, capture stdout, run through `extractVerdictJSON`, validate via `verdict.Validate`, return `Result{Status: StatusSucceeded}` on success or `Result{Status: StatusFailed, Explanation: "malformed_verdict: ..."}` on failure
- [ ] T004 [US1] Add a unit test in `go/orchestrator/driver/claudecode/review_mode_test.go` for the clean JSON-only response case — assert the driver emits `StatusSucceeded` with a validated verdict body
- [ ] T005 [P] [US2] Add a unit test in `review_mode_test.go` for the markdown-fenced JSON case — assert the post-processor strips the fences and emits `StatusSucceeded`
- [ ] T006 [P] [US2] Add a unit test in `review_mode_test.go` for the prose-only case — assert the driver emits `StatusFailed` with `malformed_verdict` in the explanation and the raw output truncated to 1 KiB
- [ ] T007 [P] [US2] Add a unit test in `review_mode_test.go` for the verdict-validation-failure case (e.g. `verdict=approve` with non-empty `blockers`) — assert `StatusFailed` and the validation error surfaces in the explanation
