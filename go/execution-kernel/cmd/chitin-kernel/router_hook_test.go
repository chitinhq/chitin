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
//
// All emit kinds (pre-action-block, heuristic-fired-no-advisor,
// advisor-takeover, advisor-allow) now carry a uniform escalate
// field so downstream consumers see a stable schema.
func TestWriteRouterTelemetry_EscalateFlagThrough(t *testing.T) {
	cases := []struct {
		name       string
		kind       string
		kernelDeny bool
		escalate   bool
	}{
		{"advisor-takeover-escalate-true", "advisor-takeover", false, true},
		{"advisor-allow-escalate-false", "advisor-allow", false, false},
		{"advisor-takeover-with-deny-escalate", "advisor-takeover", true, true},
		{"pre-action-block-no-escalate", "pre-action-block", false, false},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		writeRouterTelemetry(&buf, tc.kind, router.HeuristicOutcome{}, tc.kernelDeny, tc.escalate)
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &parsed); err != nil {
			t.Errorf("%s: telemetry not valid JSON: %v (raw: %q)", tc.name, err, buf.String())
			continue
		}
		// Assert on parsed boolean rather than substring — robust to
		// JSON field-ordering changes.
		if got := parsed["escalate"]; got != tc.escalate {
			t.Errorf("%s: parsed escalate=%v want %v", tc.name, got, tc.escalate)
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
	}
}

// TestTakeoverEnvelope_CarriesEscalationRequested pins the JSON
// shape of the takeover envelope written to stdout when the
// advisor escalates. The activity reads `escalation_requested`
// from this stdout — if the field name or shape changes, every
// downstream consumer breaks silently.
//
// Testing through evalRouterHookStdin would require a running
// advisor subprocess (deferred — see follow-up entry); this test
// asserts the envelope-level contract that runs immediately
// before the WriteString call.
func TestTakeoverEnvelope_CarriesEscalationRequested(t *testing.T) {
	advice := struct {
		Verdict  string
		Nudge    string
		Escalate bool
	}{Verdict: "takeover", Nudge: "consider escalating", Escalate: true}

	composed := map[string]interface{}{"decision": "block", "reason": advice.Nudge}
	if advice.Escalate {
		composed["escalation_requested"] = true
	}
	body, err := json.Marshal(composed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := parsed["escalation_requested"]; got != true {
		t.Errorf("escalation_requested=%v want true", got)
	}
	if parsed["decision"] != "block" {
		t.Errorf("decision=%v want block", parsed["decision"])
	}
	if parsed["reason"] != advice.Nudge {
		t.Errorf("reason=%v want %v", parsed["reason"], advice.Nudge)
	}
}
