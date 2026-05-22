# Feature Specification: Information Ingestion Pipeline

**Feature Branch**: `079-information-ingestion-pipeline`

**Created**: 2026-05-21

**Status**: Draft

**Input**: User description: "The self-improvement loop (spec 078) has two inputs — internal telemetry, and external information. This spec is the external front-end: cast a broad net for outside knowledge and filter signal from noise before it informs spec proposals. The hardest part is disseminating good information from bad. Agents with web-search, X/social-search, browser, and document-reading tools gather external information; raw gathered information MUST pass a ranking/filter stage separating credible, relevant, high-value information from noise before it influences anything. The operator can feed a specific URL/article/video in as a high-trust seed. Output feeds the knowledge base and informs 078's spec proposals — it never directly changes code or policy. Ingestion is kernel-gated."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Operator feeds a link into the pipeline (Priority: P1)

The operator reads something worth the swarm knowing — an engineering
post, an X thread, a conference talk, a paper — and feeds its URL into the
pipeline. The pipeline fetches it, reads it, runs it through the
signal/noise filter, and surfaces it into the knowledge base as a
high-trust, operator-seeded item available to the self-improvement loop
(spec 078). The operator's hand-picked input is a first-class path, not an
afterthought.

**Why this priority**: This is the smallest provable slice and the highest-
trust one — a single operator-fed link, fetched, filtered, and surfaced.
It exercises the whole pipeline (fetch → read → filter → knowledge base)
on one item the operator already vouched for, so it proves the machinery
without needing autonomous gathering to work first.

**Independent Test**: Feed the pipeline a known URL; confirm it is fetched
under kernel governance, read into a normalized item, passed through the
filter (entering with an operator-seeded trust marker), and surfaced in the
knowledge base — and that nothing in code or policy changed as a result.

**Acceptance Scenarios**:

1. **Given** the operator submits a URL, **When** the pipeline runs, **Then** the content is fetched, read into a normalized item, and the fetch is a kernel-gated, inspectable action.
2. **Given** an operator-fed item, **When** it reaches the filter, **Then** it carries an operator-seeded high-trust marker and is filtered with that provenance recorded.
3. **Given** an item passes the filter, **When** the pipeline completes, **Then** it appears in the knowledge base available to spec 078, and no code or policy has changed.
4. **Given** an operator-fed item is filtered out as low-signal despite its seed, **When** the cycle completes, **Then** the drop and its reason are recorded — the operator can see why their pick did not survive.

---

### User Story 2 - Broad-net gathering on a topic (Priority: P2)

The operator (or the self-improvement loop) names a topic worth scanning —
durable-execution patterns, agent-evaluation methods, a competitor's
approach. An agent with web-search, X/social-search, browser, and
document-reading tools casts a broad net: it gathers candidate sources
across the open web and social platforms and feeds every candidate into
the same pipeline. Breadth is the point at the gathering stage; the filter
is what makes breadth safe.

**Why this priority**: Autonomous breadth is what makes the pipeline a
*pipeline* rather than a manual inbox. P2 because the operator-fed path
(P1) proves fetch → filter → surface first; broad-net gathering is breadth
added to a proven core.

**Independent Test**: Give a gathering agent a fixed topic; confirm it
produces multiple candidate sources via its search/browse tools, that
every candidate enters the same fetch → read → filter path as an
operator-fed item, and that each gathering action is kernel-gated.

**Acceptance Scenarios**:

1. **Given** a named topic, **When** a gathering agent runs, **Then** it produces multiple candidate sources from web and social search and feeds each into the pipeline.
2. **Given** an autonomously-gathered candidate, **When** it enters the pipeline, **Then** it follows the identical fetch → read → filter path as an operator-fed item but carries a gathered (not operator-seeded) trust marker.
3. **Given** a gathering run, **When** any source is fetched, **Then** every fetch and egress is kernel-gated under the typed-egress / trust policy.
4. **Given** a gathering run finds nothing credible, **When** it completes, **Then** it records an empty gather — breadth that yields no signal is a valid outcome.

---

### User Story 3 - The signal/noise filter ranks a batch (Priority: P3)

A batch of gathered items reaches the filter. The filter ranks each item
for credibility, relevance, and value, keeps the high-signal items, and
**drops the low-signal ones with a recorded reason**. The operator can
audit any drop. This is the crux — "the hardest part is disseminating good
information from bad" — and it MUST stand between raw gathering and
anything the information influences.

**Why this priority**: The filter is the spec's reason to exist; without
it, broad-net gathering is just noise injection. P3 because P1 and P2
deliver a working narrow pipeline whose filter can be a simple pass-through
first; this story makes the filter the rigorous gate the thesis demands.

