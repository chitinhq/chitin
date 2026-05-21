# Tasks: Information Ingestion Pipeline

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md)

## Format: `[ID] [P?] [Story] Description`

- **[P]** = parallelizable (different files, no incomplete dependency)
- **[US1/US2/US3]** = the user story a task serves (story phases only)

## Path Conventions

Packages within the spec-070 orchestrator module — the ingest package
`go/orchestrator/ingest/`, the pipeline workflow `go/orchestrator/workflows/`,
the knowledge-base projection activity `go/orchestrator/activities/`.
Depends on spec 076's scheduler/DAG (`deterministic` nodes), spec 075's
driver registry (tool-equipped gathering agents + the local-LLM driver for
the optional classifier model), and the kernel's typed-egress / trust gate.

---

## Phase 1: Setup (Shared Infrastructure)

- [ ] T001 Create the `go/orchestrator/ingest/` package skeleton — `item.go`, `trust.go`, `fetch.go`, `gather.go`, `filter.go`, `verdict.go`, `dedup.go` with package doc and exported stubs (plan.md Project Structure)
- [ ] T002 Create the pipeline workflow file skeleton at `go/orchestrator/workflows/ingestion.go` — package, imports, workflow registration stub (FR-017)
- [ ] T003 [P] Wire `workflowcheck` against `go/orchestrator/workflows/ingestion.go` in the orchestrator CI determinism gate (FR-017, plan.md Constraints)

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ The Normalized Item, Trust Marker, and Filter Verdict types are pure (no Temporal import). They block every user story.**

- [ ] T004 Implement the Normalized Item type in `go/orchestrator/ingest/item.go` — a uniform representation (source ref, content, provenance, trust marker) every fetched source becomes, regardless of medium — web page, paper, article, video (FR-004, Key Entities: Normalized Item)
- [ ] T005 Implement the Trust Marker type in `go/orchestrator/ingest/trust.go` — the provenance class `operator-seeded` (high trust) or `gathered`, recorded and carried into the filter; raises trust but never bypasses filtering (FR-002, FR-008, Key Entities: Trust Marker)
- [ ] T006 Implement the Filter Verdict type in `go/orchestrator/ingest/verdict.go` — the per-item outcome: kept with a rank, dropped with a recorded reason, or held for operator review (FR-007, FR-010, Key Entities: Filter Verdict)
- [ ] T007 [P] Unit-test `go/orchestrator/ingest/` types in `item_test.go`, `trust_test.go`, `verdict_test.go` — boundaries: a video and a web page normalize to the same shape, an operator-seeded marker raises trust but a constructed verdict can still be `dropped`, a verdict covers kept/dropped/held exhaustively (FR-002, FR-004, FR-007, FR-008)

**Checkpoint**: the pipeline's pure core compiles and is tested — the workflow can now build on it.

## Phase 3: User Story 1 — Operator feeds a link into the pipeline (Priority: P1) 🎯 MVP

**Goal**: a single pipeline workflow accepts an operator-submitted URL, fetches it under kernel governance, reads it into a normalized item, passes it through the filter carrying an operator-seeded trust marker, and surfaces it in the knowledge base — nothing in code or policy changed.

**Independent test**: feed the pipeline a known URL; confirm it is fetched under kernel governance, read into a normalized item, passed through the filter (entering with an operator-seeded trust marker), and surfaced in the knowledge base — and that nothing in code or policy changed.

- [ ] T008 [US1] Implement the kernel-gated fetch + read activities in `go/orchestrator/ingest/fetch.go` — fetch a source through the kernel's typed-egress gate, read its content into a Normalized Item; the fetch is an inspectable, kernel-gated action (FR-004, FR-012; US1 acceptance scenario 1)
- [ ] T009 [US1] Implement the operator-fed entry path in `go/orchestrator/ingest/item.go` — accept a specific URL/article/video submitted directly by the operator and construct its item with an `operator-seeded` high-trust marker (FR-001, FR-002; US1 acceptance scenario 2)
- [ ] T010 [US1] Implement a pass-through filter in `go/orchestrator/ingest/filter.go` — a placeholder the real filter (US3) replaces; every item still flows through it, the operator-seeded provenance recorded as it passes (FR-005, FR-008; US3 story note: filter starts as pass-through)
- [ ] T011 [US1] Implement the pipeline workflow in `go/orchestrator/workflows/ingestion.go` — operator-fed item → fetch → read → filter → surface; a durable, individually-inspectable workflow run (FR-001, FR-017)
- [ ] T012 [US1] Implement the knowledge-base projection activity in `go/orchestrator/activities/knowledge_base.go` — project kept, ranked items into the knowledge base available to spec 078; the pipeline output MUST NOT change code, policy, or configuration (FR-011; US1 acceptance scenario 3; SC-005)
- [ ] T013 [US1] Record an operator-fed drop with its reason — an operator-fed item filtered out as low-signal records the drop and its reason; the operator can see why their pick did not survive (FR-007; US1 acceptance scenario 4; edge case: operator-fed item is itself low-signal)
- [ ] T014 [US1] Replay/determinism test for the operator-fed path in `go/orchestrator/workflows/ingestion_test.go` — Temporal `testsuite`; a known URL is fetched as a kernel-gated action, normalized, filtered, surfaced; nothing in code or policy changed (FR-001, FR-012; SC-001, SC-005; US1 Independent Test)

