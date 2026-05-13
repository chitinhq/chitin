package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
