# Feature Specification: Spec-Kit Adapter

**Feature Branch**: `077-spec-kit-adapter`

**Created**: 2026-05-21

**Status**: Draft

**Input**: User description: "The Spec-DAG Scheduler (076) consumes a normalized Work-Unit DAG, but specs come in different kits — GitHub spec-kit, superpowers, OpenSpec — each with its own files and conventions. This spec defines the Spec-Kit Adapter: a uniform interface that detects which kit a repo uses and compiles its spec artifacts into the one normalized DAG the scheduler consumes. Adding support for a new kit MUST require zero changes to the scheduler."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Compile a spec-kit repo into the DAG (Priority: P1)

chitin's own specs live under `.specify/specs/NNN-name/` (GitHub spec-kit).
The adapter reads a spec's `tasks.md` — its dependency-ordered task list and
`[P]` parallel markers — together with `plan.md` and `spec.md`, and emits a
normalized Work-Unit DAG: one node per task, edges from the declared
ordering, capability and priority carried from task metadata. The scheduler
(spec 076) consumes it unchanged.

**Why this priority**: chitin uses spec-kit; this is the adapter the
platform needs first to dogfood itself. It also establishes the
normalized-DAG production contract every other adapter follows.

**Independent Test**: Point the adapter at a known `specs/NNN-name/`
directory; confirm the emitted DAG has one node per task, edges matching the
`tasks.md` ordering and `[P]` markers, and that it is accepted by the 076
scheduler.

**Acceptance Scenarios**:

1. **Given** a spec-kit spec directory, **When** compiled, **Then** every task in `tasks.md` becomes exactly one DAG node.
2. **Given** `tasks.md` ordering and `[P]` markers, **When** compiled, **Then** non-`[P]` tasks receive sequential dependency edges and `[P]` tasks within a phase are parallel siblings.
3. **Given** the compiled DAG, **When** handed to the 076 scheduler, **Then** it is accepted as a valid Work-Unit DAG with no scheduler change.
4. **Given** a task references an FR or file path, **When** compiled, **Then** that context is carried onto the node so a driver can act without re-reading the kit.

---

### User Story 2 - A second kit, zero scheduler change (Priority: P2)

A repo using a different kit — OpenSpec (`openspec/changes/<name>/` with
proposal/apply/archive and ADDED/MODIFIED/REMOVED deltas), or superpowers
(skill-driven plans) — is compiled by its own adapter into the same
normalized DAG. The scheduler does not know or care which kit produced the
graph.

**Why this priority**: This is the kit-agnostic thesis. One adapter could be
a coincidence; two, with zero scheduler change, is the proof.

**Independent Test**: Implement a second adapter; compile a repo of that
kit; confirm the emitted DAG is structurally valid and the scheduler runs it
with zero diff to scheduler code.

**Acceptance Scenarios**:

1. **Given** a second-kit repo, **When** compiled by its adapter, **Then** the output is a valid normalized Work-Unit DAG.
2. **Given** two adapters, **When** the scheduler runs each output, **Then** no scheduler or orchestrator-core code differs between them.
3. **Given** OpenSpec's brownfield change-deltas (ADDED / MODIFIED / REMOVED), **When** compiled, **Then** each delta becomes a node with its change-kind preserved as node metadata.

---

### User Story 3 - Detect the kit; surface ambiguity honestly (Priority: P3)

The adapter layer detects which kit a repo uses (presence of `.specify/`,
`openspec/`, superpowers skill markers) and selects the adapter
automatically. Where a kit's artifacts leave a dependency or required
capability ambiguous, the adapter emits a `NEEDS CLARIFICATION` marker on
the node rather than guessing — surfacing it to the human-in-the-loop.

**Why this priority**: Detection is convenience; honest ambiguity-marking is
correctness. P1/P2 deliver the working compilers first.

**Independent Test**: Run detection over repos of each kit; confirm correct
adapter selection; feed a spec with an ambiguous dependency; confirm the
node carries `NEEDS CLARIFICATION` rather than an invented edge.

**Acceptance Scenarios**:

1. **Given** a repo, **When** detection runs, **Then** the correct kit adapter is selected, or "no recognized kit" is reported explicitly.
2. **Given** an ambiguous dependency in a spec, **When** compiled, **Then** the node is marked `NEEDS CLARIFICATION` and is not auto-edged.
3. **Given** the canonical project constitution, **When** a repo is prepared, **Then** it is projected into the kit's expected location.

---

### Edge Cases