**Checkpoint**: the operator-fed path works end to end — one hand-picked link fetched, filtered, and surfaced. The MVP.

## Phase 4: User Story 2 — Broad-net gathering on a topic (Priority: P2)

**Goal**: a tool-equipped gathering agent casts a broad net on a named topic, produces multiple candidate sources, and feeds every candidate into the identical fetch → read → filter path carrying a `gathered` trust marker; every fetch is kernel-gated; duplicates and failed fetches are handled without failing the run.

**Independent test**: give a gathering agent a fixed topic; confirm it produces multiple candidate sources via its search/browse tools, that every candidate enters the same fetch → read → filter path as an operator-fed item, and that each gathering action is kernel-gated.

- [ ] T015 [US2] Implement the broad-net gathering activity in `go/orchestrator/ingest/gather.go` — invoke a tool-equipped agent (web search, X/social search, browser, document reading) via the spec-075 driver contract on a named topic; produce multiple candidate sources; the pipeline does not re-implement those tools (FR-003; US2 acceptance scenario 1)
- [ ] T016 [US2] Route every gathered candidate through the identical fetch → read → filter path as an operator-fed item, carrying a `gathered` (not operator-seeded) trust marker (FR-001, FR-004; US2 acceptance scenario 2)
- [ ] T017 [US2] Enforce kernel-gated egress on every gathering fetch — every fetch and egress passes the typed-egress / trust policy; a fetch to a domain outside the trust policy is denied by the kernel, not silently completed (FR-012; US2 acceptance scenario 3; edge case: egress outside the trust policy)
- [ ] T018 [P] [US2] Implement deduplication in `go/orchestrator/ingest/dedup.go` — a gathered candidate already present in the knowledge base is deduplicated rather than re-ingested (FR-014; edge case: source already in the knowledge base)
- [ ] T019 [US2] Implement the failed-fetch and empty-gather records — a fetch that is unreachable, paywalled, or errors records a failed fetch for that item and the batch continues; a gathering run that finds nothing credible records an empty gather (FR-015; US2 acceptance scenario 4; edge case: unreachable/paywalled source)
- [ ] T020 [P] [US2] Broad-net gathering test in `go/orchestrator/workflows/ingestion_test.go` — a fixed topic yields multiple candidates, each routed through the same fetch → read → filter path, each fetch kernel-gated, a duplicate skipped, a failed fetch not failing the run (FR-003, FR-012, FR-014, FR-015; SC-002, SC-006; US2 Independent Test)

**Checkpoint**: autonomous breadth works — many candidates gathered, each through the same path, every fetch governed.

## Phase 5: User Story 3 — The signal/noise filter ranks a batch (Priority: P3)

**Goal**: the real deterministic filter replaces the pass-through — it ranks each item for credibility, relevance, and value, keeps the high-signal items, drops the low-signal ones with a recorded reason, holds the unsure ones for operator review, and produces identical decisions on repeated runs.

**Independent test**: feed the filter a batch mixing known high-signal and known low-signal items; confirm it ranks them, keeps the high-signal ones, drops the low-signal ones, records a per-drop reason, and that only kept items reach the knowledge base.

