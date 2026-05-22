# Implementation Plan: Agent Driver Contract

**Branch**: `075-agent-driver-contract` | **Date**: 2026-05-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/075-agent-driver-contract/spec.md`

## Summary

Spec 070 declares the Chitin Orchestrator agent-agnostic and
driver-agnostic (070 FR-017) but specifies no *how*. 075 supplies the
*how*: a single `AgentDriver` interface every agent integration
implements, a **driver registry** the scheduler queries, and
**capability cards** that declare — and let the kernel enforce — what
each agent can do. Plugging a new agent into the swarm (Hermes,
OpenClaw, Claude Code, Codex, a local-LLM driver) requires zero changes
to orchestrator core: a new driver is a new file under
`go/orchestrator/driver/` plus a config registration, nothing more.

Driver invocation is a single Temporal activity (070 FR-002/FR-004) —
retryable, timeout-bounded, individually inspectable. Selection is
deterministic: tier, then cost class, then a stable driver-id
tie-breaker. The capability card is modeled on the A2A Agent Card,
recorded in the chitin chain at registration, and enforceable by the
kernel at runtime — closing the gap between *declared* and *actual*
capability.

This plan does not re-litigate the engine (Temporal — see 070's
`docs/strategy/chitin-orchestrator-options-2026-05-20.md`) or rebuild
governance (the kernel already gates agent actions). 075 is the driver
layer the orchestrator invokes through.

## Technical Context

**Language/Version**: Go 1.23+ — matches the Chitin Kernel and the orchestrator (spec 070); the Temporal Go SDK is first-class.

**Primary Dependencies**: the Temporal Go SDK (driver invocation is a Temporal activity); the chitin kernel (governance — drivers run their agents kernel-gated); the chitin chain (capability-card audit at registration).

**Storage**: capability cards are recorded in the chitin chain; no new datastore. The driver registry is in-memory, loaded from configuration at startup.

**Testing**: `go test`; Temporal's `testsuite` for the `InvokeDriver` activity; a contract test that registers a narrow-capability driver and confirms the kernel denies an out-of-card action.

**Target Platform**: Linux, single box (chimera-ant); self-hosted.

**Project Type**: a Go package inside the orchestrator module — a library compiled into the orchestrator worker-host binary, not a standalone service.

**Performance Goals**: driver invocation is agent-bound (minutes per work unit). The goal is **uniformity + determinism of selection**, not throughput.

**Constraints**: single-box, self-hosted. Every driver invocation runs in its own dedicated git worktree (FR-007, 070 FR-013). A driver whose agent can bypass the kernel is inadmissible (FR-009). Capability tags come from a closed taxonomy — an unknown tag is a registration error (FR-015).

**Scale/Scope**: one interface, one registry, one taxonomy, one invoke activity; at least four concrete drivers at v1 (Claude Code, Codex, OpenClaw, local-LLM) plus Hermes — SC-007.

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

| Principle | Assessment |
|-----------|------------|
| §1 Side-effect boundary | PASS — drivers invoke agents that remain kernel-gated; a driver does not bypass the kernel to produce side effects. FR-009 rejects any driver whose agent can bypass governance at registration. |
| §2 Branch & worktree (amended: always workers + worktrees) | PASS — each driver invocation runs in its own dedicated worktree (FR-007); this is exactly 070 FR-013, which 075 invocations honor. |
| §3 Spec-kit promotion gate | PASS — 075 has `spec.md` + this `plan.md`; `tasks.md` follows. |
| §4 Tracked installers | N/A — 075 is a library compiled into the orchestrator worker-host binary; no standalone script runs on the operator's box, so no installer applies. |
| §5 Board-aware scripts | N/A — 075 ships no `swarm/` script that touches the kanban; the registry and drivers are orchestrator-internal. |
| §6 Swarm tooling is the exception | PASS — driver code is genuine kernel-adjacent infra (driver adapters, capability logic); it belongs under `go/`, not `swarm/`. |

No violations → Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/075-agent-driver-contract/
├── plan.md          # This file
├── research.md      # Phase 0 — A2A Agent Card shape, capability-taxonomy design
├── data-model.md    # Phase 1 — AgentDriver / CapabilityCard / Registry / WorkUnit / Result
├── quickstart.md    # Phase 1 — write a driver, register it, invoke it
└── tasks.md         # Phase 2 — /speckit-tasks output (not created here)
```

