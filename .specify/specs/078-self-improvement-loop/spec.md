# Feature Specification: Self-Improvement Loop

**Feature Branch**: `078-self-improvement-loop`

**Created**: 2026-05-21

**Status**: Draft

**Input**: User description: "chitin is not a code-generation platform — it is a self-improvement loop, and runtime governance is what makes closing that loop safe. The loop: telemetry from every layer → analysis → findings → spec proposals → [human gate] → implementation via the orchestrator → new telemetry. It runs continuously as orchestrator workflows (spec 070). Governance enables autonomy: an agent action that is net-positive and gate-able by the chitin kernel may run with high autonomy because the kernel catches the dangerous case. Most loop steps have mappable decision trees and MUST run as deterministic-activity nodes or a small local model — frontier agents are reserved for genuinely ambiguous work. The loop produces spec proposals; the operator reviews and approves before implementation. It generalizes Sentinel (spec 064)."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Telemetry becomes a reviewable spec proposal (Priority: P1)

The platform's own telemetry — governance decisions, orchestrator run
history, CI outcomes, bench results, PR results — accumulates as the swarm
works. The operator wants the swarm to *learn from itself*: a workflow
ingests that telemetry, analyzes it for recurring failures and missed
opportunities, and emits a **spec proposal** — a concrete, reviewable
change to a chitin spec — that the operator can read, judge, and approve or
reject. The swarm proposes its own next improvement; the operator stays the
authority.

**Why this priority**: This is the thesis — chitin is a self-improvement
loop, not a code factory. The single arc telemetry → analysis →
spec-proposal carries the whole value: it is the smallest slice that closes
the loop, and it *is* the loop's irreducible core. Without it, every other
story is scheduling around an empty center.

**Independent Test**: Stand up the telemetry → analysis → proposal arc as
one orchestrator workflow (spec 070); feed it a fixed telemetry window
containing a known recurring failure; confirm it emits exactly one spec
proposal that names the failure, cites the telemetry grounding it, and is a
concrete diff against a real spec — and that the proposal is queued for the
operator, never applied.

**Acceptance Scenarios**:

1. **Given** a telemetry window with a recurring failure pattern, **When** the loop workflow runs, **Then** it emits a spec proposal naming the pattern and citing the telemetry records that ground it.
2. **Given** a spec proposal is emitted, **When** the operator inspects it, **Then** it is a concrete change to a named spec — not a vague suggestion — and carries its evidence.
3. **Given** a proposal is emitted, **When** the cycle completes, **Then** the proposal is queued for operator review and nothing in code or policy has changed.
4. **Given** the operator rejects a proposal, **When** the next cycle runs, **Then** the rejection is recorded and the loop does not re-propose the identical change without new evidence.

---

### User Story 2 - Review steps run deterministic, not frontier (Priority: P2)

A loop cycle does many review-shaped steps: check a PR against its spec's
acceptance criteria, check code against a deterministic spec, scan a
telemetry window for anomalies, run an end-to-end test suite. Each has a
mappable decision tree — pass/fail against stated criteria. Routing these
to a frontier agent would make a continuously-running loop unaffordable.
The loop runs each such step as a **deterministic-activity node** (spec
076's `deterministic` kind) or via a **small local model** (spec 075's
local-LLM driver) — cheap, fast, consistent. A frontier agent is invoked
only for the genuinely ambiguous step: synthesizing the proposal's prose.

**Why this priority**: This is the economics that makes the loop able to
run *continuously* instead of once. P2 because P1's single arc must prove
out first; the deterministic tier is what turns a one-shot into a
sustainable loop.

**Independent Test**: Run a loop cycle whose work includes a PR-against-spec
review and a telemetry anomaly scan; confirm each review step ran as a
deterministic node or a small-model invocation, that no frontier agent was
invoked for them, and that the cycle's frontier-token cost is bounded to
the proposal-synthesis step alone.

**Acceptance Scenarios**:

1. **Given** a loop step that reviews a PR against its spec's acceptance criteria, **When** the cycle runs, **Then** the step runs as a deterministic node or a small-model invocation — never a frontier agent.
2. **Given** a telemetry anomaly scan, **When** it runs, **Then** it runs as a deterministic activity with zero frontier-agent token cost.
3. **Given** a genuinely ambiguous step — synthesizing proposal prose — **When** the cycle reaches it, **Then** a frontier agent is invoked, and that step alone accounts for the cycle's frontier-token spend.
4. **Given** a step's decision tree is fully mappable, **When** the loop is built, **Then** that step MUST NOT be implemented as a frontier-agent invocation.

---

### User Story 3 - The loop runs continuously on a schedule (Priority: P3)

The loop is not a command the operator runs — it is always on. A scheduled
orchestrator workflow triggers a cycle on a cadence; each cycle ingests the
telemetry accumulated since the last, analyzes it, and queues its proposals
for the human gate. The operator's job shrinks to reviewing a steady,
small stream of evidence-backed proposals.