- A repo uses two kits at once (chitin itself has both `.specify/` and `docs/superpowers/`) — detection MUST report the ambiguity and require an explicit kit choice, never pick silently.
- A spec's `tasks.md` is malformed or unparseable — compilation MUST fail with the file and location, never emit a partial DAG.
- A task references a dependency task id that does not exist — compilation MUST fail, naming the dangling reference.
- A kit carries no dependency information at all — every node becomes a parallel sibling and the adapter records that the graph is dependency-free.
- A spec is updated after its DAG was compiled — recompilation MUST produce a DAG diff (added / removed / changed nodes), never a silent wholesale replacement of in-flight work.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The platform MUST define a single `SpecKitAdapter` interface; the scheduler obtains a Work-Unit DAG ONLY through it.
- **FR-002**: Adding support for a new spec kit MUST require only a new adapter — zero changes to the scheduler (076) or orchestrator core.
- **FR-003**: Every adapter MUST emit the normalized Work-Unit DAG defined by spec 076 — nodes, dependency edges, acyclic.
- **FR-004**: A spec-kit adapter MUST compile a `.specify/specs/NNN-name/` spec — one DAG node per `tasks.md` task; edges from task ordering and `[P]` markers; capability and priority carried from task metadata.
- **FR-005**: An adapter MUST carry each task's context (FR references, file paths, the spec/plan text it needs) onto the node so a driver can act without re-reading the kit.
- **FR-006**: At least two adapters MUST be delivered — spec-kit and one of {OpenSpec, superpowers} — proving kit-agnosticism.
- **FR-007**: An OpenSpec adapter MUST preserve brownfield change-kind (ADDED / MODIFIED / REMOVED) as node metadata.
- **FR-008**: The adapter layer MUST detect which kit a repo uses; on detecting more than one, it MUST require an explicit choice rather than guess.
- **FR-009**: Where a kit's artifacts leave a dependency or required capability ambiguous, the adapter MUST emit a `NEEDS CLARIFICATION` marker on the node — never invent an edge or a capability.
- **FR-010**: Malformed or unparseable kit artifacts MUST fail compilation with the file and location — never a partial DAG.
- **FR-011**: A dangling dependency reference MUST fail compilation, naming the missing target.
- **FR-012**: Recompiling a changed spec MUST produce a DAG diff (added / removed / changed nodes), not a silent wholesale replacement of in-flight work.
- **FR-013**: The adapter MUST map a canonical project constitution into each kit's expected location (e.g. `.specify/memory/constitution.md` for spec-kit).
- **FR-014**: Capability tags an adapter assigns to nodes MUST come from the spec-075 capability taxonomy; an unmappable task MUST be marked `NEEDS CLARIFICATION`, never given an invented tag.

### Key Entities

- **SpecKitAdapter**: the uniform interface — detect, and compile a repo's specs into a normalized Work-Unit DAG.
- **Kit**: a spec methodology — spec-kit, superpowers, OpenSpec — each with its own file layout and conventions.
- **Adapter Registry**: the set of available adapters; selects one per repo by detection.
- **Normalized Work-Unit DAG**: the output contract — defined by spec 076, identical regardless of source kit.
- **Task Context**: the per-node payload — FR references, file paths, spec/plan excerpts — extracted from the kit.
- **Constitution Projection**: the canonical project principles written into each kit's expected location.
- **DAG Diff**: the result of recompiling a changed spec — added / removed / changed nodes.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A new kit is supported by adding one adapter, with zero diff to the scheduler or orchestrator core (measured by diff).
- **SC-002**: The spec-kit adapter compiles chitin's own `specs/` into a DAG the 076 scheduler runs.
- **SC-003**: Two kits compile to structurally-valid DAGs through the one interface.
- **SC-004**: 100% of ambiguous dependencies surface as `NEEDS CLARIFICATION` — zero invented edges, verified on a fixture with known ambiguity.
- **SC-005**: Malformed artifacts fail compilation with a precise location 100% of the time — zero partial DAGs.
- **SC-006**: Recompiling a changed spec yields a correct DAG diff, verified against a known before/after pair.

## Assumptions

- Spec 076 owns and defines the normalized Work-Unit DAG schema; 077 produces DAGs conforming to it.
- Spec 075's capability taxonomy is the closed vocabulary an adapter draws node capability tags from.
- chitin's own specs use GitHub spec-kit (`.specify/`), so the spec-kit adapter is the first and primary one.
- The kit landscape (spec-kit, superpowers, OpenSpec, Kiro, BMAD) is captured in the orchestrator research notes; Kiro and Google Antigravity are IDE-bound and out of scope as externally-driven kits.
- Compiling a spec into a DAG is a deterministic, side-effect-free transformation — it runs inside a Temporal activity, never in workflow code.

## Out of Scope

- The scheduling algorithm and the DAG schema definition — spec 076.
- The driver interface and the capability taxonomy definition — spec 075.
- Authoring specs, or running the kits' own slash commands / generators.
- IDE-bound kits that cannot be driven externally (Kiro, Google Antigravity).
- A registry or marketplace of specs — this compiles specs already present in a repo.
