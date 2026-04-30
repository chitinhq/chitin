package gov

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestGate(t *testing.T) (*Gate, string) {
	t.Helper()
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "chitin.yaml")
	_ = os.WriteFile(policyPath, []byte(`
id: test
mode: guide
rules:
  - id: no-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "no rm"
    suggestion: "use git rm"
    correctedCommand: "git rm <files>"
  - id: allow-read
    action: file.read
    effect: allow
    reason: "reads ok"
`), 0o644)
	pol, _, err := LoadWithInheritance(dir)
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	logDir := filepath.Join(dir, "decisions")
	dbPath := filepath.Join(dir, "gov.db")
	counter, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("OpenCounter: %v", err)
	}
	t.Cleanup(func() { counter.Close() })
	return &Gate{Policy: pol, Counter: counter, LogDir: logDir, Cwd: dir}, dir
}

func TestGate_AllowsReadAction(t *testing.T) {
	g, _ := newTestGate(t)
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if !d.Allowed {
		t.Errorf("file.read should be allowed, got %+v", d)
	}
}

func TestGate_DeniesRmRfAndLogs(t *testing.T) {
	g, dir := newTestGate(t)
	d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)
	if d.Allowed {
		t.Errorf("rm -rf should be denied")
	}
	if d.RuleID != "no-rm" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
	if d.Escalation != "normal" {
		t.Errorf("Escalation: got %q want normal (1st denial)", d.Escalation)
	}

	// Log file should exist
	logDir := filepath.Join(dir, "decisions")
	entries, _ := os.ReadDir(logDir)
	if len(entries) != 1 {
		t.Errorf("expected 1 log file, got %d", len(entries))
	}
}

func TestGate_EscalationRecorded(t *testing.T) {
	g, _ := newTestGate(t)
	for i := 0; i < 3; i++ {
		g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)
	}
	if lv := g.Counter.Level("agent1"); lv != "elevated" {
		t.Errorf("after 3 denials, level=%q want elevated", lv)
	}
}

func TestGate_LockdownDeniesEverything(t *testing.T) {
	g, _ := newTestGate(t)
	g.Counter.Lockdown("agent1")
	// file.read would normally be allowed — but lockdown overrides
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.Allowed {
		t.Errorf("agent in lockdown must be denied regardless of rule")
	}
	if d.RuleID != "lockdown" {
		t.Errorf("RuleID: got %q want lockdown", d.RuleID)
	}
}

func TestGate_MonitorModeAllowsButLogs(t *testing.T) {
	g, _ := newTestGate(t)
	// Override policy to monitor mode
	g.Policy.Mode = "monitor"
	g.Policy.InvariantModes = nil
	d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)
	if !d.Allowed {
		t.Errorf("monitor mode should allow (log-only), got denied: %+v", d)
	}
	if d.Mode != "monitor" {
		t.Errorf("Mode: got %q want monitor", d.Mode)
	}
}

// TestGate_OnDecisionFiresOnAllow / Deny / Lockdown verify the F4 addendum
// callback is invoked exactly once per Evaluate call, with the final
// Decision (post-bounds, post-monitor-override, post-stamp). Three cases
// cover the three exit paths through Evaluate.
func TestGate_OnDecisionFiresOnAllow(t *testing.T) {
	g, _ := newTestGate(t)
	var calls []Decision
	g.OnDecision = func(d *Decision) { calls = append(calls, *d) }

	g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)

	if len(calls) != 1 {
		t.Fatalf("OnDecision: got %d calls, want 1", len(calls))
	}
	if !calls[0].Allowed {
		t.Errorf("OnDecision callback received Decision.Allowed=false, want true")
	}
	if calls[0].Action.Type != ActFileRead {
		t.Errorf("OnDecision Action.Type: got %q want %q", calls[0].Action.Type, ActFileRead)
	}
}

func TestGate_OnDecisionFiresOnDeny(t *testing.T) {
	g, _ := newTestGate(t)
	var calls []Decision
	g.OnDecision = func(d *Decision) { calls = append(calls, *d) }

	g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)

	if len(calls) != 1 {
		t.Fatalf("OnDecision: got %d calls, want 1", len(calls))
	}
	if calls[0].Allowed {
		t.Errorf("OnDecision callback received Allowed=true, want false")
	}
	if calls[0].RuleID != "no-rm" {
		t.Errorf("OnDecision RuleID: got %q want no-rm", calls[0].RuleID)
	}
}

func TestGate_OnDecisionFiresOnLockdown(t *testing.T) {
	g, _ := newTestGate(t)
	g.Counter.Lockdown("agent1")

	var calls []Decision
	g.OnDecision = func(d *Decision) { calls = append(calls, *d) }

	g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)

	if len(calls) != 1 {
		t.Fatalf("OnDecision (lockdown path): got %d calls, want 1", len(calls))
	}
	if calls[0].RuleID != "lockdown" {
		t.Errorf("OnDecision lockdown RuleID: got %q want lockdown", calls[0].RuleID)
	}
}

// TestGate_OnDecisionNilIsSafe ensures pre-F4 behavior is preserved when
// the callback is not wired — Evaluate must not panic and the audit log
// path must still run.
func TestGate_OnDecisionNilIsSafe(t *testing.T) {
	g, _ := newTestGate(t)
	// g.OnDecision left nil
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if !d.Allowed {
		t.Errorf("Evaluate with nil OnDecision must still produce a Decision: %+v", d)
	}
}
