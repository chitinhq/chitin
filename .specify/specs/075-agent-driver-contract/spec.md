# Feature Specification: Agent Driver Contract

**Feature Branch**: `075-agent-driver-contract`

**Created**: 2026-05-21

**Status**: Draft

**Input**: User description: "The Chitin Orchestrator (spec 070) MUST be agent-agnostic and driver-agnostic (070 FR-017) — but 070 specifies no *how*. This spec defines the Agent Driver Contract: a uniform interface every agent integration implements, a driver registry the scheduler queries, and capability cards that declare — and let the kernel enforce — what each agent can do. Plugging a new agent into the swarm (Hermes, OpenClaw, Claude Code, Codex, a local-LLM driver) MUST require zero changes to orchestrator core."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Plug in any agent without touching the core (Priority: P1)

A new agent runtime — for example, a coding agent running against a local
LLM on the operator's own GPU — is added to the swarm by writing one driver
that satisfies the Agent Driver Contract and registering it in
configuration. The orchestrator, the scheduler, and every existing workflow
pick it up with no change to orchestrator core. Removing an agent is
deleting its driver registration.

**Why this priority**: This is the agent-agnostic thesis (070 FR-017). If
adding an agent touches orchestrator core, the platform is not
agent-agnostic — it is hard-coded with extra steps. Every other story
depends on the contract existing.

**Independent Test**: Implement the local-LLM driver — a coding-agent loop
against an OpenAI-compatible endpoint the operator self-hosts — as a new
driver only, with no diff outside `go/orchestrator/driver/` and
configuration, and confirm the orchestrator invokes it and a work unit
completes.

**Acceptance Scenarios**:

1. **Given** a driver implementing the contract is registered, **When** the orchestrator starts, **Then** the driver appears in the registry and is invocable with no orchestrator-core change.
2. **Given** a registered driver, **When** a work unit is dispatched to it, **Then** the orchestrator invokes the agent, receives a typed result, and the run is one inspectable Temporal activity.
3. **Given** a driver is removed from configuration, **When** the orchestrator restarts, **Then** it no longer offers that agent, and no core code referenced it.

---

### User Story 2 - Route work to the right agent by capability (Priority: P2)

The scheduler (spec 076) has a runnable work unit that needs a specific
capability — web research, frontier-grade code review, bulk code
generation. It asks the registry which drivers declare that capability and
are ready, and selects one deterministically by tier and cost. The operator
never hand-assigns an agent to a task; routing is a function of declared
capability and work requirement.

**Why this priority**: Capability is what makes routing *automatic and
deterministic*. Without it, "agent-agnostic" still needs a human to decide
which agent does what — the human kanban by another name.

**Independent Test**: Register two drivers with overlapping and distinct
capabilities; dispatch work units with differing capability requirements;
confirm each is routed to a capability-matching driver, deterministically,
with the selection reason recorded.

**Acceptance Scenarios**:

1. **Given** drivers publish capability cards, **When** a work unit requires capability C, **Then** the registry returns exactly the ready drivers whose card includes C.
2. **Given** multiple drivers satisfy C, **When** one is selected, **Then** the selection is deterministic — tier, then cost class, then a stable driver-id tie-breaker — and the chosen driver and reason are recorded.
3. **Given** no ready driver satisfies C, **When** the work unit is evaluated, **Then** it is marked blocked-unroutable with the missing capability named — never silently dropped or arbitrarily assigned.

---

### User Story 3 - Declared capability is enforced capability (Priority: P3)

An agent's capability card is a contract, not a brochure. When a driver's
agent acts, the chitin kernel holds it to its card: an agent that declared
`code.review` does not get to run an infrastructure deploy. The gap between
what an agent claims and what it actually does — the known attack surface
of self-reported capability — is closed by runtime enforcement.

**Why this priority**: Self-reported capability is untrustworthy on its
own. chitin already gates every agent action, so it can *enforce* the card —
turning a self-declared card into a runtime-verified one. P3 because P1/P2
deliver the working platform; this hardens it.

**Independent Test**: Register a driver whose card declares a narrow
capability set; have its agent attempt an action outside that set; confirm
the kernel denies it and the decision cites the capability card.

**Acceptance Scenarios**:

1. **Given** a driver's card declares capability set S, **When** its agent attempts an action whose capability is not in S, **Then** the kernel denies the action and the decision cites the card.
2. **Given** a capability card, **When** the driver registers, **Then** the card is recorded in the chitin chain so the declared contract is itself audited.
3. **Given** an agent stays within S, **When** it acts, **Then** no capability-based denial occurs — enforcement is silent on the happy path.

---

### Edge Cases

