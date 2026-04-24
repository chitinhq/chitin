package copilot

import (
	"os"
	"path/filepath"
	"testing"

	copilotsdk "github.com/github/copilot-sdk/go"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// TestHandler_LockdownDetectedFromDecision verifies that when gov.Gate
// returns a lockdown-class Decision (RuleID == "lockdown"), the handler
// emits a *LockdownError on LockdownCh so Run() can terminate cleanly.
// The handler itself still returns nil error — the Go SDK drops handler
// errors before they reach the wire, so LockdownCh is the only reliable
// signal path.
//
// Invariant: OnPermissionRequest sends exactly one *LockdownError on
// LockdownCh if and only if h.Gate.Evaluate returns Decision{RuleID: "lockdown"}.
func TestHandler_LockdownDetectedFromDecision(t *testing.T) {
	lockdownCh := make(chan *LockdownError, 1)
	lockdownGate := &mockGate{
		decision: gov.Decision{
			Allowed: false,
			RuleID:  "lockdown",
			Reason:  "agent in lockdown — 10 denials",
		},
	}
	h := &Handler{
		Gate:       lockdownGate,
		Agent:      "copilot-cli",
		Cwd:        "/work",
		LockdownCh: lockdownCh,
	}
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: strPtr("anything"),
	}

	res, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.Kind != wireReject {
		t.Errorf("Result kind: got %v, want %v", res.Kind, wireReject)
	}

	select {
	case lde := <-lockdownCh:
		if lde == nil || lde.Agent != "copilot-cli" {
			t.Errorf("LockdownError.Agent: got %q, want copilot-cli", lde.Agent)
		}
	default:
		t.Fatal("expected *LockdownError on LockdownCh, channel was empty")
	}
}

// TestEscalation_TenDenialsToLockdown exercises the full pipeline:
// real gov.Gate + real Counter + real Policy with a deny-all rule.
//
// Invariant: after 10 increments on the same fingerprint (total=10,
// IsLocked=true), the very next Evaluate call hits the lockdown short-circuit
// and returns Decision{RuleID:"lockdown"}, which the Handler surfaces by
// sending a *LockdownError on LockdownCh.
//
// Boundary conditions checked:
//   - Calls 1-10: denied with guide-mode decision; LockdownCh stays empty
//   - Call 11:    LockdownCh receives exactly one *LockdownError
//   - Call 12:    any Kind — LockdownCh receives again (lockdown is sticky)
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

	// Buffer of 2 so calls 11 and 12 can each land without blocking; the
	// handler's default-branch drop-on-full is also fine but a wider buffer
	// keeps the test's assertions sharper.
	lockdownCh := make(chan *LockdownError, 2)
	h := &Handler{
		Gate:       gate,
		Agent:      "copilot-cli-escalation-test",
		Cwd:        dir,
		LockdownCh: lockdownCh,
	}

	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: strPtr("rm -rf /tmp/escalation-target"),
	}

	// Calls 1-10: guide-mode deny, no lockdown signal yet.
	for i := 0; i < 10; i++ {
		if _, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{}); err != nil {
			t.Fatalf("call %d: expected nil error, got %v", i+1, err)
		}
		select {
		case lde := <-lockdownCh:
			t.Fatalf("lockdown triggered prematurely at denial %d: %v", i+1, lde)
		default:
		}
	}

	// Sanity: the counter must now be locked after 10 denials.
	if !counter.IsLocked(h.Agent) {
		t.Fatal("counter should be locked after 10 denials, but IsLocked returned false")
	}

	// Call 11: lockdown short-circuit fires; LockdownCh receives.
	if _, err = h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{}); err != nil {
		t.Fatalf("call 11: expected nil error, got %v", err)
	}
	select {
	case lde := <-lockdownCh:
		if lde.Agent != h.Agent {
			t.Errorf("LockdownError.Agent: got %q, want %q", lde.Agent, h.Agent)
		}
	default:
		t.Fatal("expected *LockdownError on LockdownCh after call 11, channel was empty")
	}

	// Post-lockdown sticky: any Kind must still signal lockdown.
	readReq := copilotsdk.PermissionRequest{
		Kind: copilotsdk.PermissionRequestKindRead,
		Path: strPtr("/etc/passwd"),
	}
	if _, err = h.OnPermissionRequest(readReq, copilotsdk.PermissionInvocation{}); err != nil {
		t.Fatalf("post-lockdown read: expected nil error, got %v", err)
	}
	select {
	case <-lockdownCh:
	default:
		t.Error("post-lockdown read request should re-signal lockdown, channel was empty")
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
