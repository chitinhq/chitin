# Feature Specification: Spec-DAG Scheduler

**Feature Branch**: `076-spec-dag-scheduler`

**Created**: 2026-05-21

**Status**: Draft

**Input**: User description: "Spec 070 FR-015 requires work sequencing to be derived deterministically (mathematically) from the spec task graph — no heuristic optimizer, no human-managed kanban deciding order. This spec defines the Spec-DAG Scheduler: specs are compiled into a dependency DAG; a deterministic Temporal workflow walks it, computes the runnable frontier, and dispatches each runnable node to a driver (spec 075) by capability. It replaces the kanban pull-loop outright — it is the P1 slice of spec 070."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Deterministic scheduling from the spec graph (Priority: P1)

The operator commits a spec. The scheduler compiles its task graph into a
dependency DAG and, on every tick, computes which work units are runnable —
every dependency satisfied — orders them deterministically, and dispatches
each to a capability-matched driver. The same DAG always produces the same
work order. No board, no human deciding what runs next.

**Why this priority**: This is spec 070's FR-015 made real, and the
replacement for the kanban pull-loop. It is the P1 slice of 070 — the
smallest provable piece of the determinism-and-telemetry thesis.

**Independent Test**: Feed the scheduler a fixed DAG with a known
dependency structure; run a tick; confirm the runnable frontier and
dispatch order are exactly what the topological + priority ordering
predicts, and that replaying the tick produces identical decisions.

**Acceptance Scenarios**:

1. **Given** a DAG, **When** a tick runs, **Then** exactly the nodes whose every dependency is `done` are dispatched.
2. **Given** two nodes are both runnable, **When** they are ordered, **Then** the order is priority descending, then a stable node-id tie-breaker — never insertion or map-iteration order.
3. **Given** the same DAG and node states, **When** a tick is replayed, **Then** it produces identical dispatch decisions.
4. **Given** a node has no satisfiable capability, **When** the tick evaluates it, **Then** it is marked blocked-unroutable naming the capability, and the rest of the frontier still proceeds.

---

### User Story 2 - Discovered work joins the graph (Priority: P2)

A work unit, mid-execution, discovers new work. Instead of a human making a
ticket, the orchestrator appends nodes and edges to the running scheduler
via a signal; the next tick recomputes the frontier including them.
Substantial discoveries are flagged for a spec amendment rather than
silently absorbed.

**Why this priority**: A compile-once DAG cannot absorb what agents learn
while working. P2 because P1's static-DAG scheduling must prove out first.

**Independent Test**: Run a scheduler over a DAG; send an append signal
adding a node that depends on an in-flight node; confirm the new node
becomes runnable only after its dependency completes.

**Acceptance Scenarios**:

1. **Given** a running scheduler, **When** an append signal adds nodes/edges, **Then** the next tick's frontier includes them with dependencies honored.
2. **Given** an append would introduce a cycle, **When** it is received, **Then** it is rejected and the scheduler continues unaffected.
3. **Given** discovered work exceeds a size threshold, **When** it is appended, **Then** it is flagged for a spec amendment, not silently absorbed.

---

### User Story 3 - One scheduler, any repo (Priority: P3)

The same scheduler runs work over any target repository on any base branch —
chitin building chitin, or the platform pulled into ReadyBench building
ReadyBench. The target repo and base ref are inputs to the DAG and its work
units, never hard-coded.

**Why this priority**: This is what makes the platform distributable and
dogfoodable beyond chitin itself. P3 because single-repo (chitin)
scheduling proves the model first.

**Independent Test**: Run the scheduler against two DAGs whose work units
target different repos / base refs; confirm each work unit's worktree is
created from the correct repo at the correct base ref.

**Acceptance Scenarios**:

1. **Given** a DAG whose work units name target repo R and base ref B, **When** a node is dispatched, **Then** its worktree is created from R at B.
2. **Given** two DAGs targeting different repos, **When** both run, **Then** no work unit observes another repo's checkout.
3. **Given** a base ref chosen by the operator, **When** a DAG runs, **Then** that ref is recorded on the run and used for every worktree in that DAG.

---

### Edge Cases

