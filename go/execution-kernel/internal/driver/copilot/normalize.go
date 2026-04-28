package copilot

import (
	"fmt"
	"os/exec"
	"strings"

	copilotsdk "github.com/github/copilot-sdk/go"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// Normalize translates a Copilot SDK PermissionRequest into chitin's
// canonical gov.Action. Returns Action{Type: ActUnknown} for unrecognized
// Kinds so that the policy default-deny catches them (fail-closed).
//
// Invariant: for every Kind k in the SDK's documented 8-value enum,
// Normalize(PermissionRequest{Kind: k}, cwd).Type is a non-empty string
// and .Path == cwd.
//
// The cwd parameter is the chitin-kernel working directory, set on every
// Action for policy-path scoping (LoadWithInheritance uses this).
func Normalize(req copilotsdk.PermissionRequest, cwd string) gov.Action {
	action := gov.Action{
		Path:   cwd,
		Params: map[string]any{},
	}

	switch req.Kind {
	case copilotsdk.PermissionRequestKindShell:
		// Route through gov.Normalize so shell commands receive the full
		// re-tagging treatment: git.force-push detection, infra.destroy
		// detection for terraform destroy / kubectl delete ns, and
		// curl-pipe-bash shape annotation.
		//
		// Invariant: for any shell command that gov.classifyShellCommand
		// maps to a more specific type, this path produces that type —
		// not the generic shell.exec default.
		cmd := ""
		if req.FullCommandText != nil {
			cmd = *req.FullCommandText
		}
		govAction, err := gov.Normalize("terminal", map[string]any{"command": cmd})
		if err != nil {
			// Fallback: at least set shell.exec with the raw target so the
			// policy default-allow-shell can match rather than panicking.
			action.Type = gov.ActShellExec
			action.Target = cmd
		} else {
			// Use gov's fully-tagged Action. Preserve Path (gov.Normalize
			// does not know cwd) and merge in any Params gov produced.
			action.Type = govAction.Type
			action.Target = govAction.Target
			for k, v := range govAction.Params {
				action.Params[k] = v
			}
		}

		// Bare `git push` (or `git push origin` without branch arg) leaves
		// Target empty because the gov-side parser is pure and can't run git.
		// Resolve the current branch from cwd here so policy rules with
		// explicit `branches:` lists (e.g. no-protected-push) can match.
		// Closes #62 — without this, `git push` on an upstream-tracking main
		// branch would fall through every protected-branch rule.
		if action.Type == gov.ActGitPush && action.Target == "" && cwd != "" {
			if branch := resolveCurrentBranch(cwd); branch != "" {
				action.Target = branch
			}
		}

	case copilotsdk.PermissionRequestKindWrite:
		action.Type = gov.ActFileWrite
		// FileName is the SDK field for the path being written.
		// Falls back to empty string if nil (nil-safe, no panic).
		if req.FileName != nil {
			action.Target = *req.FileName
		}

	case copilotsdk.PermissionRequestKindRead:
		action.Type = gov.ActFileRead
		if req.Path != nil {
			action.Target = *req.Path
		}

	case copilotsdk.PermissionRequestKindMcp:
		action.Type = gov.ActMCPCall
		// Compose "serverName/toolName" as a stable composite identifier.
		// Either or both may be nil; deref only when non-nil.
		server := ""
		tool := ""
		if req.ServerName != nil {
			server = *req.ServerName
		}
		if req.ToolName != nil {
			tool = *req.ToolName
		}
		if server != "" || tool != "" {
			action.Target = fmt.Sprintf("%s/%s", server, tool)
		}

	case copilotsdk.PermissionRequestKindURL:
		action.Type = gov.ActHTTPRequest
		if req.URL != nil {
			action.Target = *req.URL
		}

	case copilotsdk.PermissionRequestKindMemory:
		action.Type = "memory.access"
		if req.Subject != nil {
			action.Target = *req.Subject
		}

	case copilotsdk.PermissionRequestKindCustomTool:
		action.Type = "tool.custom"
		if req.ToolName != nil {
			action.Target = *req.ToolName
		}

	case copilotsdk.PermissionRequestKindHook:
		action.Type = "hook.invoke"
		if req.HookMessage != nil {
			action.Target = *req.HookMessage
		}

	default:
		// Fail-closed: unknown Kind maps to ActUnknown so the policy
		// default-deny rule catches it without special-casing.
		action.Type = gov.ActUnknown
	}

	return action
}

// resolveCurrentBranch returns the branch HEAD points at in cwd, or "" on
// detached HEAD, missing git, non-repo cwd, or any non-zero exit. The single
// side effect inside Normalize, fired only when Type=git.push AND Target=""
// — never on the happy path of explicit pushes.
func resolveCurrentBranch(cwd string) string {
	out, err := exec.Command("git", "-C", cwd, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
