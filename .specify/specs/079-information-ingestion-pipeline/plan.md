# Implementation Plan: Information Ingestion Pipeline

**Branch**: `079-information-ingestion-pipeline` | **Date**: 2026-05-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/079-information-ingestion-pipeline/spec.md`

## Summary

The self-improvement loop (spec 078) has two inputs — internal telemetry,
which 078 owns, and external information, which this spec owns. This is the
**external front-end**: cast a broad net for outside knowledge and filter
signal from noise before it informs anything. Two paths feed one pipeline —
an operator hand-feeds a specific URL/article/video as a high-trust seed,
or a tool-equipped agent (web search, X/social search, browser, document
reading) casts a broad net on a named topic — and both route through the
same fetch → read → filter stages.

The crux is the **signal/noise filter**: "the hardest part is disseminating
good information from bad." Every item, fed or gathered, passes a
deterministic filter that assesses credibility, relevance, and value, keeps
the high-signal items, **drops the low-signal ones with a recorded reason**,
and holds the unsure ones for operator review. Only kept items reach the
knowledge base; the pipeline never changes code or policy. All fetches are
**kernel-gated** under the typed-egress / trust policy, and fetched
external content is treated as untrusted **data**, never instructions.

The pipeline runs as durable Temporal workflows + activities inside the
spec-070 orchestrator module; its deterministic stages — the filter, the
deduplication — run as spec-076 `deterministic` nodes to keep continuous
ingestion affordable.

## Technical Context

**Language/Version**: Go 1.25+ — matches the Chitin Kernel and the spec-070 orchestrator module; the Temporal Go SDK is first-class.

**Primary Dependencies**: the Temporal Go SDK; the spec-070 orchestrator (`go/orchestrator/` — worker host, telemetry export); the spec-076 scheduler/DAG for `deterministic`-node execution of the filter and deduplication; the spec-075 driver registry, through which tool-equipped gathering agents (web search, X/social search, browser, document reading) are invoked and through which the filter's optional small classifier model plugs in (the local-LLM driver — the same small-model tier spec 078 relies on); the kernel's typed-egress / trust-policy gate for all network actions.

**Storage**: Temporal's own persistence holds pipeline-workflow state and history. Normalized items and filter verdicts are **projected** to a durable read-model by activities. The knowledge base is the kept-item sink — this spec defines its *inflow*, not its storage, schema, or retrieval (spec assumption). The chitin chain and Chitin Telemetry are written by activities.

**Testing**: `go test`; the Temporal `testsuite` for replay/determinism tests of the pipeline workflow; `workflowcheck` in CI as the determinism gate. A prompt-injection containment contract test (SC-007) and a filter-determinism test over 100 repeated runs (SC-004).

**Target Platform**: Linux, single box (chimera-ant); self-hosted.

**Project Type**: Go packages within the spec-070 orchestrator module — a new `go/orchestrator/ingest/` package (gathering activities, the operator-fed path, the fetch/read normalizer, the signal/noise filter, deduplication) plus the pipeline workflow and a knowledge-base projection activity.

**Performance Goals**: Ingestion is low-throughput (operator-fed items arrive at a human cadence; broad-net gathering volume is bounded accordingly). The goal is **filter rigor + determinism** — the same batch yields the same ranking and keep/drop decisions on every run (SC-004) — and **affordability**: the filter and deduplication run as deterministic nodes at zero frontier-token cost (SC-008).

**Constraints**: The pipeline workflow is a Temporal workflow — it MUST be deterministic: workflow-deterministic time only, never `time.Now`; all side effects (network fetches, knowledge-base writes, chain writes) in activities. The filter MUST be deterministic (FR-009; consistent with 070 FR-003 / 076 FR-005). Every fetch MUST pass the kernel's typed-egress gate (FR-012); a fetch outside the trust policy is denied, not silently completed. Fetched content is untrusted data — the read and filter stages MUST NOT act on embedded directives (FR-013). Batch size is bounded; the remainder is queued, never dropped (FR-016).

**Scale/Scope**: One pipeline workflow, the `ingest/` package (gathering + fetch/normalize + filter + dedup), one knowledge-base projection activity; one operator, one box. Shares the small-model tier and the local-LLM driver with spec 078.

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

| Principle | Assessment |
|-----------|------------|
| §1 Side-effect boundary | PASS — the pipeline *gathers, filters, and surfaces*; its output feeds the knowledge base and MUST NOT change code, policy, or configuration (FR-011). Every side effect (network fetch, knowledge-base projection, chain writes) runs in an activity. Every fetch and egress is kernel-gated under the typed-egress / trust policy (FR-012) — the pipeline relies on that governance, it does not rebuild it. Fetched external content is untrusted data, never instructions (FR-013). |
| §2 Branch & worktree (amended: always workers + worktrees) | PASS — the pipeline is orchestrator workflows; gathering agents run through spec-075 drivers under spec-070's worktree isolation (070 FR-013/14). The pipeline itself spawns no work surface. |
| §3 Spec-kit promotion gate | PASS — 079 has `spec.md` + this `plan.md`; `tasks.md` follows. |
| §4 Tracked installers | N/A — 079 is library + workflow code *inside* the spec-070 orchestrator binary; it ships no standalone operator script. The orchestrator's own installer (070 §4) covers it. |
| §5 Board-aware scripts | N/A — 079 ships no kanban-touching swarm script. |
| §6 Swarm tooling is the exception | PASS — the pipeline is genuine kernel-adjacent infra; it lives under `go/orchestrator/`, not `swarm/`. |

No violations → Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/079-information-ingestion-pipeline/
├── plan.md          # This file
├── research.md      # Phase 0 — fetch/normalize patterns; deterministic-filter design; prompt-injection containment
├── data-model.md    # Phase 1 — Normalized Item / Trust Marker / Filter Verdict / Knowledge Base entities
├── quickstart.md    # Phase 1 — feed one operator URL, watch it fetch → filter → surface
└── tasks.md         # Phase 2 — /speckit-tasks output
```

