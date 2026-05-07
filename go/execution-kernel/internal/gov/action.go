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

// Effect constants — match the YAML `effect:` field values.
type Effect string

const (
	EffectAllow   Effect = "allow"
	EffectDeny    Effect = "deny"
	EffectGuide   Effect = "guide"
	EffectMonitor Effect = "monitor"
	// EffectEscalate pauses the agent's tool call and asks the operator
	// to approve via the configured channel. Resolution comes through
	// the pending_approvals sqlite table; the gate's Wait helper polls
	// for it. Spec: docs/superpowers/specs/2026-05-07-operator-approval-escalation-design.md
	EffectEscalate Effect = "escalate"
)

// EscalateConfig is the per-rule configuration for effect:escalate.
// Distinct from the existing gov.EscalationConfig (which configures
// the severity-tier ladder for the lockdown counter). All fields
// have sensible defaults applied at policy parse time; only non-
// default values need to be set in chitin.yaml.
type EscalateConfig struct {
	// Channel: "hermes" (notify via hermes-gateway) | "cli-only" (no notify, queue for `chitin-kernel approve`)
	Channel string
	// TimeoutSeconds: deny resolution if no operator response within this window. Range [30, 86400].
	TimeoutSeconds int
	// RememberWindowSeconds: on approve, grant subsequent (rule_id, agent) calls for this window. 0 = single-call only.
	RememberWindowSeconds int
	// NotifyTemplate: optional Go text/template for the notification body. Empty = use built-in default.
	NotifyTemplate string
}
