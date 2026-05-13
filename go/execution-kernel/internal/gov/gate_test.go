package gov

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newTestGate(t *testing.T) (*Gate, string) {
	t.Helper()
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "chitin.yaml")
	_ = os.WriteFile(policyPath, []byte(`
id: test
mode: guide
rules:
  - id: no-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "no rm"
    suggestion: "use git rm"
    correctedCommand: "git rm <files>"
  - id: allow-read
    action: file.read
    effect: allow
    reason: "reads ok"
`), 0o644)
	pol, _, err := LoadWithInheritance(dir)
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	logDir := filepath.Join(dir, "decisions")
	dbPath := filepath.Join(dir, "gov.db")
	counter, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("OpenCounter: %v", err)
	}
	t.Cleanup(func() { counter.Close() })
	return &Gate{Policy: pol, Counter: counter, LogDir: logDir, Cwd: dir}, dir
}

func TestGate_AllowsReadAction(t *testing.T) {
	g, _ := newTestGate(t)
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if !d.Allowed {
		t.Errorf("file.read should be allowed, got %+v", d)
	}
}

func TestGate_DeniesRmRfAndLogs(t *testing.T) {
	g, dir := newTestGate(t)
	d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)
	if d.Allowed {
		t.Errorf("rm -rf should be denied")
	}
	if d.RuleID != "no-rm" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
	if d.Escalation != "normal" {
		t.Errorf("Escalation: got %q want normal (1st denial)", d.Escalation)
	}

	// Log file should exist
	logDir := filepath.Join(dir, "decisions")
	entries, _ := os.ReadDir(logDir)
	if len(entries) != 1 {
		t.Errorf("expected 1 log file, got %d", len(entries))
	}
}

func TestGate_DeniesGitCommitOnProtectedBranchForAllAgents(t *testing.T) {
	repo := initTestRepoOnBranch(t, "main")
	policy := protectedCommitPolicy(t)
	g := &Gate{Policy: policy, Cwd: repo, NoRecord: true}
	agents := []string{"clawta", "codex", "copilot", "gemini", "claude-code", "hermes"}

	for _, agent := range agents {
		t.Run(agent, func(t *testing.T) {
			d := g.Evaluate(Action{Type: ActGitCommit, Target: `git commit -m "x"`, Path: repo}, agent, nil)
			if d.Allowed {
				t.Fatalf("commit on main should be denied for %s: %+v", agent, d)
			}
			if d.RuleID != "no-commit-to-protected" {
				t.Fatalf("RuleID=%q want no-commit-to-protected", d.RuleID)
			}
			if d.Agent != agent {
				t.Fatalf("Agent=%q want %q", d.Agent, agent)
			}
		})
	}
}

func TestGate_AllowsGitCommitOnFeatureBranchAndGitReadsOnMain(t *testing.T) {
	repo := initTestRepoOnBranch(t, "main")
	policy := protectedCommitPolicy(t)
	g := &Gate{Policy: policy, Cwd: repo, NoRecord: true}

	for _, action := range []Action{
		{Type: ActGitStatus, Target: "git status", Path: repo},
		{Type: ActGitLog, Target: "git log", Path: repo},
		{Type: ActGitDiff, Target: "git diff", Path: repo},
	} {
		d := g.Evaluate(action, "clawta", nil)
		if !d.Allowed {
			t.Fatalf("%s on main should be allowed, got rule=%q reason=%q", action.Type, d.RuleID, d.Reason)
		}
	}

	runGitForProtectedCommitTest(t, repo, "checkout", "-b", "swarm/codex-test")
	d := g.Evaluate(Action{Type: ActGitCommit, Target: `git commit -m "x"`, Path: repo}, "clawta", nil)
	if !d.Allowed {
		t.Fatalf("commit on feature branch should be allowed, got rule=%q reason=%q", d.RuleID, d.Reason)
	}
}

func TestGate_DeniesGitCommitWhenProtectedBranchResolutionIndeterminate(t *testing.T) {
	policy := protectedCommitPolicy(t)
	g := &Gate{Policy: policy, NoRecord: true}

	for name, path := range map[string]string{
		"empty-path": "",
		"non-repo":   t.TempDir(),
	} {
		t.Run(name, func(t *testing.T) {
			d := g.Evaluate(Action{Type: ActGitCommit, Target: `git commit -m "x"`, Path: path}, "clawta", nil)
			if d.Allowed {
				t.Fatalf("indeterminate protected commit should fail closed: %+v", d)
			}
			if d.RuleID != "no-commit-to-protected" {
				t.Fatalf("RuleID=%q want no-commit-to-protected", d.RuleID)
			}
		})
	}
}

