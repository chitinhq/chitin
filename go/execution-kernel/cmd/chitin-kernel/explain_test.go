package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_Explain_BlockedDecisionWithSignalsAndNearMisses(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	policy := `id: explain-test
mode: enforce
rules:
  - id: deny-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "dangerous delete"
    suggestion: "use git rm"
    correctedCommand: "git rm <files>"
  - id: allow-reviewed-rm
    action: shell.exec
    effect: allow
    role: reviewer
    target: "rm -rf /tmp/demo"
    reason: "reviewers may use the fixture command"
  - id: allow-shell
    action: shell.exec
    effect: allow
    reason: "general shell access"
`
	if err := os.WriteFile(filepath.Join(repo, "chitin.yaml"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	events := `{"schema_version":"2","run_id":"run-1","session_id":"sess-1","surface":"codex","agent_instance_id":"agent-1","agent_fingerprint":"fp","event_type":"decision","chain_id":"sess-1","chain_type":"session","seq":0,"this_hash":"event-deny","ts":"2026-05-14T12:00:00Z","labels":{"driver":"codex","agent_instance_id":"agent-1","agent":"agent-1"},"payload":{"tool_name":"shell.exec","action_type":"shell.exec","action_target":"rm -rf /tmp/demo","decision":"deny","rule_id":"deny-rm"}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "events-run-1.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	govRows := `{"allowed":false,"mode":"enforce","rule_id":"deny-rm","reason":"dangerous delete","suggestion":"use git rm","corrected_command":"git rm <files>","action_type":"shell.exec","action_target":"rm -rf /tmp/demo","ts":"2026-05-14T12:00:00Z","driver":"codex","agent_instance_id":"agent-1","agent":"agent-1","role":"worker"}` + "\n" +
		`{"allowed":false,"mode":"monitor","rule_id":"router-heuristic:deny-rm","action_type":"router.signal","action_target":"Bash:rm -rf /tmp/demo","ts":"2026-05-14T12:00:00.123456789Z","agent":"agent-1","predicted_blast":0.91,"floundering_score":0.10,"drift_score":0.22}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "gov-decisions-2026-05-14.jsonl"), []byte(govRows), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLIWithHome(t, home, "explain", "event-deny", "--cwd", repo)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{
		"BLOCKED by deny-rm",
		"Action: shell.exec rm -rf /tmp/demo",
		"Why: dangerous delete",
		"deny-rm (deny, mode=enforce)",
		"predicted_blast=0.91 floundering=0.10 drift=0.22",
		"allow-reviewed-rm (allow, score=",
		"role: wanted reviewer; got \"worker\"",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout)
		}
	}
}

func TestCLI_Explain_BoundaryEmpty_NoSignalsOrNearMisses(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	policy := `id: explain-empty
mode: guide
rules:
  - id: allow-status
    action: shell.exec
    effect: allow
    target: "git status"
    reason: "status is safe"
`
	if err := os.WriteFile(filepath.Join(repo, "chitin.yaml"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	events := `{"schema_version":"2","run_id":"run-empty","session_id":"sess-empty","surface":"codex","agent_instance_id":"agent-empty","agent_fingerprint":"fp","event_type":"decision","chain_id":"sess-empty","chain_type":"session","seq":0,"this_hash":"event-empty","ts":"2026-05-14T13:00:00Z","labels":{"driver":"codex","agent_instance_id":"agent-empty","agent":"agent-empty"},"payload":{"tool_name":"shell.exec","action_type":"shell.exec","action_target":"git status","decision":"allow","rule_id":"allow-status"}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "events-run-empty.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	govRows := `{"allowed":true,"mode":"guide","rule_id":"allow-status","reason":"status is safe","action_type":"shell.exec","action_target":"git status","ts":"2026-05-14T13:00:00Z","driver":"codex","agent_instance_id":"agent-empty","agent":"agent-empty"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "gov-decisions-2026-05-14.jsonl"), []byte(govRows), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLIWithHome(t, home, "explain", "event-empty", "--cwd", repo)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{
		"Signals\n- none recorded for this decision",
		"Near Misses\n- none",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout)
		}
	}
}

