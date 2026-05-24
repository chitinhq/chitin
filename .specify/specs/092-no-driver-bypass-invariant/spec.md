# Feature Specification: No-Driver-Bypass Invariant Test

**Feature Branch**: `feat/092-no-driver-bypass-invariant`

**Created**: 2026-05-23

**Status**: Draft

**Input**: Operator selected Ares follow-up #3: "Write the no-driver-bypass invariant-test spec" after reviewing the constitution, specs, and diagrams for the new §7 rule: no implementation work reaches a driver except via the orchestrator's dispatch from a spec-derived or orchestrator-intaked work-unit.

## Goal

Turn the constitutional §7 rule from prose into an executable invariant: every implementation-producing driver invocation is attributable to an orchestrator work-unit, every direct legacy path is either retired or blocked, and any future bypass fails CI or emits an unmistakable runtime violation.

The invariant is deliberately narrower than "all agent conversation goes through the orchestrator." Operator-facing research, spec-writing, review, and reporting may happen as kernel-gated ad-hoc work. The hard gate applies when the action produces implementation work: code mutations, implementation PRs, infrastructure changes, deploys, or executable artifacts.

## File-system scope

This spec is expected to touch only invariant tests, chain/telemetry assertions, and orchestrator/driver metadata plumbing needed to prove attribution. Exact files are resolved in plan phase, but the intended scope is:

- `.specify/specs/092-no-driver-bypass-invariant/spec.md` — this spec.
- `.specify/specs/INDEX.md` — registry row for this spec.
- `go/orchestrator/**` — if needed, work-unit / workflow id propagation into driver invocation records.
- `go/execution-kernel/**` or `libs/telemetry/**` — if needed, chain/query support for detecting missing orchestrator attribution.
- `apps/chitin-console/**` — out of scope unless plan phase finds the invariant is already displayed and needs only a label update.
- `swarm/**`, `libs/adapters/**`, `libs/openclaw-plugins/**` — scan targets for legacy direct-driver paths; modifications only if a tested bypass stub or attribution hook must live there.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Implementation driver invocations are attributable (Priority: P1)

An implementation work-unit runs through the orchestrator. The scheduler dispatches a WorkUnitWorkflow, the workflow invokes a capability-matched driver, the driver performs tool calls through the kernel, and the chain/telemetry record carries the orchestrator attribution end to end.

**Why this priority**: This is the constitutional rule in executable form. If driver activity cannot be joined back to a workflow/work-unit, the operator cannot prove that the orchestrator is the dispatch boundary.

**Independent Test**: Run a fixture implementation work-unit through the orchestrator. Query the resulting chain/telemetry records and assert that every implementation-producing driver invocation and gated tool call has a non-empty `workflow_id`, `work_unit_id`, `driver_id`, `capability_tag`, and target repo/base ref.

**Acceptance Scenarios**:

1. **Given** an orchestrator-dispatched implementation work-unit, **When** its driver is invoked, **Then** the invocation record includes `workflow_id`, `work_unit_id`, `driver_id`, `capability_tag`, `target_repo`, and `base_ref`.
2. **Given** the driver issues kernel-gated tool calls, **When** the chain is queried by `work_unit_id`, **Then** all tool-call decisions for that work-unit are discoverable and attributable to the same workflow.
3. **Given** a driver invocation returns an artifact reference (branch, PR, commit, or report), **When** the result is recorded, **Then** the artifact references the originating work-unit id.

---

### User Story 2 - Direct implementation bypasses fail closed (Priority: P1)

A legacy script, cron, Discord ingress path, plugin, or CLI helper attempts to invoke a driver directly for implementation work without an orchestrator work-unit. The system rejects it before mutation or records a hard invariant violation if rejection happens at a lower layer.

**Why this priority**: The main architectural risk is recreating the old Discord/cron/script → driver → mutation path under a new name. This story ensures bypasses are not merely discouraged; they are mechanically caught.

**Independent Test**: Add a deliberately-bad fixture that attempts direct implementation driver invocation with no `work_unit_id`. The test must fail the invocation before any file mutation, PR creation, push, deploy, or infrastructure side effect occurs.

**Acceptance Scenarios**:

1. **Given** an implementation driver invocation lacking orchestrator attribution, **When** it reaches the driver contract boundary, **Then** it is denied before the driver performs any implementation-producing action.
2. **Given** a script or plugin attempts direct driver dispatch for implementation, **When** CI runs the invariant test, **Then** the test fails with the bypass path named.
3. **Given** the bypass attempt is blocked, **When** telemetry is inspected, **Then** a `driver_bypass_blocked` or equivalent event names the caller, driver, attempted capability, and missing attribution fields.

---

### User Story 3 - Legacy surfaces are classified, not guessed (Priority: P2)

The repo still contains historical swarm scripts, cron wrappers, plugins, and Discord ingress code. The invariant scanner classifies each path as: retired, read/report-only, orchestrator-intake, scheduled-workflow, deterministic activity, or illegal direct-driver path.

