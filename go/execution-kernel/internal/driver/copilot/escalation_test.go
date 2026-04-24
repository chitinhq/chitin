package copilot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	copilotsdk "github.com/github/copilot-sdk/go"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// TestHandler_LockdownDetectedFromDecision verifies that when gov.Gate
// returns a lockdown-class Decision (RuleID == "lockdown"), the handler
// returns a *LockdownError sentinel that Run() can catch for clean
// session termination.
//
// Invariant: OnPermissionRequest(req) returns (*LockdownError, _) if and
// only if h.Gate.Evaluate returns a Decision{RuleID: "lockdown"}.
func TestHandler_LockdownDetectedFromDecision(t *testing.T) {
	lockdownGate := &mockGate{
		decision: gov.Decision{
			Allowed: false,
			RuleID:  "lockdown",
			Reason:  "agent in lockdown — 10 denials",
		},
	}
	h := &Handler{
		Gate:  lockdownGate,
		Agent: "copilot-cli",
		Cwd:   "/work",
	}
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: strPtr("anything"),
	}

	_, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	if err == nil {
		t.Fatal("expected lockdown error, got nil")
	}

	var lde *LockdownError
	if !errors.As(err, &lde) {
		t.Errorf("error should be *LockdownError, got %T: %v", err, err)
	}
	if lde != nil && lde.Agent != "copilot-cli" {
		t.Errorf("LockdownError.Agent: got %q, want copilot-cli", lde.Agent)
	}
}

// TestEscalation_TenDenialsToLockdown exercises the full pipeline:
// real gov.Gate + real Counter + real Policy with a deny-all rule.
//
// Invariant: after 10 increments on the same fingerprint (total=10,
// IsLocked=true), the very next Evaluate call hits the lockdown short-circuit
// and returns Decision{RuleID:"lockdown"}, which the Handler converts to
// *LockdownError.
//
// Boundary conditions checked:
//   - Calls 1-10: denied with guide-mode error, NOT LockdownError
//   - Call 11:    LockdownError (lockdown short-circuit fires)
//   - Call 12:    any Kind — still LockdownError (lockdown is sticky)
//   - After Reset: IsLocked returns false
func TestEscalation_TenDenialsToLockdown(t *testing.T) {
	dir := t.TempDir()

	policy := testDenyAllPolicy(t)

	counter, err := gov.OpenCounter(filepath.Join(dir, "gov.db"))
	if err != nil {
		t.Fatalf("OpenCounter: %v", err)
	}
	defer counter.Close()

	gate := &gov.Gate{
		Policy:  policy,
		Counter: counter,
		LogDir:  dir,
		Cwd:     dir,
	}

	h := &Handler{
		Gate:  gate,
		Agent: "copilot-cli-escalation-test",
		Cwd:   dir,
	}

	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: strPtr("rm -rf /tmp/escalation-target"),
	}

	// Calls 1-10: denied with guide-mode error. The 10th call still returns
	// a guide-mode error because the lockdown short-circuit at step 1 of
	// Evaluate only fires on the *next* call after total reaches 10.
	for i := 0; i < 10; i++ {
		_, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
		var lde *LockdownError
		if errors.As(err, &lde) {
			t.Fatalf("lockdown triggered prematurely at denial %d (expected on call 11)", i+1)
		}
	}

	// Sanity: the counter must now be locked after 10 denials.
	if !counter.IsLocked(h.Agent) {
		t.Fatal("counter should be locked after 10 denials, but IsLocked returned false")
	}

	// Call 11: lockdown short-circuit fires, Handler must return *LockdownError.
	_, err = h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	var lde *LockdownError
	if !errors.As(err, &lde) {
		t.Fatalf("expected *LockdownError on call 11 (post-lockdown), got: %v", err)
	}
	if lde.Agent != h.Agent {
		t.Errorf("LockdownError.Agent: got %q, want %q", lde.Agent, h.Agent)
	}

	// Post-lockdown sticky: any Kind must still return LockdownError.
	readReq := copilotsdk.PermissionRequest{
		Kind: copilotsdk.PermissionRequestKindRead,
		Path: strPtr("/etc/passwd"),
	}
	_, err = h.OnPermissionRequest(readReq, copilotsdk.PermissionInvocation{})
	if !errors.As(err, &lde) {
		t.Errorf("post-lockdown read request should return *LockdownError, got: %v", err)
	}

	// Reset clears the state.
	counter.Reset(h.Agent)
	if counter.IsLocked(h.Agent) {
		t.Error("still locked after Reset — Reset should clear the locked flag")
	}
}

// testDenyAllPolicy returns a gov.Policy with deny rules for shell.exec and
// file.read — every action kind used in the escalation test will be denied,
// forcing the counter to increment on each call.
//
// Approach: YAML fixture written to a temp dir, loaded with LoadWithInheritance.
// This matches the established pattern in gov/gate_test.go and exercises the
// full load path (ApplyDefaults, regex compilation, etc.).
func testDenyAllPolicy(t *testing.T) gov.Policy {
	t.Helper()
	yaml := `id: test-deny-all
mode: guide
rules:
  - id: deny-all-shell
    action: shell.exec
    effect: deny
    reason: "test deny-all shell"
  - id: deny-all-read
    action: file.read
    effect: deny
    reason: "test deny-all read"
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chitin.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write policy fixture: %v", err)
	}
	policy, _, err := gov.LoadWithInheritance(dir)
	if err != nil {
		t.Fatalf("LoadWithInheritance: %v", err)
	}
	return policy
}
