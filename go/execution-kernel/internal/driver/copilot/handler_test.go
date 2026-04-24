package copilot

import (
	"errors"
	"strings"
	"testing"

	copilotsdk "github.com/github/copilot-sdk/go"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// mockGate lets tests inject specific Decisions.
type mockGate struct {
	decision gov.Decision
	locked   bool
}

func (m *mockGate) Evaluate(a gov.Action, agent string) gov.Decision {
	if m.locked {
		return gov.Decision{
			Allowed:    false,
			Mode:       "enforce",
			RuleID:     "lockdown",
			Reason:     "agent in lockdown — contact operator",
			Escalation: "lockdown",
			Agent:      agent,
		}
	}
	return m.decision
}

func strPtr(s string) *string { return &s }

func TestHandler_Allow(t *testing.T) {
	h := &Handler{
		Gate:  &mockGate{decision: gov.Decision{Allowed: true, RuleID: "default-allow-shell"}},
		Agent: "copilot-cli",
		Cwd:   "/work",
	}
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: strPtr("ls /tmp"),
	}
	res, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	if err != nil {
		t.Fatalf("expected no error on allow, got %v", err)
	}
	if res.Kind != copilotsdk.PermissionRequestResultKindApproved {
		t.Errorf("Result kind: got %v, want Approved", res.Kind)
	}
}

func TestHandler_GuideDenyEncodesReasonAndSuggestion(t *testing.T) {
	h := &Handler{
		Gate: &mockGate{decision: gov.Decision{
			Allowed:          false,
			Mode:             "guide",
			RuleID:           "no-destructive-rm",
			Reason:           "Recursive delete is blocked",
			Suggestion:       "Use git rm for specific files",
			CorrectedCommand: "git rm <file>",
		}},
		Agent: "copilot-cli",
		Cwd:   "/work",
	}
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: strPtr("rm -rf /"),
	}
	_, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	if err == nil {
		t.Fatal("expected error on guide-mode deny")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "chitin: ") {
		t.Errorf("error should start with 'chitin: ', got: %q", msg)
	}
	if !strings.Contains(msg, "Recursive delete is blocked") {
		t.Errorf("error should contain reason, got: %q", msg)
	}
	if !strings.Contains(msg, "suggest: Use git rm") {
		t.Errorf("error should contain suggest segment, got: %q", msg)
	}
	if !strings.Contains(msg, "try: git rm <file>") {
		t.Errorf("error should contain try segment, got: %q", msg)
	}
}

func TestHandler_DenyWithoutSuggestionOmitsSegment(t *testing.T) {
	h := &Handler{
		Gate: &mockGate{decision: gov.Decision{
			Allowed: false,
			Mode:    "guide",
			RuleID:  "generic-deny",
			Reason:  "policy violation",
			// Suggestion and CorrectedCommand intentionally empty
		}},
		Agent: "copilot-cli",
		Cwd:   "/work",
	}
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: strPtr("do-bad-thing"),
	}
	_, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "suggest:") {
		t.Errorf("empty Suggestion should NOT produce a suggest segment, got: %q", msg)
	}
	if strings.Contains(msg, "try:") {
		t.Errorf("empty CorrectedCommand should NOT produce a try segment, got: %q", msg)
	}
}

func TestHandler_LockdownReturnsSentinelError(t *testing.T) {
	h := &Handler{
		Gate:  &mockGate{locked: true},
		Agent: "copilot-cli",
		Cwd:   "/work",
	}
	req := copilotsdk.PermissionRequest{Kind: copilotsdk.PermissionRequestKindShell, FullCommandText: strPtr("anything")}
	_, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	if err == nil {
		t.Fatal("expected lockdown error")
	}

	var lde *LockdownError
	if !errors.As(err, &lde) {
		t.Errorf("error should be *LockdownError, got %T: %v", err, err)
	}
	if lde.Agent != "copilot-cli" {
		t.Errorf("LockdownError.Agent: got %q, want copilot-cli", lde.Agent)
	}
}

func TestHandler_UnknownKindGoesThroughNormalizeAndIsDenied(t *testing.T) {
	// Unknown Kind → Action{Type: "unknown"} → gate returns deny from policy default
	h := &Handler{
		Gate: &mockGate{decision: gov.Decision{
			Allowed: false,
			Mode:    "enforce",
			Reason:  "unknown action type not permitted",
		}},
		Agent: "copilot-cli",
		Cwd:   "/work",
	}
	req := copilotsdk.PermissionRequest{Kind: copilotsdk.PermissionRequestKind("nonexistent")}
	_, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	if err == nil {
		t.Fatal("expected deny on unknown kind")
	}
}
