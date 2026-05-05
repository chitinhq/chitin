package router

import (
	"time"
)

// EvaluateResult is the structured output of EvaluateHeuristics —
// the in-process Go pipeline that runs blast-radius + floundering
// in pure Go and decides whether the (slow) advisor needs to be
// consulted. The advisor call itself is left to an external caller
// (TS shim or `chitin-kernel router evaluate`) so the hot path
// stays pure-Go.
//
// Wire shape (JSON):
//
//	{
//	  "decision": "allow" | "deny",
//	  "advisor_needed": bool,
//	  "advisor_request": { ...AdvisorRequest } | null,
//	  "heuristic_outcome": { ...HeuristicOutcome }
//	}
type EvaluateResult struct {
	Decision         string            `json:"decision"`
	AdvisorNeeded    bool              `json:"advisor_needed"`
	AdvisorRequest   *AdvisorRequest   `json:"advisor_request,omitempty"`
	HeuristicOutcome HeuristicOutcome  `json:"heuristic_outcome"`
}

// EvaluateHeuristics is the in-process router pipeline. Inputs:
//
//   - input:         the PreToolUse payload (already parsed)
//   - policy:        the loaded router policy (LoadPolicy)
//   - kernelDeny:    true iff the deterministic kernel verdict was deny
//   - kernelMessage: the kernel's deny reason (empty when allowed)
//   - now:           injected time.Now for testability
//
// The function:
//
//  1. Runs blast-radius on the input (when enabled in policy)
//  2. Runs floundering against the chain (when enabled in policy)
//  3. Decides advisor_needed by walking policy.Advisor.When
//  4. Builds AdvisorRequest when advisor_needed is true (so the
//     external advisor caller has all context in one envelope)
//
// Returns EvaluateResult — pure data, no side effects, no
// subprocess spawning. Calling code is responsible for:
//
//   - reading chain events for floundering (passed in as `events`)
//   - invoking the advisor when advisor_needed
//   - composing the final hook output
//
// Invariant: when advisor_needed is true, AdvisorRequest is
// non-nil. When false, AdvisorRequest is nil.
func EvaluateHeuristics(input HookInput, policy Policy, events []ChainEvent, kernelDeny bool, kernelMessage string, now time.Time) EvaluateResult {
	outcome := HeuristicOutcome{}

	if cfg, ok := policy.Heuristics["blast_radius"]; ok && cfg.Enabled {
		score := ScoreBlastRadius(input, cfg.Threshold)
		outcome.BlastRadius = &score
		if score.Fired {
			outcome.AnyFired = true
		}
	}
	if cfg, ok := policy.Heuristics["floundering"]; ok && cfg.Enabled {
		score := DetectFloundering(events, FlounderingThresholds{
			MaxLoopCount:    cfg.MaxLoopCount,
			MaxStallSeconds: cfg.MaxStallSeconds,
		}, now)
		outcome.Floundering = &score
		if score.Fired {
			outcome.AnyFired = true
		}
	}

	decision := "allow"
	if kernelDeny {
		decision = "deny"
	}

	advisorNeeded := false
	if policy.Advisor.Enabled {
		for _, trigger := range policy.Advisor.When {
			switch trigger {
			case "blast_radius_above_threshold":
				if outcome.BlastRadius != nil && outcome.BlastRadius.Fired {
					advisorNeeded = true
				}
			case "floundering_detected":
				if outcome.Floundering != nil && outcome.Floundering.Fired {
					advisorNeeded = true
				}
			case "kernel_denied":
				if kernelDeny {
					advisorNeeded = true
				}
			}
			if advisorNeeded {
				break
			}
		}
	}

	res := EvaluateResult{
		Decision:         decision,
		AdvisorNeeded:    advisorNeeded,
		HeuristicOutcome: outcome,
	}
	if advisorNeeded {
		question := "Heuristic flagged this action. Is the agent on track, or should it pause/escalate?"
		if kernelDeny {
			question = "The kernel denied this action: " + kernelMessage + ". Should the agent be re-routed or is this denial correct?"
		}
		req := AdvisorRequest{
			Question:         question,
			Context:          "Session: " + input.SessionID + ". Heuristic outcome attached.",
			ProposedAction:   input,
			HeuristicOutcome: outcome,
			ChainDepth:       0,
		}
		res.AdvisorRequest = &req
	}
	return res
}