**Why this priority**: A raw grep for agent names will produce noise. The useful test distinguishes allowed operator-facing/reporting surfaces from illegal implementation dispatch.

**Independent Test**: Run the classifier over known fixtures: one retired path, one read-only report, one orchestrator intake, one scheduled workflow, and one illegal direct driver dispatch. Confirm each classification is correct and the illegal path fails.

**Acceptance Scenarios**:

1. **Given** a read-only report job, **When** scanned, **Then** it is classified as allowed non-implementation work and not flagged as a bypass.
2. **Given** an ingress path that mints an orchestrator work-unit, **When** scanned, **Then** it is classified as orchestrator-intake and allowed.
3. **Given** a cron/script path that directly invokes a driver to mutate code, **When** scanned, **Then** it is classified as illegal direct-driver implementation dispatch.
4. **Given** a retired legacy path, **When** scanned, **Then** the scanner requires either removal or an explicit retirement marker so dead code does not look like a live bypass.

---

### User Story 4 - Operator can audit the invariant after the fact (Priority: P3)

The operator asks, "Did any implementation work bypass the orchestrator this week?" A single query/report answers from chain/telemetry data, listing zero bypasses or naming the exact offending events.

**Why this priority**: CI catches code paths before merge; runtime audit catches configuration drift and operator-box scripts that CI may not see.

**Independent Test**: Seed telemetry with compliant and non-compliant invocation records. Run the audit query/report and assert that only the non-compliant implementation-producing records are flagged.

**Acceptance Scenarios**:

1. **Given** compliant orchestrator-dispatched driver records, **When** the audit runs, **Then** it reports zero bypasses.
2. **Given** an implementation-producing driver record without `workflow_id` or `work_unit_id`, **When** the audit runs, **Then** it flags the record and names the missing fields.
3. **Given** non-implementation operator-facing records, **When** the audit runs, **Then** they are excluded or reported separately as kernel-gated non-DAG work, not as bypasses.

## Edge Cases

- A driver is invoked for research, spec-writing, review, or report generation and creates a document artifact. This is allowed if kernel-gated and classified as non-implementation; it must not open an implementation PR, mutate code, deploy, or produce executable artifacts.
- An ad-hoc operator request produces implementation. It is allowed only if intake first mints an orchestrator work-unit; it does not need to be a pre-existing DAG node.
- A deterministic node runs a shell command with no agent driver. It must still carry workflow/work-unit attribution, but it is not a driver bypass because no driver is invoked.
- A legacy script is retained only as a wrapper that calls the orchestrator API. It is allowed if the wrapper cannot invoke a driver directly and the resulting work-unit is attributed.
- A driver performs internal retries. Retries inherit the same work-unit attribution; a retry without attribution is a violation.
- A driver invocation starts before this spec lands and lacks the new fields. The audit may grandfather a bounded migration window, but new runs after the cutover must be strict.
- A field is present but empty, placeholder, or mismatched across chain records. Treat as missing attribution, not compliant.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The platform MUST define the minimum attribution envelope for implementation-producing driver invocations: `workflow_id`, `work_unit_id`, `driver_id`, `capability_tag`, `target_repo`, `base_ref`, and invocation timestamp.
- **FR-002**: Every implementation-producing driver invocation MUST carry the attribution envelope before the driver can mutate files, create commits, push branches, open PRs, deploy, or produce executable artifacts.
- **FR-003**: The kernel/chain/telemetry records for driver tool calls MUST preserve enough attribution to join a tool-call decision back to the originating work-unit.
- **FR-004**: A missing, empty, malformed, or inconsistent attribution envelope on implementation-producing driver work MUST fail closed before mutation where technically possible.
- **FR-005**: The invariant suite MUST include at least one negative fixture that attempts direct driver invocation without orchestrator attribution and proves no mutation occurs.
- **FR-006**: The invariant suite MUST include a static/classification scan over known direct-dispatch surfaces: Discord ingress, cron/scheduled jobs, `swarm/` scripts, driver adapters, OpenClaw plugins, Hermes integration points, and CLI helpers.
- **FR-007**: The classifier MUST distinguish implementation-producing work from allowed non-implementation work (research, spec-writing, review, reporting) so operator-facing surfaces are not falsely banned.
- **FR-008**: Allowed ad-hoc implementation work MUST be represented as an orchestrator-intaked work-unit, even when it is not a pre-existing spec-DAG node.
- **FR-009**: Runtime audit MUST be able to answer whether any implementation-producing driver invocation in a time window lacked orchestrator attribution.
- **FR-010**: Violations MUST name the caller/path, attempted driver, capability/action, missing attribution fields, and whether any mutation escaped.
- **FR-011**: The invariant MUST be wired into CI or an equivalent pre-merge gate before §7 is considered mechanically enforced.
- **FR-012**: This spec MUST NOT reintroduce kanban/board state as a source of scheduling truth; board/telemetry data may be scanned only as evidence.

### Key Entities