**Why this priority**: Continuous operation is the end state — the loop
that runs itself. P3 because a single on-demand cycle (P1) and the
affordability that makes continuity viable (P2) must land first.

**Independent Test**: Schedule the loop workflow on a short cadence; let it
run several cycles over a moving telemetry window; confirm each cycle
ingests only telemetry since the prior cycle's checkpoint, queues its
proposals, and that cycles neither overlap nor skip a window.

**Acceptance Scenarios**:

1. **Given** the loop is scheduled, **When** a cycle fires, **Then** it ingests exactly the telemetry since the previous cycle's checkpoint and advances the checkpoint on completion.
2. **Given** several cycles have run, **When** the operator looks at the queue, **Then** it holds the union of all cycles' un-reviewed proposals, each tied to its cycle.
3. **Given** a cycle is still running when the next is scheduled, **When** the cadence fires, **Then** the new cycle does not start until the prior completes — no overlap, no double-ingest.
4. **Given** a cycle produces no finding worth a proposal, **When** it completes, **Then** it records an empty cycle and advances the checkpoint — silence is a valid outcome.

---

### Edge Cases

- A telemetry window is empty or the telemetry layer is unreachable — the cycle MUST complete as an empty cycle and advance its checkpoint, never block the loop or fail loudly.
- A proposal would touch a spec that no longer exists or has been superseded — the loop MUST mark the proposal stale rather than emit a change against a dead spec.
- The same finding recurs every cycle while its proposal sits un-reviewed — the loop MUST NOT re-queue a duplicate; it MUST attach new evidence to the existing pending proposal.
- A deterministic review step's decision tree encounters an input outside its mapped cases — it MUST escalate that single case to a frontier agent, never guess, and never fail the whole cycle.
- The small local model is unavailable — its steps MUST fall back to a deterministic activity where one exists, or be deferred to the next cycle; the loop never silently routes them to a frontier agent.
- An approved proposal's implementation later regresses (new telemetry shows the fix failed) — the next cycle MUST detect the regression and propose a follow-up, closing the loop on its own prior output.
- A proposal is generated for a dangerous or ungated action category — it MUST be refused at synthesis time; the loop only ever proposes within gate-able net-positive categories.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The platform MUST run a self-improvement loop as one or more durable orchestrator workflows (spec 070) — telemetry ingest → analysis → finding → spec proposal — surviving process restarts and individually inspectable.
- **FR-002**: The loop MUST ingest telemetry from every reachable layer — the governance/chitin-chain decision log, orchestrator workflow run history, CI outcomes, bench results, PR outcomes, and agent run telemetry — not a single source.
- **FR-003**: The loop's output MUST be a **spec proposal** — a concrete, reviewable change against a named chitin spec — never a direct change to code, policy, or configuration.
- **FR-004**: Every spec proposal MUST carry its **evidence** — the specific telemetry records that ground it — so the operator reviews a claim with its proof.
- **FR-005**: No proposal MUST EVER be auto-applied. Implementation MUST proceed only after explicit operator approval — the human gate is absolute (generalizes 064 R3).
- **FR-006**: An approved proposal MUST be implemented through the orchestrator (spec 070) and the spec-DAG scheduler (spec 076) — the loop does not have a side channel into the codebase.
- **FR-007**: The loop MUST restrict autonomous agent work to **gate-able net-positive categories** — code generation, PR review, review-against-deterministic-specs, review-against-deterministic-code, end-to-end test authoring, and peer review of tests/code. A proposal for a dangerous or ungated action category MUST be refused at synthesis.
- **FR-008**: A loop step whose decision tree is fully mappable (review a PR against acceptance criteria, check code against a deterministic spec, scan telemetry for anomalies, run e2e tests) MUST run as a `deterministic` node (spec 076 FR-017) or via the small-model driver — it MUST NOT be implemented as a frontier-agent invocation.
- **FR-009**: A frontier agent MUST be invoked only for genuinely ambiguous loop work — primarily synthesizing proposal prose from findings.
- **FR-010**: The loop MUST integrate a **small local model** — runnable on the operator's local GPU — for high-volume deterministic-ish review and classification work, plugged in via the spec-075 local-LLM driver.
- **FR-011**: The loop MUST be schedulable to run continuously on a cadence; each cycle MUST ingest only telemetry since the previous cycle's checkpoint and advance the checkpoint on completion.
- **FR-012**: Cycles MUST NOT overlap — a scheduled cycle MUST wait for the prior cycle to complete rather than double-ingest a window.
- **FR-013**: Every cycle's proposals MUST be queued for operator review; the queue MUST hold the union of all un-reviewed proposals, each attributable to its cycle.
- **FR-014**: The loop MUST NOT re-queue a duplicate of a still-pending proposal; new evidence for an existing finding MUST be attached to the pending proposal.
- **FR-015**: An operator rejection MUST be recorded; the loop MUST NOT re-propose an identical change without new evidence.
- **FR-016**: The loop MUST detect when an approved-and-implemented proposal later regresses (new telemetry shows the change failed its intent) and propose a follow-up — the loop closes on its own prior output.
- **FR-017**: Every loop cycle MUST emit its own telemetry — window ingested, findings, proposals, deterministic-vs-frontier step accounting — to the Chitin Telemetry layer, so the loop is itself observable and itself an input to a later cycle.
- **FR-018**: The loop MUST generalize Sentinel (spec 064): Sentinel's single arc (ingest telemetry → analyze → mine governance-policy proposals) MUST become one configured instance of the loop, not a parallel system; the loop mines improvements to any spec from telemetry anywhere.

