package main

import (
	"encoding/json"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// TestBuildDecisionEvent_DecisionStr exhausts the (Allowed, Mode) state
// space and asserts decisionStr maps to the OUTCOME (allow/deny/guide),
// NOT the policy mode. Closes #77 audit.
//
// The five reachable cases (six minus the impossible Allowed=false +
// monitor — monitor flips that to Allowed=true at gate-time):
func TestBuildDecisionEvent_DecisionStr(t *testing.T) {
	cases := []struct {
		name    string
		allowed bool
		mode    string
		want    string
	}{
		{"allow under enforce", true, "enforce", "allow"},
		{"allow under guide (was the contested case)", true, "guide", "allow"},
		{"allow under monitor (override flipped a deny)", true, "monitor", "allow"},
		{"deny under enforce (hard block)", false, "enforce", "deny"},
		{"deny under guide (soft deny — model can retry)", false, "guide", "guide"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &gov.Decision{
				Allowed: tc.allowed,
				Mode:    tc.mode,
				RuleID:  "test-rule",
				Ts:      "2026-05-02T22:00:00Z",
			}
			ev := buildDecisionEvent(d, "test-chain", "claude-code")
			var payload map[string]any
			if err := json.Unmarshal(ev.Payload, &payload); err != nil {
				t.Fatalf("payload: %v", err)
			}
			got, _ := payload["decision"].(string)
			if got != tc.want {
				t.Errorf("decisionStr: got %q, want %q (Allowed=%v Mode=%q)",
					got, tc.want, tc.allowed, tc.mode)
			}
		})
	}
}