- [ ] T021 [US3] Implement the real signal/noise filter in `go/orchestrator/ingest/filter.go` — replace the pass-through: assess each item for credibility, relevance, and value and produce a rank; deterministic — the same batch yields the same ranking and keep/drop decisions on every run (FR-005, FR-006, FR-009; US3 acceptance scenarios 1, 3)
- [ ] T022 [US3] Implement the drop-with-reason path in `go/orchestrator/ingest/filter.go` — a low-signal item is dropped with a recorded, auditable reason and never reaches the knowledge base; an operator-seeded marker raises trust but does not let an item bypass the filter (FR-007, FR-008; US3 acceptance scenario 2; edge case: operator-fed item is low-signal)
- [ ] T023 [US3] Implement the hold-for-operator-review path — an item the filter cannot confidently assess is held for operator review, never silently kept and never silently dropped (FR-010; US3 acceptance scenario 4)
- [ ] T024 [P] [US3] Plug the optional small classifier model into the filter via the spec-075 local-LLM driver, with a deterministic-heuristic fallback — when the classifier model is unavailable the filter falls back to its deterministic heuristics and marks affected items for operator review, never waving a batch through unfiltered (FR-006, FR-010; edge case: classifier model unavailable)
- [ ] T025 [P] [US3] Filter-determinism test in `go/orchestrator/ingest/filter_test.go` — a batch mixing known high- and low-signal items: 100% of high-signal kept, 100% of low-signal dropped each with a reason, identical ranking and keep/drop decisions across 100 repeated runs, only kept items reach the knowledge base (FR-007, FR-009; SC-003, SC-004, SC-005; US3 Independent Test)

**Checkpoint**: the filter is the rigorous gate the thesis demands — deterministic, auditable, between gathering and everything downstream.

## Phase 6: Polish & Cross-Cutting

- [ ] T026 [US1] Implement prompt-injection containment in `fetch.go` and `filter.go` — fetched external content is treated as untrusted data, never as instructions; the read and filter stages never act on directives embedded in fetched content; covered by a contract test in `go/orchestrator/workflows/ingestion_test.go` (FR-013; SC-007; edge case: hostile content)
- [ ] T027 [US2] Implement bounded batch size — the pipeline bounds batch size and queues candidates exceeding the bound for a later cycle, never dropping them silently (FR-016; edge case: candidate flood)
- [ ] T028 [P] Emit per-run telemetry from the pipeline workflow to the Chitin Telemetry layer — items gathered, fetched, filtered kept/dropped with reasons — so ingestion is itself observable and itself an input to spec 078's loop (FR-018; 070 FR-008)
- [ ] T029 [P] Confirm `workflowcheck` passes on `go/orchestrator/workflows/ingestion.go`, and confirm the filter and deduplication stages run as spec-076 `deterministic` nodes at zero frontier-token cost (FR-017; 076 FR-017; SC-008)
- [ ] T030 Re-run the Constitution Check — all six principles still PASS post-implementation

---

## Dependencies

- **Phase 1 → Phase 2 → Phase 3**: Setup and the pure item/trust/verdict core block all stories.
- **Phase 2 (the pure core)** is the hard prerequisite — the Normalized Item, Trust Marker, and Filter Verdict types must exist and be tested before any workflow builds on them.
- **US1 (P1)** is the MVP — independently shippable once Phases 1+2 are done; its filter is a pass-through that US3 replaces. Depends on the kernel's typed-egress gate and the orchestrator's worker host (spec 070).
- **US2 (P2)** depends on Phase 3 (the running pipeline + the fetch/read path); it adds the autonomous gathering front-end and routes every candidate through US1's proven path. Needs spec 075's driver contract for tool-equipped agents.
- **US3 (P3)** depends on Phase 3 (the pipeline + the pass-through filter it replaces); it makes the filter the rigorous deterministic gate. Independent of US2 — the filter ranks a batch regardless of how the batch was gathered.
- Within a story: types/library before workflow; workflow before its replay test; the pass-through filter before the real filter.

## Parallel Execution Examples

- Phase 1: T003 in parallel with T001/T002 (distinct concern — CI wiring).
- Phase 2: T007 follows T004–T006 but runs alongside no incomplete dependency once they land.
- Phase 4: T018 (deduplication, distinct file) in parallel with T015–T017; T020 (distinct test file) in parallel with downstream work.
- Phase 5: T024 and T025 in parallel — distinct concerns/files.
- Phase 6: T028 and T029 in parallel — distinct concerns/files.

## Implementation Strategy

**MVP = US1 (operator feeds a link into the pipeline).** Phase 1 + Phase 2
+ Phase 3 deliver the whole machinery — fetch → read → filter → surface —
proven on a single operator-vouched URL, with the filter a simple
pass-through. That alone validates the pipeline end to end and the
kernel-gated-egress boundary. US2 adds autonomous broad-net gathering as
breadth on a proven core; US3 replaces the pass-through with the rigorous
deterministic signal/noise filter — the spec's reason to exist. Each
increment adds value without breaking the prior one.
