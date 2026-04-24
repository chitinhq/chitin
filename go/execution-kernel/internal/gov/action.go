// Package gov implements Chitin's tool-boundary governance: canonical
// action vocabulary, policy evaluation, blast-radius bounds, and an
// escalation counter. See:
//   docs/superpowers/specs/2026-04-22-chitin-governance-v1-design.md
package gov

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ActionType is the canonical vocabulary of things an agent can propose.
// Closed enum: Normalize produces ActUnknown for anything not on this list.
type ActionType string

const (
	ActShellExec         ActionType = "shell.exec"
	ActFileRead          ActionType = "file.read"
	ActFileWrite         ActionType = "file.write"
	ActFileDelete        ActionType = "file.delete"
	ActFileMove          ActionType = "file.move"
	ActGitDiff           ActionType = "git.diff"
	ActGitLog            ActionType = "git.log"
	ActGitStatus         ActionType = "git.status"
	ActGitCommit         ActionType = "git.commit"
	ActGitCheckout       ActionType = "git.checkout"
	ActGitBranchCreate   ActionType = "git.branch.create"
	ActGitBranchDelete   ActionType = "git.branch.delete"
	ActGitMerge          ActionType = "git.merge"
	ActGitPush           ActionType = "git.push"
	ActGitForcePush      ActionType = "git.force-push"
	ActGitWorktreeList   ActionType = "git.worktree.list"
	ActGitWorktreeAdd    ActionType = "git.worktree.add"
	ActGitWorktreeRemove ActionType = "git.worktree.remove"
	ActGithubPRCreate    ActionType = "github.pr.create"
	ActGithubPRView      ActionType = "github.pr.view"
	ActGithubPRList      ActionType = "github.pr.list"
	ActGithubPRMerge     ActionType = "github.pr.merge"
	ActGithubPRClose     ActionType = "github.pr.close"
	ActGithubIssueList   ActionType = "github.issue.list"
	ActGithubIssueView   ActionType = "github.issue.view"
	ActGithubIssueCreate ActionType = "github.issue.create"
	ActGithubIssueClose  ActionType = "github.issue.close"
	ActGithubAPI         ActionType = "github.api"
	ActDelegateTask      ActionType = "delegate.task"
	ActHTTPRequest       ActionType = "http.request"
	ActNPMInstall        ActionType = "npm.install"
	ActNPMRun            ActionType = "npm.script.run"
	ActTestRun           ActionType = "test.run"
	ActMCPCall           ActionType = "mcp.call"
	ActInfraDestroy      ActionType = "infra.destroy"
	ActUnknown           ActionType = "unknown"
)

// Action is a normalized tool call — the unit of policy evaluation.
// Path is the cwd the action would execute against; not part of the
// fingerprint (pattern-based counting, not per-path).
type Action struct {
	Type   ActionType
	Target string
	Path   string
	Params map[string]any
}

// Fingerprint returns a stable SHA256 hex digest of (Type, Target).
// Path is excluded intentionally — rm -rf across different targets
// shares a fingerprint because the pattern is the anomaly.
func (a Action) Fingerprint() string {
	h := sha256.Sum256([]byte(string(a.Type) + "\x00" + a.Target))
	return hex.EncodeToString(h[:])
}

// String returns a debuggable one-line representation.
func (a Action) String() string {
	return fmt.Sprintf("Action{%s target=%q path=%q}", a.Type, a.Target, a.Path)
}
