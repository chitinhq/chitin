package gov

import (
	"os"
	"path/filepath"
	"strings"
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

// Invariant: bypass fires for no-protected-push when env var is set
// (Ares review msg 5252: companion to no-commit-to-protected — same
// false-positive on detached-HEAD worktree state but for git push).
func TestGate_OperatorAuthorizedBypass_ProtectedPush(t *testing.T) {
	g, _ := newGateWithRule(t, "no-protected-push", "git.push", "")
	t.Setenv("CHITIN_GOV_OPERATOR_AUTHORIZED", "1")

	d := g.Evaluate(Action{Type: ActGitPush}, "red", nil)
	if !d.Allowed {
		t.Fatalf("expected bypass to allow git.push, got %+v", d)
	}
	if d.RuleID != "operator-authorized-bypass" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
}

// Invariant: bypass fires for no-gov-self-mod-via-shell when env set
// (Ares review msg 5252: shell-vector companion to the file.write
// self-mod rule — must bypass consistently or operator-edit-via-sed
// stays blocked).
func TestGate_OperatorAuthorizedBypass_GovSelfModViaShell(t *testing.T) {
	g, _ := newGateWithRule(t, "no-gov-self-mod-via-shell",
		"shell.exec", "chitin.yaml")
	t.Setenv("CHITIN_GOV_OPERATOR_AUTHORIZED", "1")

	d := g.Evaluate(Action{Type: ActShellExec, Target: "sed -i 's/x/y/' chitin.yaml"}, "red", nil)
	if !d.Allowed {
		t.Fatalf("expected bypass to allow shell self-mod, got %+v", d)
	}
	if d.RuleID != "operator-authorized-bypass" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
}

// Invariant: Reason string explicitly states the bypass-window scope
// (operator must unset env var to close; sub-process inheritance is
// caller's responsibility). Catches the Ares P3 wording fix from
// review msg 5252.
func TestGate_OperatorAuthorizedBypass_ReasonStatesWindowScope(t *testing.T) {
	g, _ := newGateWithRule(t, "no-governance-self-modification",
		"file.write", "chitin.yaml")
	t.Setenv("CHITIN_GOV_OPERATOR_AUTHORIZED", "1")

	d := g.Evaluate(Action{Type: ActFileWrite, Target: "chitin.yaml"}, "red", nil)
	if !d.Allowed {
		t.Fatalf("expected bypass to allow, got %+v", d)
	}
	if !strings.Contains(d.Reason, "unset the env var") {
		t.Errorf("Reason missing 'unset the env var' guidance: %q", d.Reason)
	}
	if !strings.Contains(d.Reason, "Sub-process") || !strings.Contains(d.Reason, "scrub") {
		t.Errorf("Reason missing sub-process scrub warning: %q", d.Reason)
	}
}
