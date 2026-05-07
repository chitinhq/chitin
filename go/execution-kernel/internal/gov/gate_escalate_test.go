package gov

import (
	"path/filepath"
	"testing"
	"time"
)

// TestGate_EscalateApprovedReturnsAllow exercises the integration:
// a rule with Effect=escalate causes Evaluate to insert a pending row
// and block in Wait; once the test approves the row, Evaluate returns
// with Allowed=true, RuleID="escalate-approved", and a non-empty
// EscalationID stamped on the Decision.
func TestGate_EscalateApprovedReturnsAllow(t *testing.T) {
	g, dir := newTestGate(t)
	store, err := OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	if err != nil {
		t.Fatalf("escalate store: %v", err)
	}
	defer store.Close()
	g.EscalateStore = store

	prevPoll := WaitPollInterval
	WaitPollInterval = 50 * time.Millisecond
	defer func() { WaitPollInterval = prevPoll }()

	// Override policy with an escalate rule.
	g.Policy.Rules = []Rule{{
		ID: "test-escalate", Action: ActionMatcher{"shell.exec"}, Effect: EffectEscalate,
		Reason: "needs operator",
		Escalation: &EscalateConfig{
			Channel: "cli-only", TimeoutSeconds: 5, RememberWindowSeconds: 0,
		},
	}}

	type result struct{ d Decision }
	resCh := make(chan result, 1)
	go func() {
		d := g.Evaluate(Action{Type: ActShellExec, Target: "echo"}, "agent-1", nil)
		resCh <- result{d}
	}()

	// Find the inserted pending row, approve it.
	deadline := time.Now().Add(2 * time.Second)
	var pid string
	for time.Now().Before(deadline) && pid == "" {
		rows, _ := store.ListUnresolved()
		if len(rows) > 0 {
			pid = rows[0].ID
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == "" {
		t.Fatal("no pending row appeared within 2s")
	}
	_ = store.ResolveApprove(pid, "operator-cli", 0)

	select {
	case r := <-resCh:
		if !r.d.Allowed {
			t.Errorf("Allowed = false, want true")
		}
		if r.d.RuleID != "escalate-approved" {
			t.Errorf("RuleID = %q, want escalate-approved", r.d.RuleID)
		}
		if r.d.EscalationID == "" {
			t.Error("EscalationID empty, want set")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Evaluate did not return within 2s")
	}
}

// TestGate_EscalateRememberGrantShortCircuits verifies the
// remember_grants short-circuit path: when an unexpired grant exists
// for (rule_id, agent), Evaluate must NOT call Wait — it returns
// Allowed=true with RuleID="escalate-remember-grant" immediately. This
// is what makes the operator-approval flow tolerable for repeated
// actions in the same session.
func TestGate_EscalateRememberGrantShortCircuits(t *testing.T) {
	g, dir := newTestGate(t)
	store, err := OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	g.EscalateStore = store

	g.Policy.Rules = []Rule{{
		ID: "test-escalate", Action: ActionMatcher{"shell.exec"}, Effect: EffectEscalate,
		Reason: "needs operator",
		Escalation: &EscalateConfig{
			Channel: "cli-only", TimeoutSeconds: 60, RememberWindowSeconds: 300,
		},
	}}

	// Pre-seed a remember grant.
	_ = store.InsertGrant("test-escalate", "agent-1", 300)

	d := g.Evaluate(Action{Type: ActShellExec, Target: "echo"}, "agent-1", nil)
	if !d.Allowed {
		t.Errorf("Allowed = false, want true (grant should short-circuit)")
	}
	if d.RuleID != "escalate-remember-grant" {
		t.Errorf("RuleID = %q, want escalate-remember-grant", d.RuleID)
	}
}