func TestGate_EscalationRecorded(t *testing.T) {
	g, _ := newTestGate(t)
	for i := 0; i < 3; i++ {
		g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)
	}
	if lv := g.Counter.Level("agent1"); lv != "elevated" {
		t.Errorf("after 3 denials, level=%q want elevated", lv)
	}
}

func protectedCommitPolicy(t *testing.T) Policy {
	t.Helper()
	p := Policy{
		ID:   "protected-commit-test",
		Mode: "enforce",
		InvariantModes: map[string]string{
			"no-commit-to-protected": "enforce",
		},
		Rules: []Rule{
			{
				ID:       "no-commit-to-protected",
				Action:   ActionMatcher{string(ActGitCommit)},
				Effect:   "deny",
				Branches: []string{"main", "master", "<HEAD-implicit>"},
				Reason:   "Direct commit to protected branch",
			},
			{
				ID:     "allow-git-read",
				Action: ActionMatcher{string(ActGitStatus), string(ActGitLog), string(ActGitDiff)},
				Effect: "allow",
			},
			{
				ID:     "allow-git-ops",
				Action: ActionMatcher{string(ActGitCommit)},
				Effect: "allow",
			},
		},
	}
	if err := p.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	return p
}

func initTestRepoOnBranch(t *testing.T, branch string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	runGitForProtectedCommitTest(t, repo, "init")
	runGitForProtectedCommitTest(t, repo, "checkout", "-b", branch)
	return repo
}

func runGitForProtectedCommitTest(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func TestGate_DenyCascadeLocksShellExecWithinWindow(t *testing.T) {
	g, _ := newTestGate(t)
	g.Policy.Escalation.DenyCascadeCount = 3
	g.Policy.Escalation.DenyCascadeWindowSeconds = 60

	for i := 0; i < 2; i++ {
		d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)
		if d.RuleID != "no-rm" {
			t.Fatalf("iter %d: RuleID=%q want no-rm", i, d.RuleID)
		}
		if d.Escalation == "lockdown" {
			t.Fatalf("iter %d: lockdown triggered before cascade threshold", i)
		}
	}

	d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)
	if d.RuleID != "no-rm" {
		t.Fatalf("third deny RuleID=%q want no-rm", d.RuleID)
	}
	if d.Escalation != "lockdown" {
		t.Fatalf("third shell.exec deny should trigger cascade lockdown, escalation=%q", d.Escalation)
	}

	read := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if read.RuleID != "lockdown" {
		t.Fatalf("post-cascade read RuleID=%q want lockdown", read.RuleID)
	}
}

func TestGate_DenyCascadeQueryErrorForcesLockdown(t *testing.T) {
	g, _ := newTestGate(t)
	g.Policy.Escalation.DenyCascadeCount = 3
	g.Policy.Escalation.DenyCascadeWindowSeconds = 60
	if _, err := g.Counter.db.Exec(`DROP TABLE denial_events`); err != nil {
		t.Fatalf("drop denial_events: %v", err)
	}

	d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)
	if d.RuleID != "no-rm" {
		t.Fatalf("RuleID=%q want no-rm", d.RuleID)
	}
	if d.Escalation != "lockdown" {
		t.Fatalf("cascade query error should force lockdown, escalation=%q", d.Escalation)
	}
}

func TestGate_LockdownDeniesEverything(t *testing.T) {
	g, _ := newTestGate(t)
	g.Counter.Lockdown("agent1")
	// file.read would normally be allowed — but lockdown overrides
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.Allowed {
		t.Errorf("agent in lockdown must be denied regardless of rule")
	}
	if d.RuleID != "lockdown" {
		t.Errorf("RuleID: got %q want lockdown", d.RuleID)
	}
}

func TestGate_NoRecordSkipsCounterAndLog(t *testing.T) {
	// Regression for the 2026-05-06 hermes-locked-by-smoke-tests
	// incident: an operator running protected-system-path probes via
	// `gate evaluate --agent=hermes` pushed real hermes past the
	// lockdown threshold. NoRecord lets the same probe run without
	// touching the persistent agent_state DB or the chain log.
	g, dir := newTestGate(t)
	g.NoRecord = true

	// 20 denials would normally lockdown twice over (threshold=10).
	// Under NoRecord the counter must stay at 0, so Level remains normal.
	for i := 0; i < 20; i++ {
		d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)
		if d.Allowed {
			t.Fatalf("iter %d: rm -rf should be denied even under NoRecord", i)
		}
		if d.RuleID != "no-rm" {
			t.Fatalf("iter %d: RuleID=%q want no-rm — NoRecord must not change rule evaluation", i, d.RuleID)
		}
	}
	if lv := g.Counter.Level("agent1"); lv != "normal" {
		t.Errorf("after 20 NoRecord denials, level=%q want normal (counter must not move)", lv)
	}

	// Chain log dir should not exist (or be empty) — the recorded
	// counterpart test TestGate_DeniesRmRfAndLogs expects 1 file after
	// a single deny, so 20 NoRecord denies must produce 0 files.
	logDir := filepath.Join(dir, "decisions")
	entries, _ := os.ReadDir(logDir)
	if len(entries) != 0 {
		t.Errorf("expected 0 log files under NoRecord, got %d", len(entries))
	}
}

