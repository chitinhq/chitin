package gov

import (
	"path/filepath"
	"testing"
)

// gateWithEnvelope is the envelope-aware companion to newTestGate. It
// wires up the cost/tier callbacks so the Decision row gets non-zero
// stamps, lets tests inspect what was estimated, and creates a fresh
// envelope at the given limits.
func gateWithEnvelope(t *testing.T, limits BudgetLimits) (*Gate, *BudgetEnvelope) {
	t.Helper()
	g, dir := newTestGate(t)
	store, err := OpenBudgetStore(filepath.Join(dir, "gov.db"))
	if err != nil {
		t.Fatalf("OpenBudgetStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	env, err := store.Create(limits)
	if err != nil {
		t.Fatalf("Create envelope: %v", err)
	}
	g.ClassifyTier = func(a Action) Tier {
		if a.Type == ActFileRead {
			return T0Local
		}
		return T2Expensive
	}
	g.EstimateCost = func(a Action, _ string) CostDelta {
		return CostDelta{ToolCalls: 1, InputBytes: int64(len(a.Target)), USD: 0.001}
	}
	return g, env
}

func TestGate_NilEnvelope_NoFieldsStamped(t *testing.T) {
	g, _ := newTestGate(t)
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if !d.Allowed {
		t.Fatalf("expected allow, got %+v", d)
	}
	if d.EnvelopeID != "" || d.Tier != "" || d.CostUSD != 0 ||
		d.InputBytes != 0 || d.OutputBytes != 0 || d.ToolCalls != 0 {
		t.Fatalf("nil envelope should leave envelope/tier/cost fields zero, got %+v", d)
	}
}

func TestGate_NonNilEnvelope_AllowStampsFields(t *testing.T) {
	g, env := gateWithEnvelope(t, BudgetLimits{MaxToolCalls: 10, MaxInputBytes: 1024})
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", env)
	if !d.Allowed {
		t.Fatalf("allow expected, got %+v", d)
	}
	if d.EnvelopeID != env.ID {
		t.Fatalf("EnvelopeID=%q want %q", d.EnvelopeID, env.ID)
	}
	if d.Tier != T0Local {
		t.Fatalf("Tier=%q want T0", d.Tier)
	}
	if d.ToolCalls != 1 {
		t.Fatalf("ToolCalls=%d want 1", d.ToolCalls)
	}
	if d.InputBytes != int64(len("/tmp/x")) {
		t.Fatalf("InputBytes=%d want %d", d.InputBytes, len("/tmp/x"))
	}
	if d.CostUSD <= 0 {
		t.Fatalf("CostUSD=%v want > 0 (callback returned 0.001)", d.CostUSD)
	}

	// Spend was actually applied to the envelope row.
	st, _ := env.Inspect()
	if st.SpentCalls != 1 {
		t.Fatalf("SpentCalls=%d want 1", st.SpentCalls)
	}
}

func TestGate_PolicyDeny_DoesNotSpend(t *testing.T) {
	// rm -rf is denied by policy; envelope must not be debited.
	g, env := gateWithEnvelope(t, BudgetLimits{MaxToolCalls: 10})
	d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", env)
	if d.Allowed {
		t.Fatalf("policy deny expected, got allow")
	}
	if d.RuleID != "no-rm" {
		t.Fatalf("RuleID=%q want no-rm", d.RuleID)
	}
	st, _ := env.Inspect()
	if st.SpentCalls != 0 {
		t.Fatalf("policy-denied action must not spend; SpentCalls=%d", st.SpentCalls)
	}
	// Stamping still happens for analytics: EnvelopeID + Tier on row.
	if d.EnvelopeID != env.ID {
		t.Fatalf("policy-denied row must still carry EnvelopeID")
	}
	if d.Tier != T2Expensive {
		t.Fatalf("Tier=%q want T2", d.Tier)
	}
}

func TestGate_EnvelopeExhausted_ConvertsAllowToDeny(t *testing.T) {
	g, env := gateWithEnvelope(t, BudgetLimits{MaxToolCalls: 1})
	// First call eats the cap.
	d1 := g.Evaluate(Action{Type: ActFileRead, Target: "/a"}, "agent1", env)
	if !d1.Allowed {
		t.Fatalf("first call should allow, got %+v", d1)
	}
	// Second call would over-spend → envelope-exhausted, log-as-deny.
	d2 := g.Evaluate(Action{Type: ActFileRead, Target: "/b"}, "agent1", env)
	if d2.Allowed {
		t.Fatalf("second call must be denied (envelope exhausted), got allow")
	}
	if d2.RuleID != "envelope-exhausted" {
		t.Fatalf("RuleID=%q want envelope-exhausted", d2.RuleID)
	}
	if d2.EnvelopeID != env.ID {
		t.Fatalf("EnvelopeID missing on exhausted decision")
	}
}

// TestGate_EnvelopeExhausted_DoesNotIncrementCounter is the regression
// test for the review finding: hitting an operator-imposed budget cap
// is not agent misbehavior, so it must not feed the lockdown ladder.
//
// After the bug fix that distinguishes RuleIDs by error class, the
// FIRST budget-denied call returns envelope-exhausted (the spend that
// breached the cap), and subsequent calls return envelope-closed (the
// envelope is now sticky-closed). All envelope-* RuleIDs are
// budget-class and must be exempt from counter accounting.
func TestGate_EnvelopeExhausted_DoesNotIncrementCounter(t *testing.T) {
	g, env := gateWithEnvelope(t, BudgetLimits{MaxToolCalls: 1})
	g.Evaluate(Action{Type: ActFileRead, Target: "/a"}, "agent1", env) // burns the cap
	for i := 0; i < 15; i++ {
		// First iter sees envelope-exhausted (the call that breaches +
		// triggers sticky-close). Subsequent iters see envelope-closed.
		// Both are envelope-class denials and must be exempt.
		d := g.Evaluate(Action{Type: ActFileRead, Target: "/x"}, "agent1", env)
		if d.RuleID != "envelope-exhausted" && d.RuleID != "envelope-closed" {
			t.Fatalf("iter %d: RuleID=%q want envelope-exhausted or envelope-closed", i, d.RuleID)
		}
	}
	if lv := g.Counter.Level("agent1"); lv != "normal" {
		t.Fatalf("agent should remain normal — envelope hits are not strikes; got level=%q", lv)
	}
	if g.Counter.IsLocked("agent1") {
		t.Fatalf("agent must not be locked from budget hits")
	}
}

func TestGate_PolicyDenyStillIncrementsCounter(t *testing.T) {
	// Sanity: with envelope present, real policy denials still feed the
	// counter — only envelope denials are exempt.
	g, env := gateWithEnvelope(t, BudgetLimits{MaxToolCalls: 100})
	for i := 0; i < 3; i++ {
		g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", env)
	}
	if lv := g.Counter.Level("agent1"); lv != "elevated" {
		t.Fatalf("after 3 policy denials, level=%q want elevated", lv)
	}
}

// TestGate_MonitorMode_DoesNotOverrideEnvelope asserts the spec
// invariant: monitor mode flips policy denials to allow, but envelope
// caps are hard contracts — the budget stays enforced even in monitor.
func TestGate_MonitorMode_DoesNotOverrideEnvelope(t *testing.T) {
	g, env := gateWithEnvelope(t, BudgetLimits{MaxToolCalls: 1})
	g.Policy.Mode = "monitor"
	g.Policy.InvariantModes = nil

	// First read: allow + spend (under cap).
	d1 := g.Evaluate(Action{Type: ActFileRead, Target: "/a"}, "agent1", env)
	if !d1.Allowed {
		t.Fatalf("first read should allow (under cap), got %+v", d1)
	}
	// Second read: would exceed cap. Envelope denial must stand even in monitor.
	d2 := g.Evaluate(Action{Type: ActFileRead, Target: "/b"}, "agent1", env)
	if d2.Allowed {
		t.Fatalf("envelope cap must hold under monitor mode; got allow")
	}
	if d2.RuleID != "envelope-exhausted" {
		t.Fatalf("RuleID=%q want envelope-exhausted", d2.RuleID)
	}
}

// TestGate_EnvelopeClosedYieldsClosedRuleID is the regression test for
// the audit-analytics ambiguity that Copilot review surfaced: an
// operator-closed envelope must produce RuleID=envelope-closed, not
// envelope-exhausted. They mean different things downstream.
func TestGate_EnvelopeClosedYieldsClosedRuleID(t *testing.T) {
	g, env := gateWithEnvelope(t, BudgetLimits{MaxToolCalls: 100})
	// Operator closes the envelope explicitly (not budget-driven).
	if err := env.CloseEnvelope(); err != nil {
		t.Fatalf("CloseEnvelope: %v", err)
	}
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/x"}, "agent1", env)
	if d.Allowed {
		t.Fatalf("expected deny on closed envelope")
	}
	if d.RuleID != "envelope-closed" {
		t.Fatalf("RuleID=%q want envelope-closed", d.RuleID)
	}
	// Counter must not bump for envelope-class denials (existing
	// invariant — re-asserted here for the closed sub-class).
	if lv := g.Counter.Level("agent1"); lv != "normal" {
		t.Fatalf("envelope-closed must not feed counter; level=%q", lv)
	}
}

// TestGate_LockdownDoesNotSpend confirms the lockdown short-circuit
// still bypasses Spend: a locked agent's actions don't burn budget.
func TestGate_LockdownDoesNotSpend(t *testing.T) {
	g, env := gateWithEnvelope(t, BudgetLimits{MaxToolCalls: 100})
	g.Counter.Lockdown("agent1")
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/a"}, "agent1", env)
	if d.Allowed {
		t.Fatalf("locked agent must be denied")
	}
	if d.RuleID != "lockdown" {
		t.Fatalf("RuleID=%q want lockdown", d.RuleID)
	}
	// EnvelopeID is still stamped for audit-log joins.
	if d.EnvelopeID != env.ID {
		t.Fatalf("lockdown row should carry EnvelopeID for analytics")
	}
	st, _ := env.Inspect()
	if st.SpentCalls != 0 {
		t.Fatalf("lockdown must not debit envelope; SpentCalls=%d", st.SpentCalls)
	}
}
