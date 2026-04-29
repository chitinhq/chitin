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
	if !strings.Contains(parsed["reason"], "chitin:") {
		t.Fatalf("reason missing chitin: prefix: %q", parsed["reason"])
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