- A driver's underlying agent is unavailable (process down, quota exhausted) — the driver MUST report **unready** so the scheduler routes elsewhere, never fail the work unit by trying anyway.
- Two drivers have identical capability cards and tier — selection MUST still be deterministic via the stable driver-id tie-breaker.
- A driver's agent can bypass chitin governance — the driver MUST be **rejected at registration**; an ungoverned agent is not admissible.
- A capability card declares a tag outside the capability taxonomy — registration MUST fail with "unknown capability", never silently trust it.
- An agent invocation exceeds its deadline — the driver MUST return a typed timeout result the workflow can retry per policy, never hang.
- A driver is invoked concurrently for two work units — each invocation MUST be isolated (its own worktree, its own agent session); no shared mutable state.
- An agent's quota is exhausted mid-invocation — the driver MUST surface a typed, retryable failure so the scheduler can re-route or back off.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The platform MUST define a single `AgentDriver` interface that every agent integration implements; the orchestrator invokes agents ONLY through it.
- **FR-002**: Adding or removing an agent MUST require changes only within the driver package and configuration — never in orchestrator core, the scheduler, or workflow code.
- **FR-003**: Every driver MUST publish a **capability card** declaring: a stable driver id and version, the underlying agent runtime and model, a set of capability tags, a tier and cost class, and operational constraints (quota-bounded, network required, max context, worktree required).
- **FR-004**: A **driver registry** MUST load drivers from configuration at startup and answer capability queries — "which registered, ready drivers satisfy capability C?"
- **FR-005**: Driver selection MUST be deterministic — given the same registry state and work requirement, the same driver is chosen; the ordering is tier, then cost class, then a stable driver-id tie-breaker.
- **FR-006**: A driver invocation MUST take a typed **work unit** (work-unit id, spec/task context, worktree path, deadline) and return a typed **result** (status, output reference, explanation) — no free-form contract.
- **FR-007**: Every driver invocation MUST run as a single Temporal activity — retryable, timeout-bounded, individually inspectable (070 FR-002, FR-004).
- **FR-008**: A driver MUST report **readiness**; a driver whose agent is down or quota-exhausted MUST report unready so the scheduler routes elsewhere.
- **FR-009**: A driver MUST run its agent under chitin kernel governance; a driver whose agent can bypass the kernel MUST be rejected at registration (FR-009 ↔ 070 §1 side-effect boundary).
- **FR-010**: A capability card MUST be recorded in the chitin chain when its driver registers — the declared contract is itself auditable.
- **FR-011**: The kernel MUST be able to enforce a capability card at runtime — an action whose capability is outside the invoked driver's declared set MUST be deniable, with the denial citing the card.
- **FR-012**: When no registered, ready driver satisfies a required capability, the work unit MUST be marked **blocked-unroutable** with the missing capability named — never silently dropped or arbitrarily assigned.
- **FR-013**: Driver invocation MUST be uniform across runtime kinds — subprocess CLI, gateway/API, ACP, and local OpenAI-compatible endpoint — behind the one interface.
- **FR-014**: A first-party **local-LLM driver** MUST be delivered as a reference implementation — a coding-agent loop against an OpenAI-compatible endpoint the operator self-hosts — proving the contract on a non-hosted agent.
- **FR-015**: Capability tags MUST come from a closed, declared **capability taxonomy** so work requirements and driver cards refer to the same names; an unknown tag is a registration error.

### Key Entities

- **AgentDriver**: the uniform interface — identify, publish a capability card, report readiness, invoke. The only path from the orchestrator to an agent.
- **Capability Card**: declarative metadata for one driver — id, version, agent + model, capability-tag set, tier, cost class, constraints. Modeled on the A2A Agent Card; recorded in the chitin chain.
- **Capability Taxonomy**: the closed vocabulary of capability tags (e.g. `code.implement`, `code.review`, `research.web`, `research.x`, `docs.write`, `spec.author`, `bulk.codegen`) shared by cards and work-unit requirements.
- **Driver Registry**: the startup-loaded set of drivers; answers capability and readiness queries for the scheduler.
- **Work Unit (invocation input)**: work-unit id, spec/task context, worktree path, deadline — what a driver is asked to do.
- **Invocation Result**: typed outcome — status, output reference (branch / PR / artifact), explanation — what a driver returns.
- **Tier / Cost Class**: `frontier` / `mid` / `local` and a relative cost band — the deterministic selection keys.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Plugging in a new agent touches zero lines outside `go/orchestrator/driver/` and configuration (measured by diff).
- **SC-002**: The reference local-LLM driver completes a real work unit end to end — governed, and inspectable as a Temporal activity.
- **SC-003**: Given a fixed registry and work requirement, driver selection is identical across 100 repeated evaluations.
- **SC-004**: 100% of driver invocations appear as individually inspectable Temporal activities.
- **SC-005**: An agent action outside its declared capability set is denied by the kernel in 100% of attempts in the contract test.
- **SC-006**: Every registered driver's capability card is present in the chitin chain.
- **SC-007**: At least four drivers — Claude Code, Codex (Hermes), OpenClaw (Clawta), local-LLM — are registered and routable through the one contract.

## Assumptions

- Spec 070 (Chitin Orchestrator) provides the durable-execution platform; 075 defines the driver layer it invokes. Spec 076 (Spec-DAG Scheduler) is the primary consumer of the registry's capability queries.
- The chitin kernel already gates agent actions (the governance hook is installed per agent). 075 adds the capability card as a contract the kernel can enforce — it does not rebuild governance.
- The kernel's existing `internal/driver/` packages handle per-agent hook and attribution *formats*; 075's orchestrator-side `AgentDriver` handles *invocation and capability*. They share the agent taxonomy but are distinct layers.
- Agents bring their own toolsets and skills (Hermes's web/X/browser/code, OpenClaw's ACP, etc.); 075 does not re-implement agent capability — it *describes and routes* it.
- The local-LLM endpoint is operator-hosted (e.g. llama-server or vLLM, OpenAI-compatible) and reachable by the orchestrator; standing up that endpoint is an operational prerequisite, not part of this spec.
- The A2A Agent Card is the model for the capability card's shape; full A2A protocol conformance and cryptographically signed cards are later hardening, not required for v1.

## Out of Scope

- The scheduling algorithm that consumes the registry — spec 076.
- Compiling specs into the work-unit graph — spec 077.
- Re-implementing any agent's internal reasoning, tools, or skills.
- Standing up and operating the local inference endpoint (model serving, GPU operations).
- Full A2A protocol wire-conformance and cryptographically signed agent cards (future hardening).
