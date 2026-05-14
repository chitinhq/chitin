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
		`{"allowed":false,"mode":"monitor","rule_id":"router-heuristic:deny-rm","action_type":"router.signal","action_target":"Bash:rm -rf /tmp/demo","ts":"2026-05-14T12:00:00Z","agent":"agent-1","predicted_blast":0.91,"floundering_score":0.10,"drift_score":0.22}` + "\n"
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
