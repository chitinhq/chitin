---
spec_id: 110
title: codex driver — review-mode trust-check + StructuredVerdict contract
status: Draft
owner: chitinhq
created: 2026-05-24
depends_on:
  - 075
  - 094
related:
  - 108
  - 109
---

# Spec 110 — codex review-mode trust-check + verdict contract

## Why

The 2026-05-24 spec 094 dialectic dogfood dispatched codex as the second primary reviewing PR #1007. It failed in 132ms with:

```
driver "codex" failed running work unit "...:p2": exit status 1: 
Not inside a trusted directory and --skip-git-repo-check was not specified.
```

Codex CLI's git-safety check refused to run because the worker worktree at `/tmp/chitin-worktrees/wu-...` isn't on the codex CLI's pre-trusted directory list AND the chitin codex driver doesn't pass `--skip-git-repo-check`. The driver Invoke needs the flag (or operator must pre-trust the worktree-root globally), and — same as claudecode (spec 109) — the review-mode output needs to be a `StructuredVerdict` JSON, which codex's free-form prompt path doesn't currently produce either.

## User stories

### US1 (P1) — codex review-mode passes --skip-git-repo-check

> As spec 094's `DispatchMachineReviewer` invoking codex, the underlying `codex exec` subprocess starts cleanly inside the worker worktree — no git-trust prompt, no exit-1 from the safety check.

**Independent test:** Inject a fake `codex` binary that prints its argv. Invoke chitin's codex driver in review mode against a fresh worktree. Assert the captured argv includes `--skip-git-repo-check`.

### US2 (P1) — codex review-mode produces a StructuredVerdict

> Same contract as spec 109 for claudecode: review-mode output is parseable `StructuredVerdict` JSON. Driver wrapper enforces the shape via prompt template + post-processor.

**Independent test:** Identical structure to spec 109 US1's test, against codex driver.

### US3 (P2) — Schema-violation defense (parity with spec 109 US2)

> If codex's CLI emits non-JSON or invalid JSON, driver wrapper emits `Result.Status=StatusFailed` with `malformed_verdict` detail, not raw model output to the activity.

**Independent test:** Identical to spec 109 US2.

## Functional requirements

### Subprocess invocation

- **FR-001** codex driver `Invoke` in review mode constructs the argv: `codex exec --skip-git-repo-check --model <CHITIN_CODEX_MODEL or default> <prompt>`. The `--skip-git-repo-check` flag is mandatory for review-mode invocations.
- **FR-002** Non-review-mode invocations are unchanged (don't pass the flag — preserves existing safety behavior on local-driver implementation work where worktree trust matters).

### Prompt + post-processing (parity with spec 109)

- **FR-003** Review-mode prompt template embeds the `StructuredVerdict` JSON schema + example + the explicit "Return ONLY a JSON document matching this schema" instruction.
- **FR-004** Output post-processor identical to spec 109 FR-003: strip markdown fences → extract largest balanced `{...}` block → fall back to raw.
- **FR-005** On parse-or-validate failure, emit `Result.Status=StatusFailed`, `Explanation: "malformed_verdict: <error>; raw: <first 1KiB>"`.
- **FR-006** On success, emit `Result.Status=StatusSucceeded`, `Explanation: <canonical-serialized StructuredVerdict>`.

### Tests

- **FR-007** Unit test in `go/orchestrator/driver/codex/review_mode_test.go` covering: (a) `--skip-git-repo-check` is passed only in review mode, (b) clean JSON-only response, (c) markdown-fenced response, (d) prose-only response (malformed), (e) validation-failure response.
- **FR-008** Regression test: a non-review-mode codex invocation does NOT include `--skip-git-repo-check` (preserves safety).

## Success criteria

- **SC-001** Re-running the 2026-05-24 dialectic review on PR #1007 with this + spec 109 fix in place: codex primary returns `StatusSucceeded` and a parseable `StructuredVerdict`.
- **SC-002** Spec 094 `DispatchMachineReviewer` activity classifies the codex outcome as a real verdict (not `FailureError`).
- **SC-003** No regression in codex's existing (non-review) invocation path; worktree-trust safety still applies there.

## Scope

### In scope

- `--skip-git-repo-check` flag in review-mode invocations only
- Prompt template + post-processor (parity with spec 109)
- Test coverage for safety, clean JSON, malformed, validation-failure

### Out of scope

- Pre-trusting worktree paths in the codex CLI's config (operator surface; unreliable)
- Removing the safety check for non-review-mode invocations
- Other drivers (gemini, openclaw) — separate specs if they exhibit the same issue under spec 094 dispatch

## Edge cases

- **Codex CLI version doesn't recognize `--skip-git-repo-check`:** the subprocess fails with "unknown flag"; driver returns `StatusFailed` with the error. Operator upgrades codex.
- **Driver invocation outside a git repo (no worktree):** `--skip-git-repo-check` still works (codex is permissive when both conditions are absent).

## Companion

- **Spec 109** — claudecode driver's StructuredVerdict contract. Both specs surfaced from the same 2026-05-24 dialectic review attempt; both must land for `pr-review` to produce a real verdict on a codex-implemented PR.