### Source Code (repository root)

```text
go/orchestrator/driver/
├── driver.go            # the AgentDriver interface — the one path orchestrator → agent
├── card.go              # capability-card types (id, version, agent+model, tags, tier, cost, constraints)
├── taxonomy.go          # the closed capability taxonomy — the declared tag vocabulary
├── registry.go          # the driver registry — startup load + capability/readiness queries
├── select.go            # deterministic selection — tier, then cost class, then driver-id
├── invoke.go            # the InvokeDriver Temporal activity — typed WorkUnit in, typed Result out
├── claudecode/          # Claude Code driver (subprocess CLI runtime)
├── codex/               # Codex driver (Hermes-hosted; gateway/API runtime)
├── hermes/              # Hermes driver (gateway/API runtime)
├── openclaw/            # OpenClaw driver (Clawta; ACP runtime)
└── local/               # reference local-LLM driver (OpenAI-compatible endpoint)
```

**Structure Decision**: a new `go/orchestrator/driver/` package inside
the existing orchestrator module (spec 070) — one language across
kernel, orchestrator, and drivers (constitution §6: kernel-adjacent
infra belongs under `go/`). The package holds the interface, the
capability-card types, the taxonomy, the registry, the selection
function, the `InvokeDriver` activity, and the concrete drivers as
sub-packages.

**Important — two driver layers, do not conflate them.** The kernel
already has `go/execution-kernel/internal/driver/`, which handles
*per-agent hook and attribution formats* — how each agent's governance
hook is shaped and how its actions are attributed in the chain. That is
a kernel-side layer. 075's `go/orchestrator/driver/` is a distinct,
orchestrator-side layer: it handles *invocation and capability* — how
the orchestrator calls an agent and what work that agent may be routed.
The two layers share the agent taxonomy (the set of known agent
runtimes) but are separate concerns and separate packages. 075 adds the
orchestrator-side layer; it does not touch the kernel-side one.

## Implementation Phases

- **Phase 0 — Foundation.** Scaffold `go/orchestrator/driver/`; the
  `AgentDriver` interface; capability-card types; the closed capability
  taxonomy; the registry + deterministic selection; the `InvokeDriver`
  Temporal activity. Exit: a stub driver registers and invokes through
  the activity.
- **Phase 1 — Drivers (the P1 slice, US1).** Concrete drivers —
  claudecode, codex, hermes, openclaw — plus the reference local-LLM
  driver (a coding-agent loop against a self-hosted OpenAI-compatible
  endpoint). Exit: the zero-core-diff plug-in proof test passes —
  adding the local-LLM driver touched zero lines outside the driver
  package and config (SC-001).
- **Phase 2 — Capability routing (the P2 slice, US2).**
  Capability-based selection wired into the registry; blocked-unroutable
  handling for work no ready driver can satisfy. Exit: selection is
  deterministic and reasoned across repeated evaluations (SC-003).
- **Phase 3 — Card enforcement (the P3 slice, US3).** Capability-card
  recording in the chitin chain; kernel capability-card enforcement;
  the out-of-card-denial contract test. Exit: an out-of-card action is
  denied by the kernel, the denial citing the card (SC-005, SC-006).
- **Phase 4 — Polish.** `workflowcheck`/determinism on the invoke
  activity; operator docs; re-run the Constitution Check.

## Complexity Tracking

None — no constitution violations to justify.