func TestCLI_Explain_BoundaryMax_NearMissLimit(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	policy := `id: explain-max
mode: enforce
rules:
  - id: deny-curl
    action: shell.exec
    effect: deny
    target: "curl"
    reason: "network shell denied"
  - id: near-role
    action: shell.exec
    effect: allow
    role: reviewer
    target: "curl"
  - id: near-model
    action: shell.exec
    effect: allow
    model: gpt-special
    target: "curl"
`
	if err := os.WriteFile(filepath.Join(repo, "chitin.yaml"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	events := `{"schema_version":"2","run_id":"run-max","session_id":"sess-max","surface":"codex","agent_instance_id":"agent-max","agent_fingerprint":"fp","event_type":"decision","chain_id":"sess-max","chain_type":"session","seq":0,"this_hash":"event-max","ts":"2026-05-14T13:30:00Z","labels":{"driver":"codex","agent_instance_id":"agent-max","agent":"agent-max"},"payload":{"tool_name":"shell.exec","action_type":"shell.exec","action_target":"curl https://example.test","decision":"deny","rule_id":"deny-curl"}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "events-run-max.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	govRows := `{"allowed":false,"mode":"enforce","rule_id":"deny-curl","reason":"network shell denied","action_type":"shell.exec","action_target":"curl https://example.test","ts":"2026-05-14T13:30:00Z","driver":"codex","agent_instance_id":"agent-max","agent":"agent-max","role":"worker","model":"gpt-default"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "gov-decisions-2026-05-14.jsonl"), []byte(govRows), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLIWithHome(t, home, "explain", "event-max", "--cwd", repo, "--near-miss-limit", "1")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "near-role (allow, score=") {
		t.Fatalf("stdout missing capped near miss\n%s", stdout)
	}
	if strings.Contains(stdout, "near-model (allow, score=") {
		t.Fatalf("stdout exceeded near miss max\n%s", stdout)
	}
}

func TestCLI_Explain_BoundaryError_MissingEvent(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	policy := `id: explain-error
mode: guide
rules:
  - id: allow-read
    action: file.read
    effect: allow
`
	if err := os.WriteFile(filepath.Join(repo, "chitin.yaml"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLIWithHome(t, home, "explain", "missing-event", "--cwd", repo)
	if code == 0 {
		t.Fatalf("expected nonzero exit for missing event")
	}
	if !strings.Contains(stderr, "explain_failed") || !strings.Contains(stderr, "missing-event") {
		t.Fatalf("stderr should explain missing event, got %s", stderr)
	}
}

// TestCLI_Explain_RejectsExtraPositionalArg covers the Copilot finding: when
// the event id was consumed from args[0], a trailing positional arg was
// silently ignored instead of rejected, hiding operator typos.
func TestCLI_Explain_RejectsExtraPositionalArg(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLIWithHome(t, home, "explain", "event-1", "stray-extra-arg", "--cwd", repo)
	if code == 0 {
		t.Fatalf("expected nonzero exit for an extra positional arg")
	}
	if !strings.Contains(stderr, "explain_missing_event_id") {
		t.Fatalf("stderr should reject the extra arg, got %s", stderr)
	}
}

// TestCLI_Explain_ResolvesDecisionFromPayloadIdentity covers the Copilot
// finding: readDecision built its gov-row lookup key from event *labels*
// only, so events that carry identity (driver/agent/agent_instance_id) in
// the payload — and not labels — failed to resolve their decision row.
func TestCLI_Explain_ResolvesDecisionFromPayloadIdentity(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	policy := `id: explain-payload-identity
mode: guide
rules:
  - id: allow-status
    action: shell.exec
    effect: allow
    target: "git status"
    reason: "status is safe"
`
	if err := os.WriteFile(filepath.Join(repo, "chitin.yaml"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	// labels carry NO driver/agent/agent_instance_id — identity lives only
	// in the payload, the way the kernel also mirrors it.
	events := `{"schema_version":"2","run_id":"run-pi","session_id":"sess-pi","surface":"codex","agent_instance_id":"agent-pi","agent_fingerprint":"fp","event_type":"decision","chain_id":"sess-pi","chain_type":"session","seq":0,"this_hash":"event-pi","ts":"2026-05-14T14:00:00Z","labels":{},"payload":{"tool_name":"shell.exec","action_type":"shell.exec","action_target":"git status","decision":"allow","rule_id":"allow-status","driver":"codex","agent":"agent-pi","agent_instance_id":"agent-pi"}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "events-run-pi.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	govRows := `{"allowed":true,"mode":"guide","rule_id":"allow-status","reason":"status is safe","action_type":"shell.exec","action_target":"git status","ts":"2026-05-14T14:00:00Z","driver":"codex","agent_instance_id":"agent-pi","agent":"agent-pi"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "gov-decisions-2026-05-14.jsonl"), []byte(govRows), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLIWithHome(t, home, "explain", "event-pi", "--cwd", repo)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s — decision row should resolve from payload identity", code, stderr)
	}
	if !strings.Contains(stdout, "ALLOWED by allow-status") {
		t.Fatalf("stdout missing resolved decision: %s", stdout)
	}
}

func TestCLI_Explain_AllowedDecision(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	policy := `id: explain-allow
mode: guide
rules:
  - id: allow-reads
    action: file.read
    effect: allow
    reason: "reads are safe"
`
	if err := os.WriteFile(filepath.Join(repo, "chitin.yaml"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	events := `{"schema_version":"2","run_id":"run-2","session_id":"sess-2","surface":"codex","agent_instance_id":"agent-2","agent_fingerprint":"fp","event_type":"decision","chain_id":"sess-2","chain_type":"session","seq":0,"this_hash":"event-allow","ts":"2026-05-14T12:30:00Z","labels":{"driver":"codex","agent_instance_id":"agent-2","agent":"agent-2"},"payload":{"tool_name":"file.read","action_type":"file.read","action_target":"README.md","decision":"allow","rule_id":"allow-reads"}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "events-run-2.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	govRows := `{"allowed":true,"mode":"guide","rule_id":"allow-reads","reason":"reads are safe","action_type":"file.read","action_target":"README.md","ts":"2026-05-14T12:30:00Z","driver":"codex","agent_instance_id":"agent-2","agent":"agent-2"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "gov-decisions-2026-05-14.jsonl"), []byte(govRows), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLIWithHome(t, home, "explain", "event-allow", "--cwd", repo)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{
		"ALLOWED by allow-reads",
		"Action: file.read README.md",
		"Bounds",
		"status: not_applicable",
		"Signals",
		"none recorded for this decision",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout)
		}
	}
}