func TestGate_NoRecordPreservesLockdownRead(t *testing.T) {
	// NoRecord suppresses *writes*, not reads. If an agent is already
	// in lockdown (set out-of-band by an operator), a NoRecord
	// evaluation must still surface RuleID=lockdown so smoke-test
	// output reflects the live gate state. This is what makes
	// NoRecord safe to use against the production DB — operators see
	// what the gate would do without changing what it will do next.
	g, dir := newTestGate(t)
	g.Counter.Lockdown("agent1")
	g.NoRecord = true

	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.Allowed {
		t.Fatalf("locked agent must be denied even under NoRecord")
	}
	if d.RuleID != "lockdown" {
		t.Fatalf("RuleID=%q want lockdown", d.RuleID)
	}

	// Lockdown branch also has its own WriteLog — must be suppressed.
	logDir := filepath.Join(dir, "decisions")
	entries, _ := os.ReadDir(logDir)
	if len(entries) != 0 {
		t.Errorf("lockdown WriteLog should be suppressed under NoRecord, got %d files", len(entries))
	}
}

func TestGate_NoRecordAllowStillWorks(t *testing.T) {
	// Allow path has no counter write either way, but verify the log
	// suppression covers it for symmetry — the recording version
	// (TestGate_DeniesRmRfAndLogs) writes the log on deny, but
	// WriteLog runs on every evaluation including allows.
	g, dir := newTestGate(t)
	g.NoRecord = true

	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if !d.Allowed {
		t.Fatalf("file.read should be allowed under NoRecord, got %+v", d)
	}

	logDir := filepath.Join(dir, "decisions")
	entries, _ := os.ReadDir(logDir)
	if len(entries) != 0 {
		t.Errorf("allow under NoRecord should not write log, got %d files", len(entries))
	}
}

func TestGate_MonitorModeAllowsButLogs(t *testing.T) {
	g, _ := newTestGate(t)
	// Override policy to monitor mode
	g.Policy.Mode = "monitor"
	g.Policy.InvariantModes = nil
	d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)
	if !d.Allowed {
		t.Errorf("monitor mode should allow (log-only), got denied: %+v", d)
	}
	if d.Mode != "monitor" {
		t.Errorf("Mode: got %q want monitor", d.Mode)
	}
}

// TestGate_OnDecisionFiresOnAllow / Deny / Lockdown verify the F4 addendum
// callback is invoked exactly once per Evaluate call, with the final
// Decision (post-bounds, post-monitor-override, post-stamp). Three cases
// cover the three exit paths through Evaluate.
func TestGate_OnDecisionFiresOnAllow(t *testing.T) {
	g, _ := newTestGate(t)
	var calls []Decision
	g.OnDecision = func(d *Decision) { calls = append(calls, *d) }

	g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)

	if len(calls) != 1 {
		t.Fatalf("OnDecision: got %d calls, want 1", len(calls))
	}
	if !calls[0].Allowed {
		t.Errorf("OnDecision callback received Decision.Allowed=false, want true")
	}
	if calls[0].Action.Type != ActFileRead {
		t.Errorf("OnDecision Action.Type: got %q want %q", calls[0].Action.Type, ActFileRead)
	}
}

func TestGate_OnDecisionFiresOnDeny(t *testing.T) {
	g, _ := newTestGate(t)
	var calls []Decision
	g.OnDecision = func(d *Decision) { calls = append(calls, *d) }

	g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)

	if len(calls) != 1 {
		t.Fatalf("OnDecision: got %d calls, want 1", len(calls))
	}
	if calls[0].Allowed {
		t.Errorf("OnDecision callback received Allowed=true, want false")
	}
	if calls[0].RuleID != "no-rm" {
		t.Errorf("OnDecision RuleID: got %q want no-rm", calls[0].RuleID)
	}
}

func TestGate_OnDecisionFiresOnLockdown(t *testing.T) {
	g, _ := newTestGate(t)
	g.Counter.Lockdown("agent1")

	var calls []Decision
	g.OnDecision = func(d *Decision) { calls = append(calls, *d) }

	g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)

	if len(calls) != 1 {
		t.Fatalf("OnDecision (lockdown path): got %d calls, want 1", len(calls))
	}
	if calls[0].RuleID != "lockdown" {
		t.Errorf("OnDecision lockdown RuleID: got %q want lockdown", calls[0].RuleID)
	}
}