- The compiled DAG contains a cycle — compilation MUST fail with the cycle named; the scheduler never runs a cyclic graph.
- A node's dependency permanently fails — dependents MUST be marked blocked (dependency-failed), never left runnable or silently skipped.
- Every remaining node is blocked — the scheduler MUST surface an explicit stalled-graph state, not spin.
- The scheduler workflow's history approaches the limit — it MUST Continue-As-New, carrying forward the DAG + node states, losing no in-flight dispatch.
- Two ticks could observe wall-clock differently — the scheduler MUST use only workflow-deterministic time; never `time.Now`.
- A dispatched node's child workflow is still running at the next tick — the node MUST NOT be re-dispatched (exactly-once dispatch per node).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The scheduler MUST compile a set of specs into a directed acyclic graph of work-unit nodes and dependency edges (via the adapter, spec 077).
- **FR-002**: The scheduler MUST reject a non-acyclic graph at compile time, naming the cycle.
- **FR-003**: On each tick the scheduler MUST compute the runnable frontier — exactly the nodes whose every dependency is `done`.
- **FR-004**: The runnable frontier MUST be ordered deterministically — priority descending, then a stable node-id tie-breaker; no reliance on map iteration or insertion order.
- **FR-005**: Scheduling MUST be deterministic and replayable — replaying a tick over the same DAG and node states yields identical dispatch decisions (070 FR-003).
- **FR-006**: The scheduler MUST run as a durable Temporal workflow and MUST Continue-As-New to bound history (070 FR-009).
- **FR-007**: For each runnable node the scheduler MUST select a driver via the spec-075 registry by the node's required capability; selection MUST be deterministic (075 FR-005).
- **FR-008**: Each dispatched node MUST run as a child workflow that creates a fresh worktree, invokes the driver, and tears the worktree down (070 FR-013/014).
- **FR-009**: A node MUST be dispatched at most once — a node already running MUST NOT be re-dispatched on a later tick (exactly-once; 070 FR-005).
- **FR-010**: A node with no satisfiable driver MUST be marked blocked-unroutable, naming the missing capability; the rest of the frontier MUST still proceed (075 FR-012).
- **FR-011**: A node whose dependency permanently failed MUST be marked blocked (dependency-failed) and MUST NOT run.
- **FR-012**: The scheduler MUST accept an append signal that adds nodes/edges to the running DAG; an append that would create a cycle MUST be rejected.
- **FR-013**: Each work unit MUST carry a target repository and base ref; the scheduler MUST create every worktree from that repo at that ref.
- **FR-014**: Node state transitions MUST be projected to the Chitin Board read-model by an activity (070 FR-016) — the board reflects scheduler state, never drives it.
- **FR-015**: The scheduler MUST emit a per-tick telemetry record — frontier, dispatches, driver selections and their reasons — to Chitin Telemetry (070 FR-008).
- **FR-016**: A stalled graph (no runnable and no running nodes, undone nodes remain) MUST be surfaced as an explicit state, never a silent spin.

### Key Entities

- **Work-Unit DAG**: the scheduler's input contract — nodes plus dependency edges, acyclic. The normalized form every spec-kit adapter (spec 077) MUST produce. Owned by this spec as the consumer contract.
- **DAG Node**: one work unit — id, source spec/task ref, required capability tag, priority, tier hint, target repo + base ref, worktree requirement, status.
- **Node Status**: `pending` → `runnable` → `running` → `done` | `failed` | `blocked-unroutable` | `blocked-dependency-failed`.
- **Dependency Edge**: a `depends_on` relation; the transitive closure MUST be acyclic.
- **Scheduler Workflow**: the durable Temporal workflow that ticks — frontier, order, dispatch, update, Continue-As-New.
- **Runnable Frontier**: the deterministically-ordered set of nodes dispatchable on a given tick.
- **Tick Record**: the per-tick telemetry — frontier, dispatches, driver selections and reasons.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Replaying any scheduler tick over the same DAG yields identical dispatch decisions across 100 runs.
- **SC-002**: The scheduler runs as one durable workflow for 7 consecutive days (070 SC-005), every tick inspectable.
- **SC-003**: A cyclic graph is rejected at compile time 100% of the time, with the cycle named.
- **SC-004**: Zero nodes are dispatched twice across a soak run (exactly-once).
- **SC-005**: A blocked-unroutable node never stalls the rest of the frontier.
- **SC-006**: The scheduler runs work over at least two distinct target repos (chitin, ReadyBench) with correct per-repo worktree isolation.
- **SC-007**: `workflowcheck` passes on the scheduler workflow — the determinism gate is green.

## Assumptions

- Spec 070 provides the Temporal platform, the worktree package, and telemetry export. Spec 075 provides the driver registry and capability matching. Spec 077 provides the spec→DAG compiler (the kit adapters); 076 owns the DAG's normalized schema as the consumer contract.
- Specs already encode dependency information (spec-kit `tasks.md` ordering and `[P]` markers, OpenSpec phases); spec 077 extracts it. Where a spec's dependencies are ambiguous, the adapter marks them `NEEDS CLARIFICATION` rather than guessing.
- Priority is a property of the node, supplied by the spec (or a declared default); the scheduler does not infer priority heuristically (070 FR-015 — "no heuristic optimizer").
- One operator, one box, low throughput (ticks on the order of minutes) — determinism and observability matter, not QPS.

## Out of Scope

- The driver interface and capability cards — spec 075.
- The per-kit extraction of specs into DAGs — spec 077.
- Re-introducing a human-managed board as a decision input — explicitly forbidden by 070 FR-015/016.
- A heuristic or ML optimizer for ordering — forbidden by 070 FR-015; ordering is purely topological plus declared priority.
