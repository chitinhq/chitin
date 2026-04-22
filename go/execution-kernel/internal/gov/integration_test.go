package gov

import (
	"os"
	"path/filepath"
	"testing"
)

func newIntegrationGate(t *testing.T, policyYAML string) (*Gate, string) {
	t.Helper()
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "chitin.yaml"), []byte(policyYAML), 0o644)
	pol, _, err := LoadWithInheritance(dir)
	if err != nil {
		t.Fatalf("LoadWithInheritance: %v", err)
	}
	counter, err := OpenCounter(filepath.Join(dir, "gov.db"))
	if err != nil {
		t.Fatalf("OpenCounter: %v", err)
	}
	t.Cleanup(func() { counter.Close() })
	return &Gate{
		Policy: pol, Counter: counter,
		LogDir: filepath.Join(dir, "decisions"), Cwd: dir,
	}, dir
}

// Flow A from spec §Data-flow: terminal rm -rf is denied.
func TestIntegration_FlowA_DangerousShell(t *testing.T) {
	g, _ := newIntegrationGate(t, `
id: test
mode: guide
rules:
  - id: no-destructive-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "blocked"
    suggestion: "use targeted"
    correctedCommand: "git rm"
`)
	a, _ := Normalize("terminal", map[string]any{"command": "rm -rf go/"})
	d := g.Evaluate(a, "hermes")
	if d.Allowed {
		t.Fatalf("expected deny, got %+v", d)
	}
	if d.RuleID != "no-destructive-rm" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
}

// Flow B: execute_code subprocess.run bypass produces the same denial.
func TestIntegration_FlowB_BypassClosure(t *testing.T) {
	g, _ := newIntegrationGate(t, `
id: test
mode: guide
rules:
  - id: no-destructive-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "blocked"
`)
	// Via terminal
	aTerm, _ := Normalize("terminal", map[string]any{"command": "rm -rf go/"})
	dTerm := g.Evaluate(aTerm, "hermes")

	// Via execute_code subprocess
	aExec, _ := Normalize("execute_code", map[string]any{
		"code": `import subprocess
subprocess.run(["rm", "-rf", "go/"])`,
	})
	dExec := g.Evaluate(aExec, "hermes")

	if dTerm.Allowed || dExec.Allowed {
		t.Fatalf("both should be denied; got term=%+v exec=%+v", dTerm, dExec)
	}
	if dTerm.RuleID != dExec.RuleID {
		t.Errorf("bypass closure failed: terminal denied by %q, execute_code by %q",
			dTerm.RuleID, dExec.RuleID)
	}
	if aTerm.Fingerprint() != aExec.Fingerprint() {
		t.Errorf("fingerprints differ — escalation counter won't link them")
	}
}

// Flow E: escalation counter increments, reaches lockdown.
func TestIntegration_FlowE_EscalationLadder(t *testing.T) {
	g, _ := newIntegrationGate(t, `
id: test
mode: guide
rules:
  - id: no-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "blocked"
`)
	a, _ := Normalize("terminal", map[string]any{"command": "rm -rf go/"})
	for i := 0; i < 10; i++ {
		_ = g.Evaluate(a, "hermes")
	}
	if lv := g.Counter.Level("hermes"); lv != "lockdown" {
		t.Fatalf("after 10 denials, level=%q want lockdown", lv)
	}
	// Once in lockdown, even allowed actions are denied.
	okA, _ := Normalize("read_file", map[string]any{"path": "README.md"})
	dOK := g.Evaluate(okA, "hermes")
	if dOK.Allowed {
		t.Errorf("locked agent should be denied even for allow-shaped actions")
	}
	if dOK.RuleID != "lockdown" {
		t.Errorf("RuleID: got %q want lockdown", dOK.RuleID)
	}
}

func TestIntegration_NoPolicyFails(t *testing.T) {
	dir := t.TempDir()
	// no chitin.yaml anywhere
	_, _, err := LoadWithInheritance(dir)
	if err == nil {
		t.Fatal("expected no_policy_found error")
	}
}