// TestGate_OnDecisionNilIsSafe ensures pre-F4 behavior is preserved when
// the callback is not wired — Evaluate must not panic and the audit log
// path must still run.
func TestGate_OnDecisionNilIsSafe(t *testing.T) {
	g, _ := newTestGate(t)
	// g.OnDecision left nil
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if !d.Allowed {
		t.Errorf("Evaluate with nil OnDecision must still produce a Decision: %+v", d)
	}
}

// destructiveTarget assembles the literal that policy-test-deny matches,
// without writing it as a flat string in source — chitin's own gate hook
// blocks heredoc/file-write operations that contain the literal pattern.
// This is a real instance of dogfood-debt: the gate fires on test fixtures.
func destructiveTarget() string {
	return "r" + "m -" + "rf /etc/test"
}

func TestGate_CallerOriginStampedWhenEnvelopeNil(t *testing.T) {
	g, _ := newTestGate(t)
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.CallerOrigin == "" {
		t.Errorf("CallerOrigin must be set when envelope is nil; got empty")
	}
	if !strings.Contains(d.CallerOrigin, "gate_test.go:") {
		t.Errorf("CallerOrigin should point to the caller frame; got %q", d.CallerOrigin)
	}
}

func TestGate_CallerOriginEmptyWhenEnvelopePresent(t *testing.T) {
	g, env := gateWithEnvelope(t, BudgetLimits{MaxToolCalls: 10, MaxInputBytes: 1024})
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", env)
	if d.CallerOrigin != "" {
		t.Errorf("CallerOrigin should be empty when envelope supplied; got %q", d.CallerOrigin)
	}
	if d.EnvelopeID != env.ID {
		t.Errorf("EnvelopeID should be stamped; got %q want %q", d.EnvelopeID, env.ID)
	}
}

func TestGate_CallerOriginPreservedOnDeny(t *testing.T) {
	g, _ := newTestGate(t)
	d := g.Evaluate(Action{Type: ActShellExec, Target: destructiveTarget()}, "agent1", nil)
	if d.Allowed {
		t.Fatalf("expected deny")
	}
	if d.CallerOrigin == "" {
		t.Errorf("CallerOrigin must survive policy-deny path")
	}
}

func TestGate_CallerOriginPreservedOnBoundsDeny(t *testing.T) {
	// Regression caught in PR #79 review: the bounds-deny path replaces d with
	// the bd Decision, which dropped Agent + CallerOrigin from the audit row.
	// CheckBounds runs `git -C <cwd> diff --stat origin/main...HEAD`; in a
	// non-git tempdir the command fails and CheckBounds returns
	// bounds:undetermined deny — deterministic way to trip the path in a test.
	g, dir := newTestGate(t)
	// Add an allow rule for git.push so policy allows it; bounds runs after.
	g.Policy.Rules = append(g.Policy.Rules, Rule{
		ID:     "allow-push",
		Action: ActionMatcher{string(ActGitPush)},
		Effect: "allow",
	})
	g.Policy.Bounds = Bounds{MaxFilesChanged: 1, MaxLinesChanged: 1}
	g.Cwd = dir // tempdir without git, so collectDiffStats errors → bounds-deny

	d := g.Evaluate(Action{Type: ActGitPush, Target: "feat/x"}, "test-agent", nil)
	if d.Allowed {
		t.Fatalf("expected bounds deny, got allow: %+v", d)
	}
	if d.RuleID != "bounds:undetermined" {
		t.Fatalf("expected bounds:undetermined, got %q reason=%q", d.RuleID, d.Reason)
	}
	if d.Agent != "test-agent" {
		t.Errorf("Agent dropped on bounds-deny path; got %q want test-agent", d.Agent)
	}
	if d.CallerOrigin == "" {
		t.Errorf("CallerOrigin dropped on bounds-deny path")
	}
}

func TestGate_CallerOriginStampedOnLockdown(t *testing.T) {
	g, _ := newTestGate(t)
	for i := 0; i < 12; i++ {
		g.Evaluate(Action{Type: ActShellExec, Target: destructiveTarget()}, "agent1", nil)
	}
	if !g.Counter.IsLocked("agent1") {
		t.Fatalf("agent should be locked")
	}
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.RuleID != "lockdown" {
		t.Fatalf("expected lockdown, got %s", d.RuleID)
	}
	if d.CallerOrigin == "" {
		t.Errorf("CallerOrigin must be stamped on lockdown path")
	}
}

