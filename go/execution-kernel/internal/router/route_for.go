package router

// routeFor — advisory routing decision.
//
// Pure function from (signal, severity, context, policy) to
// RouteDecision. It never spawns a peer CLI, shells out, or consults an
// LLM. The result is a candidate label that can be stamped on the
// chain for downstream consumers.
//
// Contract:
//   - First rule whose Signal matches `req.Signal` wins. Severity is
//     captured for telemetry but does NOT participate in matching yet
//     (string-only, no comparator parser).
//   - Rule's Route is looked up in policy.Routes; first candidate is
//     returned. A future chain consumer can walk by quota state +
//     observed compatibility instead of taking the head.
//   - No matching rule → ErrNoRuleMatched. Caller emits no routing
//     candidate.
//   - No candidates in route → ErrRouteEmpty. Validation should have
//     prevented this; treat as a config bug worth logging.

import (
	"errors"
	"fmt"
)

var (
	ErrNoRuleMatched = errors.New("no routing rule matched the signal")
	ErrRouteEmpty    = errors.New("matched route has no candidates")
	ErrPolicyOff     = errors.New("routing policy is disabled")
)

// RouteRequest is the per-tool-call input to RouteFor.
type RouteRequest struct {
	// Signal that triggered routing: "floundering" | "blast_radius" |
	// "drift".
	Signal string

	// Human-readable severity string (recorded in telemetry; not yet
	// matched against rule.Severity programmatically — string-only
	// today, comparator parser is a future commit).
	Severity string

	// ToolCall is the worker's pending tool-call payload. Opaque to
	// RouteFor today; downstream consumers may use it for context.
	ToolCall map[string]any

	// Recent chain events for context. Same — opaque to RouteFor.
	ChainTail []map[string]any

	// Worker's workflow_id, for Provenance attribution downstream.
	WorkerWorkflowID string
}

// RouteDecision is what RouteFor returns when a rule matches.
type RouteDecision struct {
	// Rule that matched. Useful for telemetry ("rule X fired Y times
	// in the last hour") and for the future rate-limiter (rule.MaxPerHour).
	Rule RoutingRule

	// Picked candidate from rule's route (the head of the candidate
	// list today; quota-aware walk is step 6).
	Candidate Candidate

	// One-line WHY this candidate won. Always populated for
	// telemetry — "rule=floundering-loop route=patch_quality
	// candidate=copilot/gpt-5.4 (head of route, quota check deferred)".
	Rationale string
}

// RouteFor maps a router signal to a (driver, model) candidate
// using the operator's policy. Pure function — no I/O, no clocks, no
// subprocess. Safe to call from inside the gate hot path.
func RouteFor(req RouteRequest, policy RoutesPolicy) (RouteDecision, error) {
	if !policy.Enabled {
		return RouteDecision{}, ErrPolicyOff
	}
	matchedRule, ok := findFirstMatchingRule(req.Signal, policy.Rules)
	if !ok {
		return RouteDecision{}, fmt.Errorf("%w: signal=%q", ErrNoRuleMatched, req.Signal)
	}
	candidates, ok := policy.Routes[matchedRule.Route]
	if !ok || len(candidates) == 0 {
		return RouteDecision{}, fmt.Errorf("%w: route=%q (rule=%q)",
			ErrRouteEmpty, matchedRule.Route, matchedRule.Name)
	}
	picked := candidates[0]
	return RouteDecision{
		Rule:      matchedRule,
		Candidate: picked,
		Rationale: fmt.Sprintf(
			"rule=%s signal=%s route=%s candidate=%s/%s (head of route; quota-aware walk deferred to step 6)",
			matchedRule.Name, req.Signal, matchedRule.Route,
			picked.Driver, picked.Model,
		),
	}, nil
}

// findFirstMatchingRule walks rules in declared order. First rule whose
// Signal matches wins. Stable behavior — operator can rely on rule
// ordering for precedence (e.g., put a more-specific blast_radius
// rule before a catch-all drift rule).
func findFirstMatchingRule(signal string, rules []RoutingRule) (RoutingRule, bool) {
	for _, r := range rules {
		if r.Signal == signal {
			return r, true
		}
	}
	return RoutingRule{}, false
}
