package gov

import (
	"strings"
	"testing"
)

func TestActionType_ClosedEnum(t *testing.T) {
	// Spot-check a few expected types exist as constants.
	wantPresent := []ActionType{
		ActShellExec, ActFileRead, ActFileWrite, ActFileDelete,
		ActGitPush, ActGitForcePush, ActGitCommit, ActGitCheckout,
		ActGitWorktreeAdd, ActGithubPRCreate, ActGithubIssueView,
		ActDelegateTask, ActHTTPRequest, ActUnknown,
	}
	for _, a := range wantPresent {
		if a == "" {
			t.Errorf("ActionType constant is empty string — did you forget to assign?")
		}
	}
}

func TestAction_Fingerprint_Deterministic(t *testing.T) {
	a := Action{Type: ActShellExec, Target: "rm -rf go/", Path: "/tmp"}
	fp1 := a.Fingerprint()
	fp2 := a.Fingerprint()
	if fp1 != fp2 {
		t.Fatalf("Fingerprint not deterministic: %q vs %q", fp1, fp2)
	}
	if len(fp1) != 64 {
		t.Fatalf("Fingerprint should be 64 hex chars (sha256), got %d", len(fp1))
	}
}

func TestAction_Fingerprint_SamePatternSameFP(t *testing.T) {
	// Path should NOT affect the fingerprint — rm -rf across different
	// dirs shares a fingerprint for escalation-counting purposes.
	a := Action{Type: ActShellExec, Target: "rm -rf go/", Path: "/a"}
	b := Action{Type: ActShellExec, Target: "rm -rf go/", Path: "/b"}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatalf("Fingerprint should ignore Path, got different for %+v vs %+v", a, b)
	}
}

func TestAction_Fingerprint_DifferentTypeDifferentFP(t *testing.T) {
	a := Action{Type: ActShellExec, Target: "ls"}
	b := Action{Type: ActFileRead, Target: "ls"}
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatalf("Fingerprint collision across different types")
	}
}

func TestAction_String_Debuggable(t *testing.T) {
	s := Action{Type: ActShellExec, Target: "foo", Path: "/tmp"}.String()
	if !strings.Contains(s, "shell.exec") || !strings.Contains(s, "foo") {
		t.Errorf("Action.String should contain Type and Target, got %q", s)
	}
}