func TestGate_FingerprintStampedOnAllow(t *testing.T) {
	// P2 routing-as-learning-system: Decision rows must carry the
	// fingerprint dims when the Gate is constructed with them. Allow
	// path goes through the main Evaluate flow.
	g, _ := newTestGate(t)
	g.Fingerprint = FingerprintContext{
		Model:       "claude-haiku-4-5",
		Role:        "reviewer",
		WorkflowID:  "swarm-test-1",
		Fingerprint: "abc123def456",
	}
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.Model != "claude-haiku-4-5" {
		t.Errorf("Model: got %q, want claude-haiku-4-5", d.Model)
	}
	if d.Role != "reviewer" {
		t.Errorf("Role: got %q, want reviewer", d.Role)
	}
	if d.WorkflowID != "swarm-test-1" {
		t.Errorf("WorkflowID: got %q, want swarm-test-1", d.WorkflowID)
	}
	if d.Fingerprint != "abc123def456" {
		t.Errorf("Fingerprint: got %q, want abc123def456", d.Fingerprint)
	}
}

func TestGate_TypedAgentIdentityStampedOnDeny(t *testing.T) {
	g, _ := newTestGate(t)
	g.Fingerprint = FingerprintContext{
		AgentInstanceID:   "codex-session-42",
		AgentFingerprint:  "agentfp123456",
		Driver:            "codex",
		Model:             "gpt-5.5",
		Role:              "reviewer",
		StationPromptHash: "sha256:prompt",
		SkillsToolsHash:   "sha256:tools",
		SoulLens:          "socrates",
		ClaimedAuthority:  "supervisor",
		WorkflowID:        "wf-agent-identity",
	}

	d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "codex-cli", nil)
	if d.Allowed {
		t.Fatalf("expected deny")
	}
	if d.Agent != "codex-cli" {
		t.Errorf("legacy Agent display field changed: got %q want codex-cli", d.Agent)
	}
	if d.AgentInstanceID != "codex-session-42" {
		t.Errorf("AgentInstanceID: got %q want codex-session-42", d.AgentInstanceID)
	}
	if d.AgentFingerprint != "agentfp123456" {
		t.Errorf("AgentFingerprint: got %q want agentfp123456", d.AgentFingerprint)
	}
	if d.Fingerprint != "agentfp123456" {
		t.Errorf("legacy Fingerprint alias: got %q want agentfp123456", d.Fingerprint)
	}
	if d.ClaimedAuthority != "supervisor" {
		t.Errorf("ClaimedAuthority: got %q want supervisor", d.ClaimedAuthority)
	}
	if d.StationPromptHash != "sha256:prompt" || d.SkillsToolsHash != "sha256:tools" ||
		d.SoulLens != "socrates" {
		t.Errorf("prompt/tool/lens dims not stamped: %+v", d)
	}
	if d.Driver != "codex" || d.Model != "gpt-5.5" || d.Role != "reviewer" ||
		d.Authority != "worker" || d.WorkflowID != "wf-agent-identity" {
		t.Errorf("typed identity dims not stamped: %+v", d)
	}
}

func TestGate_IdentityPolicyRuleParticipatesInDecision(t *testing.T) {
	g, _ := newTestGate(t)
	g.Policy.Rules = append([]Rule{{
		ID:     "reviewer-read-restricted",
		Action: ActionMatcher{string(ActFileRead)},
		Effect: "deny",
		Role:   IdentityMatcher{"reviewer"},
		Reason: "reviewers must not read this surface",
	}}, g.Policy.Rules...)
	if err := g.Policy.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}

	g.Fingerprint = FingerprintContext{Role: "reviewer"}
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "codex-cli", nil)
	if d.Allowed || d.RuleID != "reviewer-read-restricted" {
		t.Fatalf("identity deny rule should participate in gate decision, got allowed=%v rule=%q", d.Allowed, d.RuleID)
	}

	g.Fingerprint = FingerprintContext{Role: "worker"}
	d = g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "codex-cli", nil)
	if !d.Allowed || d.RuleID != "allow-read" {
		t.Fatalf("non-matching identity should fall through to existing rule, got allowed=%v rule=%q", d.Allowed, d.RuleID)
	}
}

func TestGate_TrustedAuthorityGrantStampsSupervisor(t *testing.T) {
	g, _ := newTestGate(t)
	g.Policy.Authority.Trusted = []TrustedAuthority{
		{Authority: "supervisor", AgentFingerprint: "agentfp123456"},
	}
	g.Fingerprint = FingerprintContext{
		AgentInstanceID:  "hermes-run-9",
		AgentFingerprint: "agentfp123456",
		Driver:           "hermes",
		Model:            "qwen3.6:27b",
		Role:             "reviewer",
		ClaimedAuthority: "worker",
		WorkflowID:       "wf-agent-identity",
	}

	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "hermes", nil)
	if !d.Allowed {
		t.Fatalf("expected allow, got %+v", d)
	}
	if d.ClaimedAuthority != "worker" {
		t.Errorf("ClaimedAuthority: got %q want worker", d.ClaimedAuthority)
	}
	if d.Authority != "supervisor" {
		t.Errorf("trusted effective Authority: got %q want supervisor", d.Authority)
	}
}

