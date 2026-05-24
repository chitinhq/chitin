# Requirements Checklist — 099 GitHub Copilot Driver via Issue Assignment

Design-stage verification. Items marked `[x]` were satisfied during spec authoring; the "Deferred to implementation" section enumerates gates the impl PR must satisfy.

## Scope discipline

- [x] Spec is narrowly the **Copilot** driver, not "all drivers via GitHub"
- [x] Local drivers' dispatch path is explicitly unchanged (SC-004 measurable)
- [x] Driver choice is operator-explicit, not auto-routed (US3)
- [x] No multi-driver budget/routing logic in this spec (deferred to operator judgment)

## Telemetry trade-off (the load-bearing risk)

- [x] Telemetry blind spot is explicitly named as the load-bearing risk
- [x] What we lose is enumerated (tool calls, gate decisions, stop-hook events, error recovery, latency breakdown)
- [x] Partial mitigations are enumerated (PR-event capture FR-013, diff stats, wall-clock latency, spec 094 review verdict)
- [x] Routing implication is named: route to Copilot when implementation is well-understood; keep local for behaviors we want to learn from
- [x] Sentinel compatibility constraint is recorded (Copilot-dispatched runs feed sentinel only at PR-level granularity)

## Producer side (orchestrator → GitHub)

- [x] `--driver copilot` flag on existing `chitin-orchestrator schedule` (no new top-level subcommand for dispatch)
- [x] Issue creation contract specified (title, body, labels, assignee)
- [x] Hard-fail if Copilot is not assignable on the target repo (FR-004)
- [x] Chain event `copilot_dispatched` specified with required payload fields (FR-005)

## Consumer side (GitHub → orchestrator)

- [x] Reuses spec 098's `factory-listen` (no new HTTP transport)
- [x] PR eligibility predicate is pure (FR-007)
- [x] Idempotent PR detection via chain dedup (FR-008; SC-003)
- [x] Hand-off to spec 094's `PRReviewWorkflow` specified (FR-009)
- [x] PR-activity event capture as partial telemetry recovery (FR-013)

## Constitution

- [x] §1 kernel-only chain writer: preserved (emits via existing emit path)
- [x] §6 swarm tooling exception: code lives under `go/orchestrator/`
- [x] §7 swarm is the orchestrator: preserved — Copilot becomes a driver under orchestrator control (dispatch via issue, ingest via PR), not a parallel autonomous system

## Deferred to implementation

1. **Telemetry-recovery test:** chain-emit `copilot_pr_activity` for every PR webhook event, verify the chain contains the full payload (sans auth headers) and is replayable by `chitin-kernel chain-verify`.
2. **Sentinel adapter:** sentinel's analyzer passes must learn to read `copilot_pr_activity` events alongside the existing execution_events. Document the format mapping in a sentinel ADR.
3. **CLI `--driver` flag:** table tests for routing decision (`--driver copilot` → GitHub path; no `--driver` → local SchedulerWorkflow path; `--driver codex` etc → local path; `--driver unknown` → error).
4. **`gh issue create` integration:** wrap `gh` CLI or use the REST API; either way, test for the failure modes in FR-004.
5. **Webhook receiver extension:** spec 098's `factory-listen` already handles `push`; extend to `pull_request.opened`, `pull_request.synchronize`, `pull_request.ready_for_review`, `pull_request.labeled`, `issue_comment.created`, `pull_request_review.*`.
6. **Idempotency replay test:** 100x same `pull_request.opened` webhook → exactly 1 `PRReviewWorkflow` start (SC-003).
7. **`copilot-list` subcommand:** column format, sort order, pagination.
8. **End-to-end demo:** schedule with `--driver copilot` against a test repo, watch Copilot draft a PR, watch orchestrator ingest, watch spec 094 review verdict comment.
9. **Operator runbook:** `docs/operator/copilot-driver.md` — when to choose `--driver copilot`, telemetry expectations, troubleshooting (stale dispatch, missing label propagation, Copilot installation issues).
10. **Decision log entry:** capture the telemetry tradeoff as an operator-facing ADR so the routing implication ("Copilot for well-understood specs, local for behavior-learning specs") is discoverable, not just buried in this spec.
