package router

// routeFor — peer-escalation routing decision.
//
// Per docs/design/2026-05-06-kernel-gate-escalation.md (step 2 of 6):
// pure function from (signal, severity, context, policy) → RouteDecision.
// NO peer spawning yet (that's step 3); NO live quota integration yet
// (that's step 6 — observed dimensions feed back). This step ships
// the deterministic decision engine reachable from tests but not wired
// into the gate.
//
// Contract:
//   - First rule whose Signal matches `req.Signal` wins. Severity is
//     captured for telemetry but does NOT participate in matching yet
//     (string-only, no comparator parser).
//   - Rule's Route is looked up in policy.Routes; first candidate is
//     returned (FUTURE step 6: walk by quota state + observed
//     compatibility, not just first).
//   - No matching rule → ErrNoRuleMatched. Caller falls back to the
//     existing kernel deny+escalation_requested behavior.
//   - No candidates in route → ErrRouteEmpty. Validation should have
//     prevented this; treat as a config bug worth logging.

import (
	"errors"
	"fmt"
)

var (
	ErrNoRuleMatched = errors.New("no escalation rule matched the signal")
	ErrRouteEmpty    = errors.New("matched route has no candidates")
	ErrPolicyOff     = errors.New("escalation policy is disabled")
)

// RouteRequest is the per-tool-call input to RouteFor.
type RouteRequest struct {
	// Signal that triggered the escalation: "floundering" |
	// "blast_radius" | "drift" | "advisor_takeover".
	Signal string

	// Human-readable severity string (recorded in telemetry; not yet
	// matched against rule.Severity programmatically — string-only
	// today, comparator parser is a future commit).
	Severity string

	// ToolCall is the worker's pending tool-call payload. Opaque to
	// RouteFor today; passed through to spawnPeer (step 3) which
	// wraps it for the peer CLI's prompt.
	ToolCall map[string]any

	// Recent chain events for context. Same — opaque to RouteFor;
	// wrapped by spawnPeer.
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

// RouteFor maps an escalation signal to a (driver, model) candidate
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
