// Package driver defines the Agent Driver Contract (spec 075) — the single,
// uniform path from the Chitin Orchestrator (spec 070) to any agent.
//
// Spec 070 declares the orchestrator agent-agnostic and driver-agnostic
// (070 FR-017) but specifies no how; this package supplies it. Every agent
// integration — Claude Code, Codex, Hermes, OpenClaw, a self-hosted
// local-LLM driver — implements one interface, AgentDriver, and publishes
// one CapabilityCard. The orchestrator and the scheduler (spec 076) never
// name a concrete agent: they query the Registry by capability and invoke
// the selected driver through this contract. Plugging a new agent into the
// swarm is a new file under go/orchestrator/driver/ plus a config
// registration — zero changes to orchestrator core (FR-002, SC-001).
package driver

import "context"

// AgentDriver is the uniform interface every agent integration implements
// (FR-001). It is the ONLY path from the orchestrator to an agent: the
// orchestrator, the scheduler, and every workflow invoke agents solely
// through this interface, never by naming a concrete agent runtime.
//
// A driver is uniform across runtime kinds — subprocess CLI, gateway/API,
// ACP, and a local OpenAI-compatible endpoint all sit behind these four
// methods (FR-013). Implementations must be safe for concurrent Invoke
// calls: each invocation is isolated in its own worktree and agent session
// with no shared mutable state.
type AgentDriver interface {
	// ID returns the stable driver identifier. It is constant for the life
	// of the driver, unique within a Registry, and serves as the final
	// deterministic tie-breaker in selection (FR-005). It must equal the
	// DriverID on the driver's CapabilityCard.
	ID() string

	// Card returns the driver's capability card — its declared contract:
	// runtime, model, capability tags, tier, cost class, and operational
	// constraints (FR-003). The card is the input to capability routing
	// and the artifact the kernel enforces against (FR-011).
	Card() CapabilityCard

	// Ready reports whether the driver's underlying agent can take work
	// right now. A driver whose agent is down or quota-exhausted MUST
	// report not-ready (false) with a human-readable reason so the
	// scheduler routes elsewhere — never fail a work unit by trying anyway
	// (FR-008). On the happy path it returns (true, "").
	Ready(ctx context.Context) (ready bool, reason string)

	// Invoke runs one WorkUnit on the driver's agent, kernel-gated, and
	// returns a typed Result (FR-006). The agent runs under chitin kernel
	// governance; a driver must not bypass the kernel to produce side
	// effects (FR-009). Invoke must honor the WorkUnit deadline: on
	// overrun it returns a Result with StatusTimeout rather than hanging.
	// The error return is reserved for transport/driver faults; an agent
	// outcome — success, failure, timeout — is always carried in Result.
	Invoke(ctx context.Context, wu WorkUnit) (Result, error)
}
