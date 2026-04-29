package tier

import (
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestRoute_T0Actions(t *testing.T) {
	cases := []gov.ActionType{
		gov.ActFileRead,
		gov.ActGitDiff,
		gov.ActGitLog,
		gov.ActGitStatus,
		gov.ActGitWorktreeList,
		gov.ActGithubPRView,
		gov.ActGithubPRList,
		gov.ActGithubIssueView,
		gov.ActGithubIssueList,
		gov.ActHTTPRequest,
	}
	for _, at := range cases {
		got := Route(gov.Action{Type: at})
		if got != gov.T0Local {
			t.Errorf("Route(%s) = %s, want T0", at, got)
		}
	}
}

func TestRoute_T2Actions(t *testing.T) {
	cases := []gov.ActionType{
		gov.ActFileWrite,
		gov.ActFileDelete,
		gov.ActGitCommit,
		gov.ActGitPush,
		gov.ActGithubPRCreate,
		gov.ActDelegateTask,
		gov.ActInfraDestroy,
		gov.ActUnknown,
	}
	for _, at := range cases {
		got := Route(gov.Action{Type: at})
		if got != gov.T2Expensive {
			t.Errorf("Route(%s) = %s, want T2", at, got)
		}
	}
}

func TestRoute_ShellExecCatIsT0(t *testing.T) {
	got := Route(gov.Action{
		Type:   gov.ActShellExec,
		Params: map[string]any{"sub_action": "shell.cat"},
	})
	if got != gov.T0Local {
		t.Fatalf("shell.cat = %s, want T0", got)
	}
}

func TestRoute_ShellExecDefaultIsT2(t *testing.T) {
	got := Route(gov.Action{Type: gov.ActShellExec})
	if got != gov.T2Expensive {
		t.Fatalf("plain shell.exec = %s, want T2", got)
	}
}
