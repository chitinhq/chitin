// Package openclaw holds the OpenClaw agent driver (spec 075, task T015) —
// an ACP (Agent Client Protocol) runtime behind the spec-075 AgentDriver
// contract.
//
// The driver wraps the `openclaw agent` invocation: the orchestrator hands
// it a typed WorkUnit, the driver drives OpenClaw (Clawta) in the work
// unit's dedicated worktree under chitin kernel governance, and returns a
// typed Result. ACP is just one more runtime kind behind the one interface
// (FR-013) — the orchestrator never names it.
package openclaw
