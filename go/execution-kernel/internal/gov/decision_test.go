package gov

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteLog_PersistsCallerOrigin(t *testing.T) {
	// Regression: WriteLog had its own inline struct that dropped CallerOrigin
	// at log-write time, so the field was set on Decision but absent from JSONL.
	// The analysis layer's `decisions_missing_envelope_id` finding would then
	// over-count silently. This test enforces parity: every JSON tag on
	// Decision that's audit-relevant must round-trip through WriteLog.
	dir := t.TempDir()
	d := Decision{
		Allowed: true, Mode: "enforce", RuleID: "default-allow-reads",
		Ts:           "2026-04-30T12:00:00Z",
		CallerOrigin: "main.go:42",
	}
	if err := WriteLog(d, dir); err != nil {
		t.Fatalf("WriteLog: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 log file")
	}
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if !strings.Contains(string(data), `"caller_origin":"main.go:42"`) {
		t.Errorf("caller_origin not persisted to log; got: %s", string(data))
	}
}

func TestWriteLog_PersistsFingerprintDims(t *testing.T) {
	// P2 routing-as-learning-system: chain rows must carry the four
	// fingerprint dimensions (model, role, workflow_id, fingerprint) so
	// downstream analysis can join Decisions to PR/review outcomes.
	// This test pins the JSON-tag layout so a future field rename
	// won't silently break the analysis joins.
	dir := t.TempDir()
	d := Decision{
		Allowed: true, Mode: "enforce", RuleID: "default-allow-shell",
		Ts:    "2026-05-04T17:30:00Z",
		Agent: "claude-code", Action: Action{Type: ActShellExec, Target: "ls"},
		Model:       "claude-haiku-4-5",
		Role:        "reviewer",
		WorkflowID:  "swarm-foo-1234567890",
		Fingerprint: "abc123def456",
	}
	if err := WriteLog(d, dir); err != nil {
		t.Fatalf("WriteLog: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(entries))
	}
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	for _, want := range []string{
		`"model":"claude-haiku-4-5"`,
		`"role":"reviewer"`,
		`"workflow_id":"swarm-foo-1234567890"`,
		`"fingerprint":"abc123def456"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in JSONL; got: %s", want, string(data))
		}
	}
}

func TestWriteLog_PersistsTypedAgentIdentity(t *testing.T) {
	dir := t.TempDir()
	d := Decision{
		Allowed: true, Mode: "enforce", RuleID: "default-allow-shell",
		Ts:    "2026-05-09T13:45:00Z",
		Agent: "codex-cli", Action: Action{Type: ActShellExec, Target: "ls"},
		AgentInstanceID:  "codex-session-42",
		AgentFingerprint: "agentfp123456",
		Driver:           "codex",
		Model:            "gpt-5.5",
		Role:             "reviewer",
		ClaimedAuthority: "supervisor",
		Authority:        "worker",
		WorkflowID:       "wf-agent-identity",
		Fingerprint:      "agentfp123456",
	}
	if err := WriteLog(d, dir); err != nil {
		t.Fatalf("WriteLog: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	for _, want := range []string{
		`"agent":"codex-cli"`,
		`"agent_instance_id":"codex-session-42"`,
		`"agent_fingerprint":"agentfp123456"`,
		`"driver":"codex"`,
		`"model":"gpt-5.5"`,
		`"role":"reviewer"`,
		`"claimed_authority":"supervisor"`,
		`"authority":"worker"`,
		`"workflow_id":"wf-agent-identity"`,
		`"fingerprint":"agentfp123456"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in JSONL; got: %s", want, string(data))
		}
	}
}

func TestWriteLog_OmitsEmptyFingerprintDims(t *testing.T) {
	// Backwards compatibility: pre-fingerprint dispatches (operator
	// manual runs, older swarm builds, ad-hoc CLI invocations) write
	// rows without the new fields. Existing readers must keep working
	// — the omitempty JSON tags drop empty values from the row.
	dir := t.TempDir()
	d := Decision{
		Allowed: true, Mode: "enforce", RuleID: "default-allow-shell",
		Ts: "2026-05-04T17:30:00Z",
		// Fingerprint dims deliberately empty.
	}
	if err := WriteLog(d, dir); err != nil {
		t.Fatalf("WriteLog: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	for _, unwant := range []string{
		`"agent_instance_id":`,
		`"agent_fingerprint":`,
		`"driver":`,
		`"model":`,
		`"role":`,
		`"claimed_authority":`,
		`"authority":`,
		`"workflow_id":`,
		`"fingerprint":`,
	} {
		if strings.Contains(string(data), unwant) {
			t.Errorf("unexpected %q in JSONL when fingerprint dim is empty; got: %s", unwant, string(data))
		}
	}
}

func TestWriteLog_AppendsOneJSONLine(t *testing.T) {
	dir := t.TempDir()
	d := Decision{
		Allowed: false, Mode: "guide", RuleID: "no-rm",
		Reason: "no rm", Ts: "2026-04-22T00:00:00Z",
	}
	if err := WriteLog(d, dir); err != nil {
		t.Fatalf("WriteLog: %v", err)
	}

	// Find the file
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(entries))
	}
	path := filepath.Join(dir, entries[0].Name())
	f, _ := os.Open(path)
	defer f.Close()
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
		var got map[string]any
		if err := json.Unmarshal(sc.Bytes(), &got); err != nil {
			t.Errorf("line is not valid JSON: %v", err)
		}
		if got["rule_id"] != "no-rm" {
			t.Errorf("RuleID roundtrip: got %q", got["rule_id"])
		}
	}
	if lines != 1 {
		t.Errorf("expected 1 line, got %d", lines)
	}
}

func TestDecision_JSONL_CarriesAgent(t *testing.T) {
	dir := t.TempDir()
	// Capture now once: a separate time.Now() at filename-construction time
	// can land on the next UTC date when the test runs within ~1s of midnight,
	// flaking the assertion (issue #56).
	now := time.Now().UTC()
	d := Decision{
		Allowed: true,
		Agent:   "copilot-cli",
		Action:  Action{Type: "shell.exec", Target: "ls /tmp"},
		Ts:      now.Format(time.RFC3339),
	}
	if err := WriteLog(d, dir); err != nil {
		t.Fatalf("WriteLog: %v", err)
	}
	path := filepath.Join(dir, "gov-decisions-"+now.Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), `"agent":"copilot-cli"`) {
		t.Errorf("expected agent field in JSONL, got: %s", string(data))
	}
}

func TestWriteLog_MultipleAppends(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		_ = WriteLog(Decision{
			Allowed: true, Mode: "monitor", RuleID: "x",
			Ts: "2026-04-22T00:00:00Z",
		}, dir)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("should still be 1 file")
	}
	path := filepath.Join(dir, entries[0].Name())
	f, _ := os.Open(path)
	defer f.Close()
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
	}
	if lines != 5 {
		t.Errorf("expected 5 lines, got %d", lines)
	}
}
