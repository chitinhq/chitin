// Package tier classifies an Action by cost tier. The result is metadata
// stamped onto Decision rows for downstream session-spawn-routing tools
// — chitin itself does NOT execute differently based on tier. See the
// spec §"Why permission-gate, not tool harness".
//
// Rules live in code, not YAML, because they are kernel-level
// invariants: a YAML author should not be able to override "git.commit
// must classify T2" by setting tier_hint=T0. Future ecosystem-phase
// work may layer YAML hints on top, but the floor is in code.
package tier

import (
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// Route returns the Tier for the given Action. Pure function.
//
// T0Local rules:
//   - file.read
//   - git.{diff,log,status,worktree.list}
//   - github.{pr,issue}.{view,list}
//   - http.request (allowlist deferred — currently all http.request → T0;
//     ecosystem-phase work will narrow to a host allowlist)
//
// Default for everything else is T2Expensive. shell.exec splits via
// Action.Params["sub_action"]: "shell.cat" → T0, others stay T2.
func Route(action gov.Action) gov.Tier {
	switch action.Type {
	case gov.ActFileRead,
		gov.ActGitDiff,
		gov.ActGitLog,
		gov.ActGitStatus,
		gov.ActGitWorktreeList,
		gov.ActGithubPRView,
		gov.ActGithubPRList,
		gov.ActGithubIssueView,
		gov.ActGithubIssueList,
		gov.ActHTTPRequest:
		return gov.T0Local
	case gov.ActShellExec:
		if sub, _ := action.Params["sub_action"].(string); sub == "shell.cat" {
			return gov.T0Local
		}
		return gov.T2Expensive
	}
	return gov.T2Expensive
}