- **Implementation-producing driver invocation**: Any driver call that may mutate code, create commits, push branches, open implementation PRs, deploy, change infrastructure, or produce executable artifacts.
- **Attribution envelope**: The required metadata that proves a driver invocation came from orchestrator dispatch.
- **Orchestrator-intaked ad-hoc work-unit**: A work-unit minted at intake time from an operator request or reactive trigger, rather than from a pre-existing spec-DAG node.
- **Direct-driver bypass**: Any implementation-producing path from Discord, cron, script, plugin, CLI, or another surface to a driver without an orchestrator work-unit in the path.
- **Invariant scanner/classifier**: Test/audit code that classifies legacy and current dispatch surfaces and fails illegal paths.
- **Runtime audit report**: Query/report over chain/telemetry records that flags implementation-producing driver activity lacking attribution.

## Test coverage *(mandatory — e2e default per spec 020 §1.2)*

- **E2E required**: one orchestrator-dispatched fixture work-unit that invokes a driver or test driver and proves the full attribution chain from workflow → driver invocation → kernel decision → artifact/result.
- **E2E required**: one illegal direct-driver fixture that attempts implementation without `work_unit_id` and is denied before mutation.
- **Integration allowed with justification**: static/classification scan over repo surfaces may be an integration test because it validates source paths and fixture metadata, not a running Temporal workflow.
- **Unit allowed with justification**: attribution-envelope parsing/validation may be unit-tested because it is pure validation logic; it does not replace the required e2e tests.

## Acceptance Criteria

AC1. **Positive attribution proof.** A compliant orchestrator-dispatched implementation work-unit produces chain/telemetry records joinable by `work_unit_id` from dispatch through gated tool calls and final artifact.

AC2. **Negative bypass proof.** A direct implementation driver invocation without orchestrator attribution is blocked before mutation and emits/names a violation.

AC3. **Surface classifier.** The invariant scanner classifies legacy/current surfaces into allowed non-implementation, orchestrator-intake, scheduled workflow, deterministic activity, retired, or illegal direct-driver path.

AC4. **Runtime audit.** A single audit command/query over chain/telemetry records reports zero bypasses for compliant data and flags seeded non-compliant records with missing fields named.

AC5. **CI gate.** The invariant suite runs in CI or a documented equivalent pre-merge gate and fails on the illegal fixture.

AC6. **No kanban resurrection.** The implementation uses orchestrator, driver, kernel, and telemetry attribution; it does not use kanban/board state as a scheduling or dispatch authority.

AC7. **Spec-kit entry.** This file exists at `.specify/specs/092-no-driver-bypass-invariant/spec.md` and is registered in `.specify/specs/INDEX.md`.

## Invariants

- No implementation work reaches a driver except via orchestrator dispatch from a spec-derived or orchestrator-intaked work-unit.
- Kernel gating remains mandatory for every driver tool call; this spec proves dispatch attribution, not a replacement for policy evaluation.
- Telemetry observes and audits; it never decides scheduling or driver selection.
- The primary/shared checkout is never a work surface; compliant work still uses per-work-unit worktrees.
- Non-implementation operator-facing work remains allowed when kernel-gated and correctly classified.

## Dependencies

- **Constitution §7** — source invariant: the swarm is the orchestrator; no implementation work reaches a driver except through orchestrator dispatch.
- **Spec 070** — orchestrator durable workflow platform and per-work-unit worktrees.
- **Spec 075** — AgentDriver contract, capability cards, driver registry, kernel-enforced declared capability.
- **Spec 076** — Spec-DAG Scheduler and WorkUnitWorkflow dispatch model.
- **Spec 081 / 087** — cron/board/kanban retirement context; legacy surfaces must not remain live dispatchers.
- **Spec 090** — Discord channel ingress must be checked to ensure ingress mints work-units instead of directly dispatching implementation.
- **Spec 091** — `continue:false` stop semantics; bypass prevention must fail closed and must not create retry/stop-hook loops.

## Open Questions

O1. **Where should the runtime audit live?** Proposed: a telemetry/chain CLI or Sentinel rule, not the orchestrator hot path.

O2. **What is the exact schema name for attribution fields?** Proposed: plan phase standardizes names across orchestrator result, driver invocation, and chain event records.

O3. **How long is the migration grandfather window for older records?** Proposed: zero for new runs after implementation lands; historical records are audit-only and may be excluded by timestamp.

O4. **Should non-code document PRs count as implementation?** Proposed: specs/research/reports are non-implementation unless they mutate executable code, infra, deployment config, or open an implementation PR.

## Out of Scope

- Building the MCP Tasks ingress substrate; this spec only requires attributable orchestrator intake, not a particular protocol.
- Implementing the §7 constitution amendment itself.
- Rewriting driver capability taxonomy except where attribution fields need a capability tag.
- Migrating every cron/script to Temporal; this spec detects/blocks bypasses and may point to spec 081/087 for retirement work.
- Replacing kernel policy evaluation or changing allow/deny rules.
