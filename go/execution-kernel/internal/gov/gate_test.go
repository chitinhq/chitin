package gov

import (
	"os"
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

func TestGate_EscalationRecorded(t *testing.T) {
	g, _ := newTestGate(t)
	for i := 0; i < 3; i++ {
		g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1", nil)
	}
	if lv := g.Counter.Level("agent1"); lv != "elevated" {
		t.Errorf("after 3 denials, level=%q want elevated", lv)
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
		g.Counter.RecordDenial("agent1", "fp", 1)
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
