package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// TestWriteRouterTelemetry_StableSchema pins the JSONL telemetry
// shape emitted by writeRouterTelemetry. Downstream consumers
// (analysis lib, operator dashboards) parse this via a fixed key
// set; any rename or reorder breaks them silently.
//
// The escalate field was removed alongside the in-gate LLM advisor
// in the audit Tier 6 cull (2026-05-08); chain consumers stamp any
// escalation intent themselves when they read the heuristic signals
// off the gov-decisions log.
func TestWriteRouterTelemetry_StableSchema(t *testing.T) {
	cases := []struct {
		name       string
		kind       string
		kernelDeny bool
	}{
		{"heuristic-fired-allow", "heuristic-fired", false},
		{"heuristic-fired-deny", "heuristic-fired", true},
		{"pre-action-block", "pre-action-block", false},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		writeRouterTelemetry(&buf, tc.kind, router.HeuristicOutcome{}, tc.kernelDeny)
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &parsed); err != nil {
			t.Errorf("%s: telemetry not valid JSON: %v (raw: %q)", tc.name, err, buf.String())
			continue
		}
		if parsed["msg"] != tc.kind {
			t.Errorf("%s: msg=%v want %v", tc.name, parsed["msg"], tc.kind)
		}
		if parsed["component"] != "router-hook" {
			t.Errorf("%s: component=%v want router-hook", tc.name, parsed["component"])
		}
		if parsed["kernel_denied"] != tc.kernelDeny {
			t.Errorf("%s: kernel_denied=%v want %v", tc.name, parsed["kernel_denied"], tc.kernelDeny)
		}
		// The escalate field MUST be absent — its presence would
		// signal that the in-gate advisor path crept back in.
		if _, present := parsed["escalate"]; present {
			t.Errorf("%s: escalate field present (should have been removed in audit Tier 6 cull); raw=%q",
				tc.name, buf.String())
		}
	}
}

// TestHasNonZeroSignal_BoundaryCases pins the predicate that decides
// whether the router stamps a heuristic-signal row. The predicate
// drives chain bloat: too eager and every read-only tool call writes
// a stamping row; too lax and sub-threshold training signal is lost.
func TestHasNonZeroSignal_BoundaryCases(t *testing.T) {
	cases := []struct {
		name    string
		blast   *router.HeuristicScore
		flound  *router.HeuristicScore
		drift   router.HeuristicScore
		wantHit bool
	}{
		{"all-zero", nil, nil, router.HeuristicScore{Score: 0}, false},
		{"blast-non-zero-sub-threshold", &router.HeuristicScore{Score: 0.1, Fired: false}, nil, router.HeuristicScore{}, true},
		{"floundering-non-zero", nil, &router.HeuristicScore{Score: 0.5}, router.HeuristicScore{}, true},
		{"drift-non-zero", nil, nil, router.HeuristicScore{Score: 0.3}, true},
	}
	for _, tc := range cases {
		o := router.HeuristicOutcome{BlastRadius: tc.blast, Floundering: tc.flound}
		got := hasNonZeroSignal(o, tc.drift)
		if got != tc.wantHit {
			t.Errorf("%s: hasNonZeroSignal=%v want %v", tc.name, got, tc.wantHit)
		}
	}
}
