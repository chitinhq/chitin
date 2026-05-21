// Package claudecode will hold the Claude Code agent driver (spec 075,
// task T012) — a subprocess-CLI runtime behind the AgentDriver contract.
//
// TODO(spec-075 Phase 3, US1): implement driver.go — an AgentDriver whose
// Invoke drives the Claude Code CLI as a kernel-gated subprocess in the
// work unit's dedicated worktree, publishing a CapabilityCard and reporting
// readiness from the CLI's availability and quota. Left as a documented
// stub by the Phase 0 foundation slice (T001–T011).
package claudecode