**Independent Test**: Feed the filter a batch mixing known high-signal and
known low-signal items; confirm it ranks them, keeps the high-signal ones,
drops the low-signal ones, and records a per-drop reason — and that only
kept items reach the knowledge base.

**Acceptance Scenarios**:

1. **Given** a batch of gathered items, **When** the filter runs, **Then** each item receives a credibility, relevance, and value assessment and a resulting rank.
2. **Given** a low-signal item, **When** the filter evaluates it, **Then** it is dropped with a recorded, auditable reason and never reaches the knowledge base.
3. **Given** the same batch, **When** the filter is run twice, **Then** it produces the same ranking and the same keep/drop decisions — the filter is deterministic.
4. **Given** an item the filter cannot confidently assess, **When** it is evaluated, **Then** it is held for operator review rather than silently kept or silently dropped.

---

### Edge Cases

- A fetched source is unreachable, paywalled, or returns an error — the pipeline MUST record a failed fetch for that item and continue with the rest of the batch, never fail the whole run.
- A gathering agent's search tools return a source already in the knowledge base — the pipeline MUST deduplicate against existing items rather than re-ingest.
- An operator-fed item is itself low-signal — the filter MUST still evaluate it; the operator seed raises trust but MUST NOT bypass the filter (FR drop with reason still applies).
- A source's content is hostile — prompt injection embedded in an article or page — the pipeline MUST treat fetched external content as untrusted data, never as instructions; the reading and filtering steps MUST NOT act on embedded directives.
- A gathered source requires egress to a domain outside the trust policy — the fetch MUST be denied by the kernel, not silently completed.
- The filter's classifier model is unavailable — the filter MUST fall back to its deterministic heuristics and mark affected items for operator review, never wave a whole batch through unfiltered.
- A video or long document exceeds practical reading limits — the pipeline MUST extract a bounded, representative reading rather than fail or silently truncate without record.
- A high volume of gathered candidates floods the filter — the pipeline MUST bound batch size and queue the remainder, never drop candidates silently to keep up.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The pipeline MUST accept external information from two paths — operator-fed items (a specific URL / article / video) and autonomous broad-net gathering — and route both through the same fetch → read → filter stages.
- **FR-002**: The operator-fed path MUST be first-class: the operator MUST be able to submit a specific URL/article/video directly into the pipeline, and that item MUST be marked an operator-seeded, high-trust input.
- **FR-003**: Broad-net gathering MUST be performed by agents using their own toolsets — web search, X/social search, browser, document reading (e.g. Hermes's skill set) — invoked through the spec-075 driver contract; the pipeline does not re-implement those tools.
- **FR-004**: Every gathered or fed source MUST be fetched and read into a **normalized item** — a uniform representation (source ref, content, provenance, trust marker) regardless of whether it is a web page, paper, article, or video.
- **FR-005**: All raw gathered information MUST pass a **signal/noise filter** before it influences anything — the filter stands between gathering and the knowledge base, with no path around it.
- **FR-006**: The filter MUST assess each item for credibility, relevance, and value, and produce a rank; it MAY combine deterministic heuristics with a small classifier model.
- **FR-007**: The filter MUST **drop** low-signal items and MUST record a per-drop, auditable **reason**; a dropped item MUST NOT reach the knowledge base.
- **FR-008**: The operator-seeded trust marker MUST raise an item's trust but MUST NOT let it bypass the filter — every item, fed or gathered, is filtered.
- **FR-009**: The filter MUST be deterministic — the same batch MUST yield the same ranking and the same keep/drop decisions on repeated runs (consistent with 070 FR-003 / 076 FR-005).
- **FR-010**: An item the filter cannot confidently assess MUST be held for operator review — never silently kept and never silently dropped.
- **FR-011**: The pipeline's output — kept, ranked items — MUST feed the **knowledge base** and be available to inform spec 078's spec proposals; it MUST NOT directly change code, policy, or configuration.
- **FR-012**: All ingestion actions — web fetches and any network egress — MUST be kernel-gated under the typed-egress / trust policy; a fetch to a domain outside the trust policy MUST be denied.
- **FR-013**: Fetched external content MUST be treated as untrusted **data**, never as instructions; the read and filter stages MUST NOT act on directives embedded in fetched content (prompt-injection containment).
- **FR-014**: The pipeline MUST deduplicate gathered candidates against items already in the knowledge base rather than re-ingesting them.
- **FR-015**: A failed fetch (unreachable, paywalled, error) MUST be recorded for that item and MUST NOT fail the rest of the batch.
- **FR-016**: The pipeline MUST bound batch size; candidates exceeding the bound MUST be queued for a later cycle, never dropped silently.
- **FR-017**: The pipeline MUST run as durable orchestrator workflows (spec 070) — gather, fetch, filter, and surface steps individually inspectable; deterministic stages (the filter, deduplication) SHOULD run as `deterministic` nodes (spec 076 FR-017) rather than frontier agents.
- **FR-018**: The pipeline MUST emit telemetry — items gathered, fetched, filtered kept/dropped with reasons — to the Chitin Telemetry layer, so ingestion is itself observable and itself an input to spec 078's loop.

### Key Entities

- **Information Ingestion Pipeline**: the external front-end of the self-improvement loop — durable orchestrator workflows that gather, fetch, read, filter, and surface external information.
- **Operator-Fed Item**: a specific URL / article / video the operator submits directly — a first-class, high-trust seed into the pipeline.
- **Gathering Run**: an autonomous broad-net scan on a named topic, performed by a tool-equipped agent, producing candidate sources.
- **Normalized Item**: the uniform representation every fetched source becomes — source ref, content, provenance, trust marker — regardless of original medium.
- **Trust Marker**: the provenance class of an item — `operator-seeded` (high trust) or `gathered` — recorded and carried into the filter; raises trust but never bypasses filtering.
- **Signal/Noise Filter**: the ranking/filter stage — credibility, relevance, value assessment — that every item MUST pass; the crux of the spec. Deterministic; heuristics plus an optional small classifier model.
- **Filter Verdict**: the per-item outcome — kept with a rank, dropped with a recorded reason, or held for operator review.
- **Knowledge Base**: the sink for kept, ranked information — the surface spec 078's loop reads to inform spec proposals.
- **Egress Gate**: the kernel's typed-egress / trust-policy check every fetch MUST pass before the network is touched.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator-fed URL is fetched, normalized, filtered, and surfaced in the knowledge base in a single pipeline run, with the fetch recorded as a kernel-gated action.
- **SC-002**: A broad-net gathering run on a fixed topic produces multiple candidate sources and routes every one through the identical fetch → read → filter path.
- **SC-003**: On a batch mixing known high- and low-signal items, the filter keeps 100% of the high-signal items and drops 100% of the low-signal ones, each drop carrying a recorded reason.
- **SC-004**: The filter produces identical ranking and keep/drop decisions across 100 repeated runs over the same batch.
- **SC-005**: Zero items reach the knowledge base without passing the filter; zero pipeline outputs change code, policy, or configuration directly.
- **SC-006**: 100% of fetches and egress are kernel-gated; a fetch outside the trust policy is denied in 100% of attempts.
- **SC-007**: No fetched external content is ever acted on as instructions — prompt-injection containment holds across the contract test.
- **SC-008**: Every pipeline run is inspectable as orchestrator workflow runs, and the filter and deduplication stages run as deterministic nodes with zero frontier-agent token cost.

## Assumptions

- Spec 078 (Self-Improvement Loop) is the consumer of this pipeline's output — the loop has two inputs, internal telemetry (owned by 078) and external information (owned by 079). This spec is the external front-end only.
- Spec 070 (Chitin Orchestrator) provides the durable-workflow substrate; the pipeline is orchestrator workflows, not a new runtime.
- Spec 075 (Agent Driver Contract) provides the driver layer through which tool-equipped gathering agents (web search, X/social search, browser, document reading) are invoked; this spec routes through that contract and does not re-implement agent tooling.
- Spec 076 (Spec-DAG Scheduler) provides the `agent`/`deterministic` node split; the pipeline's deterministic stages (filter, deduplication) run as `deterministic` nodes to keep continuous ingestion affordable.
- The chitin kernel already enforces typed-egress and trust policy on network actions; this spec relies on that governance for FR-012, it does not rebuild it.
- The knowledge base exists (or is provided alongside) as the surface spec 078 reads; this spec defines what is *written into* it, not its storage design.
- The signal/noise filter's small classifier model, where used, plugs in via the spec-075 local-LLM driver — the same small-model tier spec 078 relies on; standing up that model is an operational prerequisite shared with 078, not part of this spec.
- The operator is a single human dogfooding the platform; operator-fed items arrive at a human cadence, and broad-net gathering volume is bounded accordingly.

## Out of Scope

- Internal-telemetry ingestion and the analysis → spec-proposal arc — spec 078; this spec is the *external* input only.
- The knowledge base's storage, schema, and retrieval design — this spec defines its inflow, not its internals.
- Re-implementing agent search/browse/document tooling — agents bring their own; spec 075 routes them.
- The driver interface and standing up the small classifier / local inference endpoint — spec 075 and shared operational prerequisites.
- Acting on filtered information — turning knowledge into a spec proposal is spec 078's job, behind its human gate; this pipeline never changes code or policy.
- A heuristic or ML optimizer beyond the credibility/relevance/value filter — ranking is the declared filter, not an open-ended recommender.