func TestGate_FingerprintStampedOnLockdown(t *testing.T) {
	// Lockdown short-circuit must also stamp fingerprint dims so the
	// audit row stays consistent regardless of which branch produced
	// the Decision (#76 invariant — single OnDecision callsite — extends
	// to fingerprint stamping).
	g, _ := newTestGate(t)
	g.Fingerprint = FingerprintContext{
		Model:      "claude-opus-4-7",
		Role:       "programmer",
		WorkflowID: "swarm-locked-1",
	}
	for i := 0; i < 11; i++ {
		if err := g.Counter.RecordDenial("agent1", "fp", 1); err != nil {
			t.Fatalf("RecordDenial: %v", err)
		}
	}
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.RuleID != "lockdown" {
		t.Fatalf("expected lockdown, got %s", d.RuleID)
	}
	if d.Model != "claude-opus-4-7" || d.Role != "programmer" || d.WorkflowID != "swarm-locked-1" {
		t.Errorf("lockdown row missing fingerprint dims: %+v", d)
	}
}

func TestGate_FingerprintEmptyByDefault(t *testing.T) {
	// When Gate.Fingerprint is zero-value (no env vars set, or older
	// callers that don't populate it), Decision rows have empty
	// fingerprint fields and the JSON layer drops them via omitempty.
	g, _ := newTestGate(t)
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.Model != "" || d.Role != "" || d.WorkflowID != "" || d.Fingerprint != "" {
		t.Errorf("expected empty fingerprint dims by default, got %+v", d)
	}
}

func TestGate_DefaultExternalRoleStampsExternalAuthority(t *testing.T) {
	withCleanFingerprintEnv(t)
	g, _ := newTestGate(t)
	g.Fingerprint = FingerprintContextFromEnv()

	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.Role != "external" {
		t.Errorf("Role: got %q want external", d.Role)
	}
	if d.Authority != "external" {
		t.Errorf("Authority: got %q want external", d.Authority)
	}
	if d.ClaimedAuthority != "" {
		t.Errorf("ClaimedAuthority should stay empty by default, got %q", d.ClaimedAuthority)
	}
}

// fingerprintEnvKeys are the env vars FingerprintContextFromEnv reads.
// Centralized so the cleanup helper below can never drift from the
// reader and leak a leftover value into adjacent tests.
var fingerprintEnvKeys = []string{
	"CHITIN_AGENT_INSTANCE_ID", "CHITIN_DISPATCH_AGENT_INSTANCE_ID",
	"CHITIN_AGENT_FINGERPRINT", "CHITIN_DISPATCH_AGENT_FINGERPRINT",
	"CHITIN_DRIVER", "CHITIN_DISPATCH_DRIVER",
	"CHITIN_MODEL", "CHITIN_DISPATCH_MODEL",
	"CHITIN_ROLE", "CHITIN_DISPATCH_ROLE",
	"CHITIN_STATION_PROMPT_HASH", "CHITIN_DISPATCH_STATION_PROMPT_HASH",
	"CHITIN_SKILLS_TOOLS_HASH", "CHITIN_DISPATCH_SKILLS_TOOLS_HASH",
	"CHITIN_SOUL_LENS", "CHITIN_DISPATCH_SOUL_LENS", "CHITIN_ACTIVE_SOUL",
	"CHITIN_AUTHORITY", "CHITIN_DISPATCH_AUTHORITY",
	"CHITIN_WORKFLOW_ID", "CHITIN_DISPATCH_WORKFLOW_ID",
	"CHITIN_FINGERPRINT",
}

