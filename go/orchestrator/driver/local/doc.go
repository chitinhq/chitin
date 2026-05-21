// Package local holds the reference local-LLM agent driver (spec 075, task
// T016, FR-014) — a coding-agent loop against an OpenAI-compatible endpoint
// the operator self-hosts (e.g. llama-server or vLLM).
//
// It is the proof that the spec-075 AgentDriver contract holds for a
// non-hosted agent: no vendor CLI, no quota, no marginal cost — just an
// HTTP endpoint on the operator's own GPU. The driver publishes a card with
// Tier=TierLocal and CostClass=CostFree, reports readiness from the
// endpoint's configuration and reachability, and runs a coding-agent loop
// in the work unit's dedicated worktree under chitin kernel governance. A
// local OpenAI-compatible endpoint is just one more runtime kind behind the
// one interface (FR-013) — the orchestrator never names it.
//
// The endpoint base URL is taken from the CHITIN_LOCAL_LLM_URL environment
// variable; standing up the endpoint itself is an operational prerequisite,
// not part of this driver (spec 075 Assumptions).
package local
