package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/sidecar"
)

func TestCLI_ChainReplay_JSONAndText(t *testing.T) {
	home := t.TempDir()
	body := `{"schema_version":"2","run_id":"run-1","session_id":"sess-cli","surface":"codex","agent_instance_id":"agent-1","agent_fingerprint":"fp","event_type":"decision","chain_id":"sess-cli","chain_type":"session","seq":0,"this_hash":"h1","ts":"2026-05-13T10:00:00Z","labels":{"driver":"codex","agent_instance_id":"agent-1"},"payload":{"tool_name":"shell.exec","action_type":"shell.exec","action_target":"echo hi","decision":"allow","rule_id":"allow-shell"}}` + "\n" +
		`{"schema_version":"2","run_id":"run-1","session_id":"sess-cli","surface":"codex","agent_instance_id":"agent-1","agent_fingerprint":"fp","event_type":"post_tool_use","chain_id":"sess-cli","chain_type":"session","seq":1,"this_hash":"h2","ts":"2026-05-13T10:00:01Z","labels":{"driver":"codex","agent_instance_id":"agent-1"},"payload":{"tool_name":"shell.exec","duration_ms":25}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "events-run-1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gov := `{"allowed":true,"mode":"enforce","rule_id":"allow-shell","action_type":"shell.exec","action_target":"echo hi","ts":"2026-05-13T10:00:00Z","driver":"codex","agent_instance_id":"agent-1","agent":"agent-1","cost_usd":0.5}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "gov-decisions-2026-05-13.jsonl"), []byte(gov), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLIWithHome(t, home, "chain", "replay", "--session=sess-cli")
	if code != 0 {
		t.Fatalf("json exit=%d stderr=%s", code, stderr)
	}
	var got struct {
		SessionID string `json:"session_id"`
		Summary   struct {
			ToolCallCount int     `json:"tool_call_count"`
			TotalCostUSD  float64 `json:"total_cost_usd"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("parse replay json: %v\n%s", err, stdout)
	}
	if got.SessionID != "sess-cli" || got.Summary.ToolCallCount != 1 || got.Summary.TotalCostUSD != 0.5 {
		t.Fatalf("unexpected replay json: %+v", got)
	}

	stdout, stderr, code = runCLIWithHome(t, home, "chain", "replay", "--session=sess-cli", "--format=text")
	if code != 0 {
		t.Fatalf("text exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "session sess-cli") || !strings.Contains(stdout, "shell.exec") {
		t.Fatalf("text output missing expected content: %s", stdout)
	}
}

func TestCLI_ChainSessions_Recent(t *testing.T) {
	home := t.TempDir()
	rows := map[string]string{
		"a": `{"schema_version":"2","run_id":"a","session_id":"sess-old","surface":"codex","agent_instance_id":"a1","agent_fingerprint":"fp","event_type":"decision","chain_id":"sess-old","chain_type":"session","seq":0,"this_hash":"h1","ts":"2026-05-13T09:00:00Z","labels":{"driver":"codex","agent_instance_id":"a1"},"payload":{"tool_name":"file.read"}}` + "\n",
		"b": `{"schema_version":"2","run_id":"b","session_id":"sess-new","surface":"codex","agent_instance_id":"a2","agent_fingerprint":"fp","event_type":"decision","chain_id":"sess-new","chain_type":"session","seq":0,"this_hash":"h2","ts":"2026-05-13T12:00:00Z","labels":{"driver":"hermes","agent_instance_id":"a2"},"payload":{"tool_name":"shell.exec"}}` + "\n",
	}
	for runID, body := range rows {
		if err := os.WriteFile(filepath.Join(home, "events-"+runID+".jsonl"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := runCLIWithHome(t, home, "chain", "sessions", "--recent=1")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "sess-new") {
		t.Fatalf("expected most recent session in output, got %s", stdout)
	}
}

func TestCLI_ChainBlobs(t *testing.T) {
	home := t.TempDir()
	store, err := sidecar.Open(filepath.Join(home, "sidecar.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("evt-blob-1", "prompt", []byte(`{"prompt":"run tests"}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("evt-blob-1", "tool_input", []byte(`{"command":"go test ./..."}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("evt-blob-1", "thinking", []byte(`"reasoning"`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLIWithHome(t, home, "chain", "blobs", "--event-id=evt-blob-1")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("parse chain blobs: %v\n%s", err, stdout)
	}
	if got["event_id"] != "evt-blob-1" {
		t.Fatalf("event_id=%v", got["event_id"])
	}
	if got["prompt"] == nil || got["tool_input"] == nil || got["thinking"] == nil {
		t.Fatalf("missing blobs: %+v", got)
	}
}

func TestCLI_ChainStats_WindowHours(t *testing.T) {
	home := t.TempDir()
	now := time.Now().UTC()
	body := `{"ts":"` + now.Add(-30*time.Minute).Format(time.RFC3339) + `","event_type":"decision","payload":{"tool_name":"Bash","action_type":"shell.exec","decision":"allow","rule_id":"allow"}}` + "\n" +
		`{"ts":"` + now.Add(-48*time.Hour).Format(time.RFC3339) + `","event_type":"decision","payload":{"tool_name":"Read","action_type":"file.read","decision":"allow","rule_id":"allow"}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "events-stats.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLIWithHome(t, home, "chain", "stats", "--window-hours", "24", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	var got struct {
		Total   int                    `json:"total_decisions"`
		Window  string                 `json:"window"`
		Buckets map[string]interface{} `json:"buckets"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("parse stdout: %v\n%s", err, stdout)
	}
	if got.Total != 1 || got.Window != "24h" {
		t.Fatalf("got total=%d window=%q; want total=1 window=24h", got.Total, got.Window)
	}
	if _, ok := got.Buckets["Bash"]; !ok {
		t.Fatalf("expected Bash bucket, got %+v", got.Buckets)
	}
	if _, ok := got.Buckets["Read"]; ok {
		t.Fatalf("old Read bucket should be filtered, got %+v", got.Buckets)
	}
}

func TestCLI_ChainRelated_KindAndLimit(t *testing.T) {
	home := t.TempDir()
	rows := map[string]string{
		"audit-1": `{"event_type":"swarm.audit.summary","payload":{"bullets":[]}}` + "\n",
		"audit-2": `{"event_type":"swarm.audit.summary","payload":{"bullets":[]}}` + "\n",
		"other":   `{"event_type":"decision","payload":{"decision":"allow"}}` + "\n",
	}
	for sid, body := range rows {
		if err := os.WriteFile(filepath.Join(home, "events-"+sid+".jsonl"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, code := runCLIWithHome(t, home, "chain", "related", "--kind", "swarm.audit.summary", "--limit", "1")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	lines := strings.Fields(strings.TrimSpace(stdout))
	if len(lines) != 1 {
		t.Fatalf("got lines %v; want exactly one due to --limit=1", lines)
	}
	if lines[0] != "audit-1" && lines[0] != "audit-2" {
		t.Fatalf("got %q; want one audit session", lines[0])
	}
}
