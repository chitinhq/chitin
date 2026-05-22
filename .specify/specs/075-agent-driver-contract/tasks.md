# Tasks: Agent Driver Contract

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md)

## Format: `[ID] [P?] [Story] Description`

- **[P]** = parallelizable (different files, no incomplete dependency)
- **[US1/US2/US3]** = the user story a task serves (story phases only)

## Path Conventions

New Go package `go/orchestrator/driver/` inside the orchestrator module
(spec 070) — `driver.go`, `card.go`, `taxonomy.go`, `registry.go`,
`select.go`, `invoke.go`, and concrete drivers under `claudecode/`,
`codex/`, `hermes/`, `openclaw/`, `local/`. No `swarm/` artifacts —
075 is a library, not a standalone script.

---

## Phase 1: Setup (Shared Infrastructure)

- [ ] T001 Create the `go/orchestrator/driver/` package skeleton (`driver.go`, `card.go`, `taxonomy.go`, `registry.go`, `select.go`, `invoke.go` with package decls + doc comments) per plan.md
- [ ] T002 [P] Add the empty driver sub-package directories `go/orchestrator/driver/{claudecode,codex,hermes,openclaw,local}/` with package decls

## Phase 2: Foundational (Blocking Prerequisites)

- [ ] T003 Define the `AgentDriver` interface in `go/orchestrator/driver/driver.go` — identify, publish a capability card, report readiness, invoke — the one path orchestrator → agent (FR-001, FR-013)
- [ ] T004 [P] Define the capability-card types in `go/orchestrator/driver/card.go` — driver id + version, agent runtime + model, capability-tag set, tier, cost class, operational constraints (FR-003)
- [ ] T005 [P] Define the typed `WorkUnit` invocation input and `Result` output in `go/orchestrator/driver/card.go` — work-unit id, spec/task context, worktree path, deadline; status, output reference, explanation (FR-006)
- [ ] T006 Define the closed capability taxonomy in `go/orchestrator/driver/taxonomy.go` — the declared tag vocabulary (`code.implement`, `code.review`, `research.web`, `research.x`, `docs.write`, `spec.author`, `bulk.codegen`) + an `IsKnown` check; an unknown tag is a registration error (FR-015)
- [ ] T007 Implement the driver registry in `go/orchestrator/driver/registry.go` — load drivers from configuration at startup, reject any driver with an unknown capability tag or a bypassable agent, answer "which registered, ready drivers satisfy capability C?" (FR-002, FR-004, FR-008, FR-009, FR-015)
- [ ] T008 Implement the deterministic selection function in `go/orchestrator/driver/select.go` — ordering is tier, then cost class, then a stable driver-id tie-breaker; emits the chosen driver + reason (FR-005)
- [ ] T009 Implement the `InvokeDriver` Temporal activity in `go/orchestrator/driver/invoke.go` — takes a typed `WorkUnit`, runs in a dedicated worktree, returns a typed `Result`; one retryable, timeout-bounded, inspectable activity; typed timeout result on deadline overrun (FR-006, FR-007)
- [ ] T010 [P] Unit-test the registry + selection determinism in `go/orchestrator/driver/select_test.go` — identical selection across 100 repeated evaluations on a fixed registry (FR-005, SC-003)
- [ ] T011 [P] Temporal `testsuite` test for `InvokeDriver` in `go/orchestrator/driver/invoke_test.go` — typed result on success, typed timeout result on deadline overrun (FR-007)

## Phase 3: User Story 1 — Plug in any agent without touching the core (Priority: P1) 🎯 MVP

**Goal**: at least four concrete drivers — claudecode, codex, hermes, openclaw — plus a reference local-LLM driver, all routable through the one contract; adding a driver touches zero orchestrator core.
**Independent test**: implement the local-LLM driver as a new driver only, with no diff outside `go/orchestrator/driver/` and configuration, and confirm the orchestrator invokes it and a work unit completes.

- [ ] T012 [P] [US1] Implement the Claude Code driver in `go/orchestrator/driver/claudecode/driver.go` — subprocess-CLI runtime behind `AgentDriver`; publishes its capability card, reports readiness, invokes kernel-gated (FR-001, FR-009, FR-013)
- [ ] T013 [P] [US1] Implement the Codex driver in `go/orchestrator/driver/codex/driver.go` — gateway/API runtime behind `AgentDriver` (FR-001, FR-013)
- [ ] T014 [P] [US1] Implement the Hermes driver in `go/orchestrator/driver/hermes/driver.go` — gateway/API runtime behind `AgentDriver`; card declares web/X/browser/code capability tags (FR-001, FR-003, FR-013)
- [ ] T015 [P] [US1] Implement the OpenClaw driver in `go/orchestrator/driver/openclaw/driver.go` — ACP runtime behind `AgentDriver` (FR-001, FR-013)
- [ ] T016 [US1] Implement the reference local-LLM driver in `go/orchestrator/driver/local/driver.go` — a coding-agent loop against a self-hosted OpenAI-compatible endpoint; proves the contract on a non-hosted agent (FR-014)
- [ ] T017 [US1] Zero-core-diff plug-in proof test — register the local-LLM driver, confirm a work unit completes end to end, and assert the diff touches zero lines outside `go/orchestrator/driver/` and configuration (FR-002, SC-001, SC-002, SC-007)

