# Spec 099 — Phase 0 Research

## R1 — GitHub API: gh CLI vs go-github REST

**Decision:** Shell out to the `gh` CLI binary (v2.74.0 already installed at `/home/red/.local/bin/gh`).

**Rationale:**
- Already authenticated via `gh auth login` on the operator host; one credential surface to manage.
- Operator-grade ergonomics: explicit error messages (`gh issue create --assignee copilot` fails loud with the exact reason when Copilot isn't installable on a repo).
- Subprocess-based execution naturally routes through `chitin-kernel gate evaluate` (constitution §1) — the kernel already gates `gh` command lines via existing PreToolUse hooks.
- Avoids pinning a `go-github` major version in `go.mod` for one feature.

**Alternatives considered:**
- `github.com/google/go-github/v58` — would require token plumbing (read from `GH_TOKEN`, fall back to `gh auth token`), retry/backoff handling we'd have to write, and another dependency at the kernel boundary. Rejected.
- Direct `net/http` to api.github.com — same plumbing burden minus type safety. Rejected.

**Risk:** if `gh` is not on PATH the dispatch fails. Mitigation: dispatch path checks `exec.LookPath("gh")` upfront and emits a clear `copilot_dispatch_failed` event with `reason=gh_not_installed`.

## R2 — Webhook routing extension shape

**Decision:** Add a single new ServeMux handler at `/webhook/pr` that consumes `pull_request`, `pull_request_review`, and `issue_comment` events. Keep `/webhook/push` (spec 098) untouched. GitHub's webhook config lets a single endpoint receive multiple event types via the `X-GitHub-Event` header — we dispatch on that header inside the handler.

**Rationale:**
- Mirrors spec 098's existing `/webhook/push` pattern (one path per event family) — easier to grep, easier to deny in lockdown if the spec 099 path is suspect.
- HMAC verification (`X-Hub-Signature-256`) is shared infrastructure with the push handler; extract to a common helper in this PR.
- Operators can configure a single GitHub App webhook with all three event types selected; chitin routes internally.

**Alternatives considered:**
- One handler at `/webhook/github` that dispatches on header — feels cleaner but loses the "path = capability" grep affordance. Rejected.
- Three separate paths (`/webhook/pull_request`, `/webhook/issue_comment`, etc.) — over-engineered; the three event types share the same eligibility logic. Rejected.

## R3 — Idempotent PR detection via chain dedup

**Decision:** Before starting a `PRReviewWorkflow`, query the local chain (`~/.chitin/events-*.jsonl`) via `chitin-kernel chain query` for a prior `copilot_pr_detected` event matching `(repo, pr_number)`. If one exists, return HTTP 200 with `{"dispatched": false, "reason": "already_detected"}` and emit nothing.

**Rationale:**
- Reuses the chain as the source of truth — no new dedup table.
- The chain is append-only and hash-linked, so the dedup check is a deterministic lookup, not a race-prone cache.
- Handles GitHub's at-least-once webhook delivery (FR-008 invariant: 100 redeliveries → 1 workflow start, SC-003).

**Alternatives considered:**
- In-memory map keyed on `(repo, pr_number)` — lost on restart; doesn't survive the listener bouncing during a Temporal restart. Rejected.
- SQLite dedup table — new storage surface for one invariant. Rejected.
- Temporal workflow ID derived from `(repo, pr_number)` with `WorkflowIDReusePolicy = RejectDuplicate` — would work but leaks dedup state into Temporal's namespace and doesn't help when the workflow has completed and the same webhook redelivers. Rejected.

**Implementation note:** `chitin-kernel chain query --event-type copilot_pr_detected --filter 'payload.repo=$REPO AND payload.pr_number=$N'` — if this CLI doesn't exist yet, the dispatcher can scan the jsonl directly (read-only, no kernel boundary violation). Verify in Phase 1.

## R4 — Driver registry / capability tag for Copilot

**Decision:** Copilot is already enumerated in the constitution §7 driver table (`copilot | dispatched (cloud) | PR-review / second-opinion driver`). For this spec the `--driver copilot` flag is an **explicit operator override** path (per US3), not capability-routed via `SelectDriver`.

**Rationale:**
- Spec is explicit (FR US3): driver choice is operator-explicit, not auto-routed.
- `SelectDriver` (spec 076) capability-matching is the auto-route path. Spec 099 bypasses it for Copilot specifically. Future spec 100 (potential) can encode auto-routing once we have throughput / quality data from operator-explicit dispatches.

**Implementation note:** `--driver` flag accepts an opaque string; the orchestrator only special-cases `copilot` (creating an issue instead of starting a SchedulerWorkflow). Any other `--driver` value either matches an existing local driver via SelectDriver or fails user-error with `unknown driver: <value>`.

## R5 — `PRReviewWorkflow` invocation surface

**Decision:** Add a new activity `StartPRReviewWorkflow` under `go/orchestrator/activities/review/` that wraps `client.ExecuteWorkflow(PRReviewWorkflow, ...)`. The factory-listen handler invokes this activity via a fresh Temporal workflow (a thin "router" workflow that just starts the review), so the dispatch itself is auditable.

**Rationale:**
- Direct `client.ExecuteWorkflow` from the HTTP handler works but leaves no Temporal record of the dispatch decision (only the resulting review workflow shows up). The router-workflow pattern matches what spec 098 already does for `runSchedule` (push-driven dispatch creates a `SchedulerWorkflow`).
- The router workflow is trivial (one activity call); replay-safe.

**Alternatives considered:**
- Call `client.ExecuteWorkflow(PRReviewWorkflow, ...)` directly from the HTTP handler — works but inconsistent with spec 098's pattern.
- Synchronous in-process invocation — couples the HTTP receiver to PRReview latency; if review takes 5 minutes the webhook caller times out. Rejected.

## R6 — Chain event emission pattern

**Decision:** Reuse the existing `emitChainEvent(ctx, eventType, runID, payload, stderr)` helper at `go/orchestrator/cmd/chitin-orchestrator/emit.go:82`. Add typed wrappers `emitCopilotDispatched`, `emitCopilotPRDetected`, `emitCopilotReviewPosted`, `emitCopilotReviewFailed`, `emitCopilotPRActivity`, `emitCopilotDispatchStale` paralleling the existing `emitSchedulerStarted` / `emitSchedulerCanceled` pattern.

**Rationale:**
- Fail-soft on chain-emit failure is the existing convention (spec 097 research.md D8); preserved.
- Typed wrappers per event type prevent payload-shape drift between callers.

## R7 — `--driver` flag interaction with existing `schedule` flow

**Decision:** Add the flag to the existing flag.FlagSet inside `runSchedule`. Branch on its value AFTER spec resolution (steps 3–4 of the schedule.go header comment: resolve --repo-root, --temporal-host, resolve spec-ref, compile). Validation + DAG check (steps 5) still run — we want to fail user-error if the spec is malformed before we create the GitHub issue.

**Rationale:**
- Spec validation is cheap and catches obvious operator typos (wrong spec ref, missing tasks.md) before consuming Copilot's slot.
- Skips DAG dispatch + Temporal client dial (steps 6–8) on the Copilot branch since no SchedulerWorkflow starts.

**Implementation note:** the Copilot branch path replaces steps 6–8 with: `gh issue create`, `emitCopilotDispatched`, print success + URL.

## R8 — Testing strategy

**Decision:** Three test layers per the existing pattern in `go/orchestrator/cmd/chitin-orchestrator/`:

1. **Unit (`copilot_dispatch_test.go`):** mocks the `gh` binary via `PATH` manipulation (test puts a fake `gh` script earlier on PATH). Asserts the constructed argv, the emitted chain event, and the exit code on success / failure paths.
2. **Integration (extends `factory_listen_test.go`):** sends a synthesized `pull_request.opened` webhook through the HTTP receiver (with a precomputed HMAC), asserts the router workflow starts on Temporal (test embeds a temporal-dev test server, same as spec 097's `factory_e2e_test.go`).
3. **Contract:** validates the chain event payloads against the schemas declared in `contracts/chain-events.md`.

**Rationale:** matches spec 098's e2e test scaffolding; reuses the existing temporal-dev test fixture.

## Resolved NEEDS CLARIFICATION

All technical-context unknowns from plan.md are resolved above. No remaining `NEEDS CLARIFICATION` markers.
