# Implementation Plan: PR Review Mechanism

**Branch**: `feat/094-pr-review-mechanism` | **Date**: 2026-05-23 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/094-pr-review-mechanism/spec.md`

## Summary

A deterministic, orchestrator-driven PR review workflow that the spec 093 merge orchestrator spawns as a child workflow before merge. Two primary reviewer drivers (selected by capability tag from the spec 075 registry) run in parallel; if they agree on any approve-shaped verdict, the gate passes; if they agree on `request-changes`, the gate halts; on any other combination (disagreement, abstain, or single failure), the workflow dispatches an arbiter whose verdict is final. Arbiter type is class-routed via two new policy-table fields (`review_required`, `arbiter_type`) added to spec 093 in a v1.x amendment: `governance` and `spec-only` route to the operator; the remaining classes route to a third machine driver (operationally degenerate at v1 ship — see Assumptions). Every reviewer — primary, arbiter, machine, or operator — emits the same `StructuredVerdict` shape (4-value enum + three free-text lists + optional reason); the workflow validates the shape on receipt and treats malformed verdicts as failed outcomes.

The technical approach reuses the existing chitin orchestrator worker (Go, single task queue `"chitin"`), the spec 070 worker substrate, the spec 075 driver capability registry, the spec 076 `SelectDriver` activity, and the spec 080 Discord notifier. The review workflow itself is a single Temporal workflow function (`PRReviewWorkflow`) spawned as a child by spec 093's `PRMergeWorkflow`; per-reviewer dispatch is an activity that proxies to the driver's review-mode tool and returns a `StructuredVerdict`. No new service, no new datastore — Temporal workflow history is the system of record per FR-033. The driver-registry extension (adds the `reviewer` capability tag and the review-mode contract requirement) is a small additive change to the spec 075 metadata, not a parallel registry.

## Technical Context

**Language/Version**: Go 1.22 (matches the existing `go/orchestrator/` module that spec 093 also extends).

**Primary Dependencies**: Temporal Go SDK (already vendored); spec 093's `PRMergeWorkflow` (this workflow is its child); spec 075 driver-registry capability metadata (extended with `reviewer` tag); spec 076 `SelectDriver` activity (extended or wrapped — decision deferred to research.md R-SEL); spec 080 Discord notifier; existing OTLP telemetry sink. No new runtime dependency.

**Storage**: Temporal workflow history (FR-033 — "the underlying workflow engine's history is the system of record for the full review chain"). No relational store. No file-on-disk verdict cache. Telemetry OTLP events carry content hashes only (FR-032); raw text lives in workflow history.

**Testing**: Go `testing` package + Temporal `testsuite` for workflow unit tests with fault-injected reviewer outcomes (timeouts, malformed verdicts, mixed verdicts); table-driven tests for the verdict validator (FR-014) and the dialectic aggregator (FR-009); integration test that drives a fixture PR through a real worker with stub reviewer drivers; quickstart-driven manual verification of all 12 success criteria against a real test PR.

**Target Platform**: Linux server, running as part of the same `chitin-orchestrator` worker that hosts spec 093's `MergeQueueWorkflow` and `PRMergeWorkflow`. No new service binary, no new task queue.

**Project Type**: Go module addition — new workflow + activities + verdict-schema package under `go/orchestrator/`, plus a small additive metadata change to the spec 075 driver registry. No CLI surface change at v1 — the workflow is dispatched only by spec 093's parent, never directly by an operator command.

**Performance Goals**: Per SC-009, wall-clock duration of a two-primary dialectic with no arbiter is dominated by the slower of the two primaries within 10% (parallel dispatch verified). Per SC-008, adding a new reviewer-tagged driver requires zero workflow code change. Per-PR review-gate latency is dominated by the reviewer drivers' own response time (1–10 min typical, 30 min bound) — orchestrator overhead per dialectic decision is sub-second.

**Constraints**: Temporal workflow code must be deterministic — no clocks, no I/O, no randomness inside `PRReviewWorkflow`. All side effects (driver dispatch, telemetry emit, Discord notify) flow through activities. Activity timeouts: select-reviewers 5s, dispatch-machine-reviewer 30m with 30s heartbeat (FR-026), validate-verdict 1s, emit-review-telemetry 5s, notify-operator 10s, dispatch-operator-arbiter signal-blocked with no timeout (FR-027) and a 2h re-notification heartbeat (FR-025/029). Re-review and override-review signals are workflow signals on the parent `PRMergeWorkflow`, not on `PRReviewWorkflow`, so a re-review spawns a fresh `PRReviewWorkflow` child rather than mutating an in-flight one (FR-021, FR-015 — verdicts immutable per invocation).

**Scale/Scope**: v1 typical — one `PRReviewWorkflow` per PR, dozens per day in steady state. Two reviewer-tagged drivers at v1 (`hermes`, `openclaw`); pool grows by drivers declaring the capability tag (codex, copilot, gemini, local-llm are candidates for v1.1). Single-worker single-task-queue; concurrent review workflows allowed because each is per-PR isolated.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| § | Rule | Compliance | Evidence |
|---|------|-----------|----------|
| §1 | Side-effect boundary — kernel is the only chain-writer; everything else routes through hermes/openclaw for kanban; no bypass | PASS | This workflow writes no chain events directly. Reviewer driver invocations are tool calls subject to `chitin-kernel gate evaluate` per §7 (kernel-gates-every-tool-call). The workflow does not touch kanban. |
| §2 | Worker + worktree mandatory for every unit of work — primary checkout is never a work surface | PASS | Implementation will be authored in an isolated `feat/094-pr-review-mechanism` worktree, matching the pattern used for #925, #923, #928. Reviewer drivers themselves run wherever they normally run (Discord-side for Ares/Clawta) — no review-side worktree is required because the workflow doesn't itself mutate the repo. |
| §3 | Spec-kit promotion gate — every `triage→ready` ticket has matching `spec.md` | PASS | This spec exists at `.specify/specs/094-pr-review-mechanism/spec.md` and lints clean (verified by PR #928's speckit-lint against the in-flight spec). |
| §4 | Tracked installers — every operator-box script has idempotent installer | N/A | No new operator-box script. The workflow is a Go addition to the existing `chitin-orchestrator` binary. |
| §5 | Board-aware scripts — kanban touching scripts must accept `--board` | N/A | This workflow does not touch kanban. |
| §6 | `swarm/` is transitional housing exception | PASS | New code lives in `go/orchestrator/` (`workflows/`, `activities/review/`, `activities/review/verdict/`), not in `swarm/`. |
| §7 | Swarm is the orchestrator — deterministic orchestration, telemetry at every layer, kernel gates every tool call; no implementation work reaches a driver except via orchestrator dispatch from a work-unit | PASS — DIRECT DOGFOOD | The review workflow IS an orchestrator workload, spawned by spec 093 as a deterministic child. Reviewer drivers are dispatched by `SelectDriver` + per-reviewer activity, never by chat or peer-to-peer — capability-tag selection via spec 076 is the only path. Telemetry per reviewer invocation (FR-032) covers the "telemetry at every observable layer" mandate. Reviewer tool calls (when a reviewer driver reads the PR diff, fetches spec artifacts, etc.) flow through that driver's normal `chitin-kernel gate evaluate` path — the gate is not bypassed by the review-mode surface. The no-self-review exclusion (FR-005) is the same-shape invariant as spec 092's no-driver-bypass. |

**Verdict:** All gates pass on initial check. No constitutional complexity to justify; no `Complexity Tracking` rows needed.

## Project Structure

### Documentation (this feature)

```text
specs/094-pr-review-mechanism/
├── plan.md                                  # This file (/speckit-plan output)
├── spec.md                                  # Feature specification (/speckit-specify output)
├── research.md                              # Phase 0 — decisions with rationale + alternatives
├── data-model.md                            # Phase 1 — entities, verdict shape, workflow I/O types
├── quickstart.md                            # Phase 1 — verification recipes for SC-001 .. SC-012
├── contracts/
│   ├── structured-verdict-schema.md         # Phase 1 — JSON schema for StructuredVerdict (FR-013/014)
│   ├── review-mode-driver-contract.md       # Phase 1 — input/output contract for reviewer drivers (FR-002)
│   ├── workflow-signal-schemas.md           # Phase 1 — re-review / override-review payloads (FR-021/022)
│   ├── operator-arbiter-surface.md          # Phase 1 — chosen surface mechanism per research.md R-OPSURF
│   └── policy-table-amendment.md            # Phase 1 — spec 093 v1.x policy-table amendment (review_required, arbiter_type)
├── checklists/
│   └── requirements.md                      # Spec quality checklist (created by /speckit-specify)
└── tasks.md                                 # Phase 2 — created by /speckit-tasks (NOT this command)
```

### Source Code (repository root)

```text
go/orchestrator/
├── workflows/
│   ├── pr_review.go                         # NEW — PRReviewWorkflow (child of PRMergeWorkflow)
│   └── pr_review_test.go                    # NEW — testsuite-based unit tests (all dialectic paths, fault injection)
├── activities/
│   ├── review/                              # NEW package — namespaced review activities
│   │   ├── select_reviewers.go              # Activity: capability filter + no-self-review + health filter → ReviewerSlate
│   │   ├── dispatch_machine_reviewer.go     # Activity: drive a reviewer driver via its review-mode tool, await StructuredVerdict
│   │   ├── dispatch_operator_arbiter.go     # Activity: surface to operator (per contracts/operator-arbiter-surface.md), await structured verdict
│   │   ├── validate_verdict.go              # Activity: schema validation per FR-014 (invariants on each enum value)
│   │   ├── emit_review_telemetry.go         # Activity: OTLP event per FR-032 (driver id, role, verdict, content hashes, elapsed)
│   │   ├── notify_operator.go               # Activity: Discord notify per FR-025 (dispatch + heartbeat re-notify)
│   │   └── review_test.go                   # Activity-level tests (fault injection, malformed verdicts)
│   └── review/verdict/                      # NEW package — StructuredVerdict types + invariants
│       ├── verdict.go                       # StructuredVerdict struct, enum, JSON marshalling
│       ├── invariants.go                    # FR-014 invariants as pure functions (table-driven)
│       └── verdict_test.go                  # Table-driven invariant tests including all malformed cases
├── selectdriver/                            # MODIFIED (spec 076 substrate)
│   └── select_driver.go                     # MODIFIED — accept `requireCapability` and `excludeIdentities` params (additive)
├── registry/                                # MODIFIED (spec 075 substrate)
│   └── capability_metadata.go               # MODIFIED — add `reviewer` capability tag constant + ReviewMode shape
└── cmd/chitin-orchestrator/
    └── main.go                              # MODIFIED — register PRReviewWorkflow and the new review activities
```

**Structure Decision**: Single Go module addition rather than a new microservice or new binary. New workflow lives in the existing `workflows/` package next to spec 093's `pr_merge.go`; new activities are namespaced under `activities/review/` so the blast radius is isolated and package-level testing is straightforward. The `StructuredVerdict` schema and its invariants live in their own `verdict/` sub-package so the validator (FR-014) is testable without any Temporal context — pure functions over pure data. Spec 075 and spec 076 are extended in place (additive only — new capability tag constant + two new parameters on `SelectDriver`); no parallel registry, no fork of the selection activity. The CLI surface is unchanged at v1 because the workflow is dispatched only by spec 093's parent; if a v1.x operator-facing `review submit <pr>` is later needed it can hang off `cmd/chitin-orchestrator/`.

The two-spec coupling (094 spawns from 093, 094 amends 093's policy table) is intentional and documented in spec 093's Assumptions ("spec 093 v1.x amendment after 094 ratifies"). The amendment is a small additive change — two columns added to the existing 6-class table — captured under `contracts/policy-table-amendment.md` in this feature so spec 093's plan does not need to be re-opened until the actual amendment lands.

## Complexity Tracking

> Constitution Check passed without violations. This section is intentionally empty.
