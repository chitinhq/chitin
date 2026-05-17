package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestCLI_DriversListJSON(t *testing.T) {
	t.Setenv("CHITIN_POLICY_TRUST_DIR", filepath.Join(t.TempDir(), "trust"))
	wd := t.TempDir()
	writeFileForCLI(t, filepath.Join(wd, "chitin.yaml"), `
id: test-policy
mode: enforce
drivers:
  - id: copilot
    driver: copilot
  - id: openclaw-glm-flash
    driver: openclaw
    model: glm-4.7-flash
    role: programmer
rules: []
`)

	stdout, stderr, code := runCLI(t, wd, "drivers", "list", "--json", "--cwd", wd)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	var payload struct {
		Drivers []map[string]any `json:"drivers"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("unmarshal stdout: %v", err)
	}
	if len(payload.Drivers) != 2 {
		t.Fatalf("drivers len=%d want 2", len(payload.Drivers))
	}
	if got := payload.Drivers[1]["driver"]; got != "openclaw" {
		t.Fatalf("drivers[1].driver=%v want openclaw", got)
	}
	if got := payload.Drivers[1]["model"]; got != "glm-4.7-flash" {
		t.Fatalf("drivers[1].model=%v want glm-4.7-flash", got)
	}
}

func TestCLI_DriversListJSON_PolicyFile(t *testing.T) {
	t.Setenv("CHITIN_POLICY_TRUST_DIR", filepath.Join(t.TempDir(), "trust"))
	policyDir := t.TempDir()
	policyPath := filepath.Join(policyDir, "custom-chitin.yaml")
	writeFileForCLI(t, policyPath, `
id: test-policy
mode: enforce
drivers:
  - id: codex
rules: []
`)

	stdout, stderr, code := runCLI(t, t.TempDir(), "drivers", "list", "--json", "--policy-file", policyPath)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout == "" {
		t.Fatal("stdout empty")
	}
}
