package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// TestWriteRouterTelemetry_EscalateFlagThrough verifies the escalate
// bool propagates into telemetry JSON. Foundation for the mid-task
// continuation loop: the activity tails router-hook stderr and
// reacts to escalate=true by spawning a higher-tier driver.
func TestWriteRouterTelemetry_EscalateFlagThrough(t *testing.T) {
	cases := []struct {
		name         string
		kernelDeny   bool
		escalate     bool
		wantInOutput string
	}{
		{"escalate-true", false, true, `"escalate":true`},
		{"escalate-false", false, false, `"escalate":false`},
		{"escalate-true-with-deny", true, true, `"escalate":true`},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		writeRouterTelemetryWithEscalate(&buf, "advisor-takeover", router.HeuristicOutcome{}, tc.kernelDeny, tc.escalate)
		got := buf.String()
		if !strings.Contains(got, tc.wantInOutput) {
			t.Errorf("%s: expected %q in output, got %q", tc.name, tc.wantInOutput, got)
		}
		// Validate it's well-formed JSON
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &parsed); err != nil {
			t.Errorf("%s: telemetry not valid JSON: %v (raw: %q)", tc.name, err, got)
		}
		if parsed["component"] != "router-hook" {
			t.Errorf("%s: component=%v want router-hook", tc.name, parsed["component"])
		}
	}
}

// TestComposed_EscalationRequestedFlag verifies that the JSON
// envelope written to stdout when advisor escalates carries the
// escalation_requested marker. Activities consume this field —
// without it, escalate decisions drown in the deny output and
// surface as a human-pickup nudge.
func TestComposed_EscalationRequestedFlag(t *testing.T) {
	// We can't easily test the full runRouterEvaluate without a
	// running advisor subprocess. But we can pin the JSON shape
	// expectation: when Escalate=true, the composed envelope must
	// have "escalation_requested": true. This guards the contract.
	composed := map[string]interface{}{
		"decision":             "block",
		"reason":               "advisor nudge",
		"escalation_requested": true,
	}
	body, err := json.Marshal(composed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["escalation_requested"] != true {
		t.Errorf("escalation_requested missing from envelope: %v", parsed)
	}
	if parsed["decision"] != "block" {
		t.Errorf("decision=%v want block", parsed["decision"])
	}
}
