package gov

import (
	"os"
	"path/filepath"
	"testing"
)

// newGateWithRule builds a Gate whose policy contains exactly one
// deny rule with the given id + action target. Tests use this to
// focus on the operator-authorized bypass behavior without depending
// on the broader newTestGate fixture (which loads a multi-rule policy
// and needs OpenCounter setup the other tests share).
func newGateWithRule(t *testing.T, ruleID, actionType, target string) (*Gate, string) {
	t.Helper()
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "chitin.yaml")
	yaml := `
id: bypass-test
mode: enforce
rules:
  - id: ` + ruleID + `
    action: ` + actionType + `
    effect: deny
    target: "` + target + `"
    reason: "test deny for bypass"
`
	_ = os.WriteFile(policyPath, []byte(yaml), 0o644)
	pol, _, err := LoadWithInheritanceWithOptions(dir, PolicyLoadOptions{BypassSignature: true})
	if err != nil {
		t.Fatalf("LoadWithInheritance: %v", err)
	}
	logDir := filepath.Join(dir, "decisions")
	return &Gate{Policy: pol, LogDir: logDir, Cwd: dir, NoRecord: true}, dir
}

// Invariant: a deny by no-governance-self-modification is bypassed
// when CHITIN_GOV_OPERATOR_AUTHORIZED=1 is set in env.
func TestGate_OperatorAuthorizedBypass_GovSelfModFires(t *testing.T) {
	g, _ := newGateWithRule(t, "no-governance-self-modification",
		"file.write", "chitin.yaml")
	t.Setenv("CHITIN_GOV_OPERATOR_AUTHORIZED", "1")

	d := g.Evaluate(Action{Type: ActFileWrite, Target: "chitin.yaml"}, "red", nil)
	if !d.Allowed {
		t.Fatalf("expected operator-authorized bypass to allow, got %+v", d)
	}
	if d.RuleID != "operator-authorized-bypass" {
		t.Errorf("RuleID: got %q, want operator-authorized-bypass", d.RuleID)
	}
}

// Invariant: bypass does NOT fire when env var is unset.
func TestGate_OperatorAuthorizedBypass_DefaultDenies(t *testing.T) {
	g, _ := newGateWithRule(t, "no-governance-self-modification",
		"file.write", "chitin.yaml")
	os.Unsetenv("CHITIN_GOV_OPERATOR_AUTHORIZED")

	d := g.Evaluate(Action{Type: ActFileWrite, Target: "chitin.yaml"}, "red", nil)
	if d.Allowed {
		t.Fatalf("expected deny without env var, got %+v", d)
	}
	if d.RuleID != "no-governance-self-modification" {
		t.Errorf("RuleID: got %q, want no-governance-self-modification", d.RuleID)
	}
}

// Invariant: bypass does NOT fire for non-allowlisted rules even when
// env var is set. Protects against accidental scope creep — adding new
// rules to the bypass requires explicit edit to the bypassable map.
func TestGate_OperatorAuthorizedBypass_NonAllowlistedRuleStillDenies(t *testing.T) {
	g, _ := newGateWithRule(t, "no-rm-recursive", "file.recursive_delete", "")
	t.Setenv("CHITIN_GOV_OPERATOR_AUTHORIZED", "1")

	d := g.Evaluate(Action{Type: ActFileRecursiveDelete, Target: "/tmp/x"}, "red", nil)
	if d.Allowed {
		t.Fatalf("non-allowlisted rule should not be bypassable, got %+v", d)
	}
	if d.RuleID != "no-rm-recursive" {
		t.Errorf("RuleID: got %q, want no-rm-recursive (bypass should NOT have fired)", d.RuleID)
	}
}

// Invariant: bypass fires for no-force-push when env var is set.
func TestGate_OperatorAuthorizedBypass_ForcePush(t *testing.T) {
	g, _ := newGateWithRule(t, "no-force-push", "git.force-push", "")
	t.Setenv("CHITIN_GOV_OPERATOR_AUTHORIZED", "1")

	d := g.Evaluate(Action{Type: ActGitForcePush}, "red", nil)
	if !d.Allowed {
		t.Fatalf("expected bypass to allow force-push, got %+v", d)
	}
	if d.RuleID != "operator-authorized-bypass" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
}

// Invariant: bypass fires for no-commit-to-protected when env var is set.
func TestGate_OperatorAuthorizedBypass_CommitProtected(t *testing.T) {
	g, _ := newGateWithRule(t, "no-commit-to-protected", "git.commit", "")
	t.Setenv("CHITIN_GOV_OPERATOR_AUTHORIZED", "1")

	d := g.Evaluate(Action{Type: ActGitCommit}, "red", nil)
	if !d.Allowed {
		t.Fatalf("expected bypass to allow git.commit, got %+v", d)
	}
	if d.RuleID != "operator-authorized-bypass" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
}