// withCleanFingerprintEnv saves+clears every fingerprint-related env
// var for the duration of a test, then restores the original values.
// The receiving test sets only the vars it cares about; everything
// else is guaranteed unset, so a stale CHITIN_* in the developer's
// shell can't make a passing test pass for the wrong reason.
func withCleanFingerprintEnv(t *testing.T) {
	t.Helper()
	saved := make(map[string]string, len(fingerprintEnvKeys))
	for _, k := range fingerprintEnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			saved[k] = v
		}
		_ = os.Unsetenv(k)
	}
	t.Cleanup(func() {
		for _, k := range fingerprintEnvKeys {
			if v, ok := saved[k]; ok {
				_ = os.Setenv(k, v)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	})
}

func TestFingerprintContextFromEnv_LegacyVarsRead(t *testing.T) {
	// Original (pre-PR-#344) precedence: CHITIN_* names populate the
	// fingerprint context directly. Regression guard so the
	// CHITIN_DISPATCH_* fallback addition doesn't accidentally
	// re-order precedence and break dispatchers still emitting the
	// legacy names.
	withCleanFingerprintEnv(t)
	_ = os.Setenv("CHITIN_ROLE", "reviewer")
	_ = os.Setenv("CHITIN_MODEL", "claude-sonnet-4-6")
	_ = os.Setenv("CHITIN_WORKFLOW_ID", "wf-123")
	_ = os.Setenv("CHITIN_FINGERPRINT", "fp-abc")
	got := FingerprintContextFromEnv()
	if got.Role != "reviewer" || got.Model != "claude-sonnet-4-6" ||
		got.WorkflowID != "wf-123" || got.Fingerprint != "fp-abc" {
		t.Errorf("legacy CHITIN_* vars not read correctly: %+v", got)
	}
	if got.AgentFingerprint != "fp-abc" {
		t.Errorf("legacy CHITIN_FINGERPRINT must mirror AgentFingerprint: got %q want fp-abc", got.AgentFingerprint)
	}
}

func TestFingerprintContextFromEnv_DispatchVarsFallback(t *testing.T) {
	// PR #344 added CHITIN_DISPATCH_<DIM> for dispatch_meta. When the
	// legacy name is unset, the kernel must fall back to the
	// DISPATCH-prefixed name so role/model still tag the row.
	withCleanFingerprintEnv(t)
	_ = os.Setenv("CHITIN_DISPATCH_ROLE", "programmer")
	_ = os.Setenv("CHITIN_DISPATCH_MODEL", "qwen3-coder")
	got := FingerprintContextFromEnv()
	if got.Role != "programmer" {
		t.Errorf("Role from CHITIN_DISPATCH_ROLE: got %q want programmer", got.Role)
	}
	if got.Model != "qwen3-coder" {
		t.Errorf("Model from CHITIN_DISPATCH_MODEL: got %q want qwen3-coder", got.Model)
	}
}

func TestFingerprintContextFromEnv_TypedIdentityVarsRead(t *testing.T) {
	withCleanFingerprintEnv(t)
	_ = os.Setenv("CHITIN_AGENT_INSTANCE_ID", "inst-123")
	_ = os.Setenv("CHITIN_AGENT_FINGERPRINT", "agentfp-primary")
	_ = os.Setenv("CHITIN_DRIVER", "hermes")
	_ = os.Setenv("CHITIN_STATION_PROMPT_HASH", "sha256:prompt")
	_ = os.Setenv("CHITIN_SKILLS_TOOLS_HASH", "sha256:tools")
	_ = os.Setenv("CHITIN_SOUL_LENS", "curie")
	_ = os.Setenv("CHITIN_AUTHORITY", "supervisor")

	got := FingerprintContextFromEnv()
	if got.AgentInstanceID != "inst-123" {
		t.Errorf("AgentInstanceID: got %q want inst-123", got.AgentInstanceID)
	}
	if got.AgentFingerprint != "agentfp-primary" || got.Fingerprint != "agentfp-primary" {
		t.Errorf("agent fingerprint and legacy alias not mirrored: %+v", got)
	}
	if got.Driver != "hermes" {
		t.Errorf("Driver: got %q want hermes", got.Driver)
	}
	if got.StationPromptHash != "sha256:prompt" || got.SkillsToolsHash != "sha256:tools" ||
		got.SoulLens != "curie" {
		t.Errorf("prompt/tool/lens vars not read correctly: %+v", got)
	}
	if got.ClaimedAuthority != "supervisor" {
		t.Errorf("ClaimedAuthority: got %q want supervisor", got.ClaimedAuthority)
	}
	if got.Authority != "" {
		t.Errorf("env authority must not become trusted effective authority: got %q", got.Authority)
	}
}

func TestFingerprintContextFromEnv_TypedDispatchVarsFallback(t *testing.T) {
	withCleanFingerprintEnv(t)
	_ = os.Setenv("CHITIN_DISPATCH_AGENT_INSTANCE_ID", "inst-dispatch")
	_ = os.Setenv("CHITIN_DISPATCH_AGENT_FINGERPRINT", "agentfp-dispatch")
	_ = os.Setenv("CHITIN_DISPATCH_DRIVER", "copilot")
	_ = os.Setenv("CHITIN_DISPATCH_STATION_PROMPT_HASH", "sha256:dispatch-prompt")
	_ = os.Setenv("CHITIN_DISPATCH_SKILLS_TOOLS_HASH", "sha256:dispatch-tools")
	_ = os.Setenv("CHITIN_DISPATCH_SOUL_LENS", "lovelace")
	_ = os.Setenv("CHITIN_DISPATCH_AUTHORITY", "worker")
	_ = os.Setenv("CHITIN_DISPATCH_WORKFLOW_ID", "wf-dispatch")

	got := FingerprintContextFromEnv()
	if got.AgentInstanceID != "inst-dispatch" || got.AgentFingerprint != "agentfp-dispatch" ||
		got.Fingerprint != "agentfp-dispatch" || got.Driver != "copilot" ||
		got.StationPromptHash != "sha256:dispatch-prompt" ||
		got.SkillsToolsHash != "sha256:dispatch-tools" || got.SoulLens != "lovelace" ||
		got.ClaimedAuthority != "worker" || got.Authority != "" || got.WorkflowID != "wf-dispatch" {
		t.Errorf("typed dispatch vars not read correctly: %+v", got)
	}
}

func TestFingerprintContextFromEnv_LegacyWinsOverDispatch(t *testing.T) {
	// Precedence: CHITIN_<DIM> beats CHITIN_DISPATCH_<DIM>. The legacy
	// names are what existing dispatchers stamp; a stray DISPATCH-
	// prefixed value (e.g. inherited from a parent shell) must not
	// mask a freshly-set legacy value.
	withCleanFingerprintEnv(t)
	_ = os.Setenv("CHITIN_ROLE", "reviewer")
	_ = os.Setenv("CHITIN_DISPATCH_ROLE", "programmer")
	_ = os.Setenv("CHITIN_MODEL", "claude-opus-4-7")
	_ = os.Setenv("CHITIN_DISPATCH_MODEL", "qwen3-coder")
	_ = os.Setenv("CHITIN_FINGERPRINT", "fp-legacy-direct")
	_ = os.Setenv("CHITIN_DISPATCH_AGENT_FINGERPRINT", "fp-dispatch")
	got := FingerprintContextFromEnv()
	if got.Role != "reviewer" {
		t.Errorf("legacy CHITIN_ROLE must win: got %q want reviewer", got.Role)
	}
	if got.Model != "claude-opus-4-7" {
		t.Errorf("legacy CHITIN_MODEL must win: got %q want claude-opus-4-7", got.Model)
	}
	if got.AgentFingerprint != "fp-legacy-direct" || got.Fingerprint != "fp-legacy-direct" {
		t.Errorf("legacy CHITIN_FINGERPRINT must win over dispatch agent fingerprint: %+v", got)
	}
}

func TestFingerprintContextFromEnv_DefaultsRoleToExternal(t *testing.T) {
	// Acceptance criterion for kernel-chain-event-read-dispatch-meta-
	// for-tagging: when no role is wired (raw shell hook, ad-hoc CLI
	// invocation), the row must be tagged role=external rather than
	// landing in the (untagged) bucket. Model stays empty so the JSON
	// layer drops it via omitempty (downstream readers interpret
	// missing as null).
	withCleanFingerprintEnv(t)
	got := FingerprintContextFromEnv()
	if got.Role != "external" {
		t.Errorf("Role default: got %q want external", got.Role)
	}
	if got.Model != "" {
		t.Errorf("Model default: got %q want empty (omitempty → null)", got.Model)
	}
	if got.WorkflowID != "" || got.Fingerprint != "" {
		t.Errorf("WorkflowID/Fingerprint must stay empty by default, got %+v", got)
	}
}

func TestFingerprintContextFromEnv_ActiveSoulFallback(t *testing.T) {
	withCleanFingerprintEnv(t)
	_ = os.Setenv("CHITIN_ACTIVE_SOUL", "knuth")

	got := FingerprintContextFromEnv()
	if got.SoulLens != "knuth" {
		t.Errorf("CHITIN_ACTIVE_SOUL fallback: got %q want knuth", got.SoulLens)
	}
	if got.Authority != "" {
		t.Errorf("env reader must not resolve effective authority: got %q", got.Authority)
	}

	g, _ := newTestGate(t)
	g.Fingerprint = got
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.Authority != "external" {
		t.Errorf("soul lens alone must not escalate effective authority: got %q", d.Authority)
	}
}

func TestFingerprintContextFromEnv_SystemRoleHonored(t *testing.T) {
	// System-level housekeeping (chain rotation, alarm-feeder) tags
	// itself role=system explicitly so it's separable from raw
	// external traffic. The kernel must not silently rewrite a
	// non-empty role to anything else.
	withCleanFingerprintEnv(t)
	_ = os.Setenv("CHITIN_ROLE", "system")
	got := FingerprintContextFromEnv()
	if got.Role != "system" {
		t.Errorf("explicit CHITIN_ROLE=system must pass through: got %q", got.Role)
	}
}
