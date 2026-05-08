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
	// ActFileRecursiveDelete: shell `rm` invocation with a recursive
	// flag (any spelling/whitespace/quoting variant — closes #58).
	// Re-tagged from shell.exec so the no-rm-recursive rule fires
	// regardless of how the operator (or model) spells the command.
	ActFileRecursiveDelete ActionType = "file.recursive_delete"
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
	// ActKanbanCall: Hermes Agent's per-tool kanban API calls
	// (`kanban_show`, `kanban_complete`, `kanban_block`, `kanban_comment`,
	// `kanban_heartbeat`, `kanban_create`, `kanban_link`, `kanban_unlink`,
	// `kanban_archive`, `kanban_assign`). These are runtime plumbing —
	// the worker reading/writing its own card lifecycle — but they ARE
	// tool calls per chitin's universal-interception rule, so they get
	// a canonical action_type instead of falling through to ActUnknown.
	// Without this, every long-running hermes worker accumulates 10+
	// `default-deny-unknown` denials on plumbing alone and trips
	// lockdown (root cause of the 2026-05-07 chitin-worker smoke
	// stalling at deny-everything; profile renamed from chitin-runner
	// to chitin-worker the same day to disambiguate from the deleted
	// TypeScript orchestration runner).
	ActKanbanCall        ActionType = "kanban.call"
	// ActHermesProcess: Hermes Agent's `process` tool — a runtime helper
	// for managing background processes inside the agent's own session.
	// Like ActKanbanCall, plumbing-shaped but classified explicitly so
	// it doesn't accumulate as default-deny-unknown.
	ActHermesProcess     ActionType = "hermes.process"
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

// Effect type and EscalateConfig were removed in cull Phase 3 (2026-05-08).
// The escalate effect from PR #380 reinvented hermes' built-in approval
// system (tools/approval.py). Rules now use plain string literals
// "allow" | "deny" | "guide" | "monitor" for Rule.Effect; "escalate"
// is rejected at policy parse time so a stale chitin.yaml fails loud
// instead of silently mis-routing.