## Phase 4: User Story 2 — Route work to the right agent by capability (Priority: P2)

**Goal**: the registry answers capability queries and selection is deterministic; work no ready driver can satisfy is marked blocked-unroutable, never silently dropped.
**Independent test**: register two drivers with overlapping and distinct capabilities; dispatch work units with differing requirements; confirm each routes to a capability-matching driver, deterministically, with the selection reason recorded.

- [ ] T018 [US2] Wire capability-based selection into the registry query path in `go/orchestrator/driver/registry.go` — a work requirement names a capability; the registry returns exactly the ready drivers whose card includes it, then `select.go` picks one (FR-004, FR-005)
- [ ] T019 [US2] Implement blocked-unroutable handling in `go/orchestrator/driver/registry.go` — when no registered, ready driver satisfies a required capability, return a typed blocked-unroutable outcome naming the missing capability; never drop or arbitrarily assign (FR-012)
- [ ] T020 [P] [US2] Integration test for capability routing in `go/orchestrator/driver/registry_test.go` — overlapping/distinct capability cards route deterministically with reason recorded; an unsatisfiable capability yields blocked-unroutable with the missing tag named (FR-004, FR-005, FR-012, SC-003)

## Phase 5: User Story 3 — Declared capability is enforced capability (Priority: P3)

**Goal**: a capability card is recorded in the chitin chain at registration and enforced by the kernel at runtime — an out-of-card action is denied, the denial citing the card.
**Independent test**: register a driver whose card declares a narrow capability set; have its agent attempt an action outside that set; confirm the kernel denies it and the decision cites the capability card.

- [ ] T021 [US3] Implement capability-card recording in the chitin chain in `go/orchestrator/driver/registry.go` — on driver registration, write the card to the chain so the declared contract is itself auditable (FR-010, SC-006)
- [ ] T022 [US3] Implement kernel capability-card enforcement — the kernel can deny an action whose capability is outside the invoked driver's declared set, the denial citing the card; enforcement is silent on the happy path (FR-011)
- [ ] T023 [US3] Out-of-card-denial contract test in `go/orchestrator/driver/invoke_test.go` — register a narrow-capability driver, have its agent attempt an out-of-card action, confirm the kernel denies it in 100% of attempts and the decision cites the card (FR-011, SC-005)

## Phase 6: Polish & Cross-Cutting

- [ ] T024 [P] Run `workflowcheck` on the `InvokeDriver` activity path — confirm determinism, no nondeterministic constructs in the invoke activity (FR-007)
- [ ] T025 [P] Write operator docs — how to write a driver, declare a capability card, register it, and route work to it — in `docs/runbooks/agent-driver-contract.md` (FR-001, FR-003, FR-014)
- [ ] T026 Re-run the Constitution Check — all six principles still hold post-implementation (§1, §2, §3 PASS; §4, §5 N/A; §6 PASS)

---

## Dependencies

- **Phase 1 → Phase 2 → Phase 3**: Setup and Foundational block all stories.
- **US1 (P1)** is the MVP — the concrete drivers + the zero-core-diff proof; independently shippable once Phase 1+2 are done.
- **US2 (P2)** depends on Phase 2 (the registry + selection function); independent of US1's concrete drivers.
- **US3 (P3)** depends on Phase 2 (the card types + registry); best done after US1/US2 prove the contract works.
- Within a story: types → registry/selection wiring → test.

## Parallel Execution Examples

- Phase 1: T002 alongside T001.
- Phase 2: T004, T005 in parallel (same file region — coordinate), then T010, T011 in parallel (distinct test files).
- Phase 3: T012, T013, T014, T015 in parallel (distinct driver sub-packages).

## Implementation Strategy

**MVP = US1 (the driver contract + concrete drivers).** Phase 1 + Phase 2
+ Phase 3 deliver the `AgentDriver` interface, the registry, the invoke
activity, and at least five drivers — with the zero-core-diff proof
test confirming the agent-agnostic thesis (070 FR-017, SC-001). US2
adds deterministic capability routing; US3 hardens the platform by
turning the self-declared card into a kernel-enforced one. Each phase
is independently testable; ship US1 first, then layer US2 and US3.
