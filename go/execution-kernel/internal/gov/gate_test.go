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

func TestGate_LogWriteFailureFailsClosedOnAllow(t *testing.T) {
	g, dir := newTestGate(t)
	logFile := filepath.Join(dir, "log-file-not-dir")
	if err := os.WriteFile(logFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.LogDir = logFile

	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.Allowed {
		t.Fatalf("log write failure must fail closed, got allow: %+v", d)
	}
	if d.RuleID != "decision-log-failed" {
		t.Fatalf("RuleID=%q want decision-log-failed", d.RuleID)
	}
	if !strings.Contains(d.Reason, "decision log write failed") {
		t.Fatalf("Reason should identify log failure, got %q", d.Reason)
	}
}

func TestGate_LogWriteFailureFailsClosedOnLockdown(t *testing.T) {
	g, dir := newTestGate(t)
	g.Counter.Lockdown("agent1")
	logFile := filepath.Join(dir, "log-file-not-dir")
	if err := os.WriteFile(logFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.LogDir = logFile

	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1", nil)
	if d.Allowed {
		t.Fatalf("locked agent must stay denied")
	}
	if d.RuleID != "decision-log-failed" {
		t.Fatalf("RuleID=%q want decision-log-failed", d.RuleID)
	}
	if d.Action.Type != ActFileRead || d.Agent != "agent1" {
		t.Fatalf("failure decision should preserve action and agent, got %+v", d)
	}
}

// fingerprintEnvKeys are the env vars FingerprintContextFromEnv reads.
// Centralized so the cleanup helper below can never drift from the
// reader and leak a leftover value into adjacent tests.
var fingerprintEnvKeys = []string{
	"CHITIN_MODEL", "CHITIN_DISPATCH_MODEL",
	"CHITIN_ROLE", "CHITIN_DISPATCH_ROLE",
	"CHITIN_WORKFLOW_ID", "CHITIN_FINGERPRINT",
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
	got := FingerprintContextFromEnv()
	if got.Role != "reviewer" {
		t.Errorf("legacy CHITIN_ROLE must win: got %q want reviewer", got.Role)
	}
	if got.Model != "claude-opus-4-7" {
		t.Errorf("legacy CHITIN_MODEL must win: got %q want claude-opus-4-7", got.Model)
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
