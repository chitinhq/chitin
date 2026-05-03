// Package router implements the heuristic + advisor pipeline that
// wraps the kernel's deterministic gate. It runs in-process inside
// chitin-kernel for hot-path speed (~10ms vs the TS implementation's
// ~500ms cold start).
//
// Pipeline:
//   stdin (Claude Code PreToolUse JSON)
//     ↓
//   step 1: kernel verdict (gov.Gate.Evaluate)
//     ↓ if deny → return deny
//   step 2: heuristics (BlastRadius, Floundering)
//     ↓ if none fire → return kernel verdict
//   step 3: policy.Advisor decides whether to invoke advisor
//     ↓ if yes → spawn `claude -p` with structured prompt, parse response
//   step 4: compose final hook output
//
// The TS implementation in apps/temporal-worker/src/router/ is the
// design substrate that informed this port. Test fixtures port
// directly (see router_test.go).
package router

// HookInput is the inbound shape from Claude Code's PreToolUse hook.
type HookInput struct {
	HookEventName string                 `json:"hook_event_name"`
	ToolName      string                 `json:"tool_name"`
	ToolInput     map[string]interface{} `json:"tool_input"`
	Cwd           string                 `json:"cwd,omitempty"`
	SessionID     string                 `json:"session_id,omitempty"`
}

// HookOutput is the outbound shape back to Claude Code.
type HookOutput struct {
	Decision string `json:"decision"`          // "allow" | "deny"
	Message  string `json:"message,omitempty"` // shown to the agent
	Source   string `json:"source,omitempty"`  // kernel | heuristic-deny | advisor-* | kernel-allow
}

// HeuristicScore is the result of one heuristic running over a HookInput.
type HeuristicScore struct {
	Score  float64            `json:"score"`         // 0.0-1.0
	Fired  bool               `json:"fired"`         // score >= configured threshold
	Reason string             `json:"reason"`        // short telemetry tag
	Axis   map[string]float64 `json:"axis,omitempty"`
}

// HeuristicOutcome aggregates the heuristic results for one tool call.
type HeuristicOutcome struct {
	BlastRadius *HeuristicScore `json:"blast_radius,omitempty"`
	Floundering *HeuristicScore `json:"floundering,omitempty"`
	AnyFired    bool            `json:"any_fired"`
}

// AdvisorRequest is the payload sent to the advisor LLM.
type AdvisorRequest struct {
	Question         string           `json:"question"`
	Context          string           `json:"context"`
	ProposedAction   HookInput        `json:"proposed_action"`
	HeuristicOutcome HeuristicOutcome `json:"heuristic_outcome"`
	CallerTier       string           `json:"caller_tier,omitempty"`
	ChainDepth       int              `json:"chain_depth"`
}

// AdvisorResponse is the parsed structured output from the advisor.
type AdvisorResponse struct {
	Nudge    string `json:"nudge"`
	Verdict  string `json:"verdict"`  // "continue" | "takeover"
	Escalate bool   `json:"escalate"` // true → call next-tier advisor
}

// HeuristicConfig — per-heuristic policy from chitin.yaml.
type HeuristicConfig struct {
	Enabled           bool    `yaml:"enabled" json:"enabled"`
	Threshold         float64 `yaml:"threshold,omitempty" json:"threshold,omitempty"`
	MaxLoopCount      int     `yaml:"max_loop_count,omitempty" json:"max_loop_count,omitempty"`
	MaxStallSeconds   int     `yaml:"max_stall_seconds,omitempty" json:"max_stall_seconds,omitempty"`
}

// AdvisorConfig — advisor policy from chitin.yaml.
type AdvisorConfig struct {
	Enabled bool     `yaml:"enabled" json:"enabled"`
	When    []string `yaml:"when" json:"when"`
	Chain   struct {
		MaxDepth  int      `yaml:"max_depth" json:"max_depth"`
		TierSteps []string `yaml:"tier_steps" json:"tier_steps"`
	} `yaml:"chain" json:"chain"`
	Model string `yaml:"model" json:"model"`
}

// PluginConfig — declared plugin from chitin.yaml `router.plugins[]`.
// The runtime spawns the plugin per tool call (when the policy
// triggers it). See internal/router/plugins/loader.go for the
// wire protocol.
type PluginConfig struct {
	Name      string                 `yaml:"name" json:"name"`
	Type      string                 `yaml:"type" json:"type"`           // heuristic | advisor
	Runtime   string                 `yaml:"runtime" json:"runtime"`     // python3 | node | bun | bash
	Module    string                 `yaml:"module" json:"module"`       // path to script
	Config    map[string]interface{} `yaml:"config,omitempty" json:"config,omitempty"`
	TimeoutMs int                    `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
}

// PluginsTrustConfig — operator allowlist for declared plugins.
// See plugins.TrustPolicy for verification semantics.
type PluginsTrustConfig struct {
	Mode          string            `yaml:"mode,omitempty" json:"mode,omitempty"`
	TrustedPaths  []string          `yaml:"trusted_paths,omitempty" json:"trusted_paths,omitempty"`
	TrustedHashes map[string]string `yaml:"trusted_hashes,omitempty" json:"trusted_hashes,omitempty"`
}

// Policy — full router policy from chitin.yaml `router:` section.
type Policy struct {
	Enabled      bool                       `yaml:"enabled" json:"enabled"`
	Heuristics   map[string]HeuristicConfig `yaml:"heuristics" json:"heuristics"`
	Advisor      AdvisorConfig              `yaml:"advisor" json:"advisor"`
	Plugins      []PluginConfig             `yaml:"plugins,omitempty" json:"plugins,omitempty"`
	PluginsTrust PluginsTrustConfig         `yaml:"plugins_trust,omitempty" json:"plugins_trust,omitempty"`
}

// DefaultPolicy is used when chitin.yaml omits the router section.
// Off by default — operator opts in.
func DefaultPolicy() Policy {
	return Policy{
		Enabled: false,
		Heuristics: map[string]HeuristicConfig{
			"blast_radius": {Enabled: true, Threshold: 0.6},
			"floundering": {
				Enabled:         true,
				MaxLoopCount:    3,
				MaxStallSeconds: 600,
			},
		},
		Advisor: AdvisorConfig{
			Enabled: true,
			When:    []string{"blast_radius_above_threshold", "floundering_detected"},
			Chain: struct {
				MaxDepth  int      `yaml:"max_depth" json:"max_depth"`
				TierSteps []string `yaml:"tier_steps" json:"tier_steps"`
			}{MaxDepth: 3, TierSteps: []string{"T2", "T3", "T4"}},
			Model: "claude-code-headless",
		},
	}
}
