package router

import (
	"errors"
	"strings"
	"testing"
)

func samplePolicy() RoutesPolicy {
	return RoutesPolicy{
		Version: 1,
		Enabled: true,
		Rules: []RoutingRule{
			{Name: "floundering-loop", Signal: "floundering", Severity: ">= 2 loops", Route: "patch_quality", MaxPerHour: 10},
			{Name: "blast-radius-large", Signal: "blast_radius", Severity: "> 25 files", Route: "reasoning_depth", MaxPerHour: 5},
			{Name: "drift-high", Signal: "drift", Severity: "score>=0.6", Route: "reasoning_depth", MaxPerHour: 3},
		},
		Routes: map[string][]Candidate{
			"patch_quality": {
				{Driver: "copilot", Model: "gpt-5.4"},
				{Driver: "claude", Model: "claude-opus-4-6"},
			},
			"reasoning_depth": {
				{Driver: "claude", Model: "claude-opus-4-7"},
				{Driver: "copilot", Model: "gpt-5.5"},
			},
		},
	}
}

func TestRouteFor_FlounderingMatch(t *testing.T) {
	d, err := RouteFor(RouteRequest{Signal: "floundering"}, samplePolicy())
	if err != nil {
		t.Fatalf("expected match, got %v", err)
	}
	if d.Rule.Name != "floundering-loop" {
		t.Errorf("rule: got %q want floundering-loop", d.Rule.Name)
	}
	if d.Candidate.Driver != "copilot" || d.Candidate.Model != "gpt-5.4" {
		t.Errorf("candidate: got %s/%s want copilot/gpt-5.4", d.Candidate.Driver, d.Candidate.Model)
	}
	if !strings.Contains(d.Rationale, "patch_quality") {
		t.Errorf("rationale should reference matched route; got: %s", d.Rationale)
	}
}

func TestRouteFor_BlastRadiusMatch(t *testing.T) {
	d, err := RouteFor(RouteRequest{Signal: "blast_radius"}, samplePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if d.Candidate.Driver != "claude" || d.Candidate.Model != "claude-opus-4-7" {
		t.Errorf("expected reasoning_depth head; got %s/%s", d.Candidate.Driver, d.Candidate.Model)
	}
}

func TestRouteFor_DriftMatch(t *testing.T) {
	d, err := RouteFor(RouteRequest{Signal: "drift"}, samplePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if d.Rule.Name != "drift-high" {
		t.Errorf("expected drift-high; got %q", d.Rule.Name)
	}
}

func TestRouteFor_NoRuleMatched(t *testing.T) {
	_, err := RouteFor(RouteRequest{Signal: "telepathy"}, samplePolicy())
	if !errors.Is(err, ErrNoRuleMatched) {
		t.Errorf("expected ErrNoRuleMatched; got %v", err)
	}
}

func TestRouteFor_RuleOrderingDeterminesPrecedence(t *testing.T) {
	// Two rules for the same signal — first one wins.
	policy := RoutesPolicy{
		Version: 1,
		Enabled: true,
		Rules: []RoutingRule{
			{Name: "floundering-cheap", Signal: "floundering", Route: "cheap"},
			{Name: "floundering-deep", Signal: "floundering", Route: "deep"},
		},
		Routes: map[string][]Candidate{
			"cheap": {{Driver: "copilot", Model: "gpt-4o-mini"}},
			"deep":  {{Driver: "claude", Model: "claude-opus-4-7"}},
		},
	}
	d, err := RouteFor(RouteRequest{Signal: "floundering"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if d.Rule.Name != "floundering-cheap" {
		t.Errorf("first-rule precedence broken: got %q want floundering-cheap", d.Rule.Name)
	}
}

func TestRouteFor_PolicyDisabled(t *testing.T) {
	policy := samplePolicy()
	policy.Enabled = false
	_, err := RouteFor(RouteRequest{Signal: "floundering"}, policy)
	if !errors.Is(err, ErrPolicyOff) {
		t.Errorf("disabled policy should return ErrPolicyOff; got %v", err)
	}
}

func TestRouteFor_RouteRefNotInRoutesMap(t *testing.T) {
	// Validation should catch this in LoadRoutesPolicy, but RouteFor
	// must also tolerate it (fail-open for the gate, log the bug).
	policy := RoutesPolicy{
		Version: 1,
		Enabled: true,
		Rules: []RoutingRule{
			{Name: "x", Signal: "floundering", Route: "missing"},
		},
		Routes: map[string][]Candidate{
			"other": {{Driver: "copilot", Model: "gpt-4.1"}},
		},
	}
	_, err := RouteFor(RouteRequest{Signal: "floundering"}, policy)
	if !errors.Is(err, ErrRouteEmpty) {
		t.Errorf("expected ErrRouteEmpty; got %v", err)
	}
}

func TestRouteFor_RoutePresentButEmpty(t *testing.T) {
	policy := RoutesPolicy{
		Version: 1,
		Enabled: true,
		Rules:   []RoutingRule{{Name: "x", Signal: "floundering", Route: "empty"}},
		Routes:  map[string][]Candidate{"empty": {}},
	}
	_, err := RouteFor(RouteRequest{Signal: "floundering"}, policy)
	if !errors.Is(err, ErrRouteEmpty) {
		t.Errorf("expected ErrRouteEmpty; got %v", err)
	}
}

func TestRouteFor_FirstCandidateWins(t *testing.T) {
	// Until step 6 (quota-aware walk), the head of the candidate
	// list always wins. Lock that in so step 6 is a deliberate
	// behavior change.
	policy := samplePolicy()
	d, _ := RouteFor(RouteRequest{Signal: "floundering"}, policy)
	wantHead := policy.Routes["patch_quality"][0]
	if d.Candidate != wantHead {
		t.Errorf("step 2 must always pick chain head; got %v want %v", d.Candidate, wantHead)
	}
}

func TestRouteFor_RationaleNonEmpty(t *testing.T) {
	d, _ := RouteFor(RouteRequest{Signal: "floundering"}, samplePolicy())
	if d.Rationale == "" {
		t.Error("rationale must always be populated for telemetry")
	}
}