### Key Entities

- **Self-Improvement Loop**: the durable orchestrator workflow(s) realizing telemetry → analysis → finding → spec proposal → [human gate] → implementation → new telemetry. Always-on, generalizes Sentinel.
- **Telemetry Window**: the slice of cross-layer telemetry a cycle ingests — bounded by the previous cycle's checkpoint and the current cycle's start.
- **Finding**: an analyzed observation — a recurring failure, a missed opportunity, a regression — derived from a telemetry window, with the records that evidence it.
- **Spec Proposal**: the loop's output — a concrete, reviewable change against a named chitin spec, carrying its finding and evidence. Never auto-applied.
- **Human Gate**: the operator review/approval step every proposal MUST pass before implementation. Authority stays with the human; autonomy is in analysis and proposal generation.
- **Proposal Queue**: the accumulating set of un-reviewed proposals across cycles, each attributable to its cycle and finding.
- **Review Step**: a loop step with a mappable decision tree — runs as a `deterministic` node or a small-model invocation, never a frontier agent.
- **Cycle Checkpoint**: the marker bounding telemetry ingest between consecutive cycles; advanced on cycle completion, including for empty cycles.
- **Gate-able Category**: the closed set of net-positive, kernel-gate-able action categories the loop is permitted to propose autonomous work within.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A loop cycle over a telemetry window containing a known recurring failure emits exactly one evidence-backed spec proposal naming that failure.
- **SC-002**: 100% of loop proposals are queued for operator review; zero are applied to code, policy, or configuration without explicit approval.
- **SC-003**: Across a soak run, every loop step with a mappable decision tree ran as a deterministic node or small-model invocation — zero ran as frontier-agent invocations.
- **SC-004**: A continuous loop cycle's frontier-agent token cost is bounded to the proposal-synthesis step; review and analysis steps contribute zero frontier tokens.
- **SC-005**: The loop runs continuously for 7 consecutive days, each cycle inspectable, with no overlapping cycles and no skipped telemetry windows.
- **SC-006**: A still-pending finding that recurs is never duplicated in the queue across 100 cycles — recurrence attaches evidence to the existing proposal.
- **SC-007**: Sentinel's telemetry → policy-proposal arc runs as one configured instance of the loop, with no remaining parallel Sentinel-only pipeline.
- **SC-008**: A regression in a previously-approved-and-implemented proposal is detected and surfaced as a follow-up proposal within one cycle of the regressing telemetry appearing.

## Assumptions

- Spec 070 (Chitin Orchestrator) provides the durable-workflow substrate the loop runs on; the loop is orchestrator workflows, not a new runtime.
- Spec 076 (Spec-DAG Scheduler) provides the `agent`/`deterministic` node split (076 FR-017) the loop's review steps rely on to run mechanical work as deterministic activities at zero frontier-token cost.
- Spec 075 (Agent Driver Contract) provides the driver layer, including the local-LLM driver (075 FR-014) the small local model plugs in through; standing up and serving the local model is an operational prerequisite, not part of this spec.
- Spec 064 (Telemetry-Spec Feedback / Sentinel) is the existing single-arc precedent — ingest telemetry → analyze → mine governance-policy proposals; 078 generalizes it. 064's absolute operator-gate rule (R3) is carried forward unchanged as FR-005.
- The chitin kernel already gates every agent action; "governance enables autonomy" means the loop may grant high autonomy to net-positive, gate-able categories *because* the kernel catches the dangerous case — this spec relies on that governance, it does not rebuild it.
- The telemetry layer (Chitin Telemetry) is the read surface for all loop ingest; the loop reads telemetry, it does not own telemetry collection.
- The operator is a single human dogfooding the platform; the review gate is sized for a steady, small stream of proposals, not an enterprise review queue.

## Out of Scope

- The telemetry collection layer itself — the loop is a consumer of telemetry, not its producer.
- External-information ingestion — gathering and filtering knowledge from outside the platform is **spec 079 (Information Ingestion Pipeline)**, the loop's other input.
- The driver interface, capability cards, and standing up the local inference endpoint — spec 075 and its operational prerequisites.
- The scheduling algorithm and the `agent`/`deterministic` node mechanics — spec 076.
- Replacing the operator's review judgment with an automated approver — the human gate is, by FR-005, never automated.
- A heuristic or ML optimizer that ranks or auto-prioritizes proposals beyond their declared evidence.
