package gov

import (
	"testing"
)

func TestLoadPolicy_Tier0Flash(t *testing.T) {
	p, err := LoadPolicyFile("testdata/tier-0-flash.yaml")
	if err != nil {
		t.Fatalf("LoadPolicyFile tier-0-flash: %v", err)
	}
	if p.Mode != "guide" {
		t.Errorf("Mode: got %q, want guide", p.Mode)
	}
	if p.ID != "tier-0-flash" {
		t.Errorf("ID: got %q", p.ID)
	}
}

func TestLoadPolicy_Tier2Heavy(t *testing.T) {
	p, err := LoadPolicyFile("testdata/tier-2-heavy.yaml")
	if err != nil {
		t.Fatalf("LoadPolicyFile tier-2-heavy: %v", err)
	}
	if p.Mode != "enforce" {
		t.Errorf("Mode: got %q, want enforce", p.Mode)
	}
	if p.ID != "tier-2-heavy" {
		t.Errorf("ID: got %q", p.ID)
	}
}

func TestLoadPolicy_Tier4Autonomous(t *testing.T) {
	p, err := LoadPolicyFile("testdata/tier-4-autonomous.yaml")
	if err != nil {
		t.Fatalf("LoadPolicyFile tier-4-autonomous: %v", err)
	}
	if p.Mode != "enforce" {
		t.Errorf("Mode: got %q, want enforce", p.Mode)
	}
	if p.ID != "tier-4-autonomous" {
		t.Errorf("ID: got %q", p.ID)
	}
}

func TestEvaluate_Tier0Flash_AllowsShellExec(t *testing.T) {
	p, _ := LoadPolicyFile("testdata/tier-0-flash.yaml")
	d := p.Evaluate(Action{Type: ActShellExec, Target: "ls -la"})
	if !d.Allowed {
		t.Errorf("flash policy should allow shell.exec ls: got denied (rule=%s, reason=%s)", d.RuleID, d.Reason)
	}
}

func TestEvaluate_Tier0Flash_BlocksRecursiveDelete(t *testing.T) {
	p, _ := LoadPolicyFile("testdata/tier-0-flash.yaml")
	d := p.Evaluate(Action{Type: ActFileRecursiveDelete, Target: "rm -rf /"})
	if d.Allowed {
		t.Errorf("flash policy should deny recursive delete: got allowed")
	}
	if d.RuleID != "no-recursive-delete" {
		t.Errorf("rule: got %q, want no-recursive-delete", d.RuleID)
	}
}

func TestEvaluate_Tier2Heavy_BlocksForcePush(t *testing.T) {
	p, _ := LoadPolicyFile("testdata/tier-2-heavy.yaml")
	d := p.Evaluate(Action{Type: ActGitForcePush, Target: "git push --force origin main"})
	if d.Allowed {
		t.Errorf("heavy policy should deny force push: got allowed")
	}
}

func TestEvaluate_Tier2Heavy_BlocksProtectedPush(t *testing.T) {
	p, _ := LoadPolicyFile("testdata/tier-2-heavy.yaml")
	d := p.Evaluate(Action{Type: ActGitPush, Target: "main"})
	if d.Allowed {
		t.Errorf("heavy policy should deny push to main: got allowed")
	}
}

func TestEvaluate_Tier2Heavy_AllowsFeaturePush(t *testing.T) {
	p, _ := LoadPolicyFile("testdata/tier-2-heavy.yaml")
	d := p.Evaluate(Action{Type: ActGitPush, Target: "feature/my-branch"})
	if !d.Allowed {
		t.Errorf("heavy policy should allow push to feature branch: got denied (rule=%s)", d.RuleID)
	}
}

func TestEvaluate_Tier4_BlocksSelfModViaRedirect(t *testing.T) {
	p, _ := LoadPolicyFile("testdata/tier-4-autonomous.yaml")
	d := p.Evaluate(Action{Type: ActShellExec, Target: "echo '{}' > .openclaw/openclaw.json"})
	if d.Allowed {
		t.Errorf("autonomous policy should deny self-mod via redirect: got allowed")
	}
}

func TestEvaluate_Tier4_DefaultDeny(t *testing.T) {
	p, _ := LoadPolicyFile("testdata/tier-4-autonomous.yaml")
	// An action with no matching rule should be denied (default-deny)
	d := p.Evaluate(Action{Type: "some.unknown.action", Target: "whatever"})
	if d.Allowed {
		t.Errorf("autonomous policy should default-deny unknown actions: got allowed")
	}
}