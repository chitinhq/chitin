package gov

import (
	"os"
	"path/filepath"
	"testing"
)

func loadBaseline(t *testing.T) Policy {
	t.Helper()
	p, err := LoadPolicyFile(filepath.Join("testdata", "policy-baseline.yaml"))
	if err != nil {
		t.Fatalf("LoadPolicyFile: %v", err)
	}
	return p
}

func TestPolicy_LoadBaseline(t *testing.T) {
	p := loadBaseline(t)
	if p.ID != "test-baseline" {
		t.Errorf("ID: got %q", p.ID)
	}
	if p.Mode != "guide" {
		t.Errorf("Mode: got %q", p.Mode)
	}
	if p.Bounds.MaxFilesChanged != 25 {
		t.Errorf("Bounds.MaxFilesChanged: got %d", p.Bounds.MaxFilesChanged)
	}
	if len(p.Rules) != 5 {
		t.Errorf("Rules count: got %d want 5", len(p.Rules))
	}
}

func TestPolicy_LoadMalformed(t *testing.T) {
	_, err := LoadPolicyFile(filepath.Join("testdata", "policy-malformed.yaml"))
	if err == nil {
		t.Fatal("LoadPolicyFile should fail on malformed YAML")
	}
}

func TestPolicy_Evaluate_DenyFirstWins(t *testing.T) {
	p := loadBaseline(t)
	a := Action{Type: ActShellExec, Target: "rm -rf go/"}
	d := p.Evaluate(a)
	if d.Allowed {
		t.Errorf("rm -rf should be denied")
	}
	if d.RuleID != "no-destructive-rm" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
	if d.Reason == "" {
		t.Errorf("Reason should be populated")
	}
	if d.Suggestion == "" {
		t.Errorf("Suggestion should be populated")
	}
	if d.CorrectedCommand == "" {
		t.Errorf("CorrectedCommand should be populated")
	}
}

func TestPolicy_Evaluate_BranchCondition(t *testing.T) {
	p := loadBaseline(t)
	// push to main — denied
	d := p.Evaluate(Action{Type: ActGitPush, Target: "main"})
	if d.Allowed {
		t.Errorf("push to main should be denied")
	}
	if d.RuleID != "no-protected-push" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
	// push to feature — allowed (falls through to default)
	d2 := p.Evaluate(Action{Type: ActGitPush, Target: "fix/42-something"})
	if !d2.Allowed {
		t.Errorf("push to feature branch should be allowed (default), got rule=%q reason=%q", d2.RuleID, d2.Reason)
	}
}

func TestPolicy_Evaluate_AllowMatch(t *testing.T) {
	p := loadBaseline(t)
	d := p.Evaluate(Action{Type: ActFileRead, Target: "anything"})
	if !d.Allowed {
		t.Errorf("file.read should match allow-reads, got %+v", d)
	}
	if d.RuleID != "allow-reads" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
}

func TestPolicy_Evaluate_DefaultDeny(t *testing.T) {
	// No rule matches ActUnknown by default → fail-closed deny.
	p := loadBaseline(t)
	d := p.Evaluate(Action{Type: ActUnknown, Target: "weird_tool"})
	if d.Allowed {
		t.Errorf("unknown action should default-deny, got %+v", d)
	}
	if d.RuleID != "default-deny" {
		t.Errorf("RuleID: got %q want default-deny", d.RuleID)
	}
}

func TestPolicy_ModeDefault(t *testing.T) {
	// A policy with no explicit Mode should default to "guide".
	p := Policy{ID: "test", Rules: []Rule{}}
	p.ApplyDefaults()
	if p.Mode != "guide" {
		t.Errorf("default Mode: got %q want guide", p.Mode)
	}
}

func TestPolicy_Evaluate_InvariantModeOverride(t *testing.T) {
	p := Policy{
		ID:             "t",
		Mode:           "guide",
		InvariantModes: map[string]string{"no-env-write": "enforce"},
		Rules: []Rule{{
			ID: "no-env-write", Action: ActionMatcher{"file.write"}, Effect: "deny",
			Target: ".env", Reason: "secrets",
		}},
	}
	_ = p.ApplyDefaults()
	d := p.Evaluate(Action{Type: ActFileWrite, Target: ".env"})
	if d.Mode != "enforce" {
		t.Errorf("InvariantMode override not applied: got %q want enforce", d.Mode)
	}
}

func TestPolicy_Evaluate_DenyWinsOverEarlierAllow(t *testing.T) {
	// Regression: deny-first semantics must be rule-order-independent.
	// Here the allow rule appears BEFORE the deny — if we did single-pass
	// first-match-wins (the pre-fix behavior), the allow would incorrectly
	// permit rm -rf.
	p := Policy{
		Mode: "guide",
		Rules: []Rule{
			{ID: "allow-shell", Action: ActionMatcher{"shell.exec"}, Effect: "allow",
				Reason: "generic shell"},
			{ID: "deny-rm", Action: ActionMatcher{"shell.exec"}, Effect: "deny",
				Target: "rm -rf", Reason: "dangerous"},
		},
	}
	if err := p.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	d := p.Evaluate(Action{Type: ActShellExec, Target: "rm -rf /tmp/x"})
	if d.Allowed {
		t.Errorf("deny must win over earlier allow, got %+v", d)
	}
	if d.RuleID != "deny-rm" {
		t.Errorf("RuleID: got %q want deny-rm", d.RuleID)
	}
}

func TestPolicy_RegexCompiledAtLoad(t *testing.T) {
	// Invalid target_regex must fail at LoadPolicyFile, not silently at eval.
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badPath, []byte(`
id: bad
mode: enforce
rules:
  - id: r
    action: shell.exec
    effect: deny
    target_regex: "("
    reason: "bad regex"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPolicyFile(badPath)
	if err == nil {
		t.Fatal("LoadPolicyFile must reject invalid regex at load time")
	}
}

func TestMonotonicStrictness_UnknownMode(t *testing.T) {
	// Unknown mode strings are explicit errors — don't silently default to monitor.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "chitin.yaml"), `
id: typo
mode: guardian
rules: []
`)
	_, _, err := LoadWithInheritance(root)
	if err == nil {
		t.Fatal("LoadWithInheritance must reject unknown mode")
	}
}
