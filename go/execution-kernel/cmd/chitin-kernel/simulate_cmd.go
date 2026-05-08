package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// cmdSimulate runs the same router pipeline as a real PreToolUse
// hook BUT WITHOUT executing the action. The agent gets to ask
// "if I tried this, what would happen?" — useful for pre-flight
// reasoning, operator debugging, and CI policy regression tests.
//
// Mirrors `gate evaluate --hook-stdin` in the kernel-only path AND
// the heuristic pipeline (blast-radius, floundering, drift) in the
// router-enabled path. The output is the SAME shape as the hook
// would emit: { decision, message, ... } JSON.
//
// Differences from the real hook:
//   - No envelope spend (simulated, not real).
//   - No chain event emitted (this is hypothetical).
//
// The in-gate `claude -p` advisor was culled on 2026-05-08 (audit
// Tier 6) — see
// docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md.
// Simulation is now pure-Go and deterministic; consumers (hermes'
// approval system, operator-written cron, custom kanban-dispatched
// profile) handle any LLM follow-up off the stamped heuristic
// signals on the chain.
//
// Usage:
//   chitin-kernel simulate --hook-stdin
//
// Read a synthetic HookInput JSON from stdin, print the would-be
// decision to stdout. Operator + CI use this to test policies
// before deploying.
func cmdSimulate(args []string) {
	for _, a := range args {
		switch {
		case a == "--no-advisor":
			// Accepted as a no-op for backwards-compat with operator
			// scripts written before the advisor cull. The advisor is
			// always off in this code path now.
		case a == "--hook-stdin":
			// already-default; flag accepted for parity with gate evaluate
		case a == "--help" || a == "-h":
			fmt.Fprintln(os.Stderr, `Usage: chitin-kernel simulate [--hook-stdin] [--no-advisor]

Read a synthetic Claude Code PreToolUse JSON from stdin; emit the
decision the router would produce, WITHOUT executing the action.

Useful for:
  - Operator debugging: "if my agent tried X, what would happen?"
  - Policy regression tests: pipe corpus inputs, diff outputs
    against expected
  - Pre-action reasoning: agents emit a simulate call before
    expensive actions

Flags:
  --hook-stdin    accepted for parity with gate evaluate (default)
  --no-advisor    no-op (the in-gate advisor was culled 2026-05-08;
                  flag retained for script backwards-compat)

Output JSON shape (same as the real hook):
  { "decision": "allow"|"deny", "message": "...", ... }`)
			os.Exit(0)
		}
	}

	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		exitErr("simulate_read_stdin", err.Error())
	}
	var hookIn router.HookInput
	if err := json.Unmarshal(body, &hookIn); err != nil {
		exitErr("simulate_parse_stdin", err.Error())
	}

	cwd := hookIn.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	policy := router.LoadPolicy(cwd)

	// Run heuristics
	outcome := router.HeuristicOutcome{}
	var chainEvents []router.ChainEvent
	if cfg, ok := policy.Heuristics["blast_radius"]; ok && cfg.Enabled {
		score := router.ScoreBlastRadius(hookIn, cfg.Threshold)
		outcome.BlastRadius = &score
		if score.Fired {
			outcome.AnyFired = true
		}
	}
	if cfg, ok := policy.Heuristics["floundering"]; ok && cfg.Enabled {
		chainEvents = router.ReadChainEvents(hookIn.SessionID)
		score := router.DetectFloundering(chainEvents, router.FlounderingThresholds{
			MaxLoopCount:    cfg.MaxLoopCount,
			MaxStallSeconds: cfg.MaxStallSeconds,
		}, time.Now())
		outcome.Floundering = &score
		if score.Fired {
			outcome.AnyFired = true
		}
	}
	driftThreshold := 0.5
	if cfg, ok := policy.Heuristics["blast_radius"]; ok && cfg.Threshold > 0 {
		driftThreshold = cfg.Threshold
	}
	if chainEvents == nil {
		chainEvents = router.ReadChainEvents(hookIn.SessionID)
	}
	driftScore := router.DetectDrift(hookIn, chainEvents, driftThreshold)

	result := map[string]interface{}{
		"hook_input":        hookIn,
		"router_enabled":    policy.Enabled,
		"heuristic_outcome": outcome,
		"drift_score":       driftScore,
	}

	if outcome.AnyFired || driftScore.Fired {
		result["simulated_decision"] = "allow"
		result["simulated_message"] = "heuristic-fired (kernel verdict pass-through; chain consumer can route follow-up off stamped signals)"
	} else {
		result["simulated_decision"] = "allow"
		result["simulated_message"] = "no-signals — kernel pass-through"
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		exitErr("simulate_marshal", err.Error())
	}
}
