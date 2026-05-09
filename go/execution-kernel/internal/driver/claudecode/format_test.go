package claudecode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestFormat_AllowIsEmptyExit0(t *testing.T) {
	body, code := Format(gov.Decision{Allowed: true, RuleID: "default-allow-shell"})
	if code != ExitAllow {
		t.Fatalf("code=%d want 0 (allow)", code)
	}
	if len(body) != 0 {
		t.Fatalf("allow stdout must be empty, got %q", body)
	}
}

func TestFormat_DenyIsBlockExit2WithReason(t *testing.T) {
	d := gov.Decision{
		Allowed: false,
		RuleID:  "no-rm",
		Reason:  "no rm",
	}
	body, code := Format(d)
	if code != ExitBlock {
		t.Fatalf("code=%d want 2 (block)", code)
	}
	var parsed map[string]string
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body not valid JSON: %v\nbody=%s", err, body)
	}
	if parsed["decision"] != "block" {
		t.Fatalf("decision=%q want block", parsed["decision"])
	}
	if parsed["rule_id"] != "no-rm" {
		t.Fatalf("rule_id=%q want no-rm", parsed["rule_id"])
	}
	if !strings.Contains(parsed["reason"], "chitin:") {
		t.Fatalf("reason missing chitin: prefix: %q", parsed["reason"])
	}
	if !strings.Contains(parsed["reason"], "no-rm") {
		t.Fatalf("reason missing kernel rule id: %q", parsed["reason"])
	}
	if !strings.Contains(parsed["reason"], "no rm") {
		t.Fatalf("reason missing the policy reason: %q", parsed["reason"])
	}
}

func TestFormat_DenyIncludesSuggestionAndCorrected(t *testing.T) {
	d := gov.Decision{
		Allowed:          false,
		RuleID:           "no-rm",
		Reason:           "no rm",
		Suggestion:       "use git rm",
		CorrectedCommand: "git rm <files>",
	}
	body, _ := Format(d)
	var parsed map[string]string
	_ = json.Unmarshal(body, &parsed)
	if !strings.Contains(parsed["reason"], "suggest: use git rm") {
		t.Fatalf("missing suggest segment: %q", parsed["reason"])
	}
	if !strings.Contains(parsed["reason"], "try: git rm <files>") {
		t.Fatalf("missing try segment: %q", parsed["reason"])
	}
}

func TestFormat_DenyOmitsBlankSegments(t *testing.T) {
	d := gov.Decision{Allowed: false, Reason: "no rm"}
	body, _ := Format(d)
	var parsed map[string]string
	_ = json.Unmarshal(body, &parsed)
	if strings.Contains(parsed["reason"], "suggest:") {
		t.Fatalf("blank Suggestion should not produce 'suggest:': %q", parsed["reason"])
	}
	if strings.Contains(parsed["reason"], "try:") {
		t.Fatalf("blank CorrectedCommand should not produce 'try:': %q", parsed["reason"])
	}
}

func TestFormat_EnvelopeExhaustedIsBlock(t *testing.T) {
	// Envelope-exhausted decisions go through the same block path as
	// policy denials — model sees a coherent reason and can ask the
	// operator to grant more budget.
	d := gov.Decision{
		Allowed: false,
		RuleID:  "envelope-exhausted",
		Reason:  "envelope 01J...: envelope exhausted: id=01J... calls=10/10",
	}
	body, code := Format(d)
	if code != ExitBlock {
		t.Fatalf("code=%d want 2", code)
	}
	if !strings.Contains(string(body), "envelope") {
		t.Fatalf("body missing envelope mention: %s", body)
	}
}

func TestFormat_LockdownStopsAgentLoop(t *testing.T) {
	// Regression: a lockdown decision must set continue:false +
	// stopReason so Claude Code stops the agent loop instead of
	// blocking just the current tool call. Without this the model
	// retries variants forever — observed in chain as 26 lockdown
	// fires from one envelope (envelope_id 01KQRF7D66GYXZ829G3QGRKWQB)
	// over 7+ hours on 2026-05-06.
	d := gov.Decision{
		Allowed: false,
		RuleID:  "lockdown",
		Reason:  "agent in lockdown — contact operator",
		Agent:   "claude-code",
	}
	body, code := Format(d)
	if code != ExitBlock {
		t.Fatalf("code=%d want %d (block)", code, ExitBlock)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body not valid JSON: %v\nbody=%s", err, body)
	}
	if parsed["decision"] != "block" {
		t.Fatalf("decision=%v want block", parsed["decision"])
	}
	cont, ok := parsed["continue"].(bool)
	if !ok {
		t.Fatalf("continue field missing or not bool: %v (body=%s)", parsed["continue"], body)
	}
	if cont {
		t.Fatalf("continue=true on lockdown — agent loop will not stop")
	}
	stopReason, _ := parsed["stopReason"].(string)
	if !strings.Contains(stopReason, "lockdown") {
		t.Fatalf("stopReason missing lockdown mention: %q", stopReason)
	}
	if !strings.Contains(stopReason, "chitin-kernel gate reset --agent=claude-code") {
		t.Fatalf("stopReason missing operator-recovery command with agent name: %q", stopReason)
	}
}

func TestFormat_LockdownWithoutAgentDegradesGracefully(t *testing.T) {
	// gov/gate.go always stamps Agent on lockdown decisions, but the
	// formatter shouldn't emit a malformed reset command if a future
	// caller forgets — the placeholder makes the missing field
	// obvious to the operator instead of silent string concatenation.
	d := gov.Decision{Allowed: false, RuleID: "lockdown", Reason: "x"}
	body, _ := Format(d)
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	stopReason, _ := parsed["stopReason"].(string)
	if !strings.Contains(stopReason, "--agent=<agent>") {
		t.Fatalf("missing placeholder for empty Agent: %q", stopReason)
	}
}

func TestFormat_RegularDenyDoesNotStopAgent(t *testing.T) {
	// Inverse guard: only lockdown sets continue:false. A regular
	// deny must leave the field unset so the model can retry — the
	// retry-driven escalation is what *gets* the agent to lockdown
	// in the first place (per gov/escalation.go RecordDenial). If
	// every deny stopped the loop, lockdown would never trigger.
	d := gov.Decision{Allowed: false, RuleID: "no-rm", Reason: "no rm"}
	body, _ := Format(d)
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	if _, present := parsed["continue"]; present {
		t.Fatalf("continue field set on non-lockdown deny: %v", parsed["continue"])
	}
	if _, present := parsed["stopReason"]; present {
		t.Fatalf("stopReason set on non-lockdown deny: %v", parsed["stopReason"])
	}
}
