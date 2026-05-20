package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestCLI_DriversListJSON(t *testing.T) {
	wd := t.TempDir()
	policyPath := filepath.Join(wd, "chitin.yaml")
	writeFileForCLI(t, policyPath, `
id: driver-policy
mode: enforce
drivers:
  - id: copilot
    identities: [copilot, github-copilot]
  - id: codex
    identities: [codex]
rules:
  - id: allow-reads
    action: file.read
    effect: allow
`)

	stdout, stderr, code := runCLI(t, wd,
		"drivers", "list",
		"--json",
		"--policy-file", policyPath,
	)
	if code != 0 {
		t.Fatalf("exit %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	var out struct {
		PolicyID string `json:"policy_id"`
		Count    int    `json:"count"`
		Drivers  []struct {
			ID         string   `json:"id"`
			Identities []string `json:"identities"`
		} `json:"drivers"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("unmarshal stdout: %v", err)
	}
	if out.PolicyID != "driver-policy" {
		t.Fatalf("policy_id=%q want driver-policy", out.PolicyID)
	}
	if out.Count != 2 {
		t.Fatalf("count=%d want 2", out.Count)
	}
	if len(out.Drivers) != 2 {
		t.Fatalf("len(drivers)=%d want 2", len(out.Drivers))
	}
	if out.Drivers[0].ID != "copilot" || len(out.Drivers[0].Identities) != 2 {
		t.Fatalf("first driver=%+v want copilot with identities", out.Drivers[0])
	}
}