### Source Code (repository root)

```text
go/orchestrator/
├── ingest/                     # the external ingestion pipeline — pure where it can be
│   ├── item.go                 # Normalized Item — source ref, content, provenance, trust marker
│   ├── trust.go                # Trust Marker — operator-seeded | gathered; raises trust, never bypasses the filter
│   ├── fetch.go                # fetch + read activities — kernel-gated egress, content → normalized item
│   ├── gather.go               # broad-net gathering activity — invokes a tool-equipped agent via the spec-075 driver
│   ├── filter.go               # the signal/noise filter — credibility/relevance/value rank, deterministic
│   ├── verdict.go              # Filter Verdict — kept+rank | dropped+reason | held-for-operator-review
│   ├── dedup.go                # deduplication against existing knowledge-base items
│   └── *_test.go               # unit tests (boundaries: empty gather, failed fetch, hostile content, unsure item, filter determinism)
├── workflows/
│   ├── ingestion.go            # the durable pipeline workflow — gather/feed → fetch → read → filter → surface
│   └── ingestion_test.go       # replay/determinism + injection-containment + bounded-batch tests (Temporal testsuite)
└── activities/
    └── knowledge_base.go       # projects kept, ranked items into the knowledge base (FR-011)
```

**Structure Decision**: A new `go/orchestrator/ingest/` package beside the
spec-078 `loop/` package and the spec-076 `dag/` library, reusing the
spec-070 module layout. The **signal/noise filter and the deduplication
logic are kept pure** (no Temporal import) so they can be exhaustively
unit-tested by `go test` — the filter's determinism (SC-004) is the spec's
crux and must be proven without a Temporal harness. The fetch, read, and
gather steps are Temporal **activities** (network egress is a side effect,
kernel-gated); the pipeline is a **workflow**; the knowledge-base surface
is a projection **activity**. `workflowcheck` guards the workflow layer.
The filter and dedup activities are dispatched as spec-076 `deterministic`
nodes — the pipeline *configures* the 076 scheduler, it does not
re-implement node execution.

## Implementation Phases

The pipeline is built narrow-then-broad: P1 proves the whole machinery on
one operator-vouched item, P2 adds autonomous breadth, P3 makes the filter
the rigorous gate the thesis demands. Each phase is shippable.

- **Phase 0 — Foundation.** Scaffold `go/orchestrator/ingest/`; the
  pipeline workflow file skeleton; wire `workflowcheck` against it. Exit:
  the package compiles, the determinism gate is wired.
- **Phase 1 — The operator-fed path (US1, P1 — the MVP).** The Normalized
  Item and Trust Marker types, the kernel-gated fetch + read activities,
  a pass-through filter, and a single pipeline workflow: operator URL →
  fetch → normalize → filter → surface in the knowledge base, with the
  operator-seeded trust marker recorded. Exit: an operator-fed URL is
  fetched as a kernel-gated action, normalized, filtered, and surfaced;
  nothing in code or policy changed (SC-001, SC-005).
- **Phase 2 — Broad-net gathering (US2, P2).** The gathering activity
  invokes a tool-equipped agent via the spec-075 driver on a named topic;
  every candidate enters the *identical* fetch → read → filter path
  carrying a `gathered` trust marker; deduplication against existing
  knowledge-base items; the failed-fetch and empty-gather records. Exit:
  a gathering run produces multiple candidates, each routed through the
  same path, every fetch kernel-gated (SC-002, SC-006).
- **Phase 3 — The signal/noise filter (US3, P3).** Replace the
  pass-through with the real deterministic filter — credibility,
  relevance, value assessment producing a rank; drop low-signal items
  with a recorded reason; hold unsure items for operator review; the
  optional small classifier model via the spec-075 local-LLM driver, with
  a deterministic-heuristic fallback. Exit: on a mixed batch the filter
  keeps the high-signal and drops the low-signal items, each drop with a
  reason; identical decisions across 100 runs (SC-003, SC-004).
- **Phase 4 — Hardening & polish.** Prompt-injection containment contract
  test (fetched content is data, never instructions); bounded batch size
  with the remainder queued; per-run telemetry emission; `workflowcheck`
  green; re-run the Constitution Check. Exit: injection containment holds
  (SC-007); the filter/dedup stages run as deterministic nodes at zero
  frontier-token cost (SC-008).

## Complexity Tracking

None — no constitution violations to justify.
