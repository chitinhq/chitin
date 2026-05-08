package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// TestWriteRouterTelemetry_EmitsStableSchema pins the keys + values
// emitted on the structured telemetry line. The advisor's escalate
// field was removed in the audit Tier 6 cull (2026-05-08); chain
// consumers now stamp escalation intent themselves when they read
// the heuristic signals off gov.Decision rows.
func TestWriteRouterTelemetry_EmitsStableSchema(t *testing.T) {
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
		// Regression: the advisor `escalate` field must not return —
		// chain consumers compute escalation intent off the stamped
		// signal scores, not off a router-hook flag.
		if _, ok := parsed["escalate"]; ok {
			t.Errorf("%s: telemetry line carries removed escalate field; got: %s", tc.name, buf.String())
		}
	}
}

// TestHasNonZeroSignal pins the predicate that decides whether to
// stamp a signal row on the chain. Skipping the stamp when every
// score is zero keeps the audit log small on read-only tool calls
// (Read/Glob/etc.) which trivially produce zero blast/floundering/
// drift scores.
func TestHasNonZeroSignal(t *testing.T) {
	cases := []struct {
		name    string
		outcome router.HeuristicOutcome
		drift   router.HeuristicScore
		want    bool
	}{
		{
			name:    "all-zero",
			outcome: router.HeuristicOutcome{},
			drift:   router.HeuristicScore{},
			want:    false,
		},
		{
			name: "blast-nonzero",
			outcome: router.HeuristicOutcome{
				BlastRadius: &router.HeuristicScore{Score: 0.3},
			},
			want: true,
		},
		{
			name: "floundering-nonzero",
			outcome: router.HeuristicOutcome{
				Floundering: &router.HeuristicScore{Score: 0.1},
			},
			want: true,
		},
		{
			name:  "drift-nonzero",
			drift: router.HeuristicScore{Score: 0.5},
			want:  true,
		},
		{
			name: "all-zero-but-pointers-present",
			outcome: router.HeuristicOutcome{
				BlastRadius: &router.HeuristicScore{Score: 0},
				Floundering: &router.HeuristicScore{Score: 0},
			},
			drift: router.HeuristicScore{Score: 0},
			want:  false,
		},
	}
	for _, tc := range cases {
		got := hasNonZeroSignal(tc.outcome, tc.drift)
		if got != tc.want {
			t.Errorf("%s: hasNonZeroSignal=%v want %v", tc.name, got, tc.want)
		}
	}
}
