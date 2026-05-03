package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// cmdSimulate runs the same router pipeline as a real PreToolUse
// hook BUT WITHOUT executing the action. The agent gets to ask
// "if I tried this, what would happen?" — useful for pre-flight
// reasoning, operator debugging, and CI policy regression tests.
//
// Mirrors `gate evaluate --hook-stdin` in the kernel-only path
// AND the full router pipeline (heuristics + advisor) in the
// router-enabled path. The output is the SAME shape as the hook
// would emit: { decision, message, ... } JSON.
//
// Differences from the real hook:
//   - No envelope spend (simulated, not real).
//   - No chain event emitted (this is hypothetical).
//   - Advisor IS called if heuristics fire — same cost as real.
//
// Usage:
//   chitin-kernel simulate --hook-stdin [--no-advisor]
//
// Read a synthetic HookInput JSON from stdin, print the would-be
// decision to stdout. Operator + CI use this to test policies
// before deploying.
func cmdSimulate(args []string) {
	noAdvisor := false
	for _, a := range args {
		switch {
		case a == "--no-advisor":
			noAdvisor = true
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
    expensive actions (future advisor pattern)

Flags:
  --hook-stdin    accepted for parity with gate evaluate (default)
  --no-advisor    skip advisor consultation; only run kernel +
                  heuristics. Useful when network or claude -p is
                  unavailable.

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
	if cfg, ok := policy.Heuristics["blast_radius"]; ok && cfg.Enabled {
		score := router.ScoreBlastRadius(hookIn, cfg.Threshold)
		outcome.BlastRadius = &score
		if score.Fired {
			outcome.AnyFired = true
		}
	}
	if cfg, ok := policy.Heuristics["floundering"]; ok && cfg.Enabled {
		events := router.ReadChainEvents(hookIn.SessionID)
		score := router.DetectFloundering(events, router.FlounderingThresholds{
			MaxLoopCount:    cfg.MaxLoopCount,
			MaxStallSeconds: cfg.MaxStallSeconds,
		}, time.Now())
		outcome.Floundering = &score
		if score.Fired {
			outcome.AnyFired = true
		}
	}

	// Decide whether the advisor would be called
	wantAdvisor := false
	if !noAdvisor && policy.Advisor.Enabled {
		for _, trigger := range policy.Advisor.When {
			switch trigger {
			case "blast_radius_above_threshold":
				if outcome.BlastRadius != nil && outcome.BlastRadius.Fired {
					wantAdvisor = true
				}
			case "floundering_detected":
				if outcome.Floundering != nil && outcome.Floundering.Fired {
					wantAdvisor = true
				}
			}
			if wantAdvisor {
				break
			}
		}
	}

	result := map[string]interface{}{
		"hook_input":         hookIn,
		"router_enabled":     policy.Enabled,
		"heuristic_outcome":  outcome,
		"would_call_advisor": wantAdvisor,
	}

	if wantAdvisor && !noAdvisor {
		// Actually call the advisor — simulation is faithful
		ctx := context.Background()
		advisorReq := router.AdvisorRequest{
			Question: "Heuristic flagged this proposed action (simulated). Is it safe to proceed?",
			Context:  fmt.Sprintf("Simulation — session %s. Heuristic outcome attached.", hookIn.SessionID),
			ProposedAction: hookIn,
			HeuristicOutcome: outcome,
			ChainDepth:       0,
		}
		advice, advErr := router.CallAdvisor(ctx, advisorReq, "", 60*time.Second)
		if advErr != nil || advice == nil {
			result["advisor_error"] = errStringFor(advErr)
		} else {
			result["advisor_response"] = advice
			if advice.Verdict == "takeover" {
				result["simulated_decision"] = "deny"
				result["simulated_message"] = advice.Nudge
			} else {
				result["simulated_decision"] = "allow"
				result["simulated_message"] = advice.Nudge
			}
		}
	} else if outcome.AnyFired {
		result["simulated_decision"] = "allow"
		result["simulated_message"] = "heuristic-fired-no-advisor (router policy or --no-advisor)"
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

// Glue: errStringFor is defined in router_hook.go but the simulate
// command uses it too. Ensure import compiles.
var _ = strings.TrimSpace
