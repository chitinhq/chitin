// Package local will hold the reference local-LLM agent driver (spec 075,
// task T016, FR-014) — a coding-agent loop against a self-hosted
// OpenAI-compatible endpoint, proving the AgentDriver contract on a
// non-hosted agent.
//
// TODO(spec-075 Phase 3, US1): implement driver.go — an AgentDriver whose
// Invoke runs a coding-agent loop against an operator-hosted,
// OpenAI-compatible endpoint (e.g. llama-server or vLLM), publishing a
// CapabilityCard with Tier=TierLocal and CostClass=CostFree and reporting
// readiness from the endpoint's reachability. Left as a documented stub by
// the Phase 0 foundation slice (T001–T011).
package local
