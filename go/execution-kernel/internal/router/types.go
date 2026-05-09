// Package router implements the heuristic pipeline that wraps the
// kernel's deterministic gate. It runs in-process inside
// chitin-kernel for hot-path speed (~10ms; pure Go, no LLM).
//
// Pipeline (post-cull, audit Tier 6 — 2026-05-08):
//   stdin (Claude Code PreToolUse JSON)
//     ↓
//   step 1: kernel verdict (gov.Gate.Evaluate via evalHookStdin)
//     ↓
//   step 2: heuristics (BlastRadius, Floundering, Drift) +
//     operator-declared plugins
//     ↓
//   step 3: stamp heuristic-signal scores onto the chain via
//     gov.Decision so downstream consumers can route follow-ups
//     ↓
//   step 4: emit kernel verdict; chitin never spawns an LLM in-line
//
// LLM consultation lives downstream of the gate now: hermes'
// `approvals.mode: smart` for hermes-driven tools, operator-wired
// chain consumers for other drivers. See
// docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md.
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
	Source   string `json:"source,omitempty"`  // kernel | heuristic-deny | plugin-block | kernel-allow
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

// AdvisorRequest / AdvisorResponse were removed in the audit Tier 6
// cull (2026-05-08). The in-gate `claude -p` advisor that produced
// these envelopes is gone; LLM consultation lives downstream now
// (hermes' `approvals.mode: smart` for hermes-driven tools, operator-
// wired chain consumers for other drivers). See
// docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md.

// HeuristicConfig — per-heuristic policy from chitin.yaml.
type HeuristicConfig struct {
	Enabled           bool    `yaml:"enabled" json:"enabled"`
	Threshold         float64 `yaml:"threshold,omitempty" json:"threshold,omitempty"`
	MaxLoopCount      int     `yaml:"max_loop_count,omitempty" json:"max_loop_count,omitempty"`
	MaxStallSeconds   int     `yaml:"max_stall_seconds,omitempty" json:"max_stall_seconds,omitempty"`
}

// AdvisorConfig — parse-and-ignore stub kept so chitin.yaml files
// authored before the audit Tier 6 cull (2026-05-08) continue to
// load cleanly. The kernel never reads these fields anymore. See
// docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md
// for where LLM consultation lives now.
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
	Type      string                 `yaml:"type" json:"type"`           // heuristic | pre-action
	Runtime   string                 `yaml:"runtime" json:"runtime"`     // python3 | node | bun | bash
	Module    string                 `yaml:"module" json:"module"`       // path to script
	Config    map[string]interface{} `yaml:"config,omitempty" json:"config,omitempty"`
	TimeoutMs int                    `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
	// Sandbox — opt-in subprocess isolation via bubblewrap. See
	// plugins.SandboxConfig for the field set + docs/runbooks/
	// plugin-sandbox.md for operator setup.
	Sandbox SandboxConfig `yaml:"sandbox,omitempty" json:"sandbox,omitempty"`
}

// SandboxConfig — duplicates plugins.SandboxConfig at the router
// boundary so chitin.yaml only depends on `internal/router` types.
// plugin_runner.go translates this into plugins.SandboxConfig
// before passing to plugins.Run.
type SandboxConfig struct {
	Mode               string   `yaml:"mode,omitempty" json:"mode,omitempty"`
	AllowNetwork       bool     `yaml:"allow_network,omitempty" json:"allow_network,omitempty"`
	AllowWrite         bool     `yaml:"allow_write,omitempty" json:"allow_write,omitempty"`
	ExtraReadOnlyBinds []string `yaml:"extra_ro_binds,omitempty" json:"extra_ro_binds,omitempty"`
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
			Enabled: false,
			When:    nil,
			Chain: struct {
				MaxDepth  int      `yaml:"max_depth" json:"max_depth"`
				TierSteps []string `yaml:"tier_steps" json:"tier_steps"`
			}{},
			Model: "",
		},
	}
}
